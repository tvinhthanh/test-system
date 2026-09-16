package triage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

const (
	projectA = "11111111-1111-4111-8111-111111111111"
	projectB = "22222222-2222-4222-8222-222222222222"
	envA     = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
)

func TestTriageEventsAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	res := results.New(db)
	svc := triage.New(db, res)
	ctx := context.Background()

	run, _, err := res.CreateRun(ctx, results.NewRun{ProjectID: projectA, EnvironmentID: envA, LocalRunID: "0198c3e1-7b1a-7c3d-9e4f-0123456789ab", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.AppendAttempts(ctx, run.ID, []results.AttemptEvent{{
		Sequence: 1, ClientTestID: "ct_f", DisplayName: "f", IdentityRaw: json.RawMessage(`{"framework":"playwright","filePath":"a.spec.ts","titlePath":["f"]}`),
		Status: results.AttemptFailed, Outcome: results.OutcomeUnexpected,
	}}); err != nil {
		t.Fatal(err)
	}
	list, _ := res.ListResults(ctx, run.ID)
	trID := string(list[0].ID)

	// FAILURE_SEEN is deduped per user per result per 24 h; other users are
	// their own events.
	seen1, created, err := svc.Record(ctx, triage.Record{ProjectID: projectA, TestResultID: trID, Type: triage.FailureSeen, ActorType: triage.ActorUser, ActorID: "alice"})
	if err != nil || !created {
		t.Fatalf("first seen: %v created=%v", err, created)
	}
	seen2, created, err := svc.Record(ctx, triage.Record{ProjectID: projectA, TestResultID: trID, Type: triage.FailureSeen, ActorType: triage.ActorUser, ActorID: "alice"})
	if err != nil || created || seen2.ID != seen1.ID {
		t.Errorf("second seen: %v created=%v same=%v", err, created, seen2.ID == seen1.ID)
	}
	if _, created, _ := svc.Record(ctx, triage.Record{ProjectID: projectA, TestResultID: trID, Type: triage.FailureSeen, ActorType: triage.ActorUser, ActorID: "bob"}); !created {
		t.Error("bob's FAILURE_SEEN should be its own event")
	}

	// Accepting a category stamps the result (§8.7).
	acc, created, err := svc.Record(ctx, triage.Record{ProjectID: projectA, TestResultID: trID, Type: triage.HypothesisAccepted, ActorType: triage.ActorUser, ActorID: "alice", Category: triage.CategoryTestBug, Payload: map[string]any{"note": "selector changed"}})
	if err != nil || !created {
		t.Fatalf("accept: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal(acc.Payload, &payload)
	if payload["category"] != "test_bug" || payload["note"] != "selector changed" || acc.RunID != string(run.ID) {
		t.Errorf("accepted event: %+v", acc)
	}
	tr, _ := res.GetResult(ctx, projectA, list[0].ID)
	if tr.FailureCategory != "test_bug" {
		t.Errorf("result category = %q, want test_bug", tr.FailureCategory)
	}

	events, err := svc.ListForResult(ctx, projectA, trID)
	if err != nil || len(events) != 3 {
		t.Errorf("events = %d %v", len(events), err)
	}

	// Validation and tenant boundary.
	for name, in := range map[string]triage.Record{
		"bad type":         {ProjectID: projectA, TestResultID: trID, Type: "LOOKED_AT", ActorType: triage.ActorUser, ActorID: "a"},
		"bad actor":        {ProjectID: projectA, TestResultID: trID, Type: triage.Resolved, ActorType: "ROBOT", ActorID: "a"},
		"user without id":  {ProjectID: projectA, TestResultID: trID, Type: triage.Resolved, ActorType: triage.ActorUser},
		"bad category":     {ProjectID: projectA, TestResultID: trID, Type: triage.HypothesisAccepted, ActorType: triage.ActorUser, ActorID: "a", Category: "gremlins"},
		"accept w/o basis": {ProjectID: projectA, TestResultID: trID, Type: triage.HypothesisAccepted, ActorType: triage.ActorUser, ActorID: "a"},
	} {
		if _, _, err := svc.Record(ctx, in); !errors.Is(err, triage.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err := svc.Record(ctx, triage.Record{ProjectID: projectB, TestResultID: trID, Type: triage.Resolved, ActorType: triage.ActorSystem}); !errors.Is(err, triage.ErrNotFound) {
		t.Errorf("another tenant's result: %v", err)
	}
	if evs, _ := svc.ListForResult(ctx, projectB, trID); len(evs) != 0 {
		t.Error("events leaked across projects")
	}
	// SYSTEM events need no actor id.
	if _, created, err := svc.Record(ctx, triage.Record{ProjectID: projectA, TestResultID: trID, Type: triage.Resolved, ActorType: triage.ActorSystem}); err != nil || !created {
		t.Errorf("system event: %v", err)
	}
}
