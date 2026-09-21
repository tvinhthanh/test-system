<script setup lang="ts">
import { Activity, ArrowRight, GitMerge, RefreshCw, Split } from "@lucide/vue";
import { ApiError } from "../../core/api/client";
import { useReviewQueue } from "./useReviewQueue";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import ViewTab from "../../shared/ViewTab.vue";
import ViewTabs from "../../shared/ViewTabs.vue";
import { Button } from "@/components/ui/button";
const { data, loading, error, project, busy, rowError, refresh, decide } =
  useReviewQueue();
const base = () => `/p/${encodeURIComponent(project.value)}`;
const pct = (v: number) => `${Math.round(v * 100)}%`;
const reason = (e: unknown) =>
  e instanceof ApiError ? e.message : "The decision could not be recorded.";
</script>
<template>
  <PageHeader eyebrow="Test intelligence" title="Review queue">
    <template #description>
      Renames the resolver would not decide alone (§9.3). Merging unifies the
      history of the two tests; keeping leaves them separate. Both are audited;
      a wrong merge can be undone from the test's history page.
    </template>
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh review queue"
      title="Refresh review queue"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <ViewTabs label="Test views">
    <ViewTab :to="`${base()}/tests`"><Activity />All tests</ViewTab>
    <ViewTab :to="`${base()}/flaky`">Flaky view</ViewTab>
    <ViewTab :to="`${base()}/review`" selected
      ><GitMerge />Review queue</ViewTab
    >
  </ViewTabs>
  <AsyncState :loading="loading" :error="error" :rows="3" @retry="refresh">
    <EmptyState
      v-if="!data?.length"
      :icon="GitMerge"
      title="Nothing to review"
      description="The resolver queues a pair only when a rename is likely but not certain — a close score with a small gap to the runner-up."
    />
    <ul v-else class="review-list space-y-4">
      <li
        v-for="item in data"
        :key="item.id"
        class="review-item bg-card overflow-hidden rounded-lg border"
      >
        <div
          class="review-pair grid gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] sm:items-center"
        >
          <div class="review-side min-w-0 space-y-1">
            <span
              class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
              >New identity · this run</span
            >
            <RouterLink
              class="block truncate text-sm font-medium hover:underline"
              :to="`${base()}/tests/${encodeURIComponent(item.identity.id)}/history`"
              >{{
                item.identity.canonicalTitle || item.identity.id
              }}</RouterLink
            >
            <code class="text-muted-foreground block truncate text-xs">{{
              item.identity.canonicalFilePath
            }}</code>
          </div>
          <div
            class="review-arrow text-muted-foreground hidden sm:flex"
            aria-hidden="true"
          >
            <ArrowRight :size="18" />
          </div>
          <div class="review-side min-w-0 space-y-1">
            <span
              class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
              >Candidate · same test, renamed?</span
            >
            <RouterLink
              class="block truncate text-sm font-medium hover:underline"
              :to="`${base()}/tests/${encodeURIComponent(item.candidate.id)}/history`"
              >{{
                item.candidate.canonicalTitle || item.candidate.id
              }}</RouterLink
            >
            <code class="text-muted-foreground block truncate text-xs">{{
              item.candidate.canonicalFilePath
            }}</code>
          </div>
        </div>
        <dl
          class="review-facts bg-muted/40 grid grid-cols-2 gap-x-6 gap-y-2 border-y px-4 py-3 text-xs sm:grid-cols-4 lg:grid-cols-6"
        >
          <div>
            <dt class="text-muted-foreground">Match score</dt>
            <dd class="font-mono font-medium tabular-nums">
              {{ pct(item.score) }}
            </dd>
          </div>
          <div>
            <dt class="text-muted-foreground">Gap to runner-up</dt>
            <dd class="font-mono font-medium tabular-nums">
              {{ item.runnerUpId ? pct(item.gap) : "no runner-up" }}
            </dd>
          </div>
          <div v-for="(v, k) in item.breakdown" :key="k">
            <dt class="text-muted-foreground capitalize">
              {{ String(k).replaceAll("_", " ") }}
            </dt>
            <dd class="font-mono font-medium tabular-nums">{{ pct(v) }}</dd>
          </div>
          <div>
            <dt class="text-muted-foreground">Queued</dt>
            <dd class="font-medium">{{ time(item.createdAt) }}</dd>
          </div>
          <div v-if="item.runId">
            <dt class="text-muted-foreground">Run</dt>
            <dd>
              <RouterLink
                class="font-mono font-medium hover:underline"
                :to="`${base()}/runs/${encodeURIComponent(item.runId)}`"
                >{{ item.runId.slice(0, 8) }}</RouterLink
              >
            </dd>
          </div>
        </dl>
        <div class="review-actions flex flex-wrap items-center gap-2 p-3">
          <Button
            type="button"
            size="sm"
            :disabled="busy !== null"
            @click="decide(item, 'merged')"
            ><GitMerge />Same test — merge</Button
          >
          <Button
            type="button"
            variant="outline"
            size="sm"
            :disabled="busy !== null"
            @click="decide(item, 'kept')"
            ><Split />Different tests — keep</Button
          >
          <span
            v-if="rowError?.id === item.id"
            class="text-danger ml-auto text-sm"
            role="alert"
            >{{ reason(rowError.error) }}</span
          >
        </div>
      </li>
    </ul>
  </AsyncState>
</template>
