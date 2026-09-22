import { it, expect, vi } from "vitest";
import { ApiClient, ApiError, type Transport } from "../src/core/api/client";
import { TestmgmtRepository } from "../src/features/testmgmt/testmgmtRepository";

const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(status === 204 ? null : JSON.stringify(body), { status });
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new TestmgmtRepository(client);
};
const at = "2026-09-21T09:00:00Z";
const testCase = {
  id: "c1",
  suiteId: "s1",
  suiteName: "Checkout",
  externalKey: "TC-1",
  title: "Pay with saved card",
  steps: [{ action: "open cart", expected: "cart shown" }],
  expected: "Order placed",
  priority: "p0",
  isCritical: true,
  automationStatus: "automated",
  ownerId: "",
  createdAt: at,
  updatedAt: at,
};
const release = {
  id: "r1",
  version: "2026.09.21",
  branch: "",
  status: "open",
  decidedBy: "",
  decidedAt: null,
  decisionNote: "",
  gate: {
    verdict: "blocked",
    evaluatedAt: at,
    runId: "run-1",
    branch: "main",
    p0Coverage: 0.5,
    rules: [
      {
        id: "critical_regression",
        level: "blocked",
        passed: true,
        applies: true,
        summary: "ok",
      },
      {
        id: "untriaged_failures",
        level: "blocked",
        passed: false,
        applies: true,
        summary: "2 open",
        details: { untriaged: 2 },
      },
    ],
  },
  evaluatedAt: at,
  createdAt: at,
  updatedAt: at,
};

it("decodes test cases (lists without links, single with links) and refuses bad enums", async () => {
  const list = vi.fn<Transport>(async () => json({ items: [testCase] }));
  const [c] = await repo(list).testCases(
    "p",
    { q: "saved", critical: "true" },
    signal(),
  );
  expect(c?.links).toEqual([]);
  expect(c?.priority).toBe("p0");
  const url = new URL(String(list.mock.calls[0]?.[0]), "http://localhost");
  expect(url.pathname).toBe("/v1/projects/p/test-cases");
  expect(url.searchParams.get("q")).toBe("saved");
  expect(url.searchParams.get("critical")).toBe("true");
  expect(url.searchParams.has("automation")).toBe(false);

  const one = vi.fn<Transport>(async () =>
    json({
      ...testCase,
      links: [
        {
          id: "l1",
          testCaseId: "c1",
          stableTestId: "i1",
          linkType: "exact",
          createdBy: "alice",
          createdAt: at,
        },
      ],
      requirements: ["req-1"],
    }),
  );
  const full = await repo(one).testCase("p", "c1", signal());
  expect(full.links).toHaveLength(1);
  expect(full.requirements).toEqual(["req-1"]);

  const badPriority = vi.fn<Transport>(async () =>
    json({ items: [{ ...testCase, priority: "urgent" }] }),
  );
  await expect(repo(badPriority).testCases("p", {}, signal())).rejects.toThrow(
    ApiError,
  );
  const badStatus = vi.fn<Transport>(async () =>
    json({ items: [{ ...testCase, automationStatus: "robot" }] }),
  );
  await expect(repo(badStatus).testCases("p", {}, signal())).rejects.toThrow(
    ApiError,
  );
});

it("uses PATCH and DELETE with the CSRF header and maps 403/409/404", async () => {
  const calls: { path: string; init: RequestInit }[] = [];
  const transport = vi.fn<Transport>(async (path, init) => {
    calls.push({ path: String(path), init: init ?? {} });
    if (String(init?.method) === "DELETE") return json(null, 204);
    return json({ ...testCase, title: "Renamed" });
  });
  const r = repo(transport);
  const updated = await r.updateTestCase(
    "p",
    "c1",
    { title: "Renamed" },
    signal(),
  );
  expect(updated.title).toBe("Renamed");
  await r.unlink("p", "c1", "l1", signal());
  await r.uncover("p", "c1", "req-1", signal());
  expect(calls.map((c) => [c.init.method, c.path])).toEqual([
    ["PATCH", "/v1/projects/p/test-cases/c1"],
    ["DELETE", "/v1/projects/p/test-cases/c1/links/l1"],
    ["DELETE", "/v1/projects/p/test-cases/c1/requirements/req-1"],
  ]);
  for (const c of calls) {
    const h = c.init.headers as Record<string, string>;
    expect(h["X-CSRF-Token"]).toBe("csrf-1");
  }
  // A DELETE carries no body and no content type.
  expect(calls[1]?.init.body).toBeUndefined();
  expect(
    (calls[1]?.init.headers as Record<string, string>)["Content-Type"],
  ).toBeUndefined();

  const forbidden = vi.fn<Transport>(async () =>
    json(
      {
        error: { code: "FORBIDDEN", message: "your role does not allow this" },
      },
      403,
    ),
  );
  await expect(
    repo(forbidden).decide("p", "r1", { status: "approved" }, signal()),
  ).rejects.toMatchObject({ status: 403, code: "FORBIDDEN" });
  const conflict = vi.fn<Transport>(async () =>
    json({ error: { code: "CONFLICT", message: "identity is merged" } }, 409),
  );
  await expect(
    repo(conflict).link("p", "c1", "i-merged", signal()),
  ).rejects.toMatchObject({ status: 409, code: "CONFLICT" });
  const gone = vi.fn<Transport>(async () =>
    json({ error: { code: "NOT_FOUND", message: "not found" } }, 404),
  );
  await expect(repo(gone).release("p", "nope", signal())).rejects.toMatchObject(
    { status: 404 },
  );
});

