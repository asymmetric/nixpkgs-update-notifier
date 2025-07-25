package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
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

	clients.matrix, _ = mautrix.NewClient("http://localhost", "", "")
}

// TODO: test non-existent package
func TestSub(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	tt := []struct {
		ap string
		lv string
	}{
		{
			ap: "foo",
			lv: "1970-01-01",
		},
		{
			ap: "python312Packages.foo",
			lv: "1980-01-01",
		},
	}

	var count int
	var lv string
	for _, v := range tt {
		h = handlers{
			dateFetcher: func(string) (string, error) {
				return v.lv, nil
			},
			sender: testSender,
		}
		addPackages(v.ap)
		sub(v.ap)

		if err := clients.db.QueryRow(`
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

		if err := clients.db.QueryRow(`SELECT last_visited FROM packages WHERE attr_path = ?`, v.ap).Scan(&lv); err != nil {
			panic(err)
		}

		if lv != v.lv {
			t.Errorf("Wrong last_visited date: %s", lv)
		}
	}
}

func TestSubDuplicates(t *testing.T) {
	stubJSONBlob()
	h = handlers{
		dateFetcher: func(string) (string, error) {
			return "1999", nil
		},
		sender: testSender,
	}

	t.Run("double subscribe", func(t *testing.T) {
		if err := setupDB(ctx, ":memory:"); err != nil {
			panic(err)
		}

		p := "foo"
		addPackages(p)

		sub(p)
		sub(p)

		var count int
		if err := clients.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE attr_path = ?`, p).Scan(&count); err != nil {
			panic(err)
		}

		if expected := 1; count != expected {
			t.Errorf("expected: %d; got: %d", expected, count)
		}
	})

	t.Run("subscribe and follow", func(t *testing.T) {
		if err := setupDB(ctx, ":memory:"); err != nil {
			panic(err)
		}

		p := "btrbk"
		h := "asymmetric"
		addPackages(p)

		sub(p)
		fol(h)

		var count int
		if err := clients.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE attr_path = ?`, p).Scan(&count); err != nil {
			panic(err)
		}

		if expected := 1; count != expected {
			t.Errorf("expected: %d; got: %d", expected, count)
		}
	})
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
			dateFetcher: func(string) (string, error) {
				return v.lv, nil
			},
			sender: testSender,
		}
		addPackages(v.ap)
		sub(v.ap)

		unsub(v.ap)

		if err := clients.db.QueryRow(`
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
		dateFetcher: func(string) (string, error) {
			return "1999", nil
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
				sub(p.pattern)

				if err := clients.db.QueryRow(`
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
				unsub(p.pattern)

				if err := clients.db.QueryRow(`
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

			if err := clients.db.QueryRow(`SELECT COUNT(*) FROM subscriptions`, pattern).Scan(&before); err != nil {
				panic(err)
			}

			sub(pattern)

			if err := clients.db.QueryRow(`SELECT COUNT(*) FROM subscriptions`, pattern).Scan(&after); err != nil {
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
		dateFetcher: func(string) (string, error) {
			return "1999", nil
		},
		sender: testSender,
	}

	aps := []string{
		"python31Packages.foo",
		"python32Packages.foo",
		"python33Packages.foo",
	}

	addPackages(aps...)
	sub(aps[0])

	pattern := "python*.foo"
	sub(pattern)

	var count int
	if err := clients.db.QueryRow(`
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

func TestSubscribeSetsLastVisited(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}
	today := "2000-01-01"
	h = handlers{
		dateFetcher: func(string) (string, error) {
			return today, nil
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

	sub("foo")
	sub("bar")

	if err := clients.db.QueryRow("SELECT EXISTS (SELECT 1 FROM packages WHERE last_visited <> ?)", today).Scan(&exists); err != nil {
		panic(err)
	}

	if exists {
		t.Error("last_visited should have been set")
	}
}

func TestCheckIfSubExists(t *testing.T) {
	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	ap := "foo"
	addPackages(ap)

	t.Run("should not exist", func(t *testing.T) {
		exists, err := checkIfSubExists(ap, evt.RoomID.String())
		if err != nil {
			panic(err)
		} else if exists {
			t.Errorf("should not exist")
		}
	})

	t.Run("should exist", func(t *testing.T) {
		sub(ap)

		exists, err := checkIfSubExists(ap, evt.RoomID.String())
		if err != nil {
			panic(err)
		} else if !exists {
			t.Error("should exist")
		}
	})
}

func TestFollow(t *testing.T) {
	stubJSONBlob()
	h = handlers{
		dateFetcher: func(string) (string, error) {
			return "1999", nil
		},
		sender: testSender,
	}

	ps := []string{
		"btrbk",
		"btrfs-list",
		"diceware",
		"python312Packages.diceware",
	}

	t.Run("last_visited set", func(t *testing.T) {
		if err := setupDB(ctx, ":memory:"); err != nil {
			panic(err)
		}

		for _, p := range ps {
			if _, err := clients.db.Exec("INSERT INTO packages(attr_path, last_visited) VALUES (?, ?)", p, "1999"); err != nil {
				panic(err)
			}
		}

		fol("asymmetric")

		var exists bool
		var err error
		for _, p := range ps {
			exists, err = checkIfSubExists(p, evt.RoomID.String())
			if err != nil {
				panic(err)
			}

			if !exists {
				t.Errorf("should be subscribed to %s", p)
			}
		}
	})
	t.Run("last_visited not set", func(t *testing.T) {
		if err := setupDB(ctx, ":memory:"); err != nil {
			panic(err)
		}

		// NOTE: no last_visited
		addPackages(ps...)

		fol("asymmetric")

		var exists bool
		var err error
		aps, err := findPackagesForHandle("asymmetric")
		if err != nil {
			panic(err)
		}

		for _, p := range aps {
			exists, err = checkIfSubExists(p, evt.RoomID.String())
			if err != nil {
				panic(err)
			}

			if !exists {
				t.Errorf("should be subscribed to %s", p)
			}
		}
	})

	t.Run("some packages not tracked by nixpkgs-update", func(t *testing.T) {
		if err := setupDB(ctx, ":memory:"); err != nil {
			panic(err)
		}

		last := ps[len(ps)-1]

		addPackages(ps[:len(ps)-2]...)

		fol("asymmetric")

		if exists, _ := checkIfSubExists(last, evt.RoomID.String()); exists {
			t.Errorf("should not be subscribed to %s", last)
		}
	})

}

func TestUnfollow(t *testing.T) {
	stubJSONBlob()
	h = handlers{
		dateFetcher: func(string) (string, error) {
			return "1999", nil
		},
		sender: testSender,
	}

	if err := setupDB(ctx, ":memory:"); err != nil {
		panic(err)
	}

	mps := []string{
		"btrbk",
		"btrfs-list",
		"diceware",
		"python312Packages.diceware",
	}

	all := append(mps, "foo")

	addPackages(all...)

	sub("foo")

	// First follow...
	fol("asymmetric")

	// Then unfollow
	unfol("asymmetric")

	for _, p := range mps {
		if exists, _ := checkIfSubExists(p, evt.RoomID.String()); exists {
			t.Errorf("should not be subscribed to %s", p)
		}
	}

	if exists, _ := checkIfSubExists("foo", evt.RoomID.String()); !exists {
		t.Errorf("should be subscribed to %s", "foo")
	}
}

func TestFindPackagesForHandle(t *testing.T) {
	stubJSONBlob()

	t.Run("existing handle", func(t *testing.T) {
		if err := setupDB(ctx, ":memory:"); err != nil {
			panic(err)
		}

		expected := []string{
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
		all := append([]string{"foo", "bar"}, expected...)

		addPackages(all...)

		t.Run("case match", func(t *testing.T) {
			got, err := findPackagesForHandle("asymmetric")
			if err != nil {
				panic(err)
			}

			if !slices.Equal(expected, got) {
				t.Errorf("expected: %v\ngot: %v", expected, got)
			}
		})
		t.Run("case mismatch", func(t *testing.T) {
			got, err := findPackagesForHandle("ASYMMETRIC")
			if err != nil {
				panic(err)
			}

			if !slices.Equal(expected, got) {
				t.Errorf("expected: %v\ngot: %v", expected, got)
			}
		})
	})

	t.Run("non-existing handle", func(t *testing.T) {
		if err := setupDB(ctx, ":memory:"); err != nil {
			panic(err)
		}

		got, err := findPackagesForHandle("foobar")
		if err != nil {
			panic(err)
		}

		expected := []string{}

		if !slices.Equal(expected, got) {
			t.Errorf("expected: %v\ngot: %v", expected, got)
		}
	})

	t.Run("substring package match", func(t *testing.T) {
		if err := setupDB(ctx, ":memory:"); err != nil {
			panic(err)
		}

		expected := []string{
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
		all := append([]string{"valgrind", "valgrind-light"}, expected...)
		addPackages(all...)

		got, err := findPackagesForHandle("asymmetric")
		if err != nil {
			panic(err)
		}

		// the former has maiintaner asymmetric-foo, the latter foo-asymmetric
		for _, s := range []string{"valgrind", "valgrind-light"} {
			if slices.Contains(got, s) {
				t.Errorf("should not have subscribed to %s", s)
			}
		}
	})
}

func fillEventContent(evt *event.Event, body string) {
	evt.Content = event.Content{
		Parsed: &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    body,
		},
	}
}

// TODO add a last_visited argument? what to do when it's irrelevant?
func addPackages(aps ...string) {
	for _, ap := range aps {
		if _, err := clients.db.Exec("INSERT INTO packages(attr_path) VALUES (?)", ap); err != nil {
			panic(err)
		}
	}
}

func sub(ap string) {
	fillEventContent(evt, fmt.Sprintf("sub %s", ap))
	handleMessage(ctx, evt)
}

func unsub(ap string) {
	fillEventContent(evt, fmt.Sprintf("unsub %s", ap))
	handleMessage(ctx, evt)
}

func fol(handle string) {
	fillEventContent(evt, fmt.Sprintf("follow %s", handle))
	handleMessage(ctx, evt)
}

func unfol(handle string) {
	fillEventContent(evt, fmt.Sprintf("unfollow %s", handle))
	handleMessage(ctx, evt)
}

func stubJSONBlob() {
	data, err := os.ReadFile("testdata/packages.json")
	if err != nil {
		panic(err)
	}

	if err := json.Unmarshal(data, &jsblob); err != nil {
		panic(err)
	}
}
