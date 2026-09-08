package identity

import (
	"math/rand"
	"testing"
	"time"
)

// TestRecencyGate records finding N45 and measures what the gate costs.
//
// Note the shape: each case builds its own Config. The earlier version mutated
// a package global and restored it with defer, which meant a failing test
// leaked its settings into the next one.
func TestRecencyGate(t *testing.T) {
	id := Identity{Framework: "playwright", FilePath: "tests/auth/login.spec.ts",
		TitlePath: []string{"Auth", "logs in"}}
	renamed := id
	renamed.TitlePath = []string{"Auth", "signs in"}
	now := time.Now()

	resolveAt := func(cfg Config, days int) Decision {
		r := &Resolver{
			Config:  cfg,
			Aliases: AliasStore{id.Fingerprint(""): "st_original"},
			Candidates: []Candidate{{
				StableID: "st_original", Identity: id,
				LastSeenAt: now.Add(-time.Duration(days) * 24 * time.Hour),
			}},
		}
		return r.Resolve(renamed, now).Decision
	}

	noGate := DefaultConfig()
	gated := DefaultConfig().WithRecencyGate(30)

	if got := resolveAt(noGate, 60); got != DecisionAutoMatch {
		t.Errorf("gate off, 60 days stale: got %s, expected auto_match — this is the N45 problem", got)
	}
	if got := resolveAt(gated, 60); got != DecisionReview {
		t.Errorf("30-day gate, 60 days stale: got %s, want review_queue", got)
	}
	if got := resolveAt(gated, 1); got != DecisionAutoMatch {
		t.Errorf("30-day gate, 1 day stale: got %s, want auto_match — the gate must not touch fresh renames", got)
	}
}

// TestRecencyGateCostsNothingOnFreshData measures the gate against the main
// §9.6 benchmark, where every candidate was seen yesterday.
func TestRecencyGateCostsNothingOnFreshData(t *testing.T) {
	run := func(cfg Config) Result {
		rng := rand.New(rand.NewSource(42))
		suite := BaseSuite(500, rng)
		cases := BuildRenameCases(suite, 40, rng)
		return Evaluate(cfg, suite, cases, "", time.Now())
	}
	off := run(DefaultConfig())
	on := run(DefaultConfig().WithRecencyGate(30))

	if on.Recall != off.Recall || on.Precision != off.Precision {
		t.Errorf("gate changed the benchmark: off p=%.4f r=%.4f, on p=%.4f r=%.4f",
			off.Precision, off.Recall, on.Precision, on.Recall)
	}
	t.Logf("30-day gate on fresh candidates: precision %.4f recall %.4f — unchanged",
		on.Precision, on.Recall)
}
