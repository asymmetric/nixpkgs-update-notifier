package main

import (
	"context"
	"database/sql"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	u "net/url"
	"regexp"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/html"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

var matrixHomeserver = flag.String("matrix.homeserver", "matrix.org", "Matrix homeserver for the bot account")
var matrixUsername = flag.String("matrix.username", "", "Matrix bot username")

var mainURL = flag.String("url", "https://nixpkgs-update-logs.nix-community.org", "Webpage with logs")
var dbPath = flag.String("db", "data.db", "Path to the DB file")
var tickerOpt = flag.Duration("ticker", 24*time.Hour, "How often to check url")
var debug = flag.Bool("debug", false, "Enable debug logging")

var clients = struct {
	db     *sql.DB
	http   *http.Client
	matrix *mautrix.Client
}{
	http: &http.Client{},
}

// TODO: rename last_visited to last_log_date?
// Ensure invariant: a subscription can't be added before the corresponding package has had its last_visited column set.
// Otherwise we have cases where Scan fails (because it can't cast NULL to a string), and also we can end up sending notifications for old errors.
//
//go:embed db/schema.sql
var ddl string

// These two regexps are for parsing logs.
// - "error: " is a nix build error
// - "ExitFailure" is a nixpkgs-update error
// - "failed with" is a nixpkgs/maintainers/scripts/update.py error
var erroRE = regexp.MustCompile(`^error:|ExitFailure|failed with`)
var ignoRE = regexp.MustCompile(`^~.*|^\.\.`)

// These two regexps are for matching against user input.
// We want to avoid stuff like the following, because it leads us to spam the nix-community.org server.
// - sub *
// - sub pythonPackages.*
//
// Unsubbing with the same queries is OK, because it it has different semantics and doesn't spam upstream.
var dangerousRE = regexp.MustCompile(`^sub (?:[*?]+|\w+\.\*)$`)
var subUnsubRE = regexp.MustCompile(`^(un)?sub ([\w_?*.-]+)$`)
var ownDisownRE = regexp.MustCompile(`^(dis)?own$`)

// TODO: make configurable
var (
	repoOwner    = "asymmetric"
	repoName     = "nixpkgs-update-notifier"
	githubToken  = "your-github-token"
	artifactsURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/artifacts", repoOwner, repoName)
)

// These are abstracted so that we can pass a different function in tests.
type handlers struct {
	logFetcher func(string) (string, bool)
	sender     func(string, id.RoomID) (*mautrix.RespSendEvent, error)
}

var h handlers

const helpText = `Welcome to the nixpkgs-update-notifier bot!

These are the available commands:

- **sub foo**: subscribe to package <code>foo</code>
- **unsub foo**: unsubscribe from package <code>foo</code>
- **own*: subscribe to all packages you maintain
- **disown**: unsubscribe to all packages you maintain
- **subs**: list subscriptions
- **help**: show this help message

You can use the <code>*</code> and <code>?</code> globs in queries. Things you can do:

- <code>sub python31?Packages.acme</code>
- <code>sub *.acme</code>

Things you cannot do:

- <code>sub *</code>
- <code>sub ?</code>
- <code>sub foo.*</code>

The code for the bot is [here](https://github.com/asymmetric/nixpkgs-update-notifier).
`

func init() {
	// default handlers
	h = handlers{
		logFetcher:          fetchLastLog,
		sender:              sendMarkdown,
		packagesJSONFetcher: fetchPackagesJSON,
	}

}

func main() {
	ctx := context.Background()

	flag.Parse()

	setupLogger()

	var err error
	if err = setupDB(ctx, fmt.Sprintf("file:%s", *dbPath)); err != nil {
		panic(err)
	}
	defer clients.db.Close()

	clients.matrix = setupMatrix()
	go func() {
		if err := clients.matrix.Sync(); err != nil {
			panic(err)
		}
	}()

	ticker := time.NewTicker(*tickerOpt)
	optimizeTicker := time.NewTicker(24 * time.Hour)
	slog.Debug("delay set", "value", *tickerOpt)

	// - fetch main page, add list of packages to mem
	// - fetch last log of subscribed packages
	// - enter infinite loop, block on queue
	// wake on new item in queue
	// item can be:
	// - new url to parse
	//   - if it's a non-log link, visit it and then add all log-links to the queue
	//   - if it's a log-link, download log, check for errors, insert into db accordingly
	// - fetch main page
	// - new sub
	// - new broken package, send to subbers

	slog.Info("initialized", "delay", tickerOpt)

	storeAttrPaths(*mainURL)
	updateSubs()

	for {
		select {
		case <-ticker.C:
			slog.Info("new ticker run")
			storeAttrPaths(*mainURL)
			updateSubs()
		case <-optimizeTicker.C:
			slog.Info("optimizing DB")
			if _, err := clients.db.Exec("PRAGMA optimize;"); err != nil {
				panic(err)
			}
		}
	}
}

// scrapes the main page, saving package names to db
func storeAttrPaths(url string) {
	req, err := newReqWithUA(url)
	if err != nil {
		panic(err)
	}

	resp, err := clients.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		panic(err)
	}

	hrefs := htmlquery.Find(doc, "//a/@href")
	slog.Info("storing attr paths", "count", len(hrefs))
	for _, href := range hrefs {
		attr_path := strings.TrimSuffix(htmlquery.InnerText(href), "/")
		if ignoRE.MatchString(attr_path) {
			continue
		}
		if _, err := clients.db.Exec("INSERT OR IGNORE INTO packages(attr_path) VALUES (?)", attr_path); err != nil {
			fatal(err)
		}
	}
}

