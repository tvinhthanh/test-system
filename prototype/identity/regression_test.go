package identity

import "testing"

func TestOldMappingWouldHaveLostTimeout(t *testing.T) {
	// Reproduce the pre-fix mapping: test.status only.
	oldMap := func(x pwTest) string {
		switch x.Status {
		case "expected":
			if x.ExpectedStatus == "skipped" {
				return "skipped"
			}
			return "passed"
		case "unexpected":
			return "failed"
		case "flaky":
			return "flaky_pass"
		case "skipped":
			return "skipped"
		}
		return "interrupted"
	}
	timeout := pwTest{Status: "unexpected", ExpectedStatus: "passed",
		Results: []pwResult{{Status: "timedOut"}, {Status: "timedOut"}, {Status: "timedOut"}}}
	if oldMap(timeout) != "failed" {
		t.Fatal("premise wrong")
	}
	if got := mapStatus(timeout); got != "timed_out" {
		t.Errorf("mapStatus = %q, want timed_out", got)
	}
	t.Logf("old mapping: %q  ->  fixed mapping: %q", oldMap(timeout), mapStatus(timeout))
}
