package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
)

func setupMatrix() *mautrix.Client {
	client, err := mautrix.NewClient(*matrixHomeserver, "", "")
	if err != nil {
		panic(err)
	}

	envVar := "NIXPKGS_UPDATE_NOTIFIER_PASSWORD"
	pwd, found := os.LookupEnv(envVar)
	if !found {
		fatal(fmt.Errorf("could not read password env var %s", envVar))
	}

	_, err = client.Login(context.TODO(), &mautrix.ReqLogin{
		Type:               mautrix.AuthTypePassword,
		Identifier:         mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: *matrixUsername},
		Password:           pwd,
		StoreCredentials:   true,
		StoreHomeserverURL: true,
	})
	if err != nil {
		panic(err)
	}

	syncer := mautrix.NewDefaultSyncer()
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		m := evt.Content.AsMember()
		switch m.Membership {
		case event.MembershipInvite:
			// TODO: only join if IsDirect is true, i.e. it's a DM
			if _, err := client.JoinRoomByID(ctx, evt.RoomID); err != nil {
				slog.Error(err.Error())

				return
			}

			slog.Debug("joining room", "id", evt.RoomID)
		case event.MembershipLeave:
			// remove subscription, then leave room
			if _, err := clients.db.Exec("DELETE FROM subscriptions WHERE roomid = ?", evt.RoomID); err != nil {
				panic(err)
			}

			if _, err := client.LeaveRoom(ctx, evt.RoomID); err != nil {
				slog.Error(err.Error())

				return
			}

			slog.Debug("leaving room", "id", evt.RoomID)
		default:
			slog.Debug("received unhandled event", "type", event.StateMember, "content", evt.Content)
		}
	})

	subEventID := "io.github.nixpkgs-update-notifier.subscription"

	syncer.OnEventType(event.EventMessage, handleMessage)

	// NOTE: changing this will re-play all received Matrix messages
	syncer.FilterJSON = &mautrix.Filter{
		AccountData: mautrix.FilterPart{
			Limit: 20,
			NotTypes: []event.Type{
				event.NewEventType(subEventID),
			},
		},
	}
	client.Syncer = syncer
	client.Store = mautrix.NewAccountDataStore(subEventID, client)

	return client
}

func setupDB(ctx context.Context, path string) (err error) {
	clients.db, err = sql.Open("sqlite3", fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=true", path))
	if err != nil {
		return
	}

	if err = clients.db.PingContext(ctx); err != nil {
		return
	}

	if _, err = clients.db.ExecContext(ctx, ddl); err != nil {
		return
	}

	return
}

func setupLogger() {
	opts := &slog.HandlerOptions{}

	if *debug {
		opts.Level = slog.LevelDebug
	}

	h := slog.NewTextHandler(os.Stderr, opts)
	slog.SetDefault(slog.New(h))
}
