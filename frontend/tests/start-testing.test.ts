import { it, expect } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { TestmgmtRepository } from "../src/features/testmgmt/testmgmtRepository";
import { taxonomyFor, testTypes } from "../src/features/testmgmt/testTypes";
import { purposes, usageTags } from "../src/features/testmgmt/taxonomy";

// QĐ-15: the start flow's kinds of testing are exactly the case taxonomy's
// tags and purposes, every kind has a usable starter set, new cases get the
// matching taxonomy, and the AI payloads decode as plain text.
const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new TestmgmtRepository(client);
};
const at = "2026-10-23T09:00:00Z";

it("offers the ten taxonomy kinds, each with starter cases that have steps", () => {
  expect(testTypes.map((t) => t.id).sort()).toEqual(
    [...usageTags, ...purposes].sort(),
  );
  for (const t of testTypes) {
    expect(t.starters.length, t.id).toBeGreaterThan(2);
    for (const c of t.starters) {
      expect(c.title.length).toBeGreaterThan(5);
      expect(c.steps.length).toBeGreaterThan(0);
      for (const s of c.steps) expect(s.action && s.expected).toBeTruthy();
    }
  }
});

it("tags new cases with their kind", () => {
  expect(taxonomyFor("smoke")).toMatchObject({
    tags: ["smoke"],
    purposes: [],
    executionModes: ["manual"],
  });
  expect(taxonomyFor("accessibility")).toMatchObject({
    tags: [],
    purposes: ["accessibility"],
  });
  expect(taxonomyFor("performance").performanceTypes).toEqual([]);
});

it("creates a plan with its kind and decodes AI suggestions as text", async () => {
  const bodies: unknown[] = [];
  const r = repo(async (path, init) => {
    if (init?.body) bodies.push(JSON.parse(String(init.body)));
    if (String(path).endsWith("/test-plans"))
      return json(
        {
          id: "pl1",
          name: "Smoke",
          environmentId: "e1",
          environment: null,
          testType: "smoke",
          status: "draft",
          cases: [],
          createdAt: at,
          updatedAt: at,
        },
        201,
      );
    if (String(path).endsWith("/ai-status"))
      return json({
        available: true,
        source: "organization",
        model: "claude-sonnet-5-5",
        readsPages: true,
      });
    return json({
      id: "s1",
      environmentId: "e1",
      testType: "smoke",
      url: "https://shop.example",
      context: "đăng nhập",
      count: 2,
      status: "done",
      pageRead: true,
      model: "claude-sonnet-5-5",
      keySource: "organization",
      error: "",
      requestedBy: "tia",
      createdAt: at,
      finishedAt: at,
      drafts: [
        {
          title: "<img src=x onerror=alert(1)>",
          priority: "p0",
          preconditions: "",
          why: "w",
          steps: [{ action: "a", expected: "b" }],
        },
      ],
    });
  });
  const plan = await r.createPlan(
    "p1",
    "Smoke",
    signal(),
    ["c1"],
    "e1",
    "smoke",
  );
  expect(plan.testType).toBe("smoke");
  expect(bodies[0]).toEqual({
    name: "Smoke",
    caseIds: ["c1"],
    environmentId: "e1",
    testType: "smoke",
  });
  expect((await r.aiStatus("p1", signal())).source).toBe("organization");
  const s = await r.suggestion("p1", "s1", signal());
  expect(s.drafts[0]?.title).toBe("<img src=x onerror=alert(1)>");
  expect(s.drafts[0]?.steps).toEqual([{ action: "a", expected: "b" }]);
});

it("saves AI settings with POST: model always, a key only for the own-key choice", async () => {
  const sent: { method?: string; body: unknown }[] = [];
  const settings = {
    models: [
      { id: "claude-opus-5-5", label: "Claude Opus 5.5", note: "Default." },
    ],
    server: { available: true },
    defaultModel: "claude-opus-5-5",
    organization: {
      model: "claude-opus-5-5",
      keySource: "organization",
      keyHint: "1234",
      updatedBy: "ana",
      updatedAt: at,
    },
    effective: {
      available: true,
      source: "organization",
      model: "claude-opus-5-5",
    },
  };
  const r = repo(async (_path, init) => {
    sent.push({
      method: init?.method,
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    return json(settings);
  });
  const out = await r.saveAiSettings(
    "o1",
    { model: "claude-opus-5-5", keySource: "organization", apiKey: "" },
    signal(),
  );
  expect(out.organization?.keyHint).toBe("1234");
  expect(out.server.available).toBe(true);
  await r.saveAiSettings(
    "o1",
    { model: "claude-opus-5-5", keySource: "organization", apiKey: "sk-ant-x" },
    signal(),
  );
  // The server's key: a typed key is never sent along.
  await r.saveAiSettings(
    "o1",
    { model: "claude-sonnet-5-5", keySource: "server", apiKey: "sk-ant-x" },
    signal(),
  );
  expect(sent).toEqual([
    {
      method: "POST",
      body: { model: "claude-opus-5-5", keySource: "organization" },
    },
    {
      method: "POST",
      body: {
        model: "claude-opus-5-5",
        keySource: "organization",
        apiKey: "sk-ant-x",
      },
    },
    {
      method: "POST",
      body: { model: "claude-sonnet-5-5", keySource: "server" },
    },
  ]);
});
