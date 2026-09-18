import { computed, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { TrendBucket, TriageMetrics } from "./reportsRepository";

// The window is URL state (?days=30|90) so a link carries the view. Both
// numbers come from the server for the same window; the client never
// re-derives a rate from the counts.
export const windows = [30, 90] as const;
export type Window = (typeof windows)[number];

export function useOverview() {
  const route = useRoute(),
    router = useRouter();
  const { reports } = useServices();
  const project = computed(() => String(route.params.project));
  const days = computed<Window>(() => {
    const raw = Number(route.query.days);
    return (windows as readonly number[]).includes(raw) ? (raw as Window) : 30;
  });
  const metrics = useResource<TriageMetrics>();
  const trend = useResource<TrendBucket[]>();
  function refresh() {
    void metrics.load((signal) =>
      reports.triageMetrics(project.value, days.value, signal),
    );
    void trend.load((signal) =>
      reports.categoryTrend(project.value, days.value, signal),
    );
  }
  watch(() => [project.value, days.value], refresh, { immediate: true });
  function setDays(value: Window) {
    void router.push({ query: { ...route.query, days: String(value) } });
  }
  return {
    project,
    days,
    setDays,
    refresh,
    metrics: metrics.data,
    trend: trend.data,
    loading: computed(() => metrics.loading.value || trend.loading.value),
    error: computed(() => metrics.error.value ?? trend.error.value),
  };
}

// ---- pure shaping for the chart, unit-tested ----

export interface WeekColumn {
  week: string;
  total: number;
  counts: Record<string, number>;
}

// Weekly columns in time order with every category present (0 when absent),
// so the stack order is stable from week to week and the table is rectangular.
export function columns(
  buckets: TrendBucket[],
  categories: readonly string[],
): WeekColumn[] {
  const byWeek = new Map<string, WeekColumn>();
  for (const b of buckets) {
    let col = byWeek.get(b.week);
    if (!col) {
      col = { week: b.week, total: 0, counts: {} };
      for (const c of categories) col.counts[c] = 0;
      byWeek.set(b.week, col);
    }
    col.counts[b.category] = (col.counts[b.category] ?? 0) + b.count;
    col.total += b.count;
  }
  return [...byWeek.values()].sort((a, b) => a.week.localeCompare(b.week));
}

// A duration in seconds as people read it on a dashboard: 42s, 12m, 3h 05m,
// 2d 4h. Null (no data in the window) is the caller's to label.
export function humanDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h ${String(minutes % 60).padStart(2, "0")}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}

export function percent(ratio: number): string {
  return `${Math.round(ratio * 100)}%`;
}
