package testmgmt

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCSVExportFilterSelectionSafetyAndRoundTrip(t *testing.T) {
	svc, db, _ := newService(t)
	ctx := context.Background()
	original, err := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "=1+1", ExternalKey: "=KEY", Preconditions: "'literal", TestData: "+formula", Steps: []Step{{Action: "choose => then | next", Expected: "Receipt,\n\"OK\""}}, Expected: "@expect", Actor: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.ExportCases(ctx, projectA, CaseFilter{}, []string{original.ID})
	if err != nil || out.Count != 1 {
		t.Fatalf("export: %+v %v", out, err)
	}
	rows, err := csv.NewReader(strings.NewReader(out.CSV)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{}
	for index, header := range rows[0] {
		fields[header] = rows[1][index]
	}
	if fields["title"] != "'=1+1" || fields["preconditions"] != "''literal" || fields["test_data"] != "'+formula" {
		t.Fatalf("unsafe CSV text: %+v", fields)
	}
	var steps []Step
	if err := json.Unmarshal([]byte(fields["steps_json"]), &steps); err != nil || len(steps) != 1 || steps[0] != original.Steps[0] {
		t.Fatalf("steps lost: %+v %v", steps, err)
	}
	report, err := svc.ImportCSVAs(ctx, projectA, out.CSV, "alice")
	if err != nil || report.Updated != 1 || report.Created != 0 {
		t.Fatalf("round trip: %+v %v", report, err)
	}
	restored, err := svc.GetTestCase(ctx, projectA, original.ID)
	if err != nil || restored.Title != original.Title || restored.Preconditions != original.Preconditions || restored.TestData != original.TestData || restored.Steps[0] != original.Steps[0] {
		t.Fatalf("round-trip changed definitions: %+v %v", restored, err)
	}
	if _, err := svc.ExportCases(ctx, projectB, CaseFilter{}, []string{original.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign selected case exported: %v", err)
	}
	if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO test_cases(project_id, title) SELECT $1, 'Filter-' || n FROM generate_series(1,250) n`, projectA); err != nil {
		t.Fatal(err)
	}
	filtered, err := svc.ExportCases(ctx, projectA, CaseFilter{Query: "Filter-"}, nil)
	if err != nil || filtered.Count != 250 {
		t.Fatalf("export truncated to displayed page: %d %v", filtered.Count, err)
	}
}

func TestCSVExportCapsNeverReturnPartialData(t *testing.T) {
	svc, db, _ := newService(t)
	ctx := context.Background()
	if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO test_cases(project_id, title) SELECT $1, 'Case-' || n FROM generate_series(1,5001) n`, projectA); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ExportCases(ctx, projectA, CaseFilter{}, nil)
	if !errors.Is(err, ErrInvalid) || out.CSV != "" {
		t.Fatalf("partial row-capped export returned: %d %v", out.Count, err)
	}
	if _, err := svc.CreateTestCase(ctx, projectB, NewTestCase{Title: "Large", Expected: strings.Repeat("x", 8<<20)}); err != nil {
		t.Fatal(err)
	}
	out, err = svc.ExportCases(ctx, projectB, CaseFilter{}, nil)
	if !errors.Is(err, ErrInvalid) || out.CSV != "" {
		t.Fatalf("partial byte-capped export returned: %d %v", out.Count, err)
	}
}
