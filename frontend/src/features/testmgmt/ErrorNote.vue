<script setup lang="ts">
import { ApiError } from "../../core/api/client";
// The server's word on a refused write (403 role, 409 conflict, 400 field),
// rendered where the action was; ApiError carries the code-mapped message.
defineProps<{ error: unknown; fallback?: string }>();
</script>
<template>
  <p
    class="bg-danger-soft text-danger border-danger/20 rounded-md border px-3 py-2 text-sm"
    role="alert"
  >
    {{
      error instanceof ApiError
        ? error.message
        : (fallback ?? "The action could not be completed.")
    }}
  </p>
</template>
