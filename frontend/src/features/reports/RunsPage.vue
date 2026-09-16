<script setup lang="ts">
import {
  RefreshCw,
  Search,
  GitBranch,
  ArrowUpRight,
  ChevronRight,
  ChevronsLeft,
  ListFilter,
  Inbox,
} from "@lucide/vue";
import { useRuns } from "./useRuns";
import { duration, short, time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
const {
  data,
  error,
  loading,
  query,
  search,
  project,
  filtered,
  filter,
  refresh,
  next,
  first,
  clear,
} = useRuns();
</script>
<template>
  <header class="page-heading">
    <div>
      <p class="eyebrow">REPORT CENTER</p>
      <h1>Test runs</h1>
      <p class="muted">Execution results across your environments</p>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh runs"
      title="Refresh runs"
      :disabled="loading"
      @click="refresh"
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <div class="view-tabs">
    <span class="selected"><GitBranch :size="16" />All runs</span
    ><span class="quiet-label">Playwright</span>
  </div>
  <form class="filters" @submit.prevent>
    <label class="search-field"
      ><span class="sr-only">Search branch, commit or run</span
      ><Search :size="17" /><input
        v-model="search"
        type="search"
        placeholder="Search branch, commit or run..."
        maxlength="200" /></label
    ><label class="select-field"
      ><span>Status</span
      ><select
        :value="query.status"
        @change="filter({ status: ($event.target as HTMLSelectElement).value })"
      >
        <option value="">All statuses</option>
        <option
          v-for="status in [
            'running',
            'passed',
            'failed',
            'error',
            'cancelled',
          ]"
          :key="status"
          :value="status"
        >
          {{ status }}
        </option>
      </select></label
    ><label class="select-field"
      ><span>Environment</span
      ><input
        :value="query.environment"
        placeholder="All environments"
        @change="
          filter({
            environment: ($event.target as HTMLInputElement).value.trim(),
          })
        " /></label
    ><button v-if="filtered" class="button quiet" type="button" @click="clear">
      <ListFilter :size="16" />Clear
    </button>
  </form>
  <AsyncState :loading="loading" :error="error" @retry="refresh">
    <div v-if="!data?.items.length" class="empty-state">
      <Inbox :size="32" />
      <h2>{{ filtered ? "No matching runs" : "No test runs yet" }}</h2>
      <p>
        {{
          filtered
            ? "Try a different branch, status or environment."
            : "Runs will appear after your reporter sends results."
        }}
      </p>
      <button v-if="filtered" class="button" @click="clear">
        Clear filters</button
      ><button v-else class="button" @click="refresh">
        <RefreshCw :size="16" />Refresh
      </button>
    </div>
    <div v-else class="table-scroll">
      <table class="runs-table">
        <caption class="sr-only">
          Test runs, most recent first
        </caption>
        <thead>
          <tr>
            <th scope="col">Run / branch</th>
            <th scope="col">Status</th>
            <th scope="col">Results</th>
            <th scope="col">Duration</th>
            <th scope="col">Started</th>
            <th scope="col"><span class="sr-only">Open run</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="run in data.items" :key="run.id">
            <td>
              <RouterLink
                class="run-link"
                :to="`/p/${encodeURIComponent(project)}/runs/${encodeURIComponent(run.id)}`"
                >{{ run.branch }}</RouterLink
              >
              <div class="run-meta">
                <code>{{ short(run.id) }}</code
                ><span>/</span><code>{{ short(run.commitSha) }}</code
                ><span>/</span><span>{{ run.environmentName }}</span>
              </div>
            </td>
            <td>
              <StatusBadge :status="run.status" :flaky="run.flakyCount" />
            </td>
            <td>
              <div class="result-count">{{ run.totalCount }} tests</div>
              <div
                class="result-bar"
                role="img"
                :aria-label="`${run.passedCount} passed, ${run.failedCount} failed, ${run.flakyCount} flaky of ${run.totalCount} tests`"
              >
                <i
                  v-if="run.passedCount"
                  class="success"
                  :style="{ flex: run.passedCount }"
                /><i
                  v-if="run.failedCount"
                  class="danger"
                  :style="{ flex: run.failedCount }"
                /><i
                  v-if="run.flakyCount"
                  class="warning"
                  :style="{ flex: run.flakyCount }"
                /><i
                  v-if="
                    run.totalCount >
                    run.passedCount + run.failedCount + run.flakyCount
                  "
                  :style="{
                    flex:
                      run.totalCount -
                      run.passedCount -
                      run.failedCount -
                      run.flakyCount,
                  }"
                />
              </div>
            </td>
            <td class="mono">{{ duration(run.durationMs) }}</td>
            <td class="muted">
              <time :datetime="run.startedAt">{{ time(run.startedAt) }}</time>
            </td>
            <td>
              <RouterLink
                class="icon-button"
                :aria-label="`Open run ${run.id}`"
                :to="`/p/${encodeURIComponent(project)}/runs/${encodeURIComponent(run.id)}`"
                ><ArrowUpRight :size="16"
              /></RouterLink>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <footer class="pagination">
      <span>{{ data?.items.length ?? 0 }} runs on this page</span>
      <div>
        <button
          class="icon-button"
          aria-label="First page"
          title="First page"
          :disabled="!query.cursor"
          @click="first"
        >
          <ChevronsLeft :size="18" /></button
        ><button
          class="icon-button"
          aria-label="Next page"
          title="Next page"
          :disabled="!data?.nextCursor"
          @click="next"
        >
          <ChevronRight :size="18" />
        </button>
      </div>
    </footer>
  </AsyncState>
</template>
