package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	u "net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/html"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

var matrixHomeserver = flag.String("matrix.homeserver", "matrix.org", "Matrix homeserver for the bot account")
var matrixUsername = flag.String("matrix.username", "", "Matrix bot username")

var url = flag.String("url", "https://nixpkgs-update-logs.nix-community.org", "Webpage with logs")
var dbPath = flag.String("db", "data.db", "Path to the DB file")
var tickerOpt = flag.Duration("ticker", 24*time.Hour, "How often to check url")
var debug = flag.Bool("debug", false, "Enable debug logging")

var client *mautrix.Client

var db *sql.DB

// This is set to the date the visited table is created, so we ignore logs
// older than this date -- the user is not interested in all past failures,
// just the ones since they subscribed, which by definition is after the first
// run of this program.
var tombstone string

var packages sync.Map

// - "error: " is a nix build error
// - "ExitFailure" is a nixpkgs-update error
// - "failed with" is a nixpkgs/maintainers/scripts/update.py error
var errRE = regexp.MustCompile(`error:|ExitFailure|failed with`)
var tildeRE = regexp.MustCompile("^~")

func main() {
	flag.Parse()

	setupLogger()

	if err := setupDB(); err != nil {
		panic(err)
	}
	defer db.Close()

	client = setupMatrix()
	go func() {
		if err := client.Sync(); err != nil {
			panic(err)
		}
	}()

	ch := make(chan string)
	ticker := time.NewTicker(*tickerOpt)
	optimizeTicker := time.NewTicker(24 * time.Hour)
	slog.Debug("delay set", "value", *tickerOpt)

	// fetch main page
	// - add each link to the queue
	// enter infinite loop, block on queue
	// wake on new item in queue
	// item can be:
	// - new url to parse
	//   - if it's a non-log link, visit it and then add all log-links to the queue
	//   - if it's a log-link, download log, check for errors, insert into db accordingly
	// - fetch main page
	// - new sub
	// - new broken package, send to subbers

	hCli := &http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout: 30 * time.Second,
			// TODO: does this do anyting?
			MaxConnsPerHost: 5,
		},
	}
	slog.Info("initialized", "delay", tickerOpt)

	// visit main page to send links to channel
	go scrapeLinks(*url, ch, hCli)

	re := regexp.MustCompile(`\.log$`)

	for {
		select {
		case url := <-ch:
			isLog := re.MatchString(url)

			if isLog {
				// TODO make async? probably not as it accesses db
				slog.Info("found log", "url", url)
				visitLog(url, hCli)
			} else {
				slog.Info("scraping link", "url", url)
				go scrapeLinks(url, ch, hCli)
			}
		case <-ticker.C:
			slog.Info("new ticker run")
			go scrapeLinks(*url, ch, hCli)
		case <-optimizeTicker.C:
			slog.Info("optimizing DB")
			if _, err := db.Exec("PRAGMA optimize;"); err != nil {
				panic(err)
			}
		}
	}
}

// fetches the HTML at a `url`, then iterates over <a> elements adding all links to channel `ch`
func scrapeLinks(url string, ch chan<- string, hCli *http.Client) {
	// TODO convert to simple string
	parsedURL, err := u.Parse(url)
	if err != nil {
		slog.Error(err.Error())

		return
	}

	req, err := newReqWithUA(url)
	if err != nil {
		panic(err)
	}

	resp, err := hCli.Do(req)
	if err != nil {
		slog.Error(err.Error())

		return
	}
	defer resp.Body.Close()

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error(err.Error())

		return
	}
	z := html.NewTokenizer(bytes.NewReader(r))

	// we want to avoid links starting with ~, as those are internal state of the
	// nixpkgs-update supervisor
	// TODO change to this
	// re := regexp.MustCompile("(^~.*|^../)")
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return
		case html.StartTagToken:
			t := z.Token()

			isAnchor := t.Data == "a"
			if isAnchor {
				for _, a := range t.Attr {
					if a.Key == "href" && a.Val != "../" && !tildeRE.MatchString(a.Val) {
						fullURL := parsedURL.JoinPath(a.Val)
						packages.Store(strings.Trim(parsedURL.Path, "/"), struct{}{})

						// add link to queue
						ch <- fullURL.String()
						break
					}
				}
			}
		}
	}
}

