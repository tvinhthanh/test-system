<script setup lang="ts">
import { Lock, RefreshCw, Search, Unlock } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import { executionStatuses, type ExecutionStatus } from "./enums";
import type { Execution, Plan, TestCase } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatTile from "../../shared/StatTile.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const route = useRoute();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const planId = computed(() => String(route.params.plan));
const base = () => `/p/${encodeURIComponent(project.value)}`;

const plan = useResource<Plan>();
const cases = useResource<TestCase[]>();
function refresh() {
  void plan.load((signal) =>
    testmgmt.plan(project.value, planId.value, signal),
  );
  void cases.load((signal) => testmgmt.testCases(project.value, {}, signal));
}
watch(() => [project.value, planId.value], refresh, { immediate: true });

// Every non-obsolete case is a row; the plan's latest execution per case
// is its verdict. Recording is one click per status, note optional.
const filter = ref("");
const verdicts = computed(() => {
  const m = new Map<string, Execution>();
  for (const e of plan.data.value?.executions ?? []) m.set(e.testCaseId, e);
  return m;
});
const rows = computed(() => {
  const q = filter.value.trim().toLowerCase();
  return (cases.data.value ?? []).filter(
    (c) =>
      c.automationStatus !== "obsolete" &&
      (!q ||
        c.title.toLowerCase().includes(q) ||
        c.externalKey.toLowerCase().includes(q)),
  );
});
const counts = computed(() => {
  const out: Record<ExecutionStatus | "pending", number> = {
    passed: 0,
    failed: 0,
    blocked: 0,
    skipped: 0,
    pending: 0,
  };
  for (const c of rows.value) {
    const v = verdicts.value.get(c.id);
    out[v ? v.status : "pending"]++;
  }
  return out;
});
const notes = ref<Record<string, string>>({});
const mutation = useMutation();
const closed = computed(() => plan.data.value?.status === "closed");
async function record(c: TestCase, status: ExecutionStatus) {
  const exec = await mutation.run(c.id, (signal) =>
    testmgmt.recordExecution(
      project.value,
      planId.value,
      { testCaseId: c.id, status, note: notes.value[c.id] ?? "" },
      signal,
    ),
  );
  if (exec) {
    delete notes.value[c.id];
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
        <span class="text-muted-foreground text-xs"
          >opened {{ time(plan.data.value.createdAt) }}</span
        >
      </span>
    </template>
    <Button
      v-if="plan.data.value && !closed"
      variant="outline"
      size="sm"
      :disabled="mutation.busy.value !== null"
      @click="setStatus('closed')"
      ><Lock />Close plan</Button
    >
    <Button
      v-else-if="plan.data.value"
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
    :loading="plan.loading.value || cases.loading.value"
    :error="plan.error.value ?? cases.error.value"
    @retry="refresh"
  >
    <section
      class="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-5"
      aria-label="Plan progress"
    >
      <StatTile label="Pending" :value="counts.pending" />
      <StatTile label="Passed" :value="counts.passed" />
      <StatTile label="Failed" :value="counts.failed" />
      <StatTile label="Blocked" :value="counts.blocked" />
      <StatTile label="Skipped" :value="counts.skipped" />
    </section>

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

    <EmptyState
      v-if="!rows.length"
      title="Nothing to execute"
      description="Add test cases first; every non-obsolete case appears here."
    />
    <ul
      v-else
      class="execution-list bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li
        v-for="c in rows"
        :key="c.id"
        class="grid gap-3 px-4 py-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,18rem)_auto] lg:items-center"
      >
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
          <ErrorNote
            v-if="mutation.error.value?.key === c.id"
            class="mt-2"
            :error="mutation.error.value.error"
          />
        </div>
        <Input
          v-model="notes[c.id]"
          :disabled="closed"
          placeholder="Note (optional)"
          maxlength="4000"
          :aria-label="`Note for ${c.title}`"
        />
        <div
          class="flex flex-wrap gap-1"
          role="group"
          :aria-label="`Record ${c.title}`"
        >
          <Button
            v-for="s in executionStatuses"
            :key="s"
            :variant="s === 'passed' ? 'default' : 'outline'"
            size="xs"
            :disabled="closed || mutation.busy.value !== null"
            @click="record(c, s)"
            >{{ s }}</Button
          >
        </div>
      </li>
    </ul>
  </AsyncState>
</template>
