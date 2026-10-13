package testmgmt

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

const (
	projectA = "11111111-1111-4111-8111-111111111111"
	projectB = "22222222-2222-4222-8222-222222222222"
)

// fakeEvidence stands in for query: the gate must judge only what it is
// handed (§6.8.6), so the test hands it exact facts.
type fakeEvidence struct {
	run RunEvidence
	ok  bool
	roi ROIFacts
}

func (f *fakeEvidence) LatestFinishedRun(context.Context, string, string) (RunEvidence, bool, error) {
	return f.run, f.ok, nil
}

func (f *fakeEvidence) ROIFacts(context.Context, string, time.Time, time.Time) (ROIFacts, error) {
	return f.roi, nil
}

// seedIdentity inserts a TestIdentity row directly — identity owns the
// table, but a test may plant fixtures; the module under test still reads
// it through identity's contract.
func seedIdentity(t *testing.T, db *pg.DB, project, status string) string {
	t.Helper()
	var id string
	err := db.Q(context.Background()).QueryRow(context.Background(), `
		INSERT INTO test_identities (project_id, status, merged_into_id, framework, canonical_file_path, canonical_title_path, canonical_leaf_title, template_key, current_fingerprint)
		VALUES ($1, $2, CASE WHEN $2 = 'merged' THEN (SELECT id FROM test_identities WHERE project_id = $1 AND status = 'active' LIMIT 1) END,
		        'playwright', 'tests/pay.spec.ts', ARRAY['Pay', 'declines'], 'declines', 'tk', gen_random_uuid()::text)
		RETURNING id`, project, status).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newService(t *testing.T) (Service, *pg.DB, *fakeEvidence) {
	t.Helper()
	db := pgtest.New(t, migrations.FS)
	ev := &fakeEvidence{}
	return New(db, identity.New(db), ev, Policy{}), db, ev
}

func TestTestCaseCRUDLinksAndTenantScope(t *testing.T) {
	svc, db, _ := newService(t)
	ctx := context.Background()

	suite, err := svc.CreateSuite(ctx, projectA, "", "Checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSuite(ctx, projectB, suite.ID, "child of A's suite"); !errors.Is(err, ErrNotFound) {
		t.Errorf("B nesting under A's suite: %v", err)
	}
	tc, err := svc.CreateTestCase(ctx, projectA, NewTestCase{SuiteID: suite.ID, Title: "Pay with saved card", Steps: []Step{{Action: "open cart", Expected: "cart shown"}}, Priority: P1, IsCritical: true})
	if err != nil {
		t.Fatal(err)
	}
	if tc.AutomationStatus != ManualOnly || tc.SuiteName != "Checkout" || len(tc.Steps) != 1 {
		t.Errorf("case: %+v", tc)
	}
	if _, err := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "x", Priority: "urgent"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad priority: %v", err)
	}
	if _, err := svc.GetTestCase(ctx, projectB, tc.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("B reading A's case: %v", err)
	}
	if _, err := svc.UpdateTestCase(ctx, projectB, tc.ID, TestCasePatch{Title: ptr("hijack")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("B updating A's case: %v", err)
	}

	// Links: same project + active identity only.
	idnA := seedIdentity(t, db, projectA, "active")
	idnB := seedIdentity(t, db, projectB, "active")
	merged := seedIdentity(t, db, projectA, "merged")
	if _, err := svc.LinkAutomation(ctx, projectA, tc.ID, idnB, LinkExact, "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("link to B's identity: %v", err)
	}
	if _, err := svc.LinkAutomation(ctx, projectA, tc.ID, merged, LinkExact, "alice"); !errors.Is(err, ErrConflict) {
		t.Errorf("link to merged identity: %v", err)
	}
	link, err := svc.LinkAutomation(ctx, projectA, tc.ID, idnA, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if link.LinkType != LinkExact || link.CreatedBy != "alice" {
		t.Errorf("link: %+v", link)
	}
	if _, err := svc.LinkAutomation(ctx, projectA, tc.ID, idnA, LinkExact, "alice"); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate link: %v", err)
	}
	got, _ := svc.GetTestCase(ctx, projectA, tc.ID)
	if got.AutomationStatus != Automated || len(got.Links) != 1 {
		t.Errorf("after link: status=%s links=%d", got.AutomationStatus, len(got.Links))
	}
	if err := svc.UnlinkAutomation(ctx, projectB, tc.ID, link.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("B unlinking: %v", err)
	}
	if err := svc.UnlinkAutomation(ctx, projectA, tc.ID, link.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.GetTestCase(ctx, projectA, tc.ID)
	if got.AutomationStatus != AutomationCandidate || len(got.Links) != 0 {
		t.Errorf("after unlink: status=%s links=%d", got.AutomationStatus, len(got.Links))
	}

	// Requirement coverage, scoped the same way.
	req, err := svc.CreateRequirement(ctx, projectA, NewRequirement{ExternalKey: "PAY-1", Title: "Card payments", Priority: P0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRequirement(ctx, projectA, NewRequirement{ExternalKey: "PAY-1", Title: "dup"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate external key: %v", err)
	}
	if err := svc.Cover(ctx, projectB, req.ID, tc.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("B covering A's pair: %v", err)
	}
	if err := svc.Cover(ctx, projectA, req.ID, tc.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Cover(ctx, projectA, req.ID, tc.ID); err != nil {
		t.Errorf("cover is idempotent: %v", err)
	}
	req, _ = svc.GetRequirement(ctx, projectA, req.ID)
	if req.CoveredBy != 1 {
		t.Errorf("coveredBy = %d", req.CoveredBy)
	}
	list, _ := svc.ListTestCases(ctx, projectA, CaseFilter{Critical: ptr(true), Query: "saved"})
	if len(list) != 1 {
		t.Errorf("filter: %d cases", len(list))
	}
	if list, _ := svc.ListTestCases(ctx, projectB, CaseFilter{}); len(list) != 0 {
		t.Errorf("B sees %d of A's cases", len(list))
	}
}

func TestPlansAndManualExecutions(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	tc, _ := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "Refund flow"})
	plan, err := svc.CreatePlan(ctx, projectA, NewPlan{Name: "Sprint 12 regression"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != PlanDraft {
		t.Errorf("new plan status %s", plan.Status)
	}
	if _, err := svc.RecordExecution(ctx, projectB, plan.ID, NewExecution{TestCaseID: tc.ID, Status: ExecPassed}, "bob"); !errors.Is(err, ErrNotFound) {
		t.Errorf("B executing in A's plan: %v", err)
	}
	if _, err := svc.RecordExecution(ctx, projectA, plan.ID, NewExecution{TestCaseID: tc.ID, Status: "meh"}, "bob"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad status: %v", err)
	}
	first, err := svc.RecordExecution(ctx, projectA, plan.ID, NewExecution{TestCaseID: tc.ID, Status: ExecFailed, Note: "button missing"}, "bob")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := svc.RecordExecution(ctx, projectA, plan.ID, NewExecution{TestCaseID: tc.ID, Status: ExecPassed}, "bob"); err != nil {
		t.Fatal(err)
	}
	plan, _ = svc.GetPlan(ctx, projectA, plan.ID)
	if plan.Status != PlanActive {
		t.Errorf("plan with executions should be active, got %s", plan.Status)
	}
	if len(plan.Executions) != 1 || plan.Executions[0].Status != ExecPassed || plan.Executions[0].ID == first.ID || plan.Executions[0].CaseTitle != "Refund flow" {
		t.Errorf("latest verdict per case: %+v", plan.Executions)
	}
	if _, err := svc.SetPlanStatus(ctx, projectA, plan.ID, PlanClosed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordExecution(ctx, projectA, plan.ID, NewExecution{TestCaseID: tc.ID, Status: ExecPassed}, "bob"); !errors.Is(err, ErrConflict) {
		t.Errorf("executing in a closed plan: %v", err)
	}
}

func TestExecutionSnapshotRetestAndHistory(t *testing.T) {
	svc, db, _ := newService(t)
	ctx := context.Background()
	c, err := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "Original", Steps: []Step{{Action: "Click", Expected: "Open"}}, Expected: "Ready"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.CreatePlan(ctx, projectA, NewPlan{Name: "History"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.RecordExecution(ctx, projectA, p.ID, NewExecution{TestCaseID: c.ID, Status: ExecFailed, BugURL: "https://bugs.example/123", StepResults: []StepResult{{StepIndex: 0, Status: ExecFailed, Actual: "Nothing"}}}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if first.CaseSnapshot == nil || first.CaseSnapshot.Title != "Original" {
		t.Fatalf("snapshot: %+v", first)
	}
	_, err = svc.UpdateTestCase(ctx, projectA, c.ID, TestCasePatch{Title: ptr("Renamed"), Steps: &[]Step{{Action: "Changed", Expected: "Different"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordExecution(ctx, projectA, p.ID, NewExecution{TestCaseID: c.ID, Status: ExecPassed, CaseUpdatedAt: c.UpdatedAt}, "alice"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale case accepted: %v", err)
	}
	second, err := svc.RecordExecution(ctx, projectA, p.ID, NewExecution{TestCaseID: c.ID, Status: ExecPassed, RetestOf: first.ID}, "bob")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.GetPlan(ctx, projectA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ExecutionHistory) != 2 || plan.ExecutionHistory[0].ID != second.ID || plan.ExecutionHistory[1].CaseTitle != "Original" || plan.ExecutionHistory[1].CaseSnapshot.Steps[0].Action != "Click" || plan.Executions[0].RetestOf != first.ID {
		t.Fatalf("history: %+v", plan)
	}
	if plan.ExecutionHistory[1].BugURL != "https://bugs.example/123" || plan.ExecutionHistory[1].StepResults[0].Actual != "Nothing" {
		t.Fatal("lost execution details")
	}
	for _, input := range []NewExecution{
		{BugURL: "javascript:alert(1)"}, {BugURL: "https://user:pass@bugs.example/123"},
		{StepResults: []StepResult{{StepIndex: 1, Status: ExecPassed}}},
		{StepResults: []StepResult{{StepIndex: 0, Status: ExecPassed}, {StepIndex: 0, Status: ExecFailed}}},
		{StepResults: []StepResult{{StepIndex: 0, Status: "unknown"}}},
		{StepResults: []StepResult{{StepIndex: 0, Status: ExecFailed}}},
	} {
		input.TestCaseID = c.ID
		input.Status = ExecPassed
		if _, err := svc.RecordExecution(ctx, projectA, p.ID, input, "alice"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid input %+v: %v", input, err)
		}
	}
	otherPlan, err := svc.CreatePlan(ctx, projectA, NewPlan{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordExecution(ctx, projectA, otherPlan.ID, NewExecution{TestCaseID: c.ID, Status: ExecPassed, RetestOf: first.ID}, "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-plan retest: %v", err)
	}
	if _, err := svc.GetPlan(ctx, projectB, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant history: %v", err)
	}
	if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO manual_executions(project_id, test_plan_id, test_case_id, executed_by, status, executed_at)
		SELECT $1, $2, $3, 'history fixture', 'passed', now() + n * interval '1 second' FROM generate_series(1, 201) n`, projectA, otherPlan.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	bounded, err := svc.GetPlan(ctx, projectA, otherPlan.ID)
	if err != nil || len(bounded.ExecutionHistory) != 200 || !bounded.HistoryTruncated {
		t.Fatalf("history bound: %+v %v", bounded, err)
	}
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE manual_executions SET case_snapshot = NULL WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	plan, err = svc.GetPlan(ctx, projectA, p.ID)
	if err != nil || plan.ExecutionHistory[1].CaseSnapshot != nil {
		t.Fatalf("legacy snapshot reconstructed: %+v %v", plan, err)
	}
}

func TestPlanMembershipMigrationPreservesLegacyEvidence(t *testing.T) {
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	before := fstest.MapFS{}
	for _, name := range files {
		if name >= "0031" {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: data}
	}
	db := pgtest.New(t, before)
	ctx := context.Background()
	var active, obsolete, planID string
	q := db.Q(ctx)
	if err := q.QueryRow(ctx, `INSERT INTO test_cases(project_id, title) VALUES ($1, 'Active') RETURNING id`, projectA).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := q.QueryRow(ctx, `INSERT INTO test_cases(project_id, title, automation_status) VALUES ($1, 'Old proof', 'obsolete') RETURNING id`, projectA).Scan(&obsolete); err != nil {
		t.Fatal(err)
	}
	if err := q.QueryRow(ctx, `INSERT INTO test_plans(project_id, name) VALUES ($1, 'Legacy') RETURNING id`, projectA).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Exec(ctx, `INSERT INTO manual_executions(project_id, test_plan_id, test_case_id, executed_by, status) VALUES ($1, $2, $3, 'alice', 'failed')`, projectA, planID, obsolete); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	svc := New(db, identity.New(db), &fakeEvidence{}, Policy{})
	p, err := svc.GetPlan(ctx, projectA, planID)
	if err != nil || len(p.Cases) != 2 || len(p.ExecutionHistory) != 1 {
		t.Fatalf("legacy data lost: %+v %v", p, err)
	}
	for _, c := range p.Cases {
		if c.TestCase.ID == obsolete && c.WorkStatus != "completed" {
			t.Fatalf("legacy completion: %+v", c)
		}
		if c.TestCase.ID == active && c.WorkStatus != "not_run" {
			t.Fatalf("legacy unexecuted case: %+v", c)
		}
	}
}

func TestFixedPlanScopeWorkflowAndAudit(t *testing.T) {
	svc, db, _ := newService(t)
	ctx := context.Background()
	first, err := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "First"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.CreatePlan(ctx, projectA, NewPlan{Name: "Fixed", Actor: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "Later"})
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.GetPlan(ctx, projectA, p.ID)
	if err != nil || len(p.Cases) != 1 || p.Cases[0].TestCase.ID != first.ID {
		t.Fatalf("scope changed automatically: %+v %v", p, err)
	}
	if _, err := svc.RecordExecution(ctx, projectA, p.ID, NewExecution{TestCaseID: second.ID, Status: ExecPassed}, "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("executed outside scope: %v", err)
	}
	ids := []string{first.ID, second.ID}
	stale := p.UpdatedAt
	p, err = svc.UpdatePlan(ctx, projectA, p.ID, PlanPatch{CaseIDs: &ids, ExpectedUpdatedAt: stale}, "alice")
	if err != nil || len(p.Cases) != 2 {
		t.Fatalf("scope update: %+v %v", p, err)
	}
	if _, err := svc.UpdatePlan(ctx, projectA, p.ID, PlanPatch{CaseIDs: &ids, ExpectedUpdatedAt: stale}, "bob"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale scope overwrite: %v", err)
	}
	p, err = svc.UpdatePlan(ctx, projectA, p.ID, PlanPatch{CaseUpdate: &PlanCaseUpdate{TestCaseID: first.ID, WorkStatus: ptr("in_progress")}}, "alice")
	if err != nil || p.Cases[0].WorkStatus != "in_progress" || len(p.Executions) != 0 || p.Status != PlanActive {
		t.Fatalf("start fabricated verdict: %+v %v", p, err)
	}
	if _, err := svc.RecordExecution(ctx, projectA, p.ID, NewExecution{TestCaseID: first.ID, Status: ExecPassed}, "alice"); err != nil {
		t.Fatal(err)
	}
	p, err = svc.GetPlan(ctx, projectA, p.ID)
	if err != nil || p.Cases[0].WorkStatus != "completed" {
		t.Fatalf("completion: %+v %v", p, err)
	}
	if _, err := svc.UpdatePlan(ctx, projectA, p.ID, PlanPatch{CaseIDs: &[]string{second.ID}}, "alice"); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed recorded case: %v", err)
	}
	if _, err := svc.UpdatePlan(ctx, projectA, p.ID, PlanPatch{CaseUpdate: &PlanCaseUpdate{TestCaseID: first.ID, WorkStatus: ptr("not_run")}}, "alice"); !errors.Is(err, ErrConflict) {
		t.Fatalf("reset historical verdict: %v", err)
	}
	foreign, err := svc.CreateTestCase(ctx, projectB, NewTestCase{Title: "Foreign"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdatePlan(ctx, projectA, p.ID, PlanPatch{CaseIDs: &[]string{first.ID, foreign.ID}}, "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign membership: %v", err)
	}
	empty, err := svc.CreatePlan(ctx, projectA, NewPlan{Name: "Empty", CaseIDs: &[]string{}})
	if err != nil || len(empty.Cases) != 0 {
		t.Fatalf("explicit empty scope: %+v %v", empty, err)
	}
	if _, err := svc.SetPlanStatus(ctx, projectA, p.ID, PlanClosed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdatePlan(ctx, projectA, p.ID, PlanPatch{CaseIDs: &ids}, "alice"); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed scope changed: %v", err)
	}
	var count int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM test_plan_audit WHERE test_plan_id = $1 AND actor = 'alice'`, p.ID).Scan(&count); err != nil || count != 4 {
		t.Fatalf("audit count %d %v", count, err)
	}
}

func TestCaseContextAuditImportAndSnapshots(t *testing.T) {
	svc, db, _ := newService(t)
	ctx := context.Background()
	c, err := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "Context", ExternalKey: "TC-CONTEXT", Preconditions: "  Signed in  ", TestData: "account=42", Actor: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Preconditions != "Signed in" || len(c.Audit) != 1 || c.Audit[0].Actor != "alice" || c.Audit[0].Action != "case.create" {
		t.Fatalf("create context/audit: %+v", c)
	}
	p, err := svc.CreatePlan(ctx, projectA, NewPlan{Name: "Context plan"})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := svc.RecordExecution(ctx, projectA, p.ID, NewExecution{TestCaseID: c.ID, Status: ExecPassed}, "alice")
	if err != nil || execution.CaseSnapshot == nil || execution.CaseSnapshot.Preconditions == nil || *execution.CaseSnapshot.Preconditions != "Signed in" || *execution.CaseSnapshot.TestData != "account=42" {
		t.Fatalf("snapshot context: %+v %v", execution, err)
	}
	updated, err := svc.UpdateTestCase(ctx, projectA, c.ID, TestCasePatch{Preconditions: ptr("New setup"), TestData: ptr("new=99"), ExpectedUpdatedAt: c.UpdatedAt, Actor: "bob"})
	if err != nil || len(updated.Audit) != 2 || updated.Audit[0].Actor != "bob" {
		t.Fatalf("update audit: %+v %v", updated, err)
	}
	if !containsField(updated.Audit[0].ChangedFields, "preconditions") || !containsField(updated.Audit[0].ChangedFields, "test_data") {
		t.Fatalf("changed fields: %+v", updated.Audit)
	}
	if _, err := svc.UpdateTestCase(ctx, projectA, c.ID, TestCasePatch{Title: ptr("Stale"), ExpectedUpdatedAt: c.UpdatedAt, Actor: "carol"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit accepted: %v", err)
	}
	if _, err := svc.ImportCSVAs(ctx, projectA, "key,title\nTC-CONTEXT,Imported title", "importer"); err != nil {
		t.Fatal(err)
	}
	imported, err := svc.GetTestCase(ctx, projectA, c.ID)
	if err != nil || imported.Preconditions != "New setup" || imported.TestData != "new=99" || imported.Audit[0].Actor != "importer" || imported.Audit[0].Action != "case.import" {
		t.Fatalf("CSV erased context or missing actor: %+v %v", imported, err)
	}
	if _, err := svc.ImportCSVAs(ctx, projectA, "key,title,preconditions,test_data\nTC-CONTEXT,Imported title,CSV setup,CSV data", "importer"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTestCase(ctx, projectA, c.ID, TestCasePatch{Preconditions: ptr(""), Actor: "alice"}); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.GetPlan(ctx, projectA, p.ID)
	if err != nil || *plan.ExecutionHistory[0].CaseSnapshot.Preconditions != "Signed in" || *plan.ExecutionHistory[0].CaseSnapshot.TestData != "account=42" {
		t.Fatalf("snapshot changed after edits/import: %+v %v", plan, err)
	}
	if _, err := svc.GetTestCase(ctx, projectB, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign audit leak: %v", err)
	}
	for _, value := range []string{strings.Repeat("x", 16001), strings.Repeat("\u00e9", 8001)} {
		if _, err := svc.UpdateTestCase(ctx, projectA, c.ID, TestCasePatch{TestData: &value}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("oversized context accepted: %v", err)
		}
	}
	var before, after string
	if err := db.Q(ctx).QueryRow(ctx, `SELECT before_values->>'preconditions', after_values->>'preconditions' FROM test_case_audit WHERE test_case_id = $1 AND actor = 'bob'`, c.ID).Scan(&before, &after); err != nil || before != "Signed in" || after != "New setup" {
		t.Fatalf("stored audit values: %q %q %v", before, after, err)
	}
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE manual_executions SET case_snapshot = case_snapshot - 'preconditions' - 'testData' WHERE id = $1`, execution.ID); err != nil {
		t.Fatal(err)
	}
	plan, err = svc.GetPlan(ctx, projectA, p.ID)
	if err != nil || plan.ExecutionHistory[0].CaseSnapshot.Preconditions != nil {
		t.Fatalf("legacy context fabricated: %+v %v", plan, err)
	}
	if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO test_case_audit(project_id, test_case_id, actor, action, changed_fields, after_values, created_at)
		SELECT $1, $2, 'audit fixture', 'case.update', ARRAY['title'], '{}'::jsonb, now() + n * interval '1 second' FROM generate_series(1,51) n`, projectA, c.ID); err != nil {
		t.Fatal(err)
	}
	bounded, err := svc.GetTestCase(ctx, projectA, c.ID)
	if err != nil || len(bounded.Audit) != 50 || !bounded.AuditTruncated {
		t.Fatalf("audit bound: %+v %v", bounded, err)
	}
}

func containsField(fields []string, value string) bool {
	for _, field := range fields {
		if field == value {
			return true
		}
	}
	return false
}

func TestReleaseGate(t *testing.T) {
	svc, db, ev := newService(t)
	ctx := context.Background()

	// Two critical cases linked to two identities; one p0 requirement covered
	// by an automated case.
	crit1, _ := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "Checkout", IsCritical: true})
	crit2, _ := svc.CreateTestCase(ctx, projectA, NewTestCase{Title: "Login", IsCritical: true})
	idn1 := seedIdentity(t, db, projectA, "active")
	idn2 := seedIdentity(t, db, projectA, "active")
	idnOther := seedIdentity(t, db, projectA, "active")
	if _, err := svc.LinkAutomation(ctx, projectA, crit1.ID, idn1, LinkExact, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkAutomation(ctx, projectA, crit2.ID, idn2, LinkExact, "alice"); err != nil {
		t.Fatal(err)
	}
	req, _ := svc.CreateRequirement(ctx, projectA, NewRequirement{Title: "Money moves", Priority: P0})
	if err := svc.Cover(ctx, projectA, req.ID, crit1.ID); err != nil {
		t.Fatal(err)
	}

	rel, err := svc.CreateRelease(ctx, projectA, NewRelease{Version: "2026.09.21"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRelease(ctx, projectA, NewRelease{Version: "2026.09.21"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate version: %v", err)
	}
	if _, err := svc.Evaluate(ctx, projectB, rel.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("B evaluating A's release: %v", err)
	}

	// No finished run → no evidence, nothing counted.
	ev.ok = false
	got, err := svc.Evaluate(ctx, projectA, rel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Gate == nil || got.Gate.Verdict != VerdictNoEvidence || got.Status != ReleaseOpen {
		t.Fatalf("no evidence: %+v", got.Gate)
	}

	rule := func(g *GateResult, id string) GateRule {
		for _, r := range g.Rules {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("rule %s missing in %+v", id, g.Rules)
		return GateRule{}
	}

	// Clean run: every critical identity passed, one triaged failure, no flake.
	ev.ok = true
	ev.run = RunEvidence{RunID: "run-1", Branch: "main", Total: 100, FlakyPass: 2, Results: []ResultEvidence{
		{ResultID: "r1", StableTestID: idn1, FinalStatus: "passed"},
		{ResultID: "r2", StableTestID: idn2, FinalStatus: "passed"},
		{ResultID: "r3", StableTestID: idnOther, FinalStatus: "failed", Triaged: true},
	}}
	got, _ = svc.Evaluate(ctx, projectA, rel.ID)
	if got.Gate.Verdict != VerdictApproved || got.Gate.RunID != "run-1" {
		t.Errorf("clean run: %s %+v", got.Gate.Verdict, got.Gate.Rules)
	}
	if r := rule(got.Gate, RuleP0Coverage); r.Applies || got.Gate.P0Coverage == nil || *got.Gate.P0Coverage != 1 {
		t.Errorf("first release p0 coverage: %+v cov=%v", r, got.Gate.P0Coverage)
	}
	if r := rule(got.Gate, RuleProdSmoke); r.Applies {
		t.Errorf("prod smoke must not apply before P3: %+v", r)
	}

	// Flaky rate 4 % → warn, and only warn (§6.8.4 #3).
	ev.run.FlakyPass = 4
	got, _ = svc.Evaluate(ctx, projectA, rel.ID)
	if got.Gate.Verdict != VerdictWarn || rule(got.Gate, RuleFlakyRate).Passed {
		t.Errorf("flaky 4%%: %s", got.Gate.Verdict)
	}

	// An untriaged failure on a non-critical test → blocked.
	ev.run.FlakyPass = 0
	ev.run.Results[2].Triaged = false
	got, _ = svc.Evaluate(ctx, projectA, rel.ID)
	if got.Gate.Verdict != VerdictBlocked || rule(got.Gate, RuleUntriagedFailures).Passed || !rule(got.Gate, RuleCriticalRegression).Passed {
		t.Errorf("untriaged: %s %+v", got.Gate.Verdict, got.Gate.Rules)
	}

	// A critical identity failing → blocked even when triaged.
	ev.run.Results[2].Triaged = true
	ev.run.Results[0].FinalStatus = "timed_out"
	ev.run.Results[0].Triaged = true
	got, _ = svc.Evaluate(ctx, projectA, rel.ID)
	cr := rule(got.Gate, RuleCriticalRegression)
	if got.Gate.Verdict != VerdictBlocked || cr.Passed || cr.Details["regressions"] == nil {
		t.Errorf("critical regression: %s %+v", got.Gate.Verdict, cr)
	}
	// The gate never decides for people: status stays open.
	if got.Status != ReleaseOpen || got.DecidedBy != "" {
		t.Errorf("gate changed the release status: %+v", got)
	}

	// Override with a name (§6.8.4 #1), then shipped is terminal.
	dec, err := svc.Decide(ctx, projectA, rel.ID, Decision{Status: ReleaseApproved, Note: "hotfix, regression is a known env issue"}, "carol@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Status != ReleaseApproved || dec.DecidedBy != "carol@example.com" || dec.DecidedAt.IsZero() || dec.Gate.Verdict != VerdictBlocked {
		t.Errorf("override: %+v", dec)
	}
	if _, err := svc.Decide(ctx, projectB, rel.ID, Decision{Status: ReleaseBlocked}, "mallory"); !errors.Is(err, ErrNotFound) {
		t.Errorf("B deciding A's release: %v", err)
	}
	if _, err := svc.Decide(ctx, projectA, rel.ID, Decision{Status: ReleaseOpen}, "carol"); !errors.Is(err, ErrInvalid) {
		t.Errorf("open is not a decision: %v", err)
	}
	if _, err := svc.Decide(ctx, projectA, rel.ID, Decision{Status: ReleaseShipped}, "carol"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decide(ctx, projectA, rel.ID, Decision{Status: ReleaseBlocked}, "carol"); !errors.Is(err, ErrConflict) {
		t.Errorf("deciding a shipped release: %v", err)
	}

	// Next release: p0 coverage drop (requirement uncovered) → warn with the
	// previous figure in details; a clean run otherwise.
	if err := svc.Uncover(ctx, projectA, req.ID, crit1.ID); err != nil {
		t.Fatal(err)
	}
	ev.run = RunEvidence{RunID: "run-2", Total: 10, Results: []ResultEvidence{{ResultID: "r1", StableTestID: idn1, FinalStatus: "passed"}, {ResultID: "r2", StableTestID: idn2, FinalStatus: "passed"}}}
	rel2, _ := svc.CreateRelease(ctx, projectA, NewRelease{Version: "2026.09.22"})
	got, err = svc.Evaluate(ctx, projectA, rel2.ID)
	if err != nil {
		t.Fatal(err)
	}
	pc := rule(got.Gate, RuleP0Coverage)
	if got.Gate.Verdict != VerdictWarn || pc.Passed || !pc.Applies || pc.Details["previous"] != 1.0 || pc.Details["coverage"] != 0.0 {
		t.Errorf("p0 coverage drop: %s %+v", got.Gate.Verdict, pc)
	}
	list, _ := svc.ListReleases(ctx, projectA)
	if len(list) != 2 || list[0].ID != rel2.ID {
		t.Errorf("releases newest first: %+v", list)
	}
}

func TestImportCSV(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	sheet := strings.Join([]string{
		"\ufeffKey,Title,Suite,Steps,Expected,Priority,Critical,Requirement",
		`TC-1,Pay with saved card,Checkout,"open cart => cart shown | pay => receipt",Order placed,p0,yes,PAY-1`,
		`TC-2,Apply discount,Checkout,"enter code",Total drops,p2,,PAY-1`,
		`TC-9,,Checkout,,,,,`,
		`TC-3,Bad priority,Checkout,,,urgent,,`,
		`,Keyless case,Auth,"sign in",Home shown,p1,no,`,
	}, "\n")
	rep, err := svc.ImportCSV(ctx, projectA, sheet)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 3 || rep.Updated != 0 || rep.Skipped != 1 || len(rep.Errors) != 1 || rep.Suites != 2 || rep.Requirements != 1 {
		t.Fatalf("first import: %+v", rep)
	}
	cases, _ := svc.ListTestCases(ctx, projectA, CaseFilter{})
	byKey := map[string]TestCase{}
	for _, c := range cases {
		byKey[c.ExternalKey+"/"+c.Title] = c
	}
	tc1 := byKey["TC-1/Pay with saved card"]
	if !tc1.IsCritical || tc1.Priority != P0 || tc1.SuiteName != "Checkout" || len(tc1.Steps) != 2 || tc1.Steps[1].Expected != "receipt" {
		t.Errorf("TC-1: %+v", tc1)
	}
	reqs, _ := svc.ListRequirements(ctx, projectA, 0)
	if len(reqs) != 1 || reqs[0].ExternalKey != "PAY-1" || reqs[0].CoveredBy != 2 {
		t.Errorf("requirement from sheet: %+v", reqs)
	}

	// Re-import a corrected sheet: keyed rows update, the keyless one matches
	// by title, nothing is duplicated.
	sheet2 := strings.Join([]string{
		"key,title,suite,priority,critical",
		`TC-1,Pay with saved card (v2),Checkout,p0,yes`,
		`,Keyless case,Auth,p3,`,
	}, "\n")
	rep, err = svc.ImportCSV(ctx, projectA, sheet2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Created != 0 || rep.Updated != 2 || rep.Suites != 0 {
		t.Errorf("second import: %+v", rep)
	}
	cases, _ = svc.ListTestCases(ctx, projectA, CaseFilter{})
	if len(cases) != 3 {
		t.Errorf("%d cases after re-import (want 3)", len(cases))
	}
	got, _ := svc.GetTestCase(ctx, projectA, tc1.ID)
	if got.Title != "Pay with saved card (v2)" {
		t.Errorf("keyed update: %q", got.Title)
	}
	if _, err := svc.ImportCSV(ctx, projectA, "name,steps\nno title column"); !errors.Is(err, ErrInvalid) {
		t.Errorf("missing title column: %v", err)
	}
	if _, err := svc.ImportCSV(ctx, projectB, sheet); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.ListTestCases(ctx, projectB, CaseFilter{}); len(list) != 3 {
		t.Errorf("B has its own %d cases", len(list))
	}
	if list, _ := svc.ListTestCases(ctx, projectA, CaseFilter{}); len(list) != 3 {
		t.Errorf("A untouched by B's import: %d", len(list))
	}
}

func ptr[T any](v T) *T { return &v }
