<script setup lang="ts">
import { Copy, Check } from "@lucide/vue";
import { computed, ref } from "vue";
import { stripAnsi } from "./format";
const props = defineProps<{ title: string; text: string }>();
// Report text is untrusted and may still carry terminal colour codes from
// frameworks that thought they were printing to a TTY (the reporter strips
// them at the source since 2026-09-17; older rows and other reporters may
// not). Shown and copied as clean text; never rendered as HTML.
const clean = computed(() => stripAnsi(props.text));
const feedback = ref("");
async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    feedback.value = "Copied";
  } catch {
    feedback.value = "Copy failed. Select the text to copy it.";
  }
}
</script>
<template>
  <section class="technical">
    <header>
      <h2>{{ title }}</h2>
      <span role="status">{{ feedback }}</span
      ><button
        class="icon-button"
        :aria-label="`Copy ${title}`"
        :title="`Copy ${title}`"
        @click="copy(clean)"
      >
        <Check v-if="feedback === 'Copied'" :size="16" /><Copy
          v-else
          :size="16"
        />
      </button>
    </header>
    <pre>{{ clean }}</pre>
  </section>
</template>
