package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"maunium.net/go/mautrix/event"
)

func handleSubUnsub(msg string, evt *event.Event) {
	// TODO move this to caller
	matches := subUnsubRE.FindStringSubmatch(msg)
	if matches == nil {
		slog.Error("handleGlobs: We should not be here")

		return
	}

	pattern := matches[2]

	// matches[1] is the optional "un" prefix
	if matches[1] != "" {
		handleUnsub(pattern, evt)

		return
	}

	rows, err := db.Query("SELECT attr_path FROM packages WHERE attr_path GLOB ?", pattern)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	aps := make([]string, 0)
	for rows.Next() {
		var ap string
		if err := rows.Scan(&ap); err != nil {
			panic(err)
		}
		aps = append(aps, ap)
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}

	slog.Info("received sub", "pattern", pattern, "sender", evt.Sender, "matches", len(aps))

	if len(aps) == 0 {
		if _, err = client.SendText(context.TODO(), evt.RoomID, fmt.Sprintf("no matches for %s. The list of packages is [here](https://nixpkgs-update-logs.nix-community.org/)", pattern)); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	var c int
	for _, ap := range aps {
		// Check if the user is already subscribed to the package
		if err := db.QueryRow("SELECT COUNT(*) FROM subscriptions WHERE roomid = ? AND attr_path = ?", evt.RoomID, ap).Scan(&c); err != nil {
			panic(err)
		}

		if c != 0 {
			if _, err := client.SendText(context.TODO(), evt.RoomID, "already subscribed"); err != nil {
				slog.Error(err.Error())
			}
			return
		}

		// before adding subscription, add the date of the last available log for the package, so that if the program stops for any reason, we don't notify the user of a stale error log.
		// e.g.:
		// - we add a subscription for package foo at time t
		// - package foo has a failure that predates the subscription, t - 1
		// - program stops before the next tick
		// - therefore, foo.last_visited is nil
		// - when program starts again, we'll iterate over subs and find foo
		// - we will fetch the latest log and because it's a failure, notify
		// - but the log predates the subscription, so we notified on a stale log
		purl := packageURL(ap)
		date, hasError := h.logFetcher(purl)
		if _, err := db.Exec("UPDATE packages SET last_visited = ?, error = ? WHERE attr_path = ?", date, hasError, ap); err != nil {
			panic(err)
		}

		if _, err := db.Exec("INSERT INTO subscriptions(roomid,attr_path,mxid) VALUES (?, ?, ?)", evt.RoomID, ap, evt.Sender); err != nil {
			panic(err)
		}

		// send confirmation message
		if _, err := h.sender(fmt.Sprintf("subscribed to package `%s`", ap), evt.RoomID); err != nil {
			slog.Error(err.Error())
		}

		slog.Info("added sub", "ap", ap, "sender", evt.Sender)
	}
}

func handleUnsub(pattern string, evt *event.Event) {
	// TODO: add returning, so we can print aps back to user
	rows, err := db.Query("DELETE FROM subscriptions WHERE roomid = ? AND attr_path GLOB ? RETURNING attr_path", evt.RoomID, pattern)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	var aps []string
	for rows.Next() {
		var ap string
		if err := rows.Scan(&ap); err != nil {
			panic(err)
		}

		aps = append(aps, ap)
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}

	var msg string
	if len(aps) == 0 {
		msg = fmt.Sprintf("could not find subscriptions for query `%s`", pattern)
	} else {
		var l []string
		for _, ap := range aps {
			l = append(l, fmt.Sprintf("- %s", ap))
		}

		msg = fmt.Sprintf("unsubscribed from packages:\n %s", strings.Join(l, "\n"))
	}

	// send confirmation message
	if _, err := h.sender(msg, evt.RoomID); err != nil {
		slog.Error(err.Error())
	}

	slog.Info("received unsub", "pkg", pattern, "sender", evt.Sender, "deleted", len(aps))
}

// TODO find ways to test this
func handleSubs(evt *event.Event) {
	rows, err := db.Query("SELECT attr_path FROM subscriptions WHERE roomid = ?", evt.RoomID)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			panic(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}

	var msg string
	if len(names) == 0 {
		msg = "no subs"
	} else {
		sts := []string{"Your subscriptions:"}

		for _, n := range names {
			sts = append(sts, fmt.Sprintf("- %s", n))
		}

		msg = strings.Join(sts, "\n")
	}
	if _, err = client.SendText(context.TODO(), evt.RoomID, msg); err != nil {
		slog.Error(err.Error())
	}
}
