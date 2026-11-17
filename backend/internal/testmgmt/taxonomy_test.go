package testmgmt

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTaxonomyValidation(t *testing.T) {
	for _, input := range []Taxonomy{
		{Level: "e2e"}, {Purposes: []string{"load"}}, {Tags: []string{"smoke", "smoke"}},
		{ExecutionModes: []string{"playwright"}}, {PerformanceTypes: []string{"stress"}},
		{Purposes: []string{"performance"}, PerformanceTypes: []string{"endurance"}},
	} {
		if _, err := normalizeTaxonomy(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %+v: %v", input, err)
		}
	}
	normalized, err := normalizeTaxonomy(Taxonomy{})
	if err != nil || normalized.Purposes == nil || normalized.PerformanceTypes == nil {
		t.Fatalf("unclassified: %+v %v", normalized, err)
	}
}

func TestCaseTaxonomyFiltersSnapshotsAndCSVRoundTrip(t *testing.T) {
	svc, db, _ := newService(t)
	ctx := context.Background()
	classification := Taxonomy{Level: "system", Purposes: []string{"performance", "functional"}, Tags: []string{"regression", "e2e"}, ExecutionModes: []string{"manual", "automated"}, PerformanceTypes: []string{"stress", "load"}}
	original, err := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "Checkout load", ExternalKey: "PERF-1", Taxonomy: classification, Actor: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := normalizeTaxonomy(classification)
	if !reflect.DeepEqual(original.Taxonomy, want) || original.AutomationStatus != ManualOnly {
		t.Fatalf("taxonomy changes automation status: %+v", original)
	}
	var legacyID string
	if err := db.Q(ctx).QueryRow(ctx, `INSERT INTO test_cases(project_id, title) VALUES ($1, 'Legacy') RETURNING id`, projectA).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}
	legacy, err := svc.GetTestCase(ctx, projectA, legacyID)
	if err != nil || legacy.Taxonomy.Level != "" || len(legacy.Taxonomy.ExecutionModes) != 0 {
		t.Fatalf("inferred legacy classification: %+v %v", legacy, err)
	}
	for _, filter := range []CaseFilter{
		{Level: "system"}, {Purpose: "performance"}, {Tag: "e2e"}, {ExecutionMode: "automated"}, {PerformanceType: "load"},
		{Level: "system", Purpose: "functional", Tag: "regression", ExecutionMode: "manual", PerformanceType: "stress"},
	} {
		matches, err := svc.ListTestCases(ctx, projectA, filter)
		if err != nil || len(matches) != 1 || matches[0].ID != original.ID {
			t.Fatalf("filter %+v: %+v %v", filter, matches, err)
		}
		exported, err := svc.ExportCases(ctx, projectA, filter, nil)
		if err != nil || exported.Count != 1 {
			t.Fatalf("export filter %+v: %+v %v", filter, exported, err)
		}
		foreign, err := svc.ListTestCases(ctx, projectB, filter)
		if err != nil || len(foreign) != 0 {
			t.Fatalf("foreign filter: %+v %v", foreign, err)
		}
	}
	if _, err := svc.ListTestCases(ctx, projectA, CaseFilter{Purpose: "unknown"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown filter: %v", err)
	}
	p, err := svc.CreatePlan(ctx, projectA, NewPlan{Name: "Classified plan"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.RecordExecution(ctx, projectA, p.ID, NewExecution{TestCaseID: original.ID, Status: ExecPassed}, "alice")
	if err != nil || first.CaseSnapshot.Taxonomy == nil || !reflect.DeepEqual(*first.CaseSnapshot.Taxonomy, want) {
		t.Fatalf("snapshot %+v %v", first, err)
	}
	empty := Taxonomy{}
	changed, err := svc.UpdateTestCase(ctx, projectA, original.ID, TestCasePatch{Taxonomy: &empty, Actor: "alice", ExpectedUpdatedAt: original.UpdatedAt})
	if err != nil || changed.Taxonomy.Level != "" {
		t.Fatalf("clear: %+v %v", changed, err)
	}
	history, err := svc.GetPlan(ctx, projectA, p.ID)
	if err != nil || !reflect.DeepEqual(*history.ExecutionHistory[0].CaseSnapshot.Taxonomy, want) {
		t.Fatalf("snapshot changed: %+v %v", history, err)
	}
	if _, err := svc.UpdateTestCase(ctx, projectB, original.ID, TestCasePatch{Taxonomy: &want}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign patch: %v", err)
	}
	changed, err = svc.UpdateTestCase(ctx, projectA, original.ID, TestCasePatch{Taxonomy: &want})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Audit) == 0 || !containsField(changed.Audit[0].ChangedFields, "taxonomy") {
		t.Fatalf("missing audit %+v", changed.Audit)
	}
	exported, err := svc.ExportCases(ctx, projectA, CaseFilter{}, []string{original.ID})
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.ImportCSVAs(ctx, projectB, exported.CSV, "bob")
	if err != nil || report.Created != 1 || len(report.Errors) != 0 {
		t.Fatalf("roundtrip %+v %v", report, err)
	}
	restored, _ := svc.ListTestCases(ctx, projectB, CaseFilter{PerformanceType: "load"})
	if len(restored) != 1 || !reflect.DeepEqual(restored[0].Taxonomy, want) {
		t.Fatalf("taxonomy lost: %+v", restored)
	}
	if _, err := svc.ImportCSVAs(ctx, projectA, "key,title\nPERF-1,Updated title\n", "alice"); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := svc.GetTestCase(ctx, projectA, original.ID)
	if !reflect.DeepEqual(unchanged.Taxonomy, want) {
		t.Fatalf("omitted column erased taxonomy %+v", unchanged)
	}
	var csvText strings.Builder
	writer := csv.NewWriter(&csvText)
	_ = writer.Write([]string{"key", "title", "taxonomy_json"})
	_ = writer.Write([]string{"PERF-1", "Bad", `{"purposes":["performance"],"tags":["smoke","smoke"]}`})
	writer.Flush()
	invalid, err := svc.ImportCSV(ctx, projectA, csvText.String())
	if err != nil || invalid.Updated != 0 || len(invalid.Errors) != 1 {
		t.Fatalf("bad CSV taxonomy %+v %v", invalid, err)
	}
	raw, _ := json.Marshal(Taxonomy{Level: "bogus"})
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE test_cases SET taxonomy = $2 WHERE id = $1`, original.ID, raw); err == nil {
		t.Fatal("database accepted invalid taxonomy")
	}
}
