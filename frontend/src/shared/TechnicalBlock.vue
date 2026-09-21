<script setup lang="ts">
import { Copy, Check } from "@lucide/vue";
import { computed, ref } from "vue";
import { stripAnsi } from "./format";
import { Button } from "@/components/ui/button";
const props = defineProps<{ title: string; text: string }>();
// Report text is untrusted and may still carry terminal colour codes from
// frameworks that thought they were printing to a TTY (the reporter strips
// them at the source since 2026-09-17; older rows and other reporters may
// not). Shown and copied as clean text; never rendered as HTML. QĐ-9 #2: the
// explicit Copy button is a functional requirement, not decoration.
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
  <section class="technical bg-card overflow-hidden rounded-lg border">
    <header
      class="bg-muted/60 flex min-h-10 items-center gap-3 border-b px-3 py-1.5"
    >
      <h2 class="text-xs font-semibold tracking-wide uppercase">{{ title }}</h2>
      <span
        role="status"
        class="text-muted-foreground ml-auto text-xs"
        aria-live="polite"
        >{{ feedback }}</span
      >
      <Button
        variant="ghost"
        size="icon-sm"
        :aria-label="`Copy ${title}`"
        :title="`Copy ${title}`"
        @click="copy(clean)"
      >
        <Check v-if="feedback === 'Copied'" /><Copy v-else />
      </Button>
    </header>
    <pre
      class="m-0 max-h-[28rem] overflow-auto px-4 py-3 font-mono text-[13px] leading-relaxed break-words whitespace-pre-wrap"
      >{{ clean }}</pre
    >
  </section>
</template>
