<script setup lang="ts">
import { ChevronRight, ListChecks, Plus, RefreshCw } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Plan } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import TmTabs from "./TmTabs.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute(),
  router = useRouter();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const canWrite = useProjectPermission("testmgmt.write");
const base = () => `/p/${encodeURIComponent(project.value)}`;
const plans = useResource<Plan[]>();
function refresh() {
  return plans.load((signal) => testmgmt.plans(project.value, signal));
}
watch(project, refresh, { immediate: true });
const panel = ref(false);
const name = ref("");
const mutation = useMutation();
async function create() {
  const created = await mutation.run("new", (signal) =>
    testmgmt.createPlan(project.value, name.value, signal),
  );
  if (created) {
    name.value = "";
    await router.push(`${base()}/plans/${encodeURIComponent(created.id)}`);
  }
}
</script>
<template>
  <PageHeader
    eyebrow="Test management"
    title="Test plans"
    description="A plan is one round of manual testing. Executions are recorded per case; the latest verdict counts. Manual runs never become automated runs (§6.8.6)."
  >
    <Button
      v-if="canWrite"
      size="sm"
      :variant="panel ? 'secondary' : 'default'"
      @click="panel = !panel"
      ><Plus />New plan</Button
    >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh plans"
      title="Refresh plans"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <TmTabs :project="project" current="plans" />

  <form
    v-if="canWrite && panel"
    class="bg-card mb-5 grid gap-4 rounded-lg border p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end"
    @submit.prevent="create"
  >
    <div class="space-y-2">
      <Label for="plan-name">Plan name</Label>
      <Input
        id="plan-name"
        v-model="name"
        required
        maxlength="200"
        placeholder="Release 2026.10 regression"
      />
    </div>
    <Button type="submit" size="sm" :disabled="mutation.busy.value !== null"
      >Create</Button
    >
    <ErrorNote
      v-if="mutation.error.value?.key === 'new'"
      class="sm:col-span-2"
      :error="mutation.error.value.error"
    />
  </form>

  <AsyncState
    :loading="plans.loading.value"
    :error="plans.error.value"
    @retry="refresh"
  >
    <EmptyState
      v-if="!plans.data.value?.length"
      :icon="ListChecks"
      title="No test plans yet"
      description="Open a plan when a round of manual testing starts; record each case as you go."
    >
      <Button v-if="canWrite" size="sm" @click="panel = true"
        ><Plus />New plan</Button
      >
    </EmptyState>
    <ul
      v-else
      class="plan-list bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li v-for="p in plans.data.value" :key="p.id">
        <RouterLink
          :to="`${base()}/plans/${encodeURIComponent(p.id)}`"
          class="hover:bg-accent focus-visible:bg-accent group flex items-center gap-3 px-4 py-3 transition-colors outline-none"
        >
          <span class="min-w-0 flex-1">
            <strong class="block truncate text-sm font-medium">{{
              p.name
            }}</strong>
            <span class="text-muted-foreground block text-xs"
              >opened {{ time(p.createdAt) }}</span
            >
          </span>
          <StatusBadge :status="p.status" />
          <ChevronRight
            :size="16"
            aria-hidden="true"
            class="text-muted-foreground transition-transform group-hover:translate-x-0.5"
          />
        </RouterLink>
      </li>
    </ul>
  </AsyncState>
</template>
