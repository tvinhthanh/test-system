<script setup lang="ts">
import { ChevronRight, Plus, RefreshCw, Rocket } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Release } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import TmTabs from "./TmTabs.vue";
import GateBadge from "./GateBadge.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute(),
  router = useRouter();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const canWrite = useProjectPermission("testmgmt.write");
const base = () => `/p/${encodeURIComponent(project.value)}`;
const releases = useResource<Release[]>();
function refresh() {
  return releases.load((signal) => testmgmt.releases(project.value, signal));
}
watch(project, refresh, { immediate: true });
const panel = ref(false);
const draft = ref({ version: "", branch: "" });
const mutation = useMutation();
async function create() {
  const created = await mutation.run("new", (signal) =>
    testmgmt.createRelease(project.value, { ...draft.value }, signal),
  );
  if (created) {
    draft.value = { version: "", branch: "" };
    await router.push(`${base()}/releases/${encodeURIComponent(created.id)}`);
  }
}
</script>
<template>
  <PageHeader
    eyebrow="Test management"
    title="Releases"
    description="Each release gets a gate verdict computed from the newest finished run — and a human decision, recorded by name, that may override it (§6.8.4)."
  >
    <Button
      v-if="canWrite"
      size="sm"
      :variant="panel ? 'secondary' : 'default'"
      @click="panel = !panel"
      ><Plus />New release</Button
    >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh releases"
      title="Refresh releases"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <TmTabs :project="project" current="releases" />

  <form
    v-if="canWrite && panel"
    class="bg-card mb-5 grid gap-4 rounded-lg border p-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end"
    @submit.prevent="create"
  >
    <div class="space-y-2">
      <Label for="rel-version">Version</Label>
      <Input
        id="rel-version"
        v-model="draft.version"
        required
        maxlength="100"
        placeholder="2026.10.3"
      />
    </div>
    <div class="space-y-2">
      <Label for="rel-branch">Branch (optional)</Label>
      <Input
        id="rel-branch"
        v-model="draft.branch"
        maxlength="200"
        placeholder="release/2026.10 — empty = any"
      />
    </div>
    <Button type="submit" size="sm" :disabled="mutation.busy.value !== null"
      >Create</Button
    >
    <ErrorNote
      v-if="mutation.error.value?.key === 'new'"
      class="sm:col-span-3"
      :error="mutation.error.value.error"
    />
  </form>

  <AsyncState
    :loading="releases.loading.value"
    :error="releases.error.value"
    @retry="refresh"
  >
    <EmptyState
      v-if="!releases.data.value?.length"
      :icon="Rocket"
      title="No releases yet"
      description="Open one when a version is being prepared; evaluate the gate before deciding."
    >
      <Button v-if="canWrite" size="sm" @click="panel = true"
        ><Plus />New release</Button
      >
    </EmptyState>
    <ul
      v-else
      class="release-list bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li v-for="r in releases.data.value" :key="r.id">
        <RouterLink
          :to="`${base()}/releases/${encodeURIComponent(r.id)}`"
          class="hover:bg-accent focus-visible:bg-accent group grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 px-4 py-3 transition-colors outline-none sm:grid-cols-[minmax(0,1fr)_auto_auto_1.25rem]"
        >
          <span class="min-w-0">
            <strong class="block truncate font-mono text-sm font-medium">{{
              r.version
            }}</strong>
            <span class="text-muted-foreground block text-xs">
              {{ r.branch || "any branch" }} · opened {{ time(r.createdAt) }}
              <template v-if="r.decidedBy">
                · decided by {{ r.decidedBy }}</template
              >
            </span>
          </span>
          <GateBadge :gate="r.gate" />
          <StatusBadge :status="r.status" />
          <ChevronRight
            :size="16"
            aria-hidden="true"
            class="text-muted-foreground hidden transition-transform group-hover:translate-x-0.5 sm:block"
          />
        </RouterLink>
      </li>
    </ul>
  </AsyncState>
</template>
