import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { TestmgmtRepository } from "../src/features/testmgmt/testmgmtRepository";
import { hours, minutesLabel } from "../src/features/testmgmt/useRoi";

const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new TestmgmtRepository(client);
};
const baseline = {
  id: "55555555-5555-4555-8555-555555555555",
  kind: "prospective",
  failures: 14,
  medianMinutes: 180,
  p90Minutes: null,
  rerunPassRate: 0.3,
  flakyInvestigationMinutes: null,
  periodFrom: "2026-08-01",
  periodTo: "2026-08-14",
  note: "",
  enteredBy: "em@acme.example",
  enteredAt: "2026-08-15T09:00:00Z",
};
const roi = {
  month: "2026-09",
  from: "2026-09-01T00:00:00Z",
  to: "2026-10-01T00:00:00Z",
  failures: 5,
  triaged: 5,
  triageCoverage: 1,
  untriagedRate: 0,
  ttrcMedianMinutes: 240,
  baseline,
  savedHours: -5, // slower than before: shown as it is
  flakyHours: 0.5,
  flakyRetryHours: 0.5,
  markedFlaky: 0,
  investigationCounted: false,
  fewTriaged: true,
  minTriaged: 10,
};

it("reads a month's ROI, negative savings included, and asks for the month", async () => {
  const transport = vi.fn<Transport>(async () => json(roi));
  const out = await repo(transport).roi("p", "2026-09", signal());
  expect(out.savedHours).toBe(-5);
  expect(out.baseline?.kind).toBe("prospective");
  expect(out.baseline?.p90Minutes).toBeNull();
  expect(transport.mock.calls[0]![0]).toBe("/v1/projects/p/roi?month=2026-09");
});

it("keeps 'not enough data' as null, never as zero", async () => {
  const transport = vi.fn<Transport>(async () =>
    json({ ...roi, baseline: null, savedHours: null }),
  );
  const out = await repo(transport).roi("p", "2026-09", signal());
  expect(out.baseline).toBeNull();
  expect(out.savedHours).toBeNull();
});

it("refuses an unknown baseline kind or a non-number", async () => {
  for (const bad of [
    { ...roi, baseline: { ...baseline, kind: "guess" } },
    { ...roi, flakyHours: "3" },
  ]) {
    const transport = vi.fn<Transport>(async () => json(bad));
    await expect(
      repo(transport).roi("p", "2026-09", signal()),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  }
});

it("records a baseline as JSON with CSRF", async () => {
  const transport = vi.fn<Transport>(async () => json(baseline, 201));
  const out = await repo(transport).addBaseline(
    "p",
    {
      kind: "prospective",
      failures: 14,
      medianMinutes: 180,
      periodFrom: "2026-08-01",
      periodTo: "2026-08-14",
    },
    signal(),
  );
  expect(out.enteredBy).toBe("em@acme.example");
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe("/v1/projects/p/roi/baselines");
  expect(init?.method).toBe("POST");
  expect(JSON.parse(String(init?.body))).toMatchObject({
    kind: "prospective",
    medianMinutes: 180,
  });
});

it("labels hours and minutes for people", () => {
  expect(hours(67.5)).toBe("68 h");
  expect(hours(3.5)).toBe("3.5 h");
  expect(hours(-5)).toBe("−5.0 h");
  expect(minutesLabel(null)).toBe("—");
  expect(minutesLabel(45)).toBe("45 min");
  expect(minutesLabel(180)).toBe("3.0 h");
  expect(minutesLabel(60 * 72)).toBe("3.0 days");
});

it("turns hours into money only from a typed rate, number or string", async () => {
  const { useHourlyRate } = await import("../src/features/testmgmt/useRoi");
  const { rate, value, money } = useHourlyRate();
  expect(money(10)).toBe("");
  rate.value = 50; // what a type=number v-model hands back
  expect(value.value).toBe(50);
  expect(money(10)).toContain("500");
  rate.value = "";
  expect(money(10)).toBe("");
  rate.value = "-3";
  expect(value.value).toBeNull();
});
