<script setup lang="ts">
import { Activity, ChevronRight, GitMerge, RefreshCw } from "@lucide/vue";
import { useIdentities } from "./useIdentities";
import { tone } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import Pagination from "../../shared/Pagination.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ViewTab from "../../shared/ViewTab.vue";
import ViewTabs from "../../shared/ViewTabs.vue";
import { Button } from "@/components/ui/button";
const { data, error, loading, project, flaky, cursor, refresh, next, first } =
  useIdentities();
const base = () => `/p/${encodeURIComponent(project.value)}`;
</script>
<template>
  <PageHeader
    eyebrow="Test intelligence"
    :title="flaky ? 'Flaky tests' : 'Test history'"
    description="Server-computed identity and stability signals — one row per test, across every run and rename."
  >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh test identities"
      title="Refresh test identities"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <ViewTabs label="Test views">
    <ViewTab :to="`${base()}/tests`" :selected="!flaky"
      ><Activity />All tests</ViewTab
    >
    <ViewTab :to="`${base()}/flaky`" :selected="flaky">Flaky view</ViewTab>
    <ViewTab :to="`${base()}/review`"><GitMerge />Review queue</ViewTab>
  </ViewTabs>
  <AsyncState :loading="loading" :error="error" @retry="refresh">
    <EmptyState
      v-if="!data?.items.length"
      :icon="Activity"
      :title="flaky ? 'No flaky tests found' : 'No test identities yet'"
      description="Identity history appears when a run has been resolved."
    />
    <ul
      v-else
      class="identity-list bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li v-for="identity in data.items" :key="identity.id">
        <RouterLink
          :to="`${base()}/tests/${encodeURIComponent(identity.id)}/history`"
          class="identity-row hover:bg-accent focus-visible:bg-accent group grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2 px-4 py-3 transition-colors outline-none md:grid-cols-[minmax(0,1fr)_9rem_auto_4rem_1.25rem]"
        >
          <span class="min-w-0">
            <strong class="block truncate text-sm font-medium">{{
              identity.canonicalTitle
            }}</strong>
            <code class="text-muted-foreground block truncate text-xs">{{
              identity.canonicalFilePath
            }}</code>
          </span>
          <span
            class="col-span-2 flex items-center gap-2 md:col-span-1 md:justify-end"
            :title="`${identity.totalRuns} runs`"
          >
            <span
              class="history-bars flex h-3 items-end gap-px"
              aria-hidden="true"
              ><i
                v-for="(status, index) in identity.recentStatuses"
                :key="`${identity.id}-${index}`"
                class="w-1 rounded-[1px]"
                :class="[`tone-${tone(status)}`, 'h-full']"
                :title="status"
            /></span>
            <small class="text-muted-foreground text-xs tabular-nums"
              >{{ identity.totalRuns }} runs</small
            >
          </span>
          <StatusBadge :status="identity.verdict" />
          <span
            class="score text-muted-foreground text-right font-mono text-xs tabular-nums"
            >{{
              identity.flakyScore === null
                ? "n/a"
                : `${Math.round(identity.flakyScore * 100)}%`
            }}</span
          >
          <ChevronRight
            :size="16"
            aria-hidden="true"
            class="text-muted-foreground hidden transition-transform group-hover:translate-x-0.5 md:block"
          />
        </RouterLink>
      </li>
    </ul>
    <Pagination
      :summary="`${data?.items.length ?? 0} tests on this page`"
      label="test page"
      :can-first="!!cursor"
      :can-next="!!data?.nextCursor"
      @first="first"
      @next="next"
    />
  </AsyncState>
</template>
