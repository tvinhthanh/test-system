import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { ApiClient, ApiError, type Transport } from "../src/core/api/client";
import { servicesKey } from "../src/app/providers/services";
import { SessionRepository } from "../src/features/auth/sessionRepository";
import {
  MembersRepository,
  type AccessRequest,
} from "../src/features/members/membersRepository";
import SignupPage from "../src/features/auth/SignupPage.vue";
import AccessRequests from "../src/features/members/AccessRequests.vue";

// Self-serve sign-up (QĐ-17): the form, the mailed link, and the admin's
// queue of requests on the members screen.

const json = (body: unknown, status = 200) =>
  new Response(body === null ? null : JSON.stringify(body), { status });
const client = (transport: Transport) => {
  const api = new ApiClient(transport);
  api.csrfToken = "csrf-1";
  return api;
};
const signal = new AbortController().signal;

afterEach(() => {
  history.replaceState(null, "", "/");
});

it("reads the sign-up flag and sends the form and the mailed token as JSON with CSRF", async () => {
  const transport = vi.fn<Transport>(async (input) =>
    String(input) === "/v1/session/signup/complete"
      ? json({
          user: { id: "u1", name: "", email: "new@example.com" },
          csrfToken: "csrf-2",
        })
      : String(input) === "/v1/session"
        ? json({ user: null, csrfToken: "c", signup: true })
        : json(null, 202),
  );
  const repo = new SessionRepository(client(transport));
  expect((await repo.bootstrap()).signup).toBe(true);
  await repo.requestSignup({
    email: "new@example.com",
    scopeLevel: "project",
    scopeId: "p1",
    requestedRole: "member",
    note: "QA",
  });
  const session = await repo.completeSignup("atp_sgn_x", "a long passphrase");
  expect(session.user?.email).toBe("new@example.com");
  const [, ask] = transport.mock.calls[1]!;
  expect(transport.mock.calls[1]![0]).toBe("/v1/session/signup");
  expect(JSON.parse(String(ask?.body))).toEqual({
    email: "new@example.com",
    scopeLevel: "project",
    scopeId: "p1",
    requestedRole: "member",
    note: "QA",
  });
  expect((ask?.headers as Record<string, string>)["X-CSRF-Token"]).toBe(
    "csrf-1",
  );
  expect(JSON.parse(String(transport.mock.calls[2]![1]?.body))).toEqual({
    token: "atp_sgn_x",
    password: "a long passphrase",
  });
  // An older api without the flag reads as "off".
  const old = new SessionRepository(
    client(async () => json({ user: null, csrfToken: "c" })),
  );
  expect((await old.bootstrap()).signup).toBe(false);
});

const request = (over: Partial<AccessRequest> = {}) => ({
  id: "r1",
  userId: "u1",
  email: "new@example.com",
  requestedRole: "member",
  level: "project",
  note: "QA contractor",
  createdAt: "2026-10-30T08:00:00Z",
  ...over,
});

it("lists requests per node, refuses roles outside the vocabulary, and decides by encoded id", async () => {
  const transport = vi.fn<Transport>(async (input) =>
    String(input).endsWith("/access-requests")
      ? json({ items: [request()] })
      : json(null, 204),
  );
  const repo = new MembersRepository(client(transport));
  const node = { level: "workspace" as const, id: "w 1" };
  expect((await repo.accessRequests(node, signal))[0]?.note).toBe(
    "QA contractor",
  );
  expect(transport.mock.calls[0]![0]).toBe(
    "/v1/workspaces/w%201/access-requests",
  );
  await repo.decideAccessRequest(node, "r/1", "approved", "", signal);
  await repo.decideAccessRequest(
    node,
    "r1",
    "approved",
    "workspace_admin",
    signal,
  );
  await repo.decideAccessRequest(node, "r1", "denied", "", signal);
  expect(transport.mock.calls[1]![0]).toBe(
    "/v1/workspaces/w%201/access-requests/r%2F1/decide",
  );
  expect(
    transport.mock.calls.slice(1).map(([, init]) => JSON.parse(String(init?.body))),
  ).toEqual([
    { decision: "approved" },
    { decision: "approved", role: "workspace_admin" },
    { decision: "denied" },
  ]);
  const bad = new MembersRepository(
    client(async () => json({ items: [request({ requestedRole: "god" as never })] })),
  );
  await expect(bad.accessRequests(node, signal)).rejects.toThrow();
  const gone = new MembersRepository(
    client(async () =>
      json({ error: { code: "NOT_FOUND", message: "x" } }, 404),
    ),
  );
  await expect(
    gone.decideAccessRequest(node, "r1", "approved", "", signal),
  ).rejects.toThrow(/already decided/);
});

function session(over: Record<string, unknown> = {}) {
  return {
    ready: true,
    signup: true,
    ensure: vi.fn(async () => undefined),
    requestSignup: vi.fn(async () => undefined),
    completeSignup: vi.fn(async () => true),
    ...over,
  };
}
async function open(path: string, s: ReturnType<typeof session>) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/signup", component: SignupPage },
      { path: "/:any(.*)", component: { render: () => null } },
    ],
  });
  await router.push(path);
  await router.isReady();
  const wrapper = mount(SignupPage, {
    global: {
      plugins: [router],
      provide: { [servicesKey as symbol]: { session: s } },
    },
  });
  await flushPromises();
  return wrapper;
}

