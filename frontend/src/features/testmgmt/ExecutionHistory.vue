<script setup lang="ts">
import type { Execution } from "./testmgmtRepository";
import { time } from "../../shared/format";
import StatusBadge from "../../shared/StatusBadge.vue";
defineProps<{ executions: Execution[]; contentUrl: (id: string) => string }>();
</script>

<template>
  <details v-if="executions.length" class="mt-3 border-t pt-2">
    <summary class="cursor-pointer text-sm font-medium">
      Execution history ({{ executions.length }})
    </summary>
    <ol class="mt-3 space-y-4">
      <li
        v-for="execution in executions"
        :key="execution.id"
        class="min-w-0 border-b pb-3 text-sm"
      >
        <div class="flex flex-wrap items-center gap-2">
          <StatusBadge :status="execution.status" />{{
            time(execution.executedAt)
          }}<span>{{ execution.executedBy }}</span>
        </div>
        <p class="mt-1 break-words font-medium">{{ execution.caseTitle }}</p>
        <p
          v-if="execution.retestOf"
          class="text-muted-foreground break-all text-xs"
        >
          Retest of {{ execution.retestOf }}
        </p>
        <p v-if="!execution.caseSnapshot" class="text-muted-foreground text-xs">
          Legacy execution: case snapshot unavailable.
        </p>
        <p v-if="execution.note" class="mt-1 whitespace-pre-wrap break-words">
          {{ execution.note }}
        </p>
        <a
          v-if="execution.bugUrl"
          :href="execution.bugUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="mt-1 block break-all underline"
          >{{ execution.bugUrl }}</a
        >
        <details v-if="execution.caseSnapshot" class="mt-2">
          <summary class="cursor-pointer">Case at execution</summary>
          <p
            v-if="
              execution.caseSnapshot.preconditions == null ||
              execution.caseSnapshot.testData == null
            "
            class="text-muted-foreground mt-2 text-xs"
          >
            Legacy snapshot: preconditions or test data unavailable.
          </p>
          <div v-if="execution.caseSnapshot.preconditions" class="mt-2">
            <h3 class="font-medium">Preconditions</h3>
            <p class="whitespace-pre-wrap break-words">
              {{ execution.caseSnapshot.preconditions }}
            </p>
          </div>
          <div v-if="execution.caseSnapshot.testData" class="mt-2">
            <h3 class="font-medium">Test data</h3>
            <p class="whitespace-pre-wrap break-words">
              {{ execution.caseSnapshot.testData }}
            </p>
          </div>
          <p class="text-muted-foreground text-xs">
            Case updated {{ time(execution.caseSnapshot.updatedAt) }}
          </p>
          <ol class="mt-2 space-y-2">
            <li
              v-for="(step, index) in execution.caseSnapshot.steps"
              :key="index"
              class="whitespace-pre-wrap break-words"
            >
              <p>{{ index + 1 }}. {{ step.action }}</p>
              <p class="text-muted-foreground">Expected: {{ step.expected }}</p>
              <template
                v-for="result in execution.stepResults.filter(
                  (item) => item.stepIndex === index,
                )"
                :key="result.stepIndex"
                ><StatusBadge :status="result.status" />
                <p>Actual: {{ result.actual }}</p></template
              >
            </li>
          </ol>
          <p
            v-if="execution.caseSnapshot.expected"
            class="mt-2 whitespace-pre-wrap break-words"
          >
            Expected: {{ execution.caseSnapshot.expected }}
          </p>
        </details>
        <ul v-if="execution.evidence.length" class="mt-2 flex flex-wrap gap-2">
          <li v-for="file in execution.evidence" :key="file.id" class="min-w-0">
            <a
              :href="contentUrl(file.id)"
              target="_blank"
              rel="noopener"
              class="break-all underline"
              >{{ file.name }}</a
            >
          </li>
        </ul>
      </li>
    </ol>
  </details>
</template>
