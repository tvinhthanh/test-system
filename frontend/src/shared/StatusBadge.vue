<script setup lang="ts">
import {
  CircleCheck,
  CircleAlert,
  LoaderCircle,
  CircleDashed,
  TriangleAlert,
} from "@lucide/vue";
import { computed } from "vue";
import { statusLabel, tone } from "./format";
import { cn } from "@/lib/utils";
const props = withDefaults(defineProps<{ status: string; flaky?: number }>(), {
  flaky: 0,
});
// Colour never travels alone: every tone has its own icon and the label
// (§1.7 — "passed with flaky tests" must read as a warning, not clean green).
const color = computed(() => tone(props.status, props.flaky));
const icon = computed(
  () =>
    ({
      success: CircleCheck,
      danger: CircleAlert,
      warning: TriangleAlert,
      info: LoaderCircle,
      neutral: CircleDashed,
    })[color.value],
);
const tones = {
  success: "bg-success-soft text-success border-success/25",
  danger: "bg-danger-soft text-danger border-danger/25",
  warning: "bg-warning-soft text-warning border-warning/30",
  info: "bg-info-soft text-info border-info/25",
  neutral: "bg-neutral-soft text-neutral border-neutral/25",
} as const;
</script>
<template>
  <span
    :class="
      cn(
        'status inline-flex w-fit shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2 py-0.5 text-xs font-medium',
        tones[color],
        color,
      )
    "
    ><component
      :is="icon"
      :size="13"
      aria-hidden="true"
      :class="color === 'info' && 'animate-spin'"
    /><span class="inline-block first-letter:uppercase">{{
      statusLabel(status, flaky)
    }}</span></span
  >
</template>
