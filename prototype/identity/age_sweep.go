package identity

import (
	"fmt"
	"strings"
	"time"
)

// AgeSweep measures how candidate staleness affects the decision — the §9.7
// claim that a test deleted and re-added months later "usually becomes a new
// identity" because S_recency is low.
func AgeSweep() string {
	id := Identity{Framework: "playwright", FilePath: "tests/auth/login.spec.ts",
		TitlePath: []string{"Auth", "logs in"}}
	renamed := id
	renamed.TitlePath = []string{"Auth", "signs in"}
	now := time.Now()

	var b strings.Builder
	fmt.Fprintf(&b, "CANDIDATE STALENESS vs DECISION\n")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("=", 56))
	fmt.Fprintf(&b, "Same file, same describe, reworded leaf. Only age varies.\n")
	fmt.Fprintf(&b, "S_recency carries weight %.2f of the total score.\n\n", DefaultConfig().WeightRecency)
	fmt.Fprintf(&b, "  %-10s %10s %10s %-14s\n", "age (days)", "S_recency", "score", "decision")
	for _, days := range []int{1, 7, 30, 60, 90, 180, 365} {
		r := &Resolver{
			Aliases: AliasStore{id.Fingerprint(""): "st_original"},
			Candidates: []Candidate{{
				StableID:   "st_original",
				Identity:   id,
				LastSeenAt: now.Add(-time.Duration(days) * 24 * time.Hour),
			}},
		}
		got := r.Resolve(renamed, now)
		rec := DefaultConfig().ScoreRecency(now.Add(-time.Duration(days)*24*time.Hour), now)
		fmt.Fprintf(&b, "  %-10d %10.4f %10.4f %-14s\n", days, rec, got.Score, got.Decision)
	}
	fmt.Fprintf(&b, "\nEven at S_recency = 0 the score cannot fall far: recency is 10%% of\n")
	fmt.Fprintf(&b, "the total, so it can only break ties, never veto a strong match.\n")
	return b.String()
}
