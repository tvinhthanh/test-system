<script setup lang="ts">
import { ArrowLeft } from "@lucide/vue";
defineProps<{ to: string }>();
</script>
<template>
  <RouterLink
    :to="to"
    class="text-muted-foreground hover:text-foreground mb-4 inline-flex h-8 items-center gap-1.5 text-sm transition-colors"
    ><ArrowLeft :size="15" /><slot
  /></RouterLink>
</template>
