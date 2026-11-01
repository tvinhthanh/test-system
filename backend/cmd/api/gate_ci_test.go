package main

import (
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
)

// The CI answer (21-10 review, finding 4): a person's decision wins over the
// gate; an open release goes only on an evaluated approved/warn gate.
func TestCIVerdict(t *testing.T) {
	gate := func(v testmgmt.GateVerdict) *testmgmt.GateResult { return &testmgmt.GateResult{Verdict: v} }
	for _, c := range []struct {
		name   string
		status testmgmt.ReleaseStatus
		gate   *testmgmt.GateResult
		pass   bool
	}{
		{"open, not evaluated", testmgmt.ReleaseOpen, nil, false},
		{"open, no evidence", testmgmt.ReleaseOpen, gate(testmgmt.VerdictNoEvidence), false},
		{"open, gate blocked", testmgmt.ReleaseOpen, gate(testmgmt.VerdictBlocked), false},
		{"open, gate warn", testmgmt.ReleaseOpen, gate(testmgmt.VerdictWarn), true},
		{"open, gate approved", testmgmt.ReleaseOpen, gate(testmgmt.VerdictApproved), true},
		{"blocked by a person over an approved gate", testmgmt.ReleaseBlocked, gate(testmgmt.VerdictApproved), false},
		{"blocked by a person, not evaluated", testmgmt.ReleaseBlocked, nil, false},
		{"approved over a blocked gate (named override)", testmgmt.ReleaseApproved, gate(testmgmt.VerdictBlocked), true},
		{"approved, not evaluated", testmgmt.ReleaseApproved, nil, true},
		{"shipped", testmgmt.ReleaseShipped, gate(testmgmt.VerdictWarn), true},
	} {
		got := ciVerdict(testmgmt.Release{Status: c.status, Gate: c.gate, DecidedBy: "carol@example.com"})
		if got.Pass != c.pass || got.Reason == "" {
			t.Errorf("%s: %+v, want pass=%v with a reason", c.name, got, c.pass)
		}
	}
}
