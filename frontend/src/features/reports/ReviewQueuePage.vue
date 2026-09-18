<script setup lang="ts">
import { Activity, GitMerge, RefreshCw, Split } from "@lucide/vue";
import { ApiError } from "../../core/api/client";
import { useReviewQueue } from "./useReviewQueue";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
const { data, loading, error, project, busy, rowError, refresh, decide } =
  useReviewQueue();
const pct = (v: number) => `${Math.round(v * 100)}%`;
const reason = (e: unknown) =>
  e instanceof ApiError ? e.message : "The decision could not be recorded.";
</script>
<template>
  <header class="page-heading">
    <div>
      <p class="eyebrow">TEST INTELLIGENCE</p>
      <h1>Review queue</h1>
      <p class="muted">
        Renames the resolver would not decide alone (§9.3). Merging unifies the
        history of the two tests; keeping leaves them separate. Both are
        audited; a wrong merge can be undone from the test's history page.
      </p>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh review queue"
      title="Refresh review queue"
      @click="refresh"
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <div class="view-tabs">
    <RouterLink :to="`/p/${encodeURIComponent(project)}/tests`"
      ><Activity :size="16" />All tests</RouterLink
    ><RouterLink :to="`/p/${encodeURIComponent(project)}/flaky`"
      >Flaky view</RouterLink
    ><RouterLink
      :to="`/p/${encodeURIComponent(project)}/review`"
      class="selected"
      ><GitMerge :size="16" />Review queue</RouterLink
    >
  </div>
  <AsyncState :loading="loading" :error="error" @retry="refresh">
    <div v-if="!data?.length" class="empty-state">
      <GitMerge :size="32" />
      <h2>Nothing to review</h2>
      <p>
        The resolver queues a pair only when a rename is likely but not certain
        — a close score with a small gap to the runner-up.
      </p>
    </div>
    <ul v-else class="review-list">
      <li v-for="item in data" :key="item.id" class="review-item">
        <div class="review-pair">
          <div class="review-side">
            <span class="quiet-label">New identity (this run)</span>
            <RouterLink
              :to="`/p/${encodeURIComponent(project)}/tests/${encodeURIComponent(item.identity.id)}/history`"
              ><strong>{{
                item.identity.canonicalTitle || item.identity.id
              }}</strong></RouterLink
            >
            <code>{{ item.identity.canonicalFilePath }}</code>
          </div>
          <div class="review-arrow" aria-hidden="true">
            <GitMerge :size="18" />
          </div>
          <div class="review-side">
            <span class="quiet-label">Candidate (same test, renamed?)</span>
            <RouterLink
              :to="`/p/${encodeURIComponent(project)}/tests/${encodeURIComponent(item.candidate.id)}/history`"
              ><strong>{{
                item.candidate.canonicalTitle || item.candidate.id
              }}</strong></RouterLink
            >
            <code>{{ item.candidate.canonicalFilePath }}</code>
          </div>
        </div>
        <dl class="review-facts">
          <div>
            <dt>Match score</dt>
            <dd>{{ pct(item.score) }}</dd>
          </div>
          <div>
            <dt>Gap to runner-up</dt>
            <dd>
              {{ item.runnerUpId ? pct(item.gap) : "no runner-up" }}
            </dd>
          </div>
          <div v-for="(v, k) in item.breakdown" :key="k">
            <dt>{{ k }}</dt>
            <dd>{{ pct(v) }}</dd>
          </div>
          <div>
            <dt>Queued</dt>
            <dd>{{ time(item.createdAt) }}</dd>
          </div>
          <div v-if="item.runId">
            <dt>Run</dt>
            <dd>
              <RouterLink
                :to="`/p/${encodeURIComponent(project)}/runs/${encodeURIComponent(item.runId)}`"
                ><code>{{ item.runId.slice(0, 8) }}</code></RouterLink
              >
            </dd>
          </div>
        </dl>
        <div class="review-actions">
          <button
            type="button"
            class="button"
            :disabled="busy !== null"
            @click="decide(item, 'merged')"
          >
            <GitMerge :size="16" />Same test — merge
          </button>
          <button
            type="button"
            class="button quiet"
            :disabled="busy !== null"
            @click="decide(item, 'kept')"
          >
            <Split :size="16" />Different tests — keep
          </button>
          <span
            v-if="rowError?.id === item.id"
            class="error-text"
            role="alert"
            >{{ reason(rowError.error) }}</span
          >
        </div>
      </li>
    </ul>
  </AsyncState>
</template>
