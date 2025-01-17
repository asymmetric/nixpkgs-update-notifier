package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	u "net/url"
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

func newReqWithUA(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "https://github.com/asymmetric/nixpkgs-update-notifier")

	return req, nil
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
	parsedURL, err := u.Parse(*mainURL)
	if err != nil {
		panic(err)
	}

	return parsedURL.JoinPath(attr_path).String()
}

func logURL(attr_path, date string) string {
	purl := packageURL(attr_path)

	// it's safe to just use string concatenation here because we're sure any
	// trailing / has been removed by call to packageURL
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
	slog.Debug("downloaded packages.json.br")

	slog.Debug("parsing packages.json")
	if err := json.NewDecoder(brotli.NewReader(resp.Body)).Decode(&jsblob); err != nil {
		panic(err)
	}

	slog.Info("package.json handling completed", "elapsed", time.Since(start))
}
