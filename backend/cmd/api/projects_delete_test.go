package main

import (
	"context"
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

// Deleting a project (§11.3.2) is soft: gone at once for the dashboard and
// for its CI token, listed as deleted in its workspace, restorable — and a
// name taken meanwhile blocks the restore instead of making two.
func TestDeleteAndRestoreProject(t *testing.T) {
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
	admin := signIn("wanda@acme.example", authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin)
	maint := signIn("mia@acme.example", authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, authz.RoleMaintainer)
	ingest := func() int {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/runs", strings.NewReader(`{"localRunId":"0198c3e1-7b1a-7c3d-9e4f-0000000000d1","environmentKey":"ci","startedAt":"2026-09-29T08:00:00Z"}`))
		req.Header.Set("Authorization", "Bearer "+tok.Plaintext)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Protocol-Version", "1.1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := ingest(); code != 201 {
		t.Fatalf("ingest before: %d", code)
	}
	path := "/v1/projects/" + string(project.ID)

	if r := maint.do("DELETE", path, map[string]any{"confirmName": "checkout"}, nil); r.code != 403 {
		t.Errorf("maintainer deletes a project: %d %s", r.code, r.raw)
	}
	if r := admin.do("DELETE", path, map[string]any{"confirmName": "Checkout"}, nil); r.code != 400 || r.errCode() != "CONFIRM_NAME" {
		t.Errorf("wrong confirmation: %d %s", r.code, r.raw)
	}
	r := admin.do("DELETE", path, map[string]any{"confirmName": "checkout"}, nil)
	if r.code != 200 || r.body["deletedBy"] != "wanda@acme.example" || r.body["purgeAfter"] == nil {
		t.Fatalf("delete: %d %s", r.code, r.raw)
	}
	// Gone everywhere at once.
	if r := admin.get(path + "/runs"); r.code != 404 {
		t.Errorf("runs of a deleted project: %d %s", r.code, r.raw)
	}
	if l := admin.get("/v1/projects"); strings.Contains(l.raw, string(project.ID)) {
		t.Errorf("deleted project still listed: %s", l.raw)
	}
	if code := ingest(); code != 404 {
		t.Errorf("CI token of a deleted project: %d", code)
	}
	if r := admin.do("DELETE", path, map[string]any{"confirmName": "checkout"}, nil); r.code != 404 {
		t.Errorf("delete twice: %d %s", r.code, r.raw)
	}
	deleted := admin.get("/v1/workspaces/" + string(ws.ID) + "/projects/deleted")
	if len(deleted.items()) != 1 || deleted.items()[0].(map[string]any)["name"] != "checkout" {
		t.Fatalf("deleted list: %s", deleted.raw)
	}

	// The name is free again; while it is taken, restore is a conflict.
	again := admin.post("/v1/workspaces/"+string(ws.ID)+"/projects", map[string]any{"name": "checkout"})
	if again.code != 201 {
		t.Fatalf("reuse the name: %d %s", again.code, again.raw)
	}
	restore := "/v1/workspaces/" + string(ws.ID) + "/projects/" + string(project.ID) + "/restore"
	if r := admin.post(restore, map[string]any{}); r.code != 409 {
		t.Errorf("restore into a taken name: %d %s", r.code, r.raw)
	}
	newID := again.body["id"].(string)
	if r := admin.do("DELETE", "/v1/projects/"+newID, map[string]any{"confirmName": "checkout"}, nil); r.code != 200 {
		t.Fatalf("delete the new one: %d %s", r.code, r.raw)
	}
	if r := maint.post(restore, map[string]any{}); r.code != 403 && r.code != 404 {
		t.Errorf("maintainer restores: %d %s", r.code, r.raw)
	}
	if r := admin.post(restore, map[string]any{}); r.code != 200 {
		t.Fatalf("restore: %d %s", r.code, r.raw)
	}
	if code := ingest(); code != 200 && code != 201 {
		t.Errorf("CI token after restore: %d", code)
	}
	var audited int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM authz_audit WHERE subject_kind = 'project' AND subject_id = $1 AND action IN ('project.delete', 'project.restore')`, string(project.ID)).Scan(&audited); err != nil || audited != 2 {
		t.Errorf("audit rows: %d (%v)", audited, err)
	}
}
