<script setup lang="ts">
// Every screen opens the same way: an eyebrow naming the area, the title,
// one line of context, and the screen's actions on the right.
defineProps<{ eyebrow: string; title: string; description?: string }>();
</script>
<template>
  <header class="mb-6 flex flex-wrap items-start justify-between gap-4">
    <div class="min-w-0 space-y-1.5">
      <p
        class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
      >
        {{ eyebrow }}
      </p>
      <h1 class="text-2xl font-semibold tracking-tight">{{ title }}</h1>
      <p
        v-if="description || $slots.description"
        class="text-muted-foreground max-w-3xl text-sm"
      >
        <slot name="description">{{ description }}</slot>
      </p>
    </div>
    <div v-if="$slots.default" class="flex shrink-0 items-center gap-2">
      <slot />
    </div>
  </header>
</template>
