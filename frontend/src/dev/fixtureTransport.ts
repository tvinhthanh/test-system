import type { Transport } from "../core/api/client";

const marker = "ATP_FIXTURE_ONLY";
const runIds = Array.from(
  { length: 43 },
  (_, i) => `7e14a${String(1048 - i)}-1d5c-48a6-8ae7-101001001001`,
);
const sessionKey = "atp-fixture-session";
const user = {
  id: "preview-user",
  name: "Alex Morgan",
  email: "alex@example.test",
  mfa: { enabled: false, required: false },
};
function run(i: number) {
  const status = i === 0 ? "running" : i % 4 === 1 ? "failed" : "passed";
  const flaky = i % 3 === 0 ? 2 : 0;
  return {
    id: runIds[i],
    branch:
      i % 3 === 0
        ? "feat/checkout-validation"
        : i % 5 === 0
          ? "fix/cart-persistence"
          : "main",
    commitSha: ["a72f91c298bc", "f803b2d509ae", "9be420a41c02"][i % 3],
    status,
    environmentName: i % 3 === 0 ? "Preview" : "Staging",
    startedAt: new Date(
      Date.UTC(2026, 8, 14, 9, 42) - i * 37 * 60000,
    ).toISOString(),
    durationMs: i === 0 ? null : 92000 + i * 1200,
    totalCount: 128,
    passedCount: i === 0 ? 83 : status === "failed" ? 123 - flaky : 128 - flaky,
    failedCount: status === "failed" ? 5 : 0,
    flakyCount: flaky,
  };
}

let fixtureMembers = [
  { email: "alex@example.test", level: "workspace", role: "workspace_admin" },
  { email: "jordan@example.test", level: "project", role: "maintainer" },
  { email: "sam@example.test", level: "project", role: "member" },
  { email: "taylor@example.test", level: "project", role: "viewer" },
].map((m, i) => ({
  ...m,
  userId: i === 0 ? user.id : `preview-user-${i}`,
  createdAt: new Date(Date.UTC(2026, 7, 20 + i * 5)).toISOString(),
}));

