<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Save, Search, Trash2 } from "@lucide/vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Plan, TestCase } from "./testmgmtRepository";
import AsyncState from "../../shared/AsyncState.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const props = defineProps<{ project: string; plan: Plan; disabled: boolean }>();
const emit = defineEmits<{ save: [caseIds: string[]] }>();
const { testmgmt } = useServices();
const query = ref("");
const selected = ref<string[]>([]);
const matches = useResource<TestCase[]>();
watch(
  () =>
    `${props.plan.id}:${props.plan.cases.map((item) => item.testCase.id).join(",")}`,
  () => {
    selected.value = props.plan.cases.map((item) => item.testCase.id);
  },
  { immediate: true },
);
const recorded = computed(
  () => new Set(props.plan.executions.map((item) => item.testCaseId)),
);
function search() {
  void matches.load((signal) =>
    testmgmt.testCases(props.project, { q: query.value }, signal),
  );
}
watch(() => props.project, search, { immediate: true });
const changed = computed(
  () =>
    selected.value.length !== props.plan.cases.length ||
    props.plan.cases.some((item) => !selected.value.includes(item.testCase.id)),
);
</script>

<template>
  <details class="mb-5 border-b pb-4">
    <summary class="cursor-pointer text-sm font-medium">
      Plan scope ({{ plan.cases.length }})
    </summary>
    <fieldset :disabled="disabled" class="mt-3 grid min-w-0 gap-3">
      <form class="flex min-w-0 gap-2" @submit.prevent="search">
        <Input
          v-model="query"
          type="search"
          aria-label="Search cases to add"
          placeholder="Search cases"
        />
        <Button
          type="submit"
          variant="outline"
          size="icon"
          title="Search cases"
          aria-label="Search cases"
          ><Search
        /></Button>
      </form>
      <AsyncState
        :loading="matches.loading.value"
        :error="matches.error.value"
        @retry="search"
      >
        <p class="text-muted-foreground text-xs">
          Search results: {{ matches.data.value?.length ?? 0 }}
        </p>
        <ul class="max-h-64 space-y-2 overflow-y-auto">
          <li
            v-for="testCase in matches.data.value"
            :key="testCase.id"
            class="flex min-w-0 items-start gap-2 text-sm"
          >
            <input
              :id="`scope-${testCase.id}`"
              v-model="selected"
              type="checkbox"
              :value="testCase.id"
              :disabled="
                disabled ||
                recorded.has(testCase.id) ||
                testCase.automationStatus === 'obsolete'
              "
              class="mt-1 shrink-0"
            />
            <label :for="`scope-${testCase.id}`" class="min-w-0 break-words"
              >{{ testCase.externalKey }} {{ testCase.title }}</label
            >
          </li>
        </ul>
      </AsyncState>
      <ul class="space-y-2">
        <li
          v-for="item in plan.cases.filter((item) =>
            selected.includes(item.testCase.id),
          )"
          :key="item.testCase.id"
          class="flex min-w-0 items-center justify-between gap-2 text-sm"
        >
          <span class="break-words">{{ item.testCase.title }}</span>
          <Button
            variant="ghost"
            size="icon-sm"
            :disabled="disabled || recorded.has(item.testCase.id)"
            :aria-label="`Remove ${item.testCase.title} from plan`"
            :title="
              recorded.has(item.testCase.id)
                ? 'Recorded executions keep this case in the plan'
                : 'Remove case from plan'
            "
            @click="selected = selected.filter((id) => id !== item.testCase.id)"
            ><Trash2
          /></Button>
        </li>
      </ul>
      <div class="flex flex-wrap items-center gap-3">
        <span class="text-muted-foreground text-sm"
          >Selected: {{ selected.length }}</span
        >
        <Button
          size="sm"
          :disabled="disabled || !changed"
          @click="emit('save', [...selected])"
          ><Save />Save scope</Button
        >
      </div>
    </fieldset>
  </details>
</template>
