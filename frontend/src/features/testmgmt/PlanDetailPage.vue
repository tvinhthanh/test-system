<script setup lang="ts">
import {
  FileText,
  Lock,
  Paperclip,
  Play,
  RefreshCw,
  Search,
  Unlock,
  Video,
} from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import { useResource } from "../../core/state/useResource";
import type {
  Evidence,
  Execution,
  Plan,
  TestCase,
  StepResult,
  PlanCaseUpdate,
} from "./testmgmtRepository";
import type { ExecutionStatus } from "./enums";
import ExecutionRecorder from "./ExecutionRecorder.vue";
import ExecutionHistory from "./ExecutionHistory.vue";
import PlanScopeEditor from "./PlanScopeEditor.vue";
import PlanCaseControls from "./PlanCaseControls.vue";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { bytes, time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatTile from "../../shared/StatTile.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import EnvironmentPicker from "./EnvironmentPicker.vue";
import { testTypeById } from "./testTypes";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const route = useRoute();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const canWrite = useProjectPermission("testmgmt.write");
const planId = computed(() => String(route.params.plan));
const base = () => `/p/${encodeURIComponent(project.value)}`;

const plan = useResource<Plan>();
function refresh() {
  void plan.load((signal) =>
    testmgmt.plan(project.value, planId.value, signal),
  );
}
watch(() => [project.value, planId.value], refresh, { immediate: true });

const filter = ref("");
const verdicts = computed(() => {
  const m = new Map<string, Execution>();
  for (const e of plan.data.value?.executions ?? []) m.set(e.testCaseId, e);
  return m;
});
const planCases = computed(() =>
  (plan.data.value?.cases ?? []).map((item) => item.testCase),
);
const assignments = computed(
  () =>
    new Map(
      (plan.data.value?.cases ?? []).map((item) => [item.testCase.id, item]),
    ),
);
const rows = computed(() => {
  const q = filter.value.trim().toLowerCase();
  return planCases.value.filter(
    (c) =>
      !q ||
      c.title.toLowerCase().includes(q) ||
      c.externalKey.toLowerCase().includes(q),
  );
});
const counts = computed(() => {
  const out: Record<ExecutionStatus | "pending" | "running", number> = {
    passed: 0,
    failed: 0,
    blocked: 0,
    skipped: 0,
    pending: 0,
    running: 0,
  };
  for (const c of planCases.value) {
    if (assignments.value.get(c.id)?.workStatus === "in_progress") {
      out.running++;
      continue;
    }
    const v = verdicts.value.get(c.id);
    out[v ? v.status : "pending"]++;
  }
  return out;
});
const mutation = useMutation();
const closed = computed(() => plan.data.value?.status === "closed");
const planType = computed(() => testTypeById(plan.data.value?.testType ?? ""));
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
  const exec = await mutation.run(c.id, (signal) =>
    testmgmt.recordExecution(
      project.value,
      planId.value,
      { testCaseId: c.id, ...input },
      signal,
    ),
  );
  if (exec) {
    void plan.load((signal) =>
      testmgmt.plan(project.value, planId.value, signal),
    );
  }
}
async function setStatus(status: Plan["status"]) {
  const updated = await mutation.run("status", (signal) =>
    testmgmt.setPlanStatus(project.value, planId.value, status, signal),
  );
  if (updated) plan.data.value = updated;
}
// The site this plan is tested on (QĐ-14); a closed plan keeps its own.
async function setEnvironment(environmentId: string) {
  const updated = await mutation.run("environment", (signal) =>
    testmgmt.setPlanEnvironment(
      project.value,
      planId.value,
      environmentId,
      signal,
    ),
  );
  if (updated) plan.data.value = updated;
}
async function updatePlan(
  key: string,
  patch: { caseIds?: string[]; caseUpdate?: PlanCaseUpdate },
) {
  if (!plan.data.value) return;
  const updated = await mutation.run(key, (signal) =>
    testmgmt.updatePlan(
      project.value,
      planId.value,
      { ...patch, expectedUpdatedAt: plan.data.value!.updatedAt },
      signal,
    ),
  );
  if (updated) plan.data.value = updated;
}

// Evidence (§6.8.2): a file attached to the case's latest execution, stored
// as an Artifact. The accept list mirrors the server's policy for the kind
// (§11.5); the server still sniffs, so this is convenience, not the check.
const evidenceAccept =
  "image/png,image/jpeg,image/webp,video/webm,video/mp4,application/pdf,text/plain";
