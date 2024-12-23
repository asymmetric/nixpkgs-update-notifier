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
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/html"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/asymmetric/nixpkgs-update-notifier/db"
)

// Note: the ddl contains a trigger, which ensures the following invariant: a
// subscription can't be added before the corresponding package has had its
// last_visited column set Otherwise we have cases where Scan fails (because it
// can't cast NULL to a string), and also we can end up sending notifications
// for old errors.
//
//go:embed db/schema.sql
var ddl string

var matrixHomeserver = flag.String("matrix.homeserver", "matrix.org", "Matrix homeserver for the bot account")
var matrixUsername = flag.String("matrix.username", "", "Matrix bot username")

var mainURL = flag.String("url", "https://nixpkgs-update-logs.nix-community.org", "Webpage with logs")
var dbPath = flag.String("db", "data.db", "Path to the DB file")
var tickerOpt = flag.Duration("ticker", 24*time.Hour, "How often to check url")
var debug = flag.Bool("debug", false, "Enable debug logging")

var client *mautrix.Client

// This is set to the date the visited table is created, so we ignore logs
// older than this date -- the user is not interested in all past failures,
// just the ones since they subscribed, which by definition is after the first
// run of this program.
var tombstone string

// - "error: " is a nix build error
// - "ExitFailure" is a nixpkgs-update error
// - "failed with" is a nixpkgs/maintainers/scripts/update.py error
var erroRE = regexp.MustCompile(`^error:|ExitFailure|failed with`)

var ignoRE = regexp.MustCompile(`^~.*|^\.\.`)

var subUnsubRE = regexp.MustCompile(`^(un)?sub ([a-zA-Z\d][\w._-]*)$`)

var hc = &http.Client{}

var queries *db.Queries

var ctx context.Context

func main() {
	ctx = context.Background()

	flag.Parse()

	setupLogger()

	var err error
	queries, err = setupDB(ctx)
	if err != nil {
		panic(err)
	}

	client = setupMatrix()
	go func() {
		if err := client.Sync(); err != nil {
			panic(err)
		}
	}()

	ticker := time.NewTicker(*tickerOpt)
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
		if err := queries.CreatePackage(ctx, attr_path); err != nil {
			fatal(err)
		}
	}
}

