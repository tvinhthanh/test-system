package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/aisuggest"
	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
	"github.com/vinh/automation-testing-platform/backend/internal/urlcheck"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// §11.3.2 "xóa project": after the restore window every module's rows for
// the project are gone and so are its objects in the bucket; the project's
// neighbour is untouched; the meter's history stays; a project still in its
// window, or not deleted at all, is left alone.
func TestPurgeDeletedProject(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	ten, az, res := tenancy.New(db), authz.New(db, []byte("test-pepper-test-pepper-test-pepper!")), results.New(db)
	idn, flk, tri := identity.New(db), flaky.New(db, res), triage.New(db, res)
	tm := testmgmt.New(db, idn, nil, testmgmt.Policy{})
	art := artifact.New(db, store, res)
	p := purger{tenancy: ten, authz: az, artifact: art, triage: tri, flaky: flk, identity: idn, testmgmt: tm, results: res, urlcheck: urlcheck.New(db, nil), ai: aisuggest.New(aisuggest.Deps{DB: db, Pepper: []byte("purge-test-pepper-purge-test-pepper")}),
		auto: autotest.New(autotest.Deps{DB: db, Pepper: []byte("purge-test-pepper-purge-test-pepper"), Tenancy: ten})}

	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	// seed gives a project data in every module and returns its object key.
	seed := func(name string, n int) (tenancy.ProjectID, string) {
		pr, err := ten.CreateProject(ctx, ws.ID, name, "")
		if err != nil {
			t.Fatal(err)
		}
		env, _ := ten.EnsureEnvironment(ctx, pr.ID, "ci")
		pid := string(pr.ID)
		if _, err := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: pid}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci"}); err != nil {
			t.Fatal(err)
		}
		if _, err := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelEnvironment, ID: string(env.ID)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "env"}); err != nil {
			t.Fatal(err)
		}
		u, _ := az.CreateUser(ctx, name+"@acme.example")
		if _, err := az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: pid}, authz.RoleMember); err != nil {
			t.Fatal(err)
		}
		run, _, err := res.CreateRun(ctx, results.NewRun{ProjectID: pid, EnvironmentID: string(env.ID), LocalRunID: "0198c3e1-7b1a-7c3d-9e4f-00000000000" + string(rune('0'+n)), StartedAt: time.Now().Add(-time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		raw := json.RawMessage(`{"framework":"playwright","filePath":"tests/a.spec.ts","titlePath":["A"]}`)
		if _, err := res.AppendAttempts(ctx, run.ID, []results.AttemptEvent{{Sequence: 1, ClientTestID: "ct_a", DisplayName: "A", IdentityRaw: raw, Status: results.AttemptFailed, Outcome: results.OutcomeUnexpected, DurationMs: 10}}); err != nil {
			t.Fatal(err)
		}
		if _, err := res.FinishRun(ctx, run.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		m, _, err := idn.ResolveDeferred(ctx, pid, string(run.ID), []identity.Ref{{Key: "ct_a", Signals: identity.Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"A"}}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.SetStableIDs(ctx, run.ID, m); err != nil {
			t.Fatal(err)
		}
		if _, err := flk.RecomputeRun(ctx, pid, string(run.ID)); err != nil {
			t.Fatal(err)
		}
		list, _ := res.ListResults(ctx, run.ID)
		if _, _, err := tri.Record(ctx, triage.Record{ProjectID: pid, TestResultID: string(list[0].ID), Type: triage.FailureSeen, ActorType: triage.ActorUser, ActorID: "x"}); err != nil {
			t.Fatal(err)
		}
		if _, err := tm.CreateTestCase(ctx, pid, testmgmt.NewTestCase{Title: "pays"}); err != nil {
			t.Fatal(err)
		}
		// One verified artifact whose bytes really are in the bucket.
		key := "t/" + pid + "/a.log"
		put, _ := store.PresignPut(ctx, key, "text/plain", time.Minute)
		req, _ := http.NewRequest("PUT", put, strings.NewReader("log"))
		req.Header.Set("Content-Type", "text/plain")
		if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode >= 300 {
			t.Fatalf("put object: %v %v", res, err)
		}
		if _, err := db.Q(ctx).Exec(ctx, `
			INSERT INTO artifacts (project_id, run_id, client_test_id, attempt, kind, name, content_type, declared_bytes, sha256, storage_key, status, expires_at)
			VALUES ($1, $2, 'ct_a', 0, 'log', 'a.log', 'text/plain', 3, $3, $4, 'verified', now() + interval '30 days')`,
			pid, string(run.ID), strings.Repeat("ab", 32), key); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO meter_daily (project_id, org_id, day, active_tests, raw_fingerprints) VALUES ($1, $2, current_date - 1, 1, 1)`, pid, string(org.ID)); err != nil {
			t.Fatal(err)
		}
		if _, err := urlcheck.New(db, nil).Create(ctx, pid, "", "https://"+name+".example", "wanda@acme.example"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO case_suggestions (project_id, org_id, test_type, count, requested_by) VALUES ($1, $2, 'smoke', 1, 'wanda')`, pid, string(org.ID)); err != nil {
			t.Fatal(err)
		}
		// QĐ-16: a runnable case, a test account and a run.
		if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO auto_specs (case_id, project_id, target, steps, source, updated_by)
			VALUES (gen_random_uuid(), $1, '/', '[{"do":"expect_title","value":"x"}]', 'manual', 'wanda')`, pid); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO environment_accounts (project_id, environment_id, key, username, password_sealed, updated_by)
			VALUES ($1, gen_random_uuid(), 'default', 'u', decode(repeat('00', 40), 'hex'), 'wanda')`, pid); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO auto_runs (project_id, org_id, environment_id, trigger, requested_by) VALUES ($1, $2, gen_random_uuid(), 'dashboard', 'wanda')`, pid, string(org.ID)); err != nil {
			t.Fatal(err)
		}
		return pr.ID, key
	}
	gone, goneKey := seed("checkout", 1)
	kept, keptKey := seed("search", 2)
	waiting, _ := seed("billing", 3)

	count := func(q string, id tenancy.ProjectID) int {
		t.Helper()
		var n int
		if err := db.Q(ctx).QueryRow(ctx, q, string(id)).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	perProject := []string{
		`SELECT count(*) FROM runs WHERE project_id = $1`,
		`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.project_id = $1`,
		`SELECT count(*) FROM test_identities WHERE project_id = $1`,
		`SELECT count(*) FROM test_identity_aliases WHERE project_id = $1`,
		`SELECT count(*) FROM flaky_signals WHERE project_id = $1`,
		`SELECT count(*) FROM triage_events WHERE project_id = $1`,
		`SELECT count(*) FROM test_cases WHERE project_id = $1`,
		`SELECT count(*) FROM artifacts WHERE project_id = $1`,
		`SELECT count(*) FROM url_checks WHERE project_id = $1`,
		`SELECT count(*) FROM case_suggestions WHERE project_id = $1`,
		`SELECT count(*) FROM auto_specs WHERE project_id = $1`,
		`SELECT count(*) FROM environment_accounts WHERE project_id = $1`,
		`SELECT count(*) FROM auto_runs WHERE project_id = $1`,
		`SELECT count(*) FROM api_tokens WHERE scope_id = $1 OR scope_id IN (SELECT id FROM environments WHERE project_id = $1)`,
		`SELECT count(*) FROM memberships WHERE scope_id = $1`,
		`SELECT count(*) FROM environments WHERE project_id = $1`,
		`SELECT count(*) FROM projects WHERE id = $1`,
	}
	for _, q := range perProject {
		if count(q, gone) == 0 {
			t.Fatalf("seed left nothing for: %s", q)
		}
	}

	// A live project, and one deleted but still in its window, are not due.
	if _, err := ten.DeleteProject(ctx, gone, "wanda@acme.example", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := ten.DeleteProject(ctx, waiting, "wanda@acme.example", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if n, err := p.run(ctx, time.Now(), 10); err != nil || n != 0 {
		t.Fatalf("purge inside the window: %d %v", n, err)
	}
	if err := ten.PurgeProject(ctx, kept); err == nil {
		t.Error("tenancy purged a live project")
	}
	// The window ends for "checkout".
	if n, err := p.run(ctx, time.Now().Add(2*time.Hour), 10); err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	for _, q := range perProject {
		if n := count(q, gone); n != 0 {
			t.Errorf("after purge %d rows left: %s", n, q)
		}
		if count(q, kept) == 0 || count(q, waiting) == 0 {
			t.Errorf("purge reached another project: %s", q)
		}
	}
	if _, err := store.Head(ctx, goneKey); !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("object of the purged project still in the bucket: %v", err)
	}
	if _, err := store.Head(ctx, keptKey); err != nil {
		t.Errorf("neighbour's object gone: %v", err)
	}
	if n := count(`SELECT count(*) FROM meter_daily WHERE project_id = $1`, gone); n != 1 {
		t.Errorf("meter history of the purged project: %d rows, want kept", n)
	}
	if n := count(`SELECT count(*) FROM authz_audit WHERE action = 'project.purge' AND subject_id = $1`, gone); n != 1 {
		t.Errorf("purge audit rows: %d", n)
	}
	// Nothing left to do on a second pass.
	if n, err := p.run(ctx, time.Now().Add(2*time.Hour), 10); err != nil || n != 0 {
		t.Errorf("second purge: %d %v", n, err)
	}
}

// §11.3.2 "Xóa dữ liệu run/artifact": a deleted run leaves the flaky
// signal as soon as its tests are rebuilt, and after its window the worker
// removes its results, triage, artifacts (bytes too) — never the other
// run of the same test.
func TestPurgeDeletedRun(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	ten, az, res := tenancy.New(db), authz.New(db, []byte("test-pepper-test-pepper-test-pepper!")), results.New(db)
	idn, flk, tri := identity.New(db), flaky.New(db, res), triage.New(db, res)
	tm := testmgmt.New(db, idn, nil, testmgmt.Policy{})
	art := artifact.New(db, store, res)
	p := purger{tenancy: ten, authz: az, artifact: art, triage: tri, flaky: flk, identity: idn, testmgmt: tm, results: res, urlcheck: urlcheck.New(db, nil), ai: aisuggest.New(aisuggest.Deps{DB: db, Pepper: []byte("purge-test-pepper-purge-test-pepper")}),
		auto: autotest.New(autotest.Deps{DB: db, Pepper: []byte("purge-test-pepper-purge-test-pepper"), Tenancy: ten})}

	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	pr, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	pid := string(pr.ID)
	env, _ := ten.EnsureEnvironment(ctx, pr.ID, "ci")
	raw := json.RawMessage(`{"framework":"playwright","filePath":"tests/a.spec.ts","titlePath":["A"]}`)
	var stable string
	mk := func(n int, st results.AttemptStatus, oc results.Outcome) results.RunID {
		run, _, err := res.CreateRun(ctx, results.NewRun{ProjectID: pid, EnvironmentID: string(env.ID), LocalRunID: "0198c3e1-7b1a-7c3d-9e4f-00000000010" + string(rune('0'+n)), StartedAt: time.Now().Add(-time.Duration(3-n) * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.AppendAttempts(ctx, run.ID, []results.AttemptEvent{{Sequence: 1, ClientTestID: "ct_a", DisplayName: "A", IdentityRaw: raw, Status: st, Outcome: oc, DurationMs: 10}}); err != nil {
			t.Fatal(err)
		}
		if _, err := res.FinishRun(ctx, run.ID, time.Now().Add(-time.Duration(3-n)*time.Hour+time.Minute)); err != nil {
			t.Fatal(err)
		}
		m, _, err := idn.ResolveDeferred(ctx, pid, string(run.ID), []identity.Ref{{Key: "ct_a", Signals: identity.Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"A"}}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		stable = m["ct_a"]
		if _, err := res.SetStableIDs(ctx, run.ID, m); err != nil {
			t.Fatal(err)
		}
		if _, err := flk.RecomputeRun(ctx, pid, string(run.ID)); err != nil {
			t.Fatal(err)
		}
		return run.ID
	}
	failed := mk(1, results.AttemptFailed, results.OutcomeUnexpected)
	passed := mk(2, results.AttemptPassed, results.OutcomeExpected)
	list, _ := res.ListResults(ctx, failed)
	if _, _, err := tri.Record(ctx, triage.Record{ProjectID: pid, TestResultID: string(list[0].ID), Type: triage.FailureSeen, ActorType: triage.ActorUser, ActorID: "x"}); err != nil {
		t.Fatal(err)
	}
	key := "t/" + pid + "/run.log"
	put, _ := store.PresignPut(ctx, key, "text/plain", time.Minute)
	req, _ := http.NewRequest("PUT", put, strings.NewReader("log"))
	req.Header.Set("Content-Type", "text/plain")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode >= 300 {
		t.Fatalf("put object: %v %v", r, err)
	}
	if _, err := db.Q(ctx).Exec(ctx, `
		INSERT INTO artifacts (project_id, run_id, client_test_id, attempt, kind, name, content_type, declared_bytes, sha256, storage_key, status, expires_at)
		VALUES ($1, $2, 'ct_a', 0, 'log', 'run.log', 'text/plain', 3, $3, $4, 'verified', now() + interval '30 days')`,
		pid, string(failed), strings.Repeat("ab", 32), key); err != nil {
		t.Fatal(err)
	}
	sig, _ := flk.Get(ctx, stable, string(env.ID))
	if sig.TotalRuns != 2 || sig.FailedRuns != 1 {
		t.Fatalf("signal before: %+v", sig)
	}

	gone, err := res.DeleteRun(ctx, pid, failed, "mia@acme.example", time.Hour)
	if err != nil || len(gone.StableTestIDs) != 1 {
		t.Fatalf("delete: %+v %v", gone, err)
	}
	if err := flk.Invalidate(ctx, pid, gone.StableTestIDs); err != nil {
		t.Fatal(err)
	}
	if _, err := flk.Sweep(ctx, flaky.RebuildAfter, 500); err != nil {
		t.Fatal(err)
	}
	if sig, _ := flk.Get(ctx, stable, string(env.ID)); sig.TotalRuns != 1 || sig.FailedRuns != 0 {
		t.Errorf("signal after the failed run was deleted: %+v, want 1 run, 0 failed", sig)
	}

	if n, err := p.runs(ctx, time.Now(), 10); err != nil || n != 0 {
		t.Fatalf("purge inside the window: %d %v", n, err)
	}
	if n, err := p.runs(ctx, time.Now().Add(2*time.Hour), 10); err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.Q(ctx).QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM runs WHERE id = $1`:                                         0,
		`SELECT count(*) FROM test_results WHERE run_id = $1`:                             0,
		`SELECT count(*) FROM run_events WHERE run_id = $1`:                               0,
		`SELECT count(*) FROM artifacts WHERE run_id = $1`:                                0,
		`SELECT count(*) FROM authz_audit WHERE subject_id = $1 AND action = 'run.purge'`: 1,
	} {
		if n := count(q, string(failed)); n != want {
			t.Errorf("%s: %d, want %d", q, n, want)
		}
	}
	if n := count(`SELECT count(*) FROM triage_events WHERE test_result_id = $1`, string(list[0].ID)); n != 0 {
		t.Errorf("triage of the purged result: %d", n)
	}
	if _, err := store.Head(ctx, key); !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("object still in the bucket: %v", err)
	}
	if n := count(`SELECT count(*) FROM test_results WHERE run_id = $1`, string(passed)); n != 1 {
		t.Errorf("the other run lost its results: %d", n)
	}
	if n := count(`SELECT count(*) FROM test_identities WHERE id = $1`, stable); n != 1 {
		t.Errorf("the test's identity went with one run: %d", n)
	}
}

