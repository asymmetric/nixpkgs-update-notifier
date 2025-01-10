package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/itchyny/gojq"
	"maunium.net/go/mautrix/event"
)

func handleSubUnsub(msg string, evt *event.Event) {
	// TODO move this to caller
	matches := regexes.subscribe.FindStringSubmatch(msg)
	if matches == nil {
		slog.Error("handleSubUnsub: We should not be here")

		return
	}

	pattern := matches[2]

	// matches[1] is the optional "un" prefix
	if matches[1] != "" {
		handleUnsub(pattern, evt)

		return
	}

	rows, err := clients.db.Query("SELECT attr_path FROM packages WHERE attr_path GLOB ? ORDER BY attr_path", pattern)
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
		if _, err = h.sender(fmt.Sprintf("No matches for `%s`. The list of packages is [here](https://nixpkgs-update-logs.nix-community.org/)", pattern), evt.RoomID); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	for _, ap := range aps {
		if exists, err := checkIfSubExists(ap, evt.RoomID.String()); err != nil {
			panic(err)
		} else if exists {
			if _, err := h.sender(fmt.Sprintf("Already subscribed to package `%s`", ap), evt.RoomID); err != nil {
				slog.Error(err.Error())
			}
		} else {
			// before adding subscription, add the date of the last available log for the package, so that if the program stops for any reason, we don't notify the user of a stale error log.
			// we want to avoid e.g.:
			// - we add a subscription for package foo at time t
			// - package foo has a failure that predates the subscription, t - 1
			// - program stops before the next tick
			// - therefore, foo.last_visited is nil
			// - when program starts again, we'll iterate over subs and find foo
			// - we will fetch the latest log and because it's a failure, notify
			// - but the log predates the subscription, so we notified on a stale log
			// NOTE: this invariant is also enforced via an SQL trigger.
			purl := packageURL(ap)
			date, _ := h.logFetcher(purl)
			// TODO: should we notify here already if the log has an error?
			if _, err := clients.db.Exec("UPDATE packages SET last_visited = ? WHERE attr_path = ?", date, ap); err != nil {
				panic(err)
			}

			if _, err := clients.db.Exec("INSERT INTO subscriptions(roomid,attr_path,mxid) VALUES (?, ?, ?)", evt.RoomID, ap, evt.Sender); err != nil {
				panic(err)
			}

			// send confirmation message
			if _, err := h.sender(fmt.Sprintf("Subscribed to package `%s`", ap), evt.RoomID); err != nil {
				slog.Error(err.Error())
			}

			slog.Info("added sub", "ap", ap, "sender", evt.Sender)
		}
	}
}

func handleUnsub(pattern string, evt *event.Event) {
	rows, err := clients.db.Query("DELETE FROM subscriptions WHERE roomid = ? AND attr_path GLOB ? RETURNING attr_path", evt.RoomID, pattern)
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
		msg = fmt.Sprintf("Could not find subscriptions for pattern `%s`", pattern)
	} else {
		var l []string
		for _, ap := range aps {
			l = append(l, fmt.Sprintf("- %s", ap))
		}

		msg = fmt.Sprintf("Unsubscribed from packages:\n %s", strings.Join(l, "\n"))
	}

	// send confirmation message
	if _, err := h.sender(msg, evt.RoomID); err != nil {
		slog.Error(err.Error())
	}

	slog.Info("received unsub", "pkg", pattern, "sender", evt.Sender, "deleted", len(aps))
}

// TODO find ways to test this
func handleSubs(evt *event.Event) {
	rows, err := clients.db.Query("SELECT attr_path FROM subscriptions WHERE roomid = ?", evt.RoomID)
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
		sts := []string{"Your subscriptions:\n"}

		for _, n := range names {
			sts = append(sts, fmt.Sprintf("- %s", n))
		}

		msg = strings.Join(sts, "\n")
	}
	if _, err = h.sender(msg, evt.RoomID); err != nil {
		slog.Error(err.Error())
	}
}

func handleFollowUnfollow(msg string, evt *event.Event) {
	matches := regexes.follow.FindStringSubmatch(msg)
	un := matches[1]
	handle := matches[2]
	pjson := h.packagesJSONFetcher()

	aps := findPackagesForHandle(pjson, handle)

	if un != "" {
		if _, err := clients.db.Exec("DELETE FROM subscriptions WHERE mxid = ? AND attr_path IN ?", evt.Sender, aps); err != nil {
			panic(err)
		}
	} else {
		for _, ap := range aps {
			if _, err := clients.db.Exec("INSERT INTO subscriptions(roomid,attr_path,mxid) VALUES (?, ?, ?)", evt.RoomID, ap, evt.Sender); err != nil {
				panic(err)
			}
		}
	}

}

// Checks if the user is already subscribed to the package
func checkIfSubExists(attr_path, roomid string) (exists bool, err error) {
	err = clients.db.QueryRow("SELECT EXISTS (SELECT 1 FROM subscriptions WHERE roomid = ? AND attr_path = ? LIMIT 1)", roomid, attr_path).Scan(&exists)

	return exists, err
}

func findPackagesForHandle(jsobj map[string]any, handle string) []string {
	query, err := gojq.Parse(fmt.Sprintf(`.packages|to_entries[]|select(.value.meta.maintainers[]?|.github|index("%s"))|.key`, handle))
	if err != nil {
		panic(err)
	}

	// list of maintained packages
	var mps []string
	iter := query.Run(jsobj)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			if err, ok := err.(*gojq.HaltError); ok && err.Value() == nil {
				break
			}
			panic(err)
		}
		mps = append(mps, v.(string))
	}

	return mps
}

// Fetches the packages.json.br, unpacks it and returns it as serialized JSON.
func fetchPackagesJSON() (jsobj map[string]any) {
	resp, err := http.Get(packagesURL)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(brotli.NewReader(resp.Body))
	if err != nil {
		panic(err)
	}

	if err := json.Unmarshal(data, &jsobj); err != nil {
		panic(err)
	}

	return
}
