<script setup lang="ts">
import {
  CircleCheck,
  CircleDashed,
  ShieldAlert,
  TriangleAlert,
} from "@lucide/vue";
import { computed } from "vue";
import type { GateResult } from "./testmgmtRepository";
import { cn } from "@/lib/utils";
// The gate's verdict as a badge: reserved status colours plus an icon and a
// label, never colour alone. "Not evaluated" when the gate never ran.
const props = defineProps<{ gate: GateResult | null }>();
const view = computed(() => {
  const v = props.gate?.verdict;
  switch (v) {
    case "approved":
      return {
        label: "Gate: approved",
        icon: CircleCheck,
        cls: "bg-success-soft text-success border-success/25",
      };
    case "warn":
      return {
        label: "Gate: warnings",
        icon: TriangleAlert,
        cls: "bg-warning-soft text-warning border-warning/30",
      };
    case "blocked":
      return {
        label: "Gate: blocked",
        icon: ShieldAlert,
        cls: "bg-danger-soft text-danger border-danger/25",
      };
    case "no_evidence":
      return {
        label: "Gate: no finished run",
        icon: CircleDashed,
        cls: "bg-neutral-soft text-neutral border-neutral/25",
      };
    default:
      return {
        label: "Gate: not evaluated",
        icon: CircleDashed,
        cls: "bg-neutral-soft text-neutral border-neutral/25",
      };
  }
});
</script>
<template>
  <span
    :class="
      cn(
        'gate-badge inline-flex w-fit shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2 py-0.5 text-xs font-medium',
        view.cls,
      )
    "
    :data-verdict="gate?.verdict ?? 'none'"
    ><component :is="view.icon" :size="13" aria-hidden="true" />{{
      view.label
    }}</span
  >
</template>
