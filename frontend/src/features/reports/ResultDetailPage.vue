<script setup lang="ts">
import {
  ArrowLeft,
  Check,
  CircleAlert,
  Flag,
  RefreshCw,
  UserRoundCheck,
} from "@lucide/vue";
import { useResultDetail } from "./useResultDetail";
import { duration, time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
const {
  data,
  error,
  loading,
  project,
  resultId,
  actionBusy,
  actionError,
  refresh,
  emit,
} = useResultDetail();
</script>
<template>
  <RouterLink class="back-link" :to="`/p/${encodeURIComponent(project)}/runs`"
    ><ArrowLeft :size="16" />Test runs</RouterLink
  >
  <header class="page-heading">
    <div>
      <p class="eyebrow">FAILURE DETAIL</p>
      <h1>{{ data?.result.displayName ?? `Result ${resultId}` }}</h1>
      <p v-if="data?.result.filePath" class="muted">
        <code>{{ data.result.filePath }}</code>
      </p>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh result"
      title="Refresh result"
      @click="refresh"
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <AsyncState :loading="loading" :error="error" @retry="refresh"
    ><template v-if="data">
      <div class="detail-status">
        <StatusBadge :status="data.result.finalStatus" /><span
          >{{ data.result.attemptCount }} attempts /
          {{ duration(data.result.durationMs) }}</span
        ><RouterLink
          v-if="data.result.stableTestId"
          class="text-link"
          :to="`/p/${encodeURIComponent(project)}/tests/${encodeURIComponent(data.result.stableTestId)}/history`"
          >View test history</RouterLink
        >
      </div>
      <div v-if="actionError" class="inline-error" role="alert">
        The triage action could not be saved.
        <button class="button quiet" @click="refresh">Refresh</button>
      </div>
      <div class="triage-actions">
        <span class="action-label">Triage</span
        ><button
          class="button"
          :disabled="actionBusy"
          @click="emit('MARKED_FLAKY')"
        >
          <Flag :size="16" />Mark flaky</button
        ><button
          class="button"
          :disabled="actionBusy"
          @click="emit('MARKED_KNOWN_ISSUE')"
        >
          <CircleAlert :size="16" />Known issue</button
        ><button
          class="button"
          :disabled="actionBusy"
          @click="emit('RESOLVED')"
        >
          <Check :size="16" />Resolve
        </button>
      </div>
      <section class="attempts">
        <div class="section-heading">
          <h2>Attempts</h2>
          <span class="muted">Latest attempt last</span>
        </div>
        <article
          v-for="attempt in data.attempts"
          :key="attempt.attempt"
          class="attempt"
        >
          <div class="attempt-heading">
            <StatusBadge :status="attempt.status" /><span
              >Attempt {{ attempt.attempt + 1 }} /
              {{ duration(attempt.durationMs) }}</span
            >
          </div>
          <TechnicalBlock
            v-if="attempt.message"
            title="Error"
            :text="attempt.message"
          /><TechnicalBlock
            v-if="attempt.stack"
            title="Stack trace"
            :text="attempt.stack"
          />
          <p v-if="!attempt.message && !attempt.stack" class="muted">
            No error payload for this attempt.
          </p>
        </article>
      </section>
      <section class="event-list">
        <div class="section-heading"><h2>Triage events</h2></div>
        <div v-if="!data.events.length" class="empty-state compact">
          <UserRoundCheck :size="26" />
          <p>No triage events yet.</p>
        </div>
        <div v-for="event in data.events" :key="event.id" class="event-row">
          <span class="event-dot" />
          <div>
            <strong>{{ event.type.replaceAll("_", " ") }}</strong
            ><span>{{ event.actorName }} / {{ time(event.occurredAt) }}</span>
          </div>
        </div>
      </section>
    </template></AsyncState
  >
</template>
