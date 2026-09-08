package identity

import (
	"testing"
	"time"
)

func day(n int) time.Time { return time.Date(2026, 8, n, 12, 0, 0, 0, time.UTC) }

// TestSplitPreservesHistoryBeforeCutoff is the core N14 property.
//
// A wrong auto-match on day 10 fused two tests. Splitting from day 20 must
// leave days 1-19 attributed to the original identity and days 20+ to the new
// one. Rewriting the alias in place would silently reattribute the whole
// history, which is exactly the corruption the split exists to undo.
func TestSplitPreservesHistoryBeforeCutoff(t *testing.T) {
	s := NewIdentityStore("proj")
	s.Bind("fp:checkout", "st_A", SourceExact, day(1))

	if err := s.SplitFrom("fp:checkout", "st_B", "reviewer@x", day(20)); err != nil {
		t.Fatalf("split failed: %v", err)
	}

	cases := []struct {
		when time.Time
		want string
	}{
		{day(1), "st_A"},
		{day(19), "st_A"},
		{day(20), "st_B"}, // cutoff is inclusive for the new owner
		{day(25), "st_B"},
	}
	for _, c := range cases {
		got, ok := s.ResolveAt("fp:checkout", c.when)
		if !ok {
			t.Errorf("%s: no alias resolved", c.when.Format("Jan 2"))
			continue
		}
		if got != c.want {
			t.Errorf("%s: resolved to %s, want %s", c.when.Format("Jan 2"), got, c.want)
		}
	}

	// The hot path must see only the new owner.
	if now, _ := s.ResolveNow("fp:checkout"); now != "st_B" {
		t.Errorf("ResolveNow = %s, want st_B", now)
	}
	// §8.5's partial unique index: at most one alias in force.
	if n := s.ActiveAliasCount("fp:checkout"); n != 1 {
		t.Errorf("%d active aliases, want exactly 1 — violates the partial unique index", n)
	}
	if len(s.HistoryFor("fp:checkout")) != 2 {
		t.Errorf("history should show both intervals, got %d", len(s.HistoryFor("fp:checkout")))
	}
}

// TestMergeFollowsChain: merging retires an identity, and anything still
// pointing at it must resolve through to the survivor.
func TestMergeFollowsChain(t *testing.T) {
	s := NewIdentityStore("proj")
	s.Bind("fp:a", "st_A", SourceExact, day(1))
	s.Bind("fp:b", "st_B", SourceExact, day(1))
	s.Bind("fp:c", "st_C", SourceExact, day(1))

	if err := s.Merge("st_A", "st_B", "reviewer@x", day(5)); err != nil {
		t.Fatal(err)
	}
	// Chained merge: C into B, which is already retired into A.
	if err := s.Merge("st_B", "st_C", "reviewer@x", day(6)); err == nil {
		t.Error("merging into a retired identity should be refused")
	}
	if err := s.Merge("st_A", "st_C", "reviewer@x", day(6)); err != nil {
		t.Fatal(err)
	}

	for _, fp := range []string{"fp:a", "fp:b", "fp:c"} {
		got, ok := s.ResolveNow(fp)
		if !ok || got != "st_A" {
			t.Errorf("%s resolved to %q, want st_A", fp, got)
		}
	}
	if len(s.Audit) != 2 {
		t.Errorf("want 2 audit entries, got %d — §9.4 requires an audit trail", len(s.Audit))
	}
}

// TestSplitRejectsImpossibleCutoffs guards the operator against corrupting
// history through a bad split.
func TestSplitRejectsImpossibleCutoffs(t *testing.T) {
	s := NewIdentityStore("proj")
	s.Bind("fp:x", "st_A", SourceExact, day(10))

	if err := s.SplitFrom("fp:x", "st_B", "r", day(5)); err == nil {
		t.Error("split before the alias even started should be refused")
	}
	if err := s.SplitFrom("fp:x", "st_A", "r", day(15)); err == nil {
		t.Error("splitting into the current owner should be refused")
	}
	if err := s.SplitFrom("fp:missing", "st_B", "r", day(15)); err == nil {
		t.Error("split on an unknown fingerprint should be refused")
	}
	if n := s.ActiveAliasCount("fp:x"); n != 1 {
		t.Errorf("failed splits left %d active aliases, want 1", n)
	}
}

