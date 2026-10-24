// Command worker consumes the logical queues (§6.7.4) and runs the
// periodic sweeps. It is the same code as cmd/api in a second process —
// §6.9.8 step 2: ingest and the tier-2 resolver have different load
// profiles and scale apart without a network hop between them.
//
// Today it runs one pool (run.finished → tier-2 identity resolve) and the
// §7.4 auto-close sweep. Each new job kind gets its own Worker here.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/aisuggest"
	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/billing"
	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	platlog "github.com/vinh/automation-testing-platform/backend/internal/platform/log"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
	_ "github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory" // OBJSTORE=memory (dev)
	_ "github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/s3"     // OBJSTORE=s3
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/query"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
	"github.com/vinh/automation-testing-platform/backend/internal/urlcheck"
)

const (
	resolveConcurrency = 4
	sweepEvery         = time.Minute
	// URL checks (QĐ-14) run as many at once as the checker service takes
	// (CHECKER_CONCURRENCY, default 2); more would only meet its 503.
	urlCheckConcurrency = 2
	// AI drafts: each is one model call of up to a few minutes.
	aiConcurrency = 2
	// Auto test (QĐ-16): each case holds one checker session
	// (CHECKER_SESSIONS, default 4) for up to a few minutes; drafting and
	// finding a sign-in hold one more each.
	autoCaseConcurrency  = 2
	autoDraftConcurrency = 1
	autoLoginConcurrency = 1
)

