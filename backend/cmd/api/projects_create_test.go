package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// Creating a project (§11.3.2): a workspace admin can, in their workspace;
// the new project shows in their list with every admin action; a project
// maintainer cannot; another workspace is out of reach.
func TestCreateProject(t *testing.T) {
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
	other, _ := ten.CreateWorkspace(ctx, org.ID, "Search")
	existing, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
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
	maint := signIn("mia@acme.example", authz.ScopeRef{Level: authz.LevelProject, ID: string(existing.ID)}, authz.RoleMaintainer)
	path := "/v1/workspaces/" + string(ws.ID) + "/projects"

	// The UI offers "Create project" from the workspace's member scope: it
	// must say project.manage.
	offered := false
	for _, it := range admin.get("/v1/member-scopes").items() {
		s := it.(map[string]any)
		if s["id"] == string(ws.ID) {
			for _, a := range s["actions"].([]any) {
				offered = offered || a == "project.manage"
			}
		}
	}
	if !offered {
		t.Error("member-scopes does not give the workspace admin project.manage")
	}

	if r := maint.post(path, map[string]any{"name": "billing"}); r.code != 403 && r.code != 404 {
		t.Errorf("project maintainer creates: %d %s", r.code, r.raw)
	}
	if r := admin.post("/v1/workspaces/"+string(other.ID)+"/projects", map[string]any{"name": "billing"}); r.code != 404 && r.code != 403 {
		t.Errorf("another workspace: %d %s", r.code, r.raw)
	}
	if r := admin.post(path, map[string]any{"name": ""}); r.code != 400 {
		t.Errorf("empty name: %d %s", r.code, r.raw)
	}
	if r := admin.post(path, map[string]any{"name": "checkout"}); r.code != 409 {
		t.Errorf("duplicate name: %d %s", r.code, r.raw)
	}
	r := admin.post(path, map[string]any{"name": "billing", "defaultBranch": "main"})
	id, _ := r.body["id"].(string)
	if r.code != 201 || id == "" || r.body["workspaceName"] != "Payments" {
		t.Fatalf("create: %d %s", r.code, r.raw)
	}
	// It is theirs to run: listed, with token.manage for the CI token next.
	list := admin.get("/v1/projects")
	found := false
	for _, it := range list.items() {
		p := it.(map[string]any)
		if p["id"] == id {
			found = true
			acts, _ := p["actions"].([]any)
			has := false
			for _, a := range acts {
				if a == "token.manage" {
					has = true
				}
			}
			if !has {
				t.Errorf("the creator cannot issue a token on it: %v", acts)
			}
		}
	}
	if !found {
		t.Errorf("new project not listed: %s", list.raw)
	}
}
