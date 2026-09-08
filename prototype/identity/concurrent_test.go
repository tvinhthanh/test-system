package identity

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestConcurrentIdentityCreation is the race test SPEC §9.7 marks as mandatory.
//
// Two shards of one run, or two runs at once, can meet the same unknown
// fingerprint within milliseconds. Without the unique constraint each mints its
// own identity and the test's history splits in half — silently.
func TestConcurrentIdentityCreation(t *testing.T) {
	const (
		fingerprints = 50
		workers      = 16
	)
	fps := make([]string, fingerprints)
	for i := range fps {
		fps[i] = fmt.Sprintf("fp1:race%03d", i)
	}

	safe := NewSafeIdentityStore()
	var wg sync.WaitGroup
	results := make([][]string, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			got := make([]string, len(fps))
			for i, fp := range fps {
				id, _ := safe.GetOrCreate(fp)
				got[i] = id
			}
			results[w] = got
		}(w)
	}
	wg.Wait()

	created, conflicts, distinct := safe.Stats()
	if created != fingerprints {
		t.Errorf("minted %d identities for %d fingerprints — want exactly one each",
			created, fingerprints)
	}
	if distinct != fingerprints {
		t.Errorf("%d distinct fingerprints stored, want %d", distinct, fingerprints)
	}
	// Every worker must agree on every identity. Disagreement means a test's
	// history would be split across two identities.
	for i := 0; i < fingerprints; i++ {
		want := results[0][i]
		for w := 1; w < workers; w++ {
			if results[w][i] != want {
				t.Fatalf("workers disagree on %s: worker 0 got %s, worker %d got %s",
					fps[i], want, w, results[w][i])
			}
		}
	}
	t.Logf("%d workers x %d fingerprints: %d identities minted, %d races lost and read back",
		workers, fingerprints, created, conflicts)
}

// TestDeletedThenReaddedTest covers the §9.7 row about a test removed and
// restored months later — and records finding N45, where measurement disagrees
// with what §9.7 claims.
func TestDeletedThenReaddedTest(t *testing.T) {
	id := Identity{
		Framework: "playwright",
		FilePath:  "tests/auth/login.spec.ts",
		TitlePath: []string{"Auth", "logs in"},
	}
	now := time.Now()
	mk := func(days int) *Resolver {
		return &Resolver{
			Aliases: AliasStore{id.Fingerprint(""): "st_original"},
			Candidates: []Candidate{{
				StableID: "st_original", Identity: id,
				LastSeenAt: now.Add(-time.Duration(days) * 24 * time.Hour),
			}},
		}
	}

	// Identical fingerprint reconnects regardless of age — §9.7 is right here.
	if got := mk(60).Resolve(id, now); got.Decision != DecisionExactMatch || got.MatchedID != "st_original" {
		t.Errorf("re-added test with an identical fingerprint should exact-match: %+v", got)
	}

	// Re-added with a reworded title. §9.7 says low S_recency means this
	// "usually becomes a new identity". It does not — recency is only 10% of
	// the score and cannot outweigh a same-file, same-describe match.
	renamed := id
	renamed.TitlePath = []string{"Auth", "signs in"}

	at60 := mk(60).Resolve(renamed, now)
	if at60.Decision != DecisionAutoMatch {
		t.Errorf("N45 no longer reproduces at 60 days (got %s) — re-verify before editing §9.7", at60.Decision)
	}
	at365 := mk(365).Resolve(renamed, now)
	if at365.Decision == DecisionNew {
		t.Errorf("unexpected: a year-stale candidate produced a new identity (score %.4f)", at365.Score)
	}
	t.Logf("60 days:  %s (score %.4f)", at60.Decision, at60.Score)
	t.Logf("365 days: %s (score %.4f) — never reaches the 'new identity' band below 0.60",
		at365.Decision, at365.Score)
}