it("asks to join the node from the shared link and always says to check the email", async () => {
  const s = session();
  const wrapper = await open("/signup?project=p1", s);
  expect(wrapper.find(".signup-target").text()).toContain("a project");
  expect(wrapper.find("#signup-id").exists()).toBe(false);
  await wrapper.get("#signup-email").setValue(" new@example.com ");
  await wrapper.get("#signup-role").setValue("viewer");
  await wrapper.get("#signup-note").setValue("QA contractor");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(s.requestSignup).toHaveBeenCalledWith({
    email: "new@example.com",
    scopeLevel: "project",
    scopeId: "p1",
    requestedRole: "viewer",
    note: "QA contractor",
  });
  expect(wrapper.text()).toContain("Check your email");
});

it("without a link, asks which node; a role is offered per level", async () => {
  const wrapper = await open("/signup", session());
  expect(wrapper.find("#signup-id").exists()).toBe(true);
  const roles = () =>
    wrapper.findAll("#signup-role option").map((o) => o.attributes("value"));
  expect(roles()).toEqual(["member", "viewer", "maintainer"]);
  await wrapper.get("#signup-level").setValue("organization");
  expect(roles()).toEqual(["org_admin"]);
});

it("says so when sign-up is off, or the server does not answer", async () => {
  expect((await open("/signup", session({ signup: false }))).text()).toContain(
    "Sign-up is not open here",
  );
  expect((await open("/signup", session({ ready: false }))).text()).toContain(
    "The server did not answer",
  );
});

it("finishes from the mailed link: the token leaves the address bar and only travels in the body", async () => {
  history.replaceState(null, "", "/signup#atp_sgn_tok");
  const s = session();
  const wrapper = await open("/signup", s);
  expect(location.hash).toBe("");
  await wrapper.get("#signup-password").setValue("a long enough passphrase");
  await wrapper.get("#signup-confirm").setValue("a long enough passphrase");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(s.completeSignup).toHaveBeenCalledWith(
    "atp_sgn_tok",
    "a long enough passphrase",
  );
  expect(wrapper.text()).toContain("Your account is ready");
});

it("explains a spent link and an address that got an account", async () => {
  for (const [code, text] of [
    ["SIGNUP_NOT_FOUND", "This link cannot be used"],
    ["ACCOUNT_EXISTS", "You already have an account"],
  ] as const) {
    history.replaceState(null, "", "/signup#atp_sgn_tok");
    const s = session({
      completeSignup: vi.fn(async () => {
        throw new ApiError(404, code, "x");
      }),
    });
    const wrapper = await open("/signup", s);
    await wrapper.get("#signup-password").setValue("a long enough passphrase");
    await wrapper.get("#signup-confirm").setValue("a long enough passphrase");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(wrapper.text()).toContain(text);
  }
});

it("lets an admin approve as asked or with another role, and deny on a second click; owners only for owners", async () => {
  const members = {
    accessRequests: vi.fn(async () => [
      request({ id: "o", email: "wants.owner@example.com", requestedRole: "org_owner", level: "organization" }),
      request({ id: "a", email: "wants.admin@example.com", requestedRole: "org_admin", level: "organization" }),
    ]),
    decideAccessRequest: vi.fn(async () => undefined),
  };
  const wrapper = mount(AccessRequests, {
    props: { node: { level: "organization", id: "o1" }, canOwners: false },
    global: { provide: { [servicesKey as symbol]: { members } } },
  });
  await flushPromises();
  expect(
    (wrapper.get("input[aria-label='Sign-up link']").element as HTMLInputElement)
      .value,
  ).toBe(`${location.origin}/signup?organization=o1`);
  const rows = wrapper.findAll(".access-request");
  expect(rows).toHaveLength(2);
  expect(rows[0]!.text()).toContain("Only an organization owner");
  // An admin may only grant org_admin here: no select, and the owner
  // request is approved as org_admin (an override), the other as asked.
  expect(wrapper.find("select").exists()).toBe(false);
  await rows[0]!.findAll("button").find((b) => b.text() === "Approve")!.trigger("click");
  await flushPromises();
  await rows[1]!.findAll("button").find((b) => b.text() === "Approve")!.trigger("click");
  await flushPromises();
  const node = { level: "organization", id: "o1" };
  expect(members.decideAccessRequest.mock.calls.map((c) => c.slice(0, 4))).toEqual([
    [node, "o", "approved", "org_admin"],
    [node, "a", "approved", ""],
  ]);
  expect(wrapper.emitted("decided")).toHaveLength(2);
  const deny = () =>
    wrapper.findAll(".access-request")[0]!.findAll("button").find((b) => /Deny|Confirm/.test(b.text()))!;
  await deny().trigger("click");
  expect(members.decideAccessRequest).toHaveBeenCalledTimes(2);
  await deny().trigger("click");
  await flushPromises();
  expect(members.decideAccessRequest.mock.calls[2]!.slice(0, 4)).toEqual([
    node,
    "o",
    "denied",
    "",
  ]);
});
