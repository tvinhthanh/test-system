import { computed, onBeforeUnmount, ref } from "vue";
import { ApiError } from "../../core/api/client";
import { useServices } from "../../app/providers/services";
import type {
  BulkCasePatch,
  BulkCaseResult,
  CaseFilter,
  TestCase,
} from "./testmgmtRepository";

export function useCaseBulkActions() {
  const { testmgmt } = useServices();
  const busy = ref(false);
  const error = ref<unknown>(null);
  const results = ref<(BulkCaseResult & { title: string })[]>([]);
  const total = ref(0);
  const exported = ref<number | null>(null);
  const updated = computed(
    () => results.value.filter((item) => item.updated).length,
  );
  let controller: AbortController | null = null;
  let disposed = false;
  onBeforeUnmount(() => {
    disposed = true;
    controller?.abort();
  });

  async function apply(
    project: string,
    cases: TestCase[],
    patch: BulkCasePatch,
  ) {
    if (busy.value || !cases.length) return [];
    busy.value = true;
    error.value = null;
    results.value = [];
    total.value = cases.length;
    controller = new AbortController();
    const targets = cases.map((item) => ({
      id: item.id,
      expectedUpdatedAt: item.updatedAt,
      title: item.title,
    }));
    try {
      for (let offset = 0; offset < targets.length; offset += 100) {
        const batch = targets.slice(offset, offset + 100);
        try {
          const response = await testmgmt.bulkUpdateCases(
            project,
            batch.map(({ id, expectedUpdatedAt }) => ({
              id,
              expectedUpdatedAt,
            })),
            patch,
            controller.signal,
          );
          if (disposed) return [];
          results.value.push(
            ...response.map((item, index) => ({
              ...item,
              title: batch[index]!.title,
            })),
          );
        } catch (failure) {
          if (disposed) return [];
          error.value = failure;
          const code =
            failure instanceof ApiError &&
            [400, 401, 403, 404].includes(failure.status)
              ? "REJECTED"
              : "UNCONFIRMED";
          results.value.push(
            ...batch.map((item) => ({
              id: item.id,
              title: item.title,
              updated: false,
              code,
            })),
          );
          results.value.push(
            ...targets.slice(offset + 100).map((item) => ({
              id: item.id,
              title: item.title,
              updated: false,
              code: "NOT_SENT",
            })),
          );
          break;
        }
      }
      return results.value
        .filter((item) => item.updated)
        .map((item) => item.id);
    } finally {
      busy.value = false;
      controller = null;
    }
  }

  async function exportCsv(
    project: string,
    filter: CaseFilter,
    ids?: string[],
  ) {
    if (busy.value) return;
    busy.value = true;
    error.value = null;
    exported.value = null;
    controller = new AbortController();
    try {
      const result = await testmgmt.exportCases(
        project,
        filter,
        ids,
        controller.signal,
      );
      if (disposed) return;
      const url = URL.createObjectURL(
        new Blob(["\ufeff", result.csv], { type: "text/csv;charset=utf-8" }),
      );
      try {
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = `test-cases-${project}-${result.exportedAt.slice(0, 10)}.csv`;
        anchor.click();
      } finally {
        URL.revokeObjectURL(url);
      }
      exported.value = result.count;
    } catch (failure) {
      if (!disposed)
        error.value =
          failure instanceof ApiError && failure.status === 400
            ? new ApiError(
                400,
                failure.code,
                failure.detail ?? failure.message,
                failure.requestId,
              )
            : failure;
    } finally {
      busy.value = false;
      controller = null;
    }
  }
  return { busy, error, results, total, updated, exported, apply, exportCsv };
}
