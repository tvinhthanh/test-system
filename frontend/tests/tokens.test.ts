import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { TokensRepository } from "../src/features/tokens/tokensRepository";

const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new TokensRepository(client);
};
const token = {
  id: "11111111-1111-4111-8111-111111111111",
  prefix: "atp_ing_ea3d400e",
  name: "github-actions",
  permissions: ["project:ingest"],
  createdAt: "2026-09-29T08:00:00Z",
  lastUsedAt: null,
  expiresAt: "2027-09-29T08:00:00Z",
  revokedAt: null,
};

it("issues a token and returns the secret once", async () => {
  const transport = vi.fn<Transport>(async () =>
    json({ token, secret: "atp_ing_ea3d400e0123" }, 201),
  );
  const out = await repo(transport).issue(
    "p",
    {
      name: "github-actions",
      permissions: ["project:ingest"],
      expiresInDays: 90,
    },
    signal(),
  );
  expect(out.secret).toBe("atp_ing_ea3d400e0123");
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe("/v1/projects/p/tokens");
  expect(JSON.parse(String(init?.body))).toEqual({
    name: "github-actions",
    permissions: ["project:ingest"],
    expiresInDays: 90,
  });
});

it("lists without secrets and refuses unknown permissions", async () => {
  const ok = vi.fn<Transport>(async () =>
    json({ items: [token], nextCursor: null }),
  );
  expect((await repo(ok).list("p", signal()))[0]?.prefix).toBe(
    "atp_ing_ea3d400e",
  );
  const bad = vi.fn<Transport>(async () =>
    json({
      items: [{ ...token, permissions: ["project:admin"] }],
      nextCursor: null,
    }),
  );
  await expect(repo(bad).list("p", signal())).rejects.toMatchObject({
    code: "INVALID_RESPONSE",
  });
});

it("revokes with DELETE and CSRF", async () => {
  const transport = vi.fn<Transport>(
    async () => new Response(null, { status: 204 }),
  );
  await repo(transport).revoke("p", token.id, signal());
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe(`/v1/projects/p/tokens/${token.id}`);
  expect(init?.method).toBe("DELETE");
  expect((init?.headers as Record<string, string>)["X-CSRF-Token"]).toBe(
    "csrf-1",
  );
});
