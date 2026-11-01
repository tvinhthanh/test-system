import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { TestmgmtRepository } from "../src/features/testmgmt/testmgmtRepository";

// QĐ-14: environments carry the site under test, URL checks report on a
// page. Both end up as links and text in the UI, so the decoders are the
// gate: only http(s) addresses, report text kept as plain strings.
const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new TestmgmtRepository(client);
};
const at = "2026-10-23T09:00:00Z";
const env = (baseUrl: string) => ({
  id: "e1",
  key: "staging",
  baseUrl,
  createdAt: at,
  updatedAt: at,
});

it("lists environments and refuses an address that is not http(s)", async () => {
  const ok = repo(async () =>
    json({ items: [env("https://staging.example")], nextCursor: null }),
  );
  expect((await ok.environments("p1", signal()))[0]?.baseUrl).toBe(
    "https://staging.example",
  );
  const bad = repo(async () =>
    json({ items: [env("javascript:alert(1)")], nextCursor: null }),
  );
  await expect(bad.environments("p1", signal())).rejects.toThrow();
});

it("creates a plan on an environment and reads its address back", async () => {
  const transport = vi.fn(async (_path: string, init?: RequestInit) => {
    expect(JSON.parse(String(init?.body))).toEqual({
      name: "R1",
      caseIds: [],
      environmentId: "e1",
    });
    return json(
      {
        id: "pl1",
        name: "R1",
        environmentId: "e1",
        environment: env("https://staging.example"),
        status: "draft",
        cases: [],
        createdAt: at,
        updatedAt: at,
      },
      201,
    );
  });
  const plan = await repo(transport as Transport).createPlan(
    "p1",
    "R1",
    signal(),
    [],
    "e1",
  );
  expect(plan.environment?.baseUrl).toBe("https://staging.example");
});

it("decodes a URL check report as plain text and builds screenshot paths", async () => {
  const check = {
    id: "c1",
    environmentId: "",
    url: "https://shop.example",
    status: "done",
    verdict: "failed",
    title: "<img src=x onerror=alert(1)>",
    finalUrl: "https://shop.example/",
    requestedBy: "tia@acme.example",
    checkStatuses: ["passed", "failed"],
    report: {
      url: "https://shop.example",
      finalUrl: "https://shop.example/",
      status: 200,
      title: "<b>Shop</b>",
      durationMs: 3900,
      blocked: ["http://10.0.0.1/: private"],
      metrics: {
        ttfbMs: 120,
        lcpMs: null,
        loadMs: 800,
        cls: 0,
        requests: 9,
        transferBytes: 4096,
      },
      checks: [
        {
          key: "load",
          title: "Page loads",
          status: "passed",
          summary: "200",
          details: [],
        },
        {
          key: "links",
          title: "Links work",
          status: "failed",
          summary: "1 broken",
          details: ["404 https://shop.example/x"],
        },
      ],
    },
    error: "",
    screenshots: ["desktop"],
    createdAt: at,
    finishedAt: at,
  };
  const r = repo(async () => json(check));
  const got = await r.urlCheck("p1", "c1", signal());
  expect(got.title).toBe("<img src=x onerror=alert(1)>");
  expect(got.report?.checks[1]?.details).toEqual(["404 https://shop.example/x"]);
  expect(got.report?.metrics.lcpMs).toBeNull();
  expect(r.urlCheckScreenshot("p 1", "c1", "desktop")).toBe(
    "/v1/projects/p%201/url-checks/c1/screenshots/desktop",
  );
  const unknown = repo(async () => json({ ...check, verdict: "great" }));
  await expect(unknown.urlCheck("p1", "c1", signal())).rejects.toThrow();
});

it("asks for a check with only what was given", async () => {
  const bodies: unknown[] = [];
  const transport = vi.fn(async (_path: string, init?: RequestInit) => {
    bodies.push(JSON.parse(String(init?.body)));
    return json(
      {
        id: "c2",
        environmentId: "e1",
        url: "https://staging.example",
        status: "queued",
        verdict: "",
        title: "",
        finalUrl: "",
        requestedBy: "t",
        checkStatuses: [],
        error: "",
        screenshots: [],
        createdAt: at,
        finishedAt: null,
      },
      202,
    );
  });
  const r = repo(transport as Transport);
  await r.createUrlCheck("p1", { url: "", environmentId: "e1" }, signal());
  await r.createUrlCheck(
    "p1",
    { url: "https://x.example", environmentId: "" },
    signal(),
  );
  expect(bodies).toEqual([
    { environmentId: "e1" },
    { url: "https://x.example" },
  ]);
});
