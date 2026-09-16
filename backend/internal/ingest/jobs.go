package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

// JobRunFinished is the queue kind enqueued by POST /finish (and by the
// auto-close sweep): tier 2 of §7.6 for one run.
const JobRunFinished = "run.finished"

// AutoCloseAfter is §7.4: a run open longer than this is closed as error
// RUN_TIMED_OUT.
const AutoCloseAfter = 6 * time.Hour

type runJob struct {
	ProjectID string `json:"projectId"`
	RunID     string `json:"runId"`
}

// Jobs is the worker-side half of ingest: cmd/worker builds one and runs
// Worker() and Sweep(). It shares Deps with the handlers so both sides
// speak to the same services.
type Jobs struct {
	Deps
	resolves   *metric.Counter
	autoclosed *metric.Counter
}

// AfterResolve runs inside the run.finished job after identities are
// backfilled, in the same transaction. cmd/worker chains the modules that
// consume finished runs (flaky today) here, so ingest imports none of them
// (§6.9.3 rule 3: coordination lives in cmd/).
type AfterResolve func(ctx context.Context, projectID string, runID results.RunID) error

// NewJobs registers the worker-side metrics. Call once per process.
func NewJobs(d Deps) *Jobs {
	return &Jobs{
		Deps:       d,
		resolves:   d.Metrics.Counter("identity_resolve_total", "Tier-2 identity decisions by project and decision (§9.3 step 4).", "project", "decision"),
		autoclosed: d.Metrics.Counter("runs_autoclosed_total", "Runs closed as RUN_TIMED_OUT after 6 h open (§7.4), by project.", "project"),
	}
}

// Worker consumes JobRunFinished with the given concurrency, running each
// AfterResolve in order once identities are in place.
func (j *Jobs) Worker(concurrency int, after ...AfterResolve) *queue.Worker {
	return &queue.Worker{
		Queue: j.Queue, DB: j.DB, Kind: JobRunFinished, Handle: j.runFinishedHandler(after),
		Concurrency: concurrency, Lease: 2 * time.Minute, Logger: j.Logger, Metrics: j.Metrics,
	}
}

func (j *Jobs) runFinishedHandler(after []AfterResolve) queue.Handler {
	return func(ctx context.Context, job queue.Job) error {
		var p runJob
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		n, err := j.ResolveRun(ctx, p.ProjectID, results.RunID(p.RunID))
		if err != nil {
			return err
		}
		if n > 0 {
			j.Logger.Info("tier-2 resolve", "run", p.RunID, "resolved", n)
		}
		for _, fn := range after {
			if err := fn(ctx, p.ProjectID, results.RunID(p.RunID)); err != nil {
				return err
			}
		}
		return nil
	}
}

// ResolveRun is tier 2 of §7.6 for one run, now that the run is complete
// and "is this candidate still present?" (N34) can be answered. Results
// tier 1 already pinned form the presence set; the rest go through the
// full §9.3 pipeline and get their stableTestId backfilled. Idempotent:
// a second pass finds nothing unresolved.
func (j *Jobs) ResolveRun(ctx context.Context, projectID string, runID results.RunID) (int, error) {
	list, err := j.Results.ListResults(ctx, runID)
	if err != nil {
		return 0, fmt.Errorf("list results: %w", err)
	}
	present := map[string]bool{}
	refs := map[string]identity.Ref{}
	for _, tr := range list {
		if tr.StableTestID != "" {
			present[tr.StableTestID] = true
			continue
		}
		if _, seen := refs[tr.ClientTestID]; seen {
			continue
		}
		var sig identity.Signals
		if err := json.Unmarshal(tr.IdentityRaw, &sig); err != nil {
			j.Logger.Warn("unreadable identity signals", "run", runID, "client_test_id", tr.ClientTestID, "err", err)
			continue
		}
		refs[tr.ClientTestID] = identity.Ref{Key: tr.ClientTestID, Signals: sig}
	}
	if len(refs) == 0 {
		return 0, nil
	}
	presentIDs := make([]string, 0, len(present))
	for id := range present {
		presentIDs = append(presentIDs, id)
	}
	pending := make([]identity.Ref, 0, len(refs))
	for _, ref := range refs {
		pending = append(pending, ref)
	}
	m, outcomes, err := j.Identity.ResolveDeferred(ctx, projectID, string(runID), pending, presentIDs)
	if err != nil {
		return 0, fmt.Errorf("resolve deferred: %w", err)
	}
	for _, o := range outcomes {
		j.resolves.Inc(projectID, string(o.Decision))
	}
	n, err := j.Results.SetStableIDs(ctx, runID, m)
	if err != nil {
		return 0, fmt.Errorf("backfill: %w", err)
	}
	return n, nil
}

// Sweep closes runs open longer than AutoCloseAfter as error RUN_TIMED_OUT
// (§7.4, §14.3: no run stuck in running forever) and queues tier 2 for
// each, so their results still get identities. Returns how many it closed.
func (j *Jobs) Sweep(ctx context.Context) (int, error) {
	var closed []results.Run
	err := j.DB.WithTx(ctx, func(ctx context.Context) error {
		var err error
		closed, err = j.Results.AutoClose(ctx, time.Now().Add(-AutoCloseAfter))
		if err != nil {
			return err
		}
		for _, r := range closed {
			if _, _, err := j.Queue.Add(ctx, queue.Enqueue{Kind: JobRunFinished, Key: string(r.ID), Payload: runJob{ProjectID: r.ProjectID, RunID: string(r.ID)}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, r := range closed {
		j.autoclosed.Inc(r.ProjectID)
		j.Logger.Warn("run auto-closed", "run", r.ID, "project", r.ProjectID, "started_at", r.StartedAt)
	}
	return len(closed), nil
}
