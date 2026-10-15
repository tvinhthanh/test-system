<script setup lang="ts">
import {
  copyTaxonomy,
  levels,
  purposes,
  usageTags,
  executionModes,
  performanceTypes,
  taxonomyLabel,
  type Taxonomy,
} from "./taxonomy";
import NativeSelect from "../../shared/NativeSelect.vue";
const props = defineProps<{ modelValue: Taxonomy; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [value: Taxonomy] }>();
const groups = [
  { key: "purposes", label: "Purposes", values: purposes },
  { key: "tags", label: "Usage tags", values: usageTags },
  { key: "executionModes", label: "Execution modes", values: executionModes },
  {
    key: "performanceTypes",
    label: "Performance types",
    values: performanceTypes,
  },
] as const;
function toggle(
  key: (typeof groups)[number]["key"],
  value: string,
  checked: boolean,
) {
  const next = copyTaxonomy(props.modelValue);
  const values: string[] = [...next[key]];
  next[key] = (
    checked ? [...values, value] : values.filter((v) => v !== value)
  ).sort() as never;
  if (!next.purposes.includes("performance")) next.performanceTypes = [];
  emit("update:modelValue", next);
}
</script>
<template>
  <fieldset :disabled="disabled" class="grid min-w-0 gap-4 border-y py-4">
    <legend class="text-sm font-medium">Classification</legend>
    <label class="grid gap-2 text-sm"
      >Level
      <NativeSelect
        aria-label="Level"
        :model-value="modelValue.level"
        @update:model-value="
          emit('update:modelValue', {
            ...copyTaxonomy(modelValue),
            level: $event as Taxonomy['level'],
          })
        "
      >
        <option value="">Unclassified</option>
        <option v-for="level in levels" :key="level" :value="level">
          {{ taxonomyLabel(level) }}
        </option>
      </NativeSelect>
    </label>
    <template v-for="group in groups" :key="group.key">
      <fieldset
        v-if="
          group.key !== 'performanceTypes' ||
          modelValue.purposes.includes('performance')
        "
        class="min-w-0"
      >
        <legend class="mb-2 text-sm font-medium">{{ group.label }}</legend>
        <div class="flex flex-wrap gap-x-5 gap-y-3">
          <label
            v-for="value in group.values"
            :key="value"
            class="flex items-center gap-2 text-sm"
          >
            <input
              type="checkbox"
              :aria-label="`${group.label}: ${taxonomyLabel(value)}`"
              :checked="(modelValue[group.key] as string[]).includes(value)"
              @change="
                toggle(
                  group.key,
                  value,
                  ($event.target as HTMLInputElement).checked,
                )
              "
            />
            {{ taxonomyLabel(value) }}
          </label>
        </div>
      </fieldset>
    </template>
  </fieldset>
</template>
