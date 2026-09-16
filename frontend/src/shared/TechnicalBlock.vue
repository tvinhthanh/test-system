<script setup lang="ts">
import { Copy, Check } from "@lucide/vue";
import { ref } from "vue";
defineProps<{ title: string; text: string }>();
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
        @click="copy(text)"
      >
        <Check v-if="feedback === 'Copied'" :size="16" /><Copy
          v-else
          :size="16"
        />
      </button>
    </header>
    <pre>{{ text }}</pre>
  </section>
</template>
