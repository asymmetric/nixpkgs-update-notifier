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
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/html"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

var homeserver = flag.String("homeserver", "matrix.org", "Matrix homeserver for the bot account")
var url = flag.String("url", "https://nixpkgs-update-logs.nix-community.org/", "Remote state db")
var filename = flag.String("db", "data.db", "Path to the DB file")
var username = flag.String("username", "", "Matrix bot username")
var delay = flag.Duration("delay", 24*time.Hour, "How often to check url")
var debug = flag.Bool("debug", false, "Enable debug logging")

var client *mautrix.Client

var db *sql.DB

var pkgNames = make(map[string]struct{})

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

	ticker := time.NewTicker(*delay)
	optimizeTicker := time.NewTicker(24 * time.Hour)
	slog.Debug("delay set", "value", *delay)

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

	slog.Info("Initialized")

	if last, err := getLastRun(); err != nil {
		panic(err)
	} else {
		slog.Info("last run", "ts", time.Unix(last, 0))
	}

	if err := importPackages(); err != nil {
		panic(err)
	}

	if err := doWork(); err != nil {
		panic(err)
	}

	for {
		select {
		case <-ticker.C:
			if err := doWork(); err != nil {
				panic(err)
			}
		case <-optimizeTicker.C:
			slog.Info("optimizing DB")
			if _, err := db.Exec("PRAGMA optimize;"); err != nil {
				panic(err)
			}
		}
	}
}

// TODO we should deal with, and delete, the db, all in one func, so we can use defer:
// merge fetchStateDB and findNewErrors
func fetchStateDB(url string) (*sql.DB, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	file, err := os.CreateTemp("", "state*.db")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		panic(err)
	}

	// TODO do we need _loc ?
	// TODO move to /tmp
	state, err := sql.Open("sqlite3", "temp.db?mode=ro")
	if err != nil {
		return nil, err
	}

	return state, nil
}

// parse main page, get package names
// save into []string
// range over []string, inserting into packages when missing
func importPackages() error {
	resp, err := http.Get(*url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	z := html.NewTokenizer(bytes.NewReader(r))

	re := regexp.MustCompile("^~")
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			slog.Debug("done parsing")
			return nil
		case html.StartTagToken:
			t := z.Token()

			isAnchor := t.Data == "a"
			if isAnchor {
				for _, a := range t.Attr {
					if a.Key == "href" && a.Val != "../" && !re.MatchString(a.Val) {
						pkgName := strings.TrimSuffix(a.Val, "/")

						slog.Debug("adding pkg", "pkg", pkgName)

						pkgNames[pkgName] = struct{}{}

						break
					}
				}
			}
		}
	}
}

func findNewErrors(state *sql.DB) ([]string, error) {
	// get errors that happened since last run
	last, err := getLastRun()
	if err != nil {
		return nil, err
	}

	rows, err := state.Query("SELECT attr_path from log WHERE exit_code = 1 AND finished >= ?", last)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}

		paths = append(paths, path)
	}

	if err := rows.Err(); err != nil {
		panic(err)
	}

	return paths, nil
}

