package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	u "net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"io"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/net/html"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"

	_ "github.com/mattn/go-sqlite3"
)

var matrixEnabled = pflag.Bool("matrix.enabled", true, "Whether to enable Matrix integration")
var homeserver = pflag.String("matrix.homeserver", "matrix.org", "Matrix homeserver for the bot account")
var url = pflag.String("url", "https://nixpkgs-update-logs.nix-community.org", "Webpage with logs")
var filename = pflag.String("db", "data.db", "Path to the DB file")
var config = pflag.String("config", "config.toml", "Config file")
var username = pflag.String("matrix.username", "", "Matrix bot username")
var delay = pflag.Duration("delay", 24*time.Hour, "How often to check url")
var debug = pflag.Bool("debug", false, "Enable debug logging")

var client *mautrix.Client

var db *sql.DB

func main() {
	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	viper.SetConfigFile(*config)
	if err := viper.ReadInConfig(); err != nil {
		// FIXME: broken if file missing
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			fmt.Println("config file not found, using defaults")
		} else {
			panic(err)
		}
	}

	setupLogger()

	if err := setupDB(); err != nil {
		panic(err)
	}
	defer db.Close()

	if viper.GetBool("matrix.enabled") {
		client = setupMatrix()

		go func() {
			if err := client.Sync(); err != nil {
				panic(err)
			}
		}()
	}

	ch := make(chan string)
	ticker := time.NewTicker(viper.GetDuration("delay"))
	optimizeTicker := time.NewTicker(24 * time.Hour)
	slog.Debug("delay set", "value", viper.GetDuration("delay"))

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

	// perf-opt: compile regex
	re := regexp.MustCompile(`\.log$`)

	hCli := &http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout: 30 * time.Second,
			// TODO: does this do anyting?
			MaxConnsPerHost: 5,
		},
	}
	slog.Info("Initialized")

	// visit main page to send links to channel
	go scrapeLinks(viper.GetString("url"), ch, hCli)

	for {
		select {
		case url := <-ch:
			logLink := re.MatchString(url)

			if logLink {
				// TODO make async? probably not as it accesses db
				slog.Debug("found link", "url", url)
				visitLog(url, client, hCli)
			} else {
				slog.Debug("scraping link", "url", url)
				go scrapeLinks(url, ch, hCli)
			}
		case <-ticker.C:
			slog.Debug(">>> ticker")
			go scrapeLinks(viper.GetString("url"), ch, hCli)
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
	parsedURL, err := u.Parse(url)
	if err != nil {
		slog.Error(err.Error())
	}
	resp, err := hCli.Get(parsedURL.String())
	if err != nil {
		slog.Error(err.Error())
	}
	defer resp.Body.Close()

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error(err.Error())
	}
	z := html.NewTokenizer(bytes.NewReader(r))

	re := regexp.MustCompile("^~")
	for {
		tt := z.Next()

		switch tt {
		case html.ErrorToken:
			// done
			slog.Debug("done parsing")
			return
		case html.StartTagToken:
			t := z.Token()

			isAnchor := t.Data == "a"
			if isAnchor {
				for _, a := range t.Attr {
					if a.Key == "href" && a.Val != "../" && !re.MatchString(a.Val) {
						fullURL := parsedURL.JoinPath(a.Val)
						slog.Debug("parsed", "url", fullURL.String())

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
func visitLog(url string, mCli *mautrix.Client, hCli *http.Client) {
	components := strings.Split(url, "/")
	pkgName := components[len(components)-2]
	date := strings.Trim(components[len(components)-1], ".log")
	slog.Debug("log found", "pkg", pkgName, "date", date)

	// pkgName -> pkgID
	var pkgID int64
	if err := db.QueryRow("SELECT id from packages WHERE name = ?", pkgName).Scan(&pkgID); err != nil {
		// TODO: move this to first pass, to simplify this code
		// - as we add logs to queue, add missing packages to db
		// Package did not exist in db, inserting
		if errors.Is(err, sql.ErrNoRows) {
			slog.Info("new package found", "pkg", pkgName)
			result, err := db.Exec("INSERT INTO packages(name) VALUES (?)", pkgName)
			if err != nil {
				panic(err)
			}

			pkgID, err = result.LastInsertId()
			if err != nil {
				panic(err)
			}

		} else {
			panic(err)
		}
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM visited where pkgid = ? AND date = ?", pkgID, date).Scan(&count); err != nil {
		panic(err)
	}

	// we've found this log already, skip next steps
	if count == 1 {
		slog.Debug("skipping", "url", url)
		return
	}

	resp, err := hCli.Get(url)
	if err != nil {
		slog.Error(err.Error())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error(err.Error())
	}

	// check for error in logs
	var hasError bool
	if bytes.Contains(body, []byte("error")) {
		hasError = true

		slog.Info("new log found", "err", true, "url", url)

		if viper.GetBool("matrix.enabled") {
			// TODO: handle 429

			// - find all subscribers for package
			// - send message in respective room
			// - if we're not in that room, drop from db of subs?
			rows, err := db.Query("SELECT roomid from subscriptions where pkgid = ?", pkgID)
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
				_, err := mCli.SendText(context.TODO(), id.RoomID(roomID), fmt.Sprintf("logfile contains an error: %s", url))
				if err != nil {
					// TODO check if we're not in room, in that case remove sub
					slog.Error(err.Error())
				}
			}
		}
	} else {
		slog.Info("new log found", "err", false, "url", url)
	}

	// we haven't seen this log yet, so add it to the list of seen ones
	if _, err := db.Exec("INSERT INTO visited (pkgid, date, error) VALUES (?, ?, ?)", pkgID, date, hasError); err != nil {
		panic(err)
	}

}

func setupMatrix() *mautrix.Client {
	client, err := mautrix.NewClient(viper.GetString("matrix.homeserver"), "", "")
	if err != nil {
		panic(err)
	}

	_, err = client.Login(context.TODO(), &mautrix.ReqLogin{
		Type:               mautrix.AuthTypePassword,
		Identifier:         mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: viper.GetString("matrix.username")},
		Password:           viper.GetString("matrix.password"),
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
			}

			slog.Debug("joining room", "id", evt.RoomID)
		case event.MembershipLeave:
			// remove subscription, then leave room
			if _, err := db.Exec("DELETE FROM subscriptions WHERE roomid = ?", evt.RoomID); err != nil {
				panic(err)
			}

			if _, err := client.LeaveRoom(ctx, evt.RoomID); err != nil {
				slog.Error(err.Error())
			}

			slog.Debug("leaving room", "id", evt.RoomID)
		default:
			slog.Debug("received unhandled event", "type", event.StateMember, "content", evt.Content)
		}
	})

	subRegexp := regexp.MustCompile(`^(un)?sub ([\w.]+)$`)
	helpText := format.RenderMarkdown(`Welcome to the nixpkgs-update-notifier bot!

  These are the available commands:
  - **help**: show this help message
  - **sub foo**: subscribe to package foo
  - **unsub foo**: unsubscribe from package foo
  - **subs**: list subscriptions
  `, true, false)
	subEventID := "io.github.nixpkgs-update-notifier.subscription"

	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		msg := evt.Content.AsMessage().Body
		sender := evt.Sender.String()

		slog.Debug("received msg", "msg", msg, "sender", sender)

		if sender == fmt.Sprintf("@%s:%s", viper.GetString("matrix.username"), viper.GetString("matrix.homeserver")) {
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
			rows, err := db.Query("SELECT name FROM packages AS p JOIN subscriptions AS s ON p.id = s.pkgid WHERE s.roomid = ?", evt.RoomID)
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
				msg = fmt.Sprintf("subs: %s", names)
			}
			if _, err = client.SendText(context.TODO(), evt.RoomID, msg); err != nil {
				slog.Error(err.Error())
			}

		default:
			// anything else, so print help
			if _, err := client.SendMessageEvent(context.TODO(), evt.RoomID, event.EventMessage, helpText); err != nil {
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

	// check if pkgName exists
	var pkgID int
	if err := db.QueryRow("SELECT id FROM packages WHERE name = ?", pkgName).Scan(&pkgID); err != nil {
		slog.Debug("sub/unsub from non-existing package", "pkg", pkgName)

		if _, err := client.SendText(context.TODO(), evt.RoomID, fmt.Sprintf("could not find package %s", pkgName)); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	// check here if sub already exists

	if matches[1] != "" {
		slog.Info("received unsub", "pkg", pkgName, "sender", evt.Sender)
		res, err := db.Exec("DELETE FROM subscriptions WHERE pkgid = ?", pkgID)
		if err != nil {
			panic(err)
		}

		var msg string
		if val, err := res.RowsAffected(); err != nil {
			panic(err)
		} else if val == 0 {
			msg = fmt.Sprintf("could not find subscription for package %s", pkgName)
		} else {
			msg = fmt.Sprintf("successfully unsubscribed from package %s", pkgName)
		}

		// send confirmation message
		if _, err := client.SendText(context.TODO(), evt.RoomID, msg); err != nil {
			slog.Error(err.Error())
		}
		return
	}

	slog.Info("received sub", "pkg", pkgName, "sender", evt.Sender)

	// TODO use JOIN?
	if err := db.QueryRow("SELECT id FROM packages WHERE name = ?", pkgName).Scan(&pkgID); err != nil {
		panic(err)
	}

	// check if sub already exists
	var c int
	if err := db.QueryRow("SELECT COUNT(*) FROM subscriptions WHERE roomid = ? AND pkgid = ?", rID, pkgID).Scan(&c); err != nil {
		panic(err)
	}

	if c != 0 {
		if _, err := client.SendText(context.TODO(), rID, "already subscribed"); err != nil {
			slog.Error(err.Error())
		}
		return
	}

	if _, err := db.Exec("INSERT INTO subscriptions(roomid,pkgid) VALUES (?, ?)", evt.RoomID, pkgID); err != nil {
		panic(err)
	}

	// send confirmation message
	if _, err := client.SendText(context.TODO(), evt.RoomID, fmt.Sprintf("successfully subscribed to package %s", pkgName)); err != nil {
		slog.Error(err.Error())
	}

}

func setupLogger() {
	opts := &slog.HandlerOptions{}
	h := slog.NewTextHandler(os.Stderr, opts)
	slog.SetDefault(slog.New(h))

	if viper.GetBool("debug") {
		opts.Level = slog.LevelDebug
	}
}

func setupDB() (err error) {
	db, err = sql.Open("sqlite3", fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=true", viper.GetString("db")))
	if err != nil {
		return
	}

	err = db.Ping()
	if err != nil {
		return
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS packages (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL) STRICT")
	if err != nil {
		return
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS visited (id INTEGER PRIMARY KEY, pkgid INTEGER, date TEXT NOT NULL, error INTEGER, UNIQUE(pkgid, date), FOREIGN KEY(pkgid) REFERENCES packages(id)) STRICT")
	if err != nil {
		return
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS subscriptions (id INTEGER PRIMARY KEY, roomid TEXT, pkgid INTEGER, UNIQUE(roomid, pkgid), FOREIGN KEY(pkgid) REFERENCES packages(id)) STRICT")
	if err != nil {
		return
	}

	return
}
