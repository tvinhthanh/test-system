<script setup lang="ts">
// Run a plan one case at a time (QĐ-15): the site opens in its own tab
// from the button at the top, the case's steps and expected results sit
// beside the step-by-step recorder, and recording moves on to the next case
// not yet run. Evidence attaches to the case's latest result. Kinds of
// testing with an automatic part offer a URL check of the same site.
import {
  CircleCheck,
  ExternalLink,
  Paperclip,
  ScanSearch,
  SkipForward,
} from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import { useResource } from "../../core/state/useResource";
import type {
  Execution,
  Plan,
  StepResult,
  TestCase,
  UrlCheck,
} from "./testmgmtRepository";
import type { ExecutionStatus } from "./enums";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { testTypeById } from "./testTypes";
import ExecutionRecorder from "./ExecutionRecorder.vue";
import ErrorNote from "./ErrorNote.vue";
import { explain } from "./explain";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

const route = useRoute();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const planId = computed(() => String(route.params.plan));
const canWrite = useProjectPermission("testmgmt.write");
const base = () => `/p/${encodeURIComponent(project.value)}`;

const plan = useResource<Plan>();
const refresh = () =>
  plan.load((signal) => testmgmt.plan(project.value, planId.value, signal));
watch(() => [project.value, planId.value], refresh, { immediate: true });

const cases = computed<TestCase[]>(() =>
  (plan.data.value?.cases ?? [])
    .map((c) => c.testCase)
    .filter((c) => c.automationStatus !== "obsolete"),
);
const latest = computed(() => {
  const m = new Map<string, Execution>();
  for (const e of plan.data.value?.executions ?? []) m.set(e.testCaseId, e);
  return m;
});
const done = computed(
  () => cases.value.filter((c) => latest.value.has(c.id)).length,
);
const closed = computed(() => plan.data.value?.status === "closed");
const site = computed(() => {
  const url = plan.data.value?.environment?.baseUrl ?? "";
  return /^https?:\/\//i.test(url) ? url : "";
});
const type = computed(() => testTypeById(plan.data.value?.testType ?? ""));

// The case on screen: chosen, else the first one not yet run.
const picked = ref("");
const current = computed(
  () =>
    cases.value.find((c) => c.id === picked.value) ??
    cases.value.find((c) => !latest.value.has(c.id)) ??
    cases.value[0],
);
const index = computed(() =>
  current.value ? cases.value.indexOf(current.value) : -1,
);
function nextPending(after: string) {
  const i = cases.value.findIndex((c) => c.id === after);
  const rest = [...cases.value.slice(i + 1), ...cases.value.slice(0, i)];
  return rest.find((c) => !latest.value.has(c.id))?.id ?? after;
}

const mutation = useMutation();
async function record(
  c: TestCase,
  input: {
    status: ExecutionStatus;
    note: string;
    bugUrl: string;
    retestOf: string;
    stepResults: StepResult[];
    caseUpdatedAt: string;
  },
) {
  const exec = await mutation.run("record", (signal) =>
    testmgmt.recordExecution(
      project.value,
      planId.value,
      { testCaseId: c.id, ...input },
      signal,
    ),
  );
  if (!exec) return;
  await refresh();
  // A failure usually needs evidence first; anything else moves on.
  if (input.status === "passed" || input.status === "skipped")
    picked.value = nextPending(c.id);
  else picked.value = c.id;
}

// Evidence for the case's latest result.
const picker = ref<HTMLInputElement | null>(null);
async function attach(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  const c = current.value;
  const exec = c && latest.value.get(c.id);
  if (!file || !exec) return;
  if (file.size > 20 * 1024 * 1024) {
    mutation.error.value = {
      key: "evidence",
      error: new ApiError(
        413,
        "ARTIFACT_TOO_LARGE",
        `${file.name} is over the 20 MB evidence limit.`,
      ),
    };
    return;
  }
  const stored = await mutation.run("evidence", (signal) =>
    testmgmt.attachEvidence(project.value, planId.value, exec.id, file, signal),
  );
  if (stored) await refresh();
}

