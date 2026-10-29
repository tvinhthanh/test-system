// The role vocabulary of §11.3.1, shared by the repository (decoding) and the
// screens (the selects). Each role is granted at exactly one level.
export const projectRoles = ["maintainer", "member", "viewer"] as const;
export type ProjectRole = (typeof projectRoles)[number];
export const roles = [
  "org_owner",
  "org_admin",
  "workspace_admin",
  ...projectRoles,
] as const;
export type Role = (typeof roles)[number];
export const levels = ["organization", "workspace", "project"] as const;
export type Level = (typeof levels)[number];

// What a node's screen may grant (§11.3.1 "Gán ở tầng"). org_owner is only
// offered to owners (the server answers 403 OWNER_ONLY otherwise).
export const grantable: Record<Level, readonly Role[]> = {
  organization: ["org_admin", "org_owner"],
  workspace: ["workspace_admin"],
  project: projectRoles,
};

// What each role may do, in a few words (members screen, sign-up form).
export const roleHelp: Record<Role, string> = {
  org_owner:
    "everything, including billing and other owners; two-factor authentication required",
  org_admin: "workspaces, members and tokens across the organization",
  workspace_admin: "projects and members in this workspace",
  maintainer: "tokens, retention, identity merge, release decisions",
  member: "triage and test management",
  viewer: "read only",
};

// One node whose members a screen manages.
export interface MemberNode {
  level: Level;
  id: string;
}
