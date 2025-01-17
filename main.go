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
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	"github.com/asymmetric/nixpkgs-update-notifier/regexes"
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
var updateTickerOpt = flag.Duration("timers.update", 24*time.Hour, "How often to check for new errors")
var jsonTickerOpt = flag.Duration("timers.jsblob", 5*time.Minute, "How often to fetch packages.json.br")
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

// TODO: make configurable
var (
	repoOwner    = "asymmetric"
	repoName     = "nixpkgs-update-notifier"
	githubToken  = "your-github-token"
	artifactsURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/artifacts", repoOwner, repoName)
)

const packagesURL = "https://channels.nixos.org/nixos-unstable/packages.json.br"

// These are abstracted so that we can pass a different function in tests.
type handlers struct {
	// Fetches logs for a URL.
	logFetcher func(string) (string, bool)
	// Fetches last log date for a URL.
	dateFetcher func(string) string
	// Sends messages to  a user via Matrix.
	sender func(string, id.RoomID) (*mautrix.RespSendEvent, error)
}

var h handlers

var jsblob any

const helpText = `Welcome to the nixpkgs-update-notifier bot!

These are the available commands:

- **sub foo**: subscribe to package <code>foo</code>
- **unsub foo**: unsubscribe from package <code>foo</code>
- **follow foo**: subscribe to all packages maintained by GitHub handle <code>foo</code>
- **unfollow foo**: unsubscribe to all packages maintained by GitHub handle <code>foo</code>
- **subs**: list subscriptions
- **help**: show this help message

You can use the <code>*</code> and <code>?</code> globs in queries. Things you can do:

- <code>sub python31?Packages.acme</code>
- <code>sub *.acme</code>

Things you cannot do:

- <code>sub *</code>
- <code>sub ?</code>
- <code>sub foo.*</code>
- <code>follow *</code>

The code for the bot is [here](https://github.com/asymmetric/nixpkgs-update-notifier).
`

func init() {
	// default handlers
	h = handlers{
		logFetcher:  fetchLatestLogState,
		dateFetcher: fetchLatestLogDate,
		sender:      sendMarkdown,
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

	updateTicker := time.NewTicker(*updateTickerOpt)
	optimizeTicker := time.NewTicker(24 * time.Hour)
	jsonTicker := time.NewTicker(*jsonTickerOpt)
	slog.Debug("delay set", "value", *updateTickerOpt)

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

	slog.Info("initialized", "delay", updateTickerOpt)


	storeAttrPaths(*mainURL)
	updateSubs()
	fetchPackagesJSON()

	for {
		select {
		case <-updateTicker.C:
			slog.Info("new ticker run")
			storeAttrPaths(*mainURL)
			updateSubs()
		case <-jsonTicker.C:
			// NOTE: in theory, this could be done in a goroutine, but in practice,
			// the program is idling so often that it's not really necessary.
			fetchPackagesJSON()
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
		if regexes.Ignore().MatchString(attr_path) {
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
	rows, err := clients.db.Query("SELECT attr_path FROM subscriptions ORDER BY attr_path")
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

// Given a package URL, it returns the latest log's date, and whether it contained an error.
//
// It works by:
// - fetching package page
// - finding latest log
// - fetching latest log
//
// Therefore, it makes 2 HTTP requests.
func fetchLatestLogState(url string) (string, bool) {
	// First HTTP request, returns URL of latest log
	purl := fetchLatestLogURL(url)

	slog.Debug("fetching log", "url", purl)

	req, err := newReqWithUA(purl)
	if err != nil {
		panic(err)
	}

	// Second HTTP request, returns logs itself
	slog.Debug("http req", "state", "start", "req", req.URL)
	resp, err := clients.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	slog.Debug("http req", "state", "done", "req", req.URL)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	return getDate(purl), regexes.Error().Match(body)
}

// Given the URL of a package, it returns the URL of the latest log.
//
// It does this by parsing the fetched HTML and getting the latest link.
//
// Therefore, it makes 1 HTTP request.
//
// TODO: return string
func fetchLatestLogURL(url string) string {
	req, err := newReqWithUA(url)
	if err != nil {
		panic(err)
	}

	slog.Debug("http req", "state", "start", "req", req)
	resp, err := clients.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	slog.Debug("http req", "state", "done", "req", req)

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
	return fmt.Sprintf("%s/%s", parsedURL, href)
}

// Given a URL of a package, it returns the date of the latest log.
func fetchLatestLogDate(url string) string {
	lurl := fetchLatestLogURL(url)

	return getDate(lurl)
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

	if regexes.Dangerous().MatchString(msg) {
		slog.Info("received spammy query", "msg", msg, "sender", sender)
		s := `Pattern returns too many results, please use a more specific selector.

Type **help** for a list of allowed/forbidden patterns.`

		if _, err := h.sender(s, id.RoomID(evt.RoomID)); err != nil {
			slog.Error(err.Error())
		}
	} else if regexes.Subscribe().MatchString(msg) {
		handleSubUnsub(msg, evt)
	} else if regexes.Follow().MatchString(msg) {
		handleFollowUnfollow(msg, evt)
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
