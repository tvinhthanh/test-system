import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { MeterRepository } from "../src/features/billing/meterRepository";

const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const day = {
  day: "2026-09-27",
  activeTests: 1180,
  rawFingerprints: 1204,
  corrections: 38,
  runs: 14,
  results: 1850,
  artifactBytes: 12884901888,
  drift: 0.0322,
  flagged: true,
};
const answer = {
  days: [day],
  ready: false,
  reasons: ["1 of 28 days recorded"],
  requiredDays: 28,
  driftLimit: 0.02,
};

it("reads the org meter with its verdict", async () => {
  const transport = vi.fn<Transport>(async () => json(answer));
  const out = await new MeterRepository(new ApiClient(transport)).meter(
    "org 1",
    35,
    signal(),
  );
  expect(transport.mock.calls[0]![0]).toBe(
    "/v1/organizations/org%201/meter?days=35",
  );
  expect(out.ready).toBe(false);
  expect(out.reasons).toEqual(["1 of 28 days recorded"]);
  expect(out.days[0]).toEqual(day);
});

it("refuses a malformed day", async () => {
  for (const bad of [
    { ...answer, days: [{ ...day, drift: -1 }] },
    { ...answer, days: [{ ...day, flagged: "yes" }] },
    { ...answer, ready: "no" },
  ]) {
    const transport = vi.fn<Transport>(async () => json(bad));
    await expect(
      new MeterRepository(new ApiClient(transport)).meter("o", 28, signal()),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  }
});
