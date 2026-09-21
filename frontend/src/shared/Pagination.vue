<script setup lang="ts">
import { ChevronRight, ChevronsLeft } from "@lucide/vue";
import { Button } from "@/components/ui/button";
// Keyset pagination has no page numbers (API_CONTRACT): first and next only.
withDefaults(
  defineProps<{
    summary: string;
    canFirst: boolean;
    canNext: boolean;
    label?: string;
  }>(),
  { label: "page" },
);
defineEmits<{ first: []; next: [] }>();
</script>
<template>
  <footer
    class="text-muted-foreground mt-3 flex items-center justify-between gap-3 text-xs"
  >
    <span>{{ summary }}</span>
    <div class="flex items-center gap-1">
      <Button
        variant="outline"
        size="icon-sm"
        :aria-label="`First ${label}`"
        :title="`First ${label}`"
        :disabled="!canFirst"
        @click="$emit('first')"
        ><ChevronsLeft
      /></Button>
      <Button
        variant="outline"
        size="icon-sm"
        :aria-label="`Next ${label}`"
        :title="`Next ${label}`"
        :disabled="!canNext"
        @click="$emit('next')"
        ><ChevronRight
      /></Button>
    </div>
  </footer>
</template>
