<script setup lang="ts">
import {
  ArrowUpRight,
  GitBranch,
  Inbox,
  ListFilter,
  RefreshCw,
  Search,
} from "@lucide/vue";
import { useRuns } from "./useRuns";
import { useProjectPermission } from "./useProjectPermission";
import JUnitImportSheet from "./JUnitImportSheet.vue";
import { duration, short, time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import Pagination from "../../shared/Pagination.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ViewTab from "../../shared/ViewTab.vue";
import ViewTabs from "../../shared/ViewTabs.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
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
// Backfilling history is configuring the project (maintainer and up).
const canImport = useProjectPermission("project.configure");
const statuses = ["running", "passed", "failed", "error", "cancelled"];
const runPath = (id: string) =>
  `/p/${encodeURIComponent(project.value)}/runs/${encodeURIComponent(id)}`;
</script>
<template>
  <PageHeader
    eyebrow="Report Center"
    title="Test runs"
    description="Execution results across your environments, most recent first."
  >
    <JUnitImportSheet v-if="canImport" :project="project" @imported="refresh" />
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh runs"
      title="Refresh runs"
      :disabled="loading"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>

  <ViewTabs label="Run sources">
    <ViewTab selected><GitBranch />All runs</ViewTab>
    <span
      class="text-muted-foreground inline-flex h-10 items-center px-3 text-sm"
      >Playwright</span
    >
  </ViewTabs>

  <form
    class="mb-4 flex flex-col gap-3 md:flex-row md:items-end"
    role="search"
    @submit.prevent
  >
    <label class="relative min-w-0 flex-1">
      <span class="sr-only">Search branch, commit or run</span>
      <Search
        :size="16"
        aria-hidden="true"
        class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2"
      />
      <Input
        v-model="search"
        type="search"
        class="pl-9"
        placeholder="Search branch, commit or run…"
        maxlength="200"
      />
    </label>
    <label class="flex flex-col gap-1.5 text-xs font-medium">
      <span class="text-muted-foreground">Status</span>
      <NativeSelect
        class="w-full md:w-40"
        :model-value="query.status"
        @update:model-value="filter({ status: $event })"
      >
        <option value="">All statuses</option>
        <option v-for="status in statuses" :key="status" :value="status">
          {{ status }}
        </option>
      </NativeSelect>
    </label>
    <label class="flex flex-col gap-1.5 text-xs font-medium">
      <span class="text-muted-foreground">Environment</span>
      <Input
        class="w-full md:w-44"
        :model-value="query.environment"
        placeholder="All environments"
        @change="
          filter({
            environment: ($event.target as HTMLInputElement).value.trim(),
          })
        "
      />
    </label>
    <Button
      v-if="filtered"
      variant="ghost"
      size="sm"
      type="button"
      class="md:mb-0.5"
      @click="clear"
      ><ListFilter />Clear</Button
    >
  </form>

  <AsyncState :loading="loading" :error="error" @retry="refresh">
    <EmptyState
      v-if="!data?.items.length"
      :icon="Inbox"
      :title="filtered ? 'No matching runs' : 'No test runs yet'"
      :description="
        filtered
          ? 'Try a different branch, status or environment.'
          : 'Runs will appear after your reporter sends results.'
      "
    >
      <Button v-if="filtered" variant="outline" size="sm" @click="clear"
        >Clear filters</Button
      >
      <Button v-else variant="outline" size="sm" @click="refresh"
        ><RefreshCw />Refresh</Button
      >
    </EmptyState>
    <div v-else class="table-scroll bg-card overflow-x-auto rounded-lg border">
      <Table class="runs-table min-w-[720px]">
        <TableCaption class="sr-only"
          >Test runs, most recent first</TableCaption
        >
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead>Run / branch</TableHead>
            <TableHead>Status</TableHead>
            <TableHead class="w-44">Results</TableHead>
            <TableHead class="w-28">Duration</TableHead>
            <TableHead class="w-36">Started</TableHead>
            <TableHead class="w-12"
              ><span class="sr-only">Open run</span></TableHead
            >
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="run in data.items" :key="run.id" class="group">
            <TableCell class="py-3">
              <RouterLink
                class="run-link block truncate font-medium hover:underline"
                :to="runPath(run.id)"
                >{{ run.branch || "(no branch)" }}</RouterLink
              >
              <div
                class="run-meta text-muted-foreground mt-0.5 flex items-center gap-1.5 text-xs"
              >
                <code>{{ short(run.id) }}</code>
                <template v-if="run.commitSha"
                  ><span aria-hidden="true">·</span
                  ><code>{{ short(run.commitSha) }}</code></template
                >
                <span aria-hidden="true">·</span>
                <span>{{ run.environmentName }}</span>
              </div>
            </TableCell>
            <TableCell>
              <StatusBadge :status="run.status" :flaky="run.flakyCount" />
            </TableCell>
            <TableCell>
              <div class="text-muted-foreground mb-1.5 text-xs tabular-nums">
                {{ run.totalCount }} tests
              </div>
              <div
                class="result-bar bg-muted flex h-1.5 w-32 gap-px overflow-hidden rounded-full"
                role="img"
                :aria-label="`${run.passedCount} passed, ${run.failedCount} failed, ${run.flakyCount} flaky of ${run.totalCount} tests`"
              >
                <i
                  v-if="run.passedCount"
                  class="tone-success"
                  :style="{ flex: run.passedCount }"
                /><i
                  v-if="run.failedCount"
                  class="tone-danger"
                  :style="{ flex: run.failedCount }"
                /><i
                  v-if="run.flakyCount"
                  class="tone-warning"
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
            </TableCell>
            <TableCell class="font-mono text-xs tabular-nums">{{
              duration(run.durationMs)
            }}</TableCell>
            <TableCell class="text-muted-foreground text-xs">
              <time :datetime="run.startedAt">{{ time(run.startedAt) }}</time>
            </TableCell>
            <TableCell class="text-right">
              <Button
                as-child
                variant="ghost"
                size="icon-sm"
                class="text-muted-foreground group-hover:text-foreground"
              >
                <RouterLink
                  :aria-label="`Open run ${run.id}`"
                  :to="runPath(run.id)"
                  ><ArrowUpRight
                /></RouterLink>
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    <Pagination
      :summary="`${data?.items.length ?? 0} runs on this page`"
      :can-first="!!query.cursor"
      :can-next="!!data?.nextCursor"
      @first="first"
      @next="next"
    />
  </AsyncState>
</template>
