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

func init() {
	slog.SetLogLoggerLevel(slog.LevelError)
}

// TODO: test non-existent package
func TestSub(t *testing.T) {
	ctx := context.Background()

	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	// setup barebones matrix stuff
	client, _ = mautrix.NewClient("http://localhost", "", "")

	evt := &event.Event{
		RoomID: id.RoomID("test-room"),
		Sender: id.UserID("test-sender"),
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

func testSender(text string, _ id.RoomID) (*mautrix.RespSendEvent, error) {
	return nil, nil
}