// §11.3.2 "Billing, xóa org": after the window every project of the org is
// purged — live ones and ones deleted on their own — then what was held at
// the org and its workspaces (memberships, invitations, tokens), the
// workspaces and the org. Another org, and an org still in its window,
// are left alone.
func TestPurgeDeletedOrganization(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	ten, az, res := tenancy.New(db), authz.New(db, []byte("test-pepper-test-pepper-test-pepper!")), results.New(db)
	idn := identity.New(db)
	p := purger{tenancy: ten, authz: az, artifact: artifact.New(db, store, res), triage: triage.New(db, res), flaky: flaky.New(db, res),
		identity: idn, testmgmt: testmgmt.New(db, idn, nil, testmgmt.Policy{}), results: res, urlcheck: urlcheck.New(db, nil), ai: aisuggest.New(aisuggest.Deps{DB: db, Pepper: []byte("purge-test-pepper-purge-test-pepper")}),
		auto: autotest.New(autotest.Deps{DB: db, Pepper: []byte("purge-test-pepper-purge-test-pepper"), Tenancy: ten})}

	type tree struct {
		org      tenancy.OrganizationID
		ws       tenancy.WorkspaceID
		projects []tenancy.ProjectID
	}
	n := 0
	seed := func(slug string) tree {
		org, err := ten.CreateOrganization(ctx, slug, slug)
		if err != nil {
			t.Fatal(err)
		}
		ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
		tr := tree{org: org.ID, ws: ws.ID}
		for _, name := range []string{"checkout", "search"} {
			n++
			pr, _ := ten.CreateProject(ctx, ws.ID, name, "")
			env, _ := ten.EnsureEnvironment(ctx, pr.ID, "ci")
			if _, _, err := res.CreateRun(ctx, results.NewRun{ProjectID: string(pr.ID), EnvironmentID: string(env.ID), LocalRunID: "0198c3e1-7b1a-7c3d-9e4f-0000000001" + string(rune('0'+n/10)) + string(rune('0'+n%10)), StartedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			if _, err := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(pr.ID)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci"}); err != nil {
				t.Fatal(err)
			}
			tr.projects = append(tr.projects, pr.ID)
		}
		owner, _ := az.CreateUser(ctx, "owner@"+slug+".example")
		admin, _ := az.CreateUser(ctx, "admin@"+slug+".example")
		if _, err := az.GrantRole(ctx, owner.ID, authz.ScopeRef{Level: authz.LevelOrganization, ID: string(org.ID)}, authz.RoleOrgOwner); err != nil {
			t.Fatal(err)
		}
		if _, err := az.GrantRole(ctx, admin.ID, authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin); err != nil {
			t.Fatal(err)
		}
		if _, _, err := az.Invite(ctx, "new@"+slug+".example", authz.ScopeRef{Level: authz.LevelOrganization, ID: string(org.ID)}, authz.RoleOrgAdmin, "owner@"+slug+".example"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := az.Invite(ctx, "ws@"+slug+".example", authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin, "owner@"+slug+".example"); err != nil {
			t.Fatal(err)
		}
		return tr
	}
	gone, kept, waiting := seed("acme"), seed("globex"), seed("initech")

	count := func(q string, tr tree) int {
		t.Helper()
		var c int
		if err := db.Q(ctx).QueryRow(ctx, q, string(tr.org), string(tr.ws)).Scan(&c); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return c
	}
	perOrg := []string{
		`SELECT count(*) FROM organizations WHERE id = $1 AND $2 <> ''`,
		`SELECT count(*) FROM workspaces WHERE org_id = $1 AND $2 <> ''`,
		`SELECT count(*) FROM projects WHERE workspace_id = $2 AND $1 <> ''`,
		`SELECT count(*) FROM runs WHERE project_id IN (SELECT id FROM projects WHERE workspace_id = $2) AND $1 <> ''`,
		`SELECT count(*) FROM memberships WHERE scope_id IN ($1, $2)`,
		`SELECT count(*) FROM invitations WHERE scope_id IN ($1, $2)`,
	}
	for _, q := range perOrg {
		if count(q, gone) == 0 {
			t.Fatalf("seed left nothing for: %s", q)
		}
	}
	projectTokens := func(tr tree) int {
		var c int
		ids := []string{string(tr.projects[0]), string(tr.projects[1])}
		if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM api_tokens WHERE scope_id::text = ANY($1)`, ids).Scan(&c); err != nil {
			t.Fatal(err)
		}
		return c
	}

	// One project of "acme" was deleted on its own first, with a longer window.
	if _, err := ten.DeleteProject(ctx, gone.projects[1], "owner@acme.example", 90*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := ten.DeleteOrganization(ctx, gone.org, "owner@acme.example", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := ten.DeleteOrganization(ctx, waiting.org, "owner@initech.example", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	// Hidden at once: no reads of the org, its workspace or its projects.
	if _, err := ten.GetOrganization(ctx, gone.org); !errors.Is(err, tenancy.ErrNotFound) {
		t.Errorf("GetOrganization of a deleted org: %v", err)
	}
	if _, err := ten.GetWorkspace(ctx, gone.ws); !errors.Is(err, tenancy.ErrNotFound) {
		t.Errorf("GetWorkspace under a deleted org: %v", err)
	}
	if _, err := ten.ProjectScope(ctx, gone.projects[0]); !errors.Is(err, tenancy.ErrNotFound) {
		t.Errorf("ProjectScope under a deleted org: %v", err)
	}
	if list, err := ten.ListProjects(ctx, "organization", string(gone.org)); err != nil || len(list) != 0 {
		t.Errorf("ListProjects of a deleted org: %v %v", list, err)
	}
	if err := ten.PurgeOrganization(ctx, kept.org); err == nil {
		t.Error("tenancy purged a live organization")
	}
	if err := ten.PurgeOrganization(ctx, gone.org); err == nil {
		t.Error("tenancy purged an organization that still has projects")
	}

	if c, err := p.orgs(ctx, time.Now(), 10); err != nil || c != 0 {
		t.Fatalf("purge inside the window: %d %v", c, err)
	}
	if c, err := p.orgs(ctx, time.Now().Add(2*time.Hour), 10); err != nil || c != 1 {
		t.Fatalf("purge: %d %v", c, err)
	}
	for _, q := range perOrg {
		if c := count(q, gone); c != 0 {
			t.Errorf("after purge %d rows left: %s", c, q)
		}
		if count(q, kept) == 0 || count(q, waiting) == 0 {
			t.Errorf("purge reached another org: %s", q)
		}
	}
	if projectTokens(gone) != 0 || projectTokens(kept) != 2 || projectTokens(waiting) != 2 {
		t.Errorf("project tokens: gone %d kept %d waiting %d", projectTokens(gone), projectTokens(kept), projectTokens(waiting))
	}
	var audits int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM authz_audit WHERE (action = 'organization.purge' AND subject_id = $1)
		OR (action = 'project.purge' AND subject_id::text = ANY($2))`, string(gone.org), []string{string(gone.projects[0]), string(gone.projects[1])}).Scan(&audits); err != nil || audits != 3 {
		t.Errorf("purge audit rows: %d %v", audits, err)
	}
	if c, err := p.orgs(ctx, time.Now().Add(2*time.Hour), 10); err != nil || c != 0 {
		t.Errorf("second purge: %d %v", c, err)
	}
}
