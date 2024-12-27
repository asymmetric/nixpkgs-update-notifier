package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	u "net/url"
	"os"
	"strings"

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
	return client.SendMessageEvent(context.TODO(), rid, event.EventMessage, md)
}

// date looks like 2024-12-10
func getComponents(url string) (attr_path, date string) {
	components := strings.Split(url, "/")

	attr_path = components[len(components)-2]
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

func fatal(err error) {
	slog.Error("error", err)
	os.Exit(restartExitCode)
}
