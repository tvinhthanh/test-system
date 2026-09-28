import { ApiError, type ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";
import { oauthProviders, type OAuthProvider } from "../auth/sessionRepository";

// The Google / GitHub sign-ins linked to the signed-in account (SPEC §11.2;
// GET/DELETE /v1/session/identities). Only the caller's own are listed.

export interface LinkedIdentity {
  id: string;
  provider: OAuthProvider;
  email: string;
  createdAt: string;
  lastLoginAt: string | null;
}

function parse(value: unknown): LinkedIdentity {
  const x = d.record(value);
  const provider = d.string(x.provider);
  return {
    id: d.string(x.id),
    provider: oauthProviders.find((p) => p === provider) ?? d.invalid(),
    email: d.string(x.email),
    createdAt: d.date(x.createdAt),
    lastLoginAt: x.lastLoginAt === null ? null : d.date(x.lastLoginAt),
  };
}

export class IdentitiesRepository {
  constructor(private api: ApiClient) {}

  async list(signal: AbortSignal) {
    return d.array(
      d.record(await this.api.request("/v1/session/identities", { signal }))
        .items,
      parse,
      oauthProviders.length,
    );
  }
  async unlink(id: string, signal: AbortSignal) {
    try {
      await this.api.request(
        `/v1/session/identities/${encodeURIComponent(id)}`,
        { method: "DELETE", signal },
      );
    } catch (e) {
      if (e instanceof ApiError && e.code === "LAST_SIGN_IN")
        throw new ApiError(
          e.status,
          e.code,
          "This is your only way to sign in. Link another provider first, or ask an admin to set a password.",
          e.requestId,
        );
      throw e;
    }
  }
}
