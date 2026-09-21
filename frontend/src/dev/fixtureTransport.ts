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
    if (path === "/v1/projects")
      return response({
        items: [
          {
            id: "commerce",
            name: "Commerce Web",
            workspaceName: "Acme Engineering",
          },
          {
            id: "platform",
            name: "Platform API",
            workspaceName: "Acme Engineering",
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
