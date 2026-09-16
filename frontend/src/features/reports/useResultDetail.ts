import { computed, onScopeDispose, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { ResultDetail } from "./reportsRepository";

const triageTypes = new Set([
  "HYPOTHESIS_ACCEPTED",
  "HYPOTHESIS_REJECTED",
  "MARKED_FLAKY",
  "MARKED_KNOWN_ISSUE",
  "ASSIGNED",
  "RESOLVED",
]);
export function useResultDetail() {
  const route = useRoute();
  const { reports } = useServices();
  const project = computed(() => String(route.params.project));
  const resultId = computed(() => String(route.params.result));
  const resource = useResource<ResultDetail>();
  const actionBusy = ref(false),
    actionError = ref<unknown>(null);
  const seen = ref(false);
  let disposed = false;
  function refresh() {
    return resource.load((signal) =>
      reports.result(project.value, resultId.value, signal),
    );
  }
  async function emit(type: string) {
    if (!triageTypes.has(type) && type !== "FAILURE_SEEN") return;
    if (actionBusy.value || disposed) return;
    actionBusy.value = true;
    actionError.value = null;
    try {
      await reports.triage(
        project.value,
        resultId.value,
        type,
        new AbortController().signal,
      );
      if (type === "FAILURE_SEEN") seen.value = true;
      await refresh();
    } catch (error) {
      actionError.value = error;
    } finally {
      if (!disposed) actionBusy.value = false;
    }
  }
  watch(
    () => [project.value, resultId.value],
    async () => {
      await refresh();
      if (
        !disposed &&
        !seen.value &&
        resource.data.value &&
        ["failed", "timed_out", "interrupted", "flaky_pass"].includes(
          resource.data.value.result.finalStatus,
        )
      )
        await emit("FAILURE_SEEN");
    },
    { immediate: true },
  );
  onScopeDispose(() => {
    disposed = true;
  });
  return {
    ...resource,
    project,
    resultId,
    actionBusy,
    actionError,
    refresh,
    emit,
  };
}
