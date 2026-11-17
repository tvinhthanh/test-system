<script setup lang="ts">
import { Activity } from "@lucide/vue";
import { computed, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatTile from "../../shared/StatTile.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
import { time } from "../../shared/format";
import type { Listing, LoadRun } from "./autotestRepository";
import { explainLoadError } from "./loadErrors";
import LoadTimeline from "./LoadTimeline.vue";
import { usePolledDetail } from "./usePolledDetail";
import { Button } from "@/components/ui/button";

const route = useRoute();
const { autotest } = useServices();
const project = computed(() => String(route.params.project));
const runId = computed(() => String(route.params.loadRun));
const base = computed(() => `/p/${encodeURIComponent(project.value)}`);
const detail = usePolledDetail(
  project,
  runId,
  (p, id, signal) => autotest.loadRun(p, id, signal),
  (value) => value.status === "queued" || value.status === "running",
);
// Detail has no enabled field; the bounded list is the server's feature flag.
const availability = useResource<Listing<LoadRun>>();
watch(
  project,
  () =>
    void availability.load((signal) =>
      autotest.loadRuns(project.value, signal),
    ),
  { immediate: true },
);
const result = computed(() => detail.data.value?.result);
</script>

<template>
  <BackLink :to="`${base}/load-runs`">Load tests</BackLink>
  <PageHeader
    eyebrow="Auto test / load test"
    :title="detail.data.value?.host || 'Load test'"
  >
    <template #description
      ><span v-if="detail.data.value" class="flex flex-wrap items-center gap-2"
        ><StatusBadge
          :status="
            detail.data.value.status === 'done'
              ? detail.data.value.verdict
              : detail.data.value.status
          "
        /><span
          >Started {{ time(detail.data.value.createdAt) }} by
          {{ detail.data.value.requestedBy }}</span
        ></span
      ></template
    >
  </PageHeader>
  <p
    class="bg-warning-soft text-warning mb-5 rounded-md border p-3 text-sm"
    role="note"
  >
    Load tests belong on staging or another safe test environment. Running
    against production can affect real users.
  </p>
  <p
    v-if="availability.data.value?.enabled === false"
    class="bg-info-soft text-info mb-4 rounded-md border p-3 text-sm"
  >
    Load tests are off on this server. This earlier result remains available.
  </p>
  <div v-if="availability.error.value" class="mb-4 text-sm">
    <span class="text-danger"
      >Could not check whether load tests are enabled.</span
    >
    <Button
      variant="ghost"
      size="sm"
      @click="availability.load((signal) => autotest.loadRuns(project, signal))"
      >Retry</Button
    >
  </div>
  <AsyncState
    :loading="detail.loading.value"
    :error="detail.error.value"
    @retry="detail.reload"
  >
    <template v-if="detail.data.value">
      <p
        v-if="detail.pollError.value"
        class="bg-warning-soft text-warning mb-4 rounded-md border p-3 text-sm"
        role="alert"
      >
        {{ (explainLoadError(detail.pollError.value) as Error).message }}
        <Button variant="ghost" size="sm" @click="detail.reload"
          >Retry now</Button
        >
      </p>
      <div class="mb-5 flex flex-wrap items-center gap-2">
        <StatusBadge
          :status="
            detail.data.value.status === 'done'
              ? detail.data.value.verdict
              : detail.data.value.status
          "
        /><span
          v-if="
            detail.data.value.status === 'queued' ||
            detail.data.value.status === 'running'
          "
          class="text-muted-foreground text-sm"
          >Refreshing while the test runs…</span
        ><span
          v-if="detail.data.value.finishedAt"
          class="text-muted-foreground text-sm"
          >Finished {{ time(detail.data.value.finishedAt) }}</span
        >
      </div>
      <p
        v-if="detail.data.value.error"
        class="bg-danger-soft text-danger mb-4 rounded-md border p-3 text-sm"
        role="alert"
      >
        {{ detail.data.value.error }}
      </p>
      <section class="mb-5 space-y-2 rounded-lg border p-4 text-sm">
        <h2 class="font-semibold">Run setup</h2>
        <p>
          {{ detail.data.value.vus }} virtual users ·
          {{ detail.data.value.seconds }} seconds ·
          {{ detail.data.value.rampSeconds }} second ramp
        </p>
        <p class="text-muted-foreground break-all">
          Paths: {{ detail.data.value.paths.join(", ") || "/" }}
        </p>
        <p class="text-muted-foreground">
          Thresholds: p95
          {{
            detail.data.value.maxP95Ms === null
              ? "none"
              : `${detail.data.value.maxP95Ms} ms`
          }}
          · errors
          {{
            detail.data.value.maxErrorRate === null
              ? "none"
              : `${detail.data.value.maxErrorRate}%`
          }}
        </p>
        <RouterLink
          :to="`${base}/auto/ownership/${encodeURIComponent(detail.data.value.environmentId)}`"
          class="text-primary inline-block underline"
          >View host ownership</RouterLink
        >
      </section>
      <section v-if="detail.data.value.verdict" class="mb-5 space-y-2">
        <h2 class="text-base font-semibold">Verdict</h2>
        <StatusBadge :status="detail.data.value.verdict" />
        <ul
          v-if="detail.data.value.reasons.length"
          class="list-disc space-y-1 pl-5 text-sm"
        >
          <li v-for="reason in detail.data.value.reasons" :key="reason">
            {{ reason }}
          </li>
        </ul>
        <p v-else class="text-muted-foreground text-sm">
          All configured thresholds passed.
        </p>
      </section>
      <template v-if="result">
        <div class="mb-5 grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatTile label="Requests" :value="result.requests" /><StatTile
            label="Requests / second"
            :value="result.rps"
          /><StatTile
            label="Errors"
            :value="result.errors"
            note="No answer or 5xx"
          /><StatTile label="Error rate" :value="`${result.errorRate}%`" />
        </div>
        <div class="mb-5 grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatTile
            label="p50"
            :value="`${result.latencyMs.p50} ms`"
          /><StatTile
            label="p90"
            :value="`${result.latencyMs.p90} ms`"
          /><StatTile
            label="p95"
            :value="`${result.latencyMs.p95} ms`"
          /><StatTile label="p99" :value="`${result.latencyMs.p99} ms`" />
        </div>
        <p class="text-muted-foreground mb-5 text-sm">
          Latency range: {{ result.latencyMs.min }}–{{
            result.latencyMs.max
          }}
          ms · {{ result.clientErrors }} client errors (4xx), counted separately
          from the error rate.
        </p>
        <LoadTimeline :result="result" class="mb-5" />
        <section class="mb-5 space-y-3">
          <h2 class="text-base font-semibold">HTTP statuses</h2>
          <EmptyState
            v-if="!Object.keys(result.statuses).length"
            :icon="Activity"
            compact
            title="No responses"
            description="The test did not record an HTTP response."
          />
          <div v-else class="overflow-x-auto rounded-lg border">
            <table class="w-full text-left text-sm">
              <caption class="sr-only">
                HTTP response counts
              </caption>
              <thead class="bg-muted/60 text-muted-foreground">
                <tr>
                  <th scope="col" class="px-3 py-2">Status</th>
                  <th scope="col" class="px-3 py-2">Requests</th>
                </tr>
              </thead>
              <tbody class="divide-y">
                <tr v-for="(count, code) in result.statuses" :key="code">
                  <td class="px-3 py-2">{{ code }}</td>
                  <td class="px-3 py-2 tabular-nums">{{ count }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
        <TechnicalBlock
          v-if="result.errorSamples.length"
          title="Error samples"
          :text="result.errorSamples.join('\n')"
        />
      </template>
      <EmptyState
        v-else-if="
          detail.data.value.status === 'queued' ||
          detail.data.value.status === 'running'
        "
        :icon="Activity"
        compact
        title="Waiting for measurements"
        description="The chart and latency values appear when the load test finishes."
      />
    </template>
  </AsyncState>
</template>
