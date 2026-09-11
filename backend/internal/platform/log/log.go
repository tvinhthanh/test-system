// Package log builds the process-wide structured logger (§6.4).
//
// It wraps log/slog rather than a third-party library: the requirement is
// structured output with a correlation id, and slog already does that. Import
// it as platlog to avoid shadowing the standard library's log package.
package log

import (
	"log/slog"
	"os"
)

// New returns a logger whose format depends on the environment: human-readable
// text in dev, JSON everywhere else so a log shipper can index it.
func New(env string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if env == "dev" {
		opts.Level = slog.LevelDebug
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
