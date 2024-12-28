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
		if _, err := db.Exec("INSERT INTO packages(attr_path) VALUES (?)", v.ap); err != nil {
			panic(err)
		}

		evt.Content = event.Content{
			Parsed: &event.MessageEventContent{
				MsgType: event.MsgText,
				Body:    fmt.Sprintf("sub %s", v.ap),
			},
		}
		handleMessage(ctx, evt)

		if err := db.QueryRow(`
      SELECT COUNT(*)
      FROM subscriptions
      WHERE roomid = ?
        AND mxid = ?
        AND attr_path = ?`, evt.RoomID, evt.Sender, v.ap).
			Scan(&count); err != nil {
			panic(err)
		}

		if count != 1 {
			t.Error("Subscription not found")
		}

		var lv string
		var hasErr bool
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

		evt.Content = event.Content{
			Parsed: &event.MessageEventContent{
				MsgType: event.MsgText,
				Body:    fmt.Sprintf("unsub %s", v.ap),
			},
		}
		handleMessage(ctx, evt)

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
