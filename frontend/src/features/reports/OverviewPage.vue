<script setup lang="ts">
import { ChartColumn, RefreshCw, Table } from "@lucide/vue";
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
// baseline-anchored, scaled to the busiest week.
const plotHeight = 160;
const barWidth = 18;
const gap = 10;
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
  <header class="page-heading">
    <div>
      <p class="eyebrow">ENGINEERING OVERVIEW</p>
      <h1>Triage health</h1>
      <p class="muted">
        Time to root cause, coverage and failure categories — computed by the
        server over the selected window
      </p>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh overview"
      title="Refresh overview"
      @click="refresh"
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <div class="view-tabs" role="group" aria-label="Window">
    <button
      v-for="w in windows"
      :key="w"
      type="button"
      class="tab-button"
      :class="{ selected: days === w }"
      :aria-pressed="days === w"
      @click="setDays(w)"
    >
      Last {{ w }} days
    </button>
  </div>
  <AsyncState :loading="loading" :error="error" @retry="refresh">
    <section v-if="metrics" class="kpi-row" aria-label="Key figures">
      <article class="stat-tile">
        <span class="stat-label">Median time to root cause</span>
        <strong class="stat-value">{{
          metrics.ttrcMedianSeconds === null
            ? "—"
            : humanDuration(metrics.ttrcMedianSeconds)
        }}</strong>
        <span class="stat-note">{{
          metrics.ttrcMedianSeconds === null
            ? "No triaged failure in the window"
            : "failure observed → hypothesis accepted"
        }}</span>
      </article>
      <article class="stat-tile">
        <span class="stat-label">Time to first view</span>
        <strong class="stat-value">{{
          metrics.timeToFirstViewSeconds === null
            ? "—"
            : humanDuration(metrics.timeToFirstViewSeconds)
        }}</strong>
        <span class="stat-note">median, failure → first FAILURE_SEEN</span>
      </article>
      <article class="stat-tile">
        <span class="stat-label">Untriaged</span>
        <strong class="stat-value">{{ percent(metrics.untriagedRate) }}</strong>
        <span class="stat-note"
          >{{ metrics.failures - metrics.triaged }} of
          {{ metrics.failures }} failures without a decision</span
        >
      </article>
      <article class="stat-tile">
        <span class="stat-label">Triage coverage</span>
        <strong class="stat-value">{{
          percent(metrics.triageCoverage)
        }}</strong>
        <span class="stat-note"
          >{{ metrics.seen }} of {{ metrics.failures }} failures seen by
          someone</span
        >
      </article>
    </section>

    <section class="trend" aria-labelledby="trend-title">
      <div class="section-heading">
        <h2 id="trend-title">Failure categories by week</h2>
        <div class="section-actions">
          <span class="muted"
            >{{ weeks.length }}
            {{ weeks.length === 1 ? "week" : "weeks" }}</span
          >
          <button
            type="button"
            class="icon-button"
            :aria-pressed="showTable"
            :aria-label="showTable ? 'Show chart' : 'Show table'"
            :title="showTable ? 'Show chart' : 'Show table'"
            @click="showTable = !showTable"
          >
            <Table v-if="!showTable" :size="16" /><ChartColumn
              v-else
              :size="16"
            />
          </button>
        </div>
      </div>
      <div v-if="!weeks.length" class="empty-state compact">
        <ChartColumn :size="26" />
        <h2>No failures in this window</h2>
        <p>Categories appear once a failure has been observed.</p>
      </div>
      <table v-else-if="showTable" class="trend-table">
        <thead>
          <tr>
            <th scope="col">Week of</th>
            <th v-for="c in failureCategories" :key="c" scope="col">
              {{ categoryLabel[c] }}
            </th>
            <th scope="col">Total</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="w in weeks" :key="w.week">
            <th scope="row">{{ weekLabel(w.week) }}</th>
            <td v-for="c in failureCategories" :key="c">
              {{ w.counts[c] || "" }}
            </td>
            <td>
              <strong>{{ w.total }}</strong>
            </td>
          </tr>
        </tbody>
      </table>
      <figure v-else class="trend-chart">
        <div class="chart-scroll">
          <svg
            :width="weeks.length * (barWidth + gap) + gap"
            :height="plotHeight + 28"
            role="img"
            :aria-label="`Failures per week by category, ${weeks.length} weeks, busiest week ${peak}`"
          >
            <line
              class="baseline"
              :x1="0"
              :x2="weeks.length * (barWidth + gap) + gap"
              :y1="plotHeight + 0.5"
              :y2="plotHeight + 0.5"
            />
            <g
              v-for="(w, i) in weeks"
              :key="w.week"
              class="column"
              :class="{ hovered: hovered === i }"
              :transform="`translate(${gap + i * (barWidth + gap)}, 0)`"
              tabindex="0"
              @mouseenter="hovered = i"
              @mouseleave="hovered = null"
              @focus="hovered = i"
              @blur="hovered = null"
            >
              <!-- Hit target wider than the mark. -->
              <rect
                class="hit"
                :x="-gap / 2"
                :y="0"
                :width="barWidth + gap"
                :height="plotHeight + 28"
              />
              <rect
                v-for="s in stack(w)"
                :key="s.category"
                :class="`series-${s.category}`"
                :x="0"
                :y="s.y"
                :width="barWidth"
                :height="s.h"
              />
              <text
                v-if="i % Math.max(1, Math.ceil(weeks.length / 8)) === 0"
                class="tick"
                :x="barWidth / 2"
                :y="plotHeight + 18"
                text-anchor="middle"
              >
                {{ weekLabel(w.week) }}
              </text>
            </g>
          </svg>
          <div
            v-if="hovered !== null && hoveredWeek"
            class="chart-tooltip"
            role="status"
            :style="{
              left: `${gap + hovered * (barWidth + gap) + barWidth + 8}px`,
            }"
          >
            <strong>Week of {{ weekLabel(hoveredWeek.week) }}</strong>
            <span
              v-for="c in failureCategories"
              v-show="hoveredWeek.counts[c]"
              :key="c"
              ><i :class="`swatch series-${c}`" />{{ categoryLabel[c] }}
              <b>{{ hoveredWeek.counts[c] }}</b></span
            >
            <span class="total"
              >Total <b>{{ hoveredWeek.total }}</b></span
            >
          </div>
        </div>
        <figcaption class="legend">
          <span v-for="c in failureCategories" :key="c"
            ><i :class="`swatch series-${c}`" />{{ categoryLabel[c] }}</span
          >
        </figcaption>
      </figure>
    </section>
  </AsyncState>
</template>
