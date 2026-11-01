<script setup lang="ts">
// One site scan (QĐ-16): the pages found from the start page, nearest
// first, each a URL check — the worst first in mind: the tester opens a
// page for its full report. While the scan runs the page looks again
// every 3 s.
import { ChevronRight, Radar } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { SiteScan } from "./autotestRepository";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatTile from "../../shared/StatTile.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import { time } from "../../shared/format";

const route = useRoute();
const { autotest } = useServices();
const project = computed(() => String(route.params.project));
const scanId = computed(() => String(route.params.scan));
const base = () => `/p/${encodeURIComponent(project.value)}`;
const scan = useResource<SiteScan>();
const load = () => scan.load((signal) => autotest.scan(project.value, scanId.value, signal));
watch([project, scanId], () => void load(), { immediate: true });
const live = ref<SiteScan | null>(null);
const view = computed(() => live.value ?? scan.data.value);
let timer: ReturnType<typeof setTimeout> | undefined;
watch(view, (s) => {
  clearTimeout(timer);
  if (s?.status === "running")
    timer = setTimeout(async () => {
      try {
        live.value = await autotest.scan(project.value, scanId.value, new AbortController().signal);
      } catch {
        live.value = s ? { ...s } : null;
      }
    }, 3000);
});
onBeforeUnmount(() => clearTimeout(timer));

const show = ref<"all" | "problems">("problems");
const rank: Record<string, number> = { failed: 0, warning: 1, "": 2, passed: 3 };
const pages = computed(() => {
  const list = (view.value?.checks ?? []).filter(
    (c) => show.value === "all" || c.status === "error" || c.verdict === "failed" || c.verdict === "warning",
  );
  return [...list].sort(
    (a, b) => (a.status === "error" ? -1 : rank[a.verdict]!) - (b.status === "error" ? -1 : rank[b.verdict]!) || a.depth - b.depth,
  );
});
const dots: Record<string, string> = { passed: "bg-success", warning: "bg-warning", failed: "bg-danger", not_run: "bg-neutral" };
</script>
<template>
  <BackLink :to="`${base()}/auto?tab=scan`">Auto test</BackLink>
  <PageHeader eyebrow="Site scan" :title="view?.startUrl ?? 'Site scan'">
    <template #description>
      <span v-if="view" class="flex flex-wrap items-center gap-2">
        <StatusBadge :status="view.status" />
        <span class="text-muted-foreground text-sm"
          >{{ time(view.createdAt) }} · by {{ view.requestedBy }} · up to {{ view.maxPages }} pages
          <template v-if="view.account"> · signed in as {{ view.account }}</template></span
        >
      </span>
    </template>
  </PageHeader>
  <AsyncState :loading="scan.loading.value && !view" :error="scan.error.value" @retry="load">
    <template v-if="view">
      <div class="mb-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatTile label="Pages" :value="`${view.checked} / ${view.pages}`" />
        <StatTile label="Failed" :value="view.failed" />
        <StatTile label="Warnings" :value="view.warnings" />
        <StatTile label="Passed" :value="view.passed" />
      </div>
      <div class="mb-3 flex items-center gap-2">
        <label for="scan-show" class="text-muted-foreground text-sm">Show</label>
        <NativeSelect id="scan-show" v-model="show" class="w-48">
          <option value="problems">Pages with problems</option>
          <option value="all">Every page</option>
        </NativeSelect>
      </div>
      <EmptyState
        v-if="!pages.length"
        :icon="Radar"
        :title="view.status === 'running' ? 'Checking pages…' : 'No page with a problem'"
        :description="view.status === 'running' ? 'Pages appear as they are checked.' : 'Every page that was checked passed. Show every page to see them.'"
      />
      <ul v-else class="bg-card divide-y overflow-hidden rounded-lg border">
        <li v-for="c in pages" :key="c.id">
          <RouterLink
            :to="`${base()}/url-checks/${encodeURIComponent(c.id)}`"
            class="hover:bg-accent flex items-center gap-3 px-4 py-3 transition-colors"
          >
            <StatusBadge :status="c.status === 'queued' ? 'queued' : c.status === 'error' ? 'error' : c.verdict" />
            <span class="min-w-0 flex-1">
              <strong class="block truncate text-sm font-medium">{{ c.title || c.url }}</strong>
              <span class="text-muted-foreground block truncate text-xs"
                >{{ c.url }} · {{ c.depth === 0 ? "start page" : `${c.depth} link${c.depth === 1 ? "" : "s"} away` }}</span
              >
              <span v-if="c.error" class="text-danger block truncate text-xs">{{ c.error }}</span>
            </span>
            <span v-if="c.checkStatuses.length" class="hidden items-center gap-1 sm:flex" aria-hidden="true">
              <span v-for="(st, j) in c.checkStatuses" :key="j" class="size-2 rounded-full" :class="dots[st]" />
            </span>
            <ChevronRight class="text-muted-foreground size-4" />
          </RouterLink>
        </li>
      </ul>
    </template>
  </AsyncState>
</template>
