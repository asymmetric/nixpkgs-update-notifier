package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

const restartExitCode = 100

const helpText = `Welcome to the nixpkgs-update-notifier bot!

These are the available commands:

- **sub foo**: subscribe to package <code>foo</code>
- **unsub foo**: unsubscribe from package <code>foo</code>
- **follow foo**: subscribe to all packages maintained by GitHub handle <code>foo</code>
- **unfollow foo**: unsubscribe to all packages maintained by GitHub handle <code>foo</code>
- **subs**: list subscriptions
- **help**: show this help message

You can use the <code>*</code> and <code>?</code> globs in queries. Things you can do:

- <code>sub python31?Packages.acme</code>
- <code>sub *.acme</code>

Things you cannot do:

- <code>sub *</code>
- <code>sub ?</code>
- <code>sub foo.*</code>
- <code>follow *</code>

The code for the bot is [here](https://github.com/asymmetric/nixpkgs-update-notifier).
`

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP error: %d - %s", e.StatusCode, e.Body)
}

func newReqWithUA(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "https://github.com/asymmetric/nixpkgs-update-notifier")

	return req, nil
}

func makeRequest(url string) ([]byte, error) {
	req, err := newReqWithUA(url)
	if err != nil {
		return nil, err
	}

	resp, err := clients.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
		}
	}

	return body, nil
}

func sendMarkdown(text string, rid id.RoomID) (*mautrix.RespSendEvent, error) {
	md := format.RenderMarkdown(text, true, true)
	return clients.matrix.SendMessageEvent(context.TODO(), rid, event.EventMessage, md)
}

// Given a log url, returns its date.
//
// date looks like 2024-12-10
func getDate(url string) (date string) {
	components := strings.Split(url, "/")

	date = strings.Trim(components[len(components)-1], ".log")

	return
}

// Returns the full package URL by appending its attr_path to the base URL.
func packageURL(attr_path string) string {
	return fmt.Sprintf("%s/%s", strings.Trim(*mainURL, "/"), strings.Trim(attr_path, "/"))
}

func logURL(attr_path, date string) string {
	purl := packageURL(attr_path)

	return fmt.Sprintf("%s/%s.log", purl, date)
}

// This one should be used if there's an irrecoverable problem, e.g. IO with the DB.
func fatal(err error) {
	slog.Error("error", err)
	os.Exit(restartExitCode)
}

// TODO: log duration of the whole thing to Info
// Fetches the packages.json.br, unpacks it and parses it.
func fetchPackagesJSON() {
	slog.Debug("downloading packages.json.br")

	start := time.Now()
	resp, err := http.Get(packagesURL)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	slog.Debug("parsing packages.json")
	if err := json.NewDecoder(brotli.NewReader(resp.Body)).Decode(&jsblob); err != nil {
		panic(err)
	}

	slog.Info("package.json handling completed", "elapsed", time.Since(start))
}
