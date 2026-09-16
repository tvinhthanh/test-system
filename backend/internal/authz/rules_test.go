package authz

import (
	"errors"
	"strings"
	"testing"
)

// TestMatrixMatchesSpec is §11.3.2 folded to three roles, row by row, so a
// change here is a visible diff against the SPEC table.
func TestMatrixMatchesSpec(t *testing.T) {
	type row struct {
		action                Action
		admin, member, viewer bool
	}
	rows := []row{
		{ActionView, true, true, true},
		{ActionTriage, true, true, false},
		{ActionIdentityMerge, true, false, false},
		{ActionTokenManage, true, false, false},
		{ActionProjectConfigure, true, false, false},
		{ActionProjectManage, true, false, false},
		{ActionMemberManage, true, false, false},
		{ActionOrgManage, true, false, false},
		{ActionDataDelete, true, false, false},
		{ActionIngest, false, false, false}, // humans never ingest
	}
	for _, r := range rows {
		for role, want := range map[Role]bool{RoleAdmin: r.admin, RoleMember: r.member, RoleViewer: r.viewer} {
			if got := roleAllows(role, r.action); got != want {
				t.Errorf("%s / %s = %v, want %v", role, r.action, got, want)
			}
		}
	}
	if roleAllows("superuser", ActionView) {
		t.Error("unknown role must not be allowed anything")
	}
	if stronger(RoleViewer, RoleAdmin) != RoleAdmin || stronger("", RoleViewer) != RoleViewer || stronger(RoleMember, RoleViewer) != RoleMember {
		t.Error("stronger")
	}
}

func TestTokenFormat(t *testing.T) {
	s := newSecret()
	if len(s) != secretHexLen || strings.ToLower(s) != s {
		t.Fatalf("secret = %q", s)
	}
	if s2 := newSecret(); s2 == s {
		t.Fatal("two secrets equal")
	}
	prefix, secret, ok := splitBearer(TokenPrefix + s)
	if !ok || prefix != s[:8] || secret != s {
		t.Errorf("split = %q %q %v", prefix, secret, ok)
	}
	for _, bad := range []string{
		"", "Bearer x", TokenPrefix, TokenPrefix + s[:63], TokenPrefix + s + "0",
		TokenPrefix + strings.ToUpper(s), "atp_api_" + s, strings.Replace(TokenPrefix+s, "a", "g", 1),
	} {
		if _, _, ok := splitBearer(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	pepper := []byte("0123456789abcdef0123456789abcdef")
	h1 := hashSecret(pepper, s)
	if !hashEqual(h1, hashSecret(pepper, s)) || hashEqual(h1, hashSecret([]byte("other-pepper-other-pepper-other!"), s)) || hashEqual(h1, hashSecret(pepper, newSecret())) {
		t.Error("hash should depend on both pepper and secret")
	}
}

func TestAuthorizeTokenNeedsNoDatabase(t *testing.T) {
	proj := "11111111-1111-4111-8111-111111111111"
	res := Resource{OrgID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", WorkspaceID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", ProjectID: proj}
	ingest := Principal{Kind: PrincipalToken, TokenID: "t", TokenScope: ScopeRef{LevelProject, proj}, Permissions: []Permission{PermProjectIngest}}

	if err := authorizeToken(ingest, res, ActionIngest); err != nil {
		t.Errorf("ingest on own project: %v", err)
	}
	if err := authorizeToken(ingest, res, ActionView); !errors.Is(err, ErrForbidden) {
		t.Errorf("write-only token reading: %v, want forbidden", err)
	}
	other := res
	other.ProjectID = "22222222-2222-4222-8222-222222222222"
	if err := authorizeToken(ingest, other, ActionIngest); !errors.Is(err, ErrNoAccess) {
		t.Errorf("token on another project: %v, want no access (404, §7.2)", err)
	}
	orgTok := Principal{Kind: PrincipalToken, TokenScope: ScopeRef{LevelOrganization, res.OrgID}, Permissions: []Permission{PermProjectRead}}
	if err := authorizeToken(orgTok, res, ActionView); err != nil {
		t.Errorf("org-scoped token covers the project below: %v", err)
	}
	if err := authorizeToken(orgTok, res, ActionTriage); !errors.Is(err, ErrForbidden) {
		t.Errorf("tokens never get human actions: %v", err)
	}
	envTok := Principal{Kind: PrincipalToken, TokenScope: ScopeRef{LevelEnvironment, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"}, Permissions: []Permission{PermProjectIngest}}
	if err := authorizeToken(envTok, res, ActionIngest); !errors.Is(err, ErrNoAccess) {
		t.Errorf("env token against a resource with no environment: %v", err)
	}
	res.EnvironmentID = envTok.TokenScope.ID
	if err := authorizeToken(envTok, res, ActionIngest); err != nil {
		t.Errorf("env token on its environment: %v", err)
	}
}
