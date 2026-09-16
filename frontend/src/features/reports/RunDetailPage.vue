<script setup lang="ts">
import { ArrowLeft, RefreshCw, ChevronRight, ChevronsLeft } from "@lucide/vue";
import { useRunDetail } from "./useRunDetail";
import { duration, time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
const { project, id, cursor, run, results, loadRun, loadResults, next, first } =
  useRunDetail();
</script>
<template>
  <RouterLink class="back-link" :to="`/p/${encodeURIComponent(project)}/runs`"
    ><ArrowLeft :size="16" />Test runs</RouterLink
  >
  <header class="page-heading">
    <div>
      <p class="eyebrow">RUN DETAILS</p>
      <h1>Run {{ id }}</h1>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh run"
      title="Refresh run"
      @click="
        loadRun();
        loadResults();
      "
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <AsyncState
    :loading="run.loading.value"
    :error="run.error.value"
    @retry="loadRun"
    ><template v-if="run.data.value"
      ><StatusBadge
        :status="run.data.value.status"
        :flaky="run.data.value.flakyCount" />
      <div class="metrics">
        <div
          v-for="metric in [
            { label: 'Tests', value: run.data.value.totalCount },
            { label: 'Passed', value: run.data.value.passedCount },
            { label: 'Failed', value: run.data.value.failedCount },
            { label: 'Flaky', value: run.data.value.flakyCount },
            { label: 'Duration', value: duration(run.data.value.durationMs) },
          ]"
          :key="metric.label"
        >
          <span>{{ metric.label }}</span
          ><strong>{{ metric.value }}</strong>
        </div>
      </div>
      <TechnicalBlock
        title="Run context"
        :text="`${run.data.value.branch}\n${run.data.value.commitSha}\n${run.data.value.environmentName} / ${time(run.data.value.startedAt)}`" /></template
  ></AsyncState>
  <div class="section-heading">
    <h2>Test results</h2>
    <button
      class="icon-button"
      aria-label="Refresh results"
      title="Refresh results"
      @click="loadResults"
    >
      <RefreshCw :size="16" />
    </button>
  </div>
  <AsyncState
    :loading="results.loading.value"
    :error="results.error.value"
    @retry="loadResults"
    ><div v-if="!results.data.value?.items.length" class="empty-state">
      <h2>No results received yet</h2>
      <button class="button" @click="loadResults">Refresh results</button>
    </div>
    <div v-else class="test-list">
      <article
        v-for="result in results.data.value.items"
        :key="result.id"
        class="test-row clickable"
        @click="
          $router.push(
            `/p/${encodeURIComponent(project)}/runs/${encodeURIComponent(id)}/results/${encodeURIComponent(result.id)}`,
          )
        "
      >
        <div>
          <h3>{{ result.displayName }}</h3>
          <code>{{ result.filePath }}</code>
        </div>
        <StatusBadge :status="result.finalStatus" /><span
          >{{ result.attemptCount }} attempts</span
        ><span class="mono">{{ duration(result.durationMs) }}</span>
      </article>
    </div>
    <footer class="pagination">
      <span
        >{{ results.data.value?.items.length ?? 0 }} results on this page</span
      >
      <div>
        <button
          class="icon-button"
          aria-label="First results page"
          title="First results page"
          :disabled="!cursor"
          @click="first"
        >
          <ChevronsLeft :size="18" /></button
        ><button
          class="icon-button"
          aria-label="Next results page"
          title="Next results page"
          :disabled="!results.data.value?.nextCursor"
          @click="next"
        >
          <ChevronRight :size="18" />
        </button>
      </div></footer
  ></AsyncState>
</template>
