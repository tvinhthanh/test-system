<script setup lang="ts">
// Record a TTRC baseline measured as SPEC §5.4 asks: how many failures,
// median (and p90) time to root cause, share rerun-to-pass, the period and
// whether it was rebuilt from history (retrospective) or logged as it
// happened (prospective — the official one). Append-only: a correction is
// a new entry. Only project.configure sees this; the server checks again.
import { ref } from "vue";
import { Plus } from "@lucide/vue";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import type { BaselineKind } from "./testmgmtRepository";
import NativeSelect from "../../shared/NativeSelect.vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
const emit = defineEmits<{ saved: [] }>();
const { testmgmt } = useServices();

const open = ref(false);
const kind = ref<BaselineKind>("prospective");
const failures = ref<string | number>("");
const medianHours = ref<string | number>("");
const p90Hours = ref<string | number>("");
const rerunPercent = ref<string | number>("");
const investigationMinutes = ref<string | number>("");
const periodFrom = ref("");
const periodTo = ref("");
const note = ref("");
const busy = ref(false);
const error = ref("");

// A number input's v-model hands back a number once typed, "" when empty.
const optional = (v: string | number, scale = 1) =>
  String(v).trim() === "" ? undefined : Number(v) * scale;

async function save() {
  busy.value = true;
  error.value = "";
  try {
    await testmgmt.addBaseline(
      props.project,
      {
        kind: kind.value,
        failures: Number(failures.value),
        medianMinutes: Number(medianHours.value) * 60,
        p90Minutes: optional(p90Hours.value, 60),
        rerunPassRate: optional(rerunPercent.value, 0.01),
        flakyInvestigationMinutes: optional(investigationMinutes.value),
        periodFrom: periodFrom.value,
        periodTo: periodTo.value,
        note: note.value.trim() || undefined,
      },
      new AbortController().signal,
    );
    open.value = false;
    emit("saved");
  } catch (e) {
    error.value =
      e instanceof ApiError && e.code === "INVALID_PAYLOAD"
        ? (e.detail ?? e.message)
        : e instanceof Error
          ? e.message
          : "Not saved. Try again.";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <Sheet v-model:open="open">
    <SheetTrigger as-child>
      <Button variant="outline" size="sm"><Plus />Record a baseline</Button>
    </SheetTrigger>
    <SheetContent
      class="baseline-sheet w-full gap-0 overflow-y-auto sm:max-w-md"
    >
      <SheetHeader>
        <SheetTitle>Record a measured baseline</SheetTitle>
        <SheetDescription>
          Time to root cause before the platform, from a study of real failures.
          Prospective (logged as they happened) is the official number;
          retrospective (rebuilt from history) stands in until then.
        </SheetDescription>
      </SheetHeader>
      <form class="flex flex-col gap-4 px-4 pb-4" @submit.prevent="save">
        <div class="grid grid-cols-2 gap-3">
          <div class="flex flex-col gap-1.5">
            <Label for="bl-kind">How it was measured</Label>
            <NativeSelect id="bl-kind" v-model="kind">
              <option value="prospective">Prospective</option>
              <option value="retrospective">Retrospective</option>
            </NativeSelect>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="bl-failures">Failures measured</Label>
            <Input
              id="bl-failures"
              v-model="failures"
              type="number"
              min="1"
              required
            />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="bl-median">Median TTRC (hours)</Label>
            <Input
              id="bl-median"
              v-model="medianHours"
              type="number"
              min="0.02"
              step="0.01"
              required
            />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="bl-p90">p90 TTRC (hours)</Label>
            <Input id="bl-p90" v-model="p90Hours" type="number" step="0.01" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="bl-rerun">Rerun-to-pass (%)</Label>
            <Input
              id="bl-rerun"
              v-model="rerunPercent"
              type="number"
              min="0"
              max="100"
            />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="bl-invest">Flaky investigation (min)</Label>
            <Input
              id="bl-invest"
              v-model="investigationMinutes"
              type="number"
              min="0"
            />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="bl-from">Period from</Label>
            <Input id="bl-from" v-model="periodFrom" type="date" required />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="bl-to">Period to</Label>
            <Input id="bl-to" v-model="periodTo" type="date" required />
          </div>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="bl-note">Note</Label>
          <Input
            id="bl-note"
            v-model="note"
            maxlength="2000"
            placeholder="Who measured it, how"
          />
        </div>
        <Alert v-if="error" variant="destructive" role="alert">
          <AlertDescription>{{ error }}</AlertDescription>
        </Alert>
        <SheetFooter class="px-0">
          <Button type="submit" :disabled="busy">{{
            busy ? "Saving…" : "Save baseline"
          }}</Button>
        </SheetFooter>
      </form>
    </SheetContent>
  </Sheet>
</template>
