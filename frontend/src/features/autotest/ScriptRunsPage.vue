<script setup lang="ts">
import { ArrowRight, FileArchive } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ApiError } from "../../core/api/client";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import { time } from "../../shared/format";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { useProjectPermission } from "../reports/useProjectPermission";
import type { Listing, ScriptRun } from "./autotestRepository";
import { buildScriptMultipart, type ScriptKind } from "./scriptForm";
import { explainScriptError } from "./scriptErrors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute(),
  router = useRouter();
const { autotest, testmgmt } = useServices();
const projectId = computed(() => String(route.params.project));
const base = computed(() => `/p/${encodeURIComponent(projectId.value)}`);
const canWrite = useProjectPermission("testmgmt.write");
const runs = useResource<Listing<ScriptRun>>(),
  environments =
    useResource<Awaited<ReturnType<typeof testmgmt.environments>>>();
const kind = ref<ScriptKind>("playwright"),
  environmentId = ref(""),
  archive = ref<File | null>(null);
const entry = ref(""),
  browserProject = ref(""),
  grep = ref(""),
  vus = ref("10"),
  seconds = ref("30"),
  timeoutSeconds = ref("");
const busy = ref(false),
  formError = ref(""),
  actionError = ref<unknown>(null);
let actionAbort: AbortController | undefined;
const enabled = computed(() => runs.data.value?.enabled !== false);
const ownershipUrl = computed(
  () =>
    `${base.value}/auto/ownership/${encodeURIComponent(environmentId.value)}`,
);
function refresh() {
  void runs.load((signal) => autotest.scriptRuns(projectId.value, signal));
}
watch(
  projectId,
  () => {
    refresh();
    void environments.load((signal) =>
      testmgmt.environments(projectId.value, signal),
    );
    actionAbort?.abort();
    actionError.value = null;
    formError.value = "";
  },
  { immediate: true },
);
onBeforeUnmount(() => actionAbort?.abort());
function choose(event: Event) {
  archive.value = (event.target as HTMLInputElement).files?.[0] ?? null;
}
async function submit() {
  const built = buildScriptMultipart({
    archive: archive.value,
    kind: kind.value,
    environmentId: environmentId.value,
    entry: entry.value,
    project: browserProject.value,
    grep: grep.value,
    vus: vus.value,
    seconds: seconds.value,
    timeoutSeconds: timeoutSeconds.value,
  });
  formError.value = built.error;
  actionError.value = null;
  if (!built.data || busy.value || !canWrite.value || !enabled.value) return;
  busy.value = true;
  const controller = new AbortController();
  actionAbort = controller;
  try {
    const made = await autotest.createScriptRun(
      projectId.value,
      built.data,
      controller.signal,
    );
    await router.push(
      `${base.value}/script-runs/${encodeURIComponent(made.id)}`,
    );
  } catch (error) {
    if (!controller.signal.aborted)
      actionError.value = explainScriptError(error);
  } finally {
    if (actionAbort === controller) {
      busy.value = false;
      actionAbort = undefined;
    }
  }
}
</script>