export function createFixtureTransport(): Transport {
  return async (input, init) => {
    await new Promise<void>((resolve, reject) => {
      const signal = init?.signal;
      const abort = () => {
        clearTimeout(timer);
        reject(new DOMException("Aborted", "AbortError"));
      };
      const timer = setTimeout(() => {
        signal?.removeEventListener("abort", abort);
        resolve();
      }, 180);
      if (signal?.aborted) abort();
      else signal?.addEventListener("abort", abort, { once: true });
    });
    const url = new URL(String(input), location.origin);
    const path = url.pathname;
    const signedIn = sessionStorage.getItem(sessionKey) !== "out";
    function response(value: unknown, status = 200) {
      return new Response(JSON.stringify(value), {
        status,
        headers: { "Content-Type": "application/json", "X-Request-Id": marker },
      });
    }
    if (path === "/v1/session") {
      if (init?.method === "POST") sessionStorage.setItem(sessionKey, "in");
      return response({
        csrfToken: marker,
        user: signedIn || init?.method === "POST" ? user : null,
      });
    }
    if (!signedIn)
      return response(
        {
          error: {
            code: "UNAUTHENTICATED",
            message: "Unauthenticated",
            details: {},
          },
        },
        401,
      );
    if (path === "/v1/session/logout") {
      sessionStorage.setItem(sessionKey, "out");
      return response({});
    }
    // §11.2 MFA, off until "turned on" with any six digits.
    if (path.startsWith("/v1/session/mfa")) {
      const mfa = user.mfa;
      if (path === "/v1/session/mfa")
        return response({
          ...mfa,
          recoveryCodesLeft: mfa.enabled ? 10 : 0,
        });
      if (path.endsWith("/setup"))
        return response({
          secret: "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
          otpauthUri:
            "otpauth://totp/ATP:alex%40example.test?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=ATP",
        });
      const { code } = JSON.parse(String(init?.body ?? "{}")) as {
        code?: string;
      };
      if (!/^\d{6}$/.test(code ?? ""))
        return response(
          { error: { code: "INVALID_CODE", message: "x", details: {} } },
          400,
        );
      mfa.enabled = path.endsWith("/enable");
      if (!mfa.enabled) return new Response(null, { status: 204 });
      return response({
        recoveryCodes: Array.from(
          { length: 10 },
          (_, i) => `k${i}f3q-7m${i}x2`,
        ),
      });
    }
    if (path === "/v1/projects")
      return response({
        items: [
          {
            id: "commerce",
            name: "Commerce Web",
            workspaceName: "Acme Engineering",
            actions: [
              "view",
              "triage",
              "identity.merge",
              "member.manage",
              "testmgmt.write",
              "release.decide",
            ],
          },
          {
            id: "platform",
            name: "Platform API",
            workspaceName: "Acme Engineering",
            actions: ["view"],
          },
        ],
        nextCursor: null,
      });
    const match = path.match(
      /^\/v1\/projects\/(commerce|platform)\/runs(?:\/([^/]+))?(\/results)?$/,
    );
    const project = match?.[1];
    const id = match?.[2];
    const index = id ? runIds.indexOf(id) : -1;
    if (match) {
      const results = match[3];
      if (id && (index < 0 || project === "platform"))
        return response({ error: { code: "NOT_FOUND" } }, 404);
      if (results)
        return response({
          items: Array.from({ length: 12 }, (_, i) => ({
            id: `result-${i}`,
            stableTestId: `identity-${i % 8}`,
            displayName: [
              "Checkout completes with a saved card",
              "Guest can apply a discount code",
              "Order confirmation shows delivery date",
              "User signs in with valid credentials",
            ][i % 4],
            filePath: `tests/${i < 4 ? "checkout" : "account"}/flow.spec.ts`,
            finalStatus:
              index % 4 === 1 && i === 0
                ? "failed"
                : i === 1 && index % 3 === 0
                  ? "flaky_pass"
                  : "passed",
            attemptCount: i < 2 ? 2 : 1,
            durationMs: 1200 + i * 230,
          })),
          nextCursor: null,
        });
    }
    const resultMatch = path.match(
      /^\/v1\/projects\/(commerce|platform)\/results\/(result-\d+)$/,
    );
    if (resultMatch) {
      const index = Number(
        (resultMatch[2] ?? "result-0").replace("result-", ""),
      );
      const failed = index === 0;
      const stack = failed
        ? 'Error: expect(locator).toBeVisible()\n\nLocator: getByRole("heading", { name: "Order confirmed" })\nExpected: visible\nReceived: element not found\nTimeout: 5000ms'
        : null;
      return response({
        result: {
          id: `result-${index}`,
          stableTestId: `identity-${index % 8}`,
          displayName: [
            "Checkout completes with a saved card",
            "Guest can apply a discount code",
            "Order confirmation shows delivery date",
            "User signs in with valid credentials",
          ][index % 4],
          filePath: "tests/checkout/flow.spec.ts",
          finalStatus: failed
            ? "failed"
            : index === 1
              ? "flaky_pass"
              : "passed",
          attemptCount: failed ? 2 : 1,
          durationMs: 1200 + index * 230,
        },
        attempts: [
          {
            attempt: 0,
            status: failed ? "failed" : "passed",
            durationMs: 1200,
            error: failed
              ? {
                  message: stack,
                  stack:
                    "at tests/checkout/flow.spec.ts:42:31\n at processTicksAndRejections (node:internal/process/task_queues:95:5)",
                }
              : null,
          },
          ...(failed
            ? [
                {
                  attempt: 1,
                  status: "failed",
                  durationMs: 1400,
                  error: {
                    message: stack,
                    stack: "at tests/checkout/flow.spec.ts:42:31",
                  },
                },
              ]
            : []),
        ],
        events: failed
          ? [
              {
                id: "event-1",
                type: "FAILURE_SEEN",
                actorName: "Alex Morgan",
                occurredAt: "2026-09-14T09:44:00Z",
              },
            ]
          : [],
      });
    }
    if (path.endsWith("/triage-events")) return response({});
    // Test Management (§6.8): a small, coherent set — two suites, three
    // requirements, five cases (two critical, one linked), one plan with
    // executions, two releases (one blocked, one shipped by override).
    const tm = fixtureTestmgmt(url, init);
    if (tm) return response(tm.body, tm.status);
    // §11.3 members: the preview user is a workspace admin, so the list
    // shows one inherited row next to the project's own.
    const mm = path.match(/^\/v1\/projects\/commerce\/members(?:\/([^/]+))?$/);
    if (mm) {
      const body =
        typeof init?.body === "string"
          ? (JSON.parse(init.body) as { email?: string; role?: string })
          : {};
      if (init?.method === "DELETE") {
        fixtureMembers = fixtureMembers.filter(
          (m) => !(m.userId === mm[1] && m.level === "project"),
        );
        return new Response(null, { status: 204 });
      }
      if (init?.method === "POST") {
        const email = String(body.email ?? "")
          .trim()
          .toLowerCase();
        const known = fixtureMembers.find((m) => m.email === email);
        if (!known)
          return response(
            {
              error: {
                code: "USER_NOT_FOUND",
                message: "no verified account uses that email",
                details: { field: "email" },
              },
            },
            404,
          );
        const row = {
          ...known,
          role: String(body.role),
          level: "project",
          createdAt: iso(0),
        };
        fixtureMembers = [
          ...fixtureMembers.filter(
            (m) => !(m.userId === known.userId && m.level === "project"),
          ),
          row,
        ].sort((a, b) => a.email.localeCompare(b.email));
        return response(row);
      }
      return response({ items: fixtureMembers, nextCursor: null });
    }
    // §2.5 overview and §9.4 review/aliases, so every screen renders offline.
    if (path.endsWith("/metrics/triage")) {
      const long = url.searchParams.get("days") === "90";
      return response({
        failures: long ? 412 : 137,
        triaged: long ? 301 : 96,
        seen: long ? 377 : 121,
        untriagedRate: long ? 0.269 : 0.299,
        triageCoverage: long ? 0.915 : 0.883,
        ttrcMedianSeconds: long ? 5460 : 3900,
        timeToFirstViewSeconds: long ? 1320 : 960,
      });
    }
    if (path.endsWith("/metrics/trend")) {
      const weeks = url.searchParams.get("days") === "90" ? 13 : 5;
      const mix: Record<string, number[]> = {
        app_bug: [6, 4, 9, 5, 7, 3, 8, 6, 4, 5, 7, 6, 5],
        test_bug: [3, 5, 2, 4, 3, 6, 2, 3, 5, 2, 3, 4, 3],
        flaky_timing: [4, 3, 5, 6, 4, 2, 5, 4, 3, 6, 4, 3, 5],
        env_infra: [1, 0, 3, 1, 2, 0, 1, 2, 0, 1, 3, 1, 0],
        env_data: [0, 2, 1, 0, 1, 1, 0, 2, 1, 0, 1, 0, 1],
        dependency_external: [1, 1, 0, 2, 0, 1, 1, 0, 2, 1, 0, 1, 1],
        unknown: [5, 6, 4, 7, 5, 8, 6, 5, 7, 4, 6, 5, 7],
      };
      const items: { week: string; category: string; count: number }[] = [];
      for (let w = 0; w < weeks; w++) {
        const week = new Date(
          Date.UTC(2026, 8, 14) - (weeks - 1 - w) * 7 * 86400000,
        ).toISOString();
        for (const [category, counts] of Object.entries(mix))
          if (counts[w]) items.push({ week, category, count: counts[w]! });
      }
      return response({ items });
    }
    if (path.endsWith("/review"))
      return response({
        items: [0, 1].map((i) => ({
          id: `review-${i}`,
          createdAt: new Date(Date.UTC(2026, 8, 14, 8 - i, 12)).toISOString(),
          identity: {
            id: `identity-${10 + i}`,
            canonicalTitle: i
              ? "Search returns matching products (paginated)"
              : "Checkout completes with a stored card",
            canonicalFilePath: "tests/checkout/flow.spec.ts",
            status: "active",
          },
          candidate: {
            id: `identity-${i === 0 ? 0 : 5}`,
            canonicalTitle: i
              ? "Search returns matching products"
              : "Checkout completes with a saved card",
            canonicalFilePath: "tests/checkout/flow.spec.ts",
            status: "active",
          },
          fingerprint: `f${i}9c2a1e4b7d`,
          score: i ? 0.71 : 0.83,
          runnerUpId: i ? "" : "identity-3",
          runnerUpScore: i ? 0 : 0.79,
          gap: i ? 0.71 : 0.04,
          breakdown: { title: i ? 0.66 : 0.9, structure: 0.8, recency: 0.7 },
          runId: runIds[1 + i],
        })),
      });
    if (path.endsWith("/aliases"))
      return response({
        items: [
          {
            id: "alias-1",
            fingerprint: "a72f91c298bc0f31d4e5",
            source: "exact",
            matchScore: 1,
            createdBy: "",
            validFrom: new Date(Date.UTC(2026, 6, 2, 9)).toISOString(),
            validTo: null,
          },
          {
            id: "alias-0",
            fingerprint: "5d1c0be4a97f2e6b8c03",
            source: "auto_match",
            matchScore: 0.91,
            createdBy: "resolver",
            validFrom: new Date(Date.UTC(2026, 4, 11, 9)).toISOString(),
            validTo: new Date(Date.UTC(2026, 6, 2, 9)).toISOString(),
          },
        ],
      });
    const identityMatch = path.match(
      /^\/v1\/projects\/(commerce|platform)\/identities(?:\/([^/]+))?/,
    );
    if (identityMatch) {
      const identityId = identityMatch[2];
      const identity = (index: number) => ({
        id: `identity-${index}`,
        canonicalTitle: [
          "Checkout completes with a saved card",
          "Guest can apply a discount code",
          "Order confirmation shows delivery date",
          "User signs in with valid credentials",
          "Cart preserves quantities after refresh",
          "Search returns matching products",
          "Payment rejects an expired card",
          "Account settings persist across sessions",
        ][index],
        canonicalFilePath: "tests/checkout/flow.spec.ts",
        verdict:
          index === 0
            ? "flaky"
            : index === 1
              ? "suspect"
              : index === 7
                ? "insufficient_data"
                : "stable",
        flakyScore:
          index === 0 ? 0.72 : index === 1 ? 0.38 : index === 7 ? null : 0.04,
        totalRuns: index === 7 ? 1 : 30,
        recentStatuses: Array.from({ length: index === 7 ? 1 : 18 }, (_, n) =>
          index < 2 && n % 4 === 0
            ? "flaky_pass"
            : n === 16 && index === 0
              ? "failed"
              : "passed",
        ),
        status: "active",
        mergedIntoId: null,
      });
      if (identityId && path.endsWith("/runs"))
        return response({
          items: [0, 3, 6, 9, 12].map((index) => run(index)),
          nextCursor: null,
        });
      if (identityId)
        return response(
          identity(Number(identityId.replace("identity-", "")) || 0),
        );
      const flaky = url.searchParams.get("view") === "flaky";
      return response({
        items: Array.from({ length: flaky ? 2 : 8 }, (_, index) =>
          identity(index),
        ),
        nextCursor: null,
      });
    }
    if (!match) return response({ error: { code: "NOT_FOUND" } }, 404);
    if (id) return response(run(index));
    const status = url.searchParams.get("status"),
      q = url.searchParams.get("q")?.toLowerCase() ?? "",
      env = url.searchParams.get("environment")?.toLowerCase() ?? "";
    const items =
      project === "platform"
        ? []
        : runIds
            .map((_, i) => run(i))
            .filter(
              (r) =>
                (!status || r.status === status) &&
                `${r.id} ${r.branch} ${r.commitSha}`
                  .toLowerCase()
                  .includes(q) &&
                r.environmentName.toLowerCase().includes(env),
            );
    const offset = Number(
      url.searchParams.get("cursor")?.replace("fixture-page-", "") ?? 0,
    );
    return response({
      items: items.slice(offset, offset + 20),
      nextCursor:
        offset + 20 < items.length ? `fixture-page-${offset + 20}` : null,
    });
  };
}

