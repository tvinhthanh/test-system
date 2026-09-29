import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { MembersRepository } from "../src/features/members/membersRepository";

const deleted = {
  id: "p-1",
  name: "checkout",
  deletedAt: "2026-09-29T08:00:00Z",
  deletedBy: "wanda@acme.example",
  purgeAfter: "2026-10-29T08:00:00Z",
};
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new MembersRepository(client);
};
const signal = () => new AbortController().signal;

it("deletes with the typed name in a DELETE body", async () => {
  const transport = vi.fn<Transport>(
    async () => new Response(JSON.stringify(deleted), { status: 200 }),
  );
  const out = await repo(transport).deleteProject("p-1", "checkout", signal());
  expect(out.purgeAfter).toBe(deleted.purgeAfter);
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe("/v1/projects/p-1");
  expect(init?.method).toBe("DELETE");
  expect(JSON.parse(String(init?.body))).toEqual({ confirmName: "checkout" });
});

it("lists deleted projects and restores one", async () => {
  const transport = vi.fn<Transport>(async (path) =>
    String(path).endsWith("/deleted")
      ? new Response(JSON.stringify({ items: [deleted], nextCursor: null }))
      : new Response(JSON.stringify({ id: "p-1", name: "checkout" })),
  );
  const r = repo(transport);
  expect((await r.deletedProjects("ws", signal()))[0]?.deletedBy).toBe(
    "wanda@acme.example",
  );
  expect(await r.restoreProject("ws", "p-1", signal())).toEqual({
    id: "p-1",
    name: "checkout",
  });
  expect(transport.mock.calls[1]![0]).toBe(
    "/v1/workspaces/ws/projects/p-1/restore",
  );
});
