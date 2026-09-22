package authz

import (
	"errors"
	"strings"
	"testing"
)

// TestMatrixMatchesSpec is §11.3.2 row by row, six columns in the SPEC's
// order (owner, admin, ws_admin, maintainer, member, viewer), so a change
// here is a visible diff against the table.
func TestMatrixMatchesSpec(t *testing.T) {
	cols := []Role{RoleOrgOwner, RoleOrgAdmin, RoleWorkspaceAdmin, RoleMaintainer, RoleMember, RoleViewer}
	rows := map[Action][6]bool{
		ActionView:             {true, true, true, true, true, true},
		ActionTriage:           {true, true, true, true, true, false},
		ActionIdentityMerge:    {true, true, true, true, false, false},
		ActionTokenManage:      {true, true, true, true, false, false},
		ActionProjectConfigure: {true, true, true, true, false, false},
		ActionProjectManage:    {true, true, true, false, false, false},
		ActionMemberManage:     {true, true, true, false, false, false},
		ActionOrgManage:        {true, false, false, false, false, false},
		ActionDataDelete:       {true, true, false, true, false, false}, // not a ladder: ws_admin ❌, maintainer ✅
		ActionTestmgmtWrite:    {true, true, true, true, true, false},
		ActionReleaseDecide:    {true, true, true, true, false, false},
		ActionIngest:           {false, false, false, false, false, false}, // humans never ingest
	}
	for a, want := range rows {
		for i, role := range cols {
			if got := roleAllows(role, a); got != want[i] {
				t.Errorf("%s / %s = %v, want %v", role, a, got, want[i])
			}
		}
	}
	if len(rows) != len(HumanActions)+1 {
		t.Errorf("the test covers %d actions, HumanActions lists %d — keep them in step with the SPEC table", len(rows)-1, len(HumanActions))
	}
	if roleAllows("superuser", ActionView) || roleAllows("admin", ActionView) {
		t.Error("unknown or retired role must not be allowed anything")
	}
	// A person on the chain with several roles acts with their union.
	if !anyAllows([]Role{RoleViewer, RoleMaintainer}, ActionDataDelete) || anyAllows([]Role{RoleViewer, RoleWorkspaceAdmin}, ActionDataDelete) {
		t.Error("anyAllows")
	}
	got := ActionsFor([]Role{RoleMember})
	if len(got) != 3 || got[0] != ActionView || got[1] != ActionTriage || got[2] != ActionTestmgmtWrite {
		t.Errorf("ActionsFor(member) = %v", got)
	}
	if len(ActionsFor(nil)) != 0 || len(ActionsFor([]Role{RoleOrgOwner})) != len(HumanActions) {
		t.Error("ActionsFor bounds")
	}
	// §11.3.1: every role has one level; every level of the tree has a role.
	for _, r := range Roles {
		if _, ok := RoleLevel[r]; !ok {
			t.Errorf("%s has no level", r)
		}
	}
	if RoleLevel[RoleOrgOwner] != LevelOrganization || RoleLevel[RoleWorkspaceAdmin] != LevelWorkspace || RoleLevel[RoleViewer] != LevelProject {
		t.Error("RoleLevel")
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
