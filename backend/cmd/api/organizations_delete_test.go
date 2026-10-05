package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// Deleting an organization (§11.3.2 "Billing, xóa org"): owners only, the
// name typed back; the org, its workspaces, projects and CI tokens are gone
// at once for everyone; only its owners see it as deleted and restore it.
func TestDeleteAndRestoreOrganization(t *testing.T) {
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
	other, _ := ten.CreateOrganization(ctx, "Other", "other")
	otherWS, _ := ten.CreateWorkspace(ctx, other.ID, "Main")
	otherProject, _ := ten.CreateProject(ctx, otherWS.ID, "site", "")
	tok, _ := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci"})
	signIn := func(email string, scope authz.ScopeRef, role authz.Role) *client {
		u, _ := az.CreateUser(ctx, email)
		_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
		_ = az.VerifyEmail(ctx, u.ID)
		if _, err := az.GrantRole(ctx, u.ID, scope, role); err != nil {
			t.Fatal(err)
		}
		var codes []string
		if role == authz.RoleOrgOwner {
			setup, _ := az.BeginMFASetup(ctx, u.ID)
			var err error
			if codes, err = az.EnableMFA(ctx, u.ID, authenticator(t, setup.Secret, time.Now()), ""); err != nil {
				t.Fatal(err)
			}
		}
		c := newClient(t, srv)
		c.bootstrap()
		r := c.post("/v1/session", map[string]string{"email": email, "password": email + " passphrase"})
		if r.code == 202 {
			r = c.post("/v1/session/mfa", map[string]any{"challenge": r.body["challenge"], "code": codes[0]})
		}
		if r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return c
	}
	orgScope := authz.ScopeRef{Level: authz.LevelOrganization, ID: string(org.ID)}
	owner := signIn("owner@acme.example", orgScope, authz.RoleOrgOwner)
	admin := signIn("admin@acme.example", orgScope, authz.RoleOrgAdmin)
	stranger := signIn("owner@other.example", authz.ScopeRef{Level: authz.LevelOrganization, ID: string(other.ID)}, authz.RoleOrgOwner)
	ingest := func() int {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/runs", strings.NewReader(`{"localRunId":"0198c3e1-7b1a-7c3d-9e4f-0000000000e1","environmentKey":"ci","startedAt":"2026-10-05T08:00:00Z"}`))
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
	path := "/v1/organizations/" + string(org.ID)
	confirm := map[string]any{"confirmName": "Acme"}

	if r := admin.do("DELETE", path, confirm, nil); r.code != 403 {
		t.Errorf("org admin deletes the org: %d %s", r.code, r.raw)
	}
	if r := stranger.do("DELETE", path, confirm, nil); r.code != 404 {
		t.Errorf("another org's owner: %d %s", r.code, r.raw)
	}
	if r := owner.do("DELETE", path, map[string]any{"confirmName": "acme"}, nil); r.code != 400 || r.errCode() != "CONFIRM_NAME" {
		t.Errorf("wrong confirmation: %d %s", r.code, r.raw)
	}
	r := owner.do("DELETE", path, confirm, nil)
	if r.code != 200 || r.body["deletedBy"] != "owner@acme.example" || r.body["slug"] != "acme" || r.body["purgeAfter"] == nil {
		t.Fatalf("delete: %d %s", r.code, r.raw)
	}

	// Gone at once, for every way in.
	if l := owner.get("/v1/projects"); strings.Contains(l.raw, string(project.ID)) {
		t.Errorf("project of a deleted org still listed: %s", l.raw)
	}
	for _, p := range []string{
		"/v1/projects/" + string(project.ID) + "/runs",
		"/v1/projects/" + string(project.ID) + "/settings",
		path + "/members",
		path + "/meter",
		"/v1/workspaces/" + string(ws.ID) + "/members",
	} {
		if r := owner.get(p); r.code != 404 {
			t.Errorf("GET %s under a deleted org: %d %s", p, r.code, r.raw)
		}
	}
	if r := owner.post("/v1/workspaces/"+string(ws.ID)+"/projects", map[string]any{"name": "new"}); r.code != 404 {
		t.Errorf("create a project in a deleted org: %d %s", r.code, r.raw)
	}
	if s := owner.get("/v1/member-scopes"); strings.Contains(s.raw, string(org.ID)) || strings.Contains(s.raw, string(ws.ID)) {
		t.Errorf("member-scopes still lists the deleted org: %s", s.raw)
	}
	if code := ingest(); code != 404 {
		t.Errorf("CI token under a deleted org: %d", code)
	}
	if r := owner.do("DELETE", path, confirm, nil); r.code != 404 {
		t.Errorf("delete twice: %d %s", r.code, r.raw)
	}
	// The neighbour is untouched.
	if r := stranger.get("/v1/projects/" + string(otherProject.ID) + "/runs"); r.code != 200 {
		t.Errorf("another org's project: %d %s", r.code, r.raw)
	}

	// Only owners of the org see it as deleted, and only they restore it.
	if d := owner.get("/v1/organizations/deleted"); len(d.items()) != 1 || d.items()[0].(map[string]any)["name"] != "Acme" {
		t.Fatalf("owner's deleted list: %s", d.raw)
	}
	for who, c := range map[string]*client{"admin": admin, "stranger": stranger} {
		if d := c.get("/v1/organizations/deleted"); d.code != 200 || len(d.items()) != 0 {
			t.Errorf("%s sees deleted orgs: %d %s", who, d.code, d.raw)
		}
		if r := c.post(path+"/restore", map[string]any{}); r.code != 404 {
			t.Errorf("%s restores: %d %s", who, r.code, r.raw)
		}
	}
	if r := owner.post(path+"/restore", map[string]any{}); r.code != 200 || r.body["name"] != "Acme" {
		t.Fatalf("restore: %d %s", r.code, r.raw)
	}
	if r := owner.post(path+"/restore", map[string]any{}); r.code != 404 {
		t.Errorf("restore a live org: %d %s", r.code, r.raw)
	}
	if l := owner.get("/v1/projects"); !strings.Contains(l.raw, string(project.ID)) {
		t.Errorf("project not back after restore: %s", l.raw)
	}
	if code := ingest(); code != 200 && code != 201 {
		t.Errorf("CI token after restore: %d", code)
	}
	var audited int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM authz_audit WHERE subject_kind = 'organization' AND subject_id = $1 AND action IN ('organization.delete', 'organization.restore')`, string(org.ID)).Scan(&audited); err != nil || audited != 2 {
		t.Errorf("audit rows: %d (%v)", audited, err)
	}
}
