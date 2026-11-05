<script setup lang="ts">
import { browsers, devices, type RunVariant } from "./matrix";

const props = defineProps<{ modelValue: RunVariant[] }>();
const emit = defineEmits<{ "update:modelValue": [value: RunVariant[]] }>();
const names = { chromium: "Chromium", firefox: "Firefox", webkit: "WebKit" };
function toggle(variant: RunVariant) {
  const next = new Set(props.modelValue);
  if (next.has(variant)) next.delete(variant);
  else next.add(variant);
  emit("update:modelValue", [...next]);
}
</script>

<template>
  <fieldset class="bg-card rounded-lg border p-3">
    <legend class="px-1 text-sm font-medium">Browser × device</legend>
    <p class="text-muted-foreground mb-2 text-xs">
      No selection runs Chromium on desktop.
    </p>
    <div class="grid gap-1 sm:grid-cols-3">
      <template v-for="browser in browsers" :key="browser">
        <label
          v-for="device in devices"
          :key="`${browser}/${device}`"
          class="hover:bg-accent flex min-h-11 cursor-pointer items-center gap-2 rounded px-2 text-sm"
        >
          <input
            type="checkbox"
            class="accent-primary size-4"
            :checked="modelValue.includes(`${browser}/${device}`)"
            @change="toggle(`${browser}/${device}`)"
          />
          {{ names[browser] }} · {{ device }}
        </label>
      </template>
    </div>
  </fieldset>
</template>
