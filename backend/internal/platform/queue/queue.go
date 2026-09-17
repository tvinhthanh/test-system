// Package queue is the Postgres job queue of §6.7.4/§6.7.8: many logical
// queues (Kind) in one table, claimed with FOR UPDATE SKIP LOCKED, retried
// with backoff, recovered by lease expiry when a worker dies mid-job.
//
// Enqueue joins the caller's transaction (§6.9.6): a job written next to
// the business change commits with it or not at all, which is the outbox
// guarantee without an outbox table. Handlers must be idempotent — delivery
// is at-least-once, and a job whose lease expired while its worker was
// still (slowly) finishing is claimed a second time.
//
// Platform code: it knows kinds and payloads as opaque strings and JSON.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// Job is one unit of work as handed to a handler.
type Job struct {
	ID          string
	Kind        string
	Key         string
	Payload     json.RawMessage
	Attempts    int // including this one
	MaxAttempts int
	RunAt       time.Time
	CreatedAt   time.Time // enqueue time: done − CreatedAt is the §6.4 queue latency
}

// Enqueue describes a job to add. Key deduplicates within Kind while a job
// with the same key is pending or running. Payload is marshalled to JSON.
type Enqueue struct {
	Kind        string
	Key         string
	Payload     any
	RunAt       time.Time // zero = now
	MaxAttempts int       // zero = 5
}

// ErrNotFound: no job with that id.
var ErrNotFound = errors.New("queue: job not found")

// Queue is the client side; Worker is the consumer side.
type Queue struct {
	db  *pg.DB
	now func() time.Time
	// Backoff returns how long to wait before retry n (1-based).
	Backoff func(attempt int) time.Duration
}

// New returns a Queue with exponential backoff: 5 s, 10 s, 20 s, ... capped
// at 10 min.
func New(db *pg.DB) *Queue {
	return &Queue{db: db, now: time.Now, Backoff: ExponentialBackoff(5*time.Second, 10*time.Minute)}
}

// ExponentialBackoff doubles from base each attempt up to max.
func ExponentialBackoff(base, max time.Duration) func(int) time.Duration {
	return func(attempt int) time.Duration {
		d := base
		for i := 1; i < attempt && d < max; i++ {
			d *= 2
		}
		if d > max {
			d = max
		}
		return d
	}
}

