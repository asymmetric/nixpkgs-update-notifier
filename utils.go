package main

import (
	"context"
	"fmt"
	"net/http"
	u "net/url"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

func newReqWithUA(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "https://github.com/asymmetric/nixpkgs-update-notifier")

	return req, nil
}

func sendMarkdown(s string, rid id.RoomID) (*mautrix.RespSendEvent, error) {
	m := format.RenderMarkdown(s, true, true)
	return client.SendMessageEvent(context.TODO(), rid, event.EventMessage, m)
}

func getComponents(url string) (attr_path, date string) {
	components := strings.Split(url, "/")

	attr_path = components[len(components)-2]
	date = strings.Trim(components[len(components)-1], ".log")

	return
}

func packageURL(attr_path string) string {
	parsedURL, err := u.Parse(*mainURL)
	if err != nil {
		panic(err)
	}

	return parsedURL.JoinPath(attr_path).String()
}

func logURL(attr_path, date string) string {
	purl := packageURL(attr_path)

	return fmt.Sprintf("%s/%s.log", purl, date)
}