// Iterates over subscribed-to packages, and fetches their latest log, printing out whether it contained an error.
// It also updates the packages.last_visited column.
func updateSubs() {
	rows, err := clients.db.Query("SELECT attr_path FROM subscriptions")
	if err != nil {
		fatal(err)
	}
	defer rows.Close()

	aps := make([]string, 0)
	for rows.Next() {
		var ap string
		if err := rows.Scan(&ap); err != nil {
			fatal(err)
		}
		aps = append(aps, ap)
	}
	if err := rows.Err(); err != nil {
		fatal(err)
	}

	for _, ap := range aps {
		url := packageURL(ap)
		// TODO: make async
		// TODO: we could sometimes avoid fetching altogether if we passed last_visited
		logDate, hasError := h.logFetcher(url)

		// avoid duplicate notifications by ensuring we haven't already notified for this log
		var lv string
		if err := clients.db.QueryRow("SELECT last_visited FROM packages WHERE attr_path = ?", ap).Scan(&lv); err != nil {
			fatal(err)
		}
		if logDate > lv {
			if hasError {
				slog.Info("new log", "err", true, "url", logURL(ap, logDate))
				notifySubscribers(ap, logDate)
			} else {
				slog.Info("new log", "err", false, "url", logURL(ap, logDate))
			}

			if _, err := clients.db.Exec("UPDATE packages SET last_visited = ? WHERE attr_path = ?", logDate, ap); err != nil {
				fatal(err)
			}
		} else {
			slog.Info("no new log", "url", url, "date", logDate)
		}
	}
}

// fetch package page
// find last log
// fetch last log
func fetchLastLog(url string) (date string, hasError bool) {
	req, err := newReqWithUA(url)
	if err != nil {
		panic(err)
	}

	resp, err := clients.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		panic(err)
	}

	n := htmlquery.FindOne(doc, "//a[contains(@href, '.log')][last()]/@href")
	href := htmlquery.InnerText(n)
	parsedURL, err := u.Parse(url)
	if err != nil {
		panic(err)
	}
	fullURL := parsedURL.JoinPath(href)

	_, date = getComponents(fullURL.String())

	slog.Debug("fetching log", "url", fullURL)

	req, err = newReqWithUA(fullURL.String())
	if err != nil {
		panic(err)
	}

	resp, err = clients.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	hasError = erroRE.Find(body) != nil

	return date, hasError
}

func notifySubscribers(attr_path, date string) {
	// - find all subscribers for package
	// - send message in respective room
	// - if we're not in that room, drop from db of subs?
	logPath := fmt.Sprintf("%s/%s/%s.log", *mainURL, attr_path, date)
	slog.Debug("lp", "lp", logPath)
	rows, err := clients.db.Query("SELECT roomid FROM subscriptions WHERE attr_path = ?", attr_path)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	roomIDs := make([]string, 0)
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			panic(err)
		}
		roomIDs = append(roomIDs, roomID)
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}

	for _, roomID := range roomIDs {
		slog.Info("notifying subscriber", "roomid", roomID)
		s := fmt.Sprintf("New build error for package `%s`: %s", attr_path, logPath)
		if _, err := h.sender(s, id.RoomID(roomID)); err != nil {
			// TODO check if we're not in room, in that case remove sub
			slog.Error(err.Error())
		}
	}
}

// Decides what to do, based on the message content.
func handleMessage(ctx context.Context, evt *event.Event) {
	msg := evt.Content.AsMessage().Body
	sender := evt.Sender.String()

	slog.Debug("received msg", "msg", msg, "sender", sender)

	if sender == fmt.Sprintf("@%s:%s", *matrixUsername, *matrixHomeserver) {
		slog.Debug("ignoring our own message", "msg", msg)

		return
	}

	if dangerousRE.MatchString(msg) {
		slog.Info("received spammy query", "msg", msg, "sender", sender)
		s := `Pattern returns too many results, please use a more specific selector.

Type **help** for a list of allowed/forbidden patterns.`

		if _, err := h.sender(s, id.RoomID(evt.RoomID)); err != nil {
			slog.Error(err.Error())
		}
	} else if subUnsubRE.MatchString(msg) {
		handleSubUnsub(msg, evt)
	} else if ownDisownRE.MatchString(msg) {
		// check matrix_to_nix.json, find github user that matches matrix user
		s := evt.Sender.String()
		handle, ok := handleMap[s]
		if !ok {
			// TODO tell user they can't do this
		} else {
			aps := packagesByMaintainer(handle)

			for _, ap := range aps {
				// TODO (un)subscribe from each ap
				fmt.Printf("got %s\n", ap)
			}
		}
	} else if msg == "subs" {
		handleSubs(evt)
	} else {
		// anything else, so print help
		if _, err := h.sender(helpText, evt.RoomID); err != nil {
			slog.Error(err.Error())
		}
		slog.Debug("received help", "sender", sender)
	}
}
