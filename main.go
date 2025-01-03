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

var client *mautrix.Client

var db *sql.DB

// Ensure invariant: a subscription can't be added before the corresponding package has had its last_visited column set.
// Otherwise we have cases where Scan fails (because it can't cast NULL to a string), and also we can end up sending notifications for old errors.
//
//go:embed db/schema.sql
var ddl string

// - "error: " is a nix build error
// - "ExitFailure" is a nixpkgs-update error
// - "failed with" is a nixpkgs/maintainers/scripts/update.py error
var erroRE = regexp.MustCompile(`^error:|ExitFailure|failed with`)

var ignoRE = regexp.MustCompile(`^~.*|^\.\.`)

// We want to avoid stuff like the following, because it leads us to spam the nix-community.org server.
// - sub *
// - sub pythonPackages.*
//
// Unsubbing with the same queries is OK, because it it has different semantics and doesn't spam upstream.
var dangerousRE = regexp.MustCompile(`^sub (?:[*?]+|\w+\.\*)$`)

var subUnsubRE = regexp.MustCompile(`^(un)?sub ([\w_?*.-]+)$`)

var hc = &http.Client{}

// These are abstracted so that we can pass a different function in tests.
type handlers struct {
	logFetcher func(string) (string, bool)
	sender     func(string, id.RoomID) (*mautrix.RespSendEvent, error)
}

var h handlers

var helpText = `Welcome to the nixpkgs-update-notifier bot!

These are the available commands:

- **sub foo**: subscribe to package <code>foo</code>
- **unsub foo**: unsubscribe from package <code>foo</code>
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

func main() {
	ctx := context.Background()

	flag.Parse()

	setupLogger()

	h = handlers{
		logFetcher: fetchLastLog,
		// FIXME: this only works because I'm only hitting sendMarkdown in tests.
		// When in the future I'll test code that hits client.SendText (or client.Login), I'll need another solution,
		// perhaps a full-blown mock of the client struct.
		sender: sendMarkdown,
	}

	var err error
	if err = setupDB(ctx, fmt.Sprintf("file:%s", *dbPath)); err != nil {
		panic(err)
	}
	defer db.Close()

	client = setupMatrix()
	go func() {
		if err := client.Sync(); err != nil {
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
	scrapeSubs()

	for {
		select {
		case <-ticker.C:
			slog.Info("new ticker run")
			storeAttrPaths(*mainURL)
			scrapeSubs()
		case <-optimizeTicker.C:
			slog.Info("optimizing DB")
			if _, err := db.Exec("PRAGMA optimize;"); err != nil {
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

	resp, err := hc.Do(req)
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
		if _, err := db.Exec("INSERT OR IGNORE INTO packages(attr_path) VALUES (?)", attr_path); err != nil {
			fatal(err)
		}
	}
}

// iterates over subscrubed packages, and fetches their latest logs
func scrapeSubs() {
	rows, err := db.Query("SELECT attr_path FROM subscriptions")
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
		date, hasError := h.logFetcher(url)

		// avoid duplicate notifications by ensuring we haven't already notified for this log
		var last string
		if err := db.QueryRow("SELECT last_visited FROM packages WHERE attr_path = ?", ap).Scan(&last); err != nil {
			fatal(err)
		}
		if date > last {
			if hasError {
				slog.Info("new log", "err", true, "url", logURL(ap, date))
				notifySubscribers(ap, date)
			} else {
				slog.Info("new log", "err", false, "url", logURL(ap, date))
			}

			if _, err := db.Exec("UPDATE packages SET last_visited = ?, error = ? WHERE attr_path = ?", date, hasError, ap); err != nil {
				fatal(err)
			}
		} else {
			slog.Info("no new log", "url", url, "date", date)
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

	resp, err := hc.Do(req)
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

	resp, err = hc.Do(req)
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
	rows, err := db.Query("SELECT roomid FROM subscriptions WHERE attr_path = ?", attr_path)
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
		s := fmt.Sprintf("potential new build error for package `%s`: %s", attr_path, logPath)
		if _, err := h.sender(s, id.RoomID(roomID)); err != nil {
			// TODO check if we're not in room, in that case remove sub
			slog.Error(err.Error())
		}
	}
}

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
