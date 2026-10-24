import type { ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";

// Project API tokens for CI (SPEC §11.3.2 "Tạo/thu hồi ingest token",
// §11.4). The secret comes back once, from issue(); lists carry a prefix.
export type TokenPermission = "project:ingest" | "project:read";
export interface ProjectToken {
  id: string;
  prefix: string;
  name: string;
  permissions: TokenPermission[];
  createdAt: string;
  lastUsedAt: string | null;
  expiresAt: string;
  revokedAt: string | null;
}
export interface IssuedToken {
  token: ProjectToken;
  secret: string;
}

function permission(value: unknown): TokenPermission {
  const s = d.string(value);
  return s === "project:ingest" || s === "project:read" ? s : d.invalid();
}
function nullableDate(value: unknown) {
  return value === null ? null : d.date(value);
}
function token(value: unknown): ProjectToken {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    prefix: d.string(x.prefix),
    name: d.string(x.name),
    permissions: d.array(x.permissions, permission, 2),
    createdAt: d.date(x.createdAt),
    lastUsedAt: nullableDate(x.lastUsedAt),
    expiresAt: d.date(x.expiresAt),
    revokedAt: nullableDate(x.revokedAt),
  };
}
const base = (projectId: string) =>
  `/v1/projects/${encodeURIComponent(projectId)}/tokens`;

export class TokensRepository {
  constructor(private api: ApiClient) {}
  async list(projectId: string, signal: AbortSignal) {
    return d.array(
      d.record(await this.api.request(base(projectId), { signal })).items,
      token,
      500,
    );
  }
  async issue(
    projectId: string,
    input: {
      name: string;
      permissions: TokenPermission[];
      expiresInDays: number;
    },
    signal: AbortSignal,
  ): Promise<IssuedToken> {
    const x = d.record(
      await this.api.request(base(projectId), { signal, body: input }),
    );
    const secret = d.string(x.secret);
    if (!secret.startsWith("atp_")) return d.invalid();
    return { token: token(x.token), secret };
  }
  // The reporter tarball this server hands out (server/atp.sh packs it,
  // Caddy serves /downloads/, QĐ-14): a static file, not the /v1 API, so it
  // is read with plain fetch. null when it is not served here — `vite dev`
  // answers with index.html, a stack started without atp.sh has no index.
  async reporterPackage(signal: AbortSignal): Promise<string | null> {
    try {
      const r = await fetch("/downloads/index.json", {
        cache: "no-cache",
        signal,
      });
      if (!r.ok) return null;
      const file = d.record(await r.json()).file;
      return typeof file === "string" &&
        /^atp-playwright-reporter-[\w.-]+\.tgz$/.test(file)
        ? file
        : null;
    } catch {
      return null;
    }
  }
  async revoke(projectId: string, tokenId: string, signal: AbortSignal) {
    await this.api.request(
      `${base(projectId)}/${encodeURIComponent(tokenId)}`,
      {
        signal,
        method: "DELETE",
      },
    );
  }
}
