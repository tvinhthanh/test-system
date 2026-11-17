package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// Handler processes one job. It runs inside a transaction shared with the
// job's completion: return nil and both commit; return an error and the
// handler's writes roll back, then the job is retried with backoff (or
// declared dead). A panic is treated as an error.
type Handler func(ctx context.Context, job Job) error

// Worker consumes one Kind with a fixed number of goroutines (§6.7.4: one
// pool per logical queue).
type Worker struct {
	Queue       *Queue
	DB          *pg.DB
	Kind        string
	Handle      Handler
	Concurrency int           // default 1
	Lease       time.Duration // default 60 s; a handler slower than this may run twice
	Poll        time.Duration // idle sleep, default 1 s
	Logger      *slog.Logger
	Metrics     *metric.Registry // optional

	jobs    *metric.Counter
	seconds *metric.Histogram
	depth   *metric.Gauge
	age     *metric.Gauge
	latency *metric.Histogram
	polled  *metric.Gauge
}

// workerMetrics are one registry's queue series, shared by every Worker on
// it: each series carries the kind as a label, and a registry refuses a
// name registered twice — two pools in one process (run.finished and
// urlcheck.run since QĐ-14) would otherwise panic at start.
type workerMetrics struct {
	jobs               *metric.Counter
	seconds, latency   *metric.Histogram
	depth, age, polled *metric.Gauge
}

var (
	sharedMu      sync.Mutex
	sharedMetrics = map[*metric.Registry]*workerMetrics{}
)

func metricsFor(reg *metric.Registry) *workerMetrics {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if m, ok := sharedMetrics[reg]; ok {
		return m
	}
	m := &workerMetrics{
		jobs:    reg.Counter("queue_jobs_total", "Jobs finished by kind and result.", "kind", "result"),
		seconds: reg.Histogram("queue_job_seconds", "Handler duration by kind.", metric.DefaultLatencyBuckets, "kind"),
		depth:   reg.Gauge("queue_pending", "Runnable jobs waiting, by kind.", "kind"),
		age:     reg.Gauge("queue_oldest_pending_seconds", "Age of the oldest runnable job, by kind.", "kind"),
		latency: reg.Histogram("queue_job_latency_seconds", "Enqueue-to-done latency by kind — the §6.4 resolver latency for run.finished.", metric.DefaultLatencyBuckets, "kind"),
		polled:  reg.Gauge("queue_worker_last_poll_timestamp_seconds", "Unix time this worker last looked at the queue, by kind; stale = stuck.", "kind"),
	}
	sharedMetrics[reg] = m
	return m
}

func (w *Worker) defaults() {
	if w.Concurrency <= 0 {
		w.Concurrency = 1
	}
	if w.Lease <= 0 {
		w.Lease = 60 * time.Second
	}
	if w.Poll <= 0 {
		w.Poll = time.Second
	}
	if w.Logger == nil {
		w.Logger = slog.Default()
	}
	if w.Metrics != nil && w.jobs == nil {
		m := metricsFor(w.Metrics)
		w.jobs, w.seconds, w.depth, w.age, w.latency, w.polled = m.jobs, m.seconds, m.depth, m.age, m.latency, m.polled
	}
}

// Run consumes until ctx is cancelled, then waits for in-flight handlers.
func (w *Worker) Run(ctx context.Context) {
	w.defaults()
	var wg sync.WaitGroup
	for i := 0; i < w.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				did, err := w.RunOnce(ctx)
				if err != nil {
					w.Logger.Error("queue", "kind", w.Kind, "err", err)
				}
				if w.polled != nil {
					w.polled.Set(float64(time.Now().Unix()), w.Kind)
				}
				if !did {
					w.sample(ctx)
					select {
					case <-ctx.Done():
					case <-time.After(w.Poll):
					}
				}
			}
		}()
	}
	wg.Wait()
}

// RunOnce claims and processes at most one job. It reports whether a job
// was processed; an error is an infrastructure failure (the claim or the
// bookkeeping), never the handler's — those are recorded on the job.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	w.defaults()
	job, err := w.Queue.Claim(ctx, w.Kind, w.Lease)
	if err != nil || job == nil {
		return false, err
	}
	start := time.Now()
	herr := w.DB.WithTx(ctx, func(ctx context.Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		if err := w.Handle(ctx, *job); err != nil {
			return err
		}
		return w.Queue.Complete(ctx, job.ID)
	})
	if w.seconds != nil {
		w.seconds.Observe(time.Since(start).Seconds(), w.Kind)
	}
	if herr == nil {
		w.count("done")
		if w.latency != nil {
			w.latency.Observe(time.Since(job.CreatedAt).Seconds(), w.Kind)
		}
		return true, nil
	}
	dead, ferr := w.fail(ctx, job.ID, herr)
	if ferr != nil {
		return true, fmt.Errorf("record failure of %s: %w (handler error: %v)", job.ID, ferr, herr)
	}
	if dead {
		w.count("dead")
		w.Logger.Error("job dead", "kind", w.Kind, "job", job.ID, "key", job.Key, "attempts", job.Attempts, "err", herr)
	} else {
		w.count("retry")
		w.Logger.Warn("job failed, will retry", "kind", w.Kind, "job", job.ID, "key", job.Key, "attempt", job.Attempts, "err", herr)
	}
	return true, nil
}

// fail records the handler's error, trying a few times. When the database
// dropped the connections (a restart, a failover), the handler failed on
// one and the pool may hand Fail another dead one; a single try would leave
// the job `running` until its lease expires — minutes of a run unresolved
// for a blip the pool heals in milliseconds (§14.3 "restart kết nối DB").
func (w *Worker) fail(ctx context.Context, id string, cause error) (dead bool, err error) {
	for attempt := 0; ; attempt++ {
		dead, err = w.Queue.Fail(ctx, id, cause)
		if err == nil || errors.Is(err, ErrNotFound) || attempt == 3 {
			return dead, err
		}
		select {
		case <-ctx.Done():
			return dead, err
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
}

// Drain processes jobs until none is runnable — for tests and one-shot
// tools.
func (w *Worker) Drain(ctx context.Context) (int, error) {
	n := 0
	for {
		did, err := w.RunOnce(ctx)
		if err != nil {
			return n, err
		}
		if !did {
			return n, nil
		}
		n++
	}
}

func (w *Worker) count(result string) {
	if w.jobs != nil {
		w.jobs.Inc(w.Kind, result)
	}
}

func (w *Worker) sample(ctx context.Context) {
	if w.depth == nil {
		return
	}
	pending, oldest, err := w.Queue.Depth(ctx, w.Kind)
	if err != nil {
		return
	}
	w.depth.Set(float64(pending), w.Kind)
	w.age.Set(oldest.Seconds(), w.Kind)
}