<template>
  <BackLink :to="`${base}/auto`">Auto test</BackLink>
  <PageHeader
    eyebrow="Auto test"
    title="Script runs"
    description="Upload your team's Playwright or k6 project and run it against one configured environment."
  />
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
        Script runs are off on this server. Existing results remain available.
      </p>
      <section
        v-if="canWrite && enabled"
        class="bg-card mb-6 space-y-4 rounded-lg border p-4"
        aria-label="Upload script run"
      >
        <h2 class="flex items-center gap-2 font-semibold">
          <FileArchive class="size-4" />Run an archive
        </h2>
        <p class="text-muted-foreground text-sm">
          <code>tar -czf tests.tar.gz .</code> from the project folder. Scripts
          read <code>BASE_URL</code> and can reach only the selected environment
          host.
        </p>
        <AsyncState
          :loading="environments.loading.value"
          :error="environments.error.value"
          @retry="
            environments.load((signal) =>
              testmgmt.environments(projectId, signal),
            )
          "
        >
          <EmptyState
            v-if="
              environments.data.value &&
              !environments.data.value.some((item) => item.baseUrl)
            "
            :icon="FileArchive"
            title="No site address yet"
            description="Add an environment with a site address in Auto test before uploading a script."
          >
            <RouterLink
              :to="`${base}/auto`"
              class="text-primary text-sm underline"
              >Set up an environment</RouterLink
            >
          </EmptyState>
          <form
            v-else-if="environments.data.value"
            class="space-y-4"
            @submit.prevent="submit"
          >
            <div class="grid gap-4 md:grid-cols-2">
              <div class="space-y-1.5">
                <Label for="script-archive">Archive</Label
                ><Input
                  id="script-archive"
                  type="file"
                  accept=".tar.gz,application/gzip"
                  required
                  @change="choose"
                />
                <p class="text-muted-foreground text-xs">
                  .tar.gz, at most 10 MB
                </p>
              </div>
              <div class="space-y-1.5">
                <Label for="script-kind">Runner</Label
                ><NativeSelect id="script-kind" v-model="kind" class="w-full"
                  ><option value="playwright">Playwright</option>
                  <option value="k6">k6</option></NativeSelect
                >
              </div>
              <div class="space-y-1.5 md:col-span-2">
                <Label for="script-environment">Environment</Label
                ><NativeSelect
                  id="script-environment"
                  v-model="environmentId"
                  class="w-full"
                  required
                  ><option value="">Choose an environment</option>
                  <option
                    v-for="environment in environments.data.value.filter(
                      (item) => item.baseUrl,
                    )"
                    :key="environment.id"
                    :value="environment.id"
                  >
                    {{ environment.key }} · {{ environment.baseUrl }}
                  </option></NativeSelect
                >
              </div>
              <div class="space-y-1.5">
                <Label for="script-entry">{{
                  kind === "k6" ? "Entry script" : "Config path (optional)"
                }}</Label
                ><Input
                  id="script-entry"
                  v-model="entry"
                  maxlength="200"
                  :required="kind === 'k6'"
                  placeholder="tests/main.js"
                />
              </div>
              <template v-if="kind === 'playwright'"
                ><div class="space-y-1.5">
                  <Label for="script-project">Playwright project</Label
                  ><Input
                    id="script-project"
                    v-model="browserProject"
                    maxlength="100"
                  />
                </div>
                <div class="space-y-1.5">
                  <Label for="script-grep">Test grep</Label
                  ><Input
                    id="script-grep"
                    v-model="grep"
                    maxlength="100"
                  /></div
              ></template>
              <template v-else
                ><div class="space-y-1.5">
                  <Label for="script-vus">Virtual users</Label
                  ><Input
                    id="script-vus"
                    v-model="vus"
                    type="number"
                    min="1"
                    max="200"
                    required
                  />
                </div>
                <div class="space-y-1.5">
                  <Label for="script-seconds">Duration (seconds)</Label
                  ><Input
                    id="script-seconds"
                    v-model="seconds"
                    type="number"
                    min="5"
                    max="600"
                    required
                  /></div
              ></template>
              <div class="space-y-1.5">
                <Label for="script-timeout">Timeout (seconds, optional)</Label
                ><Input
                  id="script-timeout"
                  v-model="timeoutSeconds"
                  type="number"
                  min="30"
                  max="3600"
                />
              </div>
            </div>
            <p v-if="formError" class="text-danger text-sm" role="alert">
              {{ formError }}
            </p>
            <ErrorNote v-if="actionError" :error="actionError" />
            <RouterLink
              v-if="
                actionError instanceof ApiError &&
                actionError.code === 'OWNERSHIP_NOT_PROVED' &&
                environmentId
              "
              :to="ownershipUrl"
              class="text-primary block text-sm underline"
              >Open the ownership proof</RouterLink
            >
            <Button type="submit" :disabled="busy">{{
              busy ? "Uploading…" : "Run script"
            }}</Button>
          </form>
        </AsyncState>
      </section>
      <section class="space-y-3">
        <h2 class="text-base font-semibold">Recent script runs</h2>
        <EmptyState
          v-if="!runs.data.value.items.length"
          :icon="FileArchive"
          title="No script runs yet"
          :description="
            enabled
              ? 'Upload a Playwright or k6 archive to start.'
              : 'Script runs are currently off on this server.'
          "
        />
        <ul v-else class="bg-card divide-y overflow-hidden rounded-lg border">
          <li v-for="run in runs.data.value.items" :key="run.id">
            <RouterLink
              :to="`${base}/script-runs/${encodeURIComponent(run.id)}`"
              class="hover:bg-accent flex min-h-14 items-center gap-3 px-4 py-3"
              ><StatusBadge
                :status="
                  run.status === 'done' ? run.verdict : run.status
                " /><span class="min-w-0 flex-1"
                ><strong class="block truncate text-sm"
                  >{{ run.kind }} · {{ run.base }}</strong
                ><span class="text-muted-foreground block truncate text-xs"
                  >{{ time(run.createdAt) }} · {{ run.tests }} tests ·
                  {{ run.failures }} failures</span
                ></span
              ><ArrowRight class="text-muted-foreground size-4"
            /></RouterLink>
          </li>
        </ul>
      </section>
    </template>
  </AsyncState>
</template>
