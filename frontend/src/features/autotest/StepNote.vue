<script setup lang="ts">
import { Check, Copy } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { Button } from "@/components/ui/button";

const props = defineProps<{ text: string }>();
const long = computed(
  () => props.text.length > 180 || props.text.includes("\n"),
);
const feedback = ref("");
watch(
  () => props.text,
  () => (feedback.value = ""),
);
async function copy() {
  try {
    await navigator.clipboard.writeText(props.text);
    feedback.value = "Copied";
  } catch {
    feedback.value = "Copy failed. Select the text to copy it.";
  }
}
</script>

<template>
  <div class="bg-muted/60 mt-1 rounded px-2 py-1.5 text-xs">
    <details v-if="long">
      <summary class="cursor-pointer font-medium">Check details</summary>
      <pre
        class="mt-2 max-h-64 overflow-auto font-mono break-words whitespace-pre-wrap"
        >{{ text }}</pre
      >
    </details>
    <pre v-else class="font-mono break-words whitespace-pre-wrap">{{
      text
    }}</pre>
    <div class="mt-1 flex items-center justify-end gap-2">
      <span role="status" class="text-muted-foreground">{{ feedback }}</span>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        aria-label="Copy check details"
        @click="copy"
      >
        <Check v-if="feedback === 'Copied'" /><Copy v-else />Copy
      </Button>
    </div>
  </div>
</template>
