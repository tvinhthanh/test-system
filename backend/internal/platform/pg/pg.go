// Package pg owns the Postgres connection pool and the one transaction
// convention every module follows (§6.9.6): a use case runs in ONE
// transaction, carried down through ctx; a module receives it from its caller
// and never opens its own.
//
// Stores are written against Querier, which both the pool and a transaction
// satisfy, and obtain it with DB.Q(ctx). Code that forgets WithTx still works
// — it just runs each statement in autocommit — so a missing transaction is a
// review finding, not a crash. FOR UPDATE SKIP LOCKED helpers arrive with
// platform/queue.
package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoRows is returned by QueryRow.Scan when the query matched nothing.
// Re-exported so stores need not import pgx for the one sentinel they check.
var ErrNoRows = pgx.ErrNoRows

// Querier is the slice of pgx that stores use. *pgxpool.Pool and pgx.Tx both
// implement it, which is what lets DB.Q hand back either.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB is the process-wide pool plus the transaction helpers.
type DB struct {
	pool *pgxpool.Pool
}

// Connect parses a postgres:// URL, opens the pool and pings once so a wrong
// URL fails the process at boot (§6.9.2), not on the first request.
func Connect(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("pg: parse DATABASE_URL: %w", err)
	}
	return Open(ctx, cfg)
}

// Open is Connect for callers that already built a pool config — tests that
// pin search_path, or a worker that wants a different pool size.
func Open(ctx context.Context, cfg *pgxpool.Config) (*DB, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pg: open pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg: ping %s:%d/%s: %w", cfg.ConnConfig.Host, cfg.ConnConfig.Port, cfg.ConnConfig.Database, err)
	}
	return &DB{pool: pool}, nil
}

// Close drains the pool. Call it on shutdown after the HTTP server stops.
func (d *DB) Close() { d.pool.Close() }

// Pool exposes the raw pool for the few callers that need a dedicated
// connection (migrations take an advisory lock; LISTEN needs a session).
func (d *DB) Pool() *pgxpool.Pool { return d.pool }

type txKey struct{}

// WithTx runs fn inside a transaction placed in ctx. It commits when fn
// returns nil and rolls back otherwise — including on panic, which is
// re-raised after the rollback. A call made while ctx already carries a
// transaction joins it instead of nesting: one use case, one transaction.
func (d *DB) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, joined := ctx.Value(txKey{}).(pgx.Tx); joined {
		return fn(ctx)
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pg: begin: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)
			panic(r)
		}
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("pg: rollback: %w", rbErr))
			}
			return
		}
		if cErr := tx.Commit(ctx); cErr != nil {
			err = fmt.Errorf("pg: commit: %w", cErr)
		}
	}()
	return fn(context.WithValue(ctx, txKey{}, tx))
}

// Q returns the transaction carried by ctx, or the pool when there is none.
// Stores call this on every query rather than caching the result.
func (d *DB) Q(ctx context.Context) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return d.pool
}

// InTx reports whether ctx carries a transaction — for a store that must
// refuse to run outside one (a multi-statement write, §6.9.6).
func InTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(pgx.Tx)
	return ok
}

// IsUniqueViolation reports whether err is Postgres SQLSTATE 23505, the
// signal every idempotent insert (§7.1) turns into "already exists".
func IsUniqueViolation(err error) bool { return sqlState(err) == "23505" }

// IsForeignKeyViolation reports SQLSTATE 23503: the referenced parent row
// does not exist.
func IsForeignKeyViolation(err error) bool { return sqlState(err) == "23503" }

// IsCheckViolation reports SQLSTATE 23514: a CHECK constraint rejected the
// row — the database's copy of a validation rule caught what the Go copy
// missed.
func IsCheckViolation(err error) bool { return sqlState(err) == "23514" }

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