func main() {
	cfg, err := config.Load()
	if err != nil {
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

	ten, az, res := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper)), results.New(db)
	q := queue.New(db)
	pruned := reg.Counter("queue_jobs_pruned_total", "Finished jobs deleted after JOB_RETENTION.")
	ingestDeps := ingest.Deps{
		DB: db, Queue: q, Tenancy: ten, Authz: az,
		Results: res, Identity: identity.New(db), Metrics: reg, Logger: logger,
	}
	jobs := ingest.NewJobs(ingestDeps)
	store, err := objstore.Open(cfg)
	if err != nil {
		logger.Error("boot", "err", err)
		os.Exit(2)
	}
	flk := flaky.New(db, res)
	afterResolve := func(ctx context.Context, projectID string, runID results.RunID) error {
		_, err := flk.RecomputeRun(ctx, projectID, string(runID))
		return err
	}
	// §10.6 shadow meter (QĐ-13): yesterday is recorded once, by the first
	// sweep that finds it missing; counted, never charged.
	meter := billing.New(db, meterSource{query.New(db)})
	art := artifact.New(db, store, res)
	artJobs := artifact.NewJobs(artifact.Deps{DB: db, Tenancy: ten, Authz: az, Results: res, Artifact: art, Metrics: reg, Logger: logger})
	// Deleted projects past their restore window (§11.3.2), a few per sweep.
	idn := identity.New(db)
	// AI-drafted test cases (QĐ-15): the pool always runs — an organization
	// may bring its own key even when the server has none. Pages are read
	// through the URL checker only when it runs (URLCHECK=on).
	var pages aisuggest.PageReader
	if cfg.URLCheck {
		outliner, err := urlcheck.NewHTTPOutliner(cfg.URLCheckURL)
		if err != nil {
			logger.Error("boot", "err", err)
			os.Exit(2)
		}
		pages = outliner
	}
	model := llm.New(cfg.LLMBaseURL)
	ai := aisuggest.New(aisuggest.Deps{DB: db, Pepper: []byte(cfg.TokenPepper), LLM: model, Pages: pages,
		ServerKey: cfg.LLMAPIKey})
	tm := testmgmt.New(db, idn, nil, testmgmt.Policy{})
	// Auto test (QĐ-16): cases run in the checker service's browser
	// sessions, so the pools run with URLCHECK=on; the key and model are the
	// organization's AI settings; results go to plans and test runs.
	var browser autotest.Browser
	if cfg.URLCheck {
		if browser, err = autotest.NewHTTPBrowser(cfg.URLCheckURL); err != nil {
			logger.Error("boot", "err", err)
			os.Exit(2)
		}
	}
	auto := autotest.New(autotest.Deps{DB: db, Pepper: []byte(cfg.TokenPepper), Tenancy: ten, Browser: browser, LLM: model, Keys: ai,
		Recorder: recorder{tm: tm, art: art, tenancy: ten, ingest: ingestDeps, jobs: jobs, results: res, logger: logger}})
	// URL checks (QĐ-14) and site scans (QĐ-16): the pool only with
	// URLCHECK=on; pruning and purging old checks always, so turning checks
	// off keeps nothing forever. A scan as a test account signs in with
	// the account autotest keeps.
	urlChecks := urlcheck.New(db, nil)
	if cfg.URLCheck {
		checker, err := urlcheck.NewHTTPChecker(cfg.URLCheckURL)
		if err != nil {
			logger.Error("boot", "err", err)
			os.Exit(2)
		}
		urlChecks = urlcheck.New(db, checker, scanLogins{auto})
	}
	purge := purger{tenancy: ten, authz: az, artifact: art, triage: triage.New(db, res), flaky: flk, identity: idn,
		testmgmt: tm, results: res, urlcheck: urlChecks, ai: ai, auto: auto}

	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", reg.Handler())
	metric.Pprof(metricsMux) // heap/goroutine profiles on the loopback listener, for T5 leak hunting
	metricsMux.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	}))
	metricsSrv := &http.Server{Addr: cfg.MetricsAddr, Handler: metricsMux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("worker metrics listening", "addr", cfg.MetricsAddr, "env", cfg.Env)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics stopped", "err", err)
			stop()
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w := ai.Worker(aiConcurrency)
		w.Logger, w.Metrics = logger, reg
		logger.Info("worker pool started", "kind", aisuggest.JobDraft, "concurrency", aiConcurrency, "server_key", cfg.LLMAPIKey != "", "reads_pages", pages != nil)
		w.Run(ctx)
	}()
	if cfg.URLCheck {
		pools := []struct {
			w *queue.Worker
			n int
		}{
			{urlChecks.Worker(urlCheckConcurrency), urlCheckConcurrency},
			{auto.CaseWorker(autoCaseConcurrency), autoCaseConcurrency},
			{auto.DraftWorker(autoDraftConcurrency), autoDraftConcurrency},
			{auto.LoginWorker(autoLoginConcurrency), autoLoginConcurrency},
		}
		for _, p := range pools {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p.w.Logger, p.w.Metrics = logger, reg
				logger.Info("worker pool started", "kind", p.w.Kind, "concurrency", p.n, "checker", cfg.URLCheckURL)
				p.w.Run(ctx)
			}()
		}
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		logger.Info("worker pool started", "kind", ingest.JobRunFinished, "concurrency", resolveConcurrency)
		jobs.Worker(resolveConcurrency, afterResolve).Run(ctx)
	}()
	go func() {
		defer wg.Done()
		sweep := func() {
			if n, err := jobs.Sweep(ctx); err != nil {
				logger.Error("sweep", "err", err)
			} else if n > 0 {
				logger.Info("sweep", "auto_closed", n)
			}
			if n, err := flk.Sweep(ctx, flaky.RebuildAfter, 500); err != nil {
				logger.Error("flaky sweep", "err", err)
			} else if n > 0 {
				logger.Info("flaky signals rebuilt from window", "n", n)
			}
			if err := artJobs.Sweep(ctx); err != nil {
				logger.Error("artifact sweep", "err", err)
			}
			yesterday := time.Now().UTC().AddDate(0, 0, -1)
			if done, err := meter.Recorded(ctx, yesterday); err != nil {
				logger.Error("meter", "err", err)
			} else if !done {
				if n, err := meter.Record(ctx, yesterday); err != nil {
					logger.Error("meter", "day", yesterday.Format("2006-01-02"), "err", err)
				} else {
					logger.Info("meter recorded", "day", yesterday.Format("2006-01-02"), "projects", n)
				}
			}
			if n, err := purge.runs(ctx, time.Now(), 50); err != nil {
				logger.Error("run purge", "err", err)
			} else if n > 0 {
				logger.Info("runs purged", "n", n)
			}
			if n, err := purge.run(ctx, time.Now(), 5); err != nil {
				logger.Error("project purge", "err", err)
			} else if n > 0 {
				logger.Info("projects purged", "n", n)
			}
			if n, err := purge.orgs(ctx, time.Now(), 1); err != nil {
				logger.Error("organization purge", "err", err)
			} else if n > 0 {
				logger.Info("organizations purged", "n", n)
			}
			if n, err := ai.Prune(ctx, time.Now().Add(-aisuggest.Retention), 500); err != nil {
				logger.Error("suggestion prune", "err", err)
			} else if n > 0 {
				logger.Info("suggestions pruned", "n", n)
			}
			if n, err := auto.Prune(ctx, time.Now().Add(-autotest.Retention), 500); err != nil {
				logger.Error("auto test prune", "err", err)
			} else if n > 0 {
				logger.Info("auto test drafts and runs pruned", "n", n)
			}
			if n, err := urlChecks.Prune(ctx, time.Now().Add(-urlcheck.Retention), 500); err != nil {
				logger.Error("url check prune", "err", err)
			} else if n > 0 {
				logger.Info("url checks pruned", "n", n)
			}
			// Finished jobs older than JOB_RETENTION go, a bounded batch per sweep
			// (§14.1 T5: the queue table must not grow with history).
			if n, err := q.Prune(ctx, cfg.JobRetention, 5000); err != nil {
				logger.Error("job prune", "err", err)
			} else if n > 0 {
				pruned.Add(float64(n))
				logger.Info("jobs pruned", "n", n)
			}
		}
		sweep()
		t := time.NewTicker(sweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweep()
			}
		}
	}()

	<-ctx.Done()
	logger.Info("worker shutting down")
	wg.Wait() // in-flight handlers finish; their leases would recover them anyway
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = metricsSrv.Shutdown(shutdownCtx)
	db.Close()
}

// meterSource adapts query's read model to billing.Source (§6.9.4: billing
// reads no other module's tables).
type meterSource struct{ q query.Service }

func (m meterSource) MeterFacts(ctx context.Context, day time.Time) ([]billing.Facts, error) {
	rows, err := m.q.MeterFacts(ctx, day)
	out := make([]billing.Facts, 0, len(rows))
	for _, r := range rows {
		out = append(out, billing.Facts{
			ProjectID: r.ProjectID, OrgID: r.OrgID, ActiveTests: r.ActiveTests, RawFingerprints: r.RawFingerprints,
			Merges: r.Merges, UndoMerges: r.UndoMerges, Splits: r.Splits, ReviewDecisions: r.ReviewDecisions,
			Runs: r.Runs, Results: r.Results, ArtifactBytes: r.ArtifactBytes,
		})
	}
	return out, err
}
