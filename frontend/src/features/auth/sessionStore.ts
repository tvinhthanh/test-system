import { defineStore } from "pinia";
import { ref, shallowRef } from "vue";
import type { ApiClient } from "../../core/api/client";
import { ApiError } from "../../core/api/client";
import {
  isChallenge,
  type MfaChallenge,
  type OAuthProvider,
  type SessionRepository,
  type Session,
  type User,
} from "./sessionRepository";

export function createSessionStore(
  api: ApiClient,
  repository: SessionRepository,
) {
  return defineStore("session", () => {
    const user = shallowRef<User | null>(null);
    const ready = ref(false);
    const busy = ref(false);
    const error = shallowRef<unknown>(null);
    // Set between a correct password and the second factor (SPEC §11.2).
    const challenge = shallowRef<MfaChallenge | null>(null);
    // Switched-on OAuth providers, from the bootstrap.
    const providers = shallowRef<OAuthProvider[]>([]);
    // Mail is on (bootstrap): teams can invite unknown emails.
    const invitations = ref(false);
    const passwordReset = ref(false);
    let bootstrap: Promise<void> | undefined;
    function apply(session: Session) {
      providers.value = session.providers;
      invitations.value = session.invitations;
      passwordReset.value = session.passwordReset;
      user.value = session.user;
      api.csrfToken = session.csrfToken;
    }
    function invalidate() {
      user.value = null;
      ready.value = false;
      api.clearSession();
    }
    api.onUnauthorized = invalidate;
    async function ensure(force = false) {
      if (bootstrap) return bootstrap;
      if (ready.value && !force) return;
      error.value = null;
      bootstrap = (async () => {
        try {
          apply(await repository.bootstrap());
          ready.value = true;
        } catch (e) {
          error.value = e;
          ready.value = false;
        } finally {
          bootstrap = undefined;
        }
      })();
      return bootstrap;
    }
    async function login(email: string, password: string) {
      if (busy.value) return false;
      busy.value = true;
      error.value = null;
      try {
        await ensure();
        if (!ready.value) return false;
        const result = await repository.login(email, password);
        if (isChallenge(result)) {
          challenge.value = result;
          return false;
        }
        apply(result);
        ready.value = true;
        if (!user.value) throw new Error("Missing session user");
        return true;
      } catch (e) {
        error.value =
          e instanceof ApiError && e.status === 401
            ? new ApiError(
                401,
                "INVALID_CREDENTIALS",
                "Email or password is incorrect.",
              )
            : e;
        return false;
      } finally {
        busy.value = false;
      }
    }
    async function completeMfa(code: string) {
      const pending = challenge.value;
      if (busy.value || pending === null) return false;
      busy.value = true;
      error.value = null;
      try {
        apply(await repository.completeMfa(pending.challenge, code));
        challenge.value = null;
        ready.value = true;
        if (!user.value) throw new Error("Missing session user");
        return true;
      } catch (e) {
        const reason = e instanceof ApiError ? e.code : "";
        if (reason === "MFA_CHALLENGE_EXPIRED") challenge.value = null;
        error.value =
          reason === "INVALID_CODE"
            ? new ApiError(
                400,
                reason,
                "That code is not valid. Codes change every 30 seconds and each works once.",
              )
            : reason === "MFA_CHALLENGE_EXPIRED"
              ? new ApiError(
                  401,
                  reason,
                  "That sign-in took too long. Enter your password again.",
                )
              : e;
        return false;
      } finally {
        busy.value = false;
      }
    }
    // After an OAuth sign-in the api keeps the challenge in an HttpOnly
    // cookie and sends the page to ?step=mfa: the code step runs with an
    // empty challenge, which the api reads from that cookie.
    function resumeMfa() {
      challenge.value = { challenge: "", expiresAt: "" };
    }
    // Accepting an invitation (§11.2): look it up, then set a password and
    // sign in. Errors stay with the invite page, not the session store.
    async function lookupInvitation(token: string) {
      await ensure();
      return repository.lookupInvitation(token);
    }
    async function acceptInvitation(token: string, password: string) {
      await ensure();
      apply(await repository.acceptInvitation(token, password));
      ready.value = true;
      return !!user.value;
    }
    async function forgotPassword(email: string) {
      await ensure();
      await repository.forgotPassword(email);
    }
    async function resetPassword(token: string, password: string) {
      await ensure();
      await repository.resetPassword(token, password);
    }
    function cancelMfa() {
      challenge.value = null;
      error.value = null;
    }
    // After MFA changes: the user's flags come from the bootstrap.
    async function refresh() {
      apply(await repository.bootstrap());
    }
    async function logout() {
      if (busy.value) return false;
      busy.value = true;
      error.value = null;
      try {
        await repository.logout();
        invalidate();
        return true;
      } catch (e) {
        error.value = e;
        return false;
      } finally {
        busy.value = false;
      }
    }
    return {
      user,
      ready,
      busy,
      error,
      challenge,
      providers,
      invitations,
      passwordReset,
      forgotPassword,
      changePassword: (current: string, password: string) =>
        repository.changePassword(current, password),
      resetPassword,
      lookupInvitation,
      acceptInvitation,
      ensure,
      login,
      completeMfa,
      cancelMfa,
      resumeMfa,
      refresh,
      logout,
    };
  });
}