const evidenceCap = 20 * 1024 * 1024;
const pickers = ref<Record<string, HTMLInputElement | null>>({});
function pickEvidence(c: TestCase) {
  pickers.value[c.id]?.click();
}
async function attach(c: TestCase, event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = ""; // the same file may be chosen again after an error
  const exec = verdicts.value.get(c.id);
  if (!file || !exec) return;
  if (file.size > evidenceCap) {
    mutation.error.value = {
      key: c.id,
      error: new ApiError(
        413,
        "ARTIFACT_TOO_LARGE",
        `${file.name} is over the 20 MB evidence limit.`,
      ),
    };
    return;
  }
  const stored = await mutation.run(c.id, (signal) =>
    testmgmt.attachEvidence(project.value, planId.value, exec.id, file, signal),
  );
  const failure = mutation.error.value?.error;
  if (failure instanceof ApiError && failure.status === 400) {
    // The one 400 this route gives a person: the bytes are not what the
    // file says it is, or the type is outside the evidence policy.
    mutation.error.value = {
      key: c.id,
      error: new ApiError(
        400,
        failure.code,
        `${file.name} was refused: evidence must be an image, video, PDF or text file whose content matches its type.`,
        failure.requestId,
      ),
    };
  }
  if (stored) {
    // The plan is the source of truth; refetch rather than splice, so the
    // list matches what a second viewer would load.
    void plan.load((signal) =>
      testmgmt.plan(project.value, planId.value, signal),
    );
  }
}
const evidenceUrl = (e: Evidence) =>
  testmgmt.evidenceContentUrl(project.value, e.id);
