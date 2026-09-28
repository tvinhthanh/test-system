// Package ingest is the hot path (§7.2–7.4): the three endpoints a reporter
// calls, the caps the server enforces, and the boundary where reporter
// payload stops being untrusted input (§7.10). It owns no tables. It
// authenticates with authz, resolves the token's project with tenancy,
// writes through results, and — once identity exists — asks it for the
// inline (tier-1) resolve of §7.6.
//
// This file is the whole contract (§6.9.4). Domain fan-out is results and
// identity: two of the three §6.9.3 allows. Tier 2 of §7.6 is a queue job
// (jobs.go) enqueued in the finish transaction and run by cmd/worker, which
// also sweeps runs open past §7.4.
package ingest

import (
	"log/slog"
	"net/http"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Deps is everything the handlers and jobs need; cmd/api and cmd/worker
// assemble it.
type Deps struct {
	DB       *pg.DB
	Queue    *queue.Queue
	Tenancy  tenancy.Service
	Authz    authz.Service
	Results  results.Service
	Identity identity.Service
	Metrics  *metric.Registry
	Logger   *slog.Logger
}

// Caps are §7.4, server-decided (§7.1 #3).
const (
	MaxEventsPerBatch = 500
	MaxBatchBytes     = 5 << 20
	MaxRunBytes       = 64 << 10
	RatePerMinute     = 60
	RateBurst         = 120
)

// Routes mounts the P1 endpoints on mux, each behind bearer auth and the
// per-token rate limit. Mount once; metric names are registered here.
func Routes(mux httpx.Mux, d Deps) {
	h := newHandlers(d)
	auth := authz.Middleware(d.Authz)
	limit := httpx.RateLimit(RatePerMinute, RateBurst, func(r *http.Request) string {
		p, _ := authz.PrincipalFrom(r.Context())
		return p.TokenID
	})
	// The observation wraps auth and the rate limit, not just the handler:
	// a 503 from a lost database or a 429 is an ingest outcome the §6.4
	// error rate must see (test/observe caught it counting only handler
	// answers). The project label is filled once auth has resolved it.
	wrap := func(endpoint string, fn http.HandlerFunc) http.Handler { return h.observe(endpoint, auth(limit(fn))) }
	mux.Handle("POST /v1/runs", wrap("runs", h.createRun))
	mux.Handle("POST /v1/runs/{localRunId}/events", wrap("events", h.appendEvents))
	mux.Handle("POST /v1/runs/{localRunId}/finish", wrap("finish", h.finishRun))
	// JUnit XML backfill (§6.8.5, QĐ-13): one report, one historic run.
	mux.Handle("POST /v1/imports/junit", wrap("junit", h.importJUnit))
}
