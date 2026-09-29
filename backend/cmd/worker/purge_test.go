package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
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
	p := purger{tenancy: ten, authz: az, artifact: art, triage: tri, flaky: flk, identity: idn, testmgmt: tm, results: res}

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
