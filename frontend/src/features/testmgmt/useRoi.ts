import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Baseline, Roi } from "./testmgmtRepository";

// The month in the URL (?month=YYYY-MM, UTC), the current one by default.
export const currentMonth = () => new Date().toISOString().slice(0, 7);

export function useRoi() {
  const route = useRoute(),
    router = useRouter();
  const { testmgmt } = useServices();
  const project = computed(() => String(route.params.project ?? ""));
  const month = computed(() => {
    const m = route.query.month;
    return typeof m === "string" && /^\d{4}-\d{2}$/.test(m)
      ? m
      : currentMonth();
  });
  const roi = useResource<Roi>();
  const baselines = useResource<Baseline[]>();
  function refresh() {
    void roi.load((signal) => testmgmt.roi(project.value, month.value, signal));
    void baselines.load((signal) => testmgmt.baselines(project.value, signal));
  }
  function setMonth(value: string) {
    if (/^\d{4}-\d{2}$/.test(value) && value <= currentMonth())
      void router.push({ query: { ...route.query, month: value } });
  }
  watch([project, month], refresh, { immediate: true });
  return { project, month, setMonth, roi, baselines, refresh };
}

export const hours = (h: number) =>
  `${h < 0 ? "−" : ""}${Math.abs(h) >= 10 ? Math.round(Math.abs(h)) : Math.abs(h).toFixed(1)} h`;

export function minutesLabel(m: number | null) {
  if (m === null) return "—";
  if (m < 60) return `${Math.round(m)} min`;
  const h = m / 60;
  return h < 48 ? `${h.toFixed(1)} h` : `${(h / 24).toFixed(1)} days`;
}

// Money only from the reader's own rate, never stored or sent (§6.8.3 rule 2).
export function useHourlyRate() {
  // A number input's v-model gives a number once typed, "" when empty.
  const rate = ref<string | number>("");
  const value = computed(() => {
    const n = Number(rate.value);
    return String(rate.value).trim() !== "" && Number.isFinite(n) && n > 0
      ? n
      : null;
  });
  const money = (h: number) =>
    value.value === null
      ? ""
      : new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(
          h * value.value,
        );
  return { rate, value, money };
}