// Add enqueues a job. created is false when a live job with the same
// (Kind, Key) already exists; that job's id is returned instead.
func (q *Queue) Add(ctx context.Context, e Enqueue) (id string, created bool, err error) {
	if e.Kind == "" {
		return "", false, errors.New("queue: kind required")
	}
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return "", false, fmt.Errorf("queue: payload: %w", err)
	}
	if e.Payload == nil {
		payload = []byte(`{}`)
	}
	runAt := e.RunAt
	if runAt.IsZero() {
		runAt = q.now()
	}
	maxAttempts := e.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	var key *string
	if e.Key != "" {
		key = &e.Key
	}
	err = q.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO jobs (kind, key, payload, run_at, max_attempts) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (kind, key) WHERE key IS NOT NULL AND status IN ('pending', 'running') DO NOTHING
		RETURNING id`, e.Kind, key, payload, runAt.UTC(), maxAttempts).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pg.ErrNoRows) {
		return "", false, err
	}
	err = q.db.Q(ctx).QueryRow(ctx, `
		SELECT id FROM jobs WHERE kind = $1 AND key = $2 AND status IN ('pending', 'running')`, e.Kind, e.Key).Scan(&id)
	return id, false, err
}

// Claim takes the oldest runnable job of kind — pending and due, or running
// with an expired lease — and leases it. Returns nil, nil when there is
// none. Runs in its own statement so the lease is visible to other workers
// immediately, whatever the caller's transaction.
func (q *Queue) Claim(ctx context.Context, kind string, lease time.Duration) (*Job, error) {
	now := q.now().UTC()
	var j Job
	var key *string
	err := q.db.Q(ctx).QueryRow(ctx, `
		UPDATE jobs SET status = 'running', leased_until = $3, attempts = attempts + 1, updated_at = $2
		 WHERE id = (
			SELECT id FROM jobs
			 WHERE kind = $1 AND ((status = 'pending' AND run_at <= $2) OR (status = 'running' AND leased_until < $2))
			 ORDER BY run_at
			 FOR UPDATE SKIP LOCKED
			 LIMIT 1)
		RETURNING id, kind, key, payload, attempts, max_attempts, run_at, created_at`,
		kind, now, now.Add(lease)).Scan(&j.ID, &j.Kind, &key, &j.Payload, &j.Attempts, &j.MaxAttempts, &j.RunAt, &j.CreatedAt)
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if key != nil {
		j.Key = *key
	}
	j.RunAt = j.RunAt.UTC()
	return &j, nil
}

// Complete marks a job done. It joins the caller's transaction, so a
// handler that runs inside WithTx commits its writes and its completion
// together.
func (q *Queue) Complete(ctx context.Context, id string) error {
	tag, err := q.db.Q(ctx).Exec(ctx, `
		UPDATE jobs SET status = 'done', finished_at = $2, updated_at = $2, leased_until = NULL WHERE id = $1 AND status = 'running'`, id, q.now().UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Fail records an attempt's error and schedules a retry with backoff, or
// marks the job dead once attempts reach MaxAttempts. The error text is
// stored for the operator; keep secrets out of it.
func (q *Queue) Fail(ctx context.Context, id string, cause error) (dead bool, err error) {
	now := q.now().UTC()
	msg := "unknown"
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 2000 {
			msg = msg[:2000]
		}
	}
	var attempts, maxAttempts int
	if err := q.db.Q(ctx).QueryRow(ctx, `SELECT attempts, max_attempts FROM jobs WHERE id = $1 AND status = 'running'`, id).Scan(&attempts, &maxAttempts); err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	if attempts >= maxAttempts {
		_, err := q.db.Q(ctx).Exec(ctx, `
			UPDATE jobs SET status = 'dead', last_error = $2, finished_at = $3, updated_at = $3, leased_until = NULL WHERE id = $1`, id, msg, now)
		return true, err
	}
	_, err = q.db.Q(ctx).Exec(ctx, `
		UPDATE jobs SET status = 'pending', last_error = $2, run_at = $3, updated_at = $4, leased_until = NULL WHERE id = $1`,
		id, msg, now.Add(q.Backoff(attempts)), now)
	return false, err
}

// Extend pushes a running job's lease out, for handlers that legitimately
// take longer than the lease.
func (q *Queue) Extend(ctx context.Context, id string, lease time.Duration) error {
	tag, err := q.db.Q(ctx).Exec(ctx, `UPDATE jobs SET leased_until = $2 WHERE id = $1 AND status = 'running'`, id, q.now().UTC().Add(lease))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Depth reports how many jobs of kind are waiting and how old the oldest
// runnable one is — the §6.4 "queue latency" row.
func (q *Queue) Depth(ctx context.Context, kind string) (pending int, oldest time.Duration, err error) {
	var oldestAt *time.Time
	err = q.db.Q(ctx).QueryRow(ctx, `
		SELECT count(*), min(run_at) FROM jobs WHERE kind = $1 AND status = 'pending' AND run_at <= $2`, kind, q.now().UTC()).Scan(&pending, &oldestAt)
	if err != nil {
		return 0, 0, err
	}
	if oldestAt != nil {
		oldest = q.now().Sub(*oldestAt)
	}
	return pending, oldest, nil
}

// Backlog is the whole queue in one glance, for a process that is not the
// worker: the api exposes it at scrape time so a dead or stuck worker is
// visible from a listener that is still alive (§6.4 "biết mình đang hỏng").
type Backlog struct {
	Pending int           // runnable now, any kind
	Oldest  time.Duration // age of the oldest runnable job
	Running int           // leased right now
	Dead    int           // gave up after max attempts — needs a human
	Done    int           // finished and not yet pruned (JOB_RETENTION) — grows without bound if the prune is not running
}

// Backlog reads the queue-wide counts. Three lookups, each on the partial
// index of its status, rather than one pass over the table: the api calls
// this at every scrape (cmd/api/health_metrics.go), and a single FILTER
// query over jobs reads every finished job in history — a cost that grows
// with the table for as long as the process runs (T5).
func (q *Queue) Backlog(ctx context.Context) (Backlog, error) {
	var b Backlog
	var oldestAt *time.Time
	now := q.now().UTC()
	err := q.db.Q(ctx).QueryRow(ctx, `
		SELECT (SELECT count(*) FROM jobs WHERE status = 'pending' AND run_at <= $1),
		       (SELECT min(run_at) FROM jobs WHERE status = 'pending' AND run_at <= $1),
		       (SELECT count(*) FROM jobs WHERE status = 'running'),
		       (SELECT count(*) FROM jobs WHERE status = 'dead'),
		       (SELECT count(*) FROM jobs WHERE status = 'done')`, now).Scan(&b.Pending, &oldestAt, &b.Running, &b.Dead, &b.Done)
	if err != nil {
		return Backlog{}, err
	}
	if oldestAt != nil {
		b.Oldest = now.Sub(*oldestAt)
	}
	return b, nil
}

// PruneAfter is the default retention of a finished job — long enough for
// the operator to look at it (queue_job_latency, last_error of a retried
// job); the worker reads the actual value from JOB_RETENTION.
const PruneAfter = 7 * 24 * time.Hour

// Prune deletes done jobs finished before now−olderThan, at most limit rows
// per call, and returns how many went. Dead jobs are kept: they are the
// ones a human still has to read. Without this the table grows by one row
// per run forever — the queue "phình" that T5 (§14.1) is there to catch —
// and every count over it slows with the history it drags along. The
// worker calls it from its minute sweep; small batches keep the lock and
// the WAL burst bounded on a table that is being claimed from at the same
// time.
func (q *Queue) Prune(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	if limit <= 0 {
		limit = 1000
	}
	tag, err := q.db.Q(ctx).Exec(ctx, `
		DELETE FROM jobs WHERE id IN (
			SELECT id FROM jobs WHERE status = 'done' AND finished_at < $1 ORDER BY finished_at LIMIT $2)`,
		q.now().UTC().Add(-olderThan), limit)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// Status is a job's current state, for tests and the operator.
type Status struct {
	Status      string
	Attempts    int
	LastError   string
	RunAt       time.Time
	LeasedUntil time.Time
}

// Get returns a job's status.
func (q *Queue) Get(ctx context.Context, id string) (Status, error) {
	var s Status
	var lastErr *string
	var leased *time.Time
	err := q.db.Q(ctx).QueryRow(ctx, `SELECT status, attempts, last_error, run_at, leased_until FROM jobs WHERE id = $1`, id).Scan(&s.Status, &s.Attempts, &lastErr, &s.RunAt, &leased)
	if errors.Is(err, pg.ErrNoRows) {
		return Status{}, ErrNotFound
	}
	if err != nil {
		return Status{}, err
	}
	if lastErr != nil {
		s.LastError = *lastErr
	}
	if leased != nil {
		s.LeasedUntil = leased.UTC()
	}
	s.RunAt = s.RunAt.UTC()
	return s, nil
}
