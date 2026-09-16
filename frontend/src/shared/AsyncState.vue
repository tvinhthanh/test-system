<script setup lang="ts">
import { CloudOff, RefreshCw } from "@lucide/vue";
import { ApiError } from "../core/api/client";
defineProps<{ loading: boolean; error?: unknown }>();
defineEmits<{ retry: [] }>();
</script>
<template>
  <div
    v-if="loading"
    class="skeleton-list"
    role="status"
    aria-label="Loading data"
    aria-busy="true"
  >
    <div v-for="n in 6" :key="n" class="skeleton-row"><i /><i /><i /></div>
  </div>
  <div v-else-if="error" class="empty-state" role="alert">
    <CloudOff :size="28" />
    <h2>Unable to load data</h2>
    <p>
      {{
        error instanceof ApiError
          ? error.message
          : "An unexpected response was received."
      }}
    </p>
    <code v-if="error instanceof ApiError && error.requestId"
      >Request: {{ error.requestId }}</code
    ><button class="button" @click="$emit('retry')">
      <RefreshCw :size="16" />Try again
    </button>
  </div>
  <slot v-else />
</template>
