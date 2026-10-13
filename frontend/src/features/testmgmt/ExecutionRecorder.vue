<script setup lang="ts">
import { computed, ref, shallowRef } from "vue";
import { RotateCcw, RefreshCw } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { executionStatuses, type ExecutionStatus } from "./enums";
import type { Execution, StepResult, TestCase } from "./testmgmtRepository";

const props = defineProps<{
  testCase: TestCase;
  latest?: Execution;
  disabled: boolean;
}>();
const emit = defineEmits<{
  record: [
    input: {
      status: ExecutionStatus;
      note: string;
      bugUrl: string;
      retestOf: string;
      stepResults: StepResult[];
      caseUpdatedAt: string;
    },
  ];
}>();
const note = ref("");
const displayedCase = shallowRef(props.testCase);
const caseChanged = computed(
  () => displayedCase.value.updatedAt !== props.testCase.updatedAt,
);
function reloadCase() {
  displayedCase.value = props.testCase;
  results.value = {};
}
const bugUrl = ref("");
const retest = ref(Boolean(props.latest));
const results = ref<
  Record<number, { status: ExecutionStatus | ""; actual: string }>
>({});
const missingStepStatus = computed(() =>
  Object.values(results.value).some(
    (item) => item.actual.trim() && !item.status,
  ),
);
const failedStep = computed(() =>
  Object.values(results.value).some(
    (item) => item.status === "failed" || item.status === "blocked",
  ),
);
const invalidUrl = computed(() => {
  if (!bugUrl.value.trim()) return false;
  try {
    const url = new URL(bugUrl.value.trim());
    return (
      !["http:", "https:"].includes(url.protocol) ||
      !url.hostname ||
      !!url.username ||
      !!url.password
    );
  } catch {
    return true;
  }
});
function result(index: number) {
  return (results.value[index] ??= { status: "", actual: "" });
}
function record(status: ExecutionStatus) {
  if (
    props.disabled ||
    caseChanged.value ||
    invalidUrl.value ||
    missingStepStatus.value ||
    (status === "passed" && failedStep.value)
  )
    return;
  emit("record", {
    status,
    caseUpdatedAt: displayedCase.value.updatedAt,
    note: note.value,
    bugUrl: bugUrl.value.trim(),
    retestOf: retest.value ? (props.latest?.id ?? "") : "",
    stepResults: Object.entries(results.value)
      .filter(([, item]) => item.status !== "")
      .map(([index, item]) => ({
        stepIndex: Number(index),
        status: item.status as ExecutionStatus,
        actual: item.actual,
      })),
  });
}
</script>

<template>
  <details class="mt-3 border-t pt-2">
    <summary class="cursor-pointer text-sm font-medium">
      Record execution
    </summary>
    <fieldset :disabled="disabled" class="mt-3 grid min-w-0 gap-3">
      <div v-if="caseChanged" class="grid gap-2">
        <p class="text-destructive text-sm" role="alert">
          Case changed. Reload the case before recording; step results will be
          cleared.
        </p>
        <Button variant="outline" size="sm" @click="reloadCase"
          ><RefreshCw />Reload case</Button
        >
      </div>
      <div v-if="latest" class="flex items-center gap-2 text-sm">
        <input :id="`retest-${testCase.id}`" v-model="retest" type="checkbox" />
        <label
          :for="`retest-${testCase.id}`"
          class="inline-flex items-center gap-1"
          ><RotateCcw :size="14" />Retest latest execution</label
        >
      </div>
      <div v-if="displayedCase.preconditions" class="text-sm">
        <h3 class="font-medium">Preconditions</h3>
        <p class="whitespace-pre-wrap break-words">
          {{ displayedCase.preconditions }}
        </p>
      </div>
      <div v-if="displayedCase.testData" class="text-sm">
        <h3 class="font-medium">Test data</h3>
        <p class="whitespace-pre-wrap break-words">
          {{ displayedCase.testData }}
        </p>
      </div>
      <ol v-if="displayedCase.steps.length" class="space-y-3">
        <li
          v-for="(step, index) in displayedCase.steps"
          :key="index"
          class="grid min-w-0 gap-2 border-b pb-3"
        >
          <p class="whitespace-pre-wrap break-words text-sm">
            {{ index + 1 }}. {{ step.action }}
          </p>
          <p
            class="text-muted-foreground whitespace-pre-wrap break-words text-sm"
          >
            Expected: {{ step.expected }}
          </p>
          <div class="grid gap-2 sm:grid-cols-[10rem_minmax(0,1fr)]">
            <select
              v-model="result(index).status"
              class="bg-background h-9 rounded-md border px-2 text-sm"
              :aria-label="`Step ${index + 1} result for ${testCase.title}`"
            >
              <option value="">Not recorded</option>
              <option
                v-for="status in executionStatuses"
                :key="status"
                :value="status"
              >
                {{ status }}
              </option>
            </select>
            <Input
              v-model="result(index).actual"
              maxlength="4000"
              :aria-label="`Step ${index + 1} actual result for ${testCase.title}`"
              placeholder="Actual result"
            />
          </div>
        </li>
      </ol>
      <p
        v-if="displayedCase.expected"
        class="whitespace-pre-wrap break-words text-sm"
      >
        Expected: {{ displayedCase.expected }}
      </p>
      <label class="grid gap-1 text-sm"
        >Note<Input v-model="note" maxlength="4000"
      /></label>
      <label class="grid gap-1 text-sm"
        >Bug URL<Input
          v-model="bugUrl"
          type="url"
          maxlength="2000"
          :aria-invalid="invalidUrl"
      /></label>
      <p v-if="invalidUrl" class="text-destructive text-sm" role="alert">
        Enter an HTTP or HTTPS URL without credentials.
      </p>
      <div
        class="flex flex-wrap gap-1"
        role="group"
        :aria-label="`Record ${testCase.title}`"
      >
        <Button
          v-for="status in executionStatuses"
          :key="status"
          :variant="status === 'passed' ? 'default' : 'outline'"
          size="xs"
          :disabled="
            disabled ||
            caseChanged ||
            invalidUrl ||
            missingStepStatus ||
            (status === 'passed' && failedStep)
          "
          @click="record(status)"
          >{{ status }}</Button
        >
      </div>
      <p v-if="missingStepStatus" class="text-destructive text-sm" role="alert">
        Choose a status for each step with an actual result.
      </p>
      <p v-if="failedStep" class="text-muted-foreground text-sm">
        Passed is unavailable while a step is failed or blocked.
      </p>
    </fieldset>
  </details>
</template>
