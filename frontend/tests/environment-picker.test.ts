import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter, RouterView } from "vue-router";
import { expect, it, vi } from "vitest";
import { servicesKey } from "../src/app/providers/services";
import AutoTestPage from "../src/features/autotest/AutoTestPage.vue";
import EnvironmentPicker from "../src/features/testmgmt/EnvironmentPicker.vue";

const environment = {
  id: "env-1",
  key: "staging",
  baseUrl: "https://staging.example.com",
  createdAt: "2026-10-30T00:00:00Z",
  updatedAt: "2026-10-30T00:00:00Z",
};

it("clears the selected site while adding a new environment and keeps the add fields open", async () => {
  const testmgmt = { environments: vi.fn(async () => [environment]) };
  const wrapper = mount(EnvironmentPicker, {
    props: { project: "project", modelValue: environment.id, canWrite: true },
    global: { provide: { [servicesKey as symbol]: { testmgmt } } },
  });
  await flushPromises();
  expect(testmgmt.environments).toHaveBeenCalledTimes(1);
  expect(wrapper.emitted("loaded")?.[0]?.[0]).toEqual([environment]);

  await wrapper.get("select").setValue("__new");
  expect(wrapper.emitted("update:modelValue")?.at(-1)?.[0]).toBe("");
  await wrapper.setProps({ modelValue: "" });
  expect(wrapper.find("#env-key").exists()).toBe(true);
  expect((wrapper.get("select").element as HTMLSelectElement).value).toBe(
    "__new",
  );
});

it("offers retry when environments fail to load", async () => {
  const testmgmt = {
    environments: vi
      .fn()
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce([environment]),
  };
  const wrapper = mount(EnvironmentPicker, {
    props: { project: "project", modelValue: "", canWrite: true },
    global: { provide: { [servicesKey as symbol]: { testmgmt } } },
  });
  await flushPromises();
  expect(wrapper.text()).toContain("Try again");
  await wrapper
    .findAll("button")
    .find((button) => button.text().includes("Try again"))!
    .trigger("click");
  await flushPromises();
  expect(testmgmt.environments).toHaveBeenCalledTimes(2);
  expect(wrapper.get("select").text()).toContain("staging");
});

it("loads auto test environments once and keeps tabs in sync with URL navigation", async () => {
  const other = {
    ...environment,
    id: "env-other",
    baseUrl: "https://other.example.test",
  };
  const testmgmt = {
    environments: vi.fn(async (project: string) =>
      project === "other" ? [other] : [environment],
    ),
    aiStatus: vi.fn(async () => ({
      available: false,
      model: "",
      source: "",
      readsPages: false,
    })),
  };
  const autotest = {
    specs: vi.fn(async () => ({ items: [], enabled: true })),
    runs: vi.fn(async () => ({ items: [], enabled: true })),
    scans: vi.fn(async () => ({ items: [], enabled: true })),
    accounts: vi.fn(async () => []),
  };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/p/:project/auto", component: AutoTestPage }],
  });
  await router.push("/p/project/auto?tab=cases");
  await router.isReady();
  const wrapper = mount(RouterView, {
    global: {
      plugins: [router],
      provide: {
        [servicesKey as symbol]: {
          testmgmt,
          autotest,
          projectAccess: { can: () => true },
        },
      },
    },
  });
  await flushPromises();
  expect(testmgmt.environments).toHaveBeenCalledTimes(1);
  const tab = (name: string) =>
    wrapper.findAll('[role="tab"]').find((item) => item.text() === name)!;
  await tab("Runs").trigger("click");
  await flushPromises();
  expect(router.currentRoute.value.query.tab).toBe("runs");
  expect(tab("Runs").attributes("aria-selected")).toBe("true");

  await router.push("/p/project/auto?tab=scan");
  await flushPromises();
  expect(tab("Site scan").attributes("aria-selected")).toBe("true");
  router.back();
  await flushPromises();
  expect(router.currentRoute.value.query.tab).toBe("runs");
  expect(tab("Runs").attributes("aria-selected")).toBe("true");

  await router.push("/p/other/auto?tab=scan");
  await flushPromises();
  expect(testmgmt.environments).toHaveBeenCalledTimes(2);
  expect(
    (wrapper.get("#auto-environment").element as HTMLSelectElement).value,
  ).toBe(other.id);
  expect((wrapper.get("#scan-url").element as HTMLInputElement).value).toBe(
    other.baseUrl,
  );
  wrapper.unmount();
});
