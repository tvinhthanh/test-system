import { ApiError, type ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";

// Project members (SPEC §11.3, openapi tag `members`): everyone who reaches
// the project and through which node, plus the project-level roles a holder
// of `member.manage` grants and revokes. Roles held on the workspace or org
// are listed but changed at their own level.

import {
  levels,
  roles,
  type Level,
  type ProjectRole,
  type Role,
} from "./roles";
export { projectRoles } from "./roles";
export type { Level, ProjectRole, Role };

export interface Member {
  userId: string;
  email: string;
  role: Role;
  level: Level;
  createdAt: string;
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

const members = (projectId: string) =>
  `/v1/projects/${encodeURIComponent(projectId)}/members`;

export class MembersRepository {
  constructor(private api: ApiClient) {}

  async page(projectId: string, cursor: string, signal: AbortSignal) {
    const query = new URLSearchParams({ limit: "100" });
    if (cursor) query.set("cursor", cursor);
    return d.page(
      await this.api.request(`${members(projectId)}?${query}`, { signal }),
      parseMember,
      100,
    );
  }
  // A roster is small (people, not test results); walk every page, bounded.
  async all(projectId: string, signal: AbortSignal, maxPages = 20) {
    const out: Member[] = [];
    let cursor = "";
    for (let i = 0; i < maxPages; i++) {
      const page = await this.page(projectId, cursor, signal);
      out.push(...page.items);
      if (!page.nextCursor) break;
      cursor = page.nextCursor;
    }
    return out;
  }
  async set(
    projectId: string,
    email: string,
    role: ProjectRole,
    signal: AbortSignal,
  ) {
    try {
      return parseMember(
        await this.api.request(members(projectId), {
          body: { email, role },
          signal,
        }),
      );
    } catch (e) {
      // The one 404 here that is not "this project is gone": say what to fix.
      if (e instanceof ApiError && e.code === "USER_NOT_FOUND")
        throw new ApiError(
          e.status,
          e.code,
          "No verified account uses that email. Check the spelling, or ask an operator to create the account first.",
          e.requestId,
        );
      throw e;
    }
  }
  async revoke(projectId: string, userId: string, signal: AbortSignal) {
    await this.api.request(
      `${members(projectId)}/${encodeURIComponent(userId)}`,
      { method: "DELETE", signal },
    );
  }
}
