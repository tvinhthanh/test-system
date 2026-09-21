<script setup lang="ts">
import { ChevronDown } from "@lucide/vue";
import { cn } from "@/lib/utils";
// The browser's own <select>, dressed like an Input. Filters only need a
// handful of fixed options; a custom listbox would be JS for nothing.
defineOptions({ inheritAttrs: false });
defineProps<{ modelValue: string; class?: string }>();
defineEmits<{ "update:modelValue": [value: string] }>();
</script>
<template>
  <span :class="cn('relative inline-flex', $props.class)">
    <select
      :value="modelValue"
      class="border-input bg-background focus-visible:border-ring focus-visible:ring-ring/50 dark:bg-input/30 h-9 w-full appearance-none rounded-md border py-1 pr-8 pl-3 text-sm shadow-xs transition-[color,box-shadow] outline-none focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-50"
      v-bind="$attrs"
      @change="
        $emit('update:modelValue', ($event.target as HTMLSelectElement).value)
      "
    >
      <slot />
    </select>
    <ChevronDown
      :size="15"
      aria-hidden="true"
      class="text-muted-foreground pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2"
    />
  </span>
</template>
