package pg_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
)

var widgets = fstest.MapFS{
	"0001_widgets.sql": {Data: []byte(`
		CREATE TABLE widgets (id serial PRIMARY KEY, name text NOT NULL UNIQUE);
		INSERT INTO widgets (name) VALUES ('seed');
	`)},
}

func TestMigrateIsIdempotentAndDetectsDrift(t *testing.T) {
	db := pgtest.New(t, nil)
	ctx := context.Background()

	ran, err := pg.Migrate(ctx, db, widgets)
	if err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "0001_widgets" {
		t.Errorf("first run applied %v", ran)
	}
	ran, err = pg.Migrate(ctx, db, widgets)
	if err != nil || len(ran) != 0 {
		t.Errorf("second run: ran=%v err=%v; want nothing applied", ran, err)
	}

	var n int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM widgets`).Scan(&n); err != nil || n != 1 {
		t.Errorf("widgets rows = %d, err %v; the seed must have run exactly once", n, err)
	}

	renamed := fstest.MapFS{"0001_gadgets.sql": widgets["0001_widgets.sql"]}
	if _, err := pg.Migrate(ctx, db, renamed); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Errorf("renamed migration should be refused, got %v", err)
	}
	if _, err := pg.Migrate(ctx, db, fstest.MapFS{}); err == nil || !strings.Contains(err.Error(), "older binary") {
		t.Errorf("unknown applied version should be refused, got %v", err)
	}

	// A failing migration leaves no trace: neither its DDL nor its row.
	broken := fstest.MapFS{
		"0001_widgets.sql": widgets["0001_widgets.sql"],
		"0002_broken.sql":  {Data: []byte(`CREATE TABLE gizmos (id int); INSERT INTO widgets (name) VALUES ('seed');`)},
	}
	if _, err := pg.Migrate(ctx, db, broken); err == nil {
		t.Fatal("broken migration should fail")
	}
	var exists bool
	_ = db.Q(ctx).QueryRow(ctx, `SELECT to_regclass('gizmos') IS NOT NULL`).Scan(&exists)
	if exists {
		t.Error("gizmos exists after a failed migration — the file did not run in one transaction")
	}
	_ = db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = 2)`).Scan(&exists)
	if exists {
		t.Error("version 2 recorded although it failed")
	}
}

func TestWithTxCommitsRollsBackAndJoins(t *testing.T) {
	db := pgtest.New(t, widgets)
	ctx := context.Background()
	count := func() int {
		var n int
		if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM widgets`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Commit.
	err := db.WithTx(ctx, func(ctx context.Context) error {
		if !pg.InTx(ctx) {
			t.Error("InTx false inside WithTx")
		}
		_, err := db.Q(ctx).Exec(ctx, `INSERT INTO widgets (name) VALUES ('a')`)
		return err
	})
	if err != nil || count() != 2 {
		t.Fatalf("commit: err=%v count=%d", err, count())
	}

	// Rollback on error, and the error comes back unchanged.
	sentinel := errors.New("nope")
	err = db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO widgets (name) VALUES ('b')`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || count() != 2 {
		t.Fatalf("rollback: err=%v count=%d", err, count())
	}

	// Rollback on panic, panic re-raised.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		_ = db.WithTx(ctx, func(ctx context.Context) error {
			_, _ = db.Q(ctx).Exec(ctx, `INSERT INTO widgets (name) VALUES ('c')`)
			panic("boom")
		})
	}()
	if count() != 2 {
		t.Fatalf("panic: count=%d", count())
	}

	// Nested call joins the outer transaction: inner failure rolls back both.
	err = db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO widgets (name) VALUES ('outer')`); err != nil {
			return err
		}
		return db.WithTx(ctx, func(ctx context.Context) error {
			_, err := db.Q(ctx).Exec(ctx, `INSERT INTO widgets (name) VALUES ('seed')`) // unique violation
			return err
		})
	})
	if !pg.IsUniqueViolation(err) {
		t.Errorf("expected unique violation, got %v", err)
	}
	if count() != 2 {
		t.Errorf("nested: outer insert survived a failed inner call; count=%d", count())
	}
	if pg.InTx(ctx) {
		t.Error("InTx true on a plain context")
	}
}
