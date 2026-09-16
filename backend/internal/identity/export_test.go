package identity

import (
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity/internal/match"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// NewForTest is New with an injectable clock, so alias intervals can be
// placed on specific days.
func NewForTest(db *pg.DB, now func() time.Time) Service {
	return &service{db: db, cfg: match.DefaultConfig(), now: now}
}

// TrigramSimilarityForTest exposes the Go side of the N49 parity check.
func TrigramSimilarityForTest(a, b string) float64 { return match.TrigramSimilarity(a, b) }
