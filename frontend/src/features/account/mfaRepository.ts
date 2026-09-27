import { ApiError, type ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";

// Two-factor authentication of the signed-in user (SPEC §11.2, openapi tag
// `session`, /v1/session/mfa…). Secrets and recovery codes are shown once;
// nothing here is cached.

export interface MfaStatus {
  enabled: boolean;
  required: boolean; // org owner: cannot be turned off
  recoveryCodesLeft: number;
}
export interface MfaSetup {
  secret: string; // base32
  otpauthUri: string;
}

// The api answers these by code; the client's status messages are too
// generic for a form that is about one field.
const messages: Record<string, string> = {
  INVALID_CODE:
    "That code is not valid. Codes change every 30 seconds and each works once.",
  MFA_MANDATORY: "Organization owners must keep two-factor authentication on.",
  RATE_LIMITED: "Too many wrong codes. Wait 15 minutes and try again.",
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

export class MfaRepository {
  constructor(private api: ApiClient) {}

  async status(signal: AbortSignal): Promise<MfaStatus> {
    const x = d.record(await this.api.request("/v1/session/mfa", { signal }));
    return {
      enabled: d.boolean(x.enabled),
      required: d.boolean(x.required),
      recoveryCodesLeft: d.integer(x.recoveryCodesLeft),
    };
  }
  async setup(signal: AbortSignal): Promise<MfaSetup> {
    const x = d.record(
      await explained(
        this.api.request("/v1/session/mfa/setup", { body: {}, signal }),
      ),
    );
    const otpauthUri = d.string(x.otpauthUri);
    // Only ever an authenticator URI: it becomes an href.
    if (!otpauthUri.startsWith("otpauth://totp/")) d.invalid();
    return { secret: d.string(x.secret), otpauthUri };
  }
  async enable(code: string, signal: AbortSignal): Promise<string[]> {
    const x = d.record(
      await explained(
        this.api.request("/v1/session/mfa/enable", { body: { code }, signal }),
      ),
    );
    return d.array(x.recoveryCodes, d.string, 10);
  }
  async disable(code: string, signal: AbortSignal) {
    await explained(
      this.api.request("/v1/session/mfa/disable", { body: { code }, signal }),
    );
  }
}
