import { flushPromises, mount } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { servicesKey } from "../src/app/providers/services";
import {
  TestmgmtRepository,
  type Plan,
  type PlanCase,
} from "../src/features/testmgmt/testmgmtRepository";
import PlanScopeEditor from "../src/features/testmgmt/PlanScopeEditor.vue";
import PlanCaseControls from "../src/features/testmgmt/PlanCaseControls.vue";

const at = "2026-10-13T09:00:00Z";
const caseDTO = {
  preconditions: "",
  testData: "",
  audit: [],
  auditTruncated: false,
  id: "case",
  title: "Pay",
  suiteId: "",
  suiteName: "",
  externalKey: "",
  steps: [],
  expected: "Paid",
  priority: "p1",
  isCritical: false,
  automationStatus: "manual_only",
  ownerId: "",
  createdAt: at,
  updatedAt: at,
};
const item = {
  testCase: { ...caseDTO, links: [], requirements: [] },
  assigneeUserId: "",
  assigneeEmail: "",
  assignedBy: "",
  assignedAt: null,
  workStatus: "not_run",
} as PlanCase;
const plan: Plan = {
  id: "plan",
  name: "Release",
  environmentId: "",
  status: "draft",
  cases: [item],
  executions: [],
  executionHistory: [],
  historyTruncated: false,
  createdAt: at,
  updatedAt: at,
};

it("decodes membership and sends versioned scope/assignment changes with CSRF", async () => {
  const transport = vi.fn<Transport>(
    async () => new Response(JSON.stringify(plan)),
  );
  const client = new ApiClient(transport);
  client.csrfToken = "csrf";
  const repo = new TestmgmtRepository(client);
  const result = await repo.updatePlan(
    "project",
    "plan",
    {
      caseUpdate: { testCaseId: "case", assigneeEmail: "tester@example.com" },
      expectedUpdatedAt: at,
    },
    new AbortController().signal,
  );
  expect(result.cases[0]?.workStatus).toBe("not_run");
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe("/v1/projects/project/test-plans/plan");
  expect(init?.method).toBe("PATCH");
  expect(JSON.parse(String(init?.body))).toMatchObject({
    expectedUpdatedAt: at,
    caseUpdate: { assigneeEmail: "tester@example.com" },
  });
  expect((init?.headers as Record<string, string>)["X-CSRF-Token"]).toBe(
    "csrf",
  );
});

it("scope selections survive unrelated assignment updates and recorded cases cannot be removed", async () => {
  const repository = {
    testCases: vi.fn(async () => [
      { ...item.testCase },
      { ...item.testCase, id: "other", title: "Refund" },
    ]),
  };
  const wrapper = mount(PlanScopeEditor, {
    props: { project: "project", plan, disabled: false },
    global: { provide: { [servicesKey as symbol]: { testmgmt: repository } } },
  });
  await flushPromises();
  await wrapper.get('input[value="other"]').setValue(true);
  await wrapper.setProps({
    plan: {
      ...plan,
      updatedAt: "2026-10-13T10:00:00Z",
      cases: [{ ...item, assigneeEmail: "tester@example.com" }],
    },
  });
  expect(
    (wrapper.get('input[value="other"]').element as HTMLInputElement).checked,
  ).toBe(true);
  await wrapper
    .findAll("button")
    .find((button) => button.text().includes("Save scope"))!
    .trigger("click");
  expect(wrapper.emitted("save")?.[0]?.[0]).toEqual(["case", "other"]);
  await wrapper.setProps({
    plan: {
      ...plan,
      executions: [{ testCaseId: "case" }] as Plan["executions"],
    },
  });
  expect(
    wrapper.get('input[value="case"]').attributes("disabled"),
  ).toBeDefined();
});

it("assignment and start emit workflow intent without emitting a verdict", async () => {
  const wrapper = mount(PlanCaseControls, { props: { item, disabled: false } });
  await wrapper.get("input[type=email]").setValue("tester@example.com");
  await wrapper.get("form").trigger("submit");
  await wrapper
    .findAll("button")
    .find((button) => button.text() === "Start")!
    .trigger("click");
  expect(wrapper.emitted("update")).toEqual([
    [{ testCaseId: "case", assigneeEmail: "tester@example.com" }],
    [{ testCaseId: "case", workStatus: "in_progress" }],
  ]);
});