// ---- Test Management fixture ------------------------------------------------

const tmNow = Date.UTC(2026, 8, 20, 9, 0);
const iso = (minusMinutes: number) =>
  new Date(tmNow - minusMinutes * 60000).toISOString();
const tmRequirements = [
  {
    id: "req-1",
    externalKey: "PAY-1",
    title: "Card payments settle",
    priority: "p0",
    status: "active",
    coveredBy: 2,
    createdAt: iso(4000),
    updatedAt: iso(4000),
  },
  {
    id: "req-2",
    externalKey: "PAY-7",
    title: "Refunds reach the customer",
    priority: "p0",
    status: "active",
    coveredBy: 0,
    createdAt: iso(3800),
    updatedAt: iso(3800),
  },
  {
    id: "req-3",
    externalKey: "",
    title: "Search results paginate",
    priority: "p2",
    status: "draft",
    coveredBy: 1,
    createdAt: iso(2000),
    updatedAt: iso(2000),
  },
];
const tmCase = (i: number) => ({
  id: `case-${i}`,
  suiteId: i < 3 ? "suite-1" : "suite-2",
  suiteName: i < 3 ? "Checkout" : "Search",
  externalKey: i === 4 ? "" : `TC-${i + 1}`,
  title:
    [
      "Pay with saved card",
      "Apply discount code",
      "Refund an order",
      "Search returns matching products",
      "Empty search shows suggestions",
    ][i] ?? "Case",
  steps:
    i === 4
      ? []
      : [
          {
            action: "Open the cart with one item",
            expected: "Cart shows the item and total",
          },
          {
            action: i === 2 ? "Request a refund" : "Proceed to payment",
            expected: i === 2 ? "Refund is queued" : "Payment form appears",
          },
        ],
  expected: i === 2 ? "Money returns within 5 days" : "Order confirmed",
  priority: i < 2 ? "p0" : i === 2 ? "p1" : "p2",
  isCritical: i < 2,
  automationStatus:
    i === 0 ? "automated" : i === 3 ? "automation_candidate" : "manual_only",
  ownerId: "",
  links:
    i === 0
      ? [
          {
            id: "link-1",
            testCaseId: "case-0",
            stableTestId: "identity-0",
            linkType: "exact",
            createdBy: "alex@example.test",
            createdAt: iso(3000),
          },
        ]
      : [],
  requirements:
    i === 0 ? ["req-1"] : i === 1 ? ["req-1"] : i === 3 ? ["req-3"] : [],
  createdAt: iso(3500 - i * 100),
  updatedAt: iso(500 - i * 20),
});
const tmGateBlocked = {
  verdict: "blocked",
  evaluatedAt: iso(30),
  runId: runIds[1],
  branch: "main",
  p0Coverage: 0.5,
  rules: [
    {
      id: "critical_regression",
      level: "blocked",
      passed: true,
      applies: true,
      summary: "1 of 1 linked critical identities ran, none failed",
      details: {
        criticalCases: 2,
        linkedIdentities: 1,
        seenInRun: 1,
        regressions: [],
      },
    },
    {
      id: "untriaged_failures",
      level: "blocked",
      passed: false,
      applies: true,
      summary: "2 of 5 failure(s) have no triage decision",
      details: { failures: 5, untriaged: 2 },
    },
    {
      id: "flaky_rate",
      level: "warn",
      passed: false,
      applies: true,
      summary: "flaky rate 3.9% is above 3.0%",
      details: { flakyPass: 5, total: 128, rate: 0.039, threshold: 0.03 },
    },
    {
      id: "p0_coverage",
      level: "warn",
      passed: true,
      applies: false,
      summary: "p0 coverage 50% — first evaluated release, nothing to compare",
      details: { coverage: 0.5 },
    },
    {
      id: "prod_smoke_approved",
      level: "blocked",
      passed: true,
      applies: false,
      summary:
        "production smoke approval is an Execution Plane rule (P3, QĐ-8)",
    },
  ],
};
const tmReleases = [
  {
    id: "rel-2",
    version: "2026.09.20",
    branch: "main",
    status: "open",
    decidedBy: "",
    decidedAt: null,
    decisionNote: "",
    gate: tmGateBlocked,
    evaluatedAt: iso(30),
    createdAt: iso(600),
    updatedAt: iso(30),
  },
  {
    id: "rel-1",
    version: "2026.09.13",
    branch: "",
    status: "shipped",
    decidedBy: "alex@example.test",
    decidedAt: iso(1900),
    decisionNote:
      "Hotfix for the checkout outage; the env failure is a known issue",
    gate: { ...tmGateBlocked, evaluatedAt: iso(2000), runId: runIds[3] },
    evaluatedAt: iso(2000),
    createdAt: iso(2600),
    updatedAt: iso(1900),
  },
];
const tmPlan = {
  id: "plan-1",
  name: "Release 2026.09.20 regression",
  environmentId: "",
  status: "active",
  executions: [
    {
      id: "exec-1",
      planId: "plan-1",
      testCaseId: "case-1",
      caseTitle: "Apply discount code",
      executedBy: "alex@example.test",
      status: "passed",
      note: "",
      executedAt: iso(120),
      evidence: [],
    },
    {
      id: "exec-2",
      planId: "plan-1",
      testCaseId: "case-2",
      caseTitle: "Refund an order",
      executedBy: "alex@example.test",
      status: "failed",
      note: "Refund button disabled for gift cards",
      executedAt: iso(90),
      evidence: [
        {
          id: "evidence-1",
          name: "refund-gift-card.txt",
          kind: "manual_evidence",
          contentType: "text/plain",
          sizeBytes: 812,
        },
      ],
    },
  ],
  createdAt: iso(700),
  updatedAt: iso(90),
};
function fixtureTestmgmt(
  url: URL,
  init?: RequestInit,
): { body: unknown; status?: number } | null {
  const path = url.pathname;
  const m = path.match(
    /^\/v1\/projects\/(commerce|platform)\/(suites|requirements|test-cases|test-plans|releases)(?:\/([^/]+))?(?:\/([^/]+))?(?:\/([^/]+))?(?:\/([^/]+))?$/,
  );
  if (!m) return null;
  const [, , kind, id, sub, subId, leaf] = m;
  const method = init?.method ?? "GET";
  const body =
    typeof init?.body === "string"
      ? (JSON.parse(init.body) as Record<string, unknown>)
      : {};
  const stamp = (extra: Record<string, unknown>) => ({
    createdAt: iso(0),
    updatedAt: iso(0),
    ...extra,
  });
  const missing = {
    body: { error: { code: "NOT_FOUND", message: "not found" } },
    status: 404,
  };
  switch (kind) {
    case "suites":
      return method === "POST"
        ? {
            body: stamp({
              id: "suite-new",
              parentId: "",
              name: body.name,
              cases: 0,
            }),
            status: 201,
          }
        : {
            body: {
              items: [
                {
                  id: "suite-1",
                  parentId: "",
                  name: "Checkout",
                  cases: 3,
                  createdAt: iso(5000),
                },
                {
                  id: "suite-2",
                  parentId: "",
                  name: "Search",
                  cases: 2,
                  createdAt: iso(4000),
                },
              ],
            },
          };
    case "requirements":
      if (method === "POST")
        return {
          body: stamp({
            id: "req-new",
            externalKey: body.externalKey ?? "",
            title: body.title,
            priority: body.priority ?? "p2",
            status: "active",
            coveredBy: 0,
          }),
          status: 201,
        };
      if (id && method === "PATCH") {
        const r = tmRequirements.find((x) => x.id === id);
        return r ? { body: { ...r, ...body, updatedAt: iso(0) } } : missing;
      }
      return { body: { items: tmRequirements } };
    case "test-cases": {
      if (id === "import")
        return {
          body: {
            created: 3,
            updated: 1,
            skipped: 0,
            suites: 1,
            requirements: 0,
            errors: ['row 5: priority "urgent" not in p0..p3'],
          },
        };
      if (!id) {
        if (method === "POST")
          return {
            body: stamp({
              ...tmCase(4),
              id: "case-new",
              externalKey: body.externalKey ?? "",
              title: body.title,
              priority: body.priority ?? "p2",
              isCritical: !!body.isCritical,
              steps: body.steps ?? [],
              automationStatus: "manual_only",
              links: [],
              requirements: [],
            }),
            status: 201,
          };
        const q = (url.searchParams.get("q") ?? "").toLowerCase();
        const crit = url.searchParams.get("critical");
        const auto = url.searchParams.get("automation");
        return {
          body: {
            items: [0, 1, 2, 3, 4]
              .map(tmCase)
              .filter(
                (c) =>
                  (!q || c.title.toLowerCase().includes(q)) &&
                  (!crit || String(c.isCritical) === crit) &&
                  (!auto || c.automationStatus === auto),
              )
              .map(({ links: _l, requirements: _r, ...rest }) => rest),
          },
        };
      }
      const idx = Number(id.replace("case-", ""));
      const c =
        id === "case-new"
          ? { ...tmCase(4), id, title: "New case" }
          : tmCase(Number.isInteger(idx) && idx >= 0 && idx < 5 ? idx : 0);
      if (sub === "links")
        return method === "DELETE"
          ? { body: {}, status: 204 }
          : {
              body: {
                id: "link-new",
                testCaseId: id,
                stableTestId: body.stableTestId,
                linkType: "exact",
                createdBy: "alex@example.test",
                createdAt: iso(0),
              },
              status: 201,
            };
      if (sub === "requirements") return { body: {}, status: 204 };
      if (method === "PATCH")
        return { body: { ...c, ...body, updatedAt: iso(0) } };
      return subId ? missing : { body: c };
    }
    case "test-plans":
      if (!id)
        return method === "POST"
          ? {
              body: stamp({
                id: "plan-new",
                name: body.name,
                environmentId: "",
                status: "draft",
                executions: [],
              }),
              status: 201,
            }
          : { body: { items: [{ ...tmPlan, executions: undefined }] } };
      if (sub === "executions" && subId && leaf === "evidence")
        return {
          body: {
            id: "evidence-new",
            name: "screenshot.png",
            kind: "manual_evidence",
            contentType: "image/png",
            sizeBytes: 48211,
          },
          status: 201,
        };
      if (sub === "executions")
        return {
          body: {
            id: "exec-new",
            planId: id,
            testCaseId: body.testCaseId,
            caseTitle: "",
            executedBy: "alex@example.test",
            status: body.status,
            note: body.note ?? "",
            executedAt: iso(0),
            evidence: [],
          },
          status: 201,
        };
      if (method === "PATCH")
        return { body: { ...tmPlan, status: body.status, updatedAt: iso(0) } };
      return {
        body:
          id === "plan-new"
            ? {
                ...tmPlan,
                id,
                name: "New plan",
                status: "draft",
                executions: [],
              }
            : tmPlan,
      };
    case "releases": {
      if (!id)
        return method === "POST"
          ? {
              body: stamp({
                id: "rel-new",
                version: body.version,
                branch: body.branch ?? "",
                status: "open",
                decidedBy: "",
                decidedAt: null,
                decisionNote: "",
                gate: null,
                evaluatedAt: null,
              }),
              status: 201,
            }
          : { body: { items: tmReleases } };
      const rel = tmReleases.find((r) => r.id === id) ?? {
        ...tmReleases[0]!,
        id,
        version: "new",
        gate: null,
        evaluatedAt: null,
        status: "open",
        decidedBy: "",
        decidedAt: null,
        decisionNote: "",
      };
      if (sub === "evaluate")
        return {
          body: {
            ...rel,
            gate: { ...tmGateBlocked, evaluatedAt: iso(0) },
            evaluatedAt: iso(0),
          },
        };
      if (sub === "decision")
        return {
          body: {
            ...rel,
            status: body.status,
            decidedBy: "alex@example.test",
            decidedAt: iso(0),
            decisionNote: body.note ?? "",
          },
        };
      return { body: rel };
    }
  }
  return null;
}
