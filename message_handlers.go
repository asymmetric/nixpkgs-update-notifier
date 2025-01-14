package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/asymmetric/nixpkgs-update-notifier/regexes"
	"github.com/itchyny/gojq"
	"maunium.net/go/mautrix/event"
)

func handleSubUnsub(msg string, evt *event.Event) {
	// TODO move this to caller
	matches := regexes.Subscribe().FindStringSubmatch(msg)
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
			// TODO: should we notify here already if the log has an error?
			if err := subscribe(ap, evt); err != nil {
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
	matches := regexes.Follow().FindStringSubmatch(msg)
	un := matches[1]
	handle := matches[2]

	// Log early, before slow network calls.
	if un == "" {
		slog.Info("received follow", "handle", handle, "sender", evt.Sender)
	} else {
		slog.Info("received unfollow", "handle", handle, "sender", evt.Sender)
	}

	mps, err := findPackagesForHandle(h.packagesJSONFetcher(), handle)
	if err != nil {
		if _, err = h.sender("There was a problem processing your request, sorry.", evt.RoomID); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	if len(mps) == 0 {
		if _, err := h.sender(fmt.Sprintf("No packages found for maintainer `%s`", handle), evt.RoomID); err != nil {
			slog.Error(err.Error())
		}

		return
	}

	if un != "" {
		handleUnfollow(mps, evt)
	} else {
		// used for output message
		var l []string

		for _, ap := range mps {
			if err := subscribe(ap, evt); err != nil {
				panic(err)
			}
			l = append(l, fmt.Sprintf("- %s", ap))
		}

		msg = fmt.Sprintf("Subscribed to packages:\n %s", strings.Join(l, "\n"))
		if _, err := h.sender(msg, evt.RoomID); err != nil {
			slog.Error(err.Error())
		}
	}
}

func handleUnfollow(mps []string, evt *event.Event) {
	// Create the right number of placeholders: "(?,?,?)"
	qmarks := make([]string, len(mps))
	args := make([]any, len(mps))
	for i, v := range mps {
		qmarks[i] = "?"
		args[i] = v
	}
	placeholders := strings.Join(qmarks, ",")

	query := fmt.Sprintf("DELETE FROM subscriptions WHERE mxid = ? AND attr_path IN (%s) RETURNING attr_path", placeholders)
	// We need to stash evt.Sender in args, because we can onlu pass two arguments to db.Exec
	args = append([]any{evt.Sender}, args...)
	rows, err := clients.db.Query(query, args...)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	aps := make([]string, 0)
	for rows.Next() {
		var ap string
		if err = rows.Scan(&ap); err != nil {
			panic(err)
		}
		aps = append(aps, ap)
	}
	if err = rows.Err(); err != nil {
		panic(err)
	}

	var l []string
	for _, ap := range aps {
		l = append(l, fmt.Sprintf("- %s", ap))
	}

	var msg string
	if len(l) > 0 {
		msg = fmt.Sprintf("Unsubscribed from packages:\n %s", strings.Join(l, "\n"))
	} else {
		msg = "No packages to unsubscribe from"
	}

	if _, err := h.sender(msg, evt.RoomID); err != nil {
		panic(err)
	}

}

// Checks if the user is already subscribed to the package
func checkIfSubExists(attr_path, roomid string) (exists bool, err error) {
	err = clients.db.QueryRow("SELECT EXISTS (SELECT 1 FROM subscriptions WHERE roomid = ? AND attr_path = ? LIMIT 1)", roomid, attr_path).Scan(&exists)

	return exists, err
}

// 1. uses jquery to parse the JSON blob
// 2. finds list of packages maintained by handle
// 3. uses SQL to intersect with list of tracked packages
func findPackagesForHandle(jsobj map[string]any, handle string) ([]string, error) {
	// The query needs to handle:
	// missing maintainers
	// missing github field
	query, err := gojq.Parse(fmt.Sprintf(`.packages|to_entries[]|select(.value.meta.maintainers[]?|.github // "" |test("^%s$"))|.key`, handle))
	if err != nil {
		slog.Error("gojq parse", "error", err)

		return nil, err
	}

	slog.Debug("gojq query", "query", query, "jsobj len", len(jsobj))

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
			slog.Error("gojq run", "error", err)

			return nil, err
		}
		mps = append(mps, v.(string))
	}

	// Create the right number of placeholders: "(?,?,?)"
	qmarks := make([]string, len(mps))
	args := make([]any, len(mps))
	for i, v := range mps {
		qmarks[i] = "?"
		args[i] = v
	}
	placeholders := strings.Join(qmarks, ",")

	rows, err := clients.db.Query(fmt.Sprintf("SELECT attr_path FROM packages WHERE attr_path IN (%s)", placeholders), args...)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	aps := make([]string, 0)
	for rows.Next() {
		var ap string
		if err = rows.Scan(&ap); err != nil {
			panic(err)
		}
		aps = append(aps, ap)
	}
	if err = rows.Err(); err != nil {
		panic(err)
	}

	return aps, nil
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

// subscribe fetches a last_visited date, adds it to the packages table, and adds an entry into the subscriptions table.
//
// before adding subscription, add the date of the last available log for the package, so that if the program stops for any reason, we don't notify the user of a stale error log.
// we want to avoid e.g.:
// - we add a subscription for package foo at time t
// - package foo has a failure that predates the subscription, t - 1
// - program stops before the next tick
// - therefore, foo.last_visited is nil
// - when program starts again, we'll iterate over subs and find foo
// - we will fetch the latest log and because it's a failure, notify
// - but the log predates the subscription, so we notified on a stale log
//
// NOTE: this invariant is also enforced via an SQL trigger.
func subscribe(ap string, evt *event.Event) error {
	slog.Debug("Subscribing", "attr_path", ap)

	date, _ := h.logFetcher(packageURL(ap))
	if _, err := clients.db.Exec("UPDATE packages SET last_visited = ? WHERE attr_path = ?", date, ap); err != nil {
		return err
	}

	if _, err := clients.db.Exec("INSERT INTO subscriptions(attr_path, roomid, mxid) VALUES (?, ?, ?)", ap, evt.RoomID, evt.Sender); err != nil {
		return err
	}

	return nil
}
