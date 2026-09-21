<script setup lang="ts">
import type { Component } from "vue";
import { cn } from "@/lib/utils";
// Empty, error and "nothing yet" states share one shape: a quiet icon, a
// heading (tests and live-check look for `.empty-state h2`), one sentence,
// and an optional action in the slot.
withDefaults(
  defineProps<{
    icon?: Component;
    title?: string;
    description?: string;
    compact?: boolean;
    heading?: "h1" | "h2";
    class?: string;
  }>(),
  { compact: false, heading: "h2" },
);
</script>
<template>
  <div
    :class="
      cn(
        'empty-state flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed text-center',
        compact ? 'px-4 py-8' : 'px-6 py-14',
        $props.class,
      )
    "
  >
    <span
      v-if="icon"
      class="bg-muted text-muted-foreground mb-1 inline-flex size-10 items-center justify-center rounded-full"
      ><component :is="icon" :size="20" aria-hidden="true"
    /></span>
    <component
      :is="heading"
      v-if="title"
      class="text-base font-semibold tracking-tight"
      >{{ title }}</component
    >
    <p
      v-if="description"
      class="text-muted-foreground max-w-prose text-sm text-balance"
    >
      {{ description }}
    </p>
    <div
      v-if="$slots.default"
      class="mt-2 flex flex-wrap items-center justify-center gap-2"
    >
      <slot />
    </div>
  </div>
</template>
