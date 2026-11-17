<script setup lang="ts">
// One auto run (QĐ-16): every case with each step's outcome — what
// passed, the step that failed and why (an expectation that did not hold,
// an element that was not there, a page off the site), the steps never
// reached — screenshots at the end and where it failed, the page's console
// errors, and the steps AI repaired, each with a button to keep the repair
// in the case. While the run goes on the page looks again every 3 s.
import {
  CircleCheck,
  CircleDashed,
  CircleX,
  ExternalLink,
  Sparkles,
  Wrench,
} from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { AutoRun, AutoRunCase } from "./autotestRepository";
import { useMutation } from "../testmgmt/useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { explain } from "../testmgmt/explain";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatTile from "../../shared/StatTile.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import { duration, time } from "../../shared/format";
import { describeStep } from "./steps";
import { variantLabel } from "./matrix";
import StepNote from "./StepNote.vue";
import { Button } from "@/components/ui/button";

const route = useRoute();
const { autotest } = useServices();
const project = computed(() => String(route.params.project));
const runId = computed(() => String(route.params.run));
const canWrite = useProjectPermission("testmgmt.write");
const base = () => `/p/${encodeURIComponent(project.value)}`;
const run = useResource<AutoRun>();
let timer: ReturnType<typeof setTimeout> | undefined;
let pollAbort: AbortController | undefined;
let pollDelay = 3000;
const live = ref<AutoRun | null>(null);
const view = computed(() => live.value ?? run.data.value);
function schedule() {
  clearTimeout(timer);
  const current = view.value;
  if (
    !current ||
    (current.status !== "queued" && current.status !== "running") ||
    document.visibilityState === "hidden"
  )
    return;
  timer = setTimeout(async () => {
    const expectedProject = project.value;
    const expectedRun = runId.value;
    const controller = new AbortController();
    pollAbort = controller;
    try {
      const next = await autotest.run(
        expectedProject,
        expectedRun,
        controller.signal,
      );
      if (
        controller.signal.aborted ||
        project.value !== expectedProject ||
        runId.value !== expectedRun
      )
        return;
      pollDelay = 3000;
      live.value = next;
    } catch {
      if (
        !controller.signal.aborted &&
        project.value === expectedProject &&
        runId.value === expectedRun
      ) {
        pollDelay = Math.min(pollDelay * 2, 30000);
        schedule();
      }
    }
  }, pollDelay);
}
function load() {
  clearTimeout(timer);
  pollAbort?.abort();
  live.value = null;
  pollDelay = 3000;
  return run.load((signal) => autotest.run(project.value, runId.value, signal));
}
watch([project, runId], () => void load(), { immediate: true });
watch(view, schedule);
function visibilityChanged() {
  if (document.visibilityState === "hidden") {
    clearTimeout(timer);
    pollAbort?.abort();
  } else schedule();
}
document.addEventListener("visibilitychange", visibilityChanged);
onBeforeUnmount(() => {
  clearTimeout(timer);
  pollAbort?.abort();
  document.removeEventListener("visibilitychange", visibilityChanged);
});

const verdict = computed(() => {
  const r = view.value;
  if (!r) return "";
  if (r.status !== "done") return r.status;
  return r.failed || r.errored ? "failed" : "passed";
});
const done = computed(
  () => view.value?.cases.filter((c) => c.status !== "pending").length ?? 0,
);
const shot = (c: AutoRunCase, which: "final" | "failure") =>
  autotest.shotUrl(project.value, runId.value, c.caseId, which, c.variant);
