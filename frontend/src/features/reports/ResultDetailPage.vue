<script setup lang="ts">
import {
  ArrowLeft,
  Check,
  CircleAlert,
  Flag,
  Paperclip,
  RefreshCw,
  UserRoundCheck,
} from "@lucide/vue";
import { useResultDetail } from "./useResultDetail";
import { bytes, duration, time } from "../../shared/format";
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
  artifactUrl,
  traceUrl,
} = useResultDetail();
// Only these kinds are ever rendered inline by the server (§11.5 allowlist);
// the rest download, so the link says so up front.
const inlineKinds = new Set(["screenshot", "video", "log"]);
const isMedia = (kind: string) => kind === "screenshot" || kind === "video";
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
          <ul v-if="attempt.artifacts.length" class="artifact-list">
            <li
              v-for="artifact in attempt.artifacts"
              :key="artifact.id"
              :class="{ media: isMedia(artifact.kind) }"
            >
              <div class="artifact-row">
                <Paperclip :size="14" />
                <!-- Report content is untrusted: rendered as text, never HTML.
                     The server sets Content-Disposition; a download attribute
                     is added only for kinds it will not render inline. -->
                <a
                  :href="artifactUrl(artifact.id)"
                  target="_blank"
                  rel="noopener"
                  :download="
                    inlineKinds.has(artifact.kind) ? null : artifact.name
                  "
                  >{{ artifact.name }}</a
                >
                <span class="muted"
                  >{{ artifact.kind }} / {{ bytes(artifact.sizeBytes) }}</span
                >
                <!-- §4.4 P1-min: client-side handling for video + trace. The
                     trace opens in Playwright's viewer served from this origin
                     (/trace/), which fetches the zip with the session cookie;
                     no third-party host ever sees a customer's trace. -->
                <a
                  v-if="artifact.kind === 'trace'"
                  class="trace-link"
                  :href="traceUrl(artifact.id)"
                  target="_blank"
                  rel="noopener"
                  >Open in trace viewer</a
                >
              </div>
              <!-- Screenshots and videos are the two inline kinds the server
                   allows (§11.5 allowlist); bytes come from our content route
                   with the declared, validated content type. -->
              <img
                v-if="artifact.kind === 'screenshot'"
                class="artifact-media"
                :src="artifactUrl(artifact.id)"
                :alt="artifact.name"
                loading="lazy"
                decoding="async"
              />
              <video
                v-else-if="artifact.kind === 'video'"
                class="artifact-media"
                :src="artifactUrl(artifact.id)"
                controls
                preload="metadata"
                playsinline
              ></video>
            </li>
          </ul>
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