// TODO take URL instead, so we can split more reliably?
// e.g. pkgName could be the first half of a split, the date the second
func visitLog(url string, hc *http.Client) {
	components := strings.Split(url, "/")
	pkgName := components[len(components)-2]
	date := strings.Trim(components[len(components)-1], ".log")

	slog.Debug("log found", "pkg", pkgName, "date", date)
	if date < tombstone {
		slog.Debug("skipping", "reason", "old", "url", url)

		return
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM visited where attr_path = ? AND date = ?", pkgName, date).Scan(&count); err != nil {
		panic(err)
	}

	// we've found this log already, skip next steps
	if count == 1 {
		slog.Debug("skipping", "url", url)

		return
	}

	req, err := newReqWithUA(url)
	if err != nil {
		panic(err)
	}

	resp, err := hc.Do(req)
	if err != nil {
		slog.Error(err.Error())

		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error(err.Error())

		return
	}

	// check for error in logs
	var hasError bool

	if errRE.Find(body) != nil {
		hasError = true

		slog.Info("new log found", "err", true, "url", url)

		// TODO: handle 429

		// - find all subscribers for package
		// - send message in respective room
		// - if we're not in that room, drop from db of subs?
		rows, err := db.Query("SELECT roomid FROM subscriptions WHERE attr_path = ?", pkgName)
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
			s := fmt.Sprintf("potential new build error for package `%s`: %s", pkgName, url)
			if _, err := sendMarkdown(s, id.RoomID(roomID)); err != nil {
				// TODO check if we're not in room, in that case remove sub
				slog.Error(err.Error())
			}
		}
	} else {
		slog.Info("new log found", "err", false, "url", url)
	}

	// we haven't seen this log yet, so add it to the list of seen ones
	if _, err := db.Exec("INSERT INTO visited (attr_path, date, error) VALUES (?, ?, ?)", pkgName, date, hasError); err != nil {
		panic(err)
	}

}

func setupMatrix() *mautrix.Client {
	client, err := mautrix.NewClient(*matrixHomeserver, "", "")
	if err != nil {
		panic(err)
	}

	envVar := "NIXPKGS_UPDATE_NOTIFIER_PASSWORD"
	pwd, found := os.LookupEnv(envVar)
	if !found {
		panic(fmt.Errorf("could not read password env var %s", envVar))
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
			if _, err := db.Exec("DELETE FROM subscriptions WHERE roomid = ?", evt.RoomID); err != nil {
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

	subRegexp := regexp.MustCompile(`^(un)?sub ([\w._-]+)$`)
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

		// TODO
		// - last success/first fail

		if matches := subRegexp.FindStringSubmatch(msg); matches != nil {
			handleSubUnsub(matches, evt)

			return
		}

		switch msg {
		case "subs":
			rows, err := db.Query("SELECT attr_path FROM subscriptions WHERE roomid = ?", evt.RoomID)
			if err != nil {
				panic(err)
			}
			defer rows.Close()

			names := make([]string, 0)
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					panic(err)
				}
				names = append(names, name)
			}
			if err := rows.Err(); err != nil {
				panic(err)
			}

			var msg string
			if len(names) == 0 {
				msg = "no subs"
			} else {
				sts := []string{"Your subscriptions:"}

				for _, n := range names {
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

	// TODO check if sub already exists
	if _, ok := packages.Load(pkgName); !ok {
		if _, err := sendMarkdown(fmt.Sprintf("could not find package `%s`", pkgName), evt.RoomID); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	// matches[1] is the optional "un" prefix
	if matches[1] != "" {
		slog.Info("received unsub", "pkg", pkgName, "sender", evt.Sender)
		res, err := db.Exec("DELETE FROM subscriptions WHERE attr_path = ?", pkgName)
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

	var c int
	if err := db.QueryRow("SELECT COUNT(*) FROM subscriptions WHERE roomid = ? AND attr_path = ?", rID, pkgName).Scan(&c); err != nil {
		panic(err)
	}

	if c != 0 {
		if _, err := client.SendText(context.TODO(), rID, "already subscribed"); err != nil {
			slog.Error(err.Error())
		}
		return
	}

	if _, err := db.Exec("INSERT INTO subscriptions(roomid,attr_path,mxid) VALUES (?, ?, ?)", evt.RoomID, pkgName, evt.Sender); err != nil {
		panic(err)
	}

	// send confirmation message
	if _, err := sendMarkdown(fmt.Sprintf("subscribed to package `%s`", pkgName), evt.RoomID); err != nil {
		slog.Error(err.Error())
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

func setupDB() (err error) {
	db, err = sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=true", *dbPath))
	if err != nil {
		slog.Error(err.Error())

		return
	}

	err = db.Ping()
	if err != nil {
		slog.Error(err.Error())

		return
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS visited (attr_path TEXT, date TEXT, error INTEGER, PRIMARY KEY(attr_path, date)) STRICT")
	if err != nil {
		slog.Error(err.Error())

		return
	}

	if err := db.QueryRow("SELECT date FROM visited ORDER BY date ASC LIMIT 1").Scan(&tombstone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			tombstone = time.Now().Format(time.DateOnly)
		} else {
			panic(err)
		}
	}
	slog.Info("tombstone", "tombstone", tombstone)

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS subscriptions (id INTEGER PRIMARY KEY, roomid TEXT, mxid TEXT NOT NULL, attr_path TEXT NOT NULL, UNIQUE(roomid, attr_path)) STRICT")
	if err != nil {
		slog.Error(err.Error())

		return
	}

	return
}

func newReqWithUA(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "https://github.com/asymmetric/nixpkgs-update-notifier")

	return req, nil
}

func sendMarkdown(s string, rid id.RoomID) (*mautrix.RespSendEvent, error) {
	m := format.RenderMarkdown(s, true, true)
	return client.SendMessageEvent(context.TODO(), rid, event.EventMessage, m)
}
