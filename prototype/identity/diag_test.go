package identity

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// TestDiagnoseReconcilesWithEvaluate locks finding N59.
//
// Diagnose exists to explain Evaluate's misses. That only works if both model
// the same run. Diagnose used to leave PresentInRun false on every candidate —
// the N32/N33 harness bug, fixed in Evaluate on 2026-08-26 and never carried
// over — so `-diag` reported 215 cases "blocked by gap" directly under an
// Evaluate report showing 248 auto-matched and 2 in review. Whoever trusted the
// arrow would have loosened MinGap; the two review cases are actually
// score-band misses, and MinGap has nothing to do with them.
//
// The counts must reconcile exactly, on the §9.6 benchmark and on a partial
// corpus, so the reconciliation is not an accident of one seed.
func TestDiagnoseReconcilesWithEvaluate(t *testing.T) {
	for _, seed := range []int64{42, 7, 99} {
		rng := rand.New(rand.NewSource(seed))
		suite := BaseSuite(500, rng)
		cases := BuildRenameCases(suite, SpecRenameSetSize, rng)
		cfg := DefaultConfig()
		now := time.Now()

		ev := Evaluate(cfg, suite, cases, "", now)
		d := Diagnose(cfg, suite, cases, "", now)

		// Eligible minus gap-blocked is what actually auto-matched.
		if got := d.ScoreAboveAuto - d.BlockedByGap; got != ev.AutoMatched {
			t.Errorf("seed %d: diag says %d auto-matched (eligible %d - blocked %d), Evaluate says %d",
				seed, got, d.ScoreAboveAuto, d.BlockedByGap, ev.AutoMatched)
		}
		// Gap-blocked plus score-band review is the whole Review Queue.
		if got := d.BlockedByGap + d.ScoreInReview; got != ev.SentToReview {
			t.Errorf("seed %d: diag says %d in review (blocked %d + band %d), Evaluate says %d",
				seed, got, d.BlockedByGap, d.ScoreInReview, ev.SentToReview)
		}
		if d.ScoreBelowReview != ev.NewIdentity {
			t.Errorf("seed %d: diag says %d new identities, Evaluate says %d",
				seed, d.ScoreBelowReview, ev.NewIdentity)
		}
		if d.Total != ev.Total {
			t.Errorf("seed %d: diag total %d, Evaluate total %d", seed, d.Total, ev.Total)
		}
	}
}

// TestDiagnoseWithoutPresenceIsTheOldBug demonstrates what the broken Diagnose
// was measuring, so the number 215 in N59 stays reproducible: score every
// candidate as absent and the gap rule blocks the large majority. That is the
// no-presence world N34 characterised, not the §9.6 benchmark.
func TestDiagnoseWithoutPresenceIsTheOldBug(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	suite := BaseSuite(500, rng)
	cases := BuildRenameCases(suite, SpecRenameSetSize, rng)
	cfg := DefaultConfig()
	now := time.Now()

	aliases := AliasStore{}
	cands := make([]Candidate, 0, len(suite))
	for i, id := range suite {
		sid := fmt.Sprintf("st_%04d", i)
		aliases[id.Fingerprint("")] = sid
		// PresentInRun deliberately left false for everyone — the old harness.
		cands = append(cands, Candidate{StableID: sid, Identity: id, LastSeenAt: now.Add(-24 * time.Hour)})
	}
	r := &Resolver{Aliases: aliases, Candidates: cands, Config: cfg}

	blocked := 0
	for _, c := range cases {
		got := r.Resolve(c.After, now)
		if got.Decision != DecisionExactMatch && got.Score >= cfg.ThresholdAutoMatch && got.Gap < cfg.MinGap {
			blocked++
		}
	}
	// Loose bound on purpose: the claim is "most", not a digit.
	if blocked < len(cases)/2 {
		t.Errorf("expected the no-presence world to gap-block most cases, got %d/%d — "+
			"if this dropped, N34's mechanism changed and N59's diagnosis needs re-reading", blocked, len(cases))
	}
	t.Logf("no-presence world: %d/%d gap-blocked (the figure the old -diag printed as if it were real)", blocked, len(cases))
}
