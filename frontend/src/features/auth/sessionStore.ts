import { defineStore } from "pinia";
import { ref, shallowRef } from "vue";
import type { ApiClient } from "../../core/api/client";
import { ApiError } from "../../core/api/client";
import type { SessionRepository, Session, User } from "./sessionRepository";

export function createSessionStore(
  api: ApiClient,
  repository: SessionRepository,
) {
  return defineStore("session", () => {
    const user = shallowRef<User | null>(null);
    const ready = ref(false);
    const busy = ref(false);
    const error = shallowRef<unknown>(null);
    let bootstrap: Promise<void> | undefined;
    function apply(session: Session) {
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
        apply(await repository.login(email, password));
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
    return { user, ready, busy, error, ensure, login, logout };
  });
}
