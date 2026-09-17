package queue_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

func TestBackoff(t *testing.T) {
	b := queue.ExponentialBackoff(5*time.Second, time.Minute)
	for attempt, want := range map[int]time.Duration{1: 5 * time.Second, 2: 10 * time.Second, 3: 20 * time.Second, 4: 40 * time.Second, 5: time.Minute, 9: time.Minute} {
		if got := b(attempt); got != want {
			t.Errorf("attempt %d: %v want %v", attempt, got, want)
		}
	}
}

func worker(db *pg.DB, q *queue.Queue, kind string, h queue.Handler) *queue.Worker {
	return &queue.Worker{Queue: q, DB: db, Kind: kind, Handle: h, Lease: 2 * time.Second, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: metric.New()}
}

func TestQueueSemanticsAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	q := queue.New(db)
	q.Backoff = func(int) time.Duration { return 0 } // retry immediately in tests
	ctx := context.Background()

	// Dedupe by key while live; distinct kinds are distinct queues.
	id1, created, err := q.Add(ctx, queue.Enqueue{Kind: "a", Key: "run-1", Payload: map[string]any{"run": 1}})
	if err != nil || !created {
		t.Fatalf("add: %v %v", created, err)
	}
	id2, created, _ := q.Add(ctx, queue.Enqueue{Kind: "a", Key: "run-1"})
	if created || id2 != id1 {
		t.Errorf("duplicate key should return the live job: %s vs %s created=%v", id2, id1, created)
	}
	if _, created, _ := q.Add(ctx, queue.Enqueue{Kind: "b", Key: "run-1"}); !created {
		t.Error("same key in another kind must be its own job")
	}
	if pending, _, _ := q.Depth(ctx, "a"); pending != 1 {
		t.Errorf("depth a = %d", pending)
	}

	// Enqueue inside a rolled-back transaction leaves nothing behind (outbox).
	_ = db.WithTx(ctx, func(ctx context.Context) error {
		_, _, _ = q.Add(ctx, queue.Enqueue{Kind: "a", Key: "ghost"})
		return errors.New("abort")
	})
	if pending, _, _ := q.Depth(ctx, "a"); pending != 1 {
		t.Errorf("rolled-back enqueue is visible: depth %d", pending)
	}

	// Handler failure → retry; success → done; the key is reusable after.
	var calls int32
	w := worker(db, q, "a", func(ctx context.Context, j queue.Job) error {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return errors.New("first attempt fails")
		}
		return nil
	})
	if did, err := w.RunOnce(ctx); !did || err != nil {
		t.Fatalf("first run: %v %v", did, err)
	}
	if st, _ := q.Get(ctx, id1); st.Status != "pending" || st.Attempts != 1 || st.LastError != "first attempt fails" {
		t.Errorf("after failure: %+v", st)
	}
	if did, err := w.RunOnce(ctx); !did || err != nil {
		t.Fatalf("second run: %v %v", did, err)
	}
	if st, _ := q.Get(ctx, id1); st.Status != "done" || st.Attempts != 2 {
		t.Errorf("after success: %+v", st)
	}
	if did, _ := w.RunOnce(ctx); did {
		t.Error("nothing left, yet something ran")
	}
	if _, created, _ := q.Add(ctx, queue.Enqueue{Kind: "a", Key: "run-1"}); !created {
		t.Error("key not reusable after completion")
	}

	// Dead after max attempts.
	idDead, _, _ := q.Add(ctx, queue.Enqueue{Kind: "d", MaxAttempts: 2})
	wd := worker(db, q, "d", func(context.Context, queue.Job) error { return errors.New("always") })
	if n, err := wd.Drain(ctx); err != nil || n != 2 {
		t.Fatalf("drain: n=%d err=%v", n, err)
	}
	if st, _ := q.Get(ctx, idDead); st.Status != "dead" || st.Attempts != 2 {
		t.Errorf("dead job: %+v", st)
	}

	// A panicking handler is a failed attempt, not a dead worker, and its
	// writes roll back with it.
	idPanic, _, _ := q.Add(ctx, queue.Enqueue{Kind: "p"})
	wp := worker(db, q, "p", func(ctx context.Context, j queue.Job) error {
		_, _ = db.Q(ctx).Exec(ctx, `INSERT INTO jobs (kind) VALUES ('side-effect')`)
		panic("boom")
	})
	if did, err := wp.RunOnce(ctx); !did || err != nil {
		t.Fatalf("panic run: %v %v", did, err)
	}
	if st, _ := q.Get(ctx, idPanic); st.Status != "pending" || st.LastError != "panic: boom" {
		t.Errorf("after panic: %+v", st)
	}
	if pending, _, _ := q.Depth(ctx, "side-effect"); pending != 0 {
		t.Error("handler side effect survived its panic")
	}

	// RunAt in the future is not claimable yet.
	_, _, _ = q.Add(ctx, queue.Enqueue{Kind: "later", RunAt: time.Now().Add(time.Hour)})
	wl := worker(db, q, "later", func(context.Context, queue.Job) error { return nil })
	if did, _ := wl.RunOnce(ctx); did {
		t.Error("future job ran early")
	}
}

