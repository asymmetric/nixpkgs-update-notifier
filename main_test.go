package main

import (
	"testing"
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
	ss := []string{
		"sub foo",
		"sub foo.bar",
		"sub fooPackages.bar_baz",
		"sub fooPackages.bar-baz",
		"unsub foo",
		"sub f?o",
		"sub *.foo",
		"unsub fo?",
		"unsub foo*.foo",
		"unsub *",
		"unsub fooPackages.*",
	}

	for _, s := range ss {
		if regexes.subscribe.FindString(s) == "" {
			t.Errorf("should have matched: %s", s)
		}
	}
}

func TestDangerousRegexp(t *testing.T) {
	ss := []string{
		"sub *",
		"sub **",
		"sub *?",
		"sub ?",
		"sub ??",
		"sub pythonPackages.*",
	}

	for _, s := range ss {
		if regexes.dangerous.FindString(s) == "" {
			t.Errorf("should have matched: %s", s)
		}
	}
}

func TestFollowRegexp(t *testing.T) {
	ss := []string{
		"follow *",
		"unfollow *",
	}

	for _, s := range ss {
		if regexes.dangerous.FindString(s) != "" {
			t.Errorf("should not have matched: %s", s)
		}
	}
}

func TestSubscribeSetsLastVisited(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}
	today := "2000-01-01"
	h = handlers{
		logFetcher: func(string) (string, bool) {
			return today, false
		},
		sender: testSender,
	}

	if _, err := clients.db.Exec("INSERT INTO packages(attr_path) VALUES (?)", "foo"); err != nil {
		panic(err)
	}
	if _, err := clients.db.Exec("INSERT INTO packages(attr_path, last_visited) VALUES (?, ?)", "bar", "1970-01-01"); err != nil {
		panic(err)
	}

	var exists bool

	subscribe("foo")
	subscribe("bar")

	if err := clients.db.QueryRow("SELECT EXISTS (SELECT 1 FROM packages WHERE last_visited <> ?)", today).Scan(&exists); err != nil {
		panic(err)
	}

	if exists {
		t.Error("last_visited should have been set")
	}
}
