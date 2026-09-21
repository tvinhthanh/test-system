package authz

// The matrix §11.3.2 with the six columns folded to three (§4.4). One place,
// data not code, so a reviewer can diff it against the SPEC table.

// minRole is the weakest role that may perform an action. Actions absent
// here are not for humans at all (ingest).
var minRole = map[Action]Role{
	ActionView:             RoleViewer,
	ActionTriage:           RoleMember,
	ActionIdentityMerge:    RoleAdmin,
	ActionTokenManage:      RoleAdmin,
	ActionProjectConfigure: RoleAdmin,
	ActionProjectManage:    RoleAdmin,
	ActionMemberManage:     RoleAdmin,
	ActionOrgManage:        RoleAdmin,
	ActionDataDelete:       RoleAdmin,
	ActionTestmgmtWrite:    RoleMember,
	ActionReleaseDecide:    RoleAdmin,
}

var roleRank = map[Role]int{RoleViewer: 1, RoleMember: 2, RoleAdmin: 3}

// roleAllows reports whether r is at least the role an action needs.
func roleAllows(r Role, a Action) bool {
	need, ok := minRole[a]
	return ok && roleRank[r] >= roleRank[need]
}

// stronger returns the higher-ranked of two roles; "" ranks below all.
func stronger(a, b Role) Role {
	if roleRank[b] > roleRank[a] {
		return b
	}
	return a
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

var validRoles = map[Role]bool{RoleAdmin: true, RoleMember: true, RoleViewer: true}

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
