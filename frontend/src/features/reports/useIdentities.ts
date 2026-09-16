import { computed, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Identity } from "./reportsRepository";
import type { Page } from "../../core/api/decode";

export function useIdentities() {
  const route = useRoute(),
    router = useRouter();
  const { reports } = useServices();
  const project = computed(() => String(route.params.project));
  const flaky = computed(() => route.path.endsWith("/flaky"));
  const cursor = computed(() =>
    typeof route.query.cursor === "string" ? route.query.cursor : "",
  );
  const resource = useResource<Page<Identity>>();
  function refresh() {
    return resource.load((signal) =>
      reports.identities(
        project.value,
        flaky.value ? "flaky" : "",
        cursor.value,
        signal,
      ),
    );
  }
  watch(() => [project.value, flaky.value, cursor.value], refresh, {
    immediate: true,
  });
  function next() {
    if (resource.data.value?.nextCursor)
      void router.push({ query: { cursor: resource.data.value.nextCursor } });
  }
  function first() {
    void router.push({ query: {} });
  }
  return { ...resource, project, flaky, cursor, refresh, next, first };
}
