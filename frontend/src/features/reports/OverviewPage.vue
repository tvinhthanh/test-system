<script setup lang="ts">
import { ChartColumn, RefreshCw, Table as TableIcon } from "@lucide/vue";
import { computed, ref } from "vue";
import { categoryLabel, failureCategories } from "./categories";
import {
  columns,
  humanDuration,
  percent,
  useOverview,
  windows,
} from "./useOverview";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import StatTile from "../../shared/StatTile.vue";
import ViewTab from "../../shared/ViewTab.vue";
import ViewTabs from "../../shared/ViewTabs.vue";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
const { days, setDays, refresh, metrics, trend, loading, error } =
  useOverview();

// Categories in fixed order → fixed series slots (categories.ts); `unknown`
// is drawn in the de-emphasis gray so the untriaged share reads as "missing",
// not as a kind of failure.
const weeks = computed(() => columns(trend.value ?? [], failureCategories));
const peak = computed(() =>
  weeks.value.reduce((m, w) => Math.max(m, w.total), 0),
);
const showTable = ref(false);
const hovered = ref<number | null>(null);
const hoveredWeek = computed(() =>
  hovered.value === null ? null : (weeks.value[hovered.value] ?? null),
);

// Chart geometry: thin columns, 2px surface gaps between stacked segments,
// baseline-anchored, scaled to the busiest week. Few weeks get wider columns
// so a 30-day window does not huddle in the corner; ticks thin out so labels
// never collide (≥ 64px apart).
const plotHeight = 160;
const geometry = computed(() => {
  const n = weeks.value.length;
  const barWidth = n <= 6 ? 44 : n <= 14 ? 28 : 18;
  const gap = n <= 6 ? 28 : n <= 14 ? 16 : 10;
  return {
    barWidth,
    gap,
    step: barWidth + gap,
    tickEvery: Math.max(1, Math.ceil(64 / (barWidth + gap))),
  };
});
const barHeight = (count: number) =>
  peak.value ? Math.round((count / peak.value) * plotHeight) : 0;
function stack(w: { counts: Record<string, number> }) {
  let y = plotHeight;
  const segments: { category: string; y: number; h: number }[] = [];
  for (const c of failureCategories) {
    const h = barHeight(w.counts[c] ?? 0);
    if (h > 0) {
      y -= h;
      segments.push({ category: c, y, h: Math.max(h - 2, 1) });
    }
  }
  return segments;
}
const weekLabel = (iso: string) =>
  new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "short" }).format(
    new Date(iso),
  );
