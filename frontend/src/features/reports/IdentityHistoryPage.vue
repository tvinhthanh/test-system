<script setup lang="ts">
import { ArrowLeft, Calendar, RefreshCw } from "@lucide/vue";
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import AsyncState from "../../shared/AsyncState.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
const route = useRoute();
const { reports } = useServices();
const project = computed(() => String(route.params.project));
const identityId = computed(() => String(route.params.identity));
const identity = useResource<Awaited<ReturnType<typeof reports.identity>>>();
const history = useResource<Awaited<ReturnType<typeof reports.runs>>>();
const identityData = computed(() => identity.data.value);
const historyData = computed(() => history.data.value);
function refresh() {
  void identity.load((signal) =>
    reports.identity(project.value, identityId.value, signal),
  );
  void history.load((signal) =>
    reports.identityHistory(project.value, identityId.value, "", signal),
  );
}
refresh();
</script>
<template>
  <RouterLink class="back-link" :to="`/p/${encodeURIComponent(project)}/tests`"
    ><ArrowLeft :size="16" />Test history</RouterLink
  >
  <header class="page-heading">
    <div>
      <p class="eyebrow">IDENTITY HISTORY</p>
      <h1>{{ identityData?.canonicalTitle ?? identityId }}</h1>
      <p class="muted">
        <code>{{ identityData?.canonicalFilePath }}</code>
      </p>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh identity history"
      title="Refresh identity history"
      @click="refresh"
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <AsyncState
    :loading="identity.loading.value"
    :error="identity.error.value"
    @retry="refresh"
    ><template v-if="identityData"
      ><StatusBadge :status="identityData.verdict" />
      <div class="metrics">
        <div>
          <span>Runs</span><strong>{{ identityData.totalRuns }}</strong>
        </div>
        <div>
          <span>Flaky score</span
          ><strong>{{
            identityData.flakyScore === null
              ? "N/A"
              : `${Math.round(identityData.flakyScore * 100)}%`
          }}</strong>
        </div>
      </div>
      <div class="history-large">
        <i
          v-for="(status, index) in identityData.recentStatuses"
          :key="`${status}-${index}`"
          :class="status.toLowerCase().replace('_', '-')"
          :title="status"
        /></div></template
  ></AsyncState>
  <div class="section-heading">
    <h2>Recent runs</h2>
    <Calendar :size="18" />
  </div>
  <AsyncState
    :loading="history.loading.value"
    :error="history.error.value"
    @retry="refresh"
    ><div v-if="!historyData?.items.length" class="empty-state compact">
      <p>No matching history runs.</p>
    </div>
    <div v-else class="history-runs">
      <RouterLink
        v-for="run in historyData.items"
        :key="run.id"
        class="event-row"
        :to="`/p/${encodeURIComponent(project)}/runs/${encodeURIComponent(run.id)}`"
        ><StatusBadge :status="run.status" :flaky="run.flakyCount" /><span
          >{{ run.branch }} / {{ run.environmentName }}</span
        ><span class="muted">{{ run.startedAt }}</span></RouterLink
      >
    </div></AsyncState
  >
</template>
