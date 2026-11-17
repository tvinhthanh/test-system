import { mount } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { TestmgmtRepository } from "../src/features/testmgmt/testmgmtRepository";
import SpecializedResultForm from "../src/features/testmgmt/SpecializedResultForm.vue";
import ReportFieldsEditor from "../src/features/testmgmt/ReportFieldsEditor.vue";
import {
  emptyReportData,
  reportFields,
  reportKinds,
  readSpecializedResult,
  type SpecializedInput,
} from "../src/features/testmgmt/specializedReports";
import { tone } from "../src/shared/format";

const at = "2026-10-13T09:00:00Z";
function input(
  kind: SpecializedInput["kind"] = "performance",
): SpecializedInput {
  return {
    schemaVersion: 1,
    idempotencyKey: "fixture",
    kind,
    title: "Synthetic report",
    state: "completed",
    testCaseId: "",
    notes: "",
    environment: "fixture",
    applicationVersion: "1",
    source: "fixture",
    toolVersion: "1",
    adapterVersion: "atp-v1",
    startedAt: at,
    finishedAt: at,
    retestOf: "",
    originalReport: "",
    details: emptyReportData(reportFields[kind]),
  };
}
function result(kind: SpecializedInput["kind"] = "performance") {
  return {
    id: "result",
    kind,
    title: "Synthetic report",
    outcome: "inconclusive",
    reasons: ["Missing evidence"],
    input: input(kind),
    createdBy: "tester",
    createdAt: at,
  };
}

for (const kind of reportKinds) {
  it(`renders and decodes ${kind} without deriving a passed verdict`, async () => {
    const value = input(kind);
    const wrapper = mount(SpecializedResultForm, {
      props: { initial: value, disabled: false },
    });
    expect(wrapper.get('[aria-label="Testing group"]').element).toHaveProperty(
      "value",
      kind,
    );
    const field = reportFields[kind][0]!;
    expect(wrapper.text()).toContain(field.label);
    expect(readSpecializedResult(result(kind)).outcome).toBe("inconclusive");
    await wrapper.get('[aria-label="Report title"]').setValue("Changed");
    await wrapper.get("form").trigger("submit");
    expect(wrapper.emitted("submit")![0]![0]).toMatchObject({
      title: "Changed",
      idempotencyKey: "fixture",
      kind,
    });
    expect(value.title).toBe("Synthetic report");
    wrapper.unmount();
  });
}

it("resets typed details on group change and preserves metadata after disabling", async () => {
  const wrapper = mount(SpecializedResultForm, {
    props: { initial: input(), disabled: false },
  });
  await wrapper.get('[aria-label="Latency p95 (ms)"]').setValue("125");
  await wrapper.get('[aria-label="Testing group"]').setValue("security");
  expect(wrapper.find('[aria-label="Latency p95 (ms)"]').exists()).toBe(false);
  await wrapper.get('[aria-label="Execution state"]').setValue("import_error");
  expect(
    wrapper.get('[aria-label="Original report"]').attributes(),
  ).toHaveProperty("required");
  await wrapper.setProps({ disabled: true });
  expect(wrapper.get('button[type="submit"]').attributes()).toHaveProperty(
    "disabled",
  );
});

it("adds nested findings immutably and renders untrusted evidence as text", async () => {
  const value = emptyReportData(reportFields.security);
  const wrapper = mount(ReportFieldsEditor, {
    props: { fields: reportFields.security, modelValue: value },
  });
  await wrapper.get("button").trigger("click");
  const updated = wrapper.emitted("update:modelValue")![0]![0] as typeof value;
  expect(value.findings).toEqual([]);
  expect(updated.findings).toHaveLength(1);
  await wrapper.setProps({ modelValue: updated });
  await wrapper
    .get('[aria-label="Evidence"]')
    .setValue("<script>alert(1)</script>");
  expect(wrapper.find("script").exists()).toBe(false);
  await wrapper.setProps({ disabled: true });
  expect(wrapper.find("button").exists()).toBe(false);
});

it("accepts bounded summaries but rejects invalid metrics and mismatched groups", () => {
  expect(
    readSpecializedResult({ ...result(), input: { ...input(), details: null } })
      .input.details,
  ).toEqual({});
  expect(() =>
    readSpecializedResult({ ...result(), kind: "security" }),
  ).toThrow();
  expect(() =>
    readSpecializedResult({
      ...result(),
      input: { ...input(), details: { ...input().details, p95Ms: -1 } },
    }),
  ).toThrow();
  expect(tone("inconclusive")).toBe("warning");
  expect(tone("import_error")).toBe("danger");
});

it("scopes list/create/original requests, preserves opaque cursors and sends CSRF", async () => {
  const transport = vi.fn<Transport>(
    async (_url, init) =>
      new Response(
        JSON.stringify(
          init?.method === "POST"
            ? result()
            : { items: [result()], nextCursor: "opaque+/=" },
        ),
      ),
  );
  const client = new ApiClient(transport);
  client.csrfToken = "csrf";
  const repo = new TestmgmtRepository(client),
    signal = new AbortController().signal;
  const page = await repo.specializedResults(
    "project",
    { kind: "performance", outcome: "inconclusive", cursor: "opaque+/=" },
    signal,
  );
  expect(page.nextCursor).toBe("opaque+/=");
  const query = new URL(String(transport.mock.calls[0]![0]), "http://localhost")
    .searchParams;
  expect(query.get("cursor")).toBe("opaque+/=");
  expect(query.get("kind")).toBe("performance");
  await repo.createSpecializedResult("project", input(), signal);
  expect(transport.mock.calls[1]![0]).toBe(
    "/v1/projects/project/specialized-results",
  );
  expect(
    (transport.mock.calls[1]![1]?.headers as Record<string, string>)[
      "X-CSRF-Token"
    ],
  ).toBe("csrf");
  expect(repo.specializedOriginalUrl("project", "id/part")).toBe(
    "/v1/projects/project/specialized-results/id%2Fpart/original",
  );
});
