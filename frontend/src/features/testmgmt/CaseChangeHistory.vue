<script setup lang="ts">
import type { CaseChange } from "./testmgmtRepository";
import { time } from "../../shared/format";
defineProps<{ changes: CaseChange[]; truncated: boolean }>();
</script>
<template>
  <section class="mt-6 min-w-0 border-t pt-4" aria-label="Case change history">
    <h2 class="text-sm font-semibold">Change history</h2>
    <p v-if="truncated" class="text-muted-foreground mt-2 text-xs">
      Latest 50 changes. Older changes are retained.
    </p>
    <p v-if="!changes.length" class="text-muted-foreground mt-2 text-sm">
      No recorded changes.
    </p>
    <ol v-else class="mt-3 divide-y">
      <li
        v-for="change in changes"
        :key="change.id"
        class="min-w-0 py-3 text-sm"
      >
        <div class="flex flex-wrap gap-2">
          <span>{{
            change.action === "case.create"
              ? "Created"
              : change.action === "case.import"
                ? "Imported"
                : "Updated"
          }}</span
          ><span class="break-all">{{ change.actor }}</span
          ><time class="text-muted-foreground" :datetime="change.createdAt">{{
            time(change.createdAt)
          }}</time>
        </div>
        <p class="text-muted-foreground mt-1 break-words">
          {{
            change.changedFields
              .map((field) => field.replaceAll("_", " "))
              .join(", ")
          }}
        </p>
      </li>
    </ol>
  </section>
</template>
