<script setup lang="ts">
// JUnit XML backfill (SPEC §6.8.5, QĐ-13): one report file becomes one
// historic run, dated from the report, through the same pipeline as the
// reporter. Only people who may configure the project see the button
// (project.configure, maintainer and up); the server checks it again.
import { computed, ref } from "vue";
import { FileUp, Upload } from "@lucide/vue";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import type { JUnitImportResult } from "./reportsRepository";
import NativeSelect from "../../shared/NativeSelect.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";

const props = defineProps<{ project: string }>();
const emit = defineEmits<{ imported: [result: JUnitImportResult] }>();
const { reports } = useServices();

const open = ref(false);
const file = ref<File | null>(null);
const environment = ref("ci");
const framework = ref("junit");
const branch = ref("");
const startedAt = ref("");
const busy = ref(false);
const error = ref("");
const result = ref<JUnitImportResult | null>(null);
const frameworks = [
  { value: "junit", label: "JUnit / Maven / Gradle" },
  { value: "pytest", label: "pytest" },
  { value: "jest", label: "Jest" },
  { value: "other", label: "Other" },
];
const runPath = computed(() =>
  result.value
    ? `/p/${encodeURIComponent(props.project)}/runs/${encodeURIComponent(result.value.runId)}`
    : "",
);

function pick(event: Event) {
  file.value = (event.target as HTMLInputElement).files?.[0] ?? null;
  error.value = "";
  result.value = null;
}

// The server's own words where they tell the person what to fix.
function explain(e: unknown) {
  if (!(e instanceof ApiError)) return "The import failed. Try again.";
  if (e.code === "INVALID_JUNIT")
    return `This is not a JUnit report we can read: ${e.detail ?? e.message}`;
  if (e.code === "INVALID_PAYLOAD") return e.detail ?? e.message;
  if (e.code === "RUN_CONFLICT")
    return "That run id belongs to a run a reporter sent; an import never adds to it.";
  if (e.code === "REPORT_TOO_LARGE")
    return "The report is over 20 MB. Split it by suite and import each part.";
  return e.message;
}

async function submit() {
  if (!file.value || busy.value) return;
  busy.value = true;
  error.value = "";
  result.value = null;
  try {
    const at = startedAt.value ? new Date(startedAt.value) : null;
    result.value = await reports.importJUnit(
      props.project,
      file.value,
      {
        environment: environment.value.trim() || undefined,
        framework: framework.value,
        branch: branch.value.trim() || undefined,
        startedAt:
          at && !Number.isNaN(at.getTime())
            ? at.toISOString().replace(/\.\d{3}Z$/, "Z")
            : undefined,
      },
      new AbortController().signal,
    );
    emit("imported", result.value);
  } catch (e) {
    error.value = explain(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <Sheet v-model:open="open">
    <SheetTrigger as-child>
      <Button variant="outline" size="sm"><FileUp />Import JUnit</Button>
    </SheetTrigger>
    <SheetContent class="junit-import w-full gap-0 sm:max-w-md">
      <SheetHeader>
        <SheetTitle>Import a JUnit report</SheetTitle>
        <SheetDescription>
          One XML report becomes one past run, dated from the report, with its
          failures, retries and flaky passes. Importing the same file again
          changes nothing.
        </SheetDescription>
      </SheetHeader>
      <form class="flex flex-col gap-4 px-4" @submit.prevent="submit">
        <div class="flex flex-col gap-1.5">
          <Label for="junit-file">Report (.xml)</Label>
          <Input
            id="junit-file"
            type="file"
            accept=".xml,application/xml,text/xml"
            @change="pick"
          />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="flex flex-col gap-1.5">
            <Label for="junit-env">Environment</Label>
            <Input id="junit-env" v-model="environment" maxlength="64" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="junit-framework">Framework</Label>
            <NativeSelect id="junit-framework" v-model="framework">
              <option v-for="f in frameworks" :key="f.value" :value="f.value">
                {{ f.label }}
              </option>
            </NativeSelect>
          </div>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="junit-branch">Branch</Label>
          <Input
            id="junit-branch"
            v-model="branch"
            placeholder="main"
            maxlength="255"
          />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="junit-date">Run date (only if the report has none)</Label>
          <Input id="junit-date" v-model="startedAt" type="datetime-local" />
        </div>
        <Alert v-if="error" variant="destructive" role="alert">
          <AlertTitle>Not imported</AlertTitle>
          <AlertDescription>{{ error }}</AlertDescription>
        </Alert>
        <Alert v-if="result" role="status" class="junit-result">
          <AlertTitle>{{
            result.created ? "Imported" : "Already imported"
          }}</AlertTitle>
          <AlertDescription class="block">
            {{ result.tests }} tests, run {{ result.status
            }}<template v-if="result.flakyCount">
              · {{ result.flakyCount }} flaky</template
            >.
            <RouterLink class="underline" :to="runPath" @click="open = false"
              >Open the run</RouterLink
            >
          </AlertDescription>
        </Alert>
        <SheetFooter class="px-0">
          <Button type="submit" :disabled="!file || busy"
            ><Upload />{{ busy ? "Importing…" : "Import" }}</Button
          >
        </SheetFooter>
      </form>
    </SheetContent>
  </Sheet>
</template>
