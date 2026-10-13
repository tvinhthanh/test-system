<script setup lang="ts">
import { computed, useId } from "vue";
const props = defineProps<{
  preconditions: string;
  testData: string;
  readonly?: boolean;
}>();
const emit = defineEmits<{
  "update:preconditions": [value: string];
  "update:testData": [value: string];
}>();
const id = useId();
const bytes = (value: string) => new TextEncoder().encode(value.trim()).length;
const preconditionBytes = computed(() => bytes(props.preconditions));
const dataBytes = computed(() => bytes(props.testData));
function input(field: "preconditions" | "testData", event: Event) {
  const target = event.target as HTMLTextAreaElement;
  target.setCustomValidity(
    bytes(target.value) > 16000 ? "Maximum 16,000 UTF-8 bytes." : "",
  );
  if (field === "preconditions") emit("update:preconditions", target.value);
  else emit("update:testData", target.value);
}
</script>
<template>
  <div class="grid min-w-0 gap-4 sm:grid-cols-2">
    <div class="min-w-0 space-y-2">
      <label :for="`${id}-preconditions`" class="text-sm font-medium"
        >Preconditions</label
      >
      <textarea
        :id="`${id}-preconditions`"
        :value="preconditions"
        :readonly="readonly"
        rows="4"
        maxlength="16000"
        :aria-invalid="preconditionBytes > 16000"
        class="bg-background border-input focus-visible:ring-ring w-full min-w-0 rounded-md border px-3 py-2 text-sm focus-visible:ring-2"
        @input="input('preconditions', $event)"
      />
      <p
        v-if="preconditionBytes > 16000"
        class="text-destructive text-xs"
        role="alert"
      >
        Maximum 16,000 UTF-8 bytes.
      </p>
    </div>
    <div class="min-w-0 space-y-2">
      <label :for="`${id}-test-data`" class="text-sm font-medium"
        >Test data</label
      >
      <textarea
        :id="`${id}-test-data`"
        :value="testData"
        :readonly="readonly"
        rows="4"
        maxlength="16000"
        :aria-invalid="dataBytes > 16000"
        class="bg-background border-input focus-visible:ring-ring w-full min-w-0 rounded-md border px-3 py-2 text-sm focus-visible:ring-2"
        @input="input('testData', $event)"
      />
      <p v-if="dataBytes > 16000" class="text-destructive text-xs" role="alert">
        Maximum 16,000 UTF-8 bytes.
      </p>
    </div>
  </div>
</template>
