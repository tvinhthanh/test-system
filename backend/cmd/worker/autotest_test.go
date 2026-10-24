package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// QĐ-16: a finished auto-run case lands in its plan as an execution with
// step results and its screenshots as evidence; the finished run becomes a
// test run through the JUnit import, its tests linked to the cases — once,
// however often it is recorded.
func TestRecorder(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ten, res, idn := tenancy.New(db), results.New(db), identity.New(db)
	az := authz.New(db, []byte("test-pepper-test-pepper-test-pepper!"))
	tm := testmgmt.New(db, idn, nil, testmgmt.Policy{})
	art := artifact.New(db, store, res)
	deps := ingest.Deps{DB: db, Queue: queue.New(db), Tenancy: ten, Authz: az, Results: res, Identity: idn, Metrics: metric.New(), Logger: logger}
	rec := recorder{tm: tm, art: art, tenancy: ten, results: res, logger: logger, ingest: deps, jobs: ingest.NewJobs(deps)}

	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Chat")
	project, _ := ten.CreateProject(ctx, ws.ID, "chat", "")
	env, _ := ten.CreateEnvironment(ctx, project.ID, "staging", "https://chat.example.com")
	pid := string(project.ID)
	steps := []testmgmt.Step{{Action: "Type a message"}, {Action: "Click Send"}, {Action: "See it"}}
	sent, err := tm.CreateTestCase(ctx, pid, testmgmt.NewTestCase{Title: "Send a message", Steps: steps, Actor: "tia"})
	if err != nil {
		t.Fatal(err)
	}
	broken, _ := tm.CreateTestCase(ctx, pid, testmgmt.NewTestCase{Title: "Send an empty message", Steps: steps, Actor: "tia"})
	ids := []string{sent.ID, broken.ID}
	plan, err := tm.CreatePlan(ctx, pid, testmgmt.NewPlan{CaseIDs: &ids, Name: "Auto test", EnvironmentID: string(env.ID), Actor: "tia"})
	if err != nil {
		t.Fatal(err)
	}
	shot := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 16, 'J', 'F', 'I', 'F', 0}
	now := time.Now().UTC()
	spec := autotest.Spec{Target: "/message", Steps: []autotest.Step{{Do: "fill", By: "label", Name: "Message", Value: "hi"}, {Do: "click", By: "role", Role: "button", Name: "Send"}, {Do: "expect_text", Value: "hi"}}}
	run := autotest.Run{ID: "a0000000-0000-4000-8000-000000000001", ProjectID: pid, EnvironmentID: string(env.ID), PlanID: plan.ID,
		RequestedBy: "tia@acme.example", Status: autotest.RunDone, StartedAt: now.Add(-time.Minute), FinishedAt: now, CreatedAt: now.Add(-time.Minute),
		Cases: []autotest.RunCase{
			{CaseID: sent.ID, Title: sent.Title, Spec: spec, Status: autotest.CasePassed, DurationMs: 3200, FinishedAt: now, Healed: 1,
				Results: []autotest.StepResult{{Status: "passed"}, {Status: "passed", Healed: &autotest.Heal{Reason: "Send is now Gửi"}}, {Status: "passed"}}},
			{CaseID: broken.ID, Title: broken.Title, Spec: spec, Status: autotest.CaseFailed, DurationMs: 1800, FinishedAt: now, Error: "Step 3: the text “hi” is not on the page",
				Results: []autotest.StepResult{{Status: "passed"}, {Status: "passed"}, {Status: "failed", Kind: "assert", Error: "the text “hi” is not on the page"}}},
		}}
	execID, err := rec.RecordCase(ctx, run, run.Cases[1], shot, shot)
	if err != nil || execID == "" {
		t.Fatalf("record case: %q %v", execID, err)
	}
	got, err := tm.GetPlan(ctx, pid, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var exec testmgmt.Execution
	for _, e := range got.ExecutionHistory {
		if e.ID == execID {
			exec = e
		}
	}
	if exec.Status != testmgmt.ExecFailed || len(exec.StepResults) != 3 || exec.StepResults[2].Status != testmgmt.ExecFailed ||
		exec.StepResults[2].Actual == "" || exec.ExecutedBy != "ATP auto test · tia@acme.example" {
		t.Fatalf("execution: %+v", exec)
	}
	evidence, err := art.ListEvidence(ctx, pid, []string{execID})
	if err != nil || len(evidence[execID]) != 2 {
		t.Fatalf("evidence: %+v %v", evidence, err)
	}
	healedExec, err := rec.RecordCase(ctx, run, run.Cases[0], nil, nil)
	if err != nil || healedExec == "" {
		t.Fatalf("record healed case: %v", err)
	}

	testRun, err := rec.RecordRun(ctx, run)
	if err != nil || testRun == "" {
		t.Fatalf("record run: %q %v", testRun, err)
	}
	tr, err := res.GetRunByID(ctx, results.RunID(testRun))
	if err != nil || tr.Status != results.RunFailed || tr.LocalRunID != run.ID {
		t.Fatalf("test run: %+v %v", tr, err)
	}
	// Recording again is the same run, and links are not duplicated.
	if again, err := rec.RecordRun(ctx, run); err != nil || again != testRun {
		t.Fatalf("record again: %q %v", again, err)
	}
	for _, c := range []testmgmt.TestCase{sent, broken} {
		full, err := tm.GetTestCase(ctx, pid, c.ID)
		if err != nil || len(full.Links) != 1 {
			t.Errorf("%s links: %+v %v", c.Title, full.Links, err)
		}
	}
	// A closed plan only skips the plan record.
	if _, err := tm.SetPlanStatus(ctx, pid, plan.ID, testmgmt.PlanClosed); err != nil {
		t.Fatal(err)
	}
	if id, err := rec.RecordCase(ctx, run, run.Cases[0], nil, nil); err != nil || id != "" {
		t.Errorf("closed plan: %q %v", id, err)
	}
}
