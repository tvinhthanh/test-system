import { createPinia } from "pinia";
import { createMemoryHistory } from "vue-router";
import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { SessionRepository } from "../src/features/auth/sessionRepository";
import { createSessionStore } from "../src/features/auth/sessionStore";
import { MfaRepository } from "../src/features/account/mfaRepository";
import { IdentitiesRepository } from "../src/features/account/identitiesRepository";
import { makeRouter } from "../src/app/router";

// The guard is under test, not the pages: stub the ones the router loads so
// a cold transform of real components cannot time the test out.
vi.mock("../src/app/AppShell.vue", () => ({ default: { render: () => null } }));
vi.mock("../src/features/account/SecurityPage.vue", () => ({
  default: { render: () => null },
}));
vi.mock("../src/features/reports/RunsPage.vue", () => ({
  default: { render: () => null },
}));

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const user = (mfa: { enabled: boolean; required: boolean }) => ({
  id: "u1",
  name: "owner",
  email: "owner@example.test",
  mfa,
});

function store(transport: Transport) {
  const api = new ApiClient(transport);
  return createSessionStore(api, new SessionRepository(api))(createPinia());
}

it("signs in in two steps when the password answers with a challenge", async () => {
  const calls: string[] = [];
  const transport = vi.fn<Transport>(async (input, init) => {
    const path = String(input);
    calls.push(`${init?.method ?? "GET"} ${path}`);
    if (path === "/v1/session" && init?.method === "POST")
      return json(
        {
          mfa: "required",
          challenge: "atp_mfa_x",
          expiresAt: "2026-09-27T09:05:00Z",
        },
        202,
      );
    if (path === "/v1/session/mfa") {
      const body = JSON.parse(String(init?.body)) as { code: string };
      return body.code === "123456"
        ? json({
            user: user({ enabled: true, required: true }),
            csrfToken: "c2",
          })
        : json({ error: { code: "INVALID_CODE", message: "x" } }, 400);
    }
    return json({ user: null, csrfToken: "c1" });
  });
  const s = store(transport);
  expect(await s.login("owner@example.test", "pw")).toBe(false);
  expect(s.user).toBeNull();
  expect(s.challenge?.challenge).toBe("atp_mfa_x");
  expect(s.error).toBeNull();

  expect(await s.completeMfa("000000")).toBe(false);
  expect(String((s.error as Error).message)).toContain("not valid");
  expect(s.challenge).not.toBeNull(); // try again with the same challenge

  expect(await s.completeMfa("123456")).toBe(true);
  expect(s.user?.mfa).toEqual({ enabled: true, required: true });
  expect(s.challenge).toBeNull();
  expect(calls).toContain("POST /v1/session/mfa");
});

it("drops an expired challenge so the password step comes back", async () => {
  const s = store(
    vi.fn<Transport>(async (input, init) =>
      String(input) === "/v1/session/mfa"
        ? json({ error: { code: "MFA_CHALLENGE_EXPIRED", message: "x" } }, 401)
        : init?.method === "POST"
          ? json(
              {
                mfa: "required",
                challenge: "c",
                expiresAt: "2026-09-27T09:05:00Z",
              },
              202,
            )
          : json({ user: null, csrfToken: "c1" }),
    ),
  );
  await s.login("a@example.test", "pw");
  expect(await s.completeMfa("123456")).toBe(false);
  expect(s.challenge).toBeNull();
  expect(String((s.error as Error).message)).toContain("took too long");
});

it("sends an owner without MFA to the security page, and nowhere else", async () => {
  let flags = { enabled: false, required: true };
  const s = store(
    vi.fn<Transport>(async () => json({ user: user(flags), csrfToken: "c1" })),
  );
  vi.stubGlobal("scrollTo", () => undefined); // jsdom has none
  const router = makeRouter(s, createMemoryHistory());
  await router.push("/p/commerce/runs");
  expect(router.currentRoute.value.path).toBe("/account/security");
  flags = { enabled: true, required: true };
  await s.refresh();
  await router.push("/p/commerce/runs");
  expect(router.currentRoute.value.path).toBe("/p/commerce/runs");
});

it("only accepts an authenticator URI as the setup link, and at most ten recovery codes", async () => {
  const reply = (body: unknown) => {
    const api = new ApiClient(async () => json(body));
    api.csrfToken = "c";
    return new MfaRepository(api);
  };
  const signal = new AbortController().signal;
  await expect(
    reply({ secret: "ABCD", otpauthUri: "javascript:alert(1)" }).setup(signal),
  ).rejects.toThrow();
  expect(
    (
      await reply({
        secret: "ABCD",
        otpauthUri: "otpauth://totp/ATP:a?secret=ABCD",
      }).setup(signal)
    ).secret,
  ).toBe("ABCD");
  await expect(
    reply({ recoveryCodes: Array(11).fill("aaaaa-bbbbb") }).enable("1", signal),
  ).rejects.toThrow();
});

it("offers only the OAuth providers the api switched on, ignoring names it does not know", async () => {
  const s = store(
    vi.fn<Transport>(async () =>
      json({ user: null, csrfToken: "c", providers: ["github", "gitlab"] }),
    ),
  );
  await s.ensure();
  expect(s.providers).toEqual(["github"]);
  const old = store(
    vi.fn<Transport>(async () => json({ user: null, csrfToken: "c" })),
  );
  await old.ensure();
  expect(old.providers).toEqual([]);
});

it("finishes an OAuth sign-in's code step with the challenge the api holds in its cookie", async () => {
  const bodies: unknown[] = [];
  const s = store(
    vi.fn<Transport>(async (input, init) => {
      if (String(input) === "/v1/session/mfa") {
        bodies.push(JSON.parse(String(init?.body)));
        return json({
          user: user({ enabled: true, required: false }),
          csrfToken: "c2",
        });
      }
      return json({ user: null, csrfToken: "c1", providers: ["google"] });
    }),
  );
  await s.ensure();
  s.resumeMfa();
  expect(await s.completeMfa("123456")).toBe(true);
  expect(bodies).toEqual([{ challenge: "", code: "123456" }]);
});

it("lists linked sign-ins and explains why the last one stays", async () => {
  const api = new ApiClient(async (_input, init) =>
    init?.method === "DELETE"
      ? json({ error: { code: "LAST_SIGN_IN", message: "x" } }, 409)
      : json({
          items: [
            {
              id: "i1",
              provider: "google",
              email: "a@x.io",
              createdAt: "2026-09-01T00:00:00Z",
              lastLoginAt: null,
            },
          ],
          nextCursor: null,
        }),
  );
  api.csrfToken = "c";
  const repo = new IdentitiesRepository(api);
  const signal = new AbortController().signal;
  expect((await repo.list(signal))[0]?.provider).toBe("google");
  await expect(repo.unlink("i1", signal)).rejects.toThrow(
    "This is your only way to sign in",
  );
});
