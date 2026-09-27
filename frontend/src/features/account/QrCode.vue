<script setup lang="ts">
import { computed } from "vue";
import { encode } from "uqr";
// A QR code drawn as one SVG path from the module matrix — no HTML string
// is injected (the architecture test forbids v-html). Dark modules on a
// white tile in both themes: phone cameras read dark-on-light best.
const props = defineProps<{ value: string; label: string }>();
const qr = computed(() => {
  const { data, size } = encode(props.value, { ecc: "M", border: 2 });
  let d = "";
  data.forEach((row, y) =>
    row.forEach((dark, x) => {
      if (dark) d += `M${x} ${y}h1v1h-1z`;
    }),
  );
  return { d, size };
});
</script>
<template>
  <svg
    role="img"
    :aria-label="label"
    :viewBox="`0 0 ${qr.size} ${qr.size}`"
    shape-rendering="crispEdges"
    class="rounded-md bg-white"
  >
    <path :d="qr.d" fill="#000" />
  </svg>
</template>
