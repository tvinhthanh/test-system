import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import {
  ReportsRepository,
  parseTriageMetrics,
} from "../src/features/reports/reportsRepository";
import { failureCategories } from "../src/features/reports/categories";
import {
  columns,
  humanDuration,
  percent,
} from "../src/features/reports/useOverview";

it("decodes triage metrics, requiring ratios in [0,1] and nullable medians", async () => {
  const body = {
    failures: 40,
    triaged: 25,
    seen: 30,
    untriagedRate: 0.375,
    triageCoverage: 0.75,
    ttrcMedianSeconds: 5400,
    timeToFirstViewSeconds: null,
  };
  expect(parseTriageMetrics(body)).toEqual(body);
  expect(() => parseTriageMetrics({ ...body, untriagedRate: 1.2 })).toThrow();
  expect(() =>
    parseTriageMetrics({ ...body, ttrcMedianSeconds: -1 }),
  ).toThrow();
  const transport = vi.fn<Transport>(
    async () => new Response(JSON.stringify(body)),
  );
  const repository = new ReportsRepository(new ApiClient(transport));
  await repository.triageMetrics("p", 90, new AbortController().signal);
  const url = new URL(String(transport.mock.calls[0]?.[0]), "http://localhost");
  expect(url.pathname).toBe("/v1/projects/p/metrics/triage");
  expect(url.searchParams.get("days")).toBe("90");
});

it("decodes the category trend and refuses categories outside the §8.7 enum", async () => {
  const ok = vi.fn<Transport>(
    async () =>
      new Response(
        JSON.stringify({
          items: [
            { week: "2026-09-07T00:00:00Z", category: "app_bug", count: 3 },
            { week: "2026-09-07T00:00:00Z", category: "unknown", count: 2 },
          ],
        }),
      ),
  );
  const items = await new ReportsRepository(new ApiClient(ok)).categoryTrend(
    "p",
    30,
    new AbortController().signal,
  );
  expect(items).toHaveLength(2);
  const bad = vi.fn<Transport>(
    async () =>
      new Response(
        JSON.stringify({
          items: [
            { week: "2026-09-07T00:00:00Z", category: "gremlins", count: 1 },
          ],
        }),
      ),
  );
  await expect(
    new ReportsRepository(new ApiClient(bad)).categoryTrend(
      "p",
      30,
      new AbortController().signal,
    ),
  ).rejects.toThrow();
});

it("shapes buckets into rectangular weekly columns in time order", () => {
  const cols = columns(
    [
      { week: "2026-09-14T00:00:00Z", category: "test_bug", count: 1 },
      { week: "2026-09-07T00:00:00Z", category: "app_bug", count: 3 },
      { week: "2026-09-07T00:00:00Z", category: "unknown", count: 2 },
      { week: "2026-09-07T00:00:00Z", category: "app_bug", count: 1 },
    ],
    failureCategories,
  );
  expect(cols.map((c) => c.week)).toEqual([
    "2026-09-07T00:00:00Z",
    "2026-09-14T00:00:00Z",
  ]);
  expect(cols[0]?.total).toBe(6);
  expect(cols[0]?.counts).toEqual({
    app_bug: 4,
    test_bug: 0,
    flaky_timing: 0,
    env_infra: 0,
    env_data: 0,
    dependency_external: 0,
    unknown: 2,
  });
  expect(Object.keys(cols[1]?.counts ?? {})).toHaveLength(7);
});

it("formats durations and ratios the way the tiles show them", () => {
  expect(humanDuration(42)).toBe("42s");
  expect(humanDuration(12 * 60)).toBe("12m");
  expect(humanDuration(3 * 3600 + 5 * 60)).toBe("3h 05m");
  expect(humanDuration(2 * 86400 + 4 * 3600)).toBe("2d 4h");
  expect(percent(0.375)).toBe("38%");
  expect(percent(0)).toBe("0%");
});
