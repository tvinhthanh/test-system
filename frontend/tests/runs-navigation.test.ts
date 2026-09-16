import { defineComponent, h } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import { createMemoryHistory, createRouter, RouterView } from "vue-router";
import { expect, it, vi } from "vitest";
import { useRuns } from "../src/features/reports/useRuns";
import { ApiClient } from "../src/core/api/client";
import { ReportsRepository } from "../src/features/reports/reportsRepository";
import { servicesKey } from "../src/app/providers/services";

it("clear filters does not enqueue a debounce that navigates back after opening detail", async () => {
  vi.useFakeTimers();
  let model!: ReturnType<typeof useRuns>;
  const list = defineComponent({
    setup() {
      model = useRuns();
      return () => h("div", "runs");
    },
  });
  const detail = defineComponent({ render: () => h("div", "detail") });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/p/:project/runs", component: list },
      { path: "/p/:project/runs/:run", component: detail },
    ],
  });
  await router.push("/p/one/runs?q=old");
  await router.isReady();
  const reports = new ReportsRepository(
    new ApiClient(
      async () => new Response(JSON.stringify({ items: [], nextCursor: null })),
    ),
  );
  const wrapper = mount(RouterView, {
    global: {
      plugins: [router],
      provide: { [servicesKey as symbol]: { reports } },
    },
  });
  await flushPromises();
  model.clear();
  await flushPromises();
  await router.push("/p/one/runs/run-1");
  await vi.advanceTimersByTimeAsync(500);
  expect(router.currentRoute.value.path).toBe("/p/one/runs/run-1");
  wrapper.unmount();
  vi.useRealTimers();
});
