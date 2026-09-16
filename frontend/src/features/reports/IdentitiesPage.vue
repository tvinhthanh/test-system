<script setup lang="ts">
import { Activity, ChevronRight, ChevronsLeft, RefreshCw } from "@lucide/vue";
import { useIdentities } from "./useIdentities";
import { tone } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
const { data, error, loading, project, flaky, cursor, refresh, next, first } =
  useIdentities();
</script>
<template>
  <header class="page-heading">
    <div>
      <p class="eyebrow">TEST INTELLIGENCE</p>
      <h1>{{ flaky ? "Flaky tests" : "Test history" }}</h1>
      <p class="muted">Server-computed identity and stability signals</p>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh test identities"
      title="Refresh test identities"
      @click="refresh"
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <div class="view-tabs">
    <RouterLink
      :to="`/p/${encodeURIComponent(project)}/tests`"
      :class="{ selected: !flaky }"
      ><Activity :size="16" />All tests</RouterLink
    ><RouterLink
      :to="`/p/${encodeURIComponent(project)}/flaky`"
      :class="{ selected: flaky }"
      >Flaky view</RouterLink
    >
  </div>
  <AsyncState :loading="loading" :error="error" @retry="refresh"
    ><div v-if="!data?.items.length" class="empty-state">
      <Activity :size="32" />
      <h2>{{ flaky ? "No flaky tests found" : "No test identities yet" }}</h2>
      <p>Identity history appears when a run has been resolved.</p>
    </div>
    <div v-else class="identity-list">
      <RouterLink
        v-for="identity in data.items"
        :key="identity.id"
        class="identity-row"
        :to="`/p/${encodeURIComponent(project)}/tests/${encodeURIComponent(identity.id)}/history`"
        ><div class="identity-main">
          <strong>{{ identity.canonicalTitle }}</strong
          ><code>{{ identity.canonicalFilePath }}</code>
        </div>
        <div class="identity-history">
          <span class="history-bars"
            ><i
              v-for="(status, index) in identity.recentStatuses"
              :key="`${identity.id}-${index}`"
              :class="tone(status)"
              :title="status" /></span
          ><small>{{ identity.totalRuns }} runs</small>
        </div>
        <StatusBadge :status="identity.verdict" /><span class="score">{{
          identity.flakyScore === null
            ? "Insufficient data"
            : `${Math.round(identity.flakyScore * 100)}%`
        }}</span
        ><ChevronRight :size="18"
      /></RouterLink>
    </div>
    <footer class="pagination">
      <span>{{ data?.items.length ?? 0 }} tests on this page</span>
      <div>
        <button
          class="icon-button"
          aria-label="First test page"
          title="First test page"
          :disabled="!cursor"
          @click="first"
        >
          <ChevronsLeft :size="18" /></button
        ><button
          class="icon-button"
          aria-label="Next test page"
          title="Next test page"
          :disabled="!data?.nextCursor"
          @click="next"
        >
          <ChevronRight :size="18" />
        </button>
      </div></footer
  ></AsyncState>
</template>
