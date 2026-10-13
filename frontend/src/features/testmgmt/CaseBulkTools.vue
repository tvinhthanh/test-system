<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Check, Download, X } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import NativeSelect from "../../shared/NativeSelect.vue";
import ErrorNote from "./ErrorNote.vue";
import { useCaseBulkActions } from "./useCaseBulkActions";
import type {
  BulkCasePatch,
  CaseFilter,
  Suite,
  TestCase,
} from "./testmgmtRepository";
import { priorities } from "./enums";

const props = defineProps<{
  project: string;
  selected: TestCase[];
  filter: CaseFilter;
  suites: Suite[];
  canWrite: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  updated: [ids: string[]];
  clear: [];
  busy: [value: boolean];
}>();
const bulk = useCaseBulkActions();
const action = ref<"priority" | "suiteId" | "isCritical" | "automationStatus">(
  "priority",
);
const value = ref("p2");
const confirmed = ref(false);
watch(action, (name) => {
  value.value = {
    priority: "p2",
    suiteId: "",
    isCritical: "true",
    automationStatus: "automation_candidate",
  }[name];
});
watch(
  () =>
    `${action.value}:${value.value}:${props.selected.map((item) => item.id).join(",")}`,
  () => {
    confirmed.value = false;
  },
);
watch(bulk.busy, (busy) => emit("busy", busy));
const archives = computed(
  () => action.value === "automationStatus" && value.value === "obsolete",
);
async function apply() {
  if (
    props.disabled ||
    !props.canWrite ||
    !props.selected.length ||
    (archives.value && !confirmed.value)
  )
    return;
  const patch = {
    [action.value]:
      action.value === "isCritical" ? value.value === "true" : value.value,
  } as BulkCasePatch;
  const ids = await bulk.apply(props.project, props.selected, patch);
  emit("updated", ids);
}
const resultLabel = (code: string) =>
  ({
    CONFLICT: "Changed; refresh before retry",
    NOT_FOUND: "Unavailable",
    INVALID_PAYLOAD: "Invalid change",
    INTERNAL: "Not confirmed; server error",
    REJECTED: "Request rejected",
    UNCONFIRMED: "Not confirmed; refresh before retry",
    NOT_SENT: "Not sent",
  })[code] ?? code;
</script>
<template>
  <section
    class="mb-4 grid min-w-0 gap-3 border-y py-3"
    aria-label="Bulk case actions"
  >
    <div class="flex min-w-0 flex-wrap items-center gap-2">
      <span class="mr-2 text-sm">{{ selected.length }} selected</span>
      <template v-if="canWrite && selected.length">
        <NativeSelect
          v-model="action"
          aria-label="Bulk field"
          :disabled="bulk.busy.value"
        >
          <option value="priority">Priority</option>
          <option value="suiteId">Suite</option>
          <option value="isCritical">Critical</option>
          <option value="automationStatus">Automation</option>
        </NativeSelect>
        <NativeSelect
          v-model="value"
          aria-label="Bulk value"
          :disabled="bulk.busy.value"
        >
          <template v-if="action === 'priority'"
            ><option
              v-for="priority in priorities"
              :key="priority"
              :value="priority"
            >
              {{ priority }}
            </option></template
          >
          <template v-else-if="action === 'suiteId'"
            ><option value="">No suite</option>
            <option v-for="suite in suites" :key="suite.id" :value="suite.id">
              {{ suite.name }}
            </option></template
          >
          <template v-else-if="action === 'isCritical'"
            ><option value="true">Critical</option>
            <option value="false">Not critical</option></template
          >
          <template v-else
            ><option value="manual_only">Manual only</option>
            <option value="automation_candidate">Automation candidate</option>
            <option value="obsolete">Obsolete</option></template
          >
        </NativeSelect>
        <Button
          size="sm"
          :disabled="disabled || bulk.busy.value || (archives && !confirmed)"
          @click="apply"
          ><Check />Apply</Button
        >
      </template>
      <Button
        variant="outline"
        size="sm"
        :disabled="disabled || bulk.busy.value || !selected.length"
        @click="
          bulk.exportCsv(
            project,
            {},
            selected.map((item) => item.id),
          )
        "
        ><Download />Export selected</Button
      >
      <Button
        variant="outline"
        size="sm"
        :disabled="bulk.busy.value"
        @click="bulk.exportCsv(project, filter)"
        ><Download />Export filtered</Button
      >
      <Button
        v-if="selected.length"
        variant="ghost"
        size="icon-sm"
        title="Clear selection"
        aria-label="Clear selection"
        :disabled="bulk.busy.value"
        @click="emit('clear')"
        ><X
      /></Button>
    </div>
    <label
      v-if="archives && selected.length && canWrite"
      class="flex items-center gap-2 text-sm"
      ><input
        v-model="confirmed"
        type="checkbox"
        :disabled="bulk.busy.value"
      />Confirm marking {{ selected.length }} cases obsolete</label
    >
    <p
      v-if="bulk.total.value"
      class="text-muted-foreground text-sm"
      role="status"
    >
      {{ bulk.updated.value }} updated ·
      {{ bulk.results.value.length - bulk.updated.value }} need attention ·
      {{ bulk.results.value.length }}/{{ bulk.total.value }} results
    </p>
    <p
      v-if="bulk.exported.value !== null"
      class="text-muted-foreground text-sm"
      role="status"
    >
      Exported {{ bulk.exported.value }} cases
    </p>
    <ErrorNote v-if="bulk.error.value" :error="bulk.error.value" />
    <ul
      v-if="bulk.results.value.some((item) => !item.updated)"
      class="max-h-48 space-y-1 overflow-y-auto text-sm"
    >
      <li
        v-for="item in bulk.results.value.filter((item) => !item.updated)"
        :key="item.id"
        class="break-words"
      >
        {{ item.title }}: {{ resultLabel(item.code) }}
      </li>
    </ul>
  </section>
</template>