// TestUndoWrongAutoMatch walks the whole recovery story end to end: a wrong
// auto-match, discovered later, undone without losing the good history.
func TestUndoWrongAutoMatch(t *testing.T) {
	s := NewIdentityStore("proj")
	// Two genuinely different tests, correctly separate at first.
	s.Bind("fp:login", "st_login", SourceExact, day(1))
	s.Bind("fp:logout", "st_logout", SourceExact, day(1))

	// Day 10: the matcher wrongly decides fp:logout is a rename of st_login.
	if err := s.Merge("st_login", "st_logout", "auto", day(10)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ResolveNow("fp:logout"); got != "st_login" {
		t.Fatalf("premise wrong: fp:logout resolves to %s", got)
	}

	// Day 30: a reviewer notices and splits it back out.
	if err := s.SplitFrom("fp:logout", "st_logout_v2", "reviewer@x", day(30)); err != nil {
		t.Fatal(err)
	}

	// From day 30 the two tests are separate again.
	if got, _ := s.ResolveNow("fp:logout"); got != "st_logout_v2" {
		t.Errorf("after split, fp:logout resolves to %s, want st_logout_v2", got)
	}
	if got, _ := s.ResolveNow("fp:login"); got != "st_login" {
		t.Errorf("fp:login should be untouched, got %s", got)
	}

	// Honest limitation, worth asserting so nobody assumes otherwise: days 10-29
	// stay attributed to st_login. The split fixes the future, not the past.
	// Reattributing those runs needs a separate operation — see UndoMerge and
	// finding N43.
	if got, _ := s.ResolveAt("fp:logout", day(20)); got != "st_login" {
		t.Errorf("day 20 resolved to %s; SplitFrom is expected to leave this window alone", got)
	}
	t.Logf("audit trail:")
	for _, a := range s.Audit {
		t.Logf("  %s", a)
	}
}

// TestSplitIsNotAnUndo records finding N43.
//
// §9.4 calls SplitFrom "the undo for a wrong auto-match". It is not one.
// Merge is retroactive by design — unifying the full history is the point when
// a genuine rename was missed — so undoing it needs its own operation.
func TestSplitIsNotAnUndo(t *testing.T) {
	build := func() *IdentityStore {
		s := NewIdentityStore("proj")
		s.Bind("fp:logout", "st_logout", SourceExact, day(1))
		_ = s.Merge("st_login", "st_logout", "auto", day(10)) // the wrong merge
		return s
	}

	// Recovery the way §9.4 currently prescribes.
	viaSplit := build()
	_ = viaSplit.SplitFrom("fp:logout", "st_logout_v2", "reviewer@x", day(30))
	corrupted := 0
	for d := 1; d < 30; d++ {
		if got, _ := viaSplit.ResolveAt("fp:logout", day(d)); got != "st_logout" {
			corrupted++
		}
	}

	// Recovery with a purpose-built undo.
	viaUndo := build()
	if err := viaUndo.UndoMerge("st_logout", "reviewer@x", day(30)); err != nil {
		t.Fatal(err)
	}
	stillWrong := 0
	for d := 1; d < 30; d++ {
		if got, _ := viaUndo.ResolveAt("fp:logout", day(d)); got != "st_logout" {
			stillWrong++
		}
	}

	t.Logf("SplitFrom(day 30) : %2d/29 days still attributed to the wrong test", corrupted)
	t.Logf("UndoMerge         : %2d/29 days still attributed to the wrong test", stillWrong)

	if corrupted == 0 {
		t.Error("premise wrong: SplitFrom was expected to leave a corrupted window")
	}
	if stillWrong != 0 {
		t.Errorf("UndoMerge left %d days wrong — it must restore the prior state in full", stillWrong)
	}
}
