import { mount } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { TestmgmtRepository } from "../src/features/testmgmt/testmgmtRepository";
import CaseContextEditor from "../src/features/testmgmt/CaseContextEditor.vue";
import CaseChangeHistory from "../src/features/testmgmt/CaseChangeHistory.vue";

const at = "2026-10-13T09:00:00Z";
const testCase = {
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
  preconditions: "Signed in",
  testData: "account=42",
  audit: [
    {
      id: "change",
      actor: "tester@example.com",
      action: "case.update",
      changedFields: ["preconditions", "test_data"],
      createdAt: at,
    },
  ],
  auditTruncated: false,
};

it("decodes case context and real audit, and sends optimistic edits", async () => {
  const transport = vi.fn<Transport>(
    async () => new Response(JSON.stringify(testCase)),
  );
  const client = new ApiClient(transport);
  client.csrfToken = "csrf";
  const repo = new TestmgmtRepository(client);
  const result = await repo.updateTestCase(
    "project",
    "case",
    { preconditions: "New setup", testData: "new=99", expectedUpdatedAt: at },
    new AbortController().signal,
  );
  expect(result.testData).toBe("account=42");
  expect(result.audit[0]?.actor).toBe("tester@example.com");
  expect(JSON.parse(String(transport.mock.calls[0]?.[1]?.body))).toMatchObject({
    preconditions: "New setup",
    expectedUpdatedAt: at,
  });
});

it("enforces UTF-8 byte limits and keeps context as literal text", async () => {
  const wrapper = mount(CaseContextEditor, {
    props: { preconditions: "", testData: "" },
  });
  const data = wrapper.findAll("textarea")[1]!;
  const value = "\u00e9".repeat(8001);
  await data.setValue(value);
  expect((data.element as HTMLTextAreaElement).validity.valid).toBe(false);
  expect(wrapper.emitted("update:testData")?.[0]?.[0]).toBe(value);
  await wrapper.setProps({ testData: '<script>alert("x")</script>' });
  expect(wrapper.find("script").exists()).toBe(false);
});

it("shows bounded case history without interpreting actor or field text as HTML", () => {
  const wrapper = mount(CaseChangeHistory, {
    props: {
      changes: [
        {
          ...testCase.audit[0]!,
          action: "case.update",
          actor: "<img src=x>",
          changedFields: ["test_data"],
        },
      ],
      truncated: true,
    },
  });
  expect(wrapper.text()).toContain("Latest 50 changes");
  expect(wrapper.text()).toContain("test data");
  expect(wrapper.text()).toContain("<img src=x>");
  expect(wrapper.find("img").exists()).toBe(false);
});
