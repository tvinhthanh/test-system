package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
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

// Workspace and organization members (P2, §11.3.2 "tầng tương ứng"): what
// each admin reaches, what may be granted where, and the owner rules —
// only an owner touches the owner role, and an org keeps one owner.
func TestTeamMembers(t *testing.T) {
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
	ws2, _ := ten.CreateWorkspace(ctx, org.ID, "Search")
	other, _ := ten.CreateOrganization(ctx, "Globex", "globex")
	onOrg := func(o tenancy.OrganizationID) authz.ScopeRef {
		return authz.ScopeRef{Level: authz.LevelOrganization, ID: string(o)}
	}
	// signIn creates a verified account with a role and a browser for it.
	// An owner must pass MFA (§11.2): enrolled here, signed in with a
	// recovery code.
	signIn := func(email string, scope authz.ScopeRef, role authz.Role) (authz.User, *client) {
		u, _ := az.CreateUser(ctx, email)
		pw := email + " passphrase"
		_ = az.SetPassword(ctx, u.ID, pw, "")
		_ = az.VerifyEmail(ctx, u.ID)
		if role != "" {
			if _, err := az.GrantRole(ctx, u.ID, scope, role); err != nil {
				t.Fatal(err)
			}
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
		r := c.post("/v1/session", map[string]string{"email": email, "password": pw})
		if r.code == 202 {
			r = c.post("/v1/session/mfa", map[string]any{"challenge": r.body["challenge"], "code": codes[0]})
		}
		if r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return u, c
	}
	owen, owenc := signIn("owen@example.com", onOrg(org.ID), authz.RoleOrgOwner)
	ada, adac := signIn("ada@example.com", onOrg(org.ID), authz.RoleOrgAdmin)
	_, dave := signIn("dave@example.com", authz.ScopeRef{Level: authz.LevelWorkspace, ID: string(ws.ID)}, authz.RoleWorkspaceAdmin)
	bob, _ := signIn("bob@example.com", authz.ScopeRef{}, "")
	_, gus := signIn("gus@example.com", onOrg(other.ID), authz.RoleOrgOwner)
	orgPath, wsPath := "/v1/organizations/"+string(org.ID)+"/members", "/v1/workspaces/"+string(ws.ID)+"/members"

	scopes := func(c *client) string {
		var out []string
		for _, it := range c.get("/v1/member-scopes").items() {
			s := it.(map[string]any)
			out = append(out, s["level"].(string)+":"+s["name"].(string))
		}
		return strings.Join(out, " ")
	}
	if got := scopes(adac); got != "organization:Acme workspace:Payments workspace:Search" {
		t.Errorf("org admin scopes: %s", got)
	}
	if got := scopes(dave); got != "workspace:Payments" {
		t.Errorf("workspace admin scopes: %s", got)
	}

	// A workspace admin manages the workspace, sees the org's roles as
	// inherited, and reaches neither the org nor a sibling workspace.
	if r := dave.get(wsPath); r.code != 200 || len(r.items()) != 3 {
		t.Errorf("workspace members: %d %s", r.code, r.raw)
	}
	if r := dave.post(wsPath, map[string]any{"email": "bob@example.com", "role": "member"}); r.code != 400 || r.errCode() != "INVALID_PAYLOAD" {
		t.Errorf("project role on a workspace: %d %s", r.code, r.raw)
	}
	if r := dave.post(wsPath, map[string]any{"email": "bob@example.com", "role": "workspace_admin"}); r.code != 200 || r.body["level"] != "workspace" {
		t.Errorf("grant workspace admin: %d %s", r.code, r.raw)
	}
	if r := dave.do("DELETE", wsPath+"/"+bob.ID, nil, nil); r.code != 204 {
		t.Errorf("revoke workspace admin: %d %s", r.code, r.raw)
	}
	if r := dave.get(orgPath); r.code != 404 {
		t.Errorf("workspace admin reading the org: %d %s", r.code, r.raw)
	}
	if r := dave.get("/v1/workspaces/" + string(ws2.ID) + "/members"); r.code != 404 {
		t.Errorf("workspace admin reading a sibling workspace: %d %s", r.code, r.raw)
	}

	// An org admin manages admins but not owners.
	if r := adac.post(orgPath, map[string]any{"email": "bob@example.com", "role": "org_admin"}); r.code != 200 {
		t.Errorf("org admin grants org admin: %d %s", r.code, r.raw)
	}
	if r := adac.post(orgPath, map[string]any{"email": "bob@example.com", "role": "org_owner"}); r.code != 403 || r.errCode() != "OWNER_ONLY" {
		t.Errorf("org admin grants owner: %d %s", r.code, r.raw)
	}
	if r := adac.post(orgPath, map[string]any{"email": "owen@example.com", "role": "org_admin"}); r.code != 403 || r.errCode() != "OWNER_ONLY" {
		t.Errorf("org admin demotes the owner: %d %s", r.code, r.raw)
	}
	if r := adac.do("DELETE", orgPath+"/"+owen.ID, nil, nil); r.code != 403 || r.errCode() != "OWNER_ONLY" {
		t.Errorf("org admin removes the owner: %d %s", r.code, r.raw)
	}
	if r := adac.post(orgPath, map[string]any{"email": "bob@example.com", "role": "workspace_admin"}); r.code != 400 {
		t.Errorf("workspace role on an org: %d %s", r.code, r.raw)
	}

	// The owner: the last owner cannot leave or step down; with a second
	// owner, they can.
	if r := owenc.do("DELETE", orgPath+"/"+owen.ID, nil, nil); r.code != 409 || r.errCode() != "LAST_OWNER" {
		t.Errorf("last owner leaves: %d %s", r.code, r.raw)
	}
	if r := owenc.post(orgPath, map[string]any{"email": "owen@example.com", "role": "org_admin"}); r.code != 409 || r.errCode() != "LAST_OWNER" {
		t.Errorf("last owner steps down: %d %s", r.code, r.raw)
	}
	if r := owenc.post(orgPath, map[string]any{"email": "ada@example.com", "role": "org_owner"}); r.code != 200 || r.body["role"] != "org_owner" {
		t.Fatalf("owner makes ada owner: %d %s", r.code, r.raw)
	}
	// A new owner without MFA is held at enrolment (§11.2).
	if r := adac.get("/v1/member-scopes"); r.code != 403 || r.errCode() != "MFA_ENROLLMENT_REQUIRED" {
		t.Errorf("new owner without MFA: %d %s", r.code, r.raw)
	}
	if r := owenc.post(orgPath, map[string]any{"email": "owen@example.com", "role": "org_admin"}); r.code != 200 {
		t.Errorf("owner steps down with another owner in place: %d %s", r.code, r.raw)
	}
	if _, err := az.GrantRole(ctx, ada.ID, onOrg(org.ID), authz.RoleOrgAdmin); !errors.Is(err, authz.ErrLastOwner) {
		t.Errorf("CLI demoting the last owner: %v", err)
	}
	if err := az.RevokeRole(ctx, ada.ID, onOrg(org.ID), "ops@example.com"); !errors.Is(err, authz.ErrLastOwner) {
		t.Errorf("CLI removing the last owner: %v", err)
	}

	// Another org's owner reaches none of it.
	for _, path := range []string{orgPath, wsPath} {
		if r := gus.get(path); r.code != 404 {
			t.Errorf("foreign owner reading %s: %d %s", path, r.code, r.raw)
		}
		if r := gus.post(path, map[string]any{"email": "gus@example.com", "role": "workspace_admin"}); r.code != 404 {
			t.Errorf("foreign owner writing %s: %d %s", path, r.code, r.raw)
		}
	}
	audit, _ := az.ListAudit(ctx, string(org.ID), 20)
	if len(audit) < 3 {
		t.Errorf("org audit: %+v", audit)
	}
}
