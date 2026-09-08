package identity

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// BenchmarkExactMatch is the hot path: >90% of events, and the one that must
// stay inline with ingest under §7.6's p50 < 1s budget.
func BenchmarkExactMatch(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		r, suite := buildStore(n)
		now := time.Now()
		b.Run(fmt.Sprintf("suite=%d", n), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r.Resolve(suite[i%len(suite)], now)
			}
		})
	}
}

// BenchmarkFuzzyResolve is the cold path: unknown fingerprints, which N39
// moved to run.finish. Slower is acceptable there, but it must not be absurd.
func BenchmarkFuzzyResolve(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		r, suite := buildStore(n)
		rng := rand.New(rand.NewSource(11))
		now := time.Now()
		// Pre-build renamed identities so mutation cost is outside the timer.
		renamed := make([]Identity, 200)
		for i := range renamed {
			renamed[i] = Mutate(suite[i%len(suite)], MutateLeafTitle, rng)
		}
		b.Run(fmt.Sprintf("suite=%d", n), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r.Resolve(renamed[i%len(renamed)], now)
			}
		})
	}
}

// TestTwoStageLatencyBudget checks the two stages §7.6 defines, separately.
//
// The earlier version of this test summed both stages and compared the total
// against stage 1's p50 < 1s. That was wrong twice over. §7.6 budgets 1s for
// stage 1 only — the inline exact-match path; stage 2 runs at run.finish and
// carries no such deadline, which is the whole point of the split N39 forced.
// And measured, the sum sat at 758-796ms against a 1s ceiling: a gate with 20%
// headroom over a number that moves ±15% between runs is a flaky test, in a
// prototype for a product about test flakiness.
func TestTwoStageLatencyBudget(t *testing.T) {
	const suiteSize = 10000
	r, suite := buildStore(suiteSize)
	rng := rand.New(rand.NewSource(13))
	now := time.Now()

	// A realistic run: every test reports, plus 10 renames (§9.7's "< 10 unknown
	// fingerprints in a normal run").
	renamed := make([]Identity, 10)
	for i := range renamed {
		renamed[i] = Mutate(suite[i*97%len(suite)], MutateLeafTitle, rng)
	}

	start := time.Now()
	for _, id := range suite {
		r.Resolve(id, now)
	}
	stage1 := time.Since(start)

	start = time.Now()
	for _, id := range renamed {
		r.Resolve(id, now)
	}
	stage2 := time.Since(start)

	t.Logf("suite of %d tests, 10 renames", suiteSize)
	t.Logf("  stage 1 (inline, exact match) : %v for %d events (%.1f µs/event)",
		stage1.Round(time.Millisecond), suiteSize,
		float64(stage1.Microseconds())/float64(suiteSize))
	t.Logf("  stage 2 (run.finish, fuzzy)   : %v for %d events (%.1f ms/event)",
		stage2.Round(time.Millisecond), len(renamed),
		float64(stage2.Milliseconds())/float64(len(renamed)))

	// Stage 1 is the one §7.6 puts a number on. It resolves a whole 10k run in
	// roughly 10ms, so the budget is not close — but this is the assertion that
	// belongs to §7.6, and it fires if the exact-match path ever stops being a
	// map lookup.
	if stage1 > time.Second {
		t.Errorf("stage 1 resolved a %d-test run in %v, over §7.6's inline budget of 1s", suiteSize, stage1)
	}

	// Stage 2 has no spec deadline. This ceiling is deliberately loose: the
	// prototype does an unindexed linear scan (N47), so the measured ~780ms is
	// an upper bound that a GIN index removes. 5s only trips on an algorithmic
	// regression, not on a slow machine.
	if budget := 5 * time.Second; stage2 > budget {
		t.Errorf("stage 2 took %v for %d unknown fingerprints, past the %v regression ceiling",
			stage2, len(renamed), budget)
	}
}