const kindText: Record<string, string> = {
  locator: "The element was not found or could not be used",
  assert: "The check did not hold",
  navigation: "A page did not open, or the browser tried to leave the site",
  input: "The step itself is wrong (a variable, a value)",
  page: "The page failed",
};
const mutation = useMutation();
const applied = ref(new Set<string>());
async function apply(c: AutoRunCase, index: number) {
  const key = `heal:${c.caseId}:${c.variant}:${index}`;
  const ok = await mutation.run(key, (signal) =>
    autotest.applyHeal(
      project.value,
      runId.value,
      c.caseId,
      index,
      signal,
      c.variant,
    ),
  );
  if (ok) applied.value = new Set([...applied.value, key]);
}
</script>
<template>
  <BackLink :to="`${base()}/auto?tab=runs`">Auto test</BackLink>
  <PageHeader
    eyebrow="Auto run"
    :title="
      view
        ? `${view.total} case${view.total === 1 ? '' : 's'} on the site`
        : 'Auto run'
    "
  >
    <template #description>
      <span v-if="view" class="flex flex-wrap items-center gap-2">
        <StatusBadge :status="verdict" />
        <span class="text-muted-foreground text-sm"
          >{{ time(view.createdAt) }} ·
          {{
            view.trigger === "ci" ? "started from CI" : `by ${view.requestedBy}`
          }}
          · AI repairs {{ view.heal ? "on" : "off" }}</span
        >
      </span>
    </template>
    <Button v-if="view?.planId" as-child variant="outline" size="sm">
      <RouterLink :to="`${base()}/plans/${encodeURIComponent(view.planId)}`"
        >Test plan</RouterLink
      >
    </Button>
    <Button v-if="view?.testRunId" as-child variant="outline" size="sm">
      <RouterLink :to="`${base()}/runs/${encodeURIComponent(view.testRunId)}`"
        >Test run</RouterLink
      >
    </Button>
  </PageHeader>

  <AsyncState
    :loading="run.loading.value && !view"
    :error="run.error.value"
    @retry="load"
  >
    <template v-if="view">
      <div class="mb-5 grid grid-cols-2 gap-3 sm:grid-cols-5">
        <StatTile label="Passed" :value="view.passed" />
        <StatTile label="Failed" :value="view.failed" />
        <StatTile label="Could not run" :value="view.errored" />
        <StatTile label="Repaired by AI" :value="view.healed" />
        <StatTile label="Done" :value="`${done} / ${view.total}`" />
      </div>
      <div
        v-if="view.status !== 'done'"
        class="bg-muted mb-5 h-2 overflow-hidden rounded-full"
        role="progressbar"
        :aria-valuenow="done"
        :aria-valuemax="view.total"
        aria-label="Cases finished"
      >
        <div
          class="bg-primary h-full transition-all"
          :style="{ width: `${view.total ? (done / view.total) * 100 : 0}%` }"
        />
      </div>
      <ErrorNote
        v-if="mutation.error.value"
        class="mb-4"
        :error="explain(mutation.error.value.error)"
      />

      <ol class="space-y-4">
        <li
          v-for="c in view.cases"
          :key="`${c.caseId}:${c.variant}`"
          class="auto-run-case bg-card rounded-lg border"
        >
          <header class="flex flex-wrap items-center gap-3 border-b px-4 py-3">
            <StatusBadge
              :status="c.status === 'error' ? 'blocked' : c.status"
            />
            <span class="min-w-0 flex-1">
              <RouterLink
                :to="`${base()}/cases/${encodeURIComponent(c.caseId)}`"
                class="block truncate font-medium hover:underline"
                >{{ c.title }}</RouterLink
              >
              <span class="text-muted-foreground block truncate text-xs"
                >{{ variantLabel(c.variant) }} · {{ c.target
                }}<template v-if="c.account"> · as {{ c.account }}</template>
                <template v-if="c.durationMs">
                  · {{ duration(c.durationMs) }}</template
                ></span
              >
            </span>
            <span
              v-if="c.healed"
              class="text-warning flex items-center gap-1 text-xs"
              ><Sparkles class="size-3.5" />{{ c.healed }} repaired</span
            >
          </header>
          <div class="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
            <div class="min-w-0 space-y-2">
              <p
                v-if="c.status === 'pending'"
                class="text-muted-foreground text-sm"
                role="status"
              >
                Waiting for a browser…
              </p>
              <p
                v-if="c.error && c.status === 'error'"
                class="text-danger text-sm"
                role="alert"
              >
                {{ c.error }}
              </p>
              <ol class="space-y-1.5">
                <li
                  v-for="(s, i) in c.steps"
                  :key="i"
                  class="flex gap-2 text-sm"
                >
                  <CircleCheck
                    v-if="c.results[i]?.status === 'passed'"
                    class="text-success mt-0.5 size-4 shrink-0"
                    aria-label="passed"
                  />
                  <CircleX
                    v-else-if="c.results[i]?.status === 'failed'"
                    class="text-danger mt-0.5 size-4 shrink-0"
                    aria-label="failed"
                  />
                  <CircleDashed
                    v-else
                    class="text-muted-foreground mt-0.5 size-4 shrink-0"
                    aria-label="not run"
                  />
                  <span class="min-w-0 flex-1">
                    <span
                      :class="
                        c.results[i]?.status === 'skipped' || !c.results[i]
                          ? 'text-muted-foreground'
                          : ''
                      "
                      >{{ i + 1 }}. {{ s.say || describeStep(s) }}</span
                    >
                    <span v-if="s.expected" class="text-muted-foreground">
                      → {{ s.expected }}</span
                    >
                    <span
                      v-if="c.results[i]?.status === 'failed'"
                      class="text-danger block text-xs"
                      >{{ kindText[c.results[i]!.kind] ?? "Failed" }}:
                      {{ c.results[i]!.error }}</span
                    >
                    <StepNote
                      v-if="c.results[i]?.note"
                      :text="c.results[i]!.note"
                    />
                    <span
                      v-if="c.results[i]?.healed"
                      class="bg-warning-soft mt-1 block rounded px-2 py-1 text-xs"
                    >
                      <Wrench class="mr-1 inline size-3.5" />AI repaired this
                      step: {{ c.results[i]!.healed!.reason }} — it ran “{{
                        c.results[i]!.healed!.steps.map(
                          (x) => x.say || describeStep(x),
                        ).join("; ")
                      }}” instead.
                      <Button
                        v-if="
                          canWrite &&
                          !applied.has(`heal:${c.caseId}:${c.variant}:${i}`)
                        "
                        size="xs"
                        variant="outline"
                        class="ml-1"
                        :disabled="mutation.busy.value !== null"
                        @click="apply(c, i)"
                        >Keep the repair in the case</Button
                      >
                      <strong
                        v-else-if="
                          applied.has(`heal:${c.caseId}:${c.variant}:${i}`)
                        "
                        class="ml-1"
                        >Kept.</strong
                      >
                    </span>
                  </span>
                </li>
              </ol>
              <details v-if="c.console.length" class="text-xs">
                <summary class="text-muted-foreground cursor-pointer">
                  {{ c.console.length }} console message{{
                    c.console.length === 1 ? "" : "s"
                  }}
                </summary>
                <ul class="mt-1 space-y-0.5 font-mono">
                  <li v-for="(line, j) in c.console" :key="j" class="break-all">
                    {{ line }}
                  </li>
                </ul>
              </details>
            </div>
            <div v-if="c.screenshots.length" class="space-y-2">
              <figure v-for="w in c.screenshots" :key="w">
                <a
                  :href="shot(c, w)"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="block"
                >
                  <img
                    :src="shot(c, w)"
                    :alt="`${c.title}: ${w === 'final' ? 'the page at the end' : 'where it failed'}`"
                    class="rounded border"
                    loading="lazy"
                  />
                </a>
                <figcaption
                  class="text-muted-foreground mt-1 flex items-center gap-1 text-xs"
                >
                  {{ w === "final" ? "At the end" : "Where it failed"
                  }}<ExternalLink class="size-3" />
                </figcaption>
              </figure>
            </div>
          </div>
        </li>
      </ol>
    </template>
  </AsyncState>
</template>