// URL check of the plan's site.
const check = ref<UrlCheck | null>(null);
async function urlCheck() {
  const made = await mutation.run("urlcheck", (signal) =>
    testmgmt.createUrlCheck(
      project.value,
      { url: site.value, environmentId: plan.data.value?.environmentId ?? "" },
      signal,
    ),
  );
  if (made) check.value = made;
}
const dot = (c: TestCase) => {
  const e = latest.value.get(c.id);
  return !e
    ? "bg-neutral/40"
    : e.status === "passed"
      ? "bg-success"
      : e.status === "skipped"
        ? "bg-neutral"
        : e.status === "blocked"
          ? "bg-warning"
          : "bg-danger";
};
</script>
<template>
  <BackLink :to="`${base()}/plans/${encodeURIComponent(planId)}`"
    >Plan</BackLink
  >
  <PageHeader eyebrow="Running" :title="plan.data.value?.name ?? 'Test plan'">
    <template #description>
      <span
        v-if="plan.data.value"
        class="inline-flex flex-wrap items-center gap-2"
      >
        <Badge v-if="type" variant="outline">{{ type.label }}</Badge>
        <span class="text-muted-foreground text-xs"
          >{{ done }} of {{ cases.length }} recorded</span
        >
      </span>
    </template>
    <Button v-if="site" as-child size="sm"
      ><a :href="site" target="_blank" rel="noopener noreferrer"
        ><ExternalLink />Open {{ site.replace(/^https?:\/\//, "") }}</a
      ></Button
    >
    <Button
      v-if="site && type?.urlCheck && canWrite"
      variant="outline"
      size="sm"
      :disabled="mutation.busy.value !== null"
      @click="urlCheck"
      ><ScanSearch />URL check</Button
    >
  </PageHeader>

  <div
    v-if="plan.data.value"
    class="bg-muted mb-4 h-2 overflow-hidden rounded-full"
    role="progressbar"
    :aria-valuenow="done"
    :aria-valuemax="cases.length"
    aria-label="Cases recorded"
  >
    <div
      class="bg-primary h-full transition-[width]"
      :style="{ width: `${cases.length ? (done / cases.length) * 100 : 0}%` }"
    />
  </div>
  <p
    v-if="plan.data.value && !site"
    class="bg-warning-soft text-warning mb-4 rounded-md px-3 py-2 text-sm"
  >
    This plan has no site address. Set its environment on the
    <RouterLink
      :to="`${base()}/plans/${encodeURIComponent(planId)}`"
      class="underline"
      >plan page</RouterLink
    >
    to open the site from here.
  </p>
  <p v-if="check" class="mb-4 text-sm" role="status">
    URL check started —
    <RouterLink
      :to="`${base()}/url-checks/${encodeURIComponent(check.id)}`"
      target="_blank"
      class="text-primary underline"
      >see the report</RouterLink
    >
    (opens in a new tab; the report fills in by itself).
  </p>
  <ErrorNote
    v-if="mutation.error.value?.key === 'urlcheck'"
    class="mb-4"
    :error="explain(mutation.error.value.error)"
  />

  <AsyncState
    :loading="plan.loading.value && !plan.data.value"
    :error="plan.error.value"
    @retry="refresh"
  >
    <EmptyState
      v-if="!cases.length"
      :icon="CircleCheck"
      title="This plan has no cases"
      description="Add cases on the plan page, or start a new round from Start testing."
    />
    <div v-else class="run-plan grid gap-4 lg:grid-cols-[17rem_minmax(0,1fr)]">
      <nav
        aria-label="Cases in this plan"
        class="bg-card hidden max-h-[70vh] overflow-y-auto rounded-lg border lg:block"
      >
        <ol class="divide-y">
          <li v-for="(c, i) in cases" :key="c.id">
            <button
              type="button"
              :aria-current="current?.id === c.id ? 'step' : undefined"
              :class="[
                'flex w-full items-start gap-2 px-3 py-2 text-left text-sm',
                current?.id === c.id ? 'bg-accent' : 'hover:bg-accent/60',
              ]"
              @click="picked = c.id"
            >
              <span
                :class="['mt-1.5 size-2 shrink-0 rounded-full', dot(c)]"
                aria-hidden="true"
              />
              <span class="min-w-0 flex-1">
                <span class="text-muted-foreground text-xs">{{ i + 1 }}.</span>
                {{ c.title }}
              </span>
            </button>
          </li>
        </ol>
      </nav>

      <section
        v-if="current"
        class="bg-card space-y-4 rounded-lg border p-4"
        :aria-label="`Case ${index + 1}: ${current.title}`"
      >
        <header class="flex flex-wrap items-start gap-2">
          <div class="min-w-0 flex-1">
            <p class="text-muted-foreground text-xs">
              Case {{ index + 1 }} of {{ cases.length }} ·
              {{ current.priority.toUpperCase()
              }}<template v-if="current.isCritical"> · critical</template>
            </p>
            <h2 class="text-lg font-semibold">{{ current.title }}</h2>
          </div>
          <StatusBadge
            v-if="latest.get(current.id)"
            :status="latest.get(current.id)!.status"
          />
          <Button
            variant="ghost"
            size="sm"
            :disabled="cases.length < 2"
            @click="picked = nextPending(current.id)"
            ><SkipForward />Next</Button
          >
        </header>
        <p
          v-if="current.preconditions && (closed || !canWrite)"
          class="bg-muted/50 rounded-md px-3 py-2 text-sm"
        >
          <strong class="font-medium">Before you start:</strong>
          {{ current.preconditions }}
        </p>
        <p v-if="closed" class="text-muted-foreground text-sm">
          This plan is closed; results can no longer be recorded.
        </p>
        <ExecutionRecorder
          v-else-if="canWrite"
          :key="`${current.id}-${latest.get(current.id)?.id ?? 'new'}`"
          :test-case="current"
          :latest="latest.get(current.id)"
          :disabled="mutation.busy.value !== null"
          open
          @record="record(current, $event)"
        />
        <ErrorNote
          v-if="mutation.error.value?.key === 'record'"
          :error="explain(mutation.error.value.error)"
        />
        <div
          v-if="canWrite && !closed && latest.get(current.id)"
          class="flex flex-wrap items-center gap-2 border-t pt-3 text-sm"
        >
          <Button
            variant="outline"
            size="sm"
            :disabled="mutation.busy.value !== null"
            @click="picker?.click()"
            ><Paperclip />Attach screenshot or video</Button
          >
          <span class="text-muted-foreground text-xs"
            >{{ latest.get(current.id)!.evidence.length }} file{{
              latest.get(current.id)!.evidence.length === 1 ? "" : "s"
            }}
            attached to the latest result</span
          >
          <input
            ref="picker"
            type="file"
            class="sr-only"
            tabindex="-1"
            accept="image/png,image/jpeg,image/webp,video/webm,video/mp4,application/pdf,text/plain"
            aria-label="Evidence file"
            @change="attach"
          />
          <ErrorNote
            v-if="mutation.error.value?.key === 'evidence'"
            class="basis-full"
            :error="explain(mutation.error.value.error)"
          />
        </div>
        <p
          v-if="done === cases.length"
          class="bg-success-soft text-success rounded-md px-3 py-2 text-sm"
          role="status"
        >
          Every case has a result.
          <RouterLink
            :to="`${base()}/plans/${encodeURIComponent(planId)}`"
            class="underline"
            >Review the plan</RouterLink
          >
          or
          <RouterLink :to="`${base()}/releases`" class="underline"
            >decide the release</RouterLink
          >.
        </p>
      </section>
    </div>
  </AsyncState>
</template>
