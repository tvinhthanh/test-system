package identity

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// TestHotPathShare measures how many events the exact-match path absorbs.
// §9.3 claims >99%. If true, deferring only the fuzzy path costs almost nothing.
func TestHotPathShare(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	suite := BaseSuite(500, rng)
	now := time.Now()

	aliases := AliasStore{}
	cands := make([]Candidate, 0, len(suite))
	for i, id := range suite {
		sid := fmt.Sprintf("st_%04d", i)
		aliases[id.Fingerprint("")] = sid
		cands = append(cands, Candidate{StableID: sid, Identity: id, LastSeenAt: now.Add(-24 * time.Hour)})
	}
	r := &Resolver{Aliases: aliases, Candidates: cands}

	// A realistic run: every test reports again, plus a handful of renames.
	renamed := BuildRenameCases(suite, 10, rng)
	exact, fuzzy := 0, 0
	for _, id := range suite {
		if r.Resolve(id, now).Decision == DecisionExactMatch {
			exact++
		} else {
			fuzzy++
		}
	}
	for _, c := range renamed {
		if r.Resolve(c.After, now).Decision == DecisionExactMatch {
			exact++
		} else {
			fuzzy++
		}
	}
	total := exact + fuzzy
	t.Logf("exact-match (inline, no presence needed) : %d/%d = %.2f%%", exact, total, 100*float64(exact)/float64(total))
	t.Logf("fuzzy      (needs full-run presence)     : %d/%d = %.2f%%", fuzzy, total, 100*float64(fuzzy)/float64(total))
	if float64(exact)/float64(total) < 0.90 {
		t.Errorf("hot path only absorbs %.1f%% — deferring the fuzzy path would be expensive", 100*float64(exact)/float64(total))
	}
}
