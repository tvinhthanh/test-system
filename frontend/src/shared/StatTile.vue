<script setup lang="ts">
// A number with its name and one line of provenance. Text wears text colour;
// nothing here is coloured by value (dataviz: status colour is reserved).
defineProps<{ label: string; value: string | number; note?: string }>();
</script>
<template>
  <article class="stat-tile bg-card flex flex-col gap-1 rounded-lg border p-4">
    <span class="text-muted-foreground text-xs font-medium">{{ label }}</span>
    <strong class="text-2xl font-semibold tracking-tight tabular-nums">{{
      value
    }}</strong>
    <span v-if="note" class="text-muted-foreground text-xs leading-snug">{{
      note
    }}</span>
  </article>
</template>
