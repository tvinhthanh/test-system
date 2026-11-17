import { mount } from "@vue/test-utils";
import { defineComponent } from "vue";
import { expect, it, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { servicesKey, type Services } from "../src/app/providers/services";
import {
  TestmgmtRepository,
  type TestCase,
} from "../src/features/testmgmt/testmgmtRepository";
import { useCaseBulkActions } from "../src/features/testmgmt/useCaseBulkActions";

const at = "2026-10-13T09:00:00Z";
const signal = new AbortController().signal;

function repository(body: unknown) {
  const transport = vi.fn<Transport>(
    async () => new Response(JSON.stringify(body)),
  );
  const client = new ApiClient(transport);
  client.csrfToken = "csrf";
  return { repo: new TestmgmtRepository(client), transport };
}

it("sends versioned bulk targets and decodes individual conflicts", async () => {
  const items = [
    { id: "a", updated: false, code: "CONFLICT" },
    { id: "b", updated: true, code: "" },
  ];
  const { repo, transport } = repository({ items });
  expect(
    await repo.bulkUpdateCases(
      "project",
      [
        { id: "a", expectedUpdatedAt: at },
        { id: "b", expectedUpdatedAt: at },
      ],
      { priority: "p0" },
      signal,
    ),
  ).toEqual(items);
  expect(JSON.parse(String(transport.mock.calls[0]?.[1]?.body))).toMatchObject({
    patch: { priority: "p0" },
    items: [
      { id: "a", expectedUpdatedAt: at },
      { id: "b", expectedUpdatedAt: at },
    ],
  });
});

it.each([
  { items: [{ id: "wrong", updated: true, code: "" }] },
  { items: [{ id: "a", updated: true, code: "CONFLICT" }] },
  { items: [] },
])("rejects incomplete or inconsistent bulk responses", async ({ items }) => {
  const { repo } = repository({ items });
  await expect(
    repo.bulkUpdateCases(
      "project",
      [{ id: "a", expectedUpdatedAt: at }],
      { priority: "p0" },
      signal,
    ),
  ).rejects.toThrow();
});

it("exports all filter matches without applying the list limit or selection", async () => {
  const { repo, transport } = repository({
    csv: "title\nPay\n",
    count: 250,
    exportedAt: at,
  });
  expect(
    (
      await repo.exportCases(
        "project",
        { q: "Pay", automation: "automation_candidate" },
        undefined,
        signal,
      )
    ).count,
  ).toBe(250);
  const url = new URL(String(transport.mock.calls[0]?.[0]), "http://localhost");
  expect(url.searchParams.get("q")).toBe("Pay");
  expect(url.searchParams.get("automation")).toBe("automation_candidate");
  expect(url.searchParams.has("limit")).toBe(false);
  expect(url.searchParams.has("ids")).toBe(false);
});

it("stops after an uncertain batch without retrying or claiming later cases were changed", async () => {
  const bulkUpdateCases = vi
    .fn()
    .mockImplementationOnce(async (_project, targets) =>
      targets.map((target: { id: string }) => ({
        id: target.id,
        updated: true,
        code: "",
      })),
    )
    .mockRejectedValueOnce(new Error("Connection lost"));
  let actions!: ReturnType<typeof useCaseBulkActions>;
  const wrapper = mount(
    defineComponent({
      setup() {
        actions = useCaseBulkActions();
        return () => null;
      },
    }),
    {
      global: {
        provide: {
          [servicesKey as symbol]: {
            testmgmt: { bulkUpdateCases },
          } as unknown as Services,
        },
      },
    },
  );
  const cases = Array.from(
    { length: 250 },
    (_, i) =>
      ({ id: String(i), title: `Case ${i}`, updatedAt: at }) as TestCase,
  );
  const successful = await actions.apply("project", cases, { priority: "p0" });
  expect(bulkUpdateCases).toHaveBeenCalledTimes(2);
  expect(successful).toHaveLength(100);
  expect(
    actions.results.value.filter((item) => item.code === "UNCONFIRMED"),
  ).toHaveLength(100);
  expect(
    actions.results.value.filter((item) => item.code === "NOT_SENT"),
  ).toHaveLength(50);
  expect(actions.busy.value).toBe(false);
  wrapper.unmount();
});
