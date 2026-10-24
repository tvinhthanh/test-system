import { flushPromises, mount } from "@vue/test-utils";
import { expect, it } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { AutotestRepository } from "../src/features/autotest/autotestRepository";
import StepsEditor from "../src/features/autotest/StepsEditor.vue";
import InputsEditor from "../src/features/autotest/InputsEditor.vue";
import {
  describeStep,
  type AutoStep,
} from "../src/features/autotest/steps";

// QĐ-16: the auto test repository speaks the api's shapes — runnable cases,
// drafts, runs with step results and AI repairs, accounts without
// passwords, site scans — and its editors keep only the fields a step's
// action takes. Page and model text stays text.
const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new AutotestRepository(client);
};
const at = "2026-10-24T09:00:00Z";
const step = (s: Partial<AutoStep>): AutoStep => ({
  do: "click",
  by: "",
  role: "",
  name: "",
  value: "",
  say: "",
  expected: "",
  ...s,
});

it("creates a runnable case and reads a run with a repaired step", async () => {
  const calls: { path: string; method: string; body: unknown }[] = [];
  const r = repo(async (path, init) => {
    calls.push({
      path: String(path),
      method: init?.method ?? "GET",
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    if (String(path).endsWith("/auto-specs"))
      return json(
        {
          caseId: "c1",
          title: "Gửi tin nhắn",
          priority: "p0",
          target: "/message?to={{to}}",
          account: "default",
          inputs: { to: "bob" },
          steps: [{ do: "click", by: "role", role: "button", name: "Gửi", value: "", say: "", expected: "" }],
          source: "ai",
          updatedBy: "tia",
          updatedAt: at,
          last: null,
        },
        201,
      );
    return json({
      id: "r1",
      environmentId: "e1",
      planId: "p1",
      status: "done",
      trigger: "ci",
      requestedBy: "token:t1",
      heal: true,
      total: 1,
      passed: 1,
      failed: 0,
      errored: 0,
      healed: 1,
      testRunId: "tr1",
      error: "",
      createdAt: at,
      startedAt: at,
      finishedAt: at,
      cases: [
        {
          caseId: "c1",
          position: 0,
          title: "<img src=x onerror=alert(1)>",
          target: "/message",
          account: "",
          inputs: {},
          steps: [{ do: "click", by: "role", role: "button", name: "Send", value: "", say: "", expected: "" }],
          status: "passed",
          results: [
            {
              status: "passed",
              ms: 120,
              error: "not found",
              healed: { reason: "Send is now Gửi", steps: [{ do: "click", by: "role", role: "button", name: "Gửi", value: "", say: "", expected: "" }] },
            },
          ],
          console: ["console: <b>boom</b>"],
          healed: 1,
          durationMs: 3000,
          error: "",
          screenshots: ["final"],
          executionId: "x1",
          finishedAt: at,
        },
      ],
    });
  });
  const made = await r.createCase(
    "p1",
    {
      title: "Gửi tin nhắn",
      priority: "p0",
      testType: "functional",
      target: "/message?to={{to}}",
      account: "default",
      inputs: { to: "bob" },
      steps: [step({ by: "role", role: "button", name: "Gửi" })],
      source: "ai",
    },
    signal(),
  );
  expect(made.source).toBe("ai");
  expect(calls[0]).toMatchObject({ path: "/v1/projects/p1/auto-specs", method: "POST" });
  const run = await r.run("p1", "r1", signal());
  expect(run.cases[0]?.title).toBe("<img src=x onerror=alert(1)>");
  expect(run.cases[0]?.results[0]?.healed?.steps[0]?.name).toBe("Gửi");
  expect(r.shotUrl("p1", "r1", "c1", "final")).toBe(
    "/v1/projects/p1/auto-runs/r1/cases/c1/screenshots/final",
  );
  await r.applyHeal("p1", "r1", "c1", 0, signal()).catch(() => undefined);
  expect(calls.at(-1)).toMatchObject({
    path: "/v1/projects/p1/auto-runs/r1/cases/c1/apply-heal",
    method: "POST",
    body: { step: 0 },
  });
});

it("never sends an empty password, and refuses an unknown step action", async () => {
  const bodies: unknown[] = [];
  const r = repo(async (path, init) => {
    if (init?.body) bodies.push(JSON.parse(String(init.body)));
    if (String(path).includes("/accounts"))
      return json({
        key: "default",
        username: "tester@example.com",
        loginUrl: "/login",
        loginSteps: null,
        loginStatus: "unknown",
        loginError: "",
        loginCheckedAt: null,
        updatedBy: "tia",
        updatedAt: at,
      });
    return json({ items: [{ caseId: "c1", title: "x", priority: "p2", target: "/", account: "", inputs: {}, steps: [{ do: "eval" }], source: "manual", updatedBy: "t", updatedAt: at, last: null }], enabled: true });
  });
  const a = await r.saveAccount(
    "p1",
    "e1",
    { key: "default", username: "tester@example.com", password: "", loginUrl: "/login" },
    signal(),
  );
  expect(a.loginSteps).toBeNull();
  expect(bodies[0]).toEqual({ key: "default", username: "tester@example.com", loginUrl: "/login" });
  await expect(r.specs("p1", signal())).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
});

it("words a step the way the server does", () => {
  expect(describeStep(step({ do: "fill", by: "label", name: "Message", value: "hi" }))).toBe(
    "Type “hi” into the field “Message”",
  );
  expect(describeStep(step({ do: "click", by: "role", role: "button", name: "Send" }))).toBe(
    "Click button “Send”",
  );
  expect(describeStep(step({ do: "expect_text", value: "Done" }))).toBe(
    "See the text “Done” on the page",
  );
});

it("keeps only the fields an action takes when the action changes", async () => {
  const wrapper = mount(StepsEditor, {
    props: {
      id: "s",
      modelValue: [step({ do: "fill", by: "label", name: "Message", value: "hi" })],
    },
  });
  const select = wrapper.find("select");
  await select.setValue("expect_title");
  const emitted = wrapper.emitted("update:modelValue")!;
  expect(emitted.at(-1)![0]).toEqual([
    step({ do: "expect_title", by: "", role: "", name: "", value: "hi" }),
  ]);
  await wrapper.setProps({ modelValue: emitted.at(-1)![0] as AutoStep[] });
  await select.setValue("click");
  expect((wrapper.emitted("update:modelValue")!.at(-1)![0] as AutoStep[])[0]).toMatchObject({
    do: "click",
    by: "role",
    role: "button",
    value: "",
  });
});

it("turns input rows into a name → value map", async () => {
  const wrapper = mount(InputsEditor, {
    props: { id: "i", modelValue: {} },
  });
  await wrapper.find("button").trigger("click");
  const fields = wrapper.findAll("input");
  await fields[0]!.setValue("recipient");
  await fields[1]!.setValue("bob");
  await flushPromises();
  expect(wrapper.emitted("update:modelValue")!.at(-1)![0]).toEqual({ recipient: "bob" });
});
