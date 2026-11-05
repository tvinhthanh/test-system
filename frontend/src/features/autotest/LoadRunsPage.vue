<script setup lang="ts">
import { Activity, ArrowRight, Plus } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import { useResource } from "../../core/state/useResource";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import { time } from "../../shared/format";
import { useProjectPermission } from "../reports/useProjectPermission";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import type { Environment } from "../testmgmt/testmgmtRepository";
import type { Listing, LoadRun } from "./autotestRepository";
import { explainLoadError } from "./loadErrors";
import { buildLoadInput } from "./loadForm";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute();
const router = useRouter();
const { autotest, testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const base = computed(() => `/p/${encodeURIComponent(project.value)}`);
const canWrite = useProjectPermission("testmgmt.write");
const runs = useResource<Listing<LoadRun>>();
const environments = useResource<Environment[]>();
function refresh() {
  void runs.load((signal) => autotest.loadRuns(project.value, signal));
  void environments.load((signal) =>
    testmgmt.environments(project.value, signal),
  );
}
watch(project, refresh, { immediate: true });
const selected = computed({
  get: () => {
    const requested = String(route.query.environment ?? "");
    return environments.data.value?.some((e) => e.id === requested)
      ? requested
      : (environments.data.value?.find((e) => e.baseUrl)?.id ?? "");
  },
  set: (id: string) => {
    void router.replace({
      query: { ...route.query, environment: id || undefined },
    });
  },
});
const site = computed(() =>
  environments.data.value?.find((e) => e.id === selected.value),
);
const ownershipUrl = computed(
  () => `${base.value}/auto/ownership/${encodeURIComponent(selected.value)}`,
);
const paths = ref("/");
const vus = ref("5");
const seconds = ref("30");
const rampSeconds = ref("5");
const maxP95Ms = ref("2500");
const maxErrorRate = ref("1");
const busy = ref(false);
const formError = ref("");
const actionError = ref<unknown>(null);
const featureOff = ref(false);
const enabled = computed(
  () => runs.data.value?.enabled !== false && !featureOff.value,
);
let actionAbort: AbortController | undefined;
onBeforeUnmount(() => actionAbort?.abort());
watch(project, () => {
  featureOff.value = false;
  formError.value = "";
  actionError.value = null;
  actionAbort?.abort();
});

async function start() {
  if (busy.value || !canWrite.value || !enabled.value || !site.value?.baseUrl)
    return;
  actionError.value = null;
  const parsed = buildLoadInput({
    environmentId: selected.value,
    paths: paths.value,
    vus: vus.value,
    seconds: seconds.value,
    rampSeconds: rampSeconds.value,
    maxP95Ms: maxP95Ms.value,
    maxErrorRate: maxErrorRate.value,
  });
  formError.value = parsed.error;
  if (!parsed.input) return;
  busy.value = true;
  const controller = new AbortController();
  actionAbort = controller;
  try {
    const created = await autotest.createLoadRun(
      project.value,
      parsed.input,
      controller.signal,
    );
    if (!controller.signal.aborted)
      await router.push(
        `${base.value}/load-runs/${encodeURIComponent(created.id)}`,
      );
  } catch (error) {
    if (!controller.signal.aborted) {
      actionError.value = explainLoadError(error);
      if (error instanceof ApiError && error.code === "AUTO_TEST_OFF")
        featureOff.value = true;
    }
  } finally {
    if (actionAbort === controller) {
      actionAbort = undefined;
      busy.value = false;
    }
  }
}
</script>

<template>
  <BackLink :to="`${base}/auto`">Auto test</BackLink>
  <PageHeader
    eyebrow="Auto test"
    title="Load tests"
    description="Send controlled GET traffic to a host your organization has proved it owns. Results are kept with this project."
  />
  <p
    class="bg-warning-soft text-warning mb-5 rounded-md border p-3 text-sm"
    role="note"
  >
    Use staging or another safe test environment. Load traffic can affect a live
    service.
  </p>
  <AsyncState
    :loading="runs.loading.value"
    :error="runs.error.value"
    @retry="refresh"
  >
    <template v-if="runs.data.value">
      <p
        v-if="!enabled"
        class="bg-info-soft text-info mb-5 rounded-md border p-3 text-sm"
      >
        Load tests are off on this server. Existing results remain available.
      </p>
      <section
        v-if="canWrite && enabled"
        class="bg-card mb-6 space-y-4 rounded-lg border p-4"
        aria-label="Start load test"
      >
        <div class="flex items-center gap-2">
          <Plus class="size-4" />
          <h2 class="font-semibold">Start a load test</h2>
        </div>
        <AsyncState
          :loading="environments.loading.value"
          :error="environments.error.value"
          :rows="2"
          @retry="
            environments.load((signal) =>
              testmgmt.environments(project, signal),
            )
          "
        >
          <template v-if="environments.data.value">
            <EmptyState
              v-if="!environments.data.value.some((e) => e.baseUrl)"
              :icon="Activity"
              title="No site address yet"
              description="Add an environment with a site address in Auto test before starting a load test."
            >
              <RouterLink
                :to="`${base}/auto`"
                class="text-primary text-sm underline"
                >Set up an environment</RouterLink
              >
            </EmptyState>
            <form v-else class="space-y-4" @submit.prevent="start">
              <div class="grid gap-4 md:grid-cols-3">
                <div class="space-y-1.5 md:col-span-2">
                  <Label for="load-environment">Environment</Label>
                  <NativeSelect
                    id="load-environment"
                    v-model="selected"
                    class="w-full"
                    required
                  >
                    <option
                      v-for="environment in environments.data.value.filter(
                        (e) => e.baseUrl,
                      )"
                      :key="environment.id"
                      :value="environment.id"
                    >
                      {{ environment.key }} · {{ environment.baseUrl }}
                    </option>
                  </NativeSelect>
                </div>
                <div class="flex items-end">
                  <RouterLink
                    v-if="selected"
                    :to="ownershipUrl"
                    class="text-primary inline-flex min-h-9 items-center gap-1 text-sm underline"
                    >Prove host ownership <ArrowRight class="size-4"
                  /></RouterLink>
                </div>
                <div class="space-y-1.5 md:col-span-3">
                  <Label for="load-paths">Paths, one per line</Label
                  ><textarea
                    id="load-paths"
                    v-model="paths"
                    rows="3"
                    maxlength="40020"
                    class="border-input bg-background focus-visible:ring-ring/50 w-full rounded-md border px-3 py-2 text-sm focus-visible:ring-3"
                    placeholder="/&#10;/checkout"
                  />
                  <p class="text-muted-foreground text-xs">
                    Up to 20 paths, each starting with /. Blank uses /.
                  </p>
                </div>
                <div class="space-y-1.5">
                  <Label for="load-vus">Virtual users</Label
                  ><Input
                    id="load-vus"
                    v-model="vus"
                    type="number"
                    min="1"
                    max="200"
                    step="1"
                    required
                  />
                </div>
                <div class="space-y-1.5">
                  <Label for="load-seconds">Duration (seconds)</Label
                  ><Input
                    id="load-seconds"
                    v-model="seconds"
                    type="number"
                    min="5"
                    max="600"
                    step="1"
                    required
                  />
                </div>
                <div class="space-y-1.5">
                  <Label for="load-ramp">Ramp up (seconds)</Label
                  ><Input
                    id="load-ramp"
                    v-model="rampSeconds"
                    type="number"
                    min="0"
                    step="1"
                    required
                  />
                </div>
                <div class="space-y-1.5">
                  <Label for="load-p95">Maximum p95 (ms)</Label
                  ><Input
                    id="load-p95"
                    v-model="maxP95Ms"
                    type="number"
                    min="0"
                    max="600000"
                    step="1"
                  />
                  <p class="text-muted-foreground text-xs">
                    Blank or 0 means no p95 threshold.
                  </p>
                </div>
                <div class="space-y-1.5">
                  <Label for="load-errors">Maximum error rate (%)</Label
                  ><Input
                    id="load-errors"
                    v-model="maxErrorRate"
                    type="number"
                    min="0"
                    max="100"
                    step="0.1"
                  />
                  <p class="text-muted-foreground text-xs">
                    Blank means no error-rate threshold.
                  </p>
                </div>
              </div>
              <p class="text-muted-foreground text-xs">
                The server may have lower limits for virtual users and duration;
                its response will name them.
              </p>
              <p v-if="formError" class="text-danger text-sm" role="alert">
                {{ formError }}
              </p>
              <ErrorNote v-if="actionError" :error="actionError" />
              <RouterLink
                v-if="
                  actionError instanceof ApiError &&
                  actionError.code === 'OWNERSHIP_NOT_PROVED' &&
                  selected
                "
                :to="ownershipUrl"
                class="text-primary block text-sm underline"
                >Open the ownership proof for {{ site?.key }}</RouterLink
              >
              <Button type="submit" :disabled="busy || !site?.baseUrl">{{
                busy ? "Starting…" : "Run load test"
              }}</Button>
            </form>
          </template>
        </AsyncState>
      </section>
      <section aria-label="Recent load tests" class="space-y-3">
        <h2 class="text-base font-semibold">Recent load tests</h2>
        <EmptyState
          v-if="!runs.data.value.items.length"
          :icon="Activity"
          title="No load tests yet"
          :description="
            enabled
              ? 'Prove a staging host, then run a load test to see its latency and error rate.'
              : 'Load tests are currently off on this server.'
          "
        />
        <ul v-else class="bg-card divide-y overflow-hidden rounded-lg border">
          <li v-for="run in runs.data.value.items" :key="run.id">
            <RouterLink
              :to="`${base}/load-runs/${encodeURIComponent(run.id)}`"
              class="hover:bg-accent flex min-h-14 items-center gap-3 px-4 py-3"
            >
              <StatusBadge
                :status="run.status === 'done' ? run.verdict : run.status"
              />
              <span class="min-w-0 flex-1"
                ><strong class="block truncate text-sm"
                  >{{ run.host }} · {{ run.vus }} users ·
                  {{ run.seconds }}s</strong
                ><span class="text-muted-foreground block truncate text-xs"
                  >{{ time(run.createdAt) }} ·
                  {{ run.paths.join(", ") || "/" }}</span
                ></span
              ><ArrowRight class="text-muted-foreground size-4 shrink-0" />
            </RouterLink>
          </li>
        </ul>
      </section>
    </template>
  </AsyncState>
</template>
