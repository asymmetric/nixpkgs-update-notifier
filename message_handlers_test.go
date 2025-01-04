package main

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

var ctx context.Context
var evt *event.Event
var testSender = func(text string, _ id.RoomID) (*mautrix.RespSendEvent, error) {
	return nil, nil
}

func init() {
	slog.SetLogLoggerLevel(slog.LevelError)
	ctx = context.Background()

	evt = &event.Event{
		RoomID: id.RoomID("test-room"),
		Sender: id.UserID("test-sender"),
	}

	client, _ = mautrix.NewClient("http://localhost", "", "")
}

// TODO: test non-existent package
func TestSub(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	tt := []struct {
		ap  string
		lv  string
		err bool
	}{
		{
			ap:  "foo",
			lv:  "1970-01-01",
			err: false,
		},
		{
			ap:  "python312Packages.foo",
			lv:  "1980-01-01",
			err: true,
		},
	}

	var count int
	var lv string
	var hasErr bool
	for _, v := range tt {
		h = handlers{
			logFetcher: func(string) (string, bool) {
				return v.lv, v.err
			},
			sender: testSender,
		}
		if _, err := db.Exec("INSERT INTO packages(attr_path) VALUES (?)", v.ap); err != nil {
			panic(err)
		}

		subscribe(v.ap)

		if err := db.QueryRow(`
      SELECT COUNT(*)
      FROM subscriptions
      WHERE roomid = ?
        AND mxid = ?
        AND attr_path = ?`, evt.RoomID, evt.Sender, v.ap).
			Scan(&count); err != nil {
			panic(err)
		}

		if count == 0 {
			t.Error("Subscription not found")
		} else if count > 1 {
			t.Error("Too many matches")
		}

		if err := db.QueryRow(`SELECT last_visited, error FROM packages WHERE attr_path = ?`, v.ap).Scan(&lv, &hasErr); err != nil {
			panic(err)
		}

		if lv != v.lv {
			t.Errorf("Wrong last_visited date: %s", lv)
		}

		if hasErr != v.err {
			t.Errorf("Wrong hasError: %v", hasErr)
		}
	}
}

// TODO: test non-existent package
func TestUnsub(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	// TODO: what's the point of having two test cases here?
	tt := []struct {
		ap  string
		lv  string
		err bool
	}{
		{
			ap:  "foo",
			lv:  "1970-01-01",
			err: false,
		},
		{
			ap:  "python312Packages.bar",
			lv:  "1980-01-01",
			err: true,
		},
	}

	var count int
	for _, v := range tt {
		h = handlers{
			logFetcher: func(string) (string, bool) {
				return v.lv, v.err
			},
			sender: testSender,
		}
		if _, err := db.Exec("INSERT INTO packages(attr_path, last_visited) VALUES (?, ?)", v.ap, v.lv); err != nil {
			panic(err)
		}

		// NOTE: in this test, we insert last_visited ourselves instead of relying
		// on the Go logic, since we've tested that logic in the TestSub test
		if _, err := db.Exec("INSERT INTO subscriptions (roomid, mxid, attr_path) VALUES (?, ?, ?)",
			evt.RoomID, evt.Sender, v.ap); err != nil {
			panic(err)
		}

		unsubscribe(v.ap)

		if err := db.QueryRow(`
      SELECT COUNT(*)
      FROM subscriptions
      WHERE roomid = ?
        AND mxid = ?
        AND attr_path = ?`, evt.RoomID, evt.Sender, v.ap).
			Scan(&count); err != nil {
			panic(err)
		}

		if count != 0 {
			t.Error("Subscription not removed")
		}
	}
}

// TODO: Test when user has subbed to p312Pkgs.foo, and does `unsub *.foo`
// currently, it prints out an error about not being subbed to e.g. p313Pkgs.foo
func TestSubUnsub(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	h = handlers{
		logFetcher: func(string) (string, bool) {
			return "1999", false
		},
		sender: testSender,
	}

	addPackages(
		"bar",
		"python31Packages.bar",
		"python32Packages.bar",
		"haskellPackages.bar",
	)

	patterns := []struct {
		pattern string
		matches int
	}{
		{
			pattern: "python3?Packages.bar",
			matches: 2,
		},
		{
			pattern: "*.bar",
			matches: 3,
		},
	}

	var count int
	for _, p := range patterns {
		t.Run(p.pattern, func(t *testing.T) {

			t.Run("subscribe", func(t *testing.T) {
				subscribe(p.pattern)

				if err := db.QueryRow(`
        SELECT COUNT(*)
        FROM subscriptions
        WHERE attr_path GLOB ?`, p.pattern).
					Scan(&count); err != nil {
					panic(err)
				}

				if count != p.matches {
					t.Errorf("Not enough matches for %s: %v", p.pattern, count)
				}
			})

			t.Run("unsubscribe", func(t *testing.T) {
				unsubscribe(p.pattern)

				if err := db.QueryRow(`
        SELECT COUNT(*)
        FROM subscriptions
        WHERE attr_path GLOB ?`, p.pattern).
					Scan(&count); err != nil {
					panic(err)
				}

				if count != 0 {
					t.Errorf("Leftover subscriptions for %s: %v", p.pattern, count)
				}
			})

		})
	}

	for _, pattern := range []string{
		"*",
		"**",
		"?",
		"python3Packages.*",
	} {
		t.Run("spammy queries", func(t *testing.T) {
			var before, after int

			if err := db.QueryRow(`SELECT COUNT(*) FROM subscriptions`, pattern).Scan(&before); err != nil {
				panic(err)
			}

			subscribe(pattern)

			if err := db.QueryRow(`SELECT COUNT(*) FROM subscriptions`, pattern).Scan(&after); err != nil {
				panic(err)
			}

			if before != after {
				t.Errorf("Should not have subscribed for query %s: %v, %v", pattern, before, after)
			}
		})
	}
}

func TestOverlapping(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	h = handlers{
		logFetcher: func(string) (string, bool) {
			return "1999", false
		},
		sender: testSender,
	}

	aps := []string{
		"python31Packages.foo",
		"python32Packages.foo",
		"python33Packages.foo",
	}

	addPackages(aps...)
	subscribe(aps[0])

	pattern := "python*.foo"
	subscribe(pattern)

	var count int
	if err := db.QueryRow(`
        SELECT COUNT(*)
        FROM subscriptions
        WHERE attr_path GLOB ?`, pattern).
		Scan(&count); err != nil {
		panic(err)
	}

	if count != len(aps) {
		t.Errorf("Not enough matches for %s: %v", pattern, count)
	}
}

func TestCheckIfSubExists(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	addPackages("foo")

	exists, err := checkIfSubExists("foo", evt.RoomID.String())
	if err != nil {
		panic(err)
	} else if exists {
		t.Errorf("should not exist")
	}

	subscribe("foo")

	exists, err = checkIfSubExists("foo", evt.RoomID.String())
	if err != nil {
		panic(err)
	} else if !exists {
		t.Errorf("should exist")
	}
}

func fillEventContent(evt *event.Event, body string) {
	evt.Content = event.Content{
		Parsed: &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    body,
		},
	}
}

func addPackages(aps ...string) {
	for _, ap := range aps {
		if _, err := db.Exec("INSERT INTO packages(attr_path) VALUES (?)", ap); err != nil {
			panic(err)
		}
	}
}

func subscribe(ap string) {
	fillEventContent(evt, fmt.Sprintf("sub %s", ap))
	handleMessage(ctx, evt)
}

func unsubscribe(ap string) {
	fillEventContent(evt, fmt.Sprintf("unsub %s", ap))
	handleMessage(ctx, evt)
}
