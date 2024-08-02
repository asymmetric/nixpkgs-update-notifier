package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"io"

	"github.com/gocolly/colly"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	fmt.Println("Hello Nix!")
	url := "https://nixpkgs-update-logs.nix-community.org/"
	filename := "data.db"

	db, err := sql.Open("sqlite3", fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL", filename))
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
		colly.UserAgent("asymmetric"),
		// colly.AllowedDomains(url),
		// colly.AllowURLRevisit(),
	)

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
			visitLog(link, e, db)
		} else {
			c.Visit(e.Request.AbsoluteURL(link))
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		fmt.Println("Request URL:", r.Request.URL, "failed with response:", r, "\nError:", err)
	})

	c.Visit(url)
}

func visitLog(link string, e *colly.HTMLElement, db *sql.DB) {
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
		// fmt.Printf("> error found for link %s\n", link)
		hasError = true
		// TODO notify everyone who subscribed
	}
	// we haven't seen this log yet, so add it to the list of seen ones
	statement, _ = db.Prepare("INSERT INTO visited (package, date, error) VALUES (?, ?, ?)")
	_, err = statement.Exec(pkg, date, hasError)
	if err != nil {
		panic(err)
	}

	time.Sleep(500 * time.Millisecond)

}