</script>
<template>
  <PageHeader
    eyebrow="Engineering overview"
    title="Triage health"
    description="Time to root cause, coverage and failure categories — computed by the server over the selected window."
  >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh overview"
      title="Refresh overview"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <ViewTabs label="Window">
    <ViewTab
      v-for="w in windows"
      :key="w"
      :selected="days === w"
      @select="setDays(w)"
      >Last {{ w }} days</ViewTab
    >
  </ViewTabs>
  <AsyncState :loading="loading" :error="error" :rows="4" @retry="refresh">
    <section
      v-if="metrics"
      class="kpi-row grid grid-cols-2 gap-3 lg:grid-cols-4"
      aria-label="Key figures"
    >
      <StatTile
        label="Median time to root cause"
        :value="
          metrics.ttrcMedianSeconds === null
            ? '—'
            : humanDuration(metrics.ttrcMedianSeconds)
        "
        :note="
          metrics.ttrcMedianSeconds === null
            ? 'No triaged failure in the window'
            : 'failure observed → hypothesis accepted'
        "
      />
      <StatTile
        label="Time to first view"
        :value="
          metrics.timeToFirstViewSeconds === null
            ? '—'
            : humanDuration(metrics.timeToFirstViewSeconds)
        "
        note="median, failure → first FAILURE_SEEN"
      />
      <StatTile
        label="Untriaged"
        :value="percent(metrics.untriagedRate)"
        :note="`${metrics.failures - metrics.triaged} of ${metrics.failures} failures without a decision`"
      />
      <StatTile
        label="Triage coverage"
        :value="percent(metrics.triageCoverage)"
        :note="`${metrics.seen} of ${metrics.failures} failures seen by someone`"
      />
    </section>

    <section class="trend" aria-labelledby="trend-title">
      <SectionHeading id="trend-title" title="Failure categories by week">
        <span class="text-xs"
          >{{ weeks.length }} {{ weeks.length === 1 ? "week" : "weeks" }}</span
        >
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          :aria-pressed="showTable"
          :aria-label="showTable ? 'Show chart' : 'Show table'"
          :title="showTable ? 'Show chart' : 'Show table'"
          @click="showTable = !showTable"
        >
          <TableIcon v-if="!showTable" /><ChartColumn v-else />
        </Button>
      </SectionHeading>
      <EmptyState
        v-if="!weeks.length"
        compact
        :icon="ChartColumn"
        title="No failures in this window"
        description="Categories appear once a failure has been observed."
      />
      <div
        v-else-if="showTable"
        class="bg-card overflow-x-auto rounded-lg border"
      >
        <Table class="trend-table">
          <TableHeader>
            <TableRow class="hover:bg-transparent">
              <TableHead>Week of</TableHead>
              <TableHead
                v-for="c in failureCategories"
                :key="c"
                class="text-right"
                >{{ categoryLabel[c] }}</TableHead
              >
              <TableHead class="text-right">Total</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="w in weeks" :key="w.week">
              <TableCell class="font-medium">{{ weekLabel(w.week) }}</TableCell>
              <TableCell
                v-for="c in failureCategories"
                :key="c"
                class="text-right font-mono text-xs tabular-nums"
                >{{ w.counts[c] || "" }}</TableCell
              >
              <TableCell
                class="text-right font-mono text-xs font-semibold tabular-nums"
                >{{ w.total }}</TableCell
              >
            </TableRow>
          </TableBody>
        </Table>
      </div>
      <figure v-else class="trend-chart bg-card m-0 rounded-lg border p-4">
        <div class="chart-scroll relative overflow-x-auto pb-1">
          <svg
            class="block"
            :width="weeks.length * geometry.step + geometry.gap"
            :height="plotHeight + 28"
            role="img"
            :aria-label="`Failures per week by category, ${weeks.length} weeks, busiest week ${peak}`"
          >
            <line
              class="stroke-border"
              :x1="0"
              :x2="weeks.length * geometry.step + geometry.gap"
              :y1="plotHeight + 0.5"
              :y2="plotHeight + 0.5"
            />
            <g
              v-for="(w, i) in weeks"
              :key="w.week"
              class="column outline-none"
              :class="{ hovered: hovered === i }"
              :transform="`translate(${geometry.gap + i * geometry.step}, 0)`"
              tabindex="0"
              @mouseenter="hovered = i"
              @mouseleave="hovered = null"
              @focus="hovered = i"
              @blur="hovered = null"
            >
              <!-- Hit target wider than the mark. -->
              <rect
                class="hit fill-transparent"
                :class="hovered === i && 'fill-accent'"
                :x="-geometry.gap / 2"
                :y="0"
                :width="geometry.step"
                :height="plotHeight + 28"
                rx="4"
              />
              <rect
                v-for="s in stack(w)"
                :key="s.category"
                :class="`series-${s.category}`"
                :x="0"
                :y="s.y"
                :width="geometry.barWidth"
                :height="s.h"
                rx="1"
              />
              <text
                v-if="i % geometry.tickEvery === 0"
                class="tick fill-muted-foreground text-[10px]"
                :x="geometry.barWidth / 2"
                :y="plotHeight + 18"
                text-anchor="middle"
              >
                {{ weekLabel(w.week) }}
              </text>
            </g>
          </svg>
          <div
            v-if="hovered !== null && hoveredWeek"
            class="chart-tooltip bg-popover text-popover-foreground pointer-events-none absolute top-2 z-10 flex min-w-40 flex-col gap-1 rounded-md border px-3 py-2 text-xs shadow-md"
            role="status"
            :style="{
              left: `${geometry.gap + hovered * geometry.step + geometry.barWidth + 8}px`,
            }"
          >
            <strong class="font-medium"
              >Week of {{ weekLabel(hoveredWeek.week) }}</strong
            >
            <span
              v-for="c in failureCategories"
              v-show="hoveredWeek.counts[c]"
              :key="c"
              class="flex items-center gap-2"
              ><i
                :class="`swatch series-${c} inline-block size-2.5 rounded-[2px]`"
              />{{ categoryLabel[c] }}
              <b class="ml-auto font-mono font-medium tabular-nums">{{
                hoveredWeek.counts[c]
              }}</b></span
            >
            <span class="total mt-1 flex items-center gap-2 border-t pt-1"
              >Total
              <b class="ml-auto font-mono font-medium tabular-nums">{{
                hoveredWeek.total
              }}</b></span
            >
          </div>
        </div>
        <figcaption
          class="legend text-muted-foreground mt-3 flex flex-wrap gap-x-4 gap-y-1.5 text-xs"
        >
          <span
            v-for="c in failureCategories"
            :key="c"
            class="inline-flex items-center gap-1.5"
            ><i
              :class="`swatch series-${c} inline-block size-2.5 rounded-[2px]`"
            />{{ categoryLabel[c] }}</span
          >
        </figcaption>
      </figure>
    </section>
  </AsyncState>
</template>
