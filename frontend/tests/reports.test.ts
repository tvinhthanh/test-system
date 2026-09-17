import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import {
  ReportsRepository,
  emptyQuery,
  parseRun,
} from "../src/features/reports/reportsRepository";
import { page } from "../src/core/api/decode";

it("encodes scope, forwards opaque cursor, and requests bounded server filtering", async () => {
  const transport = vi.fn<Transport>(
    async () => new Response(JSON.stringify({ items: [], nextCursor: null })),
  );
  const repository = new ReportsRepository(new ApiClient(transport));
  await repository.runs(
    "project/a",
    { ...emptyQuery, cursor: "opaque+/=", status: "failed", q: "checkout" },
    new AbortController().signal,
  );
  const url = new URL(String(transport.mock.calls[0]?.[0]), "http://localhost");
  expect(url.pathname).toBe("/v1/projects/project%2Fa/runs");
  expect(url.searchParams.get("cursor")).toBe("opaque+/=");
  expect(url.searchParams.get("limit")).toBe("20");
  expect(url.searchParams.get("status")).toBe("failed");
});
it("refuses malformed DTOs and unbounded pages", () => {
  expect(() => parseRun({ id: "bad" })).toThrow();
  expect(() =>
    page({ items: Array(21).fill({}), nextCursor: null }, () => ({}), 20),
  ).toThrow();
});

it("decodes attempt artifacts, tolerates their absence, bounds them, and owns the content path", async () => {
  const detail = (attempts: unknown[]) => ({
    result: {
      id: "r1",
      displayName: "declines expired card",
      filePath: "tests/pay.spec.ts",
      finalStatus: "failed",
      attemptCount: 1,
      durationMs: 5,
      stableTestId: null,
    },
    runId: "run-1",
    attempts,
    events: [],
  });
  const artifact = {
    id: "a1",
    name: "shot.png",
    kind: "screenshot",
    contentType: "image/png",
    sizeBytes: 1234,
    expiresAt: "2026-10-01T00:00:00Z",
  };
  const transport = vi.fn<Transport>(
    async () =>
      new Response(
        JSON.stringify(
          detail([
            {
              attempt: 0,
              status: "failed",
              durationMs: 5,
              error: null,
              artifacts: [artifact],
            },
            { attempt: 1, status: "passed", durationMs: 5, error: null },
          ]),
        ),
      ),
  );
  const repository = new ReportsRepository(new ApiClient(transport));
  const out = await repository.result("p", "r1", new AbortController().signal);
  expect(out.attempts[0]?.artifacts).toEqual([
    {
      id: "a1",
      name: "shot.png",
      kind: "screenshot",
      contentType: "image/png",
      sizeBytes: 1234,
    },
  ]);
  expect(out.attempts[1]?.artifacts).toEqual([]);
  expect(repository.artifactContentUrl("p/x", "a/1")).toBe(
    "/v1/projects/p%2Fx/artifacts/a%2F1/content",
  );

  const tooMany = vi.fn<Transport>(
    async () =>
      new Response(
        JSON.stringify(
          detail([
            {
              attempt: 0,
              status: "failed",
              durationMs: 5,
              error: null,
              artifacts: Array(21).fill(artifact),
            },
          ]),
        ),
      ),
  );
  await expect(
    new ReportsRepository(new ApiClient(tooMany)).result(
      "p",
      "r1",
      new AbortController().signal,
    ),
  ).rejects.toThrow();
});
