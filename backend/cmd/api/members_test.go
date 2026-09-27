package main

import (
	"context"
	"io"
	"log/slog"
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

// Member management over HTTP (§11.3.2 "Quản lý member"): who may call it,
// what the list shows, what a grant may name, and that every change lands
// in authz_audit with the session user as actor.
func TestProjectMembers(t *testing.T) {
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
	checkout, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	search, _ := ten.CreateProject(ctx, ws.ID, "search", "")
	user := func(email string, verified bool, scope authz.ScopeRef, role authz.Role) (authz.User, *client) {
		u, err := az.CreateUser(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		if !verified {
			return u, nil
		}
		_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
		_ = az.VerifyEmail(ctx, u.ID)
		if role != "" {
			if _, err := az.GrantRole(ctx, u.ID, scope, role); err != nil {
				t.Fatal(err)
			}
		}
		c := newClient(t, srv)
		c.bootstrap()
		if r := c.post("/v1/session", map[string]string{"email": email, "password": email + " passphrase"}); r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return u, c
	}
	onCheckout := authz.ScopeRef{Level: authz.LevelProject, ID: string(checkout.ID)}
	_, alice := user("alice@example.com", true, onCheckout, authz.RoleMember)
	_, carol := user("carol@example.com", true, onCheckout, authz.RoleMaintainer)
	_, dave := user("dave@example.com", true, authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin)
	bob, _ := user("bob@example.com", true, authz.ScopeRef{Level: authz.LevelProject, ID: string(search.ID)}, authz.RoleViewer)
	user("eve@example.com", false, authz.ScopeRef{}, "")
	base := "/v1/projects/" + string(checkout.ID)

	// The matrix: a workspace admin manages members; maintainer and member
	// do not (403 from the role — they can see the project).
	if r := dave.get("/v1/projects"); !strings.Contains(r.raw, `"member.manage"`) {
		t.Errorf("workspace admin actions lack member.manage: %s", r.raw)
	}
	for name, c := range map[string]*client{"member": alice, "maintainer": carol} {
		if r := c.get(base + "/members"); r.code != 403 || r.errCode() != "FORBIDDEN" {
			t.Errorf("%s lists members: %d %s", name, r.code, r.raw)
		}
		if r := c.post(base+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"}); r.code != 403 {
			t.Errorf("%s grants: %d %s", name, r.code, r.raw)
		}
	}

	// The list: project rows and the inherited workspace row, by email; bob
	// (a viewer of another project) is not here.
	emails := func(r resp) string {
		var out []string
		for _, it := range r.items() {
			m := it.(map[string]any)
			out = append(out, m["email"].(string)+":"+m["role"].(string)+"@"+m["level"].(string))
		}
		return strings.Join(out, " ")
	}
	if r := dave.get(base + "/members"); r.code != 200 || emails(r) != "alice@example.com:member@project carol@example.com:maintainer@project dave@example.com:workspace_admin@workspace" {
		t.Fatalf("members: %d %s", r.code, r.raw)
	}
	// Pages resume by (email, level) and a cursor is bound to its project.
	p1 := dave.get(base + "/members?limit=2")
	next, _ := p1.body["nextCursor"].(string)
	if len(p1.items()) != 2 || next == "" {
		t.Fatalf("first page: %s", p1.raw)
	}
	if r := dave.get(base + "/members?limit=2&cursor=" + next); emails(r) != "dave@example.com:workspace_admin@workspace" || r.body["nextCursor"] != nil {
		t.Errorf("second page: %s", r.raw)
	}
	if r := dave.get("/v1/projects/" + string(search.ID) + "/members?cursor=" + next); r.code != 400 || r.errCode() != "INVALID_CURSOR" {
		t.Errorf("cursor of another project: %d %s", r.code, r.raw)
	}

	// Grant, then change: one row, audited with the role it replaced.
	if r := dave.post(base+"/members", map[string]any{"email": " Bob@Example.com ", "role": "viewer"}); r.code != 200 || r.body["userId"] != bob.ID || r.body["level"] != "project" || r.body["role"] != "viewer" {
		t.Fatalf("grant bob: %d %s", r.code, r.raw)
	}
	if r := dave.post(base+"/members", map[string]any{"email": "bob@example.com", "role": "member"}); r.code != 200 || r.body["role"] != "member" {
		t.Fatalf("change bob: %d %s", r.code, r.raw)
	}
	audit, err := az.ListAudit(ctx, string(checkout.ID), 10)
	if err != nil || len(audit) != 2 || audit[0].Action != "role.grant" || audit[0].Details["previous"] != "viewer" || audit[0].Details["role"] != "member" || audit[0].Actor != "dave@example.com" || audit[1].Details["previous"] != "" {
		t.Errorf("grant audit: %v %+v", err, audit)
	}
	// Bob now reaches checkout with the rights of a member.
	bobc := newClient(t, srv)
	bobc.bootstrap()
	bobc.post("/v1/session", map[string]string{"email": "bob@example.com", "password": "bob@example.com passphrase"})
	if r := bobc.get(base + "/runs"); r.code != 200 {
		t.Errorf("bob reads checkout after the grant: %d %s", r.code, r.raw)
	}

	// What a grant may not name.
	for name, body := range map[string]map[string]any{
		"a workspace role":       {"email": "bob@example.com", "role": "workspace_admin"},
		"the owner role":         {"email": "bob@example.com", "role": "org_owner"},
		"no role":                {"email": "bob@example.com"},
		"no email":               {"role": "viewer"},
		"an over-long email":     {"email": strings.Repeat("a", 250) + "@x.io", "role": "viewer"},
		"a legacy P1-min role":   {"email": "bob@example.com", "role": "admin"},
		"an unknown role string": {"email": "bob@example.com", "role": "superuser"},
	} {
		if r := dave.post(base+"/members", body); r.code != 400 || r.errCode() != "INVALID_PAYLOAD" {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}
	for name, email := range map[string]string{"an unknown email": "nobody@example.com", "an unverified account": "eve@example.com"} {
		if r := dave.post(base+"/members", map[string]any{"email": email, "role": "viewer"}); r.code != 404 || r.errCode() != "USER_NOT_FOUND" {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}

	// Revoke: only the project-level row; inherited roles are not rows here.
	if r := dave.do("DELETE", base+"/members/"+bob.ID, nil, nil); r.code != 204 {
		t.Fatalf("revoke bob: %d %s", r.code, r.raw)
	}
	if r := bobc.get(base + "/runs"); r.code != 404 {
		t.Errorf("bob reads checkout after the revoke: %d %s", r.code, r.raw)
	}
	if r := dave.do("DELETE", base+"/members/"+bob.ID, nil, nil); r.code != 404 {
		t.Errorf("revoke twice: %d %s", r.code, r.raw)
	}
	daveID := ""
	for _, it := range dave.get(base + "/members").items() {
		if m := it.(map[string]any); m["email"] == "dave@example.com" {
			daveID = m["userId"].(string)
		}
	}
	if r := dave.do("DELETE", base+"/members/"+daveID, nil, nil); r.code != 404 {
		t.Errorf("revoking an inherited role from the project: %d %s", r.code, r.raw)
	}
	if r := dave.do("DELETE", base+"/members/not-a-uuid", nil, nil); r.code != 404 {
		t.Errorf("revoke a malformed id: %d %s", r.code, r.raw)
	}
	if audit, _ := az.ListAudit(ctx, string(checkout.ID), 10); len(audit) != 3 || audit[0].Action != "role.revoke" || audit[0].Actor != "dave@example.com" {
		t.Errorf("revoke audit: %+v", audit)
	}
	// Writes need the CSRF token like every other browser write.
	dave.csrf = ""
	if r := dave.post(base+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"}); r.code != 403 || r.errCode() != "CSRF" {
		t.Errorf("grant without CSRF: %d %s", r.code, r.raw)
	}
}
