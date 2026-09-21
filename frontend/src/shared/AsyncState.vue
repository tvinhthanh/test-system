<script setup lang="ts">
import { CloudOff, RefreshCw } from "@lucide/vue";
import { ApiError } from "../core/api/client";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import EmptyState from "./EmptyState.vue";
withDefaults(
  defineProps<{ loading: boolean; error?: unknown; rows?: number }>(),
  { rows: 6 },
);
defineEmits<{ retry: [] }>();
</script>
<template>
  <div
    v-if="loading"
    class="flex flex-col gap-3 py-2"
    role="status"
    aria-label="Loading data"
    aria-busy="true"
  >
    <div v-for="n in rows" :key="n" class="flex items-center gap-4">
      <Skeleton class="h-4 w-[38%]" /><Skeleton class="h-4 w-[18%]" /><Skeleton
        class="ml-auto h-4 w-[12%]"
      />
    </div>
  </div>
  <EmptyState
    v-else-if="error"
    role="alert"
    :icon="CloudOff"
    title="Unable to load data"
    :description="
      error instanceof ApiError
        ? error.message
        : 'An unexpected response was received.'
    "
  >
    <code
      v-if="error instanceof ApiError && error.requestId"
      class="text-muted-foreground w-full text-xs"
      >Request: {{ error.requestId }}</code
    >
    <Button variant="outline" size="sm" @click="$emit('retry')">
      <RefreshCw />Try again
    </Button>
  </EmptyState>
  <slot v-else />
</template>
