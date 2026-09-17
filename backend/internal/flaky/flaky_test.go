package flaky_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

func hist(now time.Time, statuses ...results.FinalStatus) []results.HistoryPoint {
	out := make([]results.HistoryPoint, 0, len(statuses))
	for i, s := range statuses {
		at := now.Add(-time.Duration(len(statuses)-i) * 24 * time.Hour)
		p := results.HistoryPoint{RunID: results.RunID(fmt.Sprint(i)), StartedAt: at, FinalStatus: s}
		if s == results.FinalFailed || s == results.FinalTimedOut || s == results.FinalInterrupted {
			p.ObservedAt = at
		}
		out = append(out, p)
	}
	return out
}

// TestFormulaIsSection86 pins the §8.6 formula case by case: N05's
// division guard, the three weights, skipped results carrying no signal,
// flaky_pass counting as a pass in the flip series.
func TestFormulaIsSection86(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	P, F, FP, S := results.FinalPassed, results.FinalFailed, results.FinalFlakyPass, results.FinalSkipped

	for name, c := range map[string]struct {
		h       []results.HistoryPoint
		total   int
		flips   int
		score   float64
		verdict flaky.Verdict
	}{
		"no data":           {nil, 0, 0, 0, flaky.VerdictInsufficientData},
		"one run":           {hist(now, F), 1, 0, 0, flaky.VerdictInsufficientData},
		"skips only":        {hist(now, S, S, S), 0, 0, 0, flaky.VerdictInsufficientData},
		"always passes":     {hist(now, P, P, P, P, P), 5, 0, 0, flaky.VerdictStable},
		"broke and stayed":  {hist(now, P, P, F, F, F), 5, 1, 0.5*1.0/4 + 0.2*(1-1.0/30), flaky.VerdictSuspect},
		"flip every run":    {hist(now, P, F, P, F, P, F), 6, 5, 0.5 + 0.2*(1-1.0/30), flaky.VerdictFlaky},
		"flaky passes":      {hist(now, FP, FP, FP, FP), 4, 0, 0.3, flaky.VerdictSuspect},
		"skips are ignored": {hist(now, P, S, F, S, P), 3, 2, 0.5 + 0.2*(1-3.0/30), flaky.VerdictFlaky},
		"old failure fades": {hist(now.Add(-40*24*time.Hour), P, F, P), 3, 2, 0.5, flaky.VerdictSuspect},
		// Pure alternation tops out at 0.7 (0.5 + 0.2): quarantine needs flaky
		// passes on top, and at least ten runs.
		"quarantine":         {hist(now, F, FP, F, FP, F, FP, F, FP, F, FP, F, FP), 12, 11, 0.5 + 0.3*6.0/12 + 0.2*(1-2.0/30), flaky.VerdictQuarantine},
		"too few to quarant": {hist(now, F, FP, F, FP), 4, 3, 0.5 + 0.3*2.0/4 + 0.2*(1-2.0/30), flaky.VerdictFlaky},
	} {
		st := flaky.Compute(c.h, now)
		if st.TotalRuns != c.total || st.FlipCount != c.flips || st.Verdict != c.verdict || math.Abs(st.Score-round3(c.score)) > 1e-9 {
			t.Errorf("%s: got total=%d flips=%d score=%.3f verdict=%s; want total=%d flips=%d score=%.3f verdict=%s",
				name, st.TotalRuns, st.FlipCount, st.Score, st.Verdict, c.total, c.flips, round3(c.score), c.verdict)
		}
	}
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

const (
	project = "11111111-1111-4111-8111-111111111111"
	envA    = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	envB    = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeee2"
	ident   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

func TestRecomputeAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	res := results.New(db)
	svc := flaky.New(db, res)
	ctx := context.Background()
	now := time.Now().UTC()

	// Five runs in env A, alternating pass/fail for one identity, plus a
	// run in env B: histories are per environment (§8.2).
	statuses := []struct {
		s   results.AttemptStatus
		o   results.Outcome
		env string
	}{
		{results.AttemptPassed, results.OutcomeExpected, envA},
		{results.AttemptFailed, results.OutcomeUnexpected, envA},
		{results.AttemptPassed, results.OutcomeExpected, envA},
		{results.AttemptFailed, results.OutcomeUnexpected, envA},
		{results.AttemptPassed, results.OutcomeFlaky, envA},
		{results.AttemptPassed, results.OutcomeExpected, envB},
	}
	var lastRun results.Run
	for i, st := range statuses {
		run, _, err := res.CreateRun(ctx, results.NewRun{ProjectID: project, EnvironmentID: st.env, LocalRunID: fmt.Sprintf("0198c3e1-7b1a-7c3d-9e4f-0000000000%02d", i), StartedAt: now.Add(-time.Duration(10-i) * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.AppendAttempts(ctx, run.ID, []results.AttemptEvent{{
			Sequence: 1, ClientTestID: "ct_x", DisplayName: "x", IdentityRaw: json.RawMessage(`{"framework":"playwright","filePath":"a.spec.ts","titlePath":["x"]}`),
			Status: st.s, Outcome: st.o,
		}}); err != nil {
			t.Fatal(err)
		}
		if _, err := res.SetStableIDs(ctx, run.ID, map[string]string{"ct_x": ident}); err != nil {
			t.Fatal(err)
		}
		if _, err := res.FinishRun(ctx, run.ID, now.Add(-time.Duration(10-i)*time.Hour+time.Minute)); err != nil {
			t.Fatal(err)
		}
		lastRun = run
	}

	n, err := svc.RecomputeRun(ctx, project, string(lastRun.ID)) // env B run
	if err != nil || n != 1 {
		t.Fatalf("recompute env B: n=%d err=%v", n, err)
	}
	b, err := svc.Get(ctx, ident, envB)
	if err != nil || b.TotalRuns != 1 || b.Verdict != flaky.VerdictInsufficientData || b.HasScore {
		t.Errorf("env B: %+v %v", b, err)
	}
	a, err := svc.Recompute(ctx, project, ident, envA)
	if err != nil {
		t.Fatal(err)
	}
	if a.TotalRuns != 5 || a.FailedRuns != 2 || a.FlakyPassRuns != 1 || a.FlipCount != 4 || !a.HasScore {
		t.Errorf("env A: %+v", a)
	}
	// 0.5·4/4 + 0.3·1/5 + 0.2·recency(fail 6 h ago ≈ 1) ≈ 0.76 → flaky.
	if a.Score < 0.74 || a.Score > 0.77 || a.Verdict != flaky.VerdictFlaky {
		t.Errorf("env A score %.3f verdict %s", a.Score, a.Verdict)
	}
	if a.LastFailedAt.IsZero() {
		t.Error("last_failed_at not set")
	}
	worst, err := svc.ListWorst(ctx, project, 10)
	if err != nil || len(worst) != 1 || worst[0].IdentityID != ident || worst[0].EnvironmentID != envA {
		t.Errorf("worst: %+v %v", worst, err)
	}
	// Recompute is idempotent: same row, same numbers.
	again, _ := svc.Recompute(ctx, project, ident, envA)
	if again.ID != a.ID || again.Score != a.Score {
		t.Errorf("recompute changed the row: %+v vs %+v", again, a)
	}
	if _, err := svc.RecomputeRun(ctx, "22222222-2222-4222-8222-222222222222", string(lastRun.ID)); err == nil {
		t.Error("another project's run should not be found")
	}
}

// TestIncrementalFoldMatchesExactRebuild is the guard on §8.6's
// "incremental, không full scan": finishes applied one by one through
// RecomputeRun must leave the same counts, score and verdict as a rebuild
// from the whole window, for three identities with different patterns —
// and then the sweep must drop the results that left the window.
func TestIncrementalFoldMatchesExactRebuild(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	res := results.New(db)
	svc := flaky.New(db, res)
	ctx := context.Background()
	now := time.Now().UTC()

	idents := []string{
		"aaaaaaaa-1111-4111-8111-111111111111",
		"aaaaaaaa-2222-4222-8222-222222222222",
		"aaaaaaaa-3333-4333-8333-333333333333",
	}
	// pattern per identity per run: p pass, f fail, F flaky_pass, s skipped, - absent
	patterns := []string{
		"pfpfpfpFpppfs",
		"ppppppppppppp",
		"fff-pp-Fs-fpp",
	}
	const runs = 13
	for i := 0; i < runs; i++ {
		// Runs every two days from 28 days ago: all inside the window while
		// they finish; the clock is moved later so the first ones leave it.
		started := now.Add(-time.Duration(28-2*i) * 24 * time.Hour)
		run, _, err := res.CreateRun(ctx, results.NewRun{ProjectID: project, EnvironmentID: envA, LocalRunID: fmt.Sprintf("0198c3e1-7b1a-7c3d-9e4f-0000000001%02d", i), StartedAt: started})
		if err != nil {
			t.Fatal(err)
		}
		var events []results.AttemptEvent
		ids := map[string]string{}
		for k, pat := range patterns {
			var st results.AttemptStatus
			var oc results.Outcome
			switch pat[i] {
			case 'p':
				st, oc = results.AttemptPassed, results.OutcomeExpected
			case 'f':
				st, oc = results.AttemptFailed, results.OutcomeUnexpected
			case 'F':
				st, oc = results.AttemptPassed, results.OutcomeFlaky
			case 's':
				st, oc = results.AttemptSkipped, results.OutcomeSkipped
			default:
				continue
			}
			client := fmt.Sprintf("ct_%d", k)
			events = append(events, results.AttemptEvent{
				Sequence: int64(k + 1), ClientTestID: client, DisplayName: client, IdentityRaw: json.RawMessage(`{"framework":"playwright","filePath":"a.spec.ts","titlePath":["x"]}`),
				Status: st, Outcome: oc, StableTestID: idents[k], StartedAt: started,
			})
			ids[client] = idents[k]
		}
		if len(events) > 0 {
			if _, err := res.AppendAttempts(ctx, run.ID, events); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := res.FinishRun(ctx, run.ID, started.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		// The incremental step, as the worker does it at every finish.
		if _, err := svc.RecomputeRun(ctx, project, string(run.ID)); err != nil {
			t.Fatal(err)
		}
	}

	same := func(what string, got, want flaky.Signal) {
		t.Helper()
		if got.TotalRuns != want.TotalRuns || got.FailedRuns != want.FailedRuns || got.FlakyPassRuns != want.FlakyPassRuns || got.FlipCount != want.FlipCount ||
			got.HasScore != want.HasScore || math.Abs(got.Score-want.Score) > 0.0005 || got.Verdict != want.Verdict || !got.LastFailedAt.Equal(want.LastFailedAt) || got.LastOutcome != want.LastOutcome {
			t.Errorf("%s:\n  incremental %+v\n  exact       %+v", what, got, want)
		}
	}
	// Incremental state after 13 finishes vs Compute over the same runs.
	for k, id := range idents {
		inc, err := svc.Get(ctx, id, envA)
		if err != nil {
			t.Fatal(err)
		}
		all, err := res.History(ctx, id, envA, now.Add(-60*24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		exp := flaky.Compute(all, now)
		same(fmt.Sprintf("identity %d fold vs Compute over all runs", k), inc, flaky.Signal{TotalRuns: exp.TotalRuns, FailedRuns: exp.FailedRuns, FlakyPassRuns: exp.FlakyPassRuns, FlipCount: exp.FlipCount, HasScore: exp.HasScore, Score: exp.Score, Verdict: exp.Verdict, LastFailedAt: exp.LastFailedAt, LastOutcome: exp.LastOutcome})
		if inc.TotalRuns == 0 {
			t.Errorf("identity %d: nothing folded", k)
		}
	}
	// Ten days later the first five runs are outside the 30-day window.
	// The deltas never dropped them; the sweep must.
	later := now.Add(10 * 24 * time.Hour)
	flaky.SetNow(svc, func() time.Time { return later })
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE flaky_signals SET rebuilt_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.Sweep(ctx, flaky.RebuildAfter, 100); err != nil || n != len(idents) {
		t.Fatalf("sweep: n=%d err=%v", n, err)
	}
	for k, id := range idents {
		swept, err := svc.Get(ctx, id, envA)
		if err != nil {
			t.Fatal(err)
		}
		exact, err := svc.Recompute(ctx, project, id, envA)
		if err != nil {
			t.Fatal(err)
		}
		same(fmt.Sprintf("identity %d after sweep vs exact", k), swept, exact)
		window, _ := res.History(ctx, id, envA, later.Add(-30*24*time.Hour))
		all, _ := res.History(ctx, id, envA, now.Add(-60*24*time.Hour))
		if exp := flaky.Compute(window, later); swept.TotalRuns != exp.TotalRuns || len(window) >= len(all) {
			t.Errorf("identity %d: sweep left %d runs, window has %d of %d", k, swept.TotalRuns, exp.TotalRuns, len(all))
		}
		if swept.RebuiltAt.Before(later.Add(-time.Minute)) {
			t.Errorf("identity %d: rebuilt_at not advanced: %s", k, swept.RebuiltAt)
		}
	}
	// A second sweep finds nothing stale.
	if n, err := svc.Sweep(ctx, flaky.RebuildAfter, 100); err != nil || n != 0 {
		t.Errorf("second sweep: n=%d err=%v", n, err)
	}
}
