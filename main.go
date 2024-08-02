package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"io"

	"github.com/gocolly/colly"
	"github.com/gocolly/colly/queue"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"

	_ "github.com/mattn/go-sqlite3"
)

var homeserver = pflag.String("homeserver", "matrix.org", "Matrix homeserver for the bot account")
var url = pflag.String("url", "https://nixpkgs-update-logs.nix-community.org/", "Webpage with logs")
var filename = pflag.String("db", "data.db", "Path to the DB file")
var config = pflag.String("config", "config.toml", "Config file")
var username = pflag.String("username", "", "Matrix bot username")

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
	db, err := sql.Open("sqlite3", fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL", viper.GetString("db")))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		panic(err)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS visited (id INTEGER PRIMARY KEY, package TEXT, date TEXT, error INTEGER) STRICT")
	if err != nil {
		panic(err)
	}

	c := colly.NewCollector(
		colly.UserAgent("github.com/asymmetric/nixpkgs-update-notifier"),
		// colly.AllowedDomains(url),
		// colly.AllowURLRevisit(),
	)

	q, err := queue.New(1, nil)
	if err != nil {
		panic(err)
	}

	var client *mautrix.Client
	if viper.GetBool("matrix.enabled") {
		client = setupMatrix()
	} else {
		client = &mautrix.Client{}
	}

	fmt.Println("Initialized")

	startParse(c, q, db, client)

	// this is blocking
	q.Run(c)
}

func startParse(c *colly.Collector, q *queue.Queue, db *sql.DB, client *mautrix.Client) {

	// c.OnRequest(func(r *colly.Request) {
	// 	fmt.Println("Visiting", r.URL.String())
	// })
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")

		if link == "../" {
			// fmt.Println("ignoring parent link")
			return
		}

		match, err := regexp.MatchString(`\.log$`, link)
		if err != nil {
			panic(err)
		}

		// if it's a link to a log file:
		// - did we already know this file?
		// - download file
		// - grep for failure
		if match {
			visitLog(link, e, db, client)
		} else {
			q.AddURL(e.Request.AbsoluteURL(link))
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		fmt.Println("Request URL:", r.Request.URL, "failed with response:", r, "\nError:", err)
	})

	q.AddURL(viper.GetString("url"))
}
func visitLog(link string, e *colly.HTMLElement, db *sql.DB, client *mautrix.Client) {
	fullpath := e.Request.AbsoluteURL(link)
	components := strings.Split(fullpath, "/")
	pkg := components[len(components)-2]
	date := strings.Trim(components[len(components)-1], ".log")
	fmt.Printf("pkg: %s; date: %s\n", pkg, date)

	var count int
	statement, err := db.Prepare("SELECT COUNT(*) FROM visited where package = ? AND date = ?")
	if err != nil {
		panic(err)
	}
	defer statement.Close()

	err = statement.QueryRow(pkg, date).Scan(&count)
	if err != nil {
		panic(err)
	}
	// we've found this log already, skip next steps
	if count == 1 {
		fmt.Println("  skipping")
		return
	}

	resp, err := http.Get(e.Request.AbsoluteURL(link))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	var hasError bool
	if strings.Contains(string(body[:]), "error") {
		hasError = true
		if viper.GetBool("matrix.enabled") {
			// TODO: handle 429
			_, err := client.SendText(context.TODO(), "!MenOKIzGKBfJIaUlTC:matrix.dapp.org.uk", fmt.Sprintf("logfile contains an error: %s", fullpath))
			if err != nil {
				panic(err)
			}
		} else {
			fmt.Printf("> error found for link %s\n", link)
		}

	}
	// we haven't seen this log yet, so add it to the list of seen ones
	statement, err = db.Prepare("INSERT INTO visited (package, date, error) VALUES (?, ?, ?)")
	if err != nil {
		panic(err)
	}
	defer statement.Close()

	_, err = statement.Exec(pkg, date, hasError)
	if err != nil {
		panic(err)
	}

	time.Sleep(500 * time.Millisecond)

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

	return client
}
