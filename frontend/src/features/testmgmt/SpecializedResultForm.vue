<script setup lang="ts">
import { ref, watch, toRaw } from "vue";
import { Save } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import NativeSelect from "../../shared/NativeSelect.vue";
import ReportFieldsEditor from "./ReportFieldsEditor.vue";
import {
  emptyReportData,
  reportFields,
  reportKinds,
  type SpecializedInput,
} from "./specializedReports";
const props = defineProps<{ initial: SpecializedInput; disabled: boolean }>();
const emit = defineEmits<{ submit: [input: SpecializedInput] }>();
const metadata = [
  { key: "title", label: "Report title", max: 500 },
  { key: "environment", label: "Environment", max: 128 },
  { key: "applicationVersion", label: "Application version", max: 128 },
  { key: "source", label: "Source / tool", max: 128 },
  { key: "toolVersion", label: "Tool version", max: 128 },
  { key: "adapterVersion", label: "Adapter version", max: 128 },
] as const;

const draft = ref(structuredClone(toRaw(props.initial)));
const started = ref(""),
  finished = ref("");
watch(
  () => draft.value.kind,
  (value) => {
    draft.value.details = emptyReportData(reportFields[value]);
  },
);
function save() {
  emit("submit", {
    ...draft.value,
    startedAt: started.value
      ? new Date(started.value).toISOString()
      : "0001-01-01T00:00:00Z",
    finishedAt: finished.value
      ? new Date(finished.value).toISOString()
      : "0001-01-01T00:00:00Z",
  });
}
</script>
<template>
  <form class="mb-6 min-w-0 space-y-5 border-b pb-6" @submit.prevent="save">
    <fieldset
      :disabled="disabled"
      class="grid min-w-0 gap-4 sm:grid-cols-2 xl:grid-cols-3"
    >
      <label class="grid gap-1.5 text-sm"
        >Testing group<NativeSelect
          v-model="draft.kind"
          aria-label="Testing group"
          ><option v-for="item in reportKinds" :key="item" :value="item">
            {{ item }}
          </option></NativeSelect
        ></label
      >
      <label
        v-for="field in metadata"
        :key="field.key"
        class="grid min-w-0 gap-1.5 text-sm"
        >{{ field.label
        }}<Input
          v-model="draft[field.key]"
          :aria-label="field.label"
          required
          :maxlength="field.max"
      /></label>
      <label class="grid gap-1.5 text-sm"
        >Case ID<Input
          v-model="draft.testCaseId"
          aria-label="Case ID"
          maxlength="36"
      /></label>
      <label class="grid gap-1.5 text-sm"
        >Execution state<NativeSelect
          v-model="draft.state"
          aria-label="Execution state"
          ><option value="completed">Completed</option>
          <option value="not_run">Not run</option>
          <option value="not_applicable">Not applicable</option>
          <option value="import_error">Import error</option></NativeSelect
        ></label
      >
      <label class="grid gap-1.5 text-sm"
        >Started at<Input
          v-model="started"
          type="datetime-local"
          aria-label="Started at"
          :required="draft.state === 'completed'"
      /></label>
      <label class="grid gap-1.5 text-sm"
        >Finished at<Input
          v-model="finished"
          type="datetime-local"
          aria-label="Finished at"
          :required="draft.state === 'completed'"
      /></label>
    </fieldset>
    <ReportFieldsEditor
      v-model="draft.details"
      :fields="reportFields[draft.kind]"
      :disabled="disabled"
    />
    <label class="grid gap-1.5 text-sm"
      >Original report<textarea
        v-model="draft.originalReport"
        aria-label="Original report"
        :required="['completed', 'import_error'].includes(draft.state)"
        :readonly="disabled"
        rows="5"
        maxlength="524288"
        class="border-input bg-background w-full rounded-md border px-3 py-2 font-mono text-xs"
      />
    </label>
    <label class="grid gap-1.5 text-sm"
      >Notes<textarea
        v-model="draft.notes"
        aria-label="Notes"
        :required="draft.state !== 'completed'"
        rows="3"
        maxlength="4000"
        class="border-input bg-background w-full rounded-md border px-3 py-2"
      />
    </label>
    <p v-if="draft.retestOf" class="text-muted-foreground break-all text-sm">
      Retest of {{ draft.retestOf }}
    </p>
    <Button type="submit" size="sm" :disabled="disabled"
      ><Save />Record result</Button
    >
  </form>
</template>
