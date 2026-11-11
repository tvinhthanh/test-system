import { computed, onScopeDispose, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Investigation, ResultDetail } from "./reportsRepository";

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
  const investigations = ref<Investigation[]>([]),
    investigationBusy = ref(false),
    investigationError = ref<unknown>(null);
  const seen = ref(false);
  let disposed = false;
  function refresh() {
    return resource.load(async (signal) => {
      const [detail, items] = await Promise.all([
        reports.result(project.value, resultId.value, signal),
        reports.investigations(project.value, resultId.value, signal),
      ]);
      investigations.value = items;
      return detail;
    });
  }
  async function investigate() {
    if (investigationBusy.value || disposed) return;
    investigationBusy.value = true;
    investigationError.value = null;
    try {
      await reports.startInvestigation(
        project.value,
        resultId.value,
        new AbortController().signal,
      );
      await refresh();
    } catch (error) {
      investigationError.value = error;
    } finally {
      if (!disposed) investigationBusy.value = false;
    }
  }
  async function decide(decision: "accept" | "reject") {
    const h = investigations.value[0]?.hypothesis;
    if (!h || investigationBusy.value) return;
    investigationBusy.value = true;
    investigationError.value = null;
    try {
      await reports.decideHypothesis(
        project.value,
        resultId.value,
        h.id,
        decision,
        new AbortController().signal,
      );
      await refresh();
    } catch (error) {
      investigationError.value = error;
    } finally {
      if (!disposed) investigationBusy.value = false;
    }
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
  // Same-origin URL for an artifact's bytes — the repository owns the path.
  const artifactUrl = (artifactId: string) =>
    reports.artifactContentUrl(project.value, artifactId);
  const traceUrl = (artifactId: string) =>
    reports.traceViewerUrl(window.location.origin, project.value, artifactId);
  return {
    ...resource,
    project,
    resultId,
    actionBusy,
    actionError,
    refresh,
    emit,
    artifactUrl,
    traceUrl,
    investigations,
    investigationBusy,
    investigationError,
    investigate,
    decide,
  };
}
