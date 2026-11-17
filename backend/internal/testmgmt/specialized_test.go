package testmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func reportFixture(kind string, details any) SpecializedInput {
	raw, _ := json.Marshal(details)
	return SpecializedInput{SchemaVersion: 1, IdempotencyKey: "fixture-" + kind, Kind: kind, Title: "Fixture report", State: "completed", Environment: "test", ApplicationVersion: "fixture-v1", Source: "test-fixture", ToolVersion: "1", AdapterVersion: "atp-v1", StartedAt: time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 10, 13, 1, 10, 0, 0, time.UTC), Details: raw, OriginalReport: "<script>test fixture only, not real measurements</script>"}
}
func goodPerformance() PerformanceDetails {
	before := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	return PerformanceDetails{Subtype: "stress", Scenario: "Fixture checkout", LoadConfiguration: "Synthetic 10 users", DataConfiguration: "Synthetic 100 items", DurationSeconds: ptr(600.0), WindowSeconds: ptr(60.0), P95Ms: ptr(100.0), P99Ms: ptr(150.0), ThroughputPerSecond: ptr(20.0), ErrorRatePercent: ptr(0.0), ThresholdsRecordedAt: &before, Thresholds: PerformanceThresholds{P95MaxMs: ptr(200.0), ErrorRateMaxPercent: ptr(1.0)}}
}
func goodFindingDetails() FindingsDetails {
	return FindingsDetails{Standard: "Fixture controls", StandardVersion: "1", Method: "automated", Coverage: []string{"Fixture login"}, AssessmentComplete: true, Findings: []ReportFinding{}}
}
func goodRecovery() RecoveryDetails {
	return RecoveryDetails{Fault: "Fixture disconnect", InitialConditions: "Fixture DB", RecoveryObjective: "Fixture recovery", TargetRecoverySeconds: ptr(60.0), ActualRecoverySeconds: ptr(5.0), AllowedDataLossBytes: ptr(0.0), ActualDataLossBytes: ptr(0.0), Evidence: "Fixture recovery log"}
}

func TestSpecializedAssessmentAllGroupsAndMissingData(t *testing.T) {
	fixtures := map[string]any{
		"performance": goodPerformance(), "security": goodFindingDetails(), "accessibility": goodFindingDetails(),
		"compatibility": CompatibilityDetails{Matrix: []CompatibilityCell{{Browser: "Chrome", BrowserVersion: "fixture", OS: "Windows", OSVersion: "fixture", Device: "Laptop", Status: "passed", Evidence: "Fixture screenshot"}}},
		"recovery":      goodRecovery(),
		"usability":     UsabilityDetails{Method: "Fixture interview", Participants: ptr(3.0), Conclusion: "Fixture criteria satisfied", Tasks: []UsabilityTask{{Task: "Pay", SuccessCriterion: "Receipt found", Observation: "Fixture observation", Conclusion: "Fixture task complete", Status: "passed"}}},
	}
	for kind, details := range fixtures {
		t.Run(kind, func(t *testing.T) {
			input := reportFixture(kind, details)
			outcome, reasons, _, err := assessSpecialized(input)
			if err != nil || outcome != "passed" || len(reasons) != 0 {
				t.Fatalf("assessment %s %+v %v", outcome, reasons, err)
			}
			input.Details = json.RawMessage(`{}`)
			outcome, reasons, _, err = assessSpecialized(input)
			if err != nil || outcome != "inconclusive" || len(reasons) == 0 {
				t.Fatalf("missing data turned into pass: %s %+v %v", outcome, reasons, err)
			}
			for _, state := range []string{"not_run", "not_applicable", "import_error"} {
				input.State = state
				input.Notes = "Fixture reason"
				outcome, _, _, err = assessSpecialized(input)
				if err != nil || outcome != state {
					t.Fatalf("state %s => %s %v", state, outcome, err)
				}
			}
		})
	}
	performance := goodPerformance()
	performance.P95Ms = ptr(250.0)
	performance.P99Ms = ptr(300.0)
	if outcome, _, _, err := assessSpecialized(reportFixture("performance", performance)); err != nil || outcome != "failed" {
		t.Fatalf("breached threshold: %s %v", outcome, err)
	}
	performance = goodPerformance()
	performance.ThresholdsRecordedAt = ptr(time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC))
	if outcome, _, _, _ := assessSpecialized(reportFixture("performance", performance)); outcome != "inconclusive" {
		t.Fatalf("post-run thresholds: %s", outcome)
	}
	performance = goodPerformance()
	performance.P99Ms = ptr(1.0)
	if _, _, _, err := assessSpecialized(reportFixture("performance", performance)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid metric: %v", err)
	}
	recovery := goodRecovery()
	recovery.ActualDataLossBytes = ptr(1.0)
	if outcome, _, _, _ := assessSpecialized(reportFixture("recovery", recovery)); outcome != "failed" {
		t.Fatalf("data loss: %s", outcome)
	}
	findings := goodFindingDetails()
	findings.Findings = []ReportFinding{{ID: "A", Severity: "source-high", Location: "login", Summary: "Fixture issue", Evidence: "Fixture proof", Status: "open"}}
	if outcome, _, _, _ := assessSpecialized(reportFixture("security", findings)); outcome != "failed" {
		t.Fatalf("open finding: %s", outcome)
	}
	findings.Findings[0].Status = "resolved"
	if outcome, _, _, _ := assessSpecialized(reportFixture("security", findings)); outcome != "inconclusive" {
		t.Fatalf("unverified fix: %s", outcome)
	}
	findings.Findings[0].Retested = true
	if outcome, _, _, _ := assessSpecialized(reportFixture("security", findings)); outcome != "passed" {
		t.Fatalf("retested fix: %s", outcome)
	}
	bad := reportFixture("security", map[string]any{"unknown": true})
	if _, _, _, err := assessSpecialized(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown mapping ignored: %v", err)
	}
	blankProof := CompatibilityDetails{Matrix: []CompatibilityCell{{Browser: "Chrome", BrowserVersion: "fixture", OS: "Windows", OSVersion: "fixture", Device: "Laptop", Status: "passed", Evidence: " \n "}}}
	if outcome, _, _, _ := assessSpecialized(reportFixture("compatibility", blankProof)); outcome != "inconclusive" {
		t.Fatalf("whitespace is not platform evidence: %s", outcome)
	}
	for _, subtype := range taxonomyPerformance {
		performance := goodPerformance()
		performance.Subtype, performance.Resources = subtype, "Synthetic 1 CPU versus 2 CPU"
		if outcome, _, _, err := assessSpecialized(reportFixture("performance", performance)); err != nil || outcome != "passed" {
			t.Fatalf("subtype %s: %s %v", subtype, outcome, err)
		}
	}
}

