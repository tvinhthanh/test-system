<script setup lang="ts">
defineProps<{ title: string; id?: string }>();
</script>
<template>
  <div class="mt-8 mb-3 flex items-center justify-between gap-3">
    <h2 :id="id" class="text-base font-semibold tracking-tight">{{ title }}</h2>
    <div class="text-muted-foreground flex items-center gap-2 text-sm">
      <slot />
    </div>
  </div>
</template>
