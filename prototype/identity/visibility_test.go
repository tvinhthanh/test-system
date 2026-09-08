package identity

import (
	"math/rand"
	"testing"
)

// TestPartialVisibilityIsSafeButUseless locks down finding N39.
//
// When the resolver can only see part of a run — a shard, a --grep subset, or
// simply an early ingest batch — tests that have not reported YET are
// indistinguishable from tests that have DISAPPEARED.
//
// Two things must hold, and both are load-bearing:
//
//  1. Precision must NOT degrade. The gap rule is the safety net here: when many
//     candidates look absent they all score alike, no one stands clear, and
//     auto-match declines to fire. Wrong merges never happen.
//  2. Recall DOES collapse, and that is why fuzzy resolution has to be deferred
//     to run completion rather than run inline with ingest (§7.6).
func TestPartialVisibilityIsSafeButUseless(t *testing.T) {
	results := VisibilitySweep(500, 40, 42, "")

	var full, half VisibilityResult
	for _, r := range results {
		switch r.VisibleFraction {
		case 1.00:
			full = r
		case 0.50:
			half = r
		}
	}

	// Safety property: never trade precision for visibility.
	for _, r := range results {
		if r.AutoWrong != 0 {
			t.Errorf("visibility %.2f produced %d wrong merges — the gap rule is no longer protecting us",
				r.VisibleFraction, r.AutoWrong)
		}
		if r.Precision < 0.98 {
			t.Errorf("visibility %.2f dropped precision to %.4f, below the §9.6 floor",
				r.VisibleFraction, r.Precision)
		}
	}

	// The cost, which is what forces two-stage resolution.
	if half.Recall >= full.Recall*0.6 {
		t.Errorf("expected recall to collapse under partial visibility; full=%.4f half=%.4f — "+
			"re-verify N39 before relaxing the deferred-resolution rule", full.Recall, half.Recall)
	}
	t.Logf("full visibility recall %.4f -> half visibility recall %.4f", full.Recall, half.Recall)
}

// TestDeferredResolutionRecoversRecall shows the fix: resolve fuzzy cases once
// the run is complete and accuracy returns to the full-visibility numbers.
func TestDeferredResolutionRecoversRecall(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	suite := BaseSuite(500, rng)
	cases := BuildRenameCases(suite, 40, rng)

	inline := EvaluateWithVisibility(DefaultConfig(), suite, cases, 0.5, "", timeRef(), rand.New(rand.NewSource(8)))
	deferred := EvaluateWithVisibility(DefaultConfig(), suite, cases, 1.0, "", timeRef(), rand.New(rand.NewSource(8)))

	if deferred.Recall <= inline.Recall {
		t.Fatalf("deferring did not help: inline=%.4f deferred=%.4f", inline.Recall, deferred.Recall)
	}
	t.Logf("inline (50%% visible) recall %.4f -> deferred (run complete) recall %.4f",
		inline.Recall, deferred.Recall)
}
