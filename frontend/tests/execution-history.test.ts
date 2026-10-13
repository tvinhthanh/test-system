import { mount } from "@vue/test-utils";
import { expect, it } from "vitest";
import ExecutionRecorder from "../src/features/testmgmt/ExecutionRecorder.vue";
import ExecutionHistory from "../src/features/testmgmt/ExecutionHistory.vue";
import type {
  Execution,
  TestCase,
} from "../src/features/testmgmt/testmgmtRepository";

const at = "2026-10-13T09:00:00Z";
const testCase: TestCase = {
  preconditions: "Signed in",
  testData: "Card 4242",
  audit: [],
  auditTruncated: false,
  id: "case",
  title: "Checkout",
  suiteId: "",
  suiteName: "",
  externalKey: "",
  steps: [{ action: "Pay", expected: "Receipt" }],
  expected: "Paid",
  priority: "p1",
  isCritical: false,
  automationStatus: "manual_only",
  ownerId: "",
  links: [],
  requirements: [],
  createdAt: at,
  updatedAt: at,
};
const execution: Execution = {
  id: "first",
  planId: "plan",
  testCaseId: "case",
  caseTitle: "Original checkout",
  status: "failed",
  executedBy: "alice",
  note: "Missing receipt",
  executedAt: at,
  caseSnapshot: {
    preconditions: "Original signed in",
    testData: "Original card",
    title: "Original checkout",
    steps: [{ action: "Original pay", expected: "Original receipt" }],
    expected: "Paid",
    updatedAt: at,
  },
  stepResults: [{ stepIndex: 0, status: "failed", actual: "Blank" }],
  bugUrl: "https://bugs.example/1",
  retestOf: "",
  evidence: [
    {
      id: "proof",
      name: "Original proof.png",
      kind: "manual_evidence",
      contentType: "image/png",
      sizeBytes: 2,
    },
  ],
};

it("records step results and explicitly links a retest without changing the previous execution", async () => {
  const wrapper = mount(ExecutionRecorder, {
    props: { testCase, latest: execution, disabled: false },
  });
  await wrapper.get("input[type=checkbox]").setValue(true);
  await wrapper.get("select").setValue("passed");
  await wrapper
    .get('input[placeholder="Actual result"]')
    .setValue("Receipt shown");
  await wrapper.get("input[type=url]").setValue("https://bugs.example/1");
  await wrapper.findAll("button")[0]!.trigger("click");
  expect(wrapper.emitted("record")?.[0]?.[0]).toMatchObject({
    status: "passed",
    retestOf: "first",
    bugUrl: "https://bugs.example/1",
    stepResults: [{ stepIndex: 0, status: "passed", actual: "Receipt shown" }],
  });
  expect(execution.status).toBe("failed");
});

it("refuses unsafe bug URLs and does not submit disabled executions", async () => {
  const wrapper = mount(ExecutionRecorder, {
    props: { testCase, disabled: false },
  });
  await wrapper.get("input[type=url]").setValue("javascript:alert(1)");
  expect(wrapper.get("[role=alert]").text()).toContain("HTTP");
  expect(
    wrapper
      .findAll("button")
      .every((button) => button.attributes("disabled") !== undefined),
  ).toBe(true);
  await wrapper.get("input[type=url]").setValue("");
  await wrapper.setProps({ disabled: true });
  await wrapper.findAll("button")[0]!.trigger("click");
  expect(wrapper.emitted("record")).toBeUndefined();
});

it("does not remap unsaved step results when a plan refresh brings a newer case", async () => {
  const wrapper = mount(ExecutionRecorder, {
    props: { testCase, disabled: false },
  });
  await wrapper.get("select").setValue("passed");
  await wrapper
    .get('input[placeholder="Actual result"]')
    .setValue("Original actual");
  await wrapper.setProps({
    testCase: {
      ...testCase,
      updatedAt: "2026-10-13T10:00:00Z",
      steps: [{ action: "New action", expected: "New expected" }],
    },
  });
  expect(wrapper.text()).toContain("Case changed");
  expect(wrapper.text()).not.toContain("New action");
  expect(
    wrapper
      .findAll("button")
      .find((button) => button.text() === "passed")!
      .attributes("disabled"),
  ).toBeDefined();
  await wrapper
    .findAll("button")
    .find((button) => button.text() === "Reload case")!
    .trigger("click");
  expect(wrapper.text()).toContain("New action");
  expect(
    (
      wrapper.get('input[placeholder="Actual result"]')
        .element as HTMLInputElement
    ).value,
  ).toBe("");
});

it("shows the original case, step results and evidence, and labels legacy missing snapshots", () => {
  const wrapper = mount(ExecutionHistory, {
    props: {
      executions: [
        execution,
        { ...execution, id: "legacy", caseSnapshot: null, stepResults: [] },
      ],
      contentUrl: (id) => `/v1/artifacts/${id}/content`,
    },
  });
  expect(wrapper.text()).toContain("Original pay");
  expect(wrapper.text()).toContain("Actual: Blank");
  expect(wrapper.text()).toContain(
    "Legacy execution: case snapshot unavailable.",
  );
  expect(wrapper.find('a[href="/v1/artifacts/proof/content"]').text()).toBe(
    "Original proof.png",
  );
});
