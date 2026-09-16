package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/asymmetric/nixpkgs-update-notifier/regexes"
)

func mustBuildIndexFromFixture(t *testing.T) map[string][]string {
	t.Helper()

	f, err := os.Open("testdata/packages.json")
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer f.Close()

	idx, err := buildMaintainerIndex(f)
	if err != nil {
		t.Fatalf("buildMaintainerIndex: %v", err)
	}

	return idx
}

func TestBuildMaintainerIndex_Fixture(t *testing.T) {
	idx := mustBuildIndexFromFixture(t)

	t.Run("asymmetric", func(t *testing.T) {
		// Attr paths that list "asymmetric" as a maintainer, derived directly
		// from the fixture (grep '"github": "asymmetric"').
		rawAttrPaths := []string{
			"asc-key-to-qr-code-gif",
			"btrbk",
			"btrfs-list",
			"diceware",
			"evmdis",
			"ledger-udev-rules",
			"python312Packages.diceware",
			"python313Packages.diceware",
			"siji",
			"ssb-patchwork",
		}

		expectedSet := make(map[string]struct{})
		for _, ap := range rawAttrPaths {
			expectedSet[regexes.NormalizeAttrPath(ap)] = struct{}{}
		}
		expected := make([]string, 0, len(expectedSet))
		for ap := range expectedSet {
			expected = append(expected, ap)
		}
		slices.Sort(expected)

		got := idx["asymmetric"]
		if !slices.Equal(got, expected) {
			t.Errorf("index[\"asymmetric\"] = %v; want %v", got, expected)
		}
	})

	t.Run("no empty-string key", func(t *testing.T) {
		if _, ok := idx[""]; ok {
			t.Errorf("index contains an empty-string key: %v", idx[""])
		}
	})

	t.Run("edolstra excludes maintainers without a usable github handle", func(t *testing.T) {
		got := idx["edolstra"]

		for _, unwanted := range []string{"nixpkgs-lint", "nix-generate-from-cpan", "nixStatic"} {
			if slices.Contains(got, regexes.NormalizeAttrPath(unwanted)) {
				t.Errorf("index[\"edolstra\"] unexpectedly contains %q: %v", unwanted, got)
			}
		}
	})

	t.Run("handles are lowercased", func(t *testing.T) {
		for _, want := range []struct {
			original string
			lower    string
		}{
			{"NotAShelf", "notashelf"},
			{"Artturin", "artturin"},
		} {
			if _, ok := idx[want.lower]; !ok {
				t.Errorf("expected index to contain key %q", want.lower)
			}
		}

		for handle := range idx {
			if handle != strings.ToLower(handle) {
				t.Errorf("index contains a non-lowercase key: %q", handle)
			}
		}
	})
}

func TestBuildMaintainerIndex_Inline(t *testing.T) {
	t.Run("null maintainers", func(t *testing.T) {
		idx, err := buildMaintainerIndex(strings.NewReader(`{"packages": {"foo": {"meta": {"maintainers": null}}}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(idx) != 0 {
			t.Errorf("expected empty index, got %v", idx)
		}
	})

	t.Run("extra top-level keys before and after packages are skipped", func(t *testing.T) {
		idx, err := buildMaintainerIndex(strings.NewReader(
			`{"version": 2, "packages": {"foo": {"meta": {"maintainers": [{"github": "bar"}]}}}, "extra": [1,2]}`,
		))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"foo"}
		if got := idx["bar"]; !slices.Equal(got, want) {
			t.Errorf("index[\"bar\"] = %v; want %v", got, want)
		}
	})

	t.Run("empty packages object", func(t *testing.T) {
		idx, err := buildMaintainerIndex(strings.NewReader(`{"packages": {}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(idx) != 0 {
			t.Errorf("expected empty index, got %v", idx)
		}
	})

	t.Run("malformed JSON returns an error, not a panic", func(t *testing.T) {
		_, err := buildMaintainerIndex(strings.NewReader(`{"packages": {`))
		if err == nil {
			t.Error("expected an error for malformed JSON, got nil")
		}
	})

	t.Run("duplicate maintainer in one package yields one entry", func(t *testing.T) {
		idx, err := buildMaintainerIndex(strings.NewReader(
			`{"packages": {"foo": {"meta": {"maintainers": [{"github": "bar"}, {"github": "bar"}]}}}}`,
		))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"foo"}
		if got := idx["bar"]; !slices.Equal(got, want) {
			t.Errorf("index[\"bar\"] = %v; want %v", got, want)
		}
	})

	// Regression test for the encoding/json merge footgun: reusing a struct
	// variable across loop iterations would leak maintainers from one package
	// into the next, since json.Decode merges into existing values instead of
	// resetting them.
	t.Run("maintainers do not leak between packages", func(t *testing.T) {
		idx, err := buildMaintainerIndex(strings.NewReader(
			`{"packages": {
				"foo": {"meta": {"maintainers": [{"github": "bar"}]}},
				"baz": {"meta": {}}
			}}`,
		))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"foo"}
		if got := idx["bar"]; !slices.Equal(got, want) {
			t.Errorf("index[\"bar\"] = %v; want %v (baz leaked in)", got, want)
		}
	})
}
