// Command migrate applies pending schema migrations and exits. It runs
// separately from cmd/api (§6.9.2) so a schema change is a deliberate deploy
// step with its own log line, not something that happens because a replica
// restarted.
//
// Exit codes: 0 applied (or nothing to do), 1 migration failed, 2 bad config.
package main

import (
	"context"
	"os"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	platlog "github.com/vinh/automation-testing-platform/backend/internal/platform/log"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(2)
	}
	logger := platlog.New(cfg.Env)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	db, err := pg.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("migrate", "err", err)
		os.Exit(2)
	}
	defer db.Close()

	applied, err := pg.Migrate(ctx, db, migrations.FS)
	for _, name := range applied {
		logger.Info("migration applied", "name", name)
	}
	if err != nil {
		logger.Error("migrate failed", "err", err, "applied_before_failure", len(applied))
		os.Exit(1)
	}
	logger.Info("migrate done", "applied", len(applied))
}
