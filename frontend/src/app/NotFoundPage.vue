<script setup lang="ts">
import { Compass } from "@lucide/vue";
import EmptyState from "../shared/EmptyState.vue";
import { Button } from "@/components/ui/button";
</script>
<template>
  <main class="bg-background grid min-h-svh place-items-center p-6">
    <EmptyState
      class="w-full max-w-md border-0"
      :icon="Compass"
      heading="h1"
      title="Page not found"
      description="The address does not match a project, run or test here."
    >
      <Button as-child variant="outline" size="sm"
        ><RouterLink to="/">Back to projects</RouterLink></Button
      >
    </EmptyState>
  </main>
</template>
