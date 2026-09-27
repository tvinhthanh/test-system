// The role vocabulary of §11.3.1, shared by the repository (decoding) and the
// screen (the select). Project roles are the only ones granted from a project.
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
