import { ApiError, type ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";
import {
  projectActions,
  type ProjectAction,
} from "../reports/reportsRepository";

// Members (SPEC §11.3, openapi tag `members`) of a project, a workspace or
// an organization: everyone who reaches the node and through which node,
// plus the roles a holder of `member.manage` grants and revokes there.
// Roles held higher up are listed but changed at their own level.

import {
  levels,
  roles,
  type Level,
  type MemberNode,
  type ProjectRole,
  type Role,
} from "./roles";
export { projectRoles } from "./roles";
export type { Level, MemberNode, ProjectRole, Role };

export interface Member {
  userId: string;
  email: string;
  role: Role;
  level: Level;
  createdAt: string;
}
// An organization or workspace whose members the caller manages.
export interface MemberScope {
  level: "organization" | "workspace";
  id: string;
  name: string;
  organizationName: string;
  actions: ProjectAction[];
}

function oneOf<T extends string>(values: readonly T[], value: unknown): T {
  const s = d.string(value);
  return values.find((v) => v === s) ?? d.invalid();
}
export function parseMember(value: unknown): Member {
  const x = d.record(value);
  return {
    userId: d.string(x.userId),
    email: d.string(x.email),
    role: oneOf(roles, x.role),
    level: oneOf(levels, x.level),
    createdAt: d.date(x.createdAt),
  };
}
function parseScope(value: unknown): MemberScope {
  const x = d.record(value);
  return {
    level: oneOf(["organization", "workspace"] as const, x.level),
    id: d.string(x.id),
    name: d.string(x.name),
    organizationName: d.string(x.organizationName),
    actions: d.array(
      x.actions,
      (a) => oneOf(projectActions, a),
      projectActions.length,
    ),
  };
}

const collection: Record<Level, string> = {
  organization: "organizations",
  workspace: "workspaces",
  project: "projects",
};
const members = (n: MemberNode) =>
  `/v1/${collection[n.level]}/${encodeURIComponent(n.id)}/members`;

// The api answers these by code; the client's status messages are too
// generic for a form about one person.
const messages: Record<string, string> = {
  USER_NOT_FOUND:
    "No verified account uses that email. Check the spelling, or ask an operator to create the account first.",
  OWNER_ONLY:
    "Only an organization owner can grant, change or remove the owner role.",
  LAST_OWNER:
    "An organization keeps at least one owner. Make someone else owner first.",
};
async function explained<T>(call: Promise<T>): Promise<T> {
  try {
    return await call;
  } catch (e) {
    const message = e instanceof ApiError ? messages[e.code] : undefined;
    if (e instanceof ApiError && message)
      throw new ApiError(e.status, e.code, message, e.requestId);
    throw e;
  }
}

export class MembersRepository {
  constructor(private api: ApiClient) {}

  async page(node: MemberNode, cursor: string, signal: AbortSignal) {
    const query = new URLSearchParams({ limit: "100" });
    if (cursor) query.set("cursor", cursor);
    return d.page(
      await this.api.request(`${members(node)}?${query}`, { signal }),
      parseMember,
      100,
    );
  }
  // A roster is small (people, not test results); walk every page, bounded.
  async all(node: MemberNode, signal: AbortSignal, maxPages = 20) {
    const out: Member[] = [];
    let cursor = "";
    for (let i = 0; i < maxPages; i++) {
      const page = await this.page(node, cursor, signal);
      out.push(...page.items);
      if (!page.nextCursor) break;
      cursor = page.nextCursor;
    }
    return out;
  }
  async set(node: MemberNode, email: string, role: Role, signal: AbortSignal) {
    return parseMember(
      await explained(
        this.api.request(members(node), { body: { email, role }, signal }),
      ),
    );
  }
  async revoke(node: MemberNode, userId: string, signal: AbortSignal) {
    await explained(
      this.api.request(`${members(node)}/${encodeURIComponent(userId)}`, {
        method: "DELETE",
        signal,
      }),
    );
  }
  async scopes(signal: AbortSignal) {
    return d.array(
      d.record(await this.api.request("/v1/member-scopes", { signal })).items,
      parseScope,
      500,
    );
  }
}
