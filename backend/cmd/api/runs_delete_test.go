package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// Deleting a run (§11.3.2 "Xóa dữ liệu run/artifact"): owner, org admin and
// maintainer — not workspace admin, not member; an open run cannot be; a
// deleted run and its results disappear from every read and come back with
// Restore; both are audited.
func TestDeleteAndRestoreRun(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	project, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	tok, _ := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci"})
	signIn := func(email string, scope authz.ScopeRef, role authz.Role) *client {
		u, _ := az.CreateUser(ctx, email)
		_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
		_ = az.VerifyEmail(ctx, u.ID)
		if _, err := az.GrantRole(ctx, u.ID, scope, role); err != nil {
			t.Fatal(err)
		}
		c := newClient(t, srv)
		c.bootstrap()
		if r := c.post("/v1/session", map[string]string{"email": email, "password": email + " passphrase"}); r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return c
	}
	onProject := authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}
	maint := signIn("mia@acme.example", onProject, authz.RoleMaintainer)
	member := signIn("max@acme.example", onProject, authz.RoleMember)
	wsAdmin := signIn("wanda@acme.example", authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin)

	ingest := func(path string, body any) {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(string(b)))
		req.Header.Set("Authorization", "Bearer "+tok.Plaintext)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Protocol-Version", "1.1")
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode >= 300 {
			t.Fatalf("%s: %v %v", path, res, err)
		}
		res.Body.Close()
	}
	run := func(local string, finish bool) string {
		ingest("/v1/runs", map[string]any{"localRunId": local, "environmentKey": "ci", "startedAt": "2026-09-29T08:00:00Z", "trigger": map[string]any{"ref": "refs/heads/main"}})
		ingest("/v1/runs/"+local+"/events", map[string]any{"events": []any{map[string]any{
			"sequence": 1, "type": "test.finished", "clientTestId": "ct_a", "attempt": 0,
			"identity": map[string]any{"framework": "playwright", "filePath": "a.spec.ts", "titlePath": []string{"A"}},
			"status":   "failed", "outcome": "unexpected", "durationMs": 5,
			"startedAt": "2026-09-29T08:00:01Z", "finishedAt": "2026-09-29T08:00:02Z",
		}}})
		if finish {
			ingest("/v1/runs/"+local+"/finish", map[string]any{})
		}
		var id string
		if err := db.Q(ctx).QueryRow(ctx, `SELECT id FROM runs WHERE local_run_id = $1`, local).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	doomed := run("0198c3e1-7b1a-7c3d-9e4f-0000000000e1", true)
	open := run("0198c3e1-7b1a-7c3d-9e4f-0000000000e2", false)
	base := "/v1/projects/" + string(project.ID)
	var resultID string
	if err := db.Q(ctx).QueryRow(ctx, `SELECT id FROM test_results WHERE run_id = $1`, doomed).Scan(&resultID); err != nil {
		t.Fatal(err)
	}

	if r := member.do("DELETE", base+"/runs/"+doomed, nil, nil); r.code != 403 {
		t.Errorf("member deletes a run: %d %s", r.code, r.raw)
	}
	if r := wsAdmin.do("DELETE", base+"/runs/"+doomed, nil, nil); r.code != 403 {
		t.Errorf("workspace admin deletes a run (§11.3.2 says no): %d %s", r.code, r.raw)
	}
	if r := maint.do("DELETE", base+"/runs/"+open, nil, nil); r.code != 409 || r.errCode() != "RUN_OPEN" {
		t.Errorf("open run: %d %s", r.code, r.raw)
	}
	r := maint.do("DELETE", base+"/runs/"+doomed, nil, nil)
	if r.code != 200 || r.body["deletedBy"] != "mia@acme.example" {
		t.Fatalf("delete: %d %s", r.code, r.raw)
	}
	// Gone from every read.
	if l := maint.get(base + "/runs"); strings.Contains(l.raw, doomed) || len(l.items()) != 1 {
		t.Errorf("run list: %s", l.raw)
	}
	for _, path := range []string{base + "/runs/" + doomed, base + "/runs/" + doomed + "/results", base + "/results/" + resultID} {
		if r := maint.get(path); r.code != 404 {
			t.Errorf("%s: %d %s", path, r.code, r.raw)
		}
	}
	if r := maint.post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "FAILURE_SEEN", "payload": map[string]any{}}); r.code != 404 {
		t.Errorf("triage on a deleted run's result: %d %s", r.code, r.raw)
	}
	if r := maint.do("DELETE", base+"/runs/"+doomed, nil, nil); r.code != 404 {
		t.Errorf("delete twice: %d %s", r.code, r.raw)
	}
	deleted := maint.get(base + "/runs/deleted")
	if len(deleted.items()) != 1 || deleted.items()[0].(map[string]any)["branch"] != "main" {
		t.Fatalf("deleted runs: %s", deleted.raw)
	}
	if r := member.get(base + "/runs/deleted"); r.code != 403 {
		t.Errorf("member lists deleted runs: %d %s", r.code, r.raw)
	}

	if r := maint.post(base+"/runs/"+doomed+"/restore", map[string]any{}); r.code != 200 {
		t.Fatalf("restore: %d %s", r.code, r.raw)
	}
	if r := maint.get(base + "/runs/" + doomed); r.code != 200 {
		t.Errorf("restored run: %d %s", r.code, r.raw)
	}
	if r := maint.get(base + "/results/" + resultID); r.code != 200 {
		t.Errorf("restored result: %d %s", r.code, r.raw)
	}
	var audited int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM authz_audit WHERE subject_kind = 'run' AND subject_id = $1`, doomed).Scan(&audited); err != nil || audited != 2 {
		t.Errorf("audit rows: %d (%v)", audited, err)
	}
}
