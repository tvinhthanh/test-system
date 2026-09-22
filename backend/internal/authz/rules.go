package authz

// The matrix §11.3.2, six columns, one row per action. Data, not code, so a
// reviewer can diff it against the SPEC table line by line. Rows are not a
// ladder: "delete run/artifact data" is owner/admin/maintainer but not
// workspace_admin, so a role rank would misstate the table — each row lists
// exactly who may.

var (
	everyone     = []Role{RoleOrgOwner, RoleOrgAdmin, RoleWorkspaceAdmin, RoleMaintainer, RoleMember, RoleViewer}
	upToMember   = []Role{RoleOrgOwner, RoleOrgAdmin, RoleWorkspaceAdmin, RoleMaintainer, RoleMember}
	upToMaint    = []Role{RoleOrgOwner, RoleOrgAdmin, RoleWorkspaceAdmin, RoleMaintainer}
	adminsOnly   = []Role{RoleOrgOwner, RoleOrgAdmin, RoleWorkspaceAdmin}
	ownerOnly    = []Role{RoleOrgOwner}
	dataDeleters = []Role{RoleOrgOwner, RoleOrgAdmin, RoleMaintainer} // ws_admin manages, does not destroy
)

// matrix is §11.3.2 row by row. Actions absent here are not for humans at
// all (ingest).
var matrix = map[Action][]Role{
	ActionView:             everyone,
	ActionTriage:           upToMember,
	ActionIdentityMerge:    upToMaint,
	ActionTokenManage:      upToMaint,
	ActionProjectConfigure: upToMaint,
	ActionProjectManage:    adminsOnly,
	ActionMemberManage:     adminsOnly,
	ActionOrgManage:        ownerOnly,
	ActionDataDelete:       dataDeleters,
	ActionTestmgmtWrite:    upToMember,
	ActionReleaseDecide:    upToMaint,
}

// allowed is matrix as a lookup.
var allowed = func() map[Action]map[Role]bool {
	out := map[Action]map[Role]bool{}
	for a, roles := range matrix {
		out[a] = map[Role]bool{}
		for _, r := range roles {
			out[a][r] = true
		}
	}
	return out
}()

// HumanActions is every action of the matrix, in SPEC order — what
// ActionsFor enumerates and what the projects list hands the UI.
var HumanActions = []Action{
	ActionView, ActionTriage, ActionIdentityMerge, ActionTokenManage, ActionProjectConfigure,
	ActionProjectManage, ActionMemberManage, ActionOrgManage, ActionDataDelete,
	ActionTestmgmtWrite, ActionReleaseDecide,
}

// roleAllows reports whether r may perform a.
func roleAllows(r Role, a Action) bool { return allowed[a][r] }

// anyAllows reports whether at least one of the roles may perform a — a
// person holding several memberships on the chain acts with the union.
func anyAllows(roles []Role, a Action) bool {
	for _, r := range roles {
		if roleAllows(r, a) {
			return true
		}
	}
	return false
}

// ActionsFor is the actions a set of held roles unlocks, in HumanActions
// order — the projects list answers this per project so the UI hides what
// the person may not do (§11.3.2 "hành động không có quyền → ẩn nút").
func ActionsFor(roles []Role) []Action {
	out := make([]Action, 0, len(HumanActions))
	for _, a := range HumanActions {
		if anyAllows(roles, a) {
			out = append(out, a)
		}
	}
	return out
}

// RoleLevel is §11.3.1 "Gán ở tầng": the one level a role is attached at.
// Inheritance is downwards only, so an org role covers every project and a
// project role covers nothing above it.
var RoleLevel = map[Role]Level{
	RoleOrgOwner:       LevelOrganization,
	RoleOrgAdmin:       LevelOrganization,
	RoleWorkspaceAdmin: LevelWorkspace,
	RoleMaintainer:     LevelProject,
	RoleMember:         LevelProject,
	RoleViewer:         LevelProject,
}

// permissionActions is what a token permission unlocks (§8.8). Tokens never
// reach the human matrix above.
var permissionActions = map[Permission]map[Action]bool{
	PermProjectIngest: {ActionIngest: true},
	PermProjectRead:   {ActionView: true}, // also reads a release gate for CI (§6.8.4 #2)
}

func permissionsAllow(perms []Permission, a Action) bool {
	for _, p := range perms {
		if permissionActions[p][a] {
			return true
		}
	}
	return false
}

var validPermissions = map[Permission]bool{PermProjectIngest: true, PermProjectRead: true}

// membershipLevels is where a role may be attached; environment is for
// tokens only.
var membershipLevels = map[Level]bool{LevelOrganization: true, LevelWorkspace: true, LevelProject: true}

var tokenLevels = map[Level]bool{LevelOrganization: true, LevelWorkspace: true, LevelProject: true, LevelEnvironment: true}

// idAt returns the id the resource has at a level, or "".
func (r Resource) idAt(l Level) string {
	switch l {
	case LevelOrganization:
		return r.OrgID
	case LevelWorkspace:
		return r.WorkspaceID
	case LevelProject:
		return r.ProjectID
	case LevelEnvironment:
		return r.EnvironmentID
	}
	return ""
}
