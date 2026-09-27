package authz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

var pepper = []byte("test-pepper-test-pepper-test-pepper!")

// fakeService is a test double for the middleware: it needs Authenticate
// only, which is exactly why the interface lives on the owner side.
type fakeService struct {
	authz.Service
	accept string
	err    error
}

func (f fakeService) Authenticate(_ context.Context, bearer string) (authz.Principal, error) {
	if f.err != nil {
		return authz.Principal{}, f.err
	}
	if bearer == f.accept {
		return authz.Principal{Kind: authz.PrincipalToken, TokenID: "tok"}, nil
	}
	return authz.Principal{}, authz.ErrUnauthenticated
}

func TestMiddleware(t *testing.T) {
	var got authz.Principal
	var seen bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, seen = authz.PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	h := authz.Middleware(fakeService{accept: "good"})(next)

	call := func(header string) *httptest.ResponseRecorder {
		seen = false
		req := httptest.NewRequest("POST", "/v1/runs", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := call("Bearer good"); rec.Code != 204 || !seen || got.TokenID != "tok" {
		t.Errorf("valid token: %d seen=%v principal=%+v", rec.Code, seen, got)
	}
	if rec := call("bearer good"); rec.Code != 204 {
		t.Errorf("scheme is case-insensitive per RFC 7235: %d", rec.Code)
	}
	for _, bad := range []string{"", "Bearer", "Bearer bad", "Basic good", "good"} {
		rec := call(bad)
		if rec.Code != 401 || seen {
			t.Errorf("%q: code %d seen=%v", bad, rec.Code, seen)
		}
		if !strings.Contains(rec.Body.String(), `"code":"UNAUTHENTICATED"`) || rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%q: body %s", bad, rec.Body.String())
		}
	}
	down := authz.Middleware(fakeService{err: errors.New("db down")})(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/runs", nil)
	req.Header.Set("Authorization", "Bearer good")
	down.ServeHTTP(rec, req)
	if rec.Code != 503 || strings.Contains(rec.Body.String(), "db down") {
		t.Errorf("infrastructure failure: %d %s — must be 503 and must not echo the cause", rec.Code, rec.Body.String())
	}
}

func TestRejectsBeforeRoundTrip(t *testing.T) {
	svc := authz.New(nil, pepper)
	ctx := context.Background()
	p := "11111111-1111-4111-8111-111111111111"
	if _, err := svc.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{authz.LevelProject, "x"}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci"}); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("bad scope id: %v", err)
	}
	if _, err := svc.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{authz.LevelProject, p}, Name: "ci"}); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("no permissions: %v", err)
	}
	if _, err := svc.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{authz.LevelProject, p}, Permissions: []authz.Permission{"project:admin"}, Name: "ci"}); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("unknown permission: %v", err)
	}
	if _, err := svc.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{authz.LevelProject, p}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci", ExpiresAt: time.Now().Add(-time.Hour)}); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("past expiry: %v", err)
	}
	if _, err := svc.GrantRole(ctx, p, authz.ScopeRef{authz.LevelEnvironment, p}, authz.RoleMaintainer); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("membership on environment: %v", err)
	}
	if _, err := svc.GrantRole(ctx, p, authz.ScopeRef{authz.LevelProject, p}, "admin"); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("retired P1-min role name: %v", err)
	}
	// §11.3.1 "Gán ở tầng": a role only attaches at its own level.
	for _, bad := range []struct {
		level authz.Level
		role  authz.Role
	}{
		{authz.LevelProject, authz.RoleOrgOwner}, {authz.LevelProject, authz.RoleWorkspaceAdmin},
		{authz.LevelWorkspace, authz.RoleMember}, {authz.LevelOrganization, authz.RoleMaintainer},
	} {
		if _, err := svc.GrantRole(ctx, p, authz.ScopeRef{bad.level, p}, bad.role); !errors.Is(err, authz.ErrInvalid) {
			t.Errorf("%s at %s: %v, want invalid", bad.role, bad.level, err)
		}
	}
	if _, err := svc.CreateUser(ctx, "not-an-email"); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("bad email: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "atp_ing_nothex"); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("malformed bearer: %v", err)
	}
	if err := svc.Authorize(ctx, authz.Principal{Kind: authz.PrincipalUser, UserID: p}, authz.Resource{}, authz.ActionView); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("resource without org: %v", err)
	}
	if err := svc.RevokeToken(ctx, "nope", "tester"); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("malformed token id: %v", err)
	}
}

func TestTokensAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := authz.New(db, pepper)
	ctx := context.Background()
	project := "11111111-1111-4111-8111-111111111111"
	res := authz.Resource{OrgID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", WorkspaceID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", ProjectID: project}

	issued, err := svc.IssueToken(ctx, authz.TokenSpec{
		Scope: authz.ScopeRef{authz.LevelProject, project}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "github-actions",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued.Plaintext, authz.TokenPrefix) || len(issued.Plaintext) != len(authz.TokenPrefix)+64 {
		t.Errorf("plaintext = %q", issued.Plaintext)
	}
	if issued.Token.Prefix != issued.Plaintext[len(authz.TokenPrefix):len(authz.TokenPrefix)+8] {
		t.Errorf("stored prefix %q does not match plaintext", issued.Token.Prefix)
	}
	if d := time.Until(issued.Token.ExpiresAt); d < 364*24*time.Hour || d > 366*24*time.Hour {
		t.Errorf("default expiry not ~1 year: %v", d)
	}

	// The plaintext never lands in the database in any form.
	var hit bool
	_ = db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM api_tokens WHERE encode(token_hash, 'hex') = $1 OR prefix = $1)`, issued.Plaintext[len(authz.TokenPrefix):]).Scan(&hit)
	if hit {
		t.Fatal("secret stored in clear")
	}

	p, err := svc.Authenticate(ctx, issued.Plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != authz.PrincipalToken || p.TokenID != issued.Token.ID || p.TokenScope != (authz.ScopeRef{authz.LevelProject, project}) || len(p.Permissions) != 1 || p.Permissions[0] != authz.PermProjectIngest {
		t.Errorf("principal = %+v", p)
	}
	if err := svc.Authorize(ctx, p, res, authz.ActionIngest); err != nil {
		t.Errorf("authorize ingest: %v", err)
	}
	if err := svc.Authorize(ctx, p, res, authz.ActionView); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("write-only token viewing: %v", err)
	}

	// last_used_at was touched on first use.
	list, err := svc.ListTokens(ctx, authz.ScopeRef{authz.LevelProject, project})
	if err != nil || len(list) != 1 || list[0].LastUsedAt.IsZero() {
		t.Errorf("list = %+v, %v; want one token with last_used_at set", list, err)
	}

	// Wrong secret with the right prefix, wrong pepper, tampering: all 401.
	flipped := issued.Plaintext[:len(issued.Plaintext)-1] + map[bool]string{true: "1", false: "0"}[issued.Plaintext[len(issued.Plaintext)-1] == '0']
	if _, err := svc.Authenticate(ctx, flipped); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("tampered secret: %v", err)
	}
	other := authz.New(db, []byte("another-pepper-another-pepper-!!"))
	if _, err := other.Authenticate(ctx, issued.Plaintext); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("different pepper: %v", err)
	}

	// Revoke is immediate and idempotent.
	if err := svc.RevokeToken(ctx, issued.Token.ID, "ops@gate.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, issued.Plaintext); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("revoked token still authenticates: %v", err)
	}
	if err := svc.RevokeToken(ctx, issued.Token.ID, "ops@gate.example"); err != nil {
		t.Errorf("second revoke: %v", err)
	}
	if err := svc.RevokeToken(ctx, "00000000-0000-4000-8000-000000000000", "tester"); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("revoke unknown: %v", err)
	}
	// §11.7: exactly one audit row for the one revoke that changed something,
	// carrying the actor and the token's scope; no actor → refused, nothing changes.
	if err := svc.RevokeToken(ctx, issued.Token.ID, "  "); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("revoke without actor: %v", err)
	}
	audit, err := svc.ListAudit(ctx, project, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "token.revoke" || audit[0].SubjectID != issued.Token.ID || audit[0].Actor != "ops@gate.example" || audit[0].Scope.ID != project || audit[0].Details["name"] != "github-actions" {
		t.Errorf("audit after revoke: %+v", audit)
	}

	// Expired token.
	short, err := svc.IssueToken(ctx, authz.TokenSpec{
		Scope: authz.ScopeRef{authz.LevelProject, project}, Permissions: []authz.Permission{authz.PermProjectRead}, Name: "short", ExpiresAt: time.Now().Add(200 * time.Millisecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, short.Plaintext); err != nil {
		t.Errorf("fresh short token: %v", err)
	}
	// §11.4 reminder: the short-lived token is the only live one expiring
	// within 30 days (the revoked one and the one-year defaults are not).
	if n, err := svc.ExpiringTokens(ctx, 30*24*time.Hour); err != nil || n != 1 {
		t.Errorf("expiring within 30 days: %d %v, want 1", n, err)
	}
	time.Sleep(250 * time.Millisecond)
	if _, err := svc.Authenticate(ctx, short.Plaintext); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("expired token still authenticates: %v", err)
	}
	if n, err := svc.ExpiringTokens(ctx, 30*24*time.Hour); err != nil || n != 0 {
		t.Errorf("an expired token is not \"expiring\": %d %v", n, err)
	}
}

func TestMembershipsAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := authz.New(db, pepper)
	ctx := context.Background()
	org, ws, proj := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "11111111-1111-4111-8111-111111111111"
	res := authz.Resource{OrgID: org, WorkspaceID: ws, ProjectID: proj}
	orgOnly := authz.Resource{OrgID: org}

	alice, err := svc.CreateUser(ctx, "  Alice@Example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if alice.Email != "alice@example.com" {
		t.Errorf("email not normalised: %q", alice.Email)
	}
	if _, err := svc.CreateUser(ctx, "alice@example.com"); !errors.Is(err, authz.ErrConflict) {
		t.Errorf("duplicate email: %v", err)
	}
	if got, err := svc.GetUserByEmail(ctx, "ALICE@example.com"); err != nil || got.ID != alice.ID {
		t.Errorf("get by email: %+v %v", got, err)
	}
	bob, _ := svc.CreateUser(ctx, "bob@example.com")
	stranger, _ := svc.CreateUser(ctx, "eve@other.example")
	as := func(u authz.User) authz.Principal { return authz.Principal{Kind: authz.PrincipalUser, UserID: u.ID} }

	// Nobody has anything yet: 404-class, not 403-class.
	if err := svc.Authorize(ctx, as(alice), res, authz.ActionView); !errors.Is(err, authz.ErrNoAccess) {
		t.Errorf("no membership: %v", err)
	}

	// Org owner inherits down to the project and is the only one who may
	// manage the org itself; an org admin is refused there (§11.3.2 row 8).
	if _, err := svc.GrantRole(ctx, alice.ID, authz.ScopeRef{authz.LevelOrganization, org}, authz.RoleOrgOwner); err != nil {
		t.Fatal(err)
	}
	for _, a := range []authz.Action{authz.ActionView, authz.ActionTriage, authz.ActionIdentityMerge, authz.ActionTokenManage, authz.ActionProjectManage, authz.ActionDataDelete, authz.ActionReleaseDecide} {
		if err := svc.Authorize(ctx, as(alice), res, a); err != nil {
			t.Errorf("org owner %s on project: %v", a, err)
		}
	}
	if err := svc.Authorize(ctx, as(alice), orgOnly, authz.ActionOrgManage); err != nil {
		t.Errorf("org owner on org itself: %v", err)
	}
	if err := svc.Authorize(ctx, as(alice), res, authz.ActionIngest); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("humans never ingest: %v", err)
	}
	// The sole owner cannot step down; with a second owner in place, they can.
	if _, err := svc.GrantRole(ctx, alice.ID, authz.ScopeRef{authz.LevelOrganization, org}, authz.RoleOrgAdmin); !errors.Is(err, authz.ErrLastOwner) {
		t.Errorf("sole owner stepping down: %v", err)
	}
	coOwner, _ := svc.CreateUser(ctx, "co-owner@example.com")
	if _, err := svc.GrantRole(ctx, coOwner.ID, authz.ScopeRef{authz.LevelOrganization, org}, authz.RoleOrgOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GrantRole(ctx, alice.ID, authz.ScopeRef{authz.LevelOrganization, org}, authz.RoleOrgAdmin); err != nil {
		t.Fatal(err)
	}
	if err := svc.Authorize(ctx, as(alice), orgOnly, authz.ActionOrgManage); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("org admin managing the org (billing, delete): %v, want forbidden", err)
	}
	if err := svc.Authorize(ctx, as(alice), res, authz.ActionMemberManage); err != nil {
		t.Errorf("org admin managing members: %v", err)
	}
	if roles, err := svc.Roles(ctx, as(alice), res); err != nil || len(roles) != 1 || roles[0] != authz.RoleOrgAdmin {
		t.Errorf("Roles(alice) = %v, %v", roles, err)
	}

	// Project viewer: sees, cannot triage; on the org itself has no standing.
	if _, err := svc.GrantRole(ctx, bob.ID, authz.ScopeRef{authz.LevelProject, proj}, authz.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := svc.Authorize(ctx, as(bob), res, authz.ActionView); err != nil {
		t.Errorf("viewer view: %v", err)
	}
	if err := svc.Authorize(ctx, as(bob), res, authz.ActionTriage); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("viewer triage: %v, want forbidden (403)", err)
	}
	if err := svc.Authorize(ctx, as(bob), orgOnly, authz.ActionView); !errors.Is(err, authz.ErrNoAccess) {
		t.Errorf("project viewer at org level: %v, want no access", err)
	}
	otherProject := res
	otherProject.ProjectID = "22222222-2222-4222-8222-222222222222"
	if err := svc.Authorize(ctx, as(bob), otherProject, authz.ActionView); !errors.Is(err, authz.ErrNoAccess) {
		t.Errorf("viewer on sibling project: %v, want no access", err)
	}

	// Re-granting changes the role; memberships on the chain act as their union.
	if _, err := svc.GrantRole(ctx, bob.ID, authz.ScopeRef{authz.LevelProject, proj}, authz.RoleMember); err != nil {
		t.Fatal(err)
	}
	if err := svc.Authorize(ctx, as(bob), res, authz.ActionTriage); err != nil {
		t.Errorf("member triage after upgrade: %v", err)
	}
	if _, err := svc.GrantRole(ctx, bob.ID, authz.ScopeRef{authz.LevelWorkspace, ws}, authz.RoleWorkspaceAdmin); err != nil {
		t.Fatal(err)
	}
	if err := svc.Authorize(ctx, as(bob), res, authz.ActionTokenManage); err != nil {
		t.Errorf("workspace admin over project member: %v", err)
	}
	// The one non-ladder row: a workspace admin manages, does not destroy.
	if err := svc.Authorize(ctx, as(bob), res, authz.ActionDataDelete); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("workspace admin deleting run data: %v, want forbidden", err)
	}
	if _, err := svc.GrantRole(ctx, bob.ID, authz.ScopeRef{authz.LevelProject, proj}, authz.RoleMaintainer); err != nil {
		t.Fatal(err)
	}
	if err := svc.Authorize(ctx, as(bob), res, authz.ActionDataDelete); err != nil {
		t.Errorf("maintainer deleting run data: %v", err)
	}
	if err := svc.Authorize(ctx, as(bob), res, authz.ActionProjectManage); err != nil {
		t.Errorf("ws admin + maintainer, union, project.manage: %v", err)
	}
	ms, err := svc.ListMemberships(ctx, bob.ID)
	if err != nil || len(ms) != 2 {
		t.Errorf("memberships = %+v, %v", ms, err)
	}
	if roles, err := svc.Roles(ctx, as(bob), res); err != nil || len(roles) != 2 {
		t.Errorf("Roles(bob) = %v, %v", roles, err)
	}
	if roles, _ := svc.Roles(ctx, as(bob), otherProject); len(roles) != 1 || roles[0] != authz.RoleWorkspaceAdmin {
		t.Errorf("Roles(bob, sibling project) = %v (only the workspace role reaches it)", roles)
	}

	// Cross-tenant: the stranger is 404 everywhere (§11.3.3 release gate).
	for _, r := range []authz.Resource{res, orgOnly, otherProject} {
		for _, a := range []authz.Action{authz.ActionView, authz.ActionTriage, authz.ActionOrgManage} {
			if err := svc.Authorize(ctx, as(stranger), r, a); !errors.Is(err, authz.ErrNoAccess) {
				t.Errorf("stranger %s on %+v: %v", a, r, err)
			}
		}
	}

	// Revoke.
	if err := svc.RevokeRole(ctx, bob.ID, authz.ScopeRef{authz.LevelWorkspace, ws}, "ops@gate.example"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Authorize(ctx, as(bob), res, authz.ActionProjectManage); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("after revoking workspace admin (maintainer stays): %v", err)
	}
	if err := svc.RevokeRole(ctx, bob.ID, authz.ScopeRef{authz.LevelWorkspace, ws}, "ops@gate.example"); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("revoke twice: %v", err)
	}
	if audit, err := svc.ListAudit(ctx, ws, 10); err != nil || len(audit) != 1 || audit[0].Action != "role.revoke" || audit[0].Details["user_id"] != bob.ID || audit[0].Details["role"] != "workspace_admin" || audit[0].Actor != "ops@gate.example" {
		t.Errorf("audit after role revoke: %+v %v", audit, err)
	}
	if _, err := svc.GrantRole(ctx, "00000000-0000-4000-8000-000000000000", authz.ScopeRef{authz.LevelProject, proj}, authz.RoleViewer); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("grant to unknown user: %v", err)
	}
}
