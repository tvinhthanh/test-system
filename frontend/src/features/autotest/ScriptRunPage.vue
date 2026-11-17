<script setup lang="ts">
import { FileArchive } from "@lucide/vue";
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
import type { Listing, ScriptRun } from "./autotestRepository";
import { usePolledDetail } from "./usePolledDetail";
import { explainScriptError } from "./scriptErrors";
const route = useRoute(),
  { autotest } = useServices();
const projectId = computed(() => String(route.params.project)),
  id = computed(() => String(route.params.scriptRun));
const base = computed(() => `/p/${encodeURIComponent(projectId.value)}`);
const detail = usePolledDetail(
  projectId,
  id,
  (p, key, signal) => autotest.scriptRun(p, key, signal),
  (run) => run.status === "queued" || run.status === "running",
);
const availability = useResource<Listing<ScriptRun>>();
watch(
  projectId,
  () =>
    void availability.load((signal) =>
      autotest.scriptRuns(projectId.value, signal),
    ),
  { immediate: true },
);
const status = computed(() =>
  detail.data.value?.status === "done"
    ? detail.data.value.verdict
    : (detail.data.value?.status ?? "queued"),
);
</script>
<template>
  <BackLink :to="`${base}/script-runs`">Script runs</BackLink>
  <PageHeader
    eyebrow="Auto test / script run"
    :title="
      detail.data.value
        ? `${detail.data.value.kind} · ${detail.data.value.base}`
        : 'Script run'
    "
  />
  <p
    v-if="availability.data.value?.enabled === false"
    class="bg-info-soft text-info mb-4 rounded-md border p-3 text-sm"
  >
    Script runs are off on this server. This earlier result remains available.
  </p>
  <AsyncState
    :loading="detail.loading.value"
    :error="detail.error.value"
    @retry="detail.reload"
  >
    <template v-if="detail.data.value"
      ><div class="mb-5 flex flex-wrap items-center gap-2">
        <StatusBadge :status="status" /><span
          class="text-muted-foreground text-sm"
          >Requested {{ time(detail.data.value.createdAt) }} by
          {{ detail.data.value.requestedBy }}</span
        ><span
          v-if="
            detail.data.value.status === 'queued' ||
            detail.data.value.status === 'running'
          "
          class="text-muted-foreground text-sm"
          >Refreshing while it runs…</span
        >
      </div>
      <p
        v-if="detail.pollError.value"
        class="bg-warning-soft text-warning mb-4 rounded-md border p-3 text-sm"
      >
        {{ (explainScriptError(detail.pollError.value) as Error).message }}
      </p>
      <p
        v-if="detail.data.value.error"
        class="bg-danger-soft text-danger mb-4 rounded-md border p-3 text-sm"
        role="alert"
      >
        {{ detail.data.value.error }}
      </p>
      <div class="mb-5 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatTile label="Tests" :value="detail.data.value.tests" /><StatTile
          label="Failures"
          :value="detail.data.value.failures"
        /><StatTile
          label="Exit code"
          :value="detail.data.value.exitCode ?? 'Not finished'"
        /><StatTile
          label="Timed out"
          :value="detail.data.value.timedOut ? 'Yes' : 'No'"
        />
      </div>
      <section class="mb-5 space-y-2 rounded-lg border p-4 text-sm">
        <h2 class="font-semibold">Run setup</h2>
        <p>
          {{ detail.data.value.entry || "Default entry" }} · timeout
          {{ detail.data.value.timeoutSeconds }}s ·
          {{ (detail.data.value.archiveBytes / 1024).toFixed(1) }} KiB
        </p>
        <p v-if="detail.data.value.kind === 'k6'">
          {{ detail.data.value.vus }} users ·
          {{ detail.data.value.seconds }} seconds
        </p>
        <p v-else>
          {{ detail.data.value.project || "All Playwright projects"
          }}<span v-if="detail.data.value.grep">
            · grep {{ detail.data.value.grep }}</span
          >
        </p>
        <RouterLink
          v-if="detail.data.value.testRunId"
          :to="`${base}/runs/${encodeURIComponent(detail.data.value.testRunId)}`"
          class="text-primary inline-block underline"
          >Open test run</RouterLink
        >
      </section>
      <TechnicalBlock
        v-if="detail.data.value.log"
        title="Log tail"
        :text="detail.data.value.log"
      />
      <EmptyState
        v-else
        :icon="FileArchive"
        compact
        title="No log output yet"
        :description="
          detail.data.value.status === 'queued' ||
          detail.data.value.status === 'running'
            ? 'Output appears here while the script runs.'
            : 'This run did not produce log output.'
        "
      />
    </template>
  </AsyncState>
</template>
