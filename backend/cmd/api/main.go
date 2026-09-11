// Command api is the HTTP server for the /v1 API (§6.5, §6.9.2).
//
// It owns process lifecycle only: load config, build the logger, wire
// middleware, listen, and shut down cleanly on SIGINT/SIGTERM. Business
// handlers arrive as domain modules are built (§6.9.11, P1-min column) and
// are mounted here — cmd/* is the one place allowed to import everything
// (§6.9.3).
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	platlog "github.com/vinh/automation-testing-platform/backend/internal/platform/log"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// No logger yet — config is what tells us how to build one.
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(2)
	}
	logger := platlog.New(cfg.Env)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", httpx.Health())

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpx.RequestID(httpx.Logging(logger)(mux)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api stopped", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("api shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
		os.Exit(1)
	}
}
