package query_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/query"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

type fixture struct {
	q       query.Service
	project string
	other   string
	env     tenancy.Environment
	runs    []results.Run
	failed  []results.TestResultID
	payID   string // stable id of the payment test
	res     results.Service
	tri     triage.Service
	db      *pg.DB
}

// build makes a project with N finished runs through the real write
// modules: the payment test alternates fail/pass, the cart test passes.
func build(t *testing.T, n int) fixture {
	t.Helper()
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	ten, res, idn := tenancy.New(db), results.New(db), identity.New(db)
	flk, tri := flaky.New(db, res), triage.New(db, res)
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	pa, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	pb, _ := ten.CreateProject(ctx, ws.ID, "search", "")
	env, _ := ten.EnsureEnvironment(ctx, pa.ID, "staging")
	f := fixture{q: query.New(db), project: string(pa.ID), other: string(pb.ID), env: env, res: res, tri: tri, db: db}
	now := time.Now().UTC()
	ident := func(file, leaf string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"framework": "playwright", "filePath": file, "titlePath": []string{"Checkout", leaf}})
		return b
	}
	for i := 0; i < n; i++ {
		st, outcome := results.AttemptPassed, results.OutcomeExpected
		if i%2 == 0 {
			st, outcome = results.AttemptFailed, results.OutcomeUnexpected
		}
		run, _, err := res.CreateRun(ctx, results.NewRun{ProjectID: f.project, EnvironmentID: string(env.ID), LocalRunID: fmt.Sprintf("0198c3e1-7b1a-7c3d-9e4f-0000000000%02d", i), StartedAt: now.Add(-time.Duration(n-i) * time.Hour), Branch: "main", CommitSHA: fmt.Sprintf("c%02d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.AppendAttempts(ctx, run.ID, []results.AttemptEvent{
			{Sequence: 1, ClientTestID: "ct_pay", DisplayName: "declines expired card", SuiteName: "Checkout", IdentityRaw: ident("tests/payment.spec.ts", "declines expired card"), Status: st, Outcome: outcome, DurationMs: 50, Error: json.RawMessage(`{"message":"expected 402","stack":"at payment.spec.ts:12"}`)},
			{Sequence: 2, ClientTestID: "ct_cart", DisplayName: "keeps state", SuiteName: "Checkout", IdentityRaw: ident("tests/cart.spec.ts", "keeps state"), Status: results.AttemptPassed, Outcome: results.OutcomeExpected, DurationMs: 20},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := res.FinishRun(ctx, run.ID, now.Add(-time.Duration(n-i)*time.Hour+10*time.Minute)); err != nil {
			t.Fatal(err)
		}
		m, _, err := idn.ResolveDeferred(ctx, f.project, string(run.ID), []identity.Ref{
			{Key: "ct_pay", Signals: identity.Signals{Framework: "playwright", FilePath: "tests/payment.spec.ts", TitlePath: []string{"Checkout", "declines expired card"}}},
			{Key: "ct_cart", Signals: identity.Signals{Framework: "playwright", FilePath: "tests/cart.spec.ts", TitlePath: []string{"Checkout", "keeps state"}}},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.payID = m["ct_pay"]
		if _, err := res.SetStableIDs(ctx, run.ID, m); err != nil {
			t.Fatal(err)
		}
		if _, err := flk.RecomputeRun(ctx, f.project, string(run.ID)); err != nil {
			t.Fatal(err)
		}
		list, _ := res.ListResults(ctx, run.ID)
		for _, tr := range list {
			if tr.ClientTestID == "ct_pay" && tr.FinalStatus == results.FinalFailed {
				f.failed = append(f.failed, tr.ID)
			}
		}
		f.runs = append(f.runs, run)
	}
	// One accepted hypothesis on the first failure (two hours later), one
	// FAILURE_SEEN on the second.
	first, _ := res.GetResult(ctx, f.project, f.failed[0])
	if _, _, err := tri.Record(ctx, triage.Record{ProjectID: f.project, TestResultID: string(f.failed[0]), Type: triage.HypothesisAccepted, ActorType: triage.ActorUser, ActorID: "alice", Category: triage.CategoryAppBug, OccurredAt: first.FailureObservedAt.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tri.Record(ctx, triage.Record{ProjectID: f.project, TestResultID: string(f.failed[1]), Type: triage.FailureSeen, ActorType: triage.ActorUser, ActorID: "alice"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRunsPageAndFilters(t *testing.T) {
	f := build(t, 5)
	ctx := context.Background()

	page, err := f.q.Runs(ctx, f.project, query.RunFilter{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("page 1: %+v %v", page, err)
	}
	newest := page.Items[0]
	if newest.ID != string(f.runs[4].ID) || newest.EnvironmentName != "staging" || newest.Branch != "main" || newest.TotalCount != 2 || newest.PassedCount != 1 || newest.FailedCount != 1 || newest.FlakyCount != 0 || newest.Status != "failed" || newest.DurationMs == nil || *newest.DurationMs != 600000 {
		t.Errorf("newest run DTO: %+v", newest)
	}
	// Walk every page; ids must be newest-first and never repeat.
	seen := []string{newest.ID, page.Items[1].ID}
	cur := page.NextCursor
	for cur != "" {
		p, err := f.q.Runs(ctx, f.project, query.RunFilter{Limit: 2, Cursor: cur})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range p.Items {
			seen = append(seen, r.ID)
		}
		cur = p.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("paged %d runs, want 5: %v", len(seen), seen)
	}
	for i := range seen {
		if seen[i] != string(f.runs[4-i].ID) {
			t.Errorf("order broken at %d", i)
		}
	}
	// Cursor bound to filter: reusing it with another filter is refused.
	if _, err := f.q.Runs(ctx, f.project, query.RunFilter{Limit: 2, Cursor: page.NextCursor, Status: "failed"}); !errors.Is(err, query.ErrBadCursor) {
		t.Errorf("cursor across filters: %v", err)
	}
	if _, err := f.q.Runs(ctx, f.other, query.RunFilter{Limit: 2, Cursor: page.NextCursor}); !errors.Is(err, query.ErrBadCursor) {
		t.Errorf("cursor across projects: %v", err)
	}
	if _, err := f.q.Runs(ctx, f.project, query.RunFilter{Cursor: "garbage"}); !errors.Is(err, query.ErrBadCursor) {
		t.Errorf("garbage cursor: %v", err)
	}
	// Filters: status, environment, q.
	if p, _ := f.q.Runs(ctx, f.project, query.RunFilter{Status: "failed"}); len(p.Items) != 3 {
		t.Errorf("status=failed: %d", len(p.Items))
	}
	if p, _ := f.q.Runs(ctx, f.project, query.RunFilter{Environment: "staging"}); len(p.Items) != 5 {
		t.Errorf("environment=staging: %d", len(p.Items))
	}
	if p, _ := f.q.Runs(ctx, f.project, query.RunFilter{Environment: "prod"}); len(p.Items) != 0 {
		t.Errorf("environment=prod: %d", len(p.Items))
	}
	if p, _ := f.q.Runs(ctx, f.project, query.RunFilter{Q: "C03"}); len(p.Items) != 1 || p.Items[0].CommitSHA != "c03" {
		t.Errorf("q=C03: %+v", p.Items)
	}
	if p, _ := f.q.Runs(ctx, f.project, query.RunFilter{Q: "%"}); len(p.Items) != 0 {
		t.Errorf("q=%% must be literal, got %d", len(p.Items))
	}
	if _, err := f.q.Runs(ctx, f.project, query.RunFilter{Status: "exploded"}); !errors.Is(err, query.ErrInvalid) {
		t.Errorf("bad status: %v", err)
	}
	if p, _ := f.q.Runs(ctx, f.other, query.RunFilter{}); len(p.Items) != 0 {
		t.Error("another project sees runs")
	}
	// Single run by id, tenant-scoped.
	if r, err := f.q.Run(ctx, f.project, string(f.runs[0].ID)); err != nil || r.ID != string(f.runs[0].ID) {
		t.Errorf("run: %+v %v", r, err)
	}
	if _, err := f.q.Run(ctx, f.other, string(f.runs[0].ID)); !errors.Is(err, query.ErrNotFound) {
		t.Errorf("run from another project: %v", err)
	}
}

func TestResultsAndDetail(t *testing.T) {
	f := build(t, 3)
	ctx := context.Background()
	run := f.runs[2] // payment failed here

	page, err := f.q.Results(ctx, f.project, string(run.ID), 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("results page 1: %+v %v", page, err)
	}
	first := page.Items[0]
	if first.DisplayName != "declines expired card" || first.FinalStatus != "failed" || first.FilePath != "tests/payment.spec.ts" || first.StableTestID == nil || first.DurationMs != 50 {
		t.Errorf("failures first: %+v", first)
	}
	p2, err := f.q.Results(ctx, f.project, string(run.ID), 1, page.NextCursor)
	if err != nil || len(p2.Items) != 1 || p2.Items[0].DisplayName != "keeps state" || p2.NextCursor != "" {
		t.Errorf("results page 2: %+v %v", p2, err)
	}
	if p, _ := f.q.Results(ctx, f.other, string(run.ID), 10, ""); len(p.Items) != 0 {
		t.Error("another project sees results")
	}

	d, err := f.q.ResultDetail(ctx, f.project, string(f.failed[0]))
	if err != nil {
		t.Fatal(err)
	}
	if d.Result.FinalStatus != "failed" || d.Result.FailureCategory != "app_bug" || d.RunID != string(f.runs[0].ID) {
		t.Errorf("detail result: %+v", d.Result)
	}
	if len(d.Attempts) != 1 || d.Attempts[0].Attempt != 0 || d.Attempts[0].Status != "failed" || d.Attempts[0].Error == nil || *d.Attempts[0].Error.Message != "expected 402" {
		t.Errorf("attempts: %+v", d.Attempts)
	}
	if len(d.Events) != 1 || d.Events[0].Type != "HYPOTHESIS_ACCEPTED" || d.Events[0].ActorName == "" {
		t.Errorf("events: %+v", d.Events)
	}
	if _, err := f.q.ResultDetail(ctx, f.other, string(f.failed[0])); !errors.Is(err, query.ErrNotFound) {
		t.Errorf("detail from another project: %v", err)
	}
}

func TestIdentitiesAndHistory(t *testing.T) {
	f := build(t, 4)
	ctx := context.Background()

	all, err := f.q.Identities(ctx, f.project, "", 10, "")
	if err != nil || len(all.Items) != 2 {
		t.Fatalf("identities: %+v %v", all, err)
	}
	pay := all.Items[0] // worst score first
	if pay.ID != f.payID || pay.CanonicalTitle != "declines expired card" || pay.CanonicalFilePath != "tests/payment.spec.ts" || pay.FlakyScore == nil || pay.TotalRuns != 4 {
		t.Errorf("payment identity: %+v", pay)
	}
	if len(pay.RecentStatuses) != 4 || pay.RecentStatuses[0] != "passed" || pay.RecentStatuses[1] != "failed" {
		t.Errorf("recent statuses newest first: %v", pay.RecentStatuses)
	}
	if pay.Verdict != "flaky" && pay.Verdict != "suspect" {
		t.Errorf("verdict %s", pay.Verdict)
	}
	cart := all.Items[1]
	if cart.FlakyScore == nil || *cart.FlakyScore != 0 || cart.Verdict != "stable" {
		t.Errorf("cart identity: %+v", cart)
	}
	flakyOnly, _ := f.q.Identities(ctx, f.project, "flaky", 10, "")
	if len(flakyOnly.Items) != 1 || flakyOnly.Items[0].ID != f.payID {
		t.Errorf("view=flaky: %+v", flakyOnly.Items)
	}
	// Page size 1 walks both.
	p1, _ := f.q.Identities(ctx, f.project, "", 1, "")
	p2, err := f.q.Identities(ctx, f.project, "", 1, p1.NextCursor)
	if err != nil || len(p1.Items) != 1 || len(p2.Items) != 1 || p1.Items[0].ID == p2.Items[0].ID || p2.NextCursor != "" {
		t.Errorf("identity paging: %+v / %+v %v", p1, p2, err)
	}
	if _, err := f.q.Identities(ctx, f.project, "weird", 10, ""); !errors.Is(err, query.ErrInvalid) {
		t.Errorf("bad view: %v", err)
	}
	if one, err := f.q.Identity(ctx, f.project, f.payID); err != nil || one.ID != f.payID {
		t.Errorf("identity: %+v %v", one, err)
	}
	if _, err := f.q.Identity(ctx, f.other, f.payID); !errors.Is(err, query.ErrNotFound) {
		t.Errorf("identity from another project: %v", err)
	}
	hist, err := f.q.IdentityRuns(ctx, f.project, f.payID, 3, "")
	if err != nil || len(hist.Items) != 3 || hist.NextCursor == "" || hist.Items[0].ID != string(f.runs[3].ID) {
		t.Fatalf("history: %+v %v", hist, err)
	}
	rest, _ := f.q.IdentityRuns(ctx, f.project, f.payID, 3, hist.NextCursor)
	if len(rest.Items) != 1 || rest.Items[0].ID != string(f.runs[0].ID) || rest.NextCursor != "" {
		t.Errorf("history page 2: %+v", rest)
	}
	if _, err := f.q.IdentityRuns(ctx, f.other, f.payID, 3, ""); !errors.Is(err, query.ErrNotFound) {
		t.Errorf("history from another project: %v", err)
	}
}

func TestMetrics(t *testing.T) {
	f := build(t, 3)
	ctx := context.Background()
	m, err := f.q.Triage(ctx, f.project, time.Now().Add(-24*time.Hour))
	if err != nil || m.Failures != 2 || m.Triaged != 1 || m.Seen != 1 || m.TriageCoverage != 0.5 || !m.HasTTRC || m.TTRCMedian < 119*time.Minute || m.TTRCMedian > 121*time.Minute {
		t.Errorf("triage metrics: %+v %v", m, err)
	}
	trend, _ := f.q.CategoryTrend(ctx, f.project, time.Now().Add(-30*24*time.Hour))
	counts := map[string]int{}
	for _, b := range trend {
		counts[b.Category] += b.Count
	}
	if counts["app_bug"] != 1 || counts["unknown"] != 1 {
		t.Errorf("trend: %+v", trend)
	}
}

// §6.8.3 facts (QĐ-13): the window's failures and their TTRC, the CI time
// of attempts that failed before a flaky pass, failures marked flaky — and
// nothing outside the window or the project.
func TestROIFacts(t *testing.T) {
	f := build(t, 3)
	ctx := context.Background()
	now := time.Now().UTC()
	// A flaky pass: 1.5 s and 0.5 s failing, then a pass of 0.2 s.
	run, _, err := f.res.CreateRun(ctx, results.NewRun{ProjectID: f.project, EnvironmentID: string(f.env.ID), LocalRunID: "0198c3e1-7b1a-7c3d-9e4f-0000000000ff", StartedAt: now.Add(-30 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"framework":"playwright","filePath":"tests/flaky.spec.ts","titlePath":["Flaky"]}`)
	if _, err := f.res.AppendAttempts(ctx, run.ID, []results.AttemptEvent{
		{Sequence: 1, ClientTestID: "ct_flaky", Attempt: 0, DisplayName: "Flaky", IdentityRaw: raw, Status: results.AttemptFailed, Outcome: results.OutcomeFlaky, DurationMs: 1500},
		{Sequence: 2, ClientTestID: "ct_flaky", Attempt: 1, DisplayName: "Flaky", IdentityRaw: raw, Status: results.AttemptTimedOut, Outcome: results.OutcomeFlaky, DurationMs: 500},
		{Sequence: 3, ClientTestID: "ct_flaky", Attempt: 2, DisplayName: "Flaky", IdentityRaw: raw, Status: results.AttemptPassed, Outcome: results.OutcomeFlaky, DurationMs: 200},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.res.FinishRun(ctx, run.ID, now.Add(-29*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// The second failure (only seen so far) is marked flaky.
	if _, _, err := f.tri.Record(ctx, triage.Record{ProjectID: f.project, TestResultID: string(f.failed[1]), Type: triage.MarkedFlaky, ActorType: triage.ActorUser, ActorID: "alice"}); err != nil {
		t.Fatal(err)
	}

	got, err := f.q.ROIFacts(ctx, f.project, now.Add(-24*time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Failures: the two failed payment results, plus the flaky pass's first
	// failure — failure_observed_at is set for every result that failed once.
	if got.Failures != 3 || got.Triaged != 1 || got.MarkedFlaky != 1 {
		t.Errorf("counts: %+v", got)
	}
	if got.TTRCSum < 119*time.Minute || got.TTRCSum > 121*time.Minute || got.TTRCMedian < 119*time.Minute || got.TTRCMedian > 121*time.Minute {
		t.Errorf("TTRC sum %v median %v, want ≈ 2 h", got.TTRCSum, got.TTRCMedian)
	}
	if got.FlakyRetry != 2*time.Second {
		t.Errorf("flaky retry time %v, want 2 s (the failing attempts only)", got.FlakyRetry)
	}
	// A window before any of it, and another project: nothing.
	if early, _ := f.q.ROIFacts(ctx, f.project, now.Add(-72*time.Hour), now.Add(-48*time.Hour)); early != (query.ROIFacts{}) {
		t.Errorf("window before the data: %+v", early)
	}
	if other, _ := f.q.ROIFacts(ctx, f.other, now.Add(-24*time.Hour), now.Add(time.Hour)); other != (query.ROIFacts{}) {
		t.Errorf("another project: %+v", other)
	}
}

// §10.6 shadow meter facts (QĐ-13): every project, zeros included; a merge
// lowers the active count while the raw fingerprints stay — exactly the
// movement billing must notice.
func TestMeterFacts(t *testing.T) {
	f := build(t, 3)
	ctx := context.Background()
	now := time.Now().UTC()
	find := func(rows []query.MeterFacts, id string) query.MeterFacts {
		for _, r := range rows {
			if r.ProjectID == id {
				return r
			}
		}
		t.Fatalf("project %s missing from %+v", id, rows)
		return query.MeterFacts{}
	}
	// Runs are 1–3 hours old: count those that started today (UTC).
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	runsToday := 0
	for _, r := range f.runs {
		if !r.StartedAt.Before(today) {
			runsToday++
		}
	}

	rows, err := f.q.MeterFacts(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	a := find(rows, f.project)
	if a.ActiveTests != 2 || a.RawFingerprints != 2 || a.Runs != runsToday || a.Results != 2*runsToday || a.OrgID == "" {
		t.Errorf("project A: %+v (runs today %d)", a, runsToday)
	}
	if b := find(rows, f.other); b != (query.MeterFacts{ProjectID: f.other, OrgID: a.OrgID}) {
		t.Errorf("idle project B must be all zeros: %+v", b)
	}

	// Merge the cart identity into the payment one, as identity would.
	var cart string
	if err := f.db.Q(ctx).QueryRow(ctx, `SELECT id FROM test_identities WHERE project_id = $1 AND id <> $2`, f.project, f.payID).Scan(&cart); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Q(ctx).Exec(ctx, `UPDATE test_identities SET status = 'merged', merged_into_id = $2 WHERE id = $1`, cart, f.payID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Q(ctx).Exec(ctx, `INSERT INTO identity_audit (project_id, action, subject_id, target_id, actor, at) VALUES ($1, 'merge', $2, $3, 'alice', now())`, f.project, cart, f.payID); err != nil {
		t.Fatal(err)
	}
	rows, _ = f.q.MeterFacts(ctx, now)
	if a := find(rows, f.project); a.ActiveTests != 1 || a.RawFingerprints != 2 || a.Merges != 1 {
		t.Errorf("after a merge: %+v — active must drop, raw must not", a)
	}
	// A day long before: nothing active, nothing counted.
	old, _ := f.q.MeterFacts(ctx, now.AddDate(0, 0, -90))
	if a := find(old, f.project); a.ActiveTests != 0 || a.Runs != 0 || a.Merges != 0 {
		t.Errorf("90 days ago: %+v", a)
	}
}
