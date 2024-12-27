package main

import (
	"context"
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
	aps := []string{
		"foo",
		"python312Packages.bar",
	}
	for _, ap := range aps {
		if _, err := db.Exec("INSERT OR IGNORE INTO packages(attr_path) VALUES (?)", ap); err != nil {
			panic(err)
		}
	}

	logFetcherFunc = testFetcher
	senderFunc = testSender
	client, _ = mautrix.NewClient("http://localhost", "", "")

	// it should sub
	evt := &event.Event{
		RoomID: "foo",
		Sender: "bar",
	}
	handleSubUnsub("sub foo", evt)

	var mxid string
	if err := db.QueryRow("SELECT mxid FROM subscriptions WHERE attr_path = ?", "foo").Scan(&mxid); err != nil {
		panic(err)
	}

	if mxid != "bar" {
		t.Errorf("Wrong subscriber: %s", mxid)
	}
}

func testFetcher(string) (string, bool) {
	return "foo", false
}

func testSender(text string, _ id.RoomID) (*mautrix.RespSendEvent, error) {
	return nil, nil
}
