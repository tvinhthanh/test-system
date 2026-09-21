<script setup lang="ts">
// Underlined view switcher for sibling screens (all tests / flaky / review) or
// URL-state windows (last 30 / 90 days). Children are ViewTab.
withDefaults(defineProps<{ label?: string }>(), { label: "Views" });
</script>
<template>
  <nav :aria-label="label" class="mb-5 flex gap-1 overflow-x-auto border-b">
    <slot />
  </nav>
</template>
