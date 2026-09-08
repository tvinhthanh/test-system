package identity

import (
	"math/rand"
	"testing"
)

// TestPresenceSignalIsLoadBearing locks down finding N34.
//
// The "candidate still present in this run -> score x0.3" signal from §9.3 is
// load-bearing, not a minor tweak. Remove it (or model it wrongly, as the first
// version of this harness did) and recall drops from ~0.99 to ~0.13. This test
// must fail immediately if that happens.
func TestPresenceSignalIsLoadBearing(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	suite := BaseSuite(300, rng)
	cases := BuildRenameCases(suite, 30, rng)
	now := timeNow()

	res := Evaluate(DefaultConfig(), suite, cases, "", now)

	if res.Precision < 0.98 {
		t.Errorf("precision %.4f < 0.98 — wrong merges present, the worst failure mode", res.Precision)
	}
	if res.Recall < 0.80 {
		t.Errorf("recall %.4f < 0.80 — check the PresentInRun signal (N34) before blaming thresholds", res.Recall)
	}
	if res.AutoWrong != 0 {
		t.Errorf("%d wrong merges — §9.6 ranks this above a wrong split", res.AutoWrong)
	}
}

// TestPenaltyMagnitude: the penalty must be strong enough to push a candidate
// below the auto-match threshold even when every other component is perfect.
func TestPenaltyMagnitude(t *testing.T) {
	id := Identity{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "same name"}}
	c := Candidate{StableID: "st_1", Identity: id, LastSeenAt: timeNow()}

	c.PresentInRun = true
	got := DefaultConfig().Score(id, c, timeNow(), "").Adjusted
	if got >= DefaultConfig().ThresholdReview {
		t.Errorf("candidate still in the run scored %.4f >= review threshold %.2f — penalty too weak", got, DefaultConfig().ThresholdReview)
	}
}