// iterates over subscrubed packages, and fetches their latest logs
func scrapeSubs() {
	aps, err := queries.GetAttrPaths(ctx)
	if err != nil {
		fatal(err)
	}

	for _, ap := range aps {
		url := packageURL(ap)
		// TODO: make async
		// TODO: we could sometimes avoid fetching altogether if we passed last_visited
		date, hasError := fetchLastLog(url)

		// avoid duplicate notifications by ensuring we haven't already notified for this log
		last, err := queries.GetLastVisitedByAttrPath(ctx, ap)
		if err != nil {
			fatal(err)
		}
		if last.Valid && date > last.String {
			if hasError {
				slog.Info("new log", "err", true, "url", logURL(ap, date))
				notifySubscribers(ap, date)
			} else {
				slog.Info("new log", "err", false, "url", logURL(ap, date))
			}

			params := db.UpdatePackageLastVisitedParams{
				LastVisited: sql.NullString{String: date},
				Error:       hasError,
				AttrPath:    ap,
			}
			if err := queries.UpdatePackageLastVisited(ctx, params); err != nil {
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

func setupMatrix() *mautrix.Client {
	client, err := mautrix.NewClient(*matrixHomeserver, "", "")
	if err != nil {
		panic(err)
	}

	envVar := "NIXPKGS_UPDATE_NOTIFIER_PASSWORD"
	pwd, found := os.LookupEnv(envVar)
	if !found {
		fatal(fmt.Errorf("could not read password env var %s", envVar))
	}

	_, err = client.Login(context.TODO(), &mautrix.ReqLogin{
		Type:               mautrix.AuthTypePassword,
		Identifier:         mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: *matrixUsername},
		Password:           pwd,
		StoreCredentials:   true,
		StoreHomeserverURL: true,
	})
	if err != nil {
		panic(err)
	}

	syncer := mautrix.NewDefaultSyncer()
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		m := evt.Content.AsMember()
		switch m.Membership {
		case event.MembershipInvite:
			// TODO: only join if IsDirect is true, i.e. it's a DM
			if _, err := client.JoinRoomByID(ctx, evt.RoomID); err != nil {
				slog.Error(err.Error())

				return
			}

			slog.Debug("joining room", "id", evt.RoomID)
		case event.MembershipLeave:
			// remove subscription, then leave room
			if err := queries.DeleteSubscriptionsByRoomID(ctx, evt.RoomID.String()); err != nil {
				panic(err)
			}

			if _, err := client.LeaveRoom(ctx, evt.RoomID); err != nil {
				slog.Error(err.Error())

				return
			}

			slog.Debug("leaving room", "id", evt.RoomID)
		default:
			slog.Debug("received unhandled event", "type", event.StateMember, "content", evt.Content)
		}
	})

	helpText := `Welcome to the nixpkgs-update-notifier bot!

  These are the available commands:
  - **help**: show this help message
  - **sub foo**: subscribe to package <code>foo</code>
  - **unsub foo**: unsubscribe from package <code>foo</code>
  - **subs**: list subscriptions

  The code for the bot is [here](https://github.com/asymmetric/nixpkgs-update-notifier).
  `
	subEventID := "io.github.nixpkgs-update-notifier.subscription"

	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		msg := evt.Content.AsMessage().Body
		sender := evt.Sender.String()

		slog.Debug("received msg", "msg", msg, "sender", sender)

		if sender == fmt.Sprintf("@%s:%s", *matrixUsername, *matrixHomeserver) {
			slog.Debug("ignoring our own message", "msg", msg)
			return
		}

		if matches := subUnsubRE.FindStringSubmatch(msg); matches != nil {
			handleSubUnsub(matches, evt)

			return
		}

		switch msg {
		case "subs":
			aps, err := queries.GetAttrPathsByRoomID(ctx, string(evt.RoomID))
			if err != nil {
				panic(nil)
			}

			var msg string
			if len(aps) == 0 {
				msg = "no subs"
			} else {
				sts := []string{"Your subscriptions:"}

				for _, n := range aps {
					sts = append(sts, fmt.Sprintf("- %s", n))
				}

				msg = strings.Join(sts, "\n")
			}
			if _, err = client.SendText(context.TODO(), evt.RoomID, msg); err != nil {
				slog.Error(err.Error())
			}

		default:
			// anything else, so print help
			if _, err := sendMarkdown(helpText, evt.RoomID); err != nil {
				slog.Error(err.Error())
			}
			slog.Debug("received help", "sender", sender)
		}
	})

	// NOTE: changing this will re-play all received Matrix messages
	syncer.FilterJSON = &mautrix.Filter{
		AccountData: mautrix.FilterPart{
			Limit: 20,
			NotTypes: []event.Type{
				event.NewEventType(subEventID),
			},
		},
	}
	client.Syncer = syncer
	client.Store = mautrix.NewAccountDataStore(subEventID, client)

	return client
}