func setupMatrix() *mautrix.Client {
	client, err := mautrix.NewClient(*homeserver, "", "")
	if err != nil {
		panic(err)
	}

	_, err = client.Login(context.TODO(), &mautrix.ReqLogin{
		Type:               mautrix.AuthTypePassword,
		Identifier:         mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: *username},
		Password:           os.Getenv("NUN_BOT_PASSWORD"),
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
	helpText := `- **help**: show help
- **sub foo.bar**: subscribe to package foo.bar
- **unsub foo.bar**: unsubscribe from package foo.bar
- **subs**: list subscriptions
`
	subEventID := "io.github.nixpkgs-update-notifier.subscription"

	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		msg := evt.Content.AsMessage().Body
		sender := evt.Sender.String()

		slog.Debug("received msg", "msg", msg, "sender", sender)

		if sender == fmt.Sprintf("@%s:%s", *username, *homeserver) {
			slog.Debug("ignoring our own message", "msg", msg)
			return
		}

		// TODO
		// - last success/first fail

		// TODO use a regexp to always extract first command, then we can unify the switch
		// handle this separately because it's not amenable to the switch statement below
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
				msg = fmt.Sprintf("subs: %s", names)
			}
			if _, err = client.SendText(context.TODO(), evt.RoomID, msg); err != nil {
				slog.Error(err.Error())
			}

		default:
			// anything else, so print help
			if _, err := client.SendText(context.TODO(), evt.RoomID, helpText); err != nil {
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

	slog.Debug("matrix setup complete")

	return client
}

func handleSubUnsub(matches []string, evt *event.Event) {
	pkgName := matches[2]
	rID := evt.RoomID
	if _, ok := pkgNames[pkgName]; !ok {
		if _, err := client.SendText(context.TODO(), evt.RoomID, fmt.Sprintf("package %s does not exist", pkgName)); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	// the first capture group matches "un"
	if matches[1] != "" {
		// pkgName was found, proceed with unsub
		slog.Info("received unsub", "pkg", pkgName, "sender", evt.Sender)
		res, err := db.Exec("DELETE FROM subscriptions WHERE roomid = ?", rID)
		if err != nil {
			panic(err)
		}

		if c, err := res.RowsAffected(); err != nil {
			// SQL error
			panic(err)
		} else if c == int64(0) {
			// unsub was not present
			if _, err := client.SendText(context.TODO(), evt.RoomID, fmt.Sprintf("failed to find subscription for package %s", pkgName)); err != nil {
				slog.Error(err.Error())
			}
		} else {
			// unsub went ok, send confirmation message
			if _, err := client.SendText(context.TODO(), evt.RoomID, fmt.Sprintf("unsubscribed from package %s", pkgName)); err != nil {
				slog.Error(err.Error())
			}
		}
	} else {
		// handle subscription
		slog.Info("received sub", "pkg", pkgName, "sender", evt.Sender)

		// check if sub already exists
		// TODO can I just insert and check result/error instead?
		var c int
		if err := db.QueryRow("SELECT COUNT(*) FROM subscriptions WHERE roomid = ? AND attr_path = ?", rID, pkgName).Scan(&c); err != nil {
			panic(err)
		}

		if c != 0 {
			// sub already exists
			if _, err := client.SendText(context.TODO(), rID, "already subscribed"); err != nil {
				slog.Error(err.Error())
			}
		} else {
			// sub did not exist, insert it
			if _, err := db.Exec("INSERT INTO subscriptions(roomid,attr_path) VALUES (?, ?)", evt.RoomID, pkgName); err != nil {
				panic(err)
			}

			// send confirmation message
			if _, err := client.SendText(context.TODO(), evt.RoomID, fmt.Sprintf("subscribed to package %s", pkgName)); err != nil {
				slog.Error(err.Error())
			}
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

func setupDB() (err error) {
	db, err = sql.Open("sqlite3", fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=true", *filename))
	if err != nil {
		return
	}

	err = db.Ping()
	if err != nil {
		return
	}

	// It is dumb to keep this in a db as we're only interested in the latest value, but we do it to keep all data in one place.
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS runs (id INTEGER PRIMARY KEY, timestamp INTEGER NOT NULL) STRICT")
	if err != nil {
		return
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS subscriptions (roomid TEXT NOT NULL, attr_path TEXT NOT NULL, PRIMARY KEY (roomid, attr_path)) STRICT")
	// _, err = db.Exec("CREATE TABLE IF NOT EXISTS subscriptions (id INTEGER PRIMARY KEY, roomid TEXT NOT NULL, attr_path TEXT NOT NULL, UNIQUE (roomid, attr_path)) STRICT")
	if err != nil {
		return
	}

	return
}

func notifySubscribers(newErrors []string) error {
	// TODO rename newErrors
	for _, attr_path := range newErrors {
		// TODO: handle 429
		slog.Info("new error found", "pkg", attr_path)

		// - find all subscribers for package
		// - send message in respective room
		// - if we're not in that room, drop from db of subs?
		rows, err := db.Query("SELECT roomid from subscriptions where attr_path = ?", attr_path)
		if err != nil {
			return err
		}
		defer rows.Close()

		roomIDs := make([]string, 0)
		for rows.Next() {
			var roomID string
			if err := rows.Scan(&roomID); err != nil {
				return err
			}
			roomIDs = append(roomIDs, roomID)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		for _, roomID := range roomIDs {
			_, err := client.SendText(context.TODO(), id.RoomID(roomID), fmt.Sprintf("package contains an error: %s", attr_path))
			if err != nil {
				// TODO check if we're not in room, in that case remove sub
				slog.Error(err.Error())
			} else {
				slog.Debug("notified subscriber", "roomid", roomID)
			}
		}
	}

	return nil
}

func doWork() error {
	slog.Info("doing work")
	// visit main page to download db
	// TODO use url package to avoid double //?
	state, err := fetchStateDB(fmt.Sprintf("%s/%s", *url, "~supervisor/state.db"))
	if err != nil {
		return err
	}
	// we close the state db connection, since on the next run it will be an entirely different db
	defer state.Close()

	// find new errors since last time
	newErrors, err := findNewErrors(state)
	if err != nil {
		return err
	}

	// send messages to subscribers
	if err := notifySubscribers(newErrors); err != nil {
		return err
	}

	// update last time to now
	if _, err := db.Exec("INSERT INTO runs (timestamp) VALUES (?)", time.Now().Unix()); err != nil {
		return err
	}

	return nil
}

func getLastRun() (last int64, err error) {
	// get timestamp of last run
	if err = db.QueryRow("SELECT timestamp from runs ORDER BY timestamp DESC LIMIT 1").Scan(&last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			last = time.Now().Unix()
		}
	}

	return last, err

}
