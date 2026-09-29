<script setup lang="ts">
import { ChevronRight, RefreshCw } from "@lucide/vue";
import { useRunDetail } from "./useRunDetail";
import { duration, time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import Pagination from "../../shared/Pagination.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import StatTile from "../../shared/StatTile.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
import { Button } from "@/components/ui/button";
import DeleteRunButton from "./DeleteRunButton.vue";
import { useProjectPermission } from "./useProjectPermission";
const { project, id, cursor, run, results, loadRun, loadResults, next, first } =
  useRunDetail();
// §11.3.2: owners, org admins and maintainers may delete a run.
const canDelete = useProjectPermission("data.delete");
const resultPath = (resultId: string) =>
  `/p/${encodeURIComponent(project.value)}/runs/${encodeURIComponent(id.value)}/results/${encodeURIComponent(resultId)}`;
</script>
<template>
  <BackLink :to="`/p/${encodeURIComponent(project)}/runs`">Test runs</BackLink>
  <PageHeader eyebrow="Run details" :title="`Run ${id}`">
    <template #description>
      <span class="inline-flex flex-wrap items-center gap-2">
        <StatusBadge
          v-if="run.data.value"
          :status="run.data.value.status"
          :flaky="run.data.value.flakyCount"
        />
        <span v-if="run.data.value" class="text-muted-foreground"
          >{{ run.data.value.branch }} · {{ run.data.value.environmentName }} ·
          {{ time(run.data.value.startedAt) }}</span
        >
      </span>
    </template>
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh run"
      title="Refresh run"
      @click="
        loadRun();
        loadResults();
      "
      ><RefreshCw
    /></Button>
    <DeleteRunButton
      v-if="canDelete && run.data.value"
      :project="project"
      :run="id"
    />
  </PageHeader>

  <AsyncState
    :loading="run.loading.value"
    :error="run.error.value"
    :rows="2"
    @retry="loadRun"
  >
    <template v-if="run.data.value">
      <section
        class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5"
        aria-label="Run figures"
      >
        <StatTile label="Tests" :value="run.data.value.totalCount" />
        <StatTile label="Passed" :value="run.data.value.passedCount" />
        <StatTile label="Failed" :value="run.data.value.failedCount" />
        <StatTile label="Flaky" :value="run.data.value.flakyCount" />
        <StatTile
          label="Duration"
          :value="duration(run.data.value.durationMs)"
        />
      </section>
      <div class="mt-4">
        <TechnicalBlock
          title="Run context"
          :text="`${run.data.value.branch}\n${run.data.value.commitSha}\n${run.data.value.environmentName} / ${time(run.data.value.startedAt)}`"
        />
      </div>
    </template>
  </AsyncState>

  <SectionHeading title="Test results">
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="Refresh results"
      title="Refresh results"
      @click="loadResults"
      ><RefreshCw
    /></Button>
  </SectionHeading>
  <AsyncState
    :loading="results.loading.value"
    :error="results.error.value"
    @retry="loadResults"
  >
    <EmptyState
      v-if="!results.data.value?.items.length"
      title="No results received yet"
      description="Results stream in as the reporter finishes each test."
    >
      <Button variant="outline" size="sm" @click="loadResults"
        >Refresh results</Button
      >
    </EmptyState>
    <ul
      v-else
      class="test-list bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li v-for="result in results.data.value.items" :key="result.id">
        <RouterLink
          :to="resultPath(result.id)"
          class="test-row clickable hover:bg-accent focus-visible:bg-accent group grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 px-4 py-3 transition-colors outline-none sm:grid-cols-[minmax(0,1fr)_auto_auto_auto_1.25rem]"
        >
          <span class="min-w-0">
            <h3 class="truncate text-sm font-medium">
              {{ result.displayName }}
            </h3>
            <code class="text-muted-foreground block truncate text-xs">{{
              result.filePath
            }}</code>
          </span>
          <StatusBadge :status="result.finalStatus" />
          <span
            class="text-muted-foreground col-start-1 text-xs sm:col-start-auto sm:w-20 sm:text-right"
            >{{ result.attemptCount }}
            {{ result.attemptCount === 1 ? "attempt" : "attempts" }}</span
          >
          <span
            class="text-muted-foreground font-mono text-xs tabular-nums sm:w-16 sm:text-right"
            >{{ duration(result.durationMs) }}</span
          >
          <ChevronRight
            :size="16"
            aria-hidden="true"
            class="text-muted-foreground hidden transition-transform group-hover:translate-x-0.5 sm:block"
          />
        </RouterLink>
      </li>
    </ul>
    <Pagination
      :summary="`${results.data.value?.items.length ?? 0} results on this page`"
      label="results page"
      :can-first="!!cursor"
      :can-next="!!results.data.value?.nextCursor"
      @first="first"
      @next="next"
    />
  </AsyncState>
</template>
