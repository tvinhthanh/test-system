import { computed, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useResource } from "../../core/state/useResource";
import type { Page } from "../../core/api/decode";
import type { Project } from "./reportsRepository";
import { useServices } from "../../app/providers/services";

export function useProjects() {
  const route = useRoute(),
    router = useRouter();
  const { projectAccess } = useServices();
  const resource = useResource<Page<Project>>();
  const cursor = computed(() =>
    typeof route.query.cursor === "string" ? route.query.cursor : "",
  );
  function refresh() {
    return resource.load((signal) => projectAccess.page(cursor.value, signal));
  }
  function next() {
    if (resource.data.value?.nextCursor)
      void router.push({ query: { cursor: resource.data.value.nextCursor } });
  }
  function first() {
    void router.push({ query: {} });
  }
  watch(cursor, refresh, { immediate: true });
  return { ...resource, cursor, refresh, next, first };
}
