package regexes

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
		if Error().FindString(s) == "" {
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
		if Error().FindString(s) != "" {
			t.Errorf("should not have matched: %s", s)
		}
	}
}

func TestSubUnsubRegexp(t *testing.T) {
	t.Run("should match", func(t *testing.T) {
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
			// Case insensitive tests
			"Sub foo",
			"SUB foo",
			"sUb foo",
			"Unsub foo",
			"UNSUB foo",
			"uNsUb foo",
			"UnSuB foo",
		}

		for _, s := range ss {
			if Subscribe().FindString(s) == "" {
				t.Errorf("should have matched: %s", s)
			}
		}
	})

	t.Run("should not match", func(t *testing.T) {
		ss := []string{
			"subx foo",
			"unsuby foo",
		}

		for _, s := range ss {
			if Subscribe().FindString(s) != "" {
				t.Errorf("should not have matched: %s", s)
			}
		}
	})
}

func TestDangerousRegexp(t *testing.T) {
	ss := []string{
		"sub *",
		"sub **",
		"sub *?",
		"sub ?",
		"sub ??",
		"sub pythonPackages.*",
		// Case insensitive tests
		"Sub *",
		"SUB *",
		"sUb pythonPackages.*",
		"Sub pythonPackages.*",
	}

	for _, s := range ss {
		if Dangerous().FindString(s) == "" {
			t.Errorf("should have matched: %s", s)
		}
	}
}

func TestFollowRegexp(t *testing.T) {
	t.Run("should match", func(t *testing.T) {
		ss := []string{
			"follow foo",
			"unfollow bar",
			// Case insensitive tests
			"Follow foo",
			"FOLLOW foo",
			"fOlLoW foo",
			"Unfollow bar",
			"UNFOLLOW bar",
			"uNfOlLoW bar",
			"UnFoLlOw bar",
		}
		for _, s := range ss {
			if !Follow().MatchString(s) {
				t.Errorf("should have matched: %s", s)
			}
		}
	})

	t.Run("should not match", func(t *testing.T) {
		ss := []string{
			"follow",
			"unfollow",
			"follow *",
			"unfollow *",
			"follow ?",
			"unfollow ?",
			"followx foo",
			"unfollowy bar",
			"follows foo",
			"unfollows bar",
		}

		for _, s := range ss {
			if Follow().MatchString(s) {
				t.Errorf("should not have matched: %s", s)
			}
		}
	})
}
