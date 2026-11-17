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

// One prospective baseline (§5.4) for the ROI page.
const fixtureBaseline = {
  id: "55555555-5555-4555-8555-555555555555",
  kind: "prospective",
  failures: 14,
  medianMinutes: 180,
  p90Minutes: 1560,
  rerunPassRate: 0.3,
  flakyInvestigationMinutes: 25,
  periodFrom: "2026-08-01",
  periodTo: "2026-08-14",
  note: "Two weeks, four-cell form",
  enteredBy: "em@acme.example",
  enteredAt: "2026-08-15T09:00:00Z",
};
let fixtureIdentities = [
  {
    id: "identity-1",
    provider: "github",
    email: "alex@example.test",
    createdAt: "2026-09-01T08:00:00Z",
    lastLoginAt: "2026-09-26T07:12:00Z",
  },
];
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
let fixtureInvitations = [
  {
    id: "invite-1",
    email: "new.person@example.test",
    role: "member",
    level: "project",
    invitedBy: "Alex Morgan",
    createdAt: new Date(Date.UTC(2026, 8, 18, 8)).toISOString(),
    expiresAt: new Date(Date.UTC(2026, 8, 25, 8)).toISOString(),
  },
];
const projectActions = [
  "view",
  "triage",
  "identity.merge",
  "token.manage",
  "project.configure",
  "project.manage",
  "member.manage",
  "data.delete",
  "testmgmt.write",
  "release.decide",
];
let fixtureProjects = [
  {
    id: "commerce",
    name: "Commerce Web",
    workspaceName: "Acme Engineering",
    actions: projectActions,
  },
  {
    id: "platform",
    name: "Platform API",
    workspaceName: "Acme Engineering",
    actions: ["view"],
  },
];
let fixtureDeletedProjects = [
  {
    id: "retired-storefront",
    name: "Retired Storefront",
    deletedAt: "2026-09-20T08:00:00Z",
    deletedBy: user.email,
    purgeAfter: "2026-10-20T08:00:00Z",
  },
];
const token = (
  id: string,
  name: string,
  permissions: string[],
  revokedAt: string | null,
) => ({
  id,
  prefix: "atp_ing_" + id.slice(0, 8),
  name,
  permissions,
  createdAt: "2026-09-01T08:00:00Z",
  lastUsedAt: revokedAt ? null : "2026-09-28T21:14:00Z",
  expiresAt: "2027-09-01T08:00:00Z",
  revokedAt,
});
let fixtureTokens = [
  token(
    "a1b2c3d4-0000-4000-8000-000000000001",
    "github-actions main",
    ["project:ingest", "project:read"],
    null,
  ),
  token(
    "e5f6a7b8-0000-4000-8000-000000000002",
    "old jenkins",
    ["project:ingest"],
    "2026-09-10T08:00:00Z",
  ),
];
let fixtureDeletedRuns = [
  {
    id: "deleted-run-preview",
    branch: "archive/checkout-v1",
    commitSha: "1f4c8a0d92be",
    status: "failed",
    startedAt: "2026-08-29T08:00:00Z",
    deletedAt: "2026-09-20T08:00:00Z",
    deletedBy: user.email,
    purgeAfter: "2026-10-20T08:00:00Z",
  },
];
export function createFixtureTransport(): Transport {
  const auto = createAutoFixture();
  const specialized = createSpecializedFixture();
  let fixtureAccessRequests = [
    {
      id: "request-preview",
      userId: "preview-requester",
      email: "new.member@example.test",
      requestedRole: "member",
      level: "project",
      scopeId: "commerce",
      note: "Joining the checkout testing team.",
      createdAt: "2026-10-29T08:00:00Z",
    },
  ];
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
    const sessionKind = sessionStorage.getItem(sessionKey);
    const signedIn = sessionKind !== "out";
    const signupUser = {
      ...user,
      id: "preview-signup-user",
      email:
        sessionStorage.getItem("atp-fixture-signup-email") ??
        "new@example.test",
      name: "New member",
    };
    function response(value: unknown, status = 200) {
      if (status === 204) return new Response(null, { status });
      return new Response(JSON.stringify(value), {
        status,
        headers: { "Content-Type": "application/json", "X-Request-Id": marker },
      });
    }
    if (path === "/v1/session") {
      if (init?.method === "POST") sessionStorage.setItem(sessionKey, "in");
      return response({
        csrfToken: marker,
        invitations: true,
        passwordReset: true,
        signup: true,
        providers: ["google", "github"],
        user:
          signedIn || init?.method === "POST"
            ? sessionKind === "signup"
              ? signupUser
              : user
            : null,
      });
    }
    // A preview mail is not sent. The completion path accepts an atp_sgn_
    // token so both modes of the sign-up page can be exercised explicitly.
    if (path === "/v1/session/signup") {
      if (signedIn)
        return response(
          { error: { code: "ALREADY_LOGGED_IN", message: "sign out first" } },
          400,
        );
      const body = JSON.parse(String(init?.body ?? "{}")) as {
        email?: string;
        scopeLevel?: string;
        scopeId?: string;
        requestedRole?: string;
        note?: string;
      };
      const roles: Record<string, string[]> = {
        project: ["member", "viewer", "maintainer"],
        workspace: ["workspace_admin"],
        organization: ["org_admin"],
      };
      if (
        !body.email?.includes("@") ||
        !body.scopeId ||
        !roles[body.scopeLevel ?? ""]?.includes(body.requestedRole ?? "") ||
        (body.note?.length ?? 0) > 1000
      )
        return response(
          {
            error: {
              code: "INVALID_SIGNUP",
              message: "invalid sign-up request",
            },
          },
          400,
        );
      sessionStorage.setItem("atp-fixture-signup-email", body.email);
      return response(
        {
          message: "if that email can sign up, a link to finish is on its way",
        },
        202,
      );
    }
    if (path === "/v1/session/signup/complete") {
      if (signedIn)
        return response(
          { error: { code: "ALREADY_LOGGED_IN", message: "sign out first" } },
          400,
        );
      const body = JSON.parse(String(init?.body ?? "{}")) as {
        token?: string;
        password?: string;
      };
      if (!body.token?.startsWith("atp_sgn_"))
        return response(
          { error: { code: "SIGNUP_NOT_FOUND", message: "link unavailable" } },
          404,
        );
      if ((body.password?.length ?? 0) < 12)
        return response(
          { error: { code: "INVALID_PASSWORD", message: "too short" } },
          400,
        );
      sessionStorage.setItem(sessionKey, "signup");
      return response({ csrfToken: marker, user: signupUser });
    }
    if (path === "/v1/session/password/forgot")
      return response(
        {
          message:
            "if an account uses that email, a link to choose a new password is on its way",
        },
        202,
      );
    if (path === "/v1/session/password/reset") {
      const body = JSON.parse(String(init?.body ?? "{}")) as {
        password?: string;
        token?: string;
      };
      if (!String(body.token ?? "").startsWith("atp_rst_"))
        return response(
          { error: { code: "RESET_NOT_FOUND", message: "not found" } },
          404,
        );
      if (String(body.password ?? "").length < 12)
        return response(
          { error: { code: "INVALID_PASSWORD", message: "too short" } },
          400,
        );
      sessionStorage.setItem(sessionKey, "out");
      return new Response(null, { status: 204 });
    }
    if (path === "/v1/invitations/lookup")
      return response({
        email: "new.person@example.test",
        role: "member",
        level: "project",
        scopeName: "Commerce Web (Acme Engineering)",
        invitedBy: "Alex Morgan",
        expiresAt: new Date(Date.UTC(2026, 8, 25, 8)).toISOString(),
      });
    if (path === "/v1/invitations/accept") {
      const body = JSON.parse(String(init?.body ?? "{}")) as {
        password?: string;
      };
      if (String(body.password ?? "").length < 12)
        return response(
          { error: { code: "INVALID_PASSWORD", message: "too short" } },
          400,
        );
      sessionStorage.setItem(sessionKey, "in");
      return response({ csrfToken: marker, user });
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
    // Change password: "wrong" as the current one is refused, anything
    // else is accepted.
    if (path === "/v1/session/password") {
      const body = JSON.parse(String(init?.body ?? "{}")) as {
        current?: string;
        password?: string;
      };
      if (body.current === "wrong")
        return response(
          { error: { code: "WRONG_PASSWORD", message: "not right" } },
          400,
        );
      if (String(body.password ?? "").length < 12)
        return response(
          { error: { code: "INVALID_PASSWORD", message: "too short" } },
          400,
        );
      return new Response(null, { status: 204 });
    }
    if (path === "/v1/session/logout") {
      sessionStorage.setItem(sessionKey, "out");
      return response({});
    }
    // Linked sign-ins: one GitHub; the preview user also has a password.
    if (path.startsWith("/v1/session/identities")) {
      if (init?.method === "DELETE") {
        fixtureIdentities = [];
        return new Response(null, { status: 204 });
      }
      return response({ items: fixtureIdentities, nextCursor: null });
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
        items: sessionKind === "signup" ? [] : fixtureProjects,
        nextCursor: null,
      });
    // ROI (§6.8.3): one measured baseline, so the page shows hours offline;
    // a new baseline is echoed back.
    if (/^\/v1\/projects\/[^/]+\/roi\/baselines$/.test(path))
      return init?.method === "POST"
        ? response({ ...fixtureBaseline, id: crypto.randomUUID() }, 201)
        : response({ items: [fixtureBaseline], nextCursor: null });
    if (/^\/v1\/projects\/[^/]+\/roi$/.test(path))
      return response({
        month:
          url.searchParams.get("month") || new Date().toISOString().slice(0, 7),
        from: "2026-09-01T00:00:00Z",
        to: "2026-10-01T00:00:00Z",
        failures: 42,
        triaged: 31,
        triageCoverage: 31 / 42,
        untriagedRate: 11 / 42,
        ttrcMedianMinutes: 52,
        baseline: fixtureBaseline,
        savedHours: 61.4,
        flakyHours: 6.2,
        flakyRetryHours: 3.7,
        markedFlaky: 6,
        investigationCounted: true,
        fewTriaged: false,
        minTriaged: 10,
      });
    const workspaceProjects = path.match(
      /^\/v1\/workspaces\/([^/]+)\/projects(?:\/([^/]+))?(?:\/(restore|deleted))?$/,
    );
    if (workspaceProjects) {
      const [, , projectId, action] = workspaceProjects;
      if (projectId === "deleted" && !action)
        return response({ items: fixtureDeletedProjects, nextCursor: null });
      if (projectId && action === "restore") {
        const restored = fixtureDeletedProjects.find((p) => p.id === projectId);
        if (!restored)
          return response(
            { error: { code: "NOT_FOUND", message: "not found" } },
            404,
          );
        fixtureDeletedProjects = fixtureDeletedProjects.filter(
          (p) => p.id !== projectId,
        );
        fixtureProjects = [
          ...fixtureProjects,
          {
            id: restored.id,
            name: restored.name,
            workspaceName: "Acme Engineering",
            actions: projectActions,
          },
        ];
        return response({ id: restored.id, name: restored.name });
      }
      if (!projectId && init?.method === "POST") {
        const body = JSON.parse(String(init.body ?? "{}")) as { name?: string };
        const created = {
          id: `fixture-project-${fixtureProjects.length + 1}`,
          name: String(body.name ?? ""),
          workspaceName: "Acme Engineering",
          actions: projectActions,
        };
        fixtureProjects = [...fixtureProjects, created];
        return response(created, 201);
      }
    }
    const projectLifecycle = path.match(/^\/v1\/projects\/([^/]+)$/);
    if (projectLifecycle && init?.method === "DELETE") {
      const projectId = decodeURIComponent(projectLifecycle[1]!);
      const existing = fixtureProjects.find((p) => p.id === projectId);
      if (!existing)
        return response(
          { error: { code: "NOT_FOUND", message: "not found" } },
          404,
        );
      const deleted = {
        id: existing.id,
        name: existing.name,
        deletedAt: new Date().toISOString(),
        deletedBy: user.email,
        purgeAfter: new Date(Date.now() + 30 * 86400000).toISOString(),
      };
      fixtureProjects = fixtureProjects.filter((p) => p.id !== projectId);
      fixtureDeletedProjects = [deleted, ...fixtureDeletedProjects];
      return response(deleted);
    }
    // CI tokens: state changes remain visible after issuing or revoking.
    const tokenPath = path.match(
      /^\/v1\/projects\/[^/]+\/tokens(?:\/([^/]+))?$/,
    );
    if (tokenPath) {
      if (init?.method === "DELETE") {
        fixtureTokens = fixtureTokens.map((t) =>
          t.id === tokenPath[1]
            ? { ...t, revokedAt: new Date().toISOString() }
            : t,
        );
        return new Response(null, { status: 204 });
      }
      if (init?.method === "POST") {
        const body = JSON.parse(String(init.body ?? "{}")) as {
          name?: string;
          permissions?: string[];
        };
        const issued = token(
          `c0ffee00-0000-4000-8000-${String(fixtureTokens.length + 1).padStart(12, "0")}`,
          String(body.name ?? "new pipeline"),
          body.permissions ?? ["project:ingest"],
          null,
        );
        fixtureTokens = [issued, ...fixtureTokens];
        return response(
          { token: issued, secret: "atp_ing_c0ffee00" + "x".repeat(40) },
          201,
        );
      }
      return response({ items: fixtureTokens, nextCursor: null });
    }
    // JUnit upload: always "imported" into the first fixture run, so the
    // sheet's result and its link can be seen offline.
    if (/^\/v1\/projects\/[^/]+\/imports\/junit$/.test(path))
      return response(
        {
          runId: runIds[0],
          localRunId: "44444444-4444-8444-8444-444444444444",
          created: true,
          tests: 3,
          attempts: 5,
          duplicates: 0,
          status: "failed",
          flakyCount: 1,
          startedAt: "2026-03-02T09:15:00Z",
          finishedAt: "2026-03-02T09:15:04Z",
        },
        201,
      );
    const deletedRunsPath = path.match(
      /^\/v1\/projects\/(commerce|platform)\/runs\/deleted$/,
    );
    if (deletedRunsPath)
      return response({ items: fixtureDeletedRuns, nextCursor: null });
    const restoreRunPath = path.match(
      /^\/v1\/projects\/(commerce|platform)\/runs\/([^/]+)\/restore$/,
    );
    if (restoreRunPath) {
      const runId = decodeURIComponent(restoreRunPath[2]!);
      fixtureDeletedRuns = fixtureDeletedRuns.filter((r) => r.id !== runId);
      return response({ id: runId });
    }
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
      if (id && init?.method === "DELETE") {
        const active = run(index);
        const deleted = {
          ...active,
          id,
          commitSha: String(active.commitSha),
          deletedAt: new Date().toISOString(),
          deletedBy: user.email,
          purgeAfter: new Date(Date.now() + 30 * 86400000).toISOString(),
        };
        fixtureDeletedRuns = [deleted, ...fixtureDeletedRuns];
        return response(deleted);
      }
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
    const investigationList = path.match(
      /^\/v1\/projects\/(commerce|platform)\/results\/(result-\d+)\/investigations$/,
    );
    if (investigationList) {
      const item = {
        id: "investigation-preview",
        status: "done",
        requestedBy: user.email,
        provider: "anthropic",
        model: "claude-sonnet-5-5",
        turnCount: 2,
        toolCount: 2,
        inputTokens: 1810,
        outputTokens: 220,
        error: "",
        createdAt: "2026-11-11T02:00:00Z",
        startedAt: "2026-11-11T02:00:01Z",
        finishedAt: "2026-11-11T02:00:08Z",
        hypothesis: {
          id: "hypothesis-preview",
          category: "test_bug",
          confidence: 0.82,
          evidence: [
            {
              source: "failure_context",
              fact: "Both attempts failed at the same checkout heading locator.",
            },
            {
              source: "stable_history",
              fact: "The stable test passed before the latest UI change.",
            },
          ],
          suggestedAction:
            "Update the checkout confirmation locator and keep the visibility assertion.",
          decision: null,
        },
      };
      return response(init?.body ? item : { items: [item] });
    }
    if (
      /^\/v1\/projects\/(commerce|platform)\/results\/result-\d+\/hypotheses\/[^/]+\/decision$/.test(
        path,
      )
    )
      return response({});
    // Test Management (§6.8): a small, coherent set — two suites, three
    // requirements, five cases (two critical, one linked), one plan with
    // executions, two releases (one blocked, one shipped by override).
    const autoResult = auto(url, init);
    if (autoResult) return response(autoResult.body, autoResult.status);
    const specializedResult = specialized(url, init);
    if (specializedResult)
      return response(specializedResult.body, specializedResult.status);
    const tm = fixtureTestmgmt(url, init);
    if (tm) return response(tm.body, tm.status);
    // Team screens: the preview user owns one organization with two
    // workspaces. Read-only here — writes answer as the project list does.
    if (path === "/v1/member-scopes")
      return response({
        items: (sessionKind === "signup"
          ? []
          : [
              ["organization", "acme", "Acme Engineering"],
              ["workspace", "web", "Web"],
              ["workspace", "platform-ws", "Platform"],
            ]
        ).map(([level, id, name]) => ({
          level,
          id,
          name,
          organizationName: "Acme Engineering",
          actions: ["view", "project.manage", "member.manage", "org.manage"],
        })),
        nextCursor: null,
      });
    // §10.6 shadow meter: 12 steady days and one merge wave, so the page
    // shows a flagged day and a "not yet" verdict offline.
    if (/^\/v1\/organizations\/[^/]+\/meter$/.test(path)) {
      const days = Array.from({ length: 12 }, (_, i) => {
        const active = 1180 + i * 6 - (i >= 9 ? 38 : 0);
        return {
          day: new Date(Date.UTC(2026, 8, 16 + i)).toISOString().slice(0, 10),
          activeTests: active,
          rawFingerprints: 1204 + i * 6,
          corrections: i === 9 ? 38 : i % 4 === 0 ? 1 : 0,
          runs: 14 + (i % 3),
          results: 1850 + i * 20,
          artifactBytes: Math.round((11.5 + i * 0.2) * 1024 ** 3),
          drift: i === 9 ? 0.0322 : i === 0 ? 0 : 0.0001,
          flagged: i === 9,
        };
      });
      return response({
        days,
        ready: false,
        reasons: [
          "12 of 28 days recorded",
          "1 day(s) where identity changes moved the active-test count more than 2% away from the raw fingerprints",
        ],
        requiredDays: 28,
        driftLimit: 0.02,
      });
    }
    const team = path.match(
      /^\/v1\/(organizations|workspaces)\/[^/]+\/members(?:\/[^/]+)?$/,
    );
    if (team) {
      if (init?.method === "DELETE") return new Response(null, { status: 204 });
      const org = [
        ["alex@example.test", "org_owner"],
        ["morgan@example.test", "org_owner"],
        ["riley@example.test", "org_admin"],
      ];
      const rows =
        team[1] === "organizations"
          ? org.map(([email, role]) => [email, role, "organization"])
          : [
              ...org.map(([email, role]) => [email, role, "organization"]),
              ["jordan@example.test", "workspace_admin", "workspace"],
            ];
      return response({
        items: rows.map(([email, role, level], i) => ({
          userId: `team-${i}`,
          email,
          role,
          level,
          createdAt: new Date(Date.UTC(2026, 6, 1 + i * 9)).toISOString(),
        })),
        nextCursor: null,
      });
    }
    const invitations = path.match(
      /^\/v1\/(projects|organizations|workspaces)\/[^/]+\/invitations(?:\/([^/]+))?$/,
    );
    if (invitations) {
      if (init?.method === "DELETE") {
        fixtureInvitations = fixtureInvitations.filter(
          (i) => i.id !== invitations[2],
        );
        return new Response(null, { status: 204 });
      }
      return response({ items: fixtureInvitations, nextCursor: null });
    }
    const access = path.match(
      /^\/v1\/(projects|organizations|workspaces)\/([^/]+)\/access-requests(?:\/([^/]+)\/decide)?$/,
    );
    if (access) {
      const level = access[1]!.slice(0, -1);
      const scopeId = decodeURIComponent(access[2]!);
      if (init?.method === "POST") {
        const pending = fixtureAccessRequests.find(
          (item) => item.id === access[3],
        );
        if (!pending || pending.level !== level || pending.scopeId !== scopeId)
          return response(
            { error: { code: "NOT_FOUND", message: "not found" } },
            404,
          );
        const body = JSON.parse(String(init.body ?? "{}")) as {
          decision?: string;
          role?: string;
        };
        if (body.decision !== "approved" && body.decision !== "denied")
          return response(
            { error: { code: "INVALID_PAYLOAD", message: "invalid decision" } },
            400,
          );
        fixtureAccessRequests = fixtureAccessRequests.filter(
          (item) => item.id !== pending.id,
        );
        if (body.decision === "approved" && pending.level === "project")
          fixtureMembers = [
            ...fixtureMembers,
            {
              userId: pending.userId,
              email: pending.email,
              role: body.role || pending.requestedRole,
              level: "project",
              createdAt: iso(0),
            },
          ];
        return response({ id: pending.id, decision: body.decision });
      }
      return response({
        items: fixtureAccessRequests
          .filter((item) => item.level === level && item.scopeId === scopeId)
          .map(({ scopeId: _scopeId, ...item }) => item),
      });
    }
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
        if (!known) {
          const invitation = {
            id: `invite-${fixtureInvitations.length + 1}`,
            email,
            role: String(body.role ?? "member"),
            level: "project",
            invitedBy: user.name,
            createdAt: iso(0),
            expiresAt: new Date(Date.now() + 7 * 86400000).toISOString(),
          };
          fixtureInvitations = [invitation, ...fixtureInvitations];
          return response({ invitation }, 202);
        }
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
      env = url.searchParams.get("environment")?.toLowerCase() ?? "",
      source = url.searchParams.get("source") ?? "";
    const items =
      project === "platform"
        ? []
        : runIds
            .map((_, i) => run(i))
            .filter((r) => !fixtureDeletedRuns.some((d) => d.id === r.id))
            .filter(() => !source || source === "playwright")
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
let tmSuites = [
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
];
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
      if (method === "POST") {
        const created: (typeof tmSuites)[number] = {
          id: `suite-${tmSuites.length + 1}`,
          parentId: "",
          name: String(body.name ?? ""),
          cases: 0,
          createdAt: iso(0),
        };
        tmSuites = [...tmSuites, created];
        return { body: created, status: 201 };
      }
      return { body: { items: tmSuites } };
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
        const suite = url.searchParams.get("suite");
        const crit = url.searchParams.get("critical");
        const auto = url.searchParams.get("automation");
        return {
          body: {
            items: [0, 1, 2, 3, 4]
              .map(tmCase)
              .filter(
                (c) =>
                  (!q || c.title.toLowerCase().includes(q)) &&
                  (!suite || c.suiteId === suite) &&
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

// QĐ-14/QĐ-16 preview. These answers mirror cmd/api/environments.go,
// ai.go, autotest.go and urlchecks.go; they never enter a production build.
// State belongs to one transport, so a fresh preview/test starts clean.
function createAutoFixture() {
  type Answer = { body: unknown; status?: number };
  type Step = {
    do: string;
    by: string;
    role: string;
    name: string;
    value: string;
    say: string;
    expected: string;
  };
  type Account = {
    key: string;
    username: string;
    loginUrl: string;
    loginSteps: Step[] | null;
    loginStatus: string;
    loginError: string;
    loginCheckedAt: string | null;
    updatedBy: string;
    updatedAt: string;
  };
  const stamp = iso(0);
  const missing: Answer = {
    body: { error: { code: "NOT_FOUND", message: "not found" } },
    status: 404,
  };
  const denied: Answer = {
    body: { error: { code: "FORBIDDEN", message: "permission denied" } },
    status: 403,
  };
  const invalid = (field: string): Answer => ({
    body: {
      error: {
        code: "INVALID_PAYLOAD",
        message: `check ${field}`,
        details: { field },
      },
    },
    status: 400,
  });
  const read = (init?: RequestInit): Record<string, unknown> =>
    typeof init?.body === "string"
      ? (JSON.parse(init.body) as Record<string, unknown>)
      : {};
  const strings = (value: unknown): Record<string, string> =>
    value && typeof value === "object" && !Array.isArray(value)
      ? Object.fromEntries(
          Object.entries(value).filter(([, v]) => typeof v === "string"),
        )
      : {};
  const steps = (value: unknown): Step[] =>
    Array.isArray(value) ? (value.slice(0, 40) as Step[]) : [];
  const titleStep: Step = {
    do: "expect_title",
    by: "",
    role: "",
    name: "",
    value: "Example Domain",
    say: "The example page opens",
    expected: "The page has its title",
  };
  let environments = [
    {
      id: "env-preview",
      key: "preview",
      baseUrl: "https://example.com",
      createdAt: iso(4000),
      updatedAt: stamp,
    },
    {
      id: "env-staging",
      key: "staging",
      baseUrl: "",
      createdAt: iso(3000),
      updatedAt: stamp,
    },
  ];
  type OwnershipProof = {
    host: string;
    token: string;
    method: "" | "file" | "dns";
    status: "unverified" | "checking" | "verified" | "failed";
    error: string;
    verifiedAt: string | null;
    checkedAt: string | null;
    fileUrl: string;
    fileText: string;
    dnsName: string;
    dnsText: string;
    polls: number;
    dnsAttempts: number;
  };
  const ownershipProofs = new Map<string, OwnershipProof>();
  const ownershipAnswer = (proof: OwnershipProof) => {
    const { polls: _polls, dnsAttempts: _dnsAttempts, ...answer } = proof;
    return answer;
  };
  const loadMeasurements = {
    requests: 186,
    errors: 2,
    clientErrors: 4,
    errorRate: 1.08,
    rps: 6.2,
    latencyMs: { min: 92, p50: 185, p90: 410, p95: 520, p99: 870, max: 1200 },
    statuses: { "200": 180, "404": 4, "503": 2 },
    errorSamples: ["GET /checkout: 503 Service Unavailable"],
    timeline: Array.from({ length: 30 }, (_, t) => ({
      t,
      requests: 5 + (t % 4),
      errors: t === 17 || t === 22 ? 1 : 0,
      avgMs: 160 + t * 8,
    })),
    seconds: 30,
  };
  type FixtureLoadRun = {
    id: string;
    environmentId: string;
    host: string;
    paths: string[];
    vus: number;
    seconds: number;
    rampSeconds: number;
    maxP95Ms: number | null;
    maxErrorRate: number | null;
    status: "queued" | "running" | "done" | "error";
    verdict: "" | "passed" | "failed";
    reasons: string[];
    result: typeof loadMeasurements | null;
    error: string;
    requestedBy: string;
    createdAt: string;
    startedAt: string | null;
    finishedAt: string | null;
  };
  let loadRuns: FixtureLoadRun[] = [
    {
      id: "load-run-preview",
      environmentId: "env-preview",
      host: "example.com",
      paths: ["/", "/checkout"],
      vus: 5,
      seconds: 30,
      rampSeconds: 5,
      maxP95Ms: 800,
      maxErrorRate: 2,
      status: "done",
      verdict: "passed",
      reasons: [],
      result: loadMeasurements,
      error: "",
      requestedBy: user.email,
      createdAt: iso(720),
      startedAt: iso(719),
      finishedAt: iso(718),
    },
    {
      id: "load-run-failed",
      environmentId: "env-preview",
      host: "example.com",
      paths: ["/checkout"],
      vus: 10,
      seconds: 30,
      rampSeconds: 3,
      maxP95Ms: 300,
      maxErrorRate: 0.5,
      status: "done",
      verdict: "failed",
      reasons: ["p95 520 ms exceeds 300 ms", "error rate 1.08% exceeds 0.5%"],
      result: loadMeasurements,
      error: "",
      requestedBy: user.email,
      createdAt: iso(1440),
      startedAt: iso(1439),
      finishedAt: iso(1438),
    },
    {
      id: "load-run-error",
      environmentId: "env-preview",
      host: "example.com",
      paths: ["/"],
      vus: 5,
      seconds: 30,
      rampSeconds: 0,
      maxP95Ms: null,
      maxErrorRate: null,
      status: "error",
      verdict: "",
      reasons: [],
      result: null,
      error: "The ownership proof no longer holds.",
      requestedBy: user.email,
      createdAt: iso(2160),
      startedAt: null,
      finishedAt: iso(2159),
    },
  ];
  type FixtureScriptRun = {
    id: string;
    environmentId: string;
    kind: "playwright" | "k6";
    base: string;
    entry: string;
    project: string;
    grep: string;
    vus: number | null;
    seconds: number | null;
    timeoutSeconds: number;
    archiveBytes: number;
    status: "queued" | "running" | "done" | "error";
    verdict: "" | "passed" | "failed";
    exitCode: number | null;
    timedOut: boolean;
    tests: number;
    failures: number;
    log: string;
    error: string;
    testRunId: string | null;
    requestedBy: string;
    createdAt: string;
    startedAt: string | null;
    finishedAt: string | null;
  };
  let scriptRuns: FixtureScriptRun[] = [
    {
      id: "script-run-preview",
      environmentId: "env-preview",
      kind: "playwright",
      base: "https://example.com",
      entry: "playwright.config.ts",
      project: "chromium",
      grep: "",
      vus: null,
      seconds: null,
      timeoutSeconds: 600,
      archiveBytes: 24576,
      status: "done",
      verdict: "passed",
      exitCode: 0,
      timedOut: false,
      tests: 8,
      failures: 0,
      log: "Running 8 tests using 2 workers\n8 passed (12.4s)",
      error: "",
      testRunId: "run-preview",
      requestedBy: user.email,
      createdAt: iso(600),
      startedAt: iso(599),
      finishedAt: iso(598),
    },
    {
      id: "script-run-failed",
      environmentId: "env-preview",
      kind: "k6",
      base: "https://example.com",
      entry: "load.js",
      project: "",
      grep: "",
      vus: 10,
      seconds: 30,
      timeoutSeconds: 120,
      archiveBytes: 8192,
      status: "done",
      verdict: "failed",
      exitCode: 1,
      timedOut: false,
      tests: 1,
      failures: 1,
      log: "checks.........................: 92.00%",
      error: "",
      testRunId: null,
      requestedBy: user.email,
      createdAt: iso(1200),
      startedAt: iso(1199),
      finishedAt: iso(1198),
    },
    {
      id: "script-run-timeout",
      environmentId: "env-preview",
      kind: "playwright",
      base: "https://example.com",
      entry: "playwright.config.ts",
      project: "webkit",
      grep: "checkout",
      vus: null,
      seconds: null,
      timeoutSeconds: 30,
      archiveBytes: 16384,
      status: "done",
      verdict: "failed",
      exitCode: 137,
      timedOut: true,
      tests: 2,
      failures: 1,
      log: "1 passed\n[ATP] the run reached its time limit and was stopped",
      error: "",
      testRunId: "run-preview",
      requestedBy: user.email,
      createdAt: iso(1800),
      startedAt: iso(1799),
      finishedAt: iso(1798),
    },
    {
      id: "script-run-error",
      environmentId: "env-preview",
      kind: "playwright",
      base: "https://example.com",
      entry: "playwright.config.ts",
      project: "",
      grep: "",
      vus: null,
      seconds: null,
      timeoutSeconds: 600,
      archiveBytes: 12288,
      status: "error",
      verdict: "",
      exitCode: null,
      timedOut: false,
      tests: 0,
      failures: 0,
      log: "",
      error: "The sandbox image is not available on this server.",
      testRunId: null,
      requestedBy: user.email,
      createdAt: iso(2400),
      startedAt: iso(2399),
      finishedAt: iso(2398),
    },
  ];
  let accounts: Account[] = [
    {
      key: "shopper",
      username: "shopper@example.test",
      loginUrl: "https://example.com/login",
      loginSteps: null,
      loginStatus: "unknown",
      loginError: "",
      loginCheckedAt: null,
      updatedBy: user.email,
      updatedAt: stamp,
    },
  ];
  let specs = [
    {
      caseId: "case-0",
      title: "Pay with saved card",
      priority: "p0",
      target: "/",
      account: "",
      inputs: {} as Record<string, string>,
      steps: [titleStep],
      source: "manual",
      updatedBy: user.email,
      updatedAt: stamp,
      last: {
        runId: "auto-run-preview",
        status: "passed",
        finishedAt: iso(40),
      } as { runId: string; status: string; finishedAt: string } | null,
    },
  ];
  const seedCase = {
    caseId: "case-0",
    variant: "chromium/desktop",
    position: 0,
    title: specs[0]!.title,
    target: "/",
    account: "",
    inputs: {},
    steps: [titleStep],
    status: "passed",
    results: [
      {
        status: "passed",
        kind: "",
        error: "",
        note: "The page title is available to assistive technology.",
        ms: 420,
        healed: null as { steps: Step[]; reason: string } | null,
      },
    ],
    console: [] as string[],
    healed: 0,
    durationMs: 420,
    error: "",
    screenshots: [] as string[],
    executionId: "",
    finishedAt: iso(40) as string | null,
  };
  let runs = [
    {
      id: "auto-run-preview",
      environmentId: "env-preview",
      planId: "",
      status: "done",
      trigger: "dashboard",
      requestedBy: user.email,
      heal: true,
      variants: ["chromium/desktop"],
      total: 1,
      passed: 1,
      failed: 0,
      errored: 0,
      healed: 0,
      testRunId: "",
      error: "",
      createdAt: iso(41),
      startedAt: iso(41),
      finishedAt: iso(40) as string | null,
      cases: [seedCase],
    },
  ];
  const check = {
    id: "url-check-preview",
    environmentId: "env-preview",
    url: "https://example.com",
    status: "done",
    verdict: "warning",
    title: "Example Domain",
    finalUrl: "https://example.com",
    requestedBy: user.email,
    checkStatuses: ["passed", "warning"],
    report: {
      url: "https://example.com",
      finalUrl: "https://example.com",
      status: 200,
      title: "Example Domain",
      durationMs: 820,
      blocked: [] as string[],
      metrics: {
        ttfbMs: 120,
        lcpMs: 720,
        loadMs: 820,
        cls: 0,
        requests: 1,
        transferBytes: 1256,
      },
      checks: [
        {
          key: "http",
          title: "HTTP response",
          status: "passed",
          summary: "The page responded",
          details: [] as string[],
        },
        {
          key: "mobile",
          title: "Mobile layout",
          status: "warning",
          summary: "Review this page at a narrow width",
          details: [] as string[],
        },
      ],
    },
    error: "",
    screenshots: [] as string[],
    createdAt: iso(80),
    finishedAt: iso(79) as string | null,
    depth: 0,
  };
  let checks = [check];
  let scans = [
    {
      id: "scan-preview",
      environmentId: "env-preview",
      startUrl: "https://example.com",
      maxPages: 10,
      account: "",
      status: "done",
      requestedBy: user.email,
      error: "",
      pages: 1,
      checked: 1,
      passed: 0,
      warnings: 1,
      failed: 0,
      createdAt: iso(80),
      finishedAt: iso(79) as string | null,
      checks: [check],
    },
  ];
  const drafts = new Map<string, Record<string, unknown>>();
  const caseSuggestions = new Map<string, Record<string, unknown>>();
  const listed = (items: unknown[], enabled = true): Answer => ({
    body: { items, nextCursor: null, enabled },
  });
  return (url: URL, init?: RequestInit): Answer | null => {
    const match = url.pathname.match(
      /^\/v1\/projects\/(commerce|platform)\/(.+)$/,
    );
    if (!match) return null;
    const project = match[1]!;
    const path = match[2]!;
    const method = init?.method ?? "GET";
    const body = read(init);
    const empty = project === "platform";
    const write = method !== "GET";
    if (
      write &&
      empty &&
      /^(environments|auto-specs|auto-drafts|auto-runs|site-scans|url-checks|load-runs|script-runs|test-cases\/[^/]+\/auto-spec)/.test(
        path,
      )
    )
      return denied;

    if (path === "ai-status" && !write)
      return {
        body: {
          available: true,
          source: "server",
          model: "claude-opus-5-5",
          readsPages: true,
        },
      };

    if (path === "case-suggestions" && method === "POST") {
      if (empty) return denied;
      const count = Math.min(15, Math.max(1, Number(body.count) || 1));
      const environmentId = String(body.environmentId ?? "");
      const environment = environments.find(
        (item) => item.id === environmentId,
      );
      const id = crypto.randomUUID();
      const made: Record<string, unknown> = {
        id,
        environmentId,
        testType: String(body.testType ?? "functional"),
        url: String(body.url || environment?.baseUrl || ""),
        context: String(body.context ?? ""),
        count,
        status: "done",
        drafts: Array.from({ length: count }, (_, index) => ({
          title: `Check the page ${index + 1}`,
          priority: "p2",
          preconditions: "Open the selected site.",
          steps: [{ action: "Open the page", expected: "The page loads" }],
          why: "Preview draft for the selected testing type.",
        })),
        pageRead: !!environment?.baseUrl,
        model: "claude-opus-5-5",
        keySource: "server",
        error: "",
        createdAt: stamp,
        finishedAt: stamp,
      };
      caseSuggestions.set(id, made);
      return { body: made, status: 202 };
    }
    const caseSuggestion = path.match(/^case-suggestions\/([^/]+)$/);
    if (caseSuggestion && !write) {
      const found = caseSuggestions.get(caseSuggestion[1]!);
      return found ? { body: found } : missing;
    }

    if (path === "environments") {
      if (!write)
        return { body: { items: empty ? [] : environments, nextCursor: null } };
      const key = String(body.key ?? "").trim();
      if (!key) return invalid("key");
      if (environments.some((e) => e.key === key))
        return {
          body: { error: { code: "CONFLICT", message: "environment exists" } },
          status: 409,
        };
      const made = {
        id: crypto.randomUUID(),
        key,
        baseUrl: String(body.baseUrl ?? ""),
        createdAt: stamp,
        updatedAt: stamp,
      };
      environments = [...environments, made];
      return { body: made, status: 201 };
    }
    const env = path.match(/^environments\/([^/]+)$/);
    if (env && method === "PATCH") {
      const current = environments.find((e) => e.id === env[1]);
      if (!current) return missing;
      const updated = {
        ...current,
        baseUrl: String(body.baseUrl ?? ""),
        updatedAt: stamp,
      };
      environments = environments.map((e) =>
        e.id === current.id ? updated : e,
      );
      return { body: updated };
    }
    const ownership = path.match(
      /^environments\/([^/]+)\/ownership(?:\/(check))?$/,
    );
    if (ownership) {
      const environment = environments.find((item) => item.id === ownership[1]);
      if (!environment) return missing;
      if (!environment.baseUrl) return invalid("baseUrl");
      let proof = ownershipProofs.get(environment.id);
      if (!proof) {
        const url = new URL(environment.baseUrl);
        const token = `preview-${environment.id}-proof`;
        proof = {
          host: url.host,
          token,
          method: "",
          status: "unverified",
          error: "",
          verifiedAt: null,
          checkedAt: null,
          fileUrl: `${url.origin}/.well-known/atp-verification.txt`,
          fileText: `atp-verification=${token}`,
          dnsName: `_atp-verification.${url.host}`,
          dnsText: `atp-verification=${token}`,
          polls: 0,
          dnsAttempts: 0,
        };
        ownershipProofs.set(environment.id, proof);
      }
      if (ownership[2] === "check" && method === "POST") {
        if (body.method !== "file" && body.method !== "dns")
          return invalid("method");
        proof.method = body.method;
        proof.status = "checking";
        proof.error = "";
        proof.polls = 1;
        if (body.method === "dns") proof.dnsAttempts++;
        return { body: ownershipAnswer(proof), status: 202 };
      }
      if (ownership[2] || write) return missing;
      if (proof.status === "checking" && --proof.polls <= 0) {
        proof.checkedAt = stamp;
        if (proof.method === "dns" && proof.dnsAttempts === 1) {
          proof.status = "failed";
          proof.error =
            "The TXT record has not propagated. Publish the shown value and check again.";
        } else {
          proof.status = "verified";
          proof.error = "";
          proof.verifiedAt = stamp;
        }
      }
      return { body: ownershipAnswer(proof) };
    }
    if (path === "load-runs") {
      if (!write) return listed(empty ? [] : loadRuns, !empty);
      const environment = environments.find(
        (item) => item.id === body.environmentId,
      );
      if (!environment?.baseUrl) return invalid("environmentId");
      const proof = ownershipProofs.get(environment.id);
      if (proof?.status !== "verified")
        return {
          body: {
            error: {
              code: "OWNERSHIP_NOT_PROVED",
              message: "Prove this host before sending load.",
            },
          },
          status: 409,
        };
      const v = Number(body.vus),
        duration = Number(body.seconds),
        ramp = Number(body.rampSeconds ?? 0);
      if (!Number.isInteger(v) || v < 1 || v > 20)
        return {
          body: {
            error: {
              code: "INVALID_PAYLOAD",
              message: "vus: 1 to 20 on this server (LOAD_MAX_VUS)",
            },
          },
          status: 400,
        };
      if (!Number.isInteger(duration) || duration < 5 || duration > 60)
        return {
          body: {
            error: {
              code: "INVALID_PAYLOAD",
              message: "seconds: 5 to 60 on this server (LOAD_MAX_SECONDS)",
            },
          },
          status: 400,
        };
      if (!Number.isInteger(ramp) || ramp < 0 || ramp >= duration)
        return invalid("rampSeconds");
      if (
        loadRuns.some(
          (item) => item.status === "queued" || item.status === "running",
        )
      )
        return {
          body: {
            error: {
              code: "TOO_MANY_LOAD_RUNS",
              message:
                "the organization has a load test running, or the project reached today's limit",
            },
          },
          status: 429,
        };
      const paths = Array.isArray(body.paths) ? body.paths.map(String) : [];
      if (
        paths.length > 20 ||
        paths.some((item) => !item.startsWith("/") || item.startsWith("//"))
      )
        return invalid("paths");
      const made: FixtureLoadRun = {
        id: crypto.randomUUID(),
        environmentId: environment.id,
        host: new URL(environment.baseUrl).host,
        paths: paths.length ? paths : ["/"],
        vus: v,
        seconds: duration,
        rampSeconds: ramp,
        maxP95Ms: Number(body.maxP95Ms) || null,
        maxErrorRate:
          body.maxErrorRate === undefined ? null : Number(body.maxErrorRate),
        status: "queued",
        verdict: "",
        reasons: [],
        result: null,
        error: "",
        requestedBy: user.email,
        createdAt: stamp,
        startedAt: null,
        finishedAt: null,
      };
      loadRuns = [made, ...loadRuns].slice(0, 50);
      return { body: made, status: 202 };
    }
    const loadRun = path.match(/^load-runs\/([^/]+)$/);
    if (loadRun && !write) {
      const found = loadRuns.find((item) => item.id === loadRun[1]);
      if (!found) return missing;
      const answer = { ...found };
      if (found.status === "queued") {
        found.status = "running";
        found.startedAt = stamp;
      } else if (found.status === "running") {
        found.status = "done";
        found.verdict = "passed";
        found.result = loadMeasurements;
        found.finishedAt = stamp;
      }
      return { body: answer };
    }
    if (path === "script-runs") {
      if (!write)
        return listed(
          empty ? [] : scriptRuns.map((item) => ({ ...item, log: "" })),
          !empty,
        );
      const form = init?.body instanceof FormData ? init.body : null;
      if (!form) return invalid("archive");
      const file = form.get("archive"),
        kind = String(form.get("kind") ?? "");
      const environmentId = String(form.get("environmentId") ?? "");
      const environment = environments.find(
        (item) => item.id === environmentId,
      );
      if (!(file instanceof File) || !file.name.endsWith(".tar.gz"))
        return invalid("archive");
      if (file.size > 10 * 1024 * 1024)
        return {
          body: {
            error: {
              code: "PAYLOAD_TOO_LARGE",
              message: "archive exceeds 10 MB",
            },
          },
          status: 413,
        };
      if (!environment?.baseUrl || (kind !== "playwright" && kind !== "k6"))
        return invalid(!environment?.baseUrl ? "environmentId" : "kind");
      if (
        kind === "k6" &&
        ownershipProofs.get(environmentId)?.status !== "verified"
      )
        return {
          body: {
            error: {
              code: "OWNERSHIP_NOT_PROVED",
              message: "prove this host before k6",
            },
          },
          status: 409,
        };
      const made: FixtureScriptRun = {
        id: crypto.randomUUID(),
        environmentId,
        kind,
        base: environment.baseUrl,
        entry: String(form.get("entry") ?? ""),
        project: String(form.get("project") ?? ""),
        grep: String(form.get("grep") ?? ""),
        vus: kind === "k6" ? Number(form.get("vus")) : null,
        seconds: kind === "k6" ? Number(form.get("seconds")) : null,
        timeoutSeconds: Number(form.get("timeoutSeconds") ?? 600),
        archiveBytes: file.size,
        status: "queued",
        verdict: "",
        exitCode: null,
        timedOut: false,
        tests: 0,
        failures: 0,
        log: "Archive accepted. Waiting for sandbox…",
        error: "",
        testRunId: null,
        requestedBy: user.email,
        createdAt: stamp,
        startedAt: null,
        finishedAt: null,
      };
      scriptRuns = [made, ...scriptRuns].slice(0, 50);
      return { body: made, status: 202 };
    }
    const scriptRun = path.match(/^script-runs\/([^/]+)$/);
    if (scriptRun && !write) {
      const found = scriptRuns.find((item) => item.id === scriptRun[1]);
      if (!found) return missing;
      const answer = { ...found };
      if (found.status === "queued") {
        found.status = "running";
        found.startedAt = stamp;
        found.log += "\nStarting runner…";
      } else if (found.status === "running") {
        found.status = "done";
        found.verdict = "passed";
        found.exitCode = 0;
        found.tests = 3;
        found.failures = 0;
        found.log += "\n3 passed";
        found.testRunId = "run-preview";
        found.finishedAt = stamp;
      }
      return { body: answer };
    }
    const account = path.match(
      /^environments\/([^/]+)\/accounts(?:\/([^/]+))?(?:\/(find-login))?$/,
    );
    if (account) {
      if (!environments.some((e) => e.id === account[1])) return missing;
      const key = account[2];
      if (!key && !write) return listed(empty ? [] : accounts);
      if (!key && method === "POST") {
        const accountKey = String(body.key ?? "").trim();
        const username = String(body.username ?? "").trim();
        if (!accountKey || !username)
          return invalid(!accountKey ? "key" : "username");
        const before = accounts.find((a) => a.key === accountKey);
        const saved: Account = {
          key: accountKey,
          username,
          loginUrl: String(body.loginUrl ?? ""),
          loginSteps:
            body.loginSteps === undefined
              ? (before?.loginSteps ?? null)
              : steps(body.loginSteps),
          loginStatus: before?.loginStatus ?? "unknown",
          loginError: "",
          loginCheckedAt: before?.loginCheckedAt ?? null,
          updatedBy: user.email,
          updatedAt: stamp,
        };
        accounts = [...accounts.filter((a) => a.key !== accountKey), saved];
        return { body: saved };
      }
      const found = accounts.find((a) => a.key === key);
      if (!found) return missing;
      if (method === "DELETE") {
        accounts = accounts.filter((a) => a.key !== key);
        return { body: {}, status: 204 };
      }
      if (method === "POST" && account[3] === "find-login") {
        const foundLogin = {
          ...found,
          loginStatus: "ok",
          loginError: "",
          loginCheckedAt: stamp,
          loginSteps: [
            {
              do: "fill",
              by: "label",
              role: "",
              name: "Email",
              value: "{{username}}",
              say: "",
              expected: "",
            },
            {
              do: "fill",
              by: "label",
              role: "",
              name: "Password",
              value: "{{password}}",
              say: "",
              expected: "",
            },
            {
              do: "click",
              by: "role",
              role: "button",
              name: "Sign in",
              value: "",
              say: "",
              expected: "",
            },
          ],
        };
        accounts = accounts.map((a) => (a.key === key ? foundLogin : a));
        return { body: foundLogin, status: 202 };
      }
    }

    if (path === "auto-specs/export" && !write)
      return {
        body: {
          filename: "atp-auto.spec.ts",
          content: "// Preview export. Connect a server to run these cases.\n",
        },
      };
    if (path === "auto-specs") {
      if (!write) return listed(empty ? [] : specs);
      const title = String(body.title ?? "").trim();
      if (!title || !Array.isArray(body.steps) || !body.steps.length)
        return invalid(!title ? "title" : "steps");
      const made = {
        caseId: crypto.randomUUID(),
        title,
        priority: String(body.priority ?? "p2"),
        target: String(body.target ?? "/"),
        account: String(body.account ?? ""),
        inputs: strings(body.inputs),
        steps: steps(body.steps),
        source: String(body.source ?? "manual"),
        updatedBy: user.email,
        updatedAt: stamp,
        last: null,
      };
      specs = [...specs, made];
      return { body: made, status: 201 };
    }
    const spec = path.match(/^test-cases\/([^/]+)\/auto-spec$/);
    if (spec) {
      const old = specs.find((s) => s.caseId === spec[1]);
      if (!old) return missing;
      if (!write) return { body: old };
      if (method === "DELETE") {
        specs = specs.filter((s) => s.caseId !== old.caseId);
        return { body: {}, status: 204 };
      }
      const saved = {
        ...old,
        target: String(body.target ?? old.target),
        account: String(body.account ?? old.account),
        inputs: strings(body.inputs),
        steps: steps(body.steps),
        updatedAt: stamp,
      };
      specs = specs.map((s) => (s.caseId === old.caseId ? saved : s));
      return { body: saved };
    }
    if (path === "auto-drafts" && method === "POST") {
      if (!environments.some((e) => e.id === body.environmentId))
        return invalid("environmentId");
      const id = crypto.randomUUID();
      const made: Record<string, unknown> = {
        id,
        environmentId: body.environmentId,
        target: String(body.target ?? "/"),
        account: String(body.account ?? ""),
        inputs: strings(body.inputs),
        context: String(body.context ?? ""),
        count: 1,
        testType: String(body.testType ?? "functional"),
        status: "done",
        drafts: [
          {
            title: "Example page opens",
            priority: "p2",
            why: "The page title confirms the destination loaded.",
            steps: [titleStep],
          },
        ],
        pageRead: true,
        model: "claude-opus-5-5",
        keySource: "server",
        error: "",
        requestedBy: user.email,
        createdAt: stamp,
        finishedAt: stamp,
      };
      drafts.set(id, made);
      return { body: made, status: 202 };
    }
    const draft = path.match(/^auto-drafts\/([^/]+)$/);
    if (draft && !write)
      return {
        body: drafts.get(draft[1]!) ?? missing.body,
        status: drafts.has(draft[1]!) ? 200 : 404,
      };

    if (path === "auto-runs") {
      if (!write) return listed(empty ? [] : runs);
      if (!environments.some((e) => e.id === body.environmentId))
        return invalid("environmentId");
      const ids =
        Array.isArray(body.caseIds) && body.caseIds.length
          ? body.caseIds
          : specs.map((s) => s.caseId);
      const selected = specs.filter((s) => ids.includes(s.caseId));
      if (!selected.length) return invalid("caseIds");
      const id = crypto.randomUUID();
      const requestedVariants = Array.isArray(body.variants)
        ? body.variants.map(String)
        : [];
      const variants = requestedVariants.length
        ? requestedVariants
        : ["chromium/desktop"];
      const allowedVariants = new Set([
        "chromium/desktop",
        "chromium/phone",
        "chromium/tablet",
        "firefox/desktop",
        "firefox/phone",
        "firefox/tablet",
        "webkit/desktop",
        "webkit/phone",
        "webkit/tablet",
      ]);
      if (
        variants.length > 9 ||
        variants.some((variant) => !allowedVariants.has(variant)) ||
        selected.length * variants.length > 300
      )
        return invalid("variants");
      const cases = variants.flatMap((variant) =>
        selected.map((s, position) => ({
          ...seedCase,
          caseId: s.caseId,
          variant,
          position,
          title: s.title,
          target: s.target,
          account: s.account,
          inputs: s.inputs,
          steps: s.steps,
          results: s.steps.map((step) => ({
            status: "passed",
            kind: "",
            error: "",
            note: String(step.do).startsWith("check_")
              ? "No findings exceeded this check's threshold."
              : "",
            ms: 420,
            healed: null,
          })),
        })),
      );
      const made = {
        ...runs[0]!,
        id,
        environmentId: String(body.environmentId),
        planId: "",
        heal: body.heal !== false,
        variants,
        total: cases.length,
        passed: cases.length,
        failed: 0,
        errored: 0,
        testRunId: "",
        createdAt: stamp,
        startedAt: stamp,
        finishedAt: stamp,
        cases,
      };
      runs = [made, ...runs].slice(0, 50);
      specs = specs.map((s) =>
        selected.some((c) => c.caseId === s.caseId)
          ? { ...s, last: { runId: id, status: "passed", finishedAt: stamp } }
          : s,
      );
      return { body: made, status: 202 };
    }
    const run = path.match(/^auto-runs\/([^/]+)$/);
    if (run && !write) {
      const found = runs.find((r) => r.id === run[1]);
      return found ? { body: found } : missing;
    }
    const heal = path.match(/^auto-runs\/[^/]+\/cases\/([^/]+)\/apply-heal$/);
    if (heal && method === "POST") {
      const found = specs.find((s) => s.caseId === heal[1]);
      return found ? { body: found } : missing;
    }

    if (path === "site-scans") {
      if (!write) return listed(empty ? [] : scans);
      const environmentId = String(body.environmentId ?? "");
      const environment = environments.find((e) => e.id === environmentId);
      const startUrl = String(body.url || environment?.baseUrl || "");
      if (!startUrl) return invalid("url");
      const id = crypto.randomUUID();
      const page = {
        ...check,
        id: crypto.randomUUID(),
        environmentId,
        url: startUrl,
        finalUrl: startUrl,
        report: { ...check.report, url: startUrl, finalUrl: startUrl },
        createdAt: stamp,
        finishedAt: stamp,
      };
      checks = [page, ...checks].slice(0, 50);
      const made = {
        ...scans[0]!,
        id,
        environmentId,
        startUrl,
        maxPages: Number(body.maxPages) || 10,
        account: String(body.account ?? ""),
        createdAt: stamp,
        finishedAt: stamp,
        checks: [page],
      };
      scans = [made, ...scans].slice(0, 30);
      return { body: made, status: 202 };
    }
    const scan = path.match(/^site-scans\/([^/]+)$/);
    if (scan && !write) {
      const found = scans.find((s) => s.id === scan[1]);
      return found ? { body: found } : missing;
    }
    if (path === "url-checks") {
      if (!write) return listed(empty ? [] : checks);
      const id = crypto.randomUUID();
      const made = {
        ...check,
        id,
        environmentId: String(body.environmentId ?? ""),
        url: String(body.url ?? check.url),
        createdAt: stamp,
        finishedAt: stamp,
      };
      checks = [made, ...checks].slice(0, 50);
      return { body: made, status: 202 };
    }
    const urlCheck = path.match(/^url-checks\/([^/]+)$/);
    if (urlCheck && !write) {
      const found = checks.find((c) => c.id === urlCheck[1]);
      return found ? { body: found } : missing;
    }
    return null;
  };
}

// The specialized-results preview starts empty, like a new project. Records
// entered here stay visible for this transport's lifetime and use the same
// DTO shape as the Go API; the server remains responsible for real assessment.
function createSpecializedFixture() {
  type Result = {
    id: string;
    kind: string;
    title: string;
    outcome: string;
    reasons: string[];
    input: Record<string, unknown>;
    createdBy: string;
    createdAt: string;
  };
  let records: Result[] = [];
  return (
    url: URL,
    init?: RequestInit,
  ): { body: unknown; status?: number } | null => {
    const match = url.pathname.match(
      /^\/v1\/projects\/(commerce|platform)\/specialized-results(?:\/([^/]+))?$/,
    );
    if (!match) return null;
    if (init?.method === "POST") {
      if (match[1] === "platform")
        return {
          body: { error: { code: "FORBIDDEN", message: "permission denied" } },
          status: 403,
        };
      const input = JSON.parse(String(init.body ?? "{}")) as Record<
        string,
        unknown
      >;
      const existing = records.find(
        (item) => item.input.idempotencyKey === input.idempotencyKey,
      );
      if (existing) return { body: existing };
      const state = String(input.state ?? "");
      const record: Result = {
        id: crypto.randomUUID(),
        kind: String(input.kind ?? ""),
        title: String(input.title ?? ""),
        outcome: ["not_run", "not_applicable", "import_error"].includes(state)
          ? state
          : "inconclusive",
        reasons:
          state === "completed"
            ? ["Preview only; assessment needs the server."]
            : [],
        input,
        createdBy: user.email,
        createdAt: iso(0),
      };
      records = [record, ...records].slice(0, 50);
      return { body: record, status: 201 };
    }
    if (match[2]) {
      const found = records.find((item) => item.id === match[2]);
      return found
        ? { body: found }
        : {
            body: { error: { code: "NOT_FOUND", message: "not found" } },
            status: 404,
          };
    }
    const kind = url.searchParams.get("kind");
    const outcome = url.searchParams.get("outcome");
    return {
      body: {
        items: records.filter(
          (item) =>
            (!kind || item.kind === kind) &&
            (!outcome || item.outcome === outcome),
        ),
        nextCursor: "",
      },
    };
  };
}
