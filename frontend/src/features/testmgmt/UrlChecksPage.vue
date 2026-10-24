<script setup lang="ts">
// URL checks (QĐ-14): a tester types an address — or picks an environment's
// — and the server's checker opens it in a real browser: does it load, any
// script errors, broken images or links, accessibility, speed, phone fit.
// No test code. While a check waits the list looks again every 3 s, quietly
// (no skeleton flash); the full report is on the check's own page.
import { ChevronRight, RefreshCw, ScanSearch } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Environment, UrlCheck } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import { explain } from "./explain";
import TmTabs from "./TmTabs.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type Listing = { enabled: boolean; items: UrlCheck[] };
const route = useRoute(),
  router = useRouter();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const canWrite = useProjectPermission("testmgmt.write");
const base = () => `/p/${encodeURIComponent(project.value)}`;

const checks = useResource<Listing>();
const envs = useResource<Environment[]>();
const polled = shallowRef<Listing | null>(null);
const view = computed(() => polled.value ?? checks.data.value);
function refresh() {
  polled.value = null;
  return checks.load((signal) => testmgmt.urlChecks(project.value, signal));
}
watch(
  project,
  () => {
    void refresh();
    void envs.load((signal) => testmgmt.environments(project.value, signal));
  },
  { immediate: true },
);
let timer: ReturnType<typeof setTimeout> | undefined;
let pollAbort: AbortController | undefined;
watch(view, (v) => {
  clearTimeout(timer);
  if (!v?.items.some((c) => c.status === "queued")) return;
  timer = setTimeout(async () => {
    pollAbort = new AbortController();
    try {
      polled.value = await testmgmt.urlChecks(project.value, pollAbort.signal);
    } catch {
      // the next manual refresh shows the error
    }
  }, 3000);
});
onBeforeUnmount(() => {
  clearTimeout(timer);
  pollAbort?.abort();
});

const withAddress = computed(() =>
  (envs.data.value ?? []).filter((e) => e.baseUrl),
);
const environmentId = ref("");
const address = ref("");
watch(environmentId, (id) => {
  const env = withAddress.value.find((e) => e.id === id);
  if (env) address.value = env.baseUrl;
});
const mutation = useMutation();
async function check() {
  const created = await mutation.run("check", (signal) =>
    testmgmt.createUrlCheck(
      project.value,
      { url: address.value.trim(), environmentId: environmentId.value },
      signal,
    ),
  );
  if (created)
    await router.push(`${base()}/url-checks/${encodeURIComponent(created.id)}`);
}
const dots: Record<string, string> = {
  passed: "bg-success",
  warning: "bg-warning",
  failed: "bg-danger",
  not_run: "bg-neutral",
};
</script>
<template>
  <PageHeader
    eyebrow="Test management"
    title="URL checks"
    description="Open a page in a real browser on the server and get a first-look report — no test code needed."
  >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh URL checks"
      title="Refresh URL checks"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <TmTabs :project="project" current="url-checks" />

  <Alert v-if="view && !view.enabled" class="mb-5">
    <ScanSearch />
    <AlertTitle>URL checks are off on this server</AlertTitle>
    <AlertDescription>
      An admin turns them on with <code>URLCHECK=on</code> in
      <code>backend/deploy/.env</code> and then runs
      <code>server/atp.sh up</code>. Earlier checks stay readable below.
    </AlertDescription>
  </Alert>

  <form
    v-else-if="view && canWrite"
    class="url-check-new bg-card mb-6 grid gap-4 rounded-lg border p-4 md:grid-cols-[14rem_minmax(0,1fr)_auto] md:items-end"
    @submit.prevent="check"
  >
    <div class="space-y-2">
      <Label for="check-env">Environment</Label>
      <NativeSelect id="check-env" v-model="environmentId" class="w-full">
        <option value="">Any address</option>
        <option v-for="e in withAddress" :key="e.id" :value="e.id">
          {{ e.key }}
        </option>
      </NativeSelect>
    </div>
    <div class="space-y-2">
      <Label for="check-url">Address to check</Label>
      <Input
        id="check-url"
        v-model="address"
        type="url"
        required
        maxlength="2048"
        placeholder="https://staging.example.com/checkout"
      />
    </div>
    <Button type="submit" :disabled="mutation.busy.value !== null"
      ><ScanSearch />Check</Button
    >
    <p class="text-muted-foreground text-xs md:col-span-3">
      Checks: page loads · HTTPS certificate · JavaScript errors · images and
      scripts that fail · broken links · accessibility (WCAG A/AA) · speed ·
      page basics · fits a phone screen. Takes a few seconds; one page, not the
      whole site.
    </p>
    <ErrorNote
      v-if="mutation.error.value?.key === 'check'"
      class="md:col-span-3"
      :error="explain(mutation.error.value.error)"
    />
  </form>

  <AsyncState
    :loading="checks.loading.value && !polled"
    :error="checks.error.value"
    @retry="refresh"
  >
    <EmptyState
      v-if="!view?.items.length"
      :icon="ScanSearch"
      title="No URL checks yet"
      description="Type an address above to see how the page does in a real browser."
    />
    <ul
      v-else
      class="url-check-list bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li v-for="c in view.items" :key="c.id">
        <RouterLink
          :to="`${base()}/url-checks/${encodeURIComponent(c.id)}`"
          class="hover:bg-accent focus-visible:bg-accent group flex items-center gap-3 px-4 py-3 transition-colors outline-none"
        >
          <span class="min-w-0 flex-1">
            <strong class="block truncate text-sm font-medium">{{
              c.title || c.url
            }}</strong>
            <span class="text-muted-foreground block truncate text-xs"
              >{{ c.url }} · {{ time(c.createdAt) }} · {{ c.requestedBy }}</span
            >
          </span>
          <span
            v-if="c.checkStatuses.length"
            class="hidden items-center gap-1 sm:flex"
            :aria-label="`${c.checkStatuses.filter((s) => s === 'failed').length} failed, ${c.checkStatuses.filter((s) => s === 'warning').length} warnings`"
          >
            <span
              v-for="(s, i) in c.checkStatuses"
              :key="i"
              :class="['size-2 rounded-full', dots[s]]"
            />
          </span>
          <StatusBadge :status="c.status === 'done' ? c.verdict : c.status" />
          <ChevronRight
            :size="16"
            aria-hidden="true"
            class="text-muted-foreground transition-transform group-hover:translate-x-0.5"
          />
        </RouterLink>
      </li>
    </ul>
  </AsyncState>
</template>
