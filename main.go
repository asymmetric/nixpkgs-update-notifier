package main

import (
	"fmt"

	"github.com/gocolly/colly"
	"github.com/velebak/colly-sqlite3-storage/colly/sqlite3"
)

func main() {
	fmt.Println("Hello Nix!")
	url := "https://nixpkgs-update-logs.nix-community.org/"

	c := colly.NewCollector(
		colly.UserAgent("asymmetric"),
		// FIXME
		// colly.AllowedDomains(url),
		// colly.Debugger(&debug.LogDebugger{}),
		colly.AllowURLRevisit(),
	)

	storage := &sqlite3.Storage{
		Filename: "./results.db",
	}

	defer storage.Close()

	err := c.SetStorage(storage)
	if err != nil {
		panic(err)
	}

	c.OnRequest(func(r *colly.Request) {
		fmt.Println("Visiting", r.URL.String())
	})
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")

		if link == "../" {
			// fmt.Println("ignoring parent link")
			return
		}

		// fmt.Printf("Link found: %q -> %s\n", e.Text, link)
		// ok, err := storage.IsVisited(e.Request.ID)
		// if err != nil {
		// 	panic(err)
		// }
		//
		// if ok {
		// 	fmt.Println("Already visited: %s", link)
		// 	return
		// }
		c.Visit(e.Request.AbsoluteURL(link))
	})
	c.OnScraped(func(r *colly.Response) {
		fmt.Println("Finished", r.Request.URL)
	})
	c.OnError(func(r *colly.Response, err error) {
		fmt.Println("Request URL:", r.Request.URL, "failed with response:", r, "\nError:", err)
	})

	c.Visit(url)
}
