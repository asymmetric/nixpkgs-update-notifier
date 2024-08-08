package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	u "net/url"
	"regexp"
	"strings"
	"time"

	"io"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/net/html"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"

	_ "github.com/mattn/go-sqlite3"
)

var homeserver = pflag.String("homeserver", "matrix.org", "Matrix homeserver for the bot account")
var url = pflag.String("url", "https://nixpkgs-update-logs.nix-community.org", "Webpage with logs")
var filename = pflag.String("db", "data.db", "Path to the DB file")
var config = pflag.String("config", "config.toml", "Config file")
var username = pflag.String("username", "", "Matrix bot username")
var delay = pflag.Duration("delay", 24*time.Hour, "How often to check url")

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
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS visited (id INTEGER PRIMARY KEY, pkgid INTEGER, date TEXT, error INTEGER, UNIQUE(pkgid, date), FOREIGN KEY(pkgid) REFERENCES packages(id)) STRICT")
	if err != nil {
		panic(err)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS subscriptions (id INTEGER PRIMARY KEY, mxid TEXT, pkgid INTEGER, UNIQUE(mxid, pkgid), FOREIGN KEY(pkgid) REFERENCES packages(id)) STRICT")
	if err != nil {
		panic(err)
	}

	c := setupMatrix()

	fmt.Println("Initialized")

	chURL := make(chan string)
	chFetch := make(chan bool)
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
	// go scrapeLinks(viper.GetString("url"), chURL)

	go fetchMain(viper.GetString("url"), chURL, chFetch)

	for {
		select {
		case url := <-chURL:
			fmt.Printf("got url on chan: %s\n", url)
			logLink, err := regexp.MatchString(`\.log$`, url)
			if err != nil {
				panic(err)
			}

			if logLink {
				// parse link
				// is it error?
				visitLog(url, db, c)
			} else {
				go scrapeLinks(url, chURL)
			}
		case <-chFetch:
			go fetchMain(viper.GetString("url"), chURL, chFetch)
		case <-chSync:
			// sync to matrix
		}
	}
}

func fetchMain(url string, chURL chan<- string, chFetch chan<- bool) {
	scrapeLinks(url, chURL)

	time.Sleep(viper.GetDuration("delay"))

	chFetch <- true
}

// fetches the HTML at a `url`, then iterates over <a> elements adding all links to channel `ch`
func scrapeLinks(url string, ch chan<- string) {
	parsedURL, err := u.Parse(url)
	if err != nil {
		panic(err)
	}
	resp, err := http.Get(parsedURL.String())
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	r := io.Reader(resp.Body)
	z := html.NewTokenizer(r)

	for {
		tt := z.Next()

		switch tt {
		case html.ErrorToken:
			// done
			fmt.Println("done parsing")
			return
		case html.StartTagToken:
			t := z.Token()

			isAnchor := t.Data == "a"
			if isAnchor {
				for _, a := range t.Attr {
					if a.Key == "href" && a.Val != "../" {
						fullURL := parsedURL.JoinPath(a.Val)
						fmt.Printf("parsed url: %s\n", fullURL)
						// add link to queue
						ch <- fullURL.String()
						break
					}
				}
			}
		}
	}
}

func visitLog(url string, db *sql.DB, client *mautrix.Client) {
	components := strings.Split(url, "/")
	pkgName := components[len(components)-2]
	date := strings.Trim(components[len(components)-1], ".log")
	fmt.Printf("pkg: %s; date: %s\n", pkgName, date)

	// pkgName -> pkgID
	var pkgID int64
	statement, err := db.Prepare("SELECT id from packages WHERE name = ?")
	if err != nil {
		panic(err)
	}
	defer statement.Close()

	err = statement.QueryRow(pkgName).Scan(&pkgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Printf("package %s not there yet, inserting\n", pkgName)
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

	err = statement.QueryRow(pkgID, date).Scan(&count)
	if err != nil {
		panic(err)
	}

	// we've found this log already, skip next steps
	if count == 1 {
		fmt.Println("  skipping")
		return
	}

	resp, err := http.Get(url)
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
	if strings.Contains(string(body[:]), "error") {
		hasError = true
		if viper.GetBool("matrix.enabled") {
			// TODO: handle 429
			_, err := client.SendText(context.TODO(), "!MenOKIzGKBfJIaUlTC:matrix.dapp.org.uk", fmt.Sprintf("logfile contains an error: %s", url))
			if err != nil {
				panic(err)
			}
		} else {
			fmt.Printf("> error found for link %s\n", url)
		}

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
	client, err := mautrix.NewClient(viper.GetString("homeserver"), "", "")
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
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		msg := evt.Content.AsMessage().Body
		sender := evt.Sender.String()

		fmt.Printf("rcv: %s; from: %s\n", msg, sender)
	})
	syncer.OnEventType(event.EventEncrypted, func(ctx context.Context, evt *event.Event) {
		msg := evt.Content.AsMessage().Body
		sender := evt.Sender.String()

		fmt.Printf("rcv(enc): %s; from: %s\n", msg, sender)
	})
	client.Syncer = syncer

	return client
}
