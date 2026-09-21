<script setup lang="ts">
import { Plus, RefreshCw, Target } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import {
  priorities,
  requirementStatuses,
  type Priority,
  type RequirementStatus,
} from "./enums";
import type { Requirement } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import TmTabs from "./TmTabs.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const route = useRoute();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const list = useResource<Requirement[]>();
function refresh() {
  return list.load((signal) => testmgmt.requirements(project.value, signal));
}
watch(project, refresh, { immediate: true });

const panel = ref(false);
const draft = ref({ externalKey: "", title: "", priority: "p2" as Priority });
const mutation = useMutation();
async function create() {
  const created = await mutation.run("new", (signal) =>
    testmgmt.createRequirement(project.value, { ...draft.value }, signal),
  );
  if (created) {
    draft.value = { externalKey: "", title: "", priority: "p2" };
    panel.value = false;
    await refresh();
  }
}
async function patch(
  r: Requirement,
  change: { priority?: Priority; status?: RequirementStatus },
) {
  const updated = await mutation.run(r.id, (signal) =>
    testmgmt.updateRequirement(project.value, r.id, change, signal),
  );
  if (updated && list.data.value)
    list.data.value = list.data.value.map((x) => (x.id === r.id ? updated : x));
}
const p0 = computed(() => {
  const open = (list.data.value ?? []).filter(
    (r) =>
      r.priority === "p0" && (r.status === "draft" || r.status === "active"),
  );
  return {
    total: open.length,
    covered: open.filter((r) => r.coveredBy > 0).length,
  };
});
</script>
<template>
  <PageHeader
    eyebrow="Test management"
    title="Requirements"
    description="What the product must do, and how many test cases check each one. p0 coverage feeds the release gate (§6.8.4)."
  >
    <Button
      size="sm"
      :variant="panel ? 'secondary' : 'default'"
      @click="panel = !panel"
      ><Plus />New requirement</Button
    >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh requirements"
      title="Refresh requirements"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <TmTabs :project="project" current="requirements" />

  <form
    v-if="panel"
    class="bg-card mb-5 grid gap-4 rounded-lg border p-4 sm:grid-cols-[8rem_minmax(0,1fr)_7rem_auto] sm:items-end"
    @submit.prevent="create"
  >
    <div class="space-y-2">
      <Label for="req-key">Key</Label>
      <Input
        id="req-key"
        v-model="draft.externalKey"
        placeholder="PAY-12"
        maxlength="100"
      />
    </div>
    <div class="space-y-2">
      <Label for="req-title">Title</Label>
      <Input id="req-title" v-model="draft.title" required maxlength="500" />
    </div>
    <div class="space-y-2">
      <Label for="req-priority">Priority</Label>
      <NativeSelect id="req-priority" v-model="draft.priority" class="w-full">
        <option v-for="p in priorities" :key="p" :value="p">{{ p }}</option>
      </NativeSelect>
    </div>
    <Button type="submit" size="sm" :disabled="mutation.busy.value !== null"
      >Create</Button
    >
    <ErrorNote
      v-if="mutation.error.value?.key === 'new'"
      class="sm:col-span-4"
      :error="mutation.error.value.error"
    />
  </form>

  <AsyncState
    :loading="list.loading.value"
    :error="list.error.value"
    @retry="refresh"
  >
    <EmptyState
      v-if="!list.data.value?.length"
      :icon="Target"
      title="No requirements yet"
      description="Add the p0 flows first — the release gate watches their coverage."
    >
      <Button size="sm" @click="panel = true"><Plus />New requirement</Button>
    </EmptyState>
    <template v-else>
      <p v-if="p0.total" class="text-muted-foreground mb-3 text-sm">
        <strong class="text-foreground font-medium"
          >{{ p0.covered }} of {{ p0.total }}</strong
        >
        open p0 requirement{{ p0.total === 1 ? "" : "s" }} covered by at least
        one case.
      </p>
      <div class="bg-card overflow-x-auto rounded-lg border">
        <Table class="requirement-table min-w-[640px]">
          <TableHeader>
            <TableRow class="hover:bg-transparent">
              <TableHead class="w-28">Key</TableHead>
              <TableHead>Title</TableHead>
              <TableHead class="w-24">Priority</TableHead>
              <TableHead class="w-32">Status</TableHead>
              <TableHead class="w-24 text-right">Covered by</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="r in list.data.value" :key="r.id">
              <TableCell
                ><code class="text-xs">{{
                  r.externalKey || "—"
                }}</code></TableCell
              >
              <TableCell class="font-medium">
                {{ r.title }}
                <ErrorNote
                  v-if="mutation.error.value?.key === r.id"
                  class="mt-2"
                  :error="mutation.error.value.error"
                />
              </TableCell>
              <TableCell>
                <NativeSelect
                  :model-value="r.priority"
                  class="w-20"
                  :aria-label="`Priority of ${r.title}`"
                  @update:model-value="
                    patch(r, { priority: $event as Priority })
                  "
                >
                  <option v-for="p in priorities" :key="p" :value="p">
                    {{ p }}
                  </option>
                </NativeSelect>
              </TableCell>
              <TableCell>
                <NativeSelect
                  :model-value="r.status"
                  class="w-28"
                  :aria-label="`Status of ${r.title}`"
                  @update:model-value="
                    patch(r, { status: $event as RequirementStatus })
                  "
                >
                  <option v-for="s in requirementStatuses" :key="s" :value="s">
                    {{ s }}
                  </option>
                </NativeSelect>
              </TableCell>
              <TableCell class="text-right">
                <StatusBadge
                  v-if="r.coveredBy === 0 && r.priority === 'p0'"
                  status="uncovered"
                />
                <span v-else class="font-mono text-xs tabular-nums">{{
                  r.coveredBy
                }}</span>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </template>
  </AsyncState>
</template>