func TestSpecializedPersistenceIdempotencyTenantRetestPaginationAndPurge(t *testing.T) {
	svc, db, _ := newService(t)
	ctx := context.Background()
	input := reportFixture("performance", goodPerformance())
	first, err := svc.AddSpecializedResult(ctx, projectA, input, "alice")
	if err != nil || first.Outcome != "passed" || first.CreatedBy != "alice" || first.Input.OriginalReport != "" {
		t.Fatalf("create: %+v %v", first, err)
	}
	replay, err := svc.AddSpecializedResult(ctx, projectA, input, "bob")
	if err != nil || replay.ID != first.ID || replay.CreatedBy != "alice" {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	changed := input
	changed.Title = "Different"
	if _, err := svc.AddSpecializedResult(ctx, projectA, changed, "bob"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting replay: %v", err)
	}
	text, err := svc.SpecializedOriginal(ctx, projectA, first.ID)
	if err != nil || text != input.OriginalReport {
		t.Fatalf("original lost: %q %v", text, err)
	}
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE specialized_results SET title='Changed' WHERE id=$1`, first.ID); err == nil {
		t.Fatal("append-only record accepted an update")
	}
	invalidImport := reportFixture("security", FindingsDetails{})
	invalidImport.State, invalidImport.Notes, invalidImport.OriginalReport = "import_error", "Unsupported mapping", ""
	if _, err := svc.AddSpecializedResult(ctx, projectA, invalidImport, "alice"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("import error discarded original: %v", err)
	}
	if _, err := svc.GetSpecializedResult(ctx, projectB, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign detail: %v", err)
	}
	if _, err := svc.SpecializedOriginal(ctx, projectB, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign original: %v", err)
	}
	input.IdempotencyKey = "retest"
	input.RetestOf = first.ID
	second, err := svc.AddSpecializedResult(ctx, projectA, input, "bob")
	if err != nil || second.ID == first.ID || second.Input.RetestOf != first.ID {
		t.Fatalf("retest: %+v %v", second, err)
	}
	if _, err := svc.AddSpecializedResult(ctx, projectB, input, "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign retest: %v", err)
	}
	mismatch := reportFixture("recovery", goodRecovery())
	mismatch.RetestOf = first.ID
	if _, err := svc.AddSpecializedResult(ctx, projectA, mismatch, "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mismatched retest: %v", err)
	}
	foreignCase, err := svc.CreateTestCase(ctx, projectB, NewTestCase{Title: "Foreign"})
	if err != nil {
		t.Fatal(err)
	}
	mismatch.RetestOf = ""
	mismatch.TestCaseID = foreignCase.ID
	if _, err := svc.AddSpecializedResult(ctx, projectA, mismatch, "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign case: %v", err)
	}
	for i := 0; i < 50; i++ {
		in := reportFixture("recovery", RecoveryDetails{})
		in.IdempotencyKey = fmt.Sprintf("page-%d", i)
		in.State = "not_run"
		in.Notes = "Fixture not executed"
		if _, err := svc.AddSpecializedResult(ctx, projectA, in, "alice"); err != nil {
			t.Fatal(err)
		}
	}
	page, err := svc.ListSpecializedResults(ctx, projectA, SpecializedFilter{})
	if err != nil || len(page.Items) != 50 || page.NextCursor == "" || page.Items[0].Input.Details != nil {
		t.Fatalf("page: %+v %v", page, err)
	}
	next, err := svc.ListSpecializedResults(ctx, projectA, SpecializedFilter{Before: page.NextCursor})
	if err != nil || len(next.Items) != 2 || next.NextCursor != "" {
		t.Fatalf("next page: %+v %v", next, err)
	}
	if _, err := svc.ListSpecializedResults(ctx, projectB, SpecializedFilter{Before: page.NextCursor}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign cursor: %v", err)
	}
	if _, err := svc.ListSpecializedResults(ctx, projectA, SpecializedFilter{Before: page.NextCursor, Kind: "recovery"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("filter changed cursor: %v", err)
	}
	huge := reportFixture("recovery", goodRecovery())
	huge.OriginalReport = strings.Repeat("x", 524289)
	if _, err := svc.AddSpecializedResult(ctx, projectA, huge, "alice"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("size limit: %v", err)
	}
	if err := svc.PurgeProject(ctx, projectA); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM specialized_results WHERE project_id=$1`, projectA).Scan(&count); err != nil || count != 0 {
		t.Fatalf("purge: %d %v", count, err)
	}
}
