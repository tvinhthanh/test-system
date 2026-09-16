package results_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// TestFinalStatusIsTheN41Table pins §8.4's mapping row by row.
func TestFinalStatusIsTheN41Table(t *testing.T) {
	cases := []struct {
		outcome results.Outcome
		last    results.AttemptStatus
		want    results.FinalStatus
	}{
		{results.OutcomeExpected, results.AttemptPassed, results.FinalPassed},
		{results.OutcomeExpected, results.AttemptSkipped, results.FinalSkipped}, // expected with expectedStatus=skipped
		{results.OutcomeUnexpected, results.AttemptFailed, results.FinalFailed},
		{results.OutcomeUnexpected, results.AttemptTimedOut, results.FinalTimedOut},       // only visible at result level
		{results.OutcomeUnexpected, results.AttemptInterrupted, results.FinalInterrupted}, // idem
		{results.OutcomeUnexpected, results.AttemptPassed, results.FinalFailed},           // "expected to fail" that passed is still unexpected
		{results.OutcomeSkipped, results.AttemptSkipped, results.FinalSkipped},
		{results.OutcomeFlaky, results.AttemptPassed, results.FinalFlakyPass}, // carried, never re-derived (N36)
	}
	for _, c := range cases {
		if got := results.FinalStatusOf(c.outcome, c.last); got != c.want {
			t.Errorf("(%s, %s) = %s, want %s", c.outcome, c.last, got, c.want)
		}
	}
}

const (
	projectA = "11111111-1111-4111-8111-111111111111"
	projectB = "22222222-2222-4222-8222-222222222222"
	envA     = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	localRun = "0198c3e1-7b1a-7c3d-9e4f-0123456789ab"
)

func identity(file string, titles ...string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"framework": "playwright", "filePath": file, "titlePath": titles, "paramSignature": ""})
	return b
}

func event(seq int64, client string, attempt int, status results.AttemptStatus, outcome results.Outcome) results.AttemptEvent {
	return results.AttemptEvent{
		Sequence: seq, ClientTestID: client, Attempt: attempt, DisplayName: client, SuiteName: "Checkout",
		IdentityRaw: identity("tests/checkout.spec.ts", "Checkout", client), Status: status, Outcome: outcome,
		DurationMs: 100, StartedAt: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC).Add(time.Duration(attempt) * time.Second),
	}
}

func TestRunLifecycleAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := results.New(db)
	ctx := context.Background()
	started := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)

	// Create is idempotent on (project, localRunId); another project with
	// the same localRunId is a different run.
	run, created, err := svc.CreateRun(ctx, results.NewRun{ProjectID: projectA, EnvironmentID: envA, LocalRunID: localRun, StartedAt: started,
		Trigger: json.RawMessage(`{"type":"ci","ciRunUrl":"https://ci.example/1"}`), Branch: "main"})
	if err != nil || !created || run.Status != results.RunRunning || run.Branch != "main" {
		t.Fatalf("create: %+v created=%v err=%v", run, created, err)
	}
	again, created, err := svc.CreateRun(ctx, results.NewRun{ProjectID: projectA, EnvironmentID: envA, LocalRunID: localRun, StartedAt: started.Add(time.Hour)})
	if err != nil || created || again.ID != run.ID || !again.StartedAt.Equal(started) {
		t.Fatalf("repeat create must return the original untouched: %+v created=%v err=%v", again, created, err)
	}
	other, created, err := svc.CreateRun(ctx, results.NewRun{ProjectID: projectB, EnvironmentID: envA, LocalRunID: localRun, StartedAt: started})
	if err != nil || !created || other.ID == run.ID {
		t.Fatalf("same localRunId in another project: %+v created=%v err=%v", other, created, err)
	}
	if _, err := svc.GetRun(ctx, projectB, localRun); err != nil {
		t.Errorf("get in own project: %v", err)
	}
	if got, err := svc.GetRun(ctx, projectA, localRun); err != nil || got.ID != run.ID {
		t.Errorf("get: %+v %v", got, err)
	}
	if _, err := svc.GetRun(ctx, "33333333-3333-4333-8333-333333333333", localRun); !errors.Is(err, results.ErrNotFound) {
		t.Errorf("get from a third project: %v", err)
	}

	// Batch 1: a pass, a fail-then-pass (flaky), and a timeout. Attempts of
	// the flaky test arrive out of order.
	batch := []results.AttemptEvent{
		event(1, "ct_pass", 0, results.AttemptPassed, results.OutcomeExpected),
		event(3, "ct_flaky", 1, results.AttemptPassed, results.OutcomeFlaky),
		event(2, "ct_flaky", 0, results.AttemptFailed, results.OutcomeUnexpected),
		event(4, "ct_timeout", 0, results.AttemptTimedOut, results.OutcomeUnexpected),
	}
	batch[2].Error = json.RawMessage(`{"message":"expected 200, got 500","stack":"at payment.spec.ts:12"}`)
	ap, err := svc.AppendAttempts(ctx, run.ID, batch)
	if err != nil || ap.Accepted != 4 || ap.Duplicates != 0 {
		t.Fatalf("batch 1: %+v %v", ap, err)
	}
	// Retry of the same batch plus one new event: only the new one lands.
	batch = append(batch, event(5, "ct_skip", 0, results.AttemptSkipped, results.OutcomeSkipped))
	ap, err = svc.AppendAttempts(ctx, run.ID, batch)
	if err != nil || ap.Accepted != 1 || ap.Duplicates != 4 {
		t.Fatalf("retried batch: %+v %v", ap, err)
	}

	list, err := svc.ListResults(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	byClient := map[string]results.TestResult{}
	for _, r := range list {
		byClient[r.ClientTestID] = r
	}
	if len(byClient) != 4 {
		t.Fatalf("results = %d, want 4", len(byClient))
	}
	flaky := byClient["ct_flaky"]
	if flaky.FinalStatus != results.FinalFlakyPass || flaky.AttemptCount != 2 || flaky.DurationMs != 200 {
		t.Errorf("flaky = %+v", flaky)
	}
	if flaky.FirstFailedAt.IsZero() || flaky.FailureObservedAt.IsZero() {
		t.Errorf("flaky: first_failed_at/failure_observed_at not set: %+v", flaky)
	}
	if byClient["ct_pass"].FinalStatus != results.FinalPassed || !byClient["ct_pass"].FailureObservedAt.IsZero() {
		t.Errorf("pass = %+v", byClient["ct_pass"])
	}
	if byClient["ct_timeout"].FinalStatus != results.FinalTimedOut {
		t.Errorf("timeout = %+v; timed_out must survive (N41)", byClient["ct_timeout"])
	}
	if byClient["ct_skip"].FinalStatus != results.FinalSkipped {
		t.Errorf("skip = %+v", byClient["ct_skip"])
	}
	if byClient["ct_pass"].StableTestID != "" {
		t.Error("stableTestId must be empty until identity backfills it")
	}

	// Same fingerprint twice in one run (N03): occurrenceIndex keeps both.
	ap, err = svc.AppendAttempts(ctx, run.ID, []results.AttemptEvent{
		{Sequence: 10, ClientTestID: "ct_dup", OccurrenceIndex: 0, DisplayName: "dup", IdentityRaw: identity("a.spec.ts", "dup"), Status: results.AttemptPassed, Outcome: results.OutcomeExpected},
		{Sequence: 11, ClientTestID: "ct_dup", OccurrenceIndex: 1, DisplayName: "dup", IdentityRaw: identity("a.spec.ts", "dup"), Status: results.AttemptFailed, Outcome: results.OutcomeUnexpected},
	})
	if err != nil || ap.Accepted != 2 {
		t.Fatalf("occurrence: %+v %v", ap, err)
	}
	// §8.3: failure_observed_at is the SERVER clock at the first failing
	// attempt, so it is set even when the reporter sent no timestamps.
	if list, _ := svc.ListResults(ctx, run.ID); len(list) == 6 {
		for _, r := range list {
			if r.ClientTestID == "ct_dup" && r.OccurrenceIndex == 1 && r.FailureObservedAt.IsZero() {
				t.Error("failure_observed_at missing on a failed result without client timestamps")
			}
		}
	}

	// A bad event rejects the whole batch before anything is written.
	_, err = svc.AppendAttempts(ctx, run.ID, []results.AttemptEvent{
		event(20, "ct_ok", 0, results.AttemptPassed, results.OutcomeExpected),
		event(21, "ct_bad", 0, "exploded", results.OutcomeExpected),
	})
	if !errors.Is(err, results.ErrInvalid) {
		t.Errorf("bad status: %v", err)
	}
	if list, _ := svc.ListResults(ctx, run.ID); len(list) != 6 {
		t.Errorf("partial batch written: %d results", len(list))
	}

	// Finish: failed because of the timeout and the dup, flakyCount 1.
	finishedAt := started.Add(10 * time.Minute)
	done, err := svc.FinishRun(ctx, run.ID, finishedAt)
	if err != nil || done.Status != results.RunFailed || done.FlakyCount != 1 || !done.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finish: %+v %v", done, err)
	}
	// Finishing again changes nothing; events after finish are refused.
	done2, err := svc.FinishRun(ctx, run.ID, finishedAt.Add(time.Hour))
	if err != nil || !done2.FinishedAt.Equal(finishedAt) || done2.Status != results.RunFailed {
		t.Errorf("second finish: %+v %v", done2, err)
	}
	if _, err := svc.AppendAttempts(ctx, run.ID, []results.AttemptEvent{event(30, "ct_late", 0, results.AttemptPassed, results.OutcomeExpected)}); !errors.Is(err, results.ErrRunFinished) {
		t.Errorf("events after finish: %v", err)
	}

	// A clean run finishes passed.
	clean, _, _ := svc.CreateRun(ctx, results.NewRun{ProjectID: projectA, EnvironmentID: envA, LocalRunID: "0198c3e1-7b1a-7c3d-9e4f-0123456789ff", StartedAt: started})
	_, _ = svc.AppendAttempts(ctx, clean.ID, []results.AttemptEvent{event(1, "ct_pass", 0, results.AttemptPassed, results.OutcomeExpected)})
	if done, err := svc.FinishRun(ctx, clean.ID, time.Time{}); err != nil || done.Status != results.RunPassed || done.FinishedAt.IsZero() {
		t.Errorf("clean finish: %+v %v", done, err)
	}

	// Unknown run.
	if _, err := svc.AppendAttempts(ctx, "00000000-0000-4000-8000-000000000000", nil); !errors.Is(err, results.ErrNotFound) {
		t.Errorf("append to unknown run: %v", err)
	}
	if _, err := svc.FinishRun(ctx, "00000000-0000-4000-8000-000000000000", time.Time{}); !errors.Is(err, results.ErrNotFound) {
		t.Errorf("finish unknown run: %v", err)
	}
}
