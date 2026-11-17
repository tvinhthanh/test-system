<script setup lang="ts">
import NativeSelect from "../../shared/NativeSelect.vue";
import {
  taxonomyFilterOptions,
  taxonomyLabel,
  type TaxonomyFilter,
} from "./taxonomy";
withDefaults(
  defineProps<{
    modelValue: TaxonomyFilter;
    disabled?: boolean;
    prefix?: string;
  }>(),
  { prefix: "Filter" },
);
const emit = defineEmits<{ change: [patch: Record<string, string>] }>();
</script>
<template>
  <div
    class="mb-4 grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-5"
  >
    <label
      v-for="(option, key) in taxonomyFilterOptions"
      :key="key"
      class="grid min-w-0 gap-1.5 text-xs font-medium"
    >
      {{ option.label }}
      <NativeSelect
        :aria-label="`${prefix} ${option.label}`"
        :disabled="disabled"
        :model-value="modelValue[key] ?? ''"
        @update:model-value="emit('change', { [key]: $event })"
      >
        <option value="">Any</option>
        <option v-for="value in option.values" :key="value" :value="value">
          {{ taxonomyLabel(value) }}
        </option>
      </NativeSelect>
    </label>
  </div>
</template>
