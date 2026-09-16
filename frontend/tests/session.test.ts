import { createPinia } from "pinia";
import { it, expect, vi } from "vitest";
import { ApiClient } from "../src/core/api/client";
import { SessionRepository } from "../src/features/auth/sessionRepository";
import { createSessionStore } from "../src/features/auth/sessionStore";

it("deduplicates session bootstrap across concurrent guards", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(JSON.stringify({ user: null, csrfToken: "token" })),
  );
  const api = new ApiClient(fetcher);
  const store = createSessionStore(
    api,
    new SessionRepository(api),
  )(createPinia());
  await Promise.all([store.ensure(), store.ensure(), store.ensure()]);
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(api.csrfToken).toBe("token");
});

it("rejects duplicate login submissions while a login is pending", async () => {
  let finish!: (response: Response) => void;
  const fetcher = vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) =>
    init?.method === "POST"
      ? new Promise<Response>((r) => {
          finish = r;
        })
      : new Response(JSON.stringify({ user: null, csrfToken: "csrf" })),
  );
  const api = new ApiClient(fetcher);
  const store = createSessionStore(
    api,
    new SessionRepository(api),
  )(createPinia());
  await store.ensure();
  const first = store.login("a@example.test", "password");
  await Promise.resolve();
  expect(await store.login("a@example.test", "password")).toBe(false);
  finish(
    new Response(
      JSON.stringify({
        user: { id: "1", name: "A", email: "a@example.test" },
        csrfToken: "new",
      }),
    ),
  );
  expect(await first).toBe(true);
  expect(fetcher).toHaveBeenCalledTimes(2);
});