func handleSubUnsub(matches []string, evt *event.Event) {
	pkgName := matches[2]
	rID := evt.RoomID

	// check if package exists
	c, err := queries.CountPackagesByAttrPath(context.Background(), pkgName)
	if err != nil {
		panic(err)
	}

	if c == 0 {
		if _, err := sendMarkdown(fmt.Sprintf("could not find package `%s`. The list is [here](https://nixpkgs-update-logs.nix-community.org/)", pkgName), evt.RoomID); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	// matches[1] is the optional "un" prefix
	if matches[1] != "" {
		slog.Info("received unsub", "pkg", pkgName, "sender", evt.Sender)

		params := db.DeleteSubscriptionParams{
			Roomid:   rID.String(),
			AttrPath: pkgName,
		}
		res, err := queries.DeleteSubscription(ctx, params)
		if err != nil {
			panic(err)
		}

		var msg string
		if val, err := res.RowsAffected(); err != nil {
			panic(err)
		} else if val == 0 {
			msg = fmt.Sprintf("could not find subscription for package `%s`", pkgName)
		} else {
			msg = fmt.Sprintf("unsubscribed from package `%s`", pkgName)
		}

		// send confirmation message
		if _, err := sendMarkdown(msg, evt.RoomID); err != nil {
			slog.Error(err.Error())
		}
		return
	}

	slog.Info("received sub", "pkg", pkgName, "sender", evt.Sender)

	params := db.CountSubscriptionsByRoomIDAndAttrPathParams{
		Roomid:   rID.String(),
		AttrPath: pkgName,
	}

	c, err = queries.CountSubscriptionsByRoomIDAndAttrPath(ctx, params)
	if err != nil {
		panic(err)
	}

	if c != 0 {
		if _, err := client.SendText(context.TODO(), rID, "already subscribed"); err != nil {
			slog.Error(err.Error())
		}
		return
	}

	// before adding subscription, add the date of the last available log for the package, so that if the program stops for any reason, we don't notify the user of a stale error log.
	// e.g.:
	// - we add a subscription for package foo at time t
	// - package foo has a failure that predates the subscription, t - 1
	// - program stops before the next tick
	// - therefore, foo.last_visited is nil
	// - when program starts again, we'll iterate over subs and find foo
	// - we will fetch the latest log and because it's a failure, notify
	// - but the log predates the subscription, so we notified on a stale log
	purl := packageURL(pkgName)
	date, hasError := fetchLastLog(purl)
	// TODO: resolve naming conflict
	pars := db.UpdatePackageLastVisitedParams{
		LastVisited: sql.NullString{String: date},
		Error:       hasError,
		AttrPath:    pkgName,
	}
	if err := queries.UpdatePackageLastVisited(ctx, pars); err != nil {
		panic(err)
	}

	foo := db.CreateSubscriptionParams{
		// TODO: use rID ?
		Roomid:   evt.RoomID.String(),
		AttrPath: pkgName,
		Mxid:     evt.Sender.String(),
	}
	if err := queries.CreateSubscription(ctx, foo); err != nil {
		panic(err)
	}

	// send confirmation message
	if _, err := sendMarkdown(fmt.Sprintf("subscribed to package `%s`", pkgName), evt.RoomID); err != nil {
		slog.Error(err.Error())
	}

}

func notifySubscribers(attr_path, date string) {
	// - find all subscribers for package
	// - send message in respective room
	// - if we're not in that room, drop from db of subs?
	logPath := fmt.Sprintf("%s/%s/%s.log", *mainURL, attr_path, date)
	slog.Debug("lp", "lp", logPath)
	roomIDs, err := queries.GetRoomIDsByAttrPath(ctx, attr_path)
	if err != nil {
		panic(err)
	}

	for _, roomID := range roomIDs {
		slog.Info("notifying subscriber", "roomid", roomID)
		s := fmt.Sprintf("potential new build error for package `%s`: %s", attr_path, logPath)
		if _, err := sendMarkdown(s, id.RoomID(roomID)); err != nil {
			// TODO check if we're not in room, in that case remove sub
			slog.Error(err.Error())
		}
	}
}

func setupLogger() {
	opts := &slog.HandlerOptions{}

	if *debug {
		opts.Level = slog.LevelDebug
	}

	h := slog.NewTextHandler(os.Stderr, opts)
	slog.SetDefault(slog.New(h))
}

func setupDB(ctx context.Context) (*db.Queries, error) {
	dbh, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=true", *dbPath))
	if err != nil {
		return nil, err
	}

	if err = dbh.PingContext(ctx); err != nil {
		return nil, err
	}

	if _, err = dbh.ExecContext(ctx, ddl); err != nil {
		return nil, err
	}

	queries := db.New(dbh)

	return queries, nil
}
