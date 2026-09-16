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
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	platlog "github.com/vinh/automation-testing-platform/backend/internal/platform/log"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
	_ "github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory" // OBJSTORE=memory (dev)
	_ "github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/s3"     // OBJSTORE=s3
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

const (
	resolveConcurrency = 4
	sweepEvery         = time.Minute
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
	reg.GaugeFunc("go_goroutines", "Number of goroutines.", func() float64 { return float64(runtime.NumGoroutine()) })

	ten, az, res := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper)), results.New(db)
	jobs := ingest.NewJobs(ingest.Deps{
		DB: db, Queue: queue.New(db), Tenancy: ten, Authz: az,
		Results: res, Identity: identity.New(db), Metrics: reg, Logger: logger,
	})
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
	artJobs := artifact.NewJobs(artifact.Deps{DB: db, Tenancy: ten, Authz: az, Results: res, Artifact: artifact.New(db, store, res), Metrics: reg, Logger: logger})

	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", reg.Handler())
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
			if err := artJobs.Sweep(ctx); err != nil {
				logger.Error("artifact sweep", "err", err)
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
