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
export interface Session {
  user: User | null;
  csrfToken: string;
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
  return {
    csrfToken: string(json.csrfToken),
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
  async logout() {
    await this.api.request("/v1/session/logout", { body: {} });
  }
}