// TestLeaseExpiryRecoversCrashedWorker is §14.3 row 1: a worker that dies
// mid-job holds a lease; once it expires another worker claims the job.
func TestLeaseExpiryRecoversCrashedWorker(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	q := queue.New(db)
	ctx := context.Background()
	id, _, _ := q.Add(ctx, queue.Enqueue{Kind: "k", Key: "x"})

	// "Crash": claim with a short lease and never complete.
	j, err := q.Claim(ctx, "k", 300*time.Millisecond)
	if err != nil || j == nil || j.ID != id || j.Attempts != 1 {
		t.Fatalf("claim: %+v %v", j, err)
	}
	if j2, _ := q.Claim(ctx, "k", time.Second); j2 != nil {
		t.Fatal("job claimed twice while leased")
	}
	time.Sleep(350 * time.Millisecond)
	j3, err := q.Claim(ctx, "k", time.Second)
	if err != nil || j3 == nil || j3.ID != id || j3.Attempts != 2 {
		t.Fatalf("reclaim after lease expiry: %+v %v", j3, err)
	}
	if err := q.Complete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(ctx, id); !errors.Is(err, queue.ErrNotFound) {
		t.Errorf("completing twice: %v", err)
	}
}

// TestConcurrentWorkersShareWithoutDuplicates: SKIP LOCKED hands each job
// to exactly one of many workers.
func TestConcurrentWorkersShareWithoutDuplicates(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	q := queue.New(db)
	ctx := context.Background()
	const jobs = 40
	for i := 0; i < jobs; i++ {
		if _, _, err := q.Add(ctx, queue.Enqueue{Kind: "c", Payload: i}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[string]int{}
	w := worker(db, q, "c", func(_ context.Context, j queue.Job) error {
		mu.Lock()
		seen[j.ID]++
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	w.Concurrency = 8
	w.Poll = 20 * time.Millisecond
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() {
		for {
			if pending, _, _ := q.Depth(ctx, "c"); pending == 0 {
				var running int
				_ = db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'c' AND status <> 'done'`).Scan(&running)
				if running == 0 {
					cancel()
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	w.Run(runCtx)
	if len(seen) != jobs {
		t.Errorf("%d distinct jobs processed, want %d", len(seen), jobs)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %s processed %d times", id, n)
		}
	}
	if !strings.Contains(w.Metrics.Expose(), `queue_jobs_total{kind="c",result="done"} 40`) {
		t.Errorf("metrics:\n%s", w.Metrics.Expose())
	}
}

// TestPruneKeepsLiveAndDeadJobs: Prune drops only done jobs older than the
// retention, in bounded batches; pending, running and dead rows stay, and
// Backlog reads the same numbers before and after.
func TestPruneKeepsLiveAndDeadJobs(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	q := queue.New(db)
	ctx := context.Background()

	// 30 done jobs: 20 finished 8 days ago, 10 finished an hour ago.
	for i := 0; i < 30; i++ {
		id, _, _ := q.Add(ctx, queue.Enqueue{Kind: "old"})
		age := "8 days"
		if i >= 20 {
			age = "1 hour"
		}
		if _, err := db.Q(ctx).Exec(ctx, `UPDATE jobs SET status = 'done', finished_at = now() - $2::interval WHERE id = $1`, id, age); err != nil {
			t.Fatal(err)
		}
	}
	// One of each state that must survive.
	_, _, _ = q.Add(ctx, queue.Enqueue{Kind: "live", Key: "pending"})
	idRun, _, _ := q.Add(ctx, queue.Enqueue{Kind: "live", Key: "running"})
	if _, err := q.Claim(ctx, "live", time.Minute); err != nil {
		t.Fatal(err)
	}
	_ = idRun
	idDead, _, _ := q.Add(ctx, queue.Enqueue{Kind: "live", Key: "dead"})
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE jobs SET status = 'dead', finished_at = now() - interval '30 days' WHERE id = $1`, idDead); err != nil {
		t.Fatal(err)
	}
	before, err := q.Backlog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.Pending != 1 || before.Running != 1 || before.Dead != 1 {
		t.Fatalf("backlog before prune: %+v", before)
	}

	// Batches of 15: the first call cannot finish the job, the second does,
	// the third finds nothing.
	got := []int{}
	for i := 0; i < 3; i++ {
		n, err := q.Prune(ctx, queue.PruneAfter, 15)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, n)
	}
	if want := []int{15, 5, 0}; !equalInts(got, want) {
		t.Errorf("prune batches %v, want %v", got, want)
	}
	var left int
	_ = db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM jobs`).Scan(&left)
	if left != 10+3 {
		t.Errorf("%d rows left, want 13 (10 recent done + pending + running + dead)", left)
	}
	after, _ := q.Backlog(ctx)
	if after.Pending != before.Pending || after.Running != before.Running || after.Dead != before.Dead {
		t.Errorf("backlog changed by prune: %+v → %+v", before, after)
	}
	if err := db.Q(ctx).QueryRow(ctx, `SELECT 1 FROM jobs WHERE id = $1 AND status = 'dead'`, idDead).Scan(new(int)); err != nil {
		t.Errorf("dead job pruned: %v", err)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
