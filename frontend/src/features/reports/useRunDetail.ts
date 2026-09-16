import { computed, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Page } from "../../core/api/decode";
import type { Run, TestResult } from "./reportsRepository";

export function useRunDetail() {
  const route = useRoute(),
    router = useRouter();
  const { reports } = useServices();
  const project = computed(() => String(route.params.project));
  const id = computed(() => String(route.params.run));
  const cursor = computed(() =>
    typeof route.query.cursor === "string" ? route.query.cursor : "",
  );
  const run = useResource<Run>();
  const results = useResource<Page<TestResult>>();
  function loadRun() {
    return run.load((signal) => reports.run(project.value, id.value, signal));
  }
  function loadResults() {
    return results.load((signal) =>
      reports.results(project.value, id.value, cursor.value, signal),
    );
  }
  watch(() => [project.value, id.value], loadRun, { immediate: true });
  watch(() => [project.value, id.value, cursor.value], loadResults, {
    immediate: true,
  });
  function next() {
    if (results.data.value?.nextCursor)
      void router.push({ query: { cursor: results.data.value.nextCursor } });
  }
  function first() {
    void router.push({ query: {} });
  }
  return {
    project,
    id,
    cursor,
    run,
    results,
    loadRun,
    loadResults,
    next,
    first,
  };
}
