// Command api is the HTTP server for the /v1 API (§6.5, §6.9.2).
//
// It owns process lifecycle only: load config, build the logger, connect the
// pool, wire middleware, listen, and shut down cleanly on SIGINT/SIGTERM.
// Business handlers arrive as domain modules are built (§6.9.11, P1-min
// column) and are mounted here — cmd/* is the one place allowed to import
// everything (§6.9.3).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	platlog "github.com/vinh/automation-testing-platform/backend/internal/platform/log"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
	_ "github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory" // OBJSTORE=memory (dev)
	_ "github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/s3"     // OBJSTORE=s3
	"github.com/vinh/automation-testing-platform/backend/internal/platform/outbound"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/query"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// No logger yet — config is what tells us how to build one.
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(2)
	}
	logger := platlog.New(cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := pg.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("boot", "err", err)
		os.Exit(2)
	}

	reg := metric.New()
	metric.Process(reg)
	db.Metrics(reg)

	store, err := objstore.Open(cfg)
	if err != nil {
		logger.Error("boot", "err", err)
		os.Exit(2)
	}
	handler, _ := buildHandler(cfg, db, store, reg, logger)

	api := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	// /metrics on its own listener, loopback by default (config.MetricsAddr).
	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", reg.Handler())
	metric.Pprof(metricsMux) // heap/goroutine profiles on the loopback listener, for T5 leak hunting
	metricsSrv := &http.Server{Addr: cfg.MetricsAddr, Handler: metricsMux, ReadHeaderTimeout: 5 * time.Second}

	serve := func(name string, srv *http.Server) {
		logger.Info(name+" listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(name+" stopped", "err", err)
			stop()
		}
	}
	go serve("api", api)
	go serve("metrics", metricsSrv)

	<-ctx.Done()
	logger.Info("api shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exit := 0
	for _, srv := range []*http.Server{api, metricsSrv} {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown", "addr", srv.Addr, "err", err)
			exit = 1
		}
	}
	db.Close()
	os.Exit(exit)
}

// buildHandler wires every module onto one mux (§6.9.3: cmd/* is the one
// place that imports everything) and wraps it in the middleware stack.
// Separate from main so the HTTP integration test drives the exact
// handler the binary serves; the Router is returned so the test can walk
// every mounted pattern (§14.3 cross-tenant is per endpoint, not per sample).
func buildHandler(cfg config.Config, db *pg.DB, store objstore.Store, reg *metric.Registry, logger *slog.Logger) (http.Handler, *httpx.Router) {
	// Modules, in dependency order (§6.9.3).
	ten := tenancy.New(db)
	az := authz.New(db, []byte(cfg.TokenPepper))
	res := results.New(db)
	idn := identity.New(db)
	art := artifact.New(db, store, res)

	healthGauges(reg, queue.New(db), res, idn, az, logger)

	mux := httpx.NewRouter()
	mux.Handle("GET /healthz", httpx.Health())
	ingest.Routes(mux, ingest.Deps{
		DB: db, Queue: queue.New(db), Tenancy: ten, Authz: az, Results: res, Identity: idn,
		Metrics: reg, Logger: logger,
	})
	artifact.Routes(mux, artifact.Deps{DB: db, Tenancy: ten, Authz: az, Results: res, Artifact: art, Metrics: reg, Logger: logger})

	// Dashboard: cookie sessions (§11.2) and the read model (§6.9.5).
	web := authz.WebOptions{
		AllowedOrigins: cfg.DashboardOrigins, Secure: cfg.CookieSecure,
		OAuth: oauthProviders(cfg), StateKey: authz.OAuthStateKey([]byte(cfg.TokenPepper)),
	}
	// The only outbound HTTP the api makes for sign-in, allowed exactly the
	// providers' own endpoints (§11.6 API7).
	var endpoints []string
	for _, p := range web.OAuth {
		endpoints = append(endpoints, p.Endpoints()...)
	}
	if client, err := outbound.New(10*time.Second, endpoints...); err != nil {
		logger.Error("oauth sign-in off: provider endpoints", "err", err)
		web.OAuth = nil
	} else {
		web.OAuthHTTP = client
	}
	for _, p := range web.OAuth {
		logger.Info("oauth sign-in on", "provider", p.Name, "redirect_uri", p.RedirectURL)
	}
	authz.AuthRoutes(mux, az, web)
	// Test Management (P2, §6.8): the gate reads the Intelligence Plane
	// through query — the consumer-side Evidence interface (§6.9.4) keeps
	// testmgmt from importing a fourth domain (§6.9.3 rule 3).
	qry := query.New(db)
	tm := testmgmt.New(db, idn, gateEvidence{qry}, testmgmt.DefaultPolicy)
	(&dashboard{tenancy: ten, authz: az, query: qry, triage: triage.New(db, res), idents: idn, tm: tm, artifact: art, store: store, logger: logger, ratePerMinute: cfg.DashboardRatePerMinute}).routes(mux)

	// Order, outside in: request id → access log → browser policy (headers,
	// CORS, preflight) → session → metrics (must wrap the mux directly: it
	// reads the matched route pattern) → routes.
	policy := httpx.BrowserPolicy{AllowedOrigins: cfg.DashboardOrigins, HSTS: cfg.CookieSecure}
	return httpx.RequestID(httpx.Logging(logger)(policy.Middleware(authz.SessionMiddleware(az, web)(httpx.Metrics(reg)(mux))))), mux
}

// oauthProviders turns the OAUTH_GOOGLE / OAUTH_GITHUB switches into the
// sign-in providers (§11.2); a switched-off provider does not exist for the
// api — its routes answer "disabled" and the sign-in page shows no button.
// A variable so tests can point the providers at a fake.
var oauthProviders = func(cfg config.Config) []*authz.OAuthProvider {
	var out []*authz.OAuthProvider
	if cfg.OAuthGoogle.Enabled {
		out = append(out, authz.Google(cfg.OAuthGoogle.ClientID, cfg.OAuthGoogle.ClientSecret, cfg.PublicURL))
	}
	if cfg.OAuthGitHub.Enabled {
		out = append(out, authz.GitHub(cfg.OAuthGitHub.ClientID, cfg.OAuthGitHub.ClientSecret, cfg.PublicURL))
	}
	return out
}

// gateEvidence adapts query's read model to testmgmt.Evidence.
type gateEvidence struct{ q query.Service }

func (g gateEvidence) LatestFinishedRun(ctx context.Context, projectID, branch string) (testmgmt.RunEvidence, bool, error) {
	run, ok, err := g.q.GateEvidence(ctx, projectID, branch)
	if err != nil || !ok {
		return testmgmt.RunEvidence{}, ok, err
	}
	out := testmgmt.RunEvidence{RunID: run.RunID, Branch: run.Branch, FinishedAt: run.FinishedAt, Total: run.Total, FlakyPass: run.FlakyPass, Results: make([]testmgmt.ResultEvidence, 0, len(run.Results))}
	for _, r := range run.Results {
		out.Results = append(out.Results, testmgmt.ResultEvidence{ResultID: r.ResultID, StableTestID: r.StableTestID, FinalStatus: r.FinalStatus, Triaged: r.Triaged})
	}
	return out, true, nil
}
