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

func TestNormalizeAttrPath(t *testing.T) {
	tt := []struct {
		input string
		want  string
	}{
		// beam: versioned -> beam26Packages
		{"beam27Packages.erlang", "beam26Packages.erlang"},
		{"beam28Packages.elixir", "beam26Packages.elixir"},
		// linux kernel: versioned generic -> linuxPackages
		{"linuxKernel.packages.linux_6_6.foo", "linuxPackages.foo"},
		{"linuxKernel.packages.linux_6_12.bar", "linuxPackages.bar"},
		// lua: versioned -> lua51Packages
		{"lua52Packages.foo", "lua51Packages.foo"},
		{"lua53Packages.foo", "lua51Packages.foo"},
		{"luajitPackages.foo", "lua51Packages.foo"},
		// llvm: versioned -> unversioned
		{"llvmPackages_17.clang", "llvmPackages.clang"},
		{"llvmPackages_18.lld", "llvmPackages.lld"},
		// ocaml: _latest -> unversioned
		{"ocamlPackages_latest.foo", "ocamlPackages.foo"},
		// php extensions: versioned -> unversioned
		{"php82Extensions.foo", "phpExtensions.foo"},
		{"php83Extensions.bar", "phpExtensions.bar"},
		// php packages: versioned -> unversioned
		{"php82Packages.foo", "phpPackages.foo"},
		{"php83Packages.bar", "phpPackages.bar"},
		// postgresql: versioned -> unversioned
		{"postgresql15Packages.foo", "postgresqlPackages.foo"},
		// python3: versioned -> unversioned
		{"python312Packages.foo", "python3Packages.foo"},
		{"python313Packages.bar", "python3Packages.bar"},
		// no match: unchanged
		{"haskellPackages.foo", "haskellPackages.foo"},
		{"python2Packages.foo", "python2Packages.foo"},
		{"btrbk", "btrbk"},
	}

	for _, tc := range tt {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeAttrPath(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeAttrPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
