import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import {
  MembersRepository,
  parseMember,
} from "../src/features/members/membersRepository";
import { TeamAccess } from "../src/features/members/teamAccess";

const P = { level: "project", id: "p" } as const;

const row = (email: string, level = "project", role = "member") => ({
  userId: `u-${email}`,
  email,
  role,
  level,
  createdAt: "2026-09-27T08:00:00Z",
});

it("walks every page of the roster, bounded, forwarding the opaque cursor", async () => {
  const transport = vi.fn<Transport>(async (input) => {
    const cursor = new URL(String(input), "http://localhost").searchParams.get(
      "cursor",
    );
    return new Response(
      JSON.stringify(
        cursor === "next+/="
          ? {
              items: [row("b@x.io", "workspace", "workspace_admin")],
              nextCursor: null,
            }
          : { items: [row("a@x.io")], nextCursor: "next+/=" },
      ),
    );
  });
  const repo = new MembersRepository(new ApiClient(transport));
  const all = await repo.all(
    { level: "project", id: "project/a" },
    new AbortController().signal,
  );
  expect(all.map((m) => `${m.email}@${m.level}`)).toEqual([
    "a@x.io@project",
    "b@x.io@workspace",
  ]);
  const first = new URL(
    String(transport.mock.calls[0]?.[0]),
    "http://localhost",
  );
  expect(first.pathname).toBe("/v1/projects/project%2Fa/members");
  expect(first.searchParams.get("limit")).toBe("100");
  // A server that never ends the list cannot spin the page forever.
  const endless = vi.fn<Transport>(
    async () =>
      new Response(JSON.stringify({ items: [row("a@x.io")], nextCursor: "c" })),
  );
  await new MembersRepository(new ApiClient(endless)).all(
    { level: "project", id: "p" },
    new AbortController().signal,
    3,
  );
  expect(endless).toHaveBeenCalledTimes(3);
});

it("refuses roles and levels outside the backend vocabulary", () => {
  expect(parseMember(row("a@x.io", "project", "viewer")).role).toBe("viewer");
  expect(() => parseMember(row("a@x.io", "project", "admin"))).toThrow();
  expect(() => parseMember(row("a@x.io", "environment"))).toThrow();
});

it("grants with a JSON body and revokes by encoded user id", async () => {
  const transport = vi.fn<Transport>(async (_input, init) =>
    init?.method === "DELETE"
      ? new Response(null, { status: 204 })
      : new Response(JSON.stringify(row("a@x.io", "project", "viewer"))),
  );
  const api = new ApiClient(transport);
  api.csrfToken = "csrf";
  const repo = new MembersRepository(api);
  const signal = new AbortController().signal;
  const granted = await repo.set(P, "a@x.io", "viewer", signal);
  expect(granted.kind === "member" && granted.member.role).toBe("viewer");
  const [, init] = transport.mock.calls[0] ?? [];
  expect(init?.method).toBe("POST");
  expect(JSON.parse(String(init?.body))).toEqual({
    email: "a@x.io",
    role: "viewer",
  });
  await repo.revoke(P, "u/1", signal);
  expect(String(transport.mock.calls[1]?.[0])).toBe(
    "/v1/projects/p/members/u%2F1",
  );
  expect(transport.mock.calls[1]?.[1]?.method).toBe("DELETE");
});

it("turns USER_NOT_FOUND into advice, and leaves a missing project generic", async () => {
  const notFound = (code: string) =>
    vi.fn<Transport>(
      async () =>
        new Response(JSON.stringify({ error: { code, message: "x" } }), {
          status: 404,
        }),
    );
  const repo = (t: Transport) => {
    const api = new ApiClient(t);
    api.csrfToken = "csrf";
    return new MembersRepository(api);
  };
  const signal = new AbortController().signal;
  await expect(
    repo(notFound("USER_NOT_FOUND")).set(P, "a@x.io", "viewer", signal),
  ).rejects.toThrow("No verified account uses that email");
  await expect(
    repo(notFound("PROJECT_NOT_FOUND")).set(P, "a@x.io", "viewer", signal),
  ).rejects.toThrow("unavailable or no longer exists");
});

it("addresses workspaces and organizations by their own collections, and explains the owner rules", async () => {
  const seen: string[] = [];
  const transport = vi.fn<Transport>(async (input, init) => {
    seen.push(`${init?.method ?? "GET"} ${String(input)}`);
    if (String(input).includes("/organizations/"))
      return new Response(
        JSON.stringify({ error: { code: "OWNER_ONLY", message: "x" } }),
        { status: 403 },
      );
    return new Response(JSON.stringify({ items: [], nextCursor: null }));
  });
  const api = new ApiClient(transport);
  api.csrfToken = "c";
  const repo = new MembersRepository(api);
  const signal = new AbortController().signal;
  await repo.all({ level: "workspace", id: "w/1" }, signal);
  await expect(
    repo.set(
      { level: "organization", id: "o1" },
      "a@x.io",
      "org_owner",
      signal,
    ),
  ).rejects.toThrow("Only an organization owner");
  expect(seen[0]).toBe("GET /v1/workspaces/w%2F1/members?limit=100");
  expect(seen[1]).toBe("POST /v1/organizations/o1/members");
});

it("keeps asking for team scopes until it gets an answer, and forgets them on clear", async () => {
  let fail = true;
  const transport = vi.fn<Transport>(async () =>
    fail
      ? new Response(
          JSON.stringify({ error: { code: "MFA_ENROLLMENT_REQUIRED" } }),
          { status: 403 },
        )
      : new Response(
          JSON.stringify({
            items: [
              {
                level: "organization",
                id: "o1",
                name: "Acme",
                organizationName: "Acme",
                actions: ["view", "member.manage", "org.manage"],
              },
            ],
            nextCursor: null,
          }),
        ),
  );
  const team = new TeamAccess(new MembersRepository(new ApiClient(transport)));
  await team.ensure();
  expect(team.scopes).toEqual([]);
  fail = false;
  await team.ensure();
  expect(team.find("organization", "o1")?.actions).toContain("org.manage");
  await team.ensure();
  expect(transport).toHaveBeenCalledTimes(2); // cached once answered
  team.clear();
  expect(team.scopes).toEqual([]);
});

it("tells an invitation from a grant, and lists and withdraws invitations by node", async () => {
  const seen: string[] = [];
  const invitation = {
    id: "inv1",
    email: "new@x.io",
    role: "member",
    invitedBy: "dana@x.io",
    createdAt: "2026-09-28T00:00:00Z",
    expiresAt: "2026-10-05T00:00:00Z",
  };
  const api = new ApiClient(async (input, init) => {
    seen.push(`${init?.method ?? "GET"} ${String(input)}`);
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    if (String(input).endsWith("/invitations"))
      return new Response(
        JSON.stringify({ items: [invitation], nextCursor: null }),
      );
    return new Response(JSON.stringify({ invitation }), { status: 202 });
  });
  api.csrfToken = "c";
  const repo = new MembersRepository(api);
  const signal = new AbortController().signal;
  const added = await repo.set(P, "new@x.io", "member", signal);
  expect(added.kind).toBe("invited");
  expect((await repo.invitations(P, signal))[0]?.email).toBe("new@x.io");
  await repo.revokeInvitation(P, "inv1", signal);
  expect(seen).toEqual([
    "POST /v1/projects/p/members",
    "GET /v1/projects/p/invitations",
    "DELETE /v1/projects/p/invitations/inv1",
  ]);
});
