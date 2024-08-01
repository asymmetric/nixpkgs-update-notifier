package main

import (
	"database/sql"
	"fmt"
	"hash/fnv"
	"net/http"
	"regexp"
	"strings"

	"io"

	"github.com/gocolly/colly"
	"github.com/velebak/colly-sqlite3-storage/colly/sqlite3"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	fmt.Println("Hello Nix!")
	url := "https://nixpkgs-update-logs.nix-community.org/"
	filename := "data.sql"

	db, err := sql.Open("sqlite3", filename)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		panic(err)
	}

	statement, _ := db.Prepare("CREATE TABLE IF NOT EXISTS visited (id INTEGER PRIMARY KEY, pathID INTEGER) STRICT")
	_, err = statement.Exec()
	if err != nil {
		panic(err)
	}

	statement, _ = db.Prepare("CREATE INDEX IF NOT EXISTS idx_visited ON visited (pathID)")
	_, err = statement.Exec()
	if err != nil {
		panic(err)
	}

	c := colly.NewCollector(
		colly.UserAgent("asymmetric"),
		// colly.AllowedDomains(url),
		// colly.Debugger(&debug.LogDebugger{}),
		// colly.AllowURLRevisit(),
	)

	storage := &sqlite3.Storage{
		Filename: "./results.db",
	}

	defer storage.Close()

	// err := c.SetStorage(storage)
	// if err != nil {
	// 	panic(err)
	// }

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

		fullpath := e.Request.AbsoluteURL(link)

		// if it's a link to a log file:
		// - did we already know this file?
		// - download file
		// - grep for failure
		if match {
			fmt.Printf("log file: %s\n", fullpath)

			var count int
			h := fnv.New64a()
			io.WriteString(h, fullpath)
			pathID := h.Sum64()

			statement, err := db.Prepare("SELECT COUNT(*) FROM visited where pathId = ?")
			if err != nil {
				panic(err)
			}
			// [golang/go/issues/6113] we can't use uint64 with the high bit set
			// but we can cast it and store as an int64 without data loss
			err = statement.QueryRow(int64(pathID)).Scan(&count)
			if err != nil {
				panic(err)
			}
			if count > 1 {
				panic(err)
			}
			// we've found this log already, skip next steps
			if count == 1 {
				fmt.Printf("  link already there: %s\n", fullpath)
				return
			}

			statement, err = db.Prepare("INSERT INTO visited (pathID) VALUES (?)")
			if err != nil {
				panic(err)
			}
			_, err = statement.Exec(int64(pathID))
			if err != nil {
				panic(err)
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

			if strings.Contains(string(body[:]), "error") {
				fmt.Printf("> error found for link %s", link)
				// TODO notify everyone who subscribed
			} else {
				fmt.Printf("< error not found for link %s", link)
			}
		}

		c.Visit(e.Request.AbsoluteURL(link))
	})

	c.OnError(func(r *colly.Response, err error) {
		fmt.Println("Request URL:", r.Request.URL, "failed with response:", r, "\nError:", err)
	})

	c.Visit(url)
}
