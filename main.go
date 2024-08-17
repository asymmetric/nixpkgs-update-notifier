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
	"maunium.net/go/mautrix/id"

	_ "github.com/mattn/go-sqlite3"
)

var homeserver = pflag.String("homeserver", "matrix.org", "Matrix homeserver for the bot account")
var url = pflag.String("url", "https://nixpkgs-update-logs.nix-community.org", "Webpage with logs")
var filename = pflag.String("db", "data.db", "Path to the DB file")
var config = pflag.String("config", "config.toml", "Config file")
var username = pflag.String("username", "", "Matrix bot username")
var delay = pflag.Duration("delay", 24*time.Hour, "How often to check url")
var debug = pflag.Bool("debug", false, "Enable debug logging")

var subRegexp = regexp.MustCompile(`sub(?:scribe)? ([\w.]+)`)
var helpText = `- **help**: show help
- **sub foo.bar**: subscribe to package foo.bar
`
var eventID = "io.github.asymmetric"

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

	if viper.GetBool("debug") {
		opts := &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}
		h := slog.NewTextHandler(os.Stderr, opts)
		slog.SetDefault(slog.New(h))
	}

	db, err := sql.Open("sqlite3", fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=true", viper.GetString("db")))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		panic(err)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS packages (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL) STRICT")
	if err != nil {
		panic(err)
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS visited (id INTEGER PRIMARY KEY, pkgid INTEGER, date TEXT NOT NULL, error INTEGER, UNIQUE(pkgid, date), FOREIGN KEY(pkgid) REFERENCES packages(id)) STRICT")
	if err != nil {
		panic(err)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS subscriptions (id INTEGER PRIMARY KEY, mxid TEXT, pkgid INTEGER, UNIQUE(mxid, pkgid), FOREIGN KEY(pkgid) REFERENCES packages(id)) STRICT")
	if err != nil {
		panic(err)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS matrix_sync_tokens (mxid TEXT PRIMARY KEY, sync_token TEXT NOT NULL) STRICT")
	if err != nil {
		panic(err)
	}

	// TODO: this should not run if matrix is disabled
	c := setupMatrix()

	if viper.GetBool("matrix.enabled") {
		go func() {
			if err := c.Sync(); err != nil {
				// TODO: recover from errors rather than panicking
				panic(err)
			}
		}()
	}

	ch := make(chan string)
	ticker := time.NewTicker(viper.GetDuration("delay"))
	slog.Debug("delay set", "value", viper.GetDuration("delay"))
	chSync := make(chan bool)

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
	re, err := regexp.Compile(`\.log$`)
	if err != nil {
		panic(err)
	}

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
				visitLog(url, db, c, hCli)
			} else {
				slog.Debug("scraping link", "url", url)
				go scrapeLinks(url, ch, hCli)
			}
		case <-ticker.C:
			slog.Debug(">>> ticker")
			go scrapeLinks(viper.GetString("url"), ch, hCli)
		case <-chSync:
			// sync to matrix
		}
	}
}

// fetches the HTML at a `url`, then iterates over <a> elements adding all links to channel `ch`
func scrapeLinks(url string, ch chan<- string, hCli *http.Client) {
	parsedURL, err := u.Parse(url)
	if err != nil {
		panic(err)
	}
	resp, err := hCli.Get(parsedURL.String())
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	z := html.NewTokenizer(bytes.NewReader(r))

	re, err := regexp.Compile("^~")
	if err != nil {
		panic(err)
	}

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
					// TODO: skip ~ links
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
func visitLog(url string, db *sql.DB, mCli *mautrix.Client, hCli *http.Client) {
	components := strings.Split(url, "/")
	pkgName := components[len(components)-2]
	date := strings.Trim(components[len(components)-1], ".log")
	slog.Debug("log found", "pkg", pkgName, "date", date)

	// pkgName -> pkgID
	var pkgID int64
	statement, err := db.Prepare("SELECT id from packages WHERE name = ?")
	if err != nil {
		panic(err)
	}
	defer statement.Close()

	err = statement.QueryRow(pkgName).Scan(&pkgID)
	if err != nil {
		// TODO: move this to first pass, to simplify this code
		// - as we add logs to queue, add missing packages to db
		// Package did not exist in db, inserting
		if errors.Is(err, sql.ErrNoRows) {
			slog.Info("new package found", "pkg", pkgName)
			statement, err := db.Prepare("INSERT INTO packages(name) VALUES (?)")
			if err != nil {
				panic(err)
			}

			result, err := statement.Exec(pkgName)
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
	// TODO: use SELECT 1 here instead? no because it can return zero rows when not found
	statement, err = db.Prepare("SELECT COUNT(*) FROM visited where pkgid = ? AND date = ?")
	if err != nil {
		panic(err)
	}
	defer statement.Close()

	// TODO check for ErrNoRows here too?
	err = statement.QueryRow(pkgID, date).Scan(&count)
	if err != nil {
		panic(err)
	}

	// we've found this log already, skip next steps
	if count == 1 {
		slog.Debug("  skipping")
		return
	}

	resp, err := hCli.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	// check for error in logs
	var hasError bool
	if bytes.Contains(body, []byte("error")) {
		hasError = true

		slog.Info("new log found", "err", true, "url", url)

		if viper.GetBool("matrix.enabled") {
			// TODO: handle 429
			_, err := mCli.SendText(context.TODO(), "!MenOKIzGKBfJIaUlTC:matrix.dapp.org.uk", fmt.Sprintf("logfile contains an error: %s", url))
			if err != nil {
				panic(err)
			}
		} else {
		}
	} else {
		slog.Info("new log found", "err", false, "url", url)
	}

	// we haven't seen this log yet, so add it to the list of seen ones
	statement, err = db.Prepare("INSERT INTO visited (pkgid, date, error) VALUES (?, ?, ?)")
	if err != nil {
		panic(err)
	}
	defer statement.Close()

	_, err = statement.Exec(pkgID, date, hasError)
	if err != nil {
		panic(err)
	}

	// time.Sleep(500 * time.Millisecond)

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
				panic(err)
			}

			slog.Debug("joining room", "id", evt.RoomID)
		case event.MembershipLeave:
			if _, err := client.LeaveRoom(ctx, evt.RoomID); err != nil {
				panic(err)
			}

			slog.Debug("leaving room", "id", evt.RoomID)
		}
	})

	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		msg := evt.Content.AsMessage().Body
		sender := evt.Sender.String()

		if sender == fmt.Sprintf("@%s:%s", viper.GetString("matrix.username"), viper.GetString("matrix.homeserver")) {
			slog.Debug("ignoring our own message", "msg", msg)
			return
		}

		slog.Debug("received msg", "msg", msg, "sender", sender)

		// TODO
		// - last success/first fail
		if matches := subRegexp.FindStringSubmatch(msg); matches != nil {
			pkg := matches[1]
			slog.Info("received sub", "pkg", pkg, "sender", sender)
			// TOD
		} else {
			_, err := client.SendText(context.TODO(), evt.RoomID, helpText)
			if err != nil {
				panic(err)
			}
			slog.Debug("received help", "sender", sender)
		}
	})

	syncer.FilterJSON = &mautrix.Filter{
		AccountData: mautrix.FilterPart{
			Limit: 20,
			NotTypes: []event.Type{
				event.NewEventType(eventID),
			},
		},
	}
	client.Syncer = syncer

	return client
}
