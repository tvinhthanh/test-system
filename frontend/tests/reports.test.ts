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