const isImage = (e: Evidence) => e.contentType.startsWith("image/");
const isVideo = (e: Evidence) => e.contentType.startsWith("video/");
</script>
<template>
  <BackLink :to="`${base()}/plans`">Test plans</BackLink>
  <PageHeader eyebrow="Test plan" :title="plan.data.value?.name ?? planId">
    <template #description>
      <span
        v-if="plan.data.value"
        class="inline-flex flex-wrap items-center gap-2"
      >
        <StatusBadge :status="plan.data.value.status" />
        <Badge v-if="planType" variant="outline">{{ planType.label }}</Badge>
        <span class="text-muted-foreground text-xs"
          >opened {{ time(plan.data.value.createdAt) }}</span
        >
      </span>
    </template>
    <!-- QĐ-15: case by case, with the site one click away. -->
    <Button
      v-if="canWrite && plan.data.value && !closed && planCases.length"
      as-child
      size="sm"
      ><RouterLink :to="`${base()}/plans/${encodeURIComponent(planId)}/run`"
        ><Play />Run</RouterLink
      ></Button
    >
    <Button
      v-if="canWrite && plan.data.value && !closed"
      variant="outline"
      size="sm"
      :disabled="mutation.busy.value !== null"
      @click="setStatus('closed')"
      ><Lock />Close plan</Button
    >
    <Button
      v-else-if="canWrite && plan.data.value"
      variant="outline"
      size="sm"
      :disabled="mutation.busy.value !== null"
      @click="setStatus('active')"
      ><Unlock />Reopen</Button
    >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh plan"
      title="Refresh plan"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <ErrorNote
    v-if="mutation.error.value?.key === 'status'"
    class="mb-4"
    :error="mutation.error.value.error"
  />

  <AsyncState
    :loading="plan.loading.value"
    :error="plan.error.value"
    @retry="refresh"
  >
    <section
      v-if="plan.data.value"
      class="plan-environment bg-card mb-4 rounded-lg border p-4"
      aria-label="Site under test"
    >
      <EnvironmentPicker
        v-if="canWrite && !closed"
        id="plan-detail-environment"
        :model-value="plan.data.value.environmentId"
        :project="project"
        :can-write="canWrite"
        :disabled="mutation.busy.value !== null"
        @update:model-value="setEnvironment"
      />
      <p v-else class="text-sm">
        <span class="text-muted-foreground">Site under test: </span>
        <template v-if="plan.data.value.environment">
          {{ plan.data.value.environment.key }}
          <a
            v-if="plan.data.value.environment.baseUrl"
            :href="plan.data.value.environment.baseUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="text-primary underline"
            >{{ plan.data.value.environment.baseUrl }}</a
          >
        </template>
        <span v-else class="text-muted-foreground">not recorded</span>
      </p>
      <ErrorNote
        v-if="mutation.error.value?.key === 'environment'"
        class="mt-3"
        :error="mutation.error.value.error"
      />
    </section>
    <section
      class="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6"
      aria-label="Plan progress"
    >
      <StatTile label="Pending" :value="counts.pending" />
      <StatTile label="In progress" :value="counts.running" />
      <StatTile label="Passed" :value="counts.passed" />
      <StatTile label="Failed" :value="counts.failed" />
      <StatTile label="Blocked" :value="counts.blocked" />
      <StatTile label="Skipped" :value="counts.skipped" />
    </section>
    <PlanScopeEditor
      v-if="canWrite && plan.data.value"
      :project="project"
      :plan="plan.data.value"
      :disabled="closed || mutation.busy.value !== null"
      @save="updatePlan('scope', { caseIds: $event })"
    />
    <ErrorNote
      v-if="mutation.error.value?.key === 'scope'"
      class="mb-4"
      :error="mutation.error.value.error"
    />

    <label class="relative mb-4 block max-w-md">
      <span class="sr-only">Filter cases</span>
      <Search
        :size="16"
        aria-hidden="true"
        class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2"
      />
      <Input
        v-model="filter"
        type="search"
        class="pl-9"
        placeholder="Filter cases…"
      />
    </label>
    <p
      v-if="plan.data.value?.historyTruncated"
      class="text-muted-foreground mb-4 text-sm"
      role="status"
    >
      Showing the latest 200 executions in this plan. Older executions are
      retained.
    </p>

    <EmptyState
      v-if="!rows.length"
      :title="filter ? 'No matching cases' : 'No cases in this plan'"
    />
    <ul
      v-else
      class="execution-list bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li v-for="c in rows" :key="c.id" class="grid min-w-0 gap-3 px-4 py-3">
        <div class="min-w-0">
          <RouterLink
            :to="`${base()}/cases/${encodeURIComponent(c.id)}`"
            class="block truncate text-sm font-medium hover:underline"
            ><code
              v-if="c.externalKey"
              class="text-muted-foreground mr-1 text-xs"
              >{{ c.externalKey }}</code
            >{{ c.title }}</RouterLink
          >
          <span
            class="text-muted-foreground mt-0.5 flex flex-wrap items-center gap-2 text-xs"
          >
            <Badge variant="outline" class="font-mono uppercase">{{
              c.priority
            }}</Badge>
            <template v-if="verdicts.get(c.id)">
              <StatusBadge :status="verdicts.get(c.id)!.status" />
              {{ verdicts.get(c.id)!.executedBy }} ·
              {{ time(verdicts.get(c.id)!.executedAt) }}
              <span v-if="verdicts.get(c.id)!.note" class="truncate"
                >— {{ verdicts.get(c.id)!.note }}</span
              >
            </template>
            <span v-else>not executed in this plan</span>
          </span>
          <!-- Evidence of the latest execution: images inline as thumbnails,
               everything else a link. Same content route as the failure
               detail; the server picks inline vs download (§11.5). -->
          <ul
            v-if="verdicts.get(c.id)?.evidence.length"
            class="evidence-list mt-2 flex flex-wrap gap-2"
            :aria-label="`Evidence for ${c.title}`"
          >
            <li
              v-for="e in verdicts.get(c.id)!.evidence"
              :key="e.id"
              class="min-w-0"
            >
              <a
                :href="evidenceUrl(e)"
                target="_blank"
                rel="noopener"
                class="bg-muted hover:bg-accent inline-flex max-w-56 items-center gap-1.5 overflow-hidden rounded-md border text-xs"
                :title="`${e.name} · ${bytes(e.sizeBytes)}`"
              >
                <img
                  v-if="isImage(e)"
                  class="evidence-thumb block h-12 w-16 shrink-0 object-cover"
                  :src="evidenceUrl(e)"
                  :alt="e.name"
                  loading="lazy"
                  decoding="async"
                />
                <span
                  v-else
                  class="text-muted-foreground flex h-12 w-10 shrink-0 items-center justify-center"
                  aria-hidden="true"
                >
                  <Video v-if="isVideo(e)" :size="16" />
                  <FileText v-else :size="16" />
                </span>
                <span class="truncate pr-2">{{ e.name }}</span>
              </a>
            </li>
          </ul>
          <ErrorNote
            v-if="mutation.error.value?.key === c.id"
            class="mt-2"
            :error="mutation.error.value.error"
          />
        </div>
        <PlanCaseControls
          v-if="assignments.get(c.id)"
          :item="assignments.get(c.id)!"
          :disabled="closed || !canWrite || mutation.busy.value !== null"
          @update="updatePlan(c.id, { caseUpdate: $event })"
        />
        <ExecutionHistory
          :executions="
            (plan.data.value?.executionHistory ?? []).filter(
              (e) => e.testCaseId === c.id,
            )
          "
          :content-url="(id) => testmgmt.evidenceContentUrl(project, id)"
        />
        <ExecutionRecorder
          v-if="canWrite"
          :key="`${c.id}-${verdicts.get(c.id)?.id ?? 'new'}`"
          :test-case="c"
          :latest="verdicts.get(c.id)"
          :disabled="
            closed ||
            c.automationStatus === 'obsolete' ||
            mutation.busy.value !== null
          "
          @record="record(c, $event)"
        />
        <div
          v-if="canWrite"
          class="flex flex-wrap gap-1"
          role="group"
          :aria-label="`Record ${c.title}`"
        >
          <!-- Only a recorded execution can carry evidence: the file is an
               Artifact of that execution, not of the case. -->
          <Button
            variant="ghost"
            size="xs"
            class="evidence-attach"
            :disabled="
              closed || mutation.busy.value !== null || !verdicts.get(c.id)
            "
            :title="
              verdicts.get(c.id)
                ? 'Attach evidence to the latest execution'
                : 'Record a verdict first'
            "
            :aria-label="`Attach evidence for ${c.title}`"
            @click="pickEvidence(c)"
            ><Paperclip
          /></Button>
          <input
            :ref="(el) => (pickers[c.id] = el as HTMLInputElement | null)"
            type="file"
            class="sr-only"
            tabindex="-1"
            :accept="evidenceAccept"
            :aria-label="`Evidence file for ${c.title}`"
            @change="attach(c, $event)"
          />
        </div>
      </li>
    </ul>
  </AsyncState>
</template>
