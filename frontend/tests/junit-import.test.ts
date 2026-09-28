import { it, expect, vi } from "vitest";
import { ApiClient, ApiError, type Transport } from "../src/core/api/client";
import { ReportsRepository } from "../src/features/reports/reportsRepository";

const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new ReportsRepository(client);
};
const report = new Blob(
  ['<testsuite name="x" timestamp="2026-03-02T09:15:00"/>'],
  { type: "" }, // what a browser gives a .xml it does not know
);
const answer = {
  runId: "33333333-3333-4333-8333-333333333333",
  localRunId: "44444444-4444-8444-8444-444444444444",
  created: true,
  tests: 3,
  attempts: 5,
  duplicates: 0,
  status: "failed",
  flakyCount: 1,
  startedAt: "2026-03-02T09:15:00Z",
  finishedAt: "2026-03-02T09:15:04Z",
};

it("sends the report as its own XML bytes with the options in the query", async () => {
  const transport = vi.fn<Transport>(async () => json(answer, 201));
  const out = await repo(transport).importJUnit(
    "p 1",
    report,
    {
      environment: "nightly",
      framework: "junit",
      branch: "main",
      startedAt: undefined,
    },
    signal(),
  );
  expect(out).toEqual({
    runId: answer.runId,
    created: true,
    tests: 3,
    attempts: 5,
    status: "failed",
    flakyCount: 1,
    startedAt: answer.startedAt,
  });
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe(
    "/v1/projects/p%201/imports/junit?environment=nightly&framework=junit&branch=main",
  );
  expect(init?.method).toBe("POST");
  expect(init?.body).toBe(report); // the Blob itself, never JSON.stringify'd
  const headers = init?.headers as Record<string, string>;
  expect(headers["Content-Type"]).toBe("application/xml");
  expect(headers["X-CSRF-Token"]).toBe("csrf-1");
});

it("keeps the server's message on the error for the import form to show", async () => {
  const transport = vi.fn<Transport>(async () =>
    json(
      {
        error: {
          code: "INVALID_JUNIT",
          message: 'junit: no timestamp on testsuite "x" — pass startedAt',
        },
      },
      400,
    ),
  );
  const err = await repo(transport)
    .importJUnit("p", report, {}, signal())
    .catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).code).toBe("INVALID_JUNIT");
  expect((err as ApiError).detail).toContain("pass startedAt");
  // The generic message is unchanged for every other screen.
  expect((err as ApiError).message).not.toContain("startedAt");
});

it("refuses an answer that is not an import result", async () => {
  const transport = vi.fn<Transport>(async () =>
    json({ ...answer, created: "yes" }, 201),
  );
  await expect(
    repo(transport).importJUnit("p", report, {}, signal()),
  ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
});
