package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store/migrations"
	"github.com/tikaydaes/churr/internal/store/postgres"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// A database made before migration 0002 (broad mentions, ruling R64) is
// brought up to date when it is opened, and its rows read as "follows the
// server".
func TestUpgradeFromSchema1(t *testing.T) {
	ctx := context.Background()
	url := storetest.PostgresURL(t)
	ms, err := migrations.List(migrations.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	const conv = "0193a0c7-0000-7000-8000-000000000001"
	for _, q := range []string{
		ms[0].SQL,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (1, 1)`,
		`INSERT INTO conversations (id, kind, private, name, created_at) VALUES ('` + conv + `', 'channel', false, 'old', 1)`,
	} {
		if _, err := old.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	s, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("opening a database at schema 1: %v", err)
	}
	defer s.Close()
	c, err := s.Conversations().Get(ctx, conv)
	if err != nil {
		t.Fatal(err)
	}
	if c.BroadMentions != model.BroadServer {
		t.Fatalf("a channel from before the migration reads %q, want %q", c.BroadMentions, model.BroadServer)
	}
	acc := model.Account{Username: "ann", DisplayName: "Ann", Role: model.RoleOwner}
	if _, err := s.Accounts().Create(ctx, &acc); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Members().AddDirect(ctx, conv, acc.ID, model.LevelMember, model.Now()); err != nil {
		t.Fatal(err)
	}
	if m, _, err := s.Members().Get(ctx, conv, acc.ID); err != nil || m.IgnoreBroad || m.MayPost {
		t.Fatalf("a new member row: %+v, %v", m, err)
	}
	// Migration 0004 (ruling R66): an old channel follows the server and
	// is no announcement channel.
	if c.LazyDelivery != model.LazyServer || c.Announcement || c.AnnouncementReplies {
		t.Fatalf("a channel from before migration 0004 reads %+v", c)
	}
}
