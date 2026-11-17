import { mount } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import CaseTaxonomyEditor from "../src/features/testmgmt/CaseTaxonomyEditor.vue";
import {
  copyTaxonomy,
  taxonomySummary,
} from "../src/features/testmgmt/taxonomy";
import { ApiClient, type Transport } from "../src/core/api/client";
import { TestmgmtRepository } from "../src/features/testmgmt/testmgmtRepository";

it("keeps dimensions independent and removes subtypes when Performance is removed", async () => {
  const value = {
    ...copyTaxonomy(),
    purposes: ["functional", "performance"] as ("functional" | "performance")[],
    performanceTypes: ["stress"] as "stress"[],
  };
  const wrapper = mount(CaseTaxonomyEditor, { props: { modelValue: value } });
  await wrapper
    .get('input[aria-label="Execution modes: Automated"]')
    .setValue(true);
  const updated = wrapper
    .emitted("update:modelValue")!
    .at(-1)![0] as typeof value;
  expect(updated.executionModes).toEqual(["automated"]);
  expect(updated.performanceTypes).toEqual(["stress"]);
  expect(value.executionModes).toEqual([]);
  await wrapper
    .get('input[aria-label="Purposes: Performance"]')
    .setValue(false);
  const cleared = wrapper
    .emitted("update:modelValue")!
    .at(-1)![0] as typeof value;
  expect(cleared.performanceTypes).toEqual([]);
  expect(cleared.purposes).toEqual(["functional"]);
});

it("shows legacy absence separately from deliberately unclassified snapshots", () => {
  expect(taxonomySummary(null)).toBe("Classification unavailable");
  expect(taxonomySummary(copyTaxonomy())).toBe("Unclassified");
  expect(
    taxonomySummary({ ...copyTaxonomy(), tags: ["e2e", "regression"] }),
  ).toBe("E2E / Regression");
});

it("forwards taxonomy filters identically to list and export", async () => {
  const transport = vi.fn<Transport>(
    async (url) =>
      new Response(
        JSON.stringify(
          String(url).includes("/export?")
            ? {
                csv: "title\nPay",
                count: 1,
                exportedAt: "2026-10-15T08:00:00Z",
              }
            : { items: [], nextCursor: null },
        ),
      ),
  );
  const repo = new TestmgmtRepository(new ApiClient(transport));
  const filter = {
    level: "system",
    purpose: "performance",
    tag: "e2e",
    executionMode: "automated",
    performanceType: "stress",
  };
  const signal = new AbortController().signal;
  await repo.testCases("p", filter, signal);
  await repo.exportCases("p", filter, undefined, signal);
  for (const call of transport.mock.calls) {
    const query = new URL(String(call[0]), "http://localhost").searchParams;
    for (const [key, value] of Object.entries(filter))
      expect(query.get(key)).toBe(value);
  }
});
