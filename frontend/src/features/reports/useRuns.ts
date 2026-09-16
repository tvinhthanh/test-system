import { computed, onScopeDispose, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Page } from "../../core/api/decode";
import type { Run, RunsQuery } from "./reportsRepository";

export function useRuns() {
  const route = useRoute(),
    router = useRouter();
  const { reports } = useServices();
  const scalar = (value: unknown) => (typeof value === "string" ? value : "");
  const project = computed(() => scalar(route.params.project));
  const query = computed<RunsQuery>(() => ({
    q: scalar(route.query.q),
    status: scalar(route.query.status),
    environment: scalar(route.query.environment),
    cursor: scalar(route.query.cursor),
  }));
  const resource = useResource<Page<Run>>();
  const search = ref(query.value.q);
  let debounce: ReturnType<typeof setTimeout> | undefined;
  function refresh() {
    return resource.load((signal) =>
      reports.runs(project.value, query.value, signal),
    );
  }
  function filter(values: Partial<RunsQuery>) {
    return router.push({
      query: { ...query.value, ...values, cursor: undefined },
    });
  }
  watch(
    () => query.value.q,
    (value) => {
      clearTimeout(debounce);
      search.value = value;
    },
  );
  watch(search, (value) => {
    clearTimeout(debounce);
    if (value === query.value.q) return;
    debounce = setTimeout(() => {
      void filter({ q: value });
    }, 300);
  });
  watch(() => [project.value, ...Object.values(query.value)], refresh, {
    immediate: true,
  });
  onScopeDispose(() => clearTimeout(debounce));
  const filtered = computed(
    () => !!(query.value.q || query.value.status || query.value.environment),
  );
  function next() {
    if (resource.data.value?.nextCursor)
      void router.push({
        query: { ...route.query, cursor: resource.data.value.nextCursor },
      });
  }
  function first() {
    void router.push({ query: { ...route.query, cursor: undefined } });
  }
  function clear() {
    clearTimeout(debounce);
    void router.push({ query: {} });
  }
  return {
    ...resource,
    search,
    query,
    project,
    filtered,
    filter,
    refresh,
    next,
    first,
    clear,
  };
}
