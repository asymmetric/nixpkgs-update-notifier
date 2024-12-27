package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestErrRegexp(t *testing.T) {
	positives := []string{
		// https://nixpkgs-update-logs.nix-community.org/grafana-dash-n-grab/2024-09-13.log
		"error: attribute 'originalSrc' in selection path 'grafana-dash-n-grab.originalSrc' not found",
		// https://nixpkgs-update-logs.nix-community.org/babashka/2024-09-13.log
		"Received ExitFailure 1 when running",
		// https://nixpkgs-update-logs.nix-community.org/php83Extensions.ssh2/2024-09-19.log
		"The update script for php-ssh2-1.3.1 failed with exit code 1",
		// https://nixpkgs-update-logs.nix-community.org/kyverno-chainsaw/2024-09-19.log
		"error: builder for '/nix/store/gxvr06ifbpw342msbqbjd89fv8572kdr-kyverno-chainsaw-0.2.10-go-modules.drv' failed with exit code 1;",
	}
	for _, s := range positives {
		if erroRE.FindString(s) == "" {
			t.Errorf("should have matched: %s", s)
		}
	}

	falsePositives := []string{
		// https://nixpkgs-update-logs.nix-community.org/glibc/2024-08-05.log
		`"configure: error: Pthreads are required to build libgomp"`,
		// https://nixpkgs-update-logs.nix-community.org/rPackages.MBESS/2023-12-24.log
		`"flock ${xvfb-run} xvfb-run -a -e xvfb-error R"`,
		// https://nixpkgs-update-logs.nix-community.org/testlib/2024-09-13.log
		`copying nixpkgs_review/errors.py -> build/lib/nixpkgs_review`,
	}

	for _, s := range falsePositives {
		if erroRE.FindString(s) != "" {
			t.Errorf("should not have matched: %s", s)
		}
	}
}

func TestSubUnsubRegexp(t *testing.T) {
	positives := []string{
		"sub foo",
		"unsub foo",
	}

	for _, s := range positives {
		if subUnsubRE.FindString(s) == "" {
			t.Errorf("should have matched: %s", s)
		}
	}

	falsePositives := []string{
		`sub -foo`,
		`unsub -foo`,
	}

	for _, s := range falsePositives {
		if subUnsubRE.FindString(s) != "" {
			t.Errorf("should not have matched: %s", s)
		}
	}
}

func TestSub(t *testing.T) {
	ctx := context.Background()

	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	// setup
	client, _ = mautrix.NewClient("http://localhost", "", "")

	rid := id.RoomID("test-room")
	sender := id.UserID("test-sender")
	evt := &event.Event{
		RoomID: rid,
		Sender: sender,
	}

	var count int
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

	// TODO move somewhere else
	slog.SetLogLoggerLevel(slog.LevelError)

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
        AND attr_path = ?`, rid, sender, v.ap).
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

func newFakeEvent(evtType event.Type, parsed interface{}) *event.Event {
	data, err := json.Marshal(parsed)
	if err != nil {
		panic(err)
	}
	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	if err != nil {
		panic(err)
	}
	content := event.Content{
		VeryRaw: data,
		Raw:     raw,
		Parsed:  parsed,
	}
	return &event.Event{
		Sender:    "@foo:bar.net",
		Type:      evtType,
		Timestamp: 1523791120,
		ID:        "$123:bar.net",
		RoomID:    "!fakeroom:bar.net",
		Content:   content,
	}
}
