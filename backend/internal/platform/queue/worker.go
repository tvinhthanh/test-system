package queue

import (
	"context"
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
		w.jobs = w.Metrics.Counter("queue_jobs_total", "Jobs finished by kind and result.", "kind", "result")
		w.seconds = w.Metrics.Histogram("queue_job_seconds", "Handler duration by kind.", metric.DefaultLatencyBuckets, "kind")
		w.depth = w.Metrics.Gauge("queue_pending", "Runnable jobs waiting, by kind.", "kind")
		w.age = w.Metrics.Gauge("queue_oldest_pending_seconds", "Age of the oldest runnable job, by kind.", "kind")
		w.latency = w.Metrics.Histogram("queue_job_latency_seconds", "Enqueue-to-done latency by kind — the §6.4 resolver latency for run.finished.", metric.DefaultLatencyBuckets, "kind")
		w.polled = w.Metrics.Gauge("queue_worker_last_poll_timestamp_seconds", "Unix time this worker last looked at the queue, by kind; stale = stuck.", "kind")
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
	dead, ferr := w.Queue.Fail(ctx, job.ID, herr)
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
