<script setup lang="ts">
import { Moon, Sun, SunMoon } from "@lucide/vue";
import { computed } from "vue";
import { useTheme } from "../shared/theme";
import { Button } from "@/components/ui/button";
const { choice, cycle } = useTheme();
const label = computed(
  () =>
    ({
      light: "Theme: light. Switch to dark",
      dark: "Theme: dark. Switch to system",
      system: "Theme: system. Switch to light",
    })[choice.value],
);
</script>
<template>
  <Button
    variant="ghost"
    size="icon-sm"
    :aria-label="label"
    :title="label"
    @click="cycle"
  >
    <Sun v-if="choice === 'light'" /><Moon
      v-else-if="choice === 'dark'"
    /><SunMoon v-else />
  </Button>
</template>
