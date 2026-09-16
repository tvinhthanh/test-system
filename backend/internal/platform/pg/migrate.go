package pg

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Migration is one numbered SQL file. Files are named NNNN_name.sql; the
// number is the version, the rest is a label recorded next to it.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

var migrationFileRe = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// ParseMigrations reads every NNNN_name.sql at the root of fsys and returns
// them in version order. It refuses duplicates and gaps: a gap is almost
// always two branches that each added "the next number", and the safe answer
// is to stop before either runs.
func ParseMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: read dir: %w", err)
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migrate: %q does not match NNNN_name.sql", e.Name())
		}
		v, _ := strconv.Atoi(m[1])
		b, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("migrate: read %s: %w", e.Name(), err)
		}
		if strings.TrimSpace(string(b)) == "" {
			return nil, fmt.Errorf("migrate: %s is empty", e.Name())
		}
		out = append(out, Migration{Version: v, Name: m[2], SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if want := i + 1; m.Version != want {
			if i > 0 && out[i-1].Version == m.Version {
				return nil, fmt.Errorf("migrate: version %04d appears twice (%s, %s)", m.Version, out[i-1].Name, m.Name)
			}
			return nil, fmt.Errorf("migrate: versions must be contiguous from 0001; got %04d where %04d was expected", m.Version, want)
		}
	}
	return out, nil
}

// migrateLockID is the advisory lock key two concurrent migrate processes
// contend on. Arbitrary, but must never change.
const migrateLockID = 0x41545021 // "ATP!"

// Migrate applies every migration in fsys that schema_migrations does not
// yet record, each in its own transaction, and returns the names it applied.
// It takes a session-level advisory lock so two replicas starting at once
// cannot both run 0007.
//
// Drift is an error, not a warning: a recorded version whose name differs
// from the file, or a recorded version the binary does not know about, means
// this binary and this database disagree about history, and neither
// "skip it" nor "run it anyway" is safe (§8.10).
func Migrate(ctx context.Context, db *DB, fsys fs.FS) ([]string, error) {
	migrations, err := ParseMigrations(fsys)
	if err != nil {
		return nil, err
	}

	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, fmt.Errorf("migrate: ensure schema_migrations: %w", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		return nil, fmt.Errorf("migrate: lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrateLockID) }()

	applied := map[int]string{}
	rows, err := conn.Query(ctx, `SELECT version, name FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		var n string
		if err := rows.Scan(&v, &n); err != nil {
			rows.Close()
			return nil, err
		}
		applied[v] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	known := map[int]bool{}
	for _, m := range migrations {
		known[m.Version] = true
		if name, ok := applied[m.Version]; ok && name != m.Name {
			return nil, fmt.Errorf("migrate: version %04d is recorded as %q but the file says %q — history has drifted", m.Version, name, m.Name)
		}
	}
	for v, name := range applied {
		if !known[v] {
			return nil, fmt.Errorf("migrate: database has %04d_%s applied but this binary does not know it — older binary against newer schema", v, name)
		}
	}

	var ran []string
	for _, m := range migrations {
		if _, done := applied[m.Version]; done {
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return ran, fmt.Errorf("migrate: begin %04d: %w", m.Version, err)
		}
		// No args → simple protocol → a file may hold several statements.
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return ran, fmt.Errorf("migrate: apply %04d_%s: %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name); err != nil {
			_ = tx.Rollback(ctx)
			return ran, fmt.Errorf("migrate: record %04d: %w", m.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return ran, fmt.Errorf("migrate: commit %04d: %w", m.Version, err)
		}
		ran = append(ran, fmt.Sprintf("%04d_%s", m.Version, m.Name))
	}
	return ran, nil
}
