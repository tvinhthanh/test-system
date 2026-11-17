import { computed, onBeforeUnmount, ref, watch, toRaw } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import { ApiError } from "../../core/api/client";
import { useProjectPermission } from "../reports/useProjectPermission";
import { useMutation } from "./useMutation";
import {
  emptyReportData,
  reportFields,
  type SpecializedInput,
  type SpecializedResult,
} from "./specializedReports";
export function useSpecializedResults() {
  const { testmgmt } = useServices(),
    route = useRoute(),
    router = useRouter();
  const project = computed(() => String(route.params.project));
  const selected = computed(() => String(route.query.result ?? ""));
  const kind = computed(() => String(route.query.kind ?? "")),
    outcome = computed(() => String(route.query.outcome ?? ""));
  const cursor = computed(() => String(route.query.cursor ?? ""));
  const base = () =>
    `/p/${encodeURIComponent(project.value)}/specialized-results`;
  const canWrite = useProjectPermission("testmgmt.write");
  const list = useResource<{
      items: SpecializedResult[];
      nextCursor: string;
    }>(),
    detail = useResource<SpecializedResult>();
  const mutation = useMutation(),
    panel = ref(false);
  function blank(): SpecializedInput {
    return {
      schemaVersion: 1,
      idempotencyKey: crypto.randomUUID(),
      testCaseId: "",
      kind: "performance",
      title: "",
      state: "completed",
      notes: "",
      environment: "",
      applicationVersion: "",
      source: "manual",
      toolVersion: "1",
      adapterVersion: "atp-report-v1",
      startedAt: "",
      finishedAt: "",
      retestOf: "",
      details: emptyReportData(reportFields.performance),
      originalReport: "",
    };
  }
  const draft = ref(blank());
  let disposed = false;
  onBeforeUnmount(() => {
    disposed = true;
  });
  async function refresh() {
    await list.load(async (signal) => {
      const page = await testmgmt.specializedResults(
        project.value,
        { kind: kind.value, outcome: outcome.value, cursor: cursor.value },
        signal,
      );
      return page;
    });
  }
  watch(
    () => [project.value, kind.value, outcome.value, cursor.value],
    () => {
      panel.value = false;
      void refresh();
    },
    { immediate: true },
  );
  watch(
    () => [project.value, selected.value],
    () => {
      if (selected.value)
        void detail.load((signal) =>
          testmgmt.specializedResult(project.value, selected.value, signal),
        );
    },
    { immediate: true },
  );
  function filter(patch: Record<string, string>) {
    void router.replace({
      path: base(),
      query: { kind: kind.value, outcome: outcome.value, cursor: "", ...patch },
    });
  }
  function openNew() {
    draft.value = blank();
    panel.value = true;
    void router.push({
      path: base(),
      query: { kind: kind.value, outcome: outcome.value },
    });
  }
  function retest() {
    const old = detail.data.value;
    if (!old) return;
    const value = structuredClone(toRaw(old.input));
    value.idempotencyKey = crypto.randomUUID();
    value.retestOf = old.id;
    value.originalReport = "";
    value.state = "completed";
    draft.value = {
      ...value,
      details: emptyReportData(reportFields[value.kind]),
    };
    panel.value = true;
    void router.push({
      path: base(),
      query: { kind: kind.value, outcome: outcome.value },
    });
  }
  async function persist(input: SpecializedInput) {
    const target = project.value;
    const saved = await mutation.run("save", (signal) =>
      testmgmt.createSpecializedResult(target, input, signal),
    );
    if (saved && !disposed && project.value === target) {
      panel.value = false;
      await refresh();
      if (disposed || project.value !== target) return;
      await router.push({ path: base(), query: { result: saved.id } });
    }
  }
  async function importFile(event: Event) {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file) return;
    const target = project.value;
    const saved = await mutation.run("import", async (signal) => {
      if (file.size > 4 * 1024 * 1024)
        throw new ApiError(
          400,
          "INVALID_PAYLOAD",
          "Report file exceeds 4 MiB.",
        );
      const text = await file.text();
      let input: SpecializedInput;
      try {
        input = JSON.parse(text);
      } catch {
        throw new ApiError(400, "INVALID_PAYLOAD", "Invalid ATP report JSON.");
      }
      if (!input || typeof input !== "object" || Array.isArray(input))
        throw new ApiError(
          400,
          "INVALID_PAYLOAD",
          "Expected an ATP report object.",
        );
      if (!input.idempotencyKey) {
        const hash = await crypto.subtle.digest(
          "SHA-256",
          new TextEncoder().encode(text),
        );
        input.idempotencyKey =
          "json-" +
          Array.from(new Uint8Array(hash), (b) =>
            b.toString(16).padStart(2, "0"),
          ).join("");
      }
      if (disposed || target !== project.value || signal.aborted)
        throw new DOMException("Import cancelled", "AbortError");
      return testmgmt.createSpecializedResult(target, input, signal);
    });
    (event.target as HTMLInputElement).value = "";
    if (saved && !disposed && target === project.value) {
      await refresh();
      if (disposed || project.value !== target) return;
      await router.push({ path: base(), query: { result: saved.id } });
    }
  }

  return {
    testmgmt,
    project,
    selected,
    kind,
    outcome,
    cursor,
    base,
    canWrite,
    list,
    detail,
    mutation,
    panel,
    draft,
    refresh,
    filter,
    openNew,
    retest,
    persist,
    importFile,
  };
}
