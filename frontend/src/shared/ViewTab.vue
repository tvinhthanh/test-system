<script setup lang="ts">
import { cn } from "@/lib/utils";
// A RouterLink when `to` is given, otherwise a button (URL query state).
withDefaults(defineProps<{ to?: string; selected?: boolean }>(), {
  selected: false,
});
defineEmits<{ select: [] }>();
const base =
  "-mb-px inline-flex h-10 shrink-0 items-center gap-2 border-b-2 px-3 text-sm font-medium transition-colors [&_svg]:size-4";
const on = "border-primary text-foreground";
const off =
  "border-transparent text-muted-foreground hover:text-foreground hover:border-border";
</script>
<template>
  <RouterLink
    v-if="to"
    :to="to"
    :class="cn(base, selected ? on : off, selected && 'selected')"
    :aria-current="selected ? 'page' : undefined"
    ><slot
  /></RouterLink>
  <button
    v-else
    type="button"
    :class="cn(base, selected ? on : off, selected && 'selected')"
    :aria-pressed="selected"
    @click="$emit('select')"
  >
    <slot />
  </button>
</template>