it("decodes a release with its gate, tolerates null gate, and builds the CI URL", async () => {
  const withGate = vi.fn<Transport>(async () => json(release));
  const r = await repo(withGate).release("p", "r1", signal());
  expect(r.gate?.verdict).toBe("blocked");
  expect(r.gate?.rules).toHaveLength(2);
  expect(r.gate?.rules[0]?.details).toEqual({});
  expect(r.gate?.rules[1]?.details).toEqual({ untriaged: 2 });
  expect(r.gate?.p0Coverage).toBe(0.5);
  expect(r.decidedAt).toBeNull();

  const fresh = vi.fn<Transport>(async () =>
    json({ items: [{ ...release, gate: null, evaluatedAt: null }] }),
  );
  const [open] = await repo(fresh).releases("p", signal());
  expect(open?.gate).toBeNull();

  const badVerdict = vi.fn<Transport>(async () =>
    json({ ...release, gate: { ...release.gate, verdict: "maybe" } }),
  );
  await expect(repo(badVerdict).release("p", "r1", signal())).rejects.toThrow(
    ApiError,
  );

  expect(repo(withGate).ciGateUrl("https://atp.example", "r 1")).toBe(
    "https://atp.example/v1/releases/r%201/gate",
  );
});

it("imports CSV as JSON {csv} and decodes the report", async () => {
  const transport = vi.fn<Transport>(async (_p, init) => {
    expect(JSON.parse(String(init?.body))).toEqual({ csv: "title\nA" });
    return json({
      created: 1,
      updated: 0,
      skipped: 0,
      suites: 0,
      requirements: 0,
      errors: [],
    });
  });
  const rep = await repo(transport).importCsv("p", "title\nA", signal());
  expect(rep.created).toBe(1);
  expect(rep.errors).toEqual([]);
});

it("attaches evidence as multipart with CSRF, no JSON content type, and decodes it on the plan", async () => {
  const transport = vi.fn<Transport>(async (path, init) => {
    if (String(path).endsWith("/evidence")) {
      // FormData goes as-is: the browser sets the multipart boundary, so the
      // client must not force application/json.
      expect(init?.method).toBe("POST");
      expect(init?.body).toBeInstanceOf(FormData);
      const file = (init!.body as FormData).get("file") as File;
      expect(file.name).toBe("checkout.png");
      const headers = init?.headers as Record<string, string>;
      expect(headers["X-CSRF-Token"]).toBe("csrf-1");
      expect(headers["Content-Type"]).toBeUndefined();
      return json(
        {
          id: "a1",
          name: "checkout.png",
          kind: "manual_evidence",
          contentType: "image/png",
          sizeBytes: 1234,
          expiresAt: at,
        },
        201,
      );
    }
    return json({
      id: "plan-1",
      name: "Sprint 12",
      environmentId: "",
      status: "active",
      executions: [
        {
          id: "e1",
          planId: "plan-1",
          testCaseId: "c1",
          caseTitle: "Pay with saved card",
          executedBy: "alice@example.com",
          status: "passed",
          note: "",
          executedAt: at,
          evidence: [
            {
              id: "a1",
              name: "checkout.png",
              kind: "manual_evidence",
              contentType: "image/png",
              sizeBytes: 1234,
              expiresAt: at,
            },
          ],
        },
      ],
      createdAt: at,
      updatedAt: at,
    });
  });
  const r = repo(transport);
  const file = new File([new Uint8Array([137, 80, 78, 71])], "checkout.png", {
    type: "image/png",
  });
  const stored = await r.attachEvidence("p", "plan-1", "e1", file, signal());
  expect(stored).toEqual({
    id: "a1",
    name: "checkout.png",
    kind: "manual_evidence",
    contentType: "image/png",
    sizeBytes: 1234,
  });
  expect(transport.mock.calls[0]![0]).toBe(
    "/v1/projects/p/test-plans/plan-1/executions/e1/evidence",
  );
  const plan = await r.plan("p", "plan-1", signal());
  expect(plan.executions[0]!.evidence).toEqual([stored]);
  expect(r.evidenceContentUrl("p", "a1")).toBe(
    "/v1/projects/p/artifacts/a1/content",
  );
  // An execution without the field (older server) decodes to no evidence.
  const bare = repo(async () =>
    json({
      id: "e2",
      planId: "plan-1",
      testCaseId: "c1",
      caseTitle: "x",
      executedBy: "a",
      status: "failed",
      note: "",
      executedAt: at,
    }),
  );
  const exec = await bare.recordExecution(
    "p",
    "plan-1",
    { testCaseId: "c1", status: "failed" },
    signal(),
  );
  expect(exec.evidence).toEqual([]);
});
