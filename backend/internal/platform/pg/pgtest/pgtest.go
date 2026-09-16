// Package pgtest gives a test a real Postgres, migrated, in a schema of its
// own that disappears when the test ends. Store tests run against the real
// engine because §6.9.7 and N49/N50 are explicit that the interesting bugs
// (pg_trgm parity, fuzzystrmatch limits, unique-constraint semantics) do not
// reproduce on a fake.
//
// Set TEST_DATABASE_URL to a throwaway database; without it every test that
// calls New is skipped, so `go test ./...` stays green on a laptop with no
// Postgres. CI must set it (§6.9.7: "on real Postgres in CI").
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// EnvURL is the environment variable New reads.
const EnvURL = "TEST_DATABASE_URL"

// New returns a DB whose every connection has search_path pinned to a fresh
// schema, with migrations from fsys applied. Pass nil to skip migrations.
// The schema is dropped with CASCADE in t.Cleanup.
func New(t testing.TB, migrations fs.FS) *pg.DB {
	t.Helper()
	url := os.Getenv(EnvURL)
	if url == "" {
		t.Skipf("%s not set; skipping test that needs Postgres", EnvURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var b [4]byte
	_, _ = rand.Read(b[:])
	schema := "t_" + hex.EncodeToString(b[:])

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("pgtest: parse %s: %v", EnvURL, err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, fmt.Sprintf(`SET search_path TO %s`, pgx.Identifier{schema}.Sanitize()))
		return err
	}

	// Create the schema on an admin connection before the pool exists, so
	// AfterConnect never races with it.
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("pgtest: create schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = admin.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		_ = admin.Close(ctx)
	})

	db, err := pg.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("pgtest: open: %v", err)
	}
	t.Cleanup(db.Close)

	if migrations != nil {
		if _, err := pg.Migrate(ctx, db, migrations); err != nil {
			t.Fatalf("pgtest: migrate: %v", err)
		}
	}
	return db
}
