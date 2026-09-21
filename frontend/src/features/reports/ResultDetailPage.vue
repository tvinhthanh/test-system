<script setup lang="ts">
import {
  Check,
  CircleAlert,
  ExternalLink,
  Flag,
  History,
  Paperclip,
  RefreshCw,
  UserRoundCheck,
} from "@lucide/vue";
import { useResultDetail } from "./useResultDetail";
import { bytes, duration, time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
import { Button } from "@/components/ui/button";
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
</script>
<template>
  <BackLink :to="`/p/${encodeURIComponent(project)}/runs`">Test runs</BackLink>
  <PageHeader
    eyebrow="Failure detail"
    :title="data?.result.displayName ?? `Result ${resultId}`"
  >
    <template #description>
      <span class="inline-flex flex-wrap items-center gap-2">
        <code v-if="data?.result.filePath" class="text-xs">{{
          data.result.filePath
        }}</code>
        <template v-if="data">
          <StatusBadge :status="data.result.finalStatus" />
          <span class="text-muted-foreground text-xs"
            >{{ data.result.attemptCount }}
            {{ data.result.attemptCount === 1 ? "attempt" : "attempts" }} ·
            {{ duration(data.result.durationMs) }}</span
          >
        </template>
      </span>
    </template>
    <Button
      v-if="data?.result.stableTestId"
      as-child
      variant="outline"
      size="sm"
    >
      <RouterLink
        :to="`/p/${encodeURIComponent(project)}/tests/${encodeURIComponent(data.result.stableTestId)}/history`"
        ><History />Test history</RouterLink
      >
    </Button>
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh result"
      title="Refresh result"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>

  <AsyncState :loading="loading" :error="error" @retry="refresh">
    <template v-if="data">
      <!-- Triage: three decisions, one row, always visible above the fold -->
      <div
        class="bg-card mb-6 flex flex-wrap items-center gap-2 rounded-lg border p-3"
      >
        <span class="text-muted-foreground mr-1 px-1 text-xs font-medium"
          >Triage</span
        >
        <Button
          variant="outline"
          size="sm"
          :disabled="actionBusy"
          @click="emit('MARKED_FLAKY')"
          ><Flag />Mark flaky</Button
        >
        <Button
          variant="outline"
          size="sm"
          :disabled="actionBusy"
          @click="emit('MARKED_KNOWN_ISSUE')"
          ><CircleAlert />Known issue</Button
        >
        <Button size="sm" :disabled="actionBusy" @click="emit('RESOLVED')"
          ><Check />Resolve</Button
        >
        <span
          v-if="actionError"
          class="text-danger ml-auto inline-flex items-center gap-2 text-sm"
          role="alert"
          >The triage action could not be saved.
          <Button variant="ghost" size="xs" @click="refresh">Refresh</Button>
        </span>
      </div>

      <section class="attempts" aria-labelledby="attempts-title">
        <SectionHeading id="attempts-title" title="Attempts"
          ><span class="text-xs">Latest attempt last</span></SectionHeading
        >
        <div class="space-y-6">
          <article
            v-for="attempt in data.attempts"
            :key="attempt.attempt"
            class="attempt space-y-3"
          >
            <div class="flex flex-wrap items-center gap-2">
              <StatusBadge :status="attempt.status" />
              <span class="text-sm font-medium"
                >Attempt {{ attempt.attempt + 1 }}</span
              >
              <span class="text-muted-foreground font-mono text-xs">{{
                duration(attempt.durationMs)
              }}</span>
            </div>
            <TechnicalBlock
              v-if="attempt.message"
              title="Error"
              :text="attempt.message"
            />
            <TechnicalBlock
              v-if="attempt.stack"
              title="Stack trace"
              :text="attempt.stack"
            />
            <p
              v-if="!attempt.message && !attempt.stack"
              class="text-muted-foreground text-sm"
            >
              No error payload for this attempt.
            </p>
            <ul
              v-if="attempt.artifacts.length"
              class="artifact-list grid gap-3 sm:grid-cols-2"
            >
              <li
                v-for="artifact in attempt.artifacts"
                :key="artifact.id"
                class="bg-card overflow-hidden rounded-lg border"
                :class="
                  (artifact.kind === 'screenshot' ||
                    artifact.kind === 'video') &&
                  'sm:col-span-2'
                "
              >
                <div class="flex min-w-0 items-center gap-2 px-3 py-2 text-sm">
                  <Paperclip
                    :size="14"
                    aria-hidden="true"
                    class="text-muted-foreground"
                  />
                  <!-- Report content is untrusted: rendered as text, never HTML.
                       The server sets Content-Disposition; a download attribute
                       is added only for kinds it will not render inline. -->
                  <a
                    :href="artifactUrl(artifact.id)"
                    target="_blank"
                    rel="noopener"
                    class="min-w-0 truncate font-medium hover:underline"
                    :download="
                      inlineKinds.has(artifact.kind) ? null : artifact.name
                    "
                    >{{ artifact.name }}</a
                  >
                  <span class="text-muted-foreground shrink-0 text-xs"
                    >{{ artifact.kind }} · {{ bytes(artifact.sizeBytes) }}</span
                  >
                  <!-- §4.4 P1-min: client-side handling for video + trace. The
                       trace opens in Playwright's viewer served from this origin
                       (/trace/), which fetches the zip with the session cookie;
                       no third-party host ever sees a customer's trace. -->
                  <Button
                    v-if="artifact.kind === 'trace'"
                    as-child
                    variant="secondary"
                    size="xs"
                    class="ml-auto"
                  >
                    <a
                      class="trace-link"
                      :href="traceUrl(artifact.id)"
                      target="_blank"
                      rel="noopener"
                      >Open in trace viewer<ExternalLink
                    /></a>
                  </Button>
                </div>
                <!-- Screenshots and videos are the two inline kinds the server
                     allows (§11.5 allowlist); bytes come from our content route
                     with the declared, validated content type. -->
                <img
                  v-if="artifact.kind === 'screenshot'"
                  class="artifact-media bg-muted block max-h-[32rem] w-full border-t object-contain"
                  :src="artifactUrl(artifact.id)"
                  :alt="artifact.name"
                  loading="lazy"
                  decoding="async"
                />
                <video
                  v-else-if="artifact.kind === 'video'"
                  class="artifact-media block max-h-[32rem] w-full border-t bg-black"
                  :src="artifactUrl(artifact.id)"
                  controls
                  preload="metadata"
                  playsinline
                ></video>
              </li>
            </ul>
          </article>
        </div>
      </section>

      <section class="event-list" aria-labelledby="events-title">
        <SectionHeading id="events-title" title="Triage events" />
        <EmptyState
          v-if="!data.events.length"
          compact
          :icon="UserRoundCheck"
          description="No triage events yet."
        />
        <ol v-else class="relative ml-2 space-y-4 border-l pl-5">
          <li
            v-for="event in data.events"
            :key="event.id"
            class="relative text-sm"
          >
            <span
              class="bg-primary ring-background absolute top-1.5 -left-[1.6rem] size-2.5 rounded-full ring-4"
              aria-hidden="true"
            />
            <strong class="font-medium capitalize">{{
              event.type.replaceAll("_", " ").toLowerCase()
            }}</strong>
            <span class="text-muted-foreground block text-xs"
              >{{ event.actorName }} · {{ time(event.occurredAt) }}</span
            >
          </li>
        </ol>
      </section>
    </template>
  </AsyncState>
</template>
