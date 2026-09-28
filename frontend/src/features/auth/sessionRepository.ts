import type { ApiClient } from "../../core/api/client";
import { boolean, date, record, string } from "../../core/api/decode";

export interface MfaFlags {
  enabled: boolean;
  // An org owner (SPEC §11.2): MFA cannot be off, and until it is on the
  // api answers 403 MFA_ENROLLMENT_REQUIRED outside the session routes.
  required: boolean;
}
export interface User {
  id: string;
  name: string;
  email: string;
  mfa: MfaFlags | null;
}
// OAuth sign-ins the api has switched on (SPEC §11.2; OAUTH_GOOGLE /
// OAUTH_GITHUB in its configuration).
export const oauthProviders = ["google", "github"] as const;
export type OAuthProvider = (typeof oauthProviders)[number];
export interface Session {
  user: User | null;
  csrfToken: string;
  providers: OAuthProvider[];
  // Mail is on: adding an email with no account to a team invites it.
  invitations: boolean;
  // Forgot-password by email is available.
  passwordReset: boolean;
}
// A correct password on an account with MFA on: no session yet (202).
export interface MfaChallenge {
  challenge: string;
  expiresAt: string;
}
function parseSession(value: unknown): Session {
  const json = record(value);
  const user = json.user === null ? null : record(json.user);
  const mfa = user && user.mfa !== undefined ? record(user.mfa) : null;
  // Unknown names are skipped, not fatal: an older client must still sign in.
  const providers = Array.isArray(json.providers)
    ? oauthProviders.filter((p) => (json.providers as unknown[]).includes(p))
    : [];
  return {
    csrfToken: string(json.csrfToken),
    providers,
    invitations: json.invitations === true,
    passwordReset: json.passwordReset === true,
    user: user
      ? {
          id: string(user.id),
          name: string(user.name),
          email: string(user.email),
          mfa: mfa
            ? { enabled: boolean(mfa.enabled), required: boolean(mfa.required) }
            : null,
        }
      : null,
  };
}
function parseLogin(value: unknown): Session | MfaChallenge {
  const json = record(value);
  if (json.mfa === "required")
    return {
      challenge: string(json.challenge),
      expiresAt: date(json.expiresAt),
    };
  return parseSession(json);
}
export const isChallenge = (
  result: Session | MfaChallenge,
): result is MfaChallenge => "challenge" in result;

// An open invitation, as the accept page shows it (SPEC §11.2).
export interface InvitationInfo {
  email: string;
  role: string;
  level: "organization" | "workspace" | "project";
  scopeName: string;
  invitedBy: string;
  expiresAt: string;
}
function parseInvitation(value: unknown): InvitationInfo {
  const x = record(value);
  const level = string(x.level);
  if (level !== "organization" && level !== "workspace" && level !== "project")
    throw new Error("Invalid invitation level");
  return {
    email: string(x.email),
    role: string(x.role),
    level,
    scopeName: string(x.scopeName),
    invitedBy: string(x.invitedBy),
    expiresAt: date(x.expiresAt),
  };
}

export class SessionRepository {
  constructor(private api: ApiClient) {}
  async bootstrap() {
    return parseSession(await this.api.request("/v1/session"));
  }
  async login(email: string, password: string) {
    return parseLogin(
      await this.api.request("/v1/session", { body: { email, password } }),
    );
  }
  async completeMfa(challenge: string, code: string) {
    return parseSession(
      await this.api.request("/v1/session/mfa", { body: { challenge, code } }),
    );
  }
  // The token only ever travels in a body (the link carries it in a
  // fragment), never in a URL the server would log.
  async lookupInvitation(token: string) {
    return parseInvitation(
      await this.api.request("/v1/invitations/lookup", { body: { token } }),
    );
  }
  async acceptInvitation(token: string, password: string) {
    return parseSession(
      await this.api.request("/v1/invitations/accept", {
        body: { token, password },
      }),
    );
  }
  // Forgot password (SPEC §11.2): the answer is the same for any email.
  async forgotPassword(email: string) {
    await this.api.request("/v1/session/password/forgot", { body: { email } });
  }
  async resetPassword(token: string, password: string) {
    await this.api.request("/v1/session/password/reset", {
      body: { token, password },
    });
  }
  // §11.2: the signed-in person changes their own password; other
  // sessions end, this one stays.
  async changePassword(current: string, password: string) {
    await this.api.request("/v1/session/password", {
      body: { current, password },
    });
  }
  async logout() {
    await this.api.request("/v1/session/logout", { body: {} });
  }
}
