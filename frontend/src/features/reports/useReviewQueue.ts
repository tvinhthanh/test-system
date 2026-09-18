import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { ReviewItem } from "./reportsRepository";

// §9.4 review queue: pairs the resolver would not decide alone. The server
// owns the decision (merge or keep, audited); the UI shows the two sides, the
// score and the gap, and asks. An admin decides; anyone with triage rights
// may look. Decision errors (403 for a member, 409 for an already-decided
// item) are shown per row, never swallowed.
export function useReviewQueue() {
  const route = useRoute();
  const { reports } = useServices();
  const project = computed(() => String(route.params.project));
  const queue = useResource<ReviewItem[]>();
  const busy = ref<string | null>(null); // item id being decided
  const rowError = ref<{ id: string; error: unknown } | null>(null);
  function refresh() {
    return queue.load((signal) => reports.reviewQueue(project.value, signal));
  }
  watch(project, refresh, { immediate: true });
  async function decide(item: ReviewItem, decision: "merged" | "kept") {
    if (busy.value) return;
    busy.value = item.id;
    rowError.value = null;
    try {
      await reports.decideReview(
        project.value,
        item.id,
        decision,
        new AbortController().signal,
      );
      await refresh();
    } catch (error) {
      rowError.value = { id: item.id, error };
    } finally {
      busy.value = null;
    }
  }
  return { ...queue, project, busy, rowError, refresh, decide };
}
