<script setup lang="ts">
// One URL check's report (QĐ-14): the verdict, each check with what it found,
// the page's timings, what the server refused to open, and the desktop and
// phone screenshots. Everything in the report came from the checked page —
// it is shown as text, and a link is only drawn for an http(s) address.
import {
  CircleAlert,
  CircleCheck,
  CircleDashed,
  ExternalLink,
  LoaderCircle,
  RotateCcw,
  ShieldBan,
  TriangleAlert,
} from "@lucide/vue";
import { computed, onBeforeUnmount, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { UrlCheck } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { bytes, time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import StatTile from "../../shared/StatTile.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import { explain } from "./explain";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

const route = useRoute(),
  router = useRouter();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const checkId = computed(() => String(route.params.check));
const canWrite = useProjectPermission("testmgmt.write");
const base = () => `/p/${encodeURIComponent(project.value)}`;

const check = useResource<UrlCheck>();
const polled = shallowRef<UrlCheck | null>(null);
const view = computed(() => polled.value ?? check.data.value);
function refresh() {
  polled.value = null;
  return check.load((signal) =>
    testmgmt.urlCheck(project.value, checkId.value, signal),
  );
}
watch(() => [project.value, checkId.value], refresh, { immediate: true });
let timer: ReturnType<typeof setTimeout> | undefined;
let pollAbort: AbortController | undefined;
watch(view, (v) => {
  clearTimeout(timer);
  if (v?.status !== "queued") return;
  timer = setTimeout(async () => {
    pollAbort = new AbortController();
    try {
      polled.value = await testmgmt.urlCheck(
        project.value,
        checkId.value,
        pollAbort.signal,
      );
    } catch {
      // the refresh button shows the error
    }
  }, 3000);
});
onBeforeUnmount(() => {
  clearTimeout(timer);
  pollAbort?.abort();
});

const http = (u: string) => /^https?:\/\//i.test(u);
const icons = {
  passed: CircleCheck,
  warning: TriangleAlert,
  failed: CircleAlert,
  not_run: CircleDashed,
};
const tones = {
  passed: "text-success",
  warning: "text-warning",
  failed: "text-danger",
  not_run: "text-muted-foreground",
};
const ms = (n: number | null) =>
  n == null ? "—" : n >= 1000 ? `${(n / 1000).toFixed(1)} s` : `${n} ms`;
const shot = (which: "desktop" | "mobile") =>
  testmgmt.urlCheckScreenshot(project.value, checkId.value, which);

const mutation = useMutation();
async function again() {
  const c = view.value;
  if (!c) return;
  const created = await mutation.run("again", (signal) =>
    testmgmt.createUrlCheck(
      project.value,
      { url: c.url, environmentId: c.environmentId },
      signal,
    ),
  );
  if (created)
    await router.push(`${base()}/url-checks/${encodeURIComponent(created.id)}`);
}
</script>
<template>
  <BackLink :to="`${base()}/url-checks`">URL checks</BackLink>
  <PageHeader
    eyebrow="URL check"
    :title="view?.title || view?.url || 'URL check'"
  >
    <template #description>
      <span v-if="view" class="inline-flex flex-wrap items-center gap-2">
        <StatusBadge
          :status="view.status === 'done' ? view.verdict : view.status"
        />
        <a
          v-if="http(view.finalUrl || view.url)"
          :href="view.finalUrl || view.url"
          target="_blank"
          rel="noopener noreferrer"
          class="text-primary inline-flex items-center gap-1 underline"
          >{{ view.finalUrl || view.url
          }}<ExternalLink :size="13" aria-hidden="true"
        /></a>
        <span class="text-muted-foreground text-xs"
          >{{ time(view.createdAt) }} · {{ view.requestedBy }}</span
        >
      </span>
    </template>
    <Button
      v-if="canWrite && view && view.status !== 'queued'"
      variant="outline"
      size="sm"
      :disabled="mutation.busy.value !== null"
      @click="again"
      ><RotateCcw />Check again</Button
    >
  </PageHeader>
  <ErrorNote
    v-if="mutation.error.value?.key === 'again'"
    class="mb-4"
    :error="explain(mutation.error.value.error)"
  />

  <AsyncState
    :loading="check.loading.value && !polled"
    :error="check.error.value"
    @retry="refresh"
  >
    <template v-if="view">
      <Alert v-if="view.status === 'queued'" role="status">
        <LoaderCircle class="animate-spin" />
        <AlertTitle>Checking…</AlertTitle>
        <AlertDescription
          >A browser on the server is opening the page. This usually takes 5–20
          seconds; the report appears here by itself.</AlertDescription
        >
      </Alert>
      <Alert v-else-if="view.status === 'error'" variant="destructive">
        <CircleAlert />
        <AlertTitle>The check could not run</AlertTitle>
        <AlertDescription>{{ view.error }}</AlertDescription>
      </Alert>

      <template v-else-if="view.report">
        <ul class="url-check-results bg-card divide-y rounded-lg border">
          <li
            v-for="c in view.report.checks"
            :key="c.key"
            class="flex gap-3 px-4 py-3"
          >
            <component
              :is="icons[c.status]"
              :size="18"
              :class="['mt-0.5 shrink-0', tones[c.status]]"
              aria-hidden="true"
            />
            <div class="min-w-0 flex-1 space-y-1">
              <p class="text-sm">
                <strong class="font-medium">{{ c.title }}</strong>
                <span class="sr-only"> — {{ c.status }}</span>
                <span class="text-muted-foreground"> · {{ c.summary }}</span>
              </p>
              <details v-if="c.details.length" class="text-xs">
                <summary
                  class="text-muted-foreground hover:text-foreground cursor-pointer"
                >
                  {{ c.details.length }} detail{{
                    c.details.length > 1 ? "s" : ""
                  }}
                </summary>
                <ul
                  class="bg-muted/50 mt-2 space-y-1 rounded-md p-3 font-mono break-all"
                >
                  <li v-for="(d, i) in c.details" :key="i">{{ d }}</li>
                </ul>
              </details>
            </div>
          </li>
        </ul>

        <template v-if="view.report.status > 0">
          <SectionHeading title="Timings" />
          <section
            class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5"
            aria-label="Timings"
          >
            <StatTile
              label="First byte"
              :value="ms(view.report.metrics.ttfbMs)"
              note="server answer"
            />
            <StatTile
              label="Main content"
              :value="ms(view.report.metrics.lcpMs)"
              note="good: under 2.5 s"
            />
            <StatTile
              label="Fully loaded"
              :value="ms(view.report.metrics.loadMs)"
            />
            <StatTile
              label="Requests"
              :value="view.report.metrics.requests ?? '—'"
            />
            <StatTile
              label="Transferred"
              :value="
                view.report.metrics.transferBytes == null
                  ? '—'
                  : bytes(view.report.metrics.transferBytes)
              "
            />
          </section>
        </template>

        <template v-if="view.screenshots.length">
          <SectionHeading title="Screenshots" />
          <div
            class="grid gap-4 md:grid-cols-[minmax(0,3fr)_minmax(0,1fr)] md:items-start"
          >
            <figure
              v-if="view.screenshots.includes('desktop')"
              class="space-y-2"
            >
              <a :href="shot('desktop')" target="_blank" rel="noopener">
                <img
                  :src="shot('desktop')"
                  alt="The page on a 1366 × 768 desktop screen"
                  class="w-full rounded-md border"
                  loading="lazy"
                />
              </a>
              <figcaption class="text-muted-foreground text-xs">
                Desktop, 1366 × 768
              </figcaption>
            </figure>
            <figure
              v-if="view.screenshots.includes('mobile')"
              class="max-w-60 space-y-2"
            >
              <a :href="shot('mobile')" target="_blank" rel="noopener">
                <img
                  :src="shot('mobile')"
                  alt="The page on a 390 × 844 phone screen"
                  class="w-full rounded-md border"
                  loading="lazy"
                />
              </a>
              <figcaption class="text-muted-foreground text-xs">
                Phone, 390 × 844
              </figcaption>
            </figure>
          </div>
        </template>

        <template v-if="view.report.blocked.length">
          <SectionHeading title="Not opened" />
          <Alert>
            <ShieldBan />
            <AlertTitle
              >The server refused these addresses on purpose</AlertTitle
            >
            <AlertDescription class="block space-y-2">
              <p>
                Private, loopback and cloud-metadata addresses are never opened
                from the server, so a page cannot use the check to reach inside
                its network. A staging site on the office LAN needs
                <code>URLCHECK_ALLOW_PRIVATE=on</code>.
              </p>
              <ul class="font-mono text-xs break-all">
                <li v-for="(b, i) in view.report.blocked" :key="i">{{ b }}</li>
              </ul>
            </AlertDescription>
          </Alert>
        </template>
        <p class="text-muted-foreground mt-6 text-xs">
          Checked in {{ ms(view.report.durationMs) }}. Automatic checks are a
          first look: they do not replace the test cases in your plans.
        </p>
      </template>
    </template>
  </AsyncState>
</template>
