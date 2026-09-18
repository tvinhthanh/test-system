import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { ReportsRepository } from "../src/features/reports/reportsRepository";

const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
// Every write needs the CSRF double-submit token; the client refuses to send
// one without it, so the helper sets one like a bootstrapped session would.
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new ReportsRepository(client);
};

const side = {
  id: "11111111-1111-4111-8111-111111111111",
  canonicalTitle: "Checkout › accepts valid card",
  canonicalFilePath: "checkout.spec.ts",
  status: "active",
};
const item = {
  id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  createdAt: "2026-09-18T09:00:00Z",
  identity: side,
  candidate: { ...side, id: "22222222-2222-4222-8222-222222222222" },
  fingerprint: "fp1:abc",
  score: 0.84,
  runnerUpId: "",
  runnerUpScore: 0,
  gap: 0.09,
  breakdown: { title: 0.91, path: 0.8 },
  runId: "33333333-3333-4333-8333-333333333333",
};

it("decodes the review queue and refuses items whose sides are not identities", async () => {
  const transport = vi.fn<Transport>(async () =>
    json({ items: [item], nextCursor: null }),
  );
  const out = await repo(transport).reviewQueue("p", signal());
  expect(out).toEqual([item]);
  const url = new URL(String(transport.mock.calls[0]?.[0]), "http://localhost");
  expect(url.pathname).toBe("/v1/projects/p/review");
  expect(url.searchParams.get("limit")).toBe("100");

  const broken = vi.fn<Transport>(async () =>
    json({ items: [{ ...item, candidate: "not-an-object" }] }),
  );
  await expect(repo(broken).reviewQueue("p", signal())).rejects.toThrow();
  const badScore = vi.fn<Transport>(async () =>
    json({ items: [{ ...item, score: "high" }] }),
  );
  await expect(repo(badScore).reviewQueue("p", signal())).rejects.toThrow();
});

it("posts a decision with the CSRF header and surfaces 403/409 as ApiError", async () => {
  const calls: RequestInit[] = [];
  const transport = vi.fn<Transport>(async (_path, init) => {
    calls.push(init ?? {});
    return json({ id: item.id, status: "kept" });
  });
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  await new ReportsRepository(client).decideReview(
    "p",
    item.id,
    "kept",
    signal(),
  );
  expect(calls[0]?.method).toBe("POST");
  expect(JSON.parse(String(calls[0]?.body))).toEqual({ decision: "kept" });
  expect((calls[0]?.headers as Record<string, string>)["X-CSRF-Token"]).toBe(
    "csrf-1",
  );

  const forbidden = vi.fn<Transport>(async () =>
    json(
      {
        error: { code: "FORBIDDEN", message: "your role does not allow this" },
      },
      403,
    ),
  );
  await expect(
    repo(forbidden).decideReview("p", item.id, "merged", signal()),
  ).rejects.toMatchObject({ status: 403, code: "FORBIDDEN" });
  const conflict = vi.fn<Transport>(async () =>
    json({ error: { code: "CONFLICT", message: "item already kept" } }, 409),
  );
  await expect(
    repo(conflict).decideReview("p", item.id, "kept", signal()),
  ).rejects.toMatchObject({ status: 409, code: "CONFLICT" });
});

it("decodes aliases (validTo null = in force) and the identity status fields", async () => {
  const aliases = [
    {
      id: "a1",
      fingerprint: "fp1:abc",
      source: "exact",
      matchScore: 0,
      createdBy: "",
      validFrom: "2026-09-17T09:00:00Z",
      validTo: null,
    },
    {
      id: "a2",
      fingerprint: "fp1:old",
      source: "manual_merge",
      matchScore: 0.83,
      createdBy: "alice@example.com",
      validFrom: "2026-09-10T09:00:00Z",
      validTo: "2026-09-17T09:00:00Z",
    },
  ];
  const out = await repo(
    vi.fn<Transport>(async () => json({ items: aliases })),
  ).aliases("p", side.id, signal());
  expect(out).toEqual(aliases);

  const identity = {
    id: side.id,
    canonicalTitle: side.canonicalTitle,
    canonicalFilePath: side.canonicalFilePath,
    verdict: "stable",
    flakyScore: 0.04,
    totalRuns: 30,
    recentStatuses: ["passed"],
    status: "merged",
    mergedIntoId: "22222222-2222-4222-8222-222222222222",
  };
  const got = await repo(vi.fn<Transport>(async () => json(identity))).identity(
    "p",
    side.id,
    signal(),
  );
  expect(got.status).toBe("merged");
  expect(got.mergedIntoId).toBe(identity.mergedIntoId);
  await expect(
    repo(
      vi.fn<Transport>(async () => json({ ...identity, status: "zombie" })),
    ).identity("p", side.id, signal()),
  ).rejects.toThrow();
});

it("merge, undo and split own their paths and bodies; split returns the new id", async () => {
  const seen: { path: string; body: unknown }[] = [];
  const transport = vi.fn<Transport>(async (path, init) => {
    seen.push({
      path: String(path),
      body: init?.body ? JSON.parse(String(init.body)) : null,
    });
    return json({
      newIdentityId: "99999999-9999-4999-8999-999999999999",
      mergedInto: "x",
      restored: "y",
    });
  });
  const r = repo(transport);
  await r.mergeIdentity("p/x", "id/1", "target", signal());
  await r.undoMerge("p/x", "id/1", signal());
  const newId = await r.splitIdentity(
    "p/x",
    "id/1",
    "fp1:abc",
    "2026-09-18T09:00:00.000Z",
    signal(),
  );
  expect(newId).toBe("99999999-9999-4999-8999-999999999999");
  expect(seen.map((s) => s.path)).toEqual([
    "/v1/projects/p%2Fx/identities/id%2F1/merge",
    "/v1/projects/p%2Fx/identities/id%2F1/undo-merge",
    "/v1/projects/p%2Fx/identities/id%2F1/split",
  ]);
  expect(seen[0]?.body).toEqual({ into: "target" });
  expect(seen[2]?.body).toEqual({
    fingerprint: "fp1:abc",
    at: "2026-09-18T09:00:00.000Z",
  });
});
