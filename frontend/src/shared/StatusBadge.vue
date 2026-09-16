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
const props = withDefaults(defineProps<{ status: string; flaky?: number }>(), {
  flaky: 0,
});
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
</script>
<template>
  <span class="status" :class="color"
    ><component :is="icon" :size="14" aria-hidden="true" /><span>{{
      statusLabel(status, flaky)
    }}</span></span
  >
</template>
