package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

// healthGauges are the §6.4 "am I broken" numbers read from the database at
// scrape time by the api, on purpose not by the worker: a dead worker cannot
// report its own backlog, and its /metrics is gone with it. Each read has a
// short deadline; a failing read reports -1 so a scraper can tell "the
// database is unreachable" from "the queue is empty".
//
//	queue_backlog_jobs / queue_backlog_oldest_seconds  — jobs runnable now, any kind, and the oldest one's age
//	queue_running_jobs / queue_dead_jobs               — leased right now / gave up after max attempts
//	queue_finished_jobs                                — done and not yet pruned; JOB_RETENTION bounds it
//	runs_open_over_limit                               — runs still running past AutoCloseAfter (the sweep is not running)
//	review_queue_pending                               — identity pairs waiting for a human (§6.4: alert > 50)
//	api_tokens_expiring_30d                            — live tokens expiring within 30 days (§11.4 reminder)
func healthGauges(reg *metric.Registry, q *queue.Queue, res results.Service, idn identity.Service, az authz.Service, logger *slog.Logger) {
	read := func(name string, fn func(ctx context.Context) (float64, error)) func() float64 {
		return func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			v, err := fn(ctx)
			if err != nil {
				logger.Warn("health gauge", "metric", name, "err", err)
				return -1
			}
			return v
		}
	}
	backlog := func(pick func(queue.Backlog) float64) func(context.Context) (float64, error) {
		return func(ctx context.Context) (float64, error) {
			b, err := q.Backlog(ctx)
			if err != nil {
				return 0, err
			}
			return pick(b), nil
		}
	}
	reg.GaugeFunc("queue_backlog_jobs", "Jobs runnable now, any kind, as seen by the api (§6.4 queue latency).", read("queue_backlog_jobs", backlog(func(b queue.Backlog) float64 { return float64(b.Pending) })))
	reg.GaugeFunc("queue_backlog_oldest_seconds", "Age of the oldest runnable job, any kind, as seen by the api.", read("queue_backlog_oldest_seconds", backlog(func(b queue.Backlog) float64 { return b.Oldest.Seconds() })))
	reg.GaugeFunc("queue_running_jobs", "Jobs leased right now.", read("queue_running_jobs", backlog(func(b queue.Backlog) float64 { return float64(b.Running) })))
	reg.GaugeFunc("queue_dead_jobs", "Jobs that exhausted their attempts and need a human.", read("queue_dead_jobs", backlog(func(b queue.Backlog) float64 { return float64(b.Dead) })))
	reg.GaugeFunc("queue_finished_jobs", "Done jobs not yet pruned — grows without bound when the worker prune is not running (T5).", read("queue_finished_jobs", backlog(func(b queue.Backlog) float64 { return float64(b.Done) })))
	reg.GaugeFunc("runs_open_over_limit", "Runs still running past the auto-close limit — non-zero for long means the worker sweep is not running (§7.4, §14.3).", read("runs_open_over_limit", func(ctx context.Context) (float64, error) {
		n, err := res.OpenRuns(ctx, time.Now().Add(-ingest.AutoCloseAfter))
		return float64(n), err
	}))
	reg.GaugeFunc("review_queue_pending", "Identity pairs waiting for a human decision (§6.4: alert above 50).", read("review_queue_pending", func(ctx context.Context) (float64, error) {
		n, err := idn.PendingReviews(ctx)
		return float64(n), err
	}))
	reg.GaugeFunc("api_tokens_expiring_30d", "Live ingest tokens that expire within 30 days (§11.4: remind the owner before CI starts failing with 401).", read("api_tokens_expiring_30d", func(ctx context.Context) (float64, error) {
		n, err := az.ExpiringTokens(ctx, 30*24*time.Hour)
		return float64(n), err
	}))
}
