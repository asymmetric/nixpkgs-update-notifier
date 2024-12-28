package main

import (
	"context"
	"fmt"
	"log/slog"

	"maunium.net/go/mautrix/event"
)

func handleSubUnsub(msg string, evt *event.Event) {
	matches := subUnsubRE.FindStringSubmatch(msg)
	if matches == nil {
		slog.Error("We should not be here")
		return
	}

	pkgName := matches[2]

	// check if package exists
	var c int
	if err := db.QueryRow("SELECT COUNT(*) FROM packages WHERE attr_path = ?", pkgName).Scan(&c); err != nil {
		panic(err)
	}

	if c == 0 {
		if _, err := h.sender(fmt.Sprintf("could not find package `%s`. The list is [here](https://nixpkgs-update-logs.nix-community.org/)", pkgName), evt.RoomID); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	// matches[1] is the optional "un" prefix
	if matches[1] != "" {
		slog.Info("received unsub", "pkg", pkgName, "sender", evt.Sender)
		res, err := db.Exec("DELETE FROM subscriptions WHERE roomid = ? AND attr_path = ?", evt.RoomID, pkgName)
		if err != nil {
			panic(err)
		}

		var msg string
		if val, err := res.RowsAffected(); err != nil {
			panic(err)
		} else if val == 0 {
			msg = fmt.Sprintf("could not find subscription for package `%s`", pkgName)
		} else {
			msg = fmt.Sprintf("unsubscribed from package `%s`", pkgName)
		}

		// send confirmation message
		if _, err := h.sender(msg, evt.RoomID); err != nil {
			slog.Error(err.Error())
		}
		return
	}

	slog.Info("received sub", "pkg", pkgName, "sender", evt.Sender)

	if err := db.QueryRow("SELECT COUNT(*) FROM subscriptions WHERE roomid = ? AND attr_path = ?", evt.RoomID, pkgName).Scan(&c); err != nil {
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
	purl := packageURL(pkgName)
	date, hasError := h.logFetcher(purl)
	if _, err := db.Exec("UPDATE packages SET last_visited = ?, error = ? WHERE attr_path = ?", date, hasError, pkgName); err != nil {
		panic(err)
	}

	if _, err := db.Exec("INSERT INTO subscriptions(roomid,attr_path,mxid) VALUES (?, ?, ?)", evt.RoomID, pkgName, evt.Sender); err != nil {
		panic(err)
	}

	// send confirmation message
	if _, err := h.sender(fmt.Sprintf("subscribed to package `%s`", pkgName), evt.RoomID); err != nil {
		slog.Error(err.Error())
	}

}
