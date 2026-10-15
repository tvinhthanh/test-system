<script setup lang="ts">
import { Plus, Trash2 } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import NativeSelect from "../../shared/NativeSelect.vue";
import {
  emptyReportData,
  type ReportField,
  type ReportData,
} from "./specializedReports";
const props = defineProps<{
  fields: ReportField[];
  modelValue: ReportData;
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: ReportData] }>();
const update = (key: string, value: unknown) =>
  emit("update:modelValue", { ...props.modelValue, [key]: value });
const rows = (key: string) =>
  (props.modelValue[key] as ReportData[] | undefined) ?? [];
function rowUpdate(key: string, index: number, value: ReportData) {
  update(
    key,
    rows(key).map((r, i) => (i === index ? value : r)),
  );
}
function input(field: ReportField, event: Event) {
  const value = (event.target as HTMLInputElement).value;
  update(
    field.key,
    field.type === "number"
      ? value === ""
        ? null
        : Number(value)
      : field.type === "date"
        ? value === ""
          ? null
          : new Date(value).toISOString()
        : field.type === "lines"
          ? value
              .split(/\r?\n/)
              .map((v) => v.trim())
              .filter(Boolean)
          : value,
  );
}
function dateInput(value: unknown) {
  if (typeof value !== "string" || !Number.isFinite(Date.parse(value)))
    return "";
  const d = new Date(value);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}
</script>
<template>
  <div class="grid min-w-0 gap-4 sm:grid-cols-2">
    <template v-for="field in fields" :key="field.key">
      <fieldset
        v-if="field.fields"
        class="min-w-0 space-y-3 border-y py-3 sm:col-span-2"
        :disabled="disabled"
      >
        <legend class="text-sm font-medium">{{ field.label }}</legend>
        <template v-if="field.repeat">
          <div
            v-for="(row, index) in rows(field.key)"
            :key="index"
            class="min-w-0 space-y-3 border-b pb-4"
          >
            <div class="flex items-center justify-between">
              <span class="text-muted-foreground text-xs"
                >{{ field.label }} {{ index + 1 }}</span
              ><Button
                v-if="!disabled"
                type="button"
                variant="ghost"
                size="icon-sm"
                :aria-label="`Remove ${field.label} ${index + 1}`"
                :title="`Remove ${field.label} ${index + 1}`"
                @click="
                  update(
                    field.key,
                    rows(field.key).filter((_, i) => i !== index),
                  )
                "
                ><Trash2
              /></Button>
            </div>
            <ReportFieldsEditor
              :fields="field.fields"
              :model-value="row"
              :disabled="disabled"
              @update:model-value="rowUpdate(field.key, index, $event)"
            />
          </div>
          <Button
            v-if="!disabled"
            type="button"
            variant="outline"
            size="sm"
            :disabled="rows(field.key).length >= 200"
            @click="
              update(field.key, [
                ...rows(field.key),
                emptyReportData(field.fields!),
              ])
            "
            ><Plus />Add {{ field.label }}</Button
          >
          <p
            v-else-if="!rows(field.key).length"
            class="text-muted-foreground text-sm"
          >
            None recorded
          </p>
        </template>
        <ReportFieldsEditor
          v-else
          :fields="field.fields"
          :model-value="(modelValue[field.key] as ReportData) ?? {}"
          :disabled="disabled"
          @update:model-value="update(field.key, $event)"
        />
      </fieldset>
      <label
        v-else-if="field.type === 'boolean'"
        class="flex items-center gap-2 text-sm sm:col-span-2"
        ><input
          type="checkbox"
          :aria-label="field.label"
          :checked="!!modelValue[field.key]"
          :disabled="disabled"
          @change="
            update(field.key, ($event.target as HTMLInputElement).checked)
          "
        />{{ field.label }}</label
      >
      <label
        v-else
        class="grid min-w-0 gap-1.5 text-sm"
        :class="{
          'sm:col-span-2': field.type === 'text' || field.type === 'lines',
        }"
      >
        {{ field.label }}
        <NativeSelect
          v-if="field.options"
          :aria-label="field.label"
          :disabled="disabled"
          :model-value="String(modelValue[field.key] ?? '')"
          @update:model-value="update(field.key, $event)"
          ><option value="">Not recorded</option>
          <option v-for="option in field.options" :key="option" :value="option">
            {{ option.replaceAll("_", " ") }}
          </option></NativeSelect
        >
        <textarea
          v-else-if="field.type === 'text' || field.type === 'lines'"
          :aria-label="field.label"
          :readonly="disabled"
          :value="
            field.type === 'lines'
              ? ((modelValue[field.key] as string[]) ?? []).join('\n')
              : String(modelValue[field.key] ?? '')
          "
          maxlength="16000"
          rows="3"
          class="border-input bg-background min-w-0 w-full rounded-md border px-3 py-2"
          @input="input(field, $event)"
        />
        <input
          v-else
          :aria-label="field.label"
          :type="
            field.type === 'number'
              ? 'number'
              : field.type === 'date'
                ? 'datetime-local'
                : 'text'
          "
          :readonly="disabled"
          :min="field.type === 'number' ? 0 : undefined"
          :step="field.type === 'number' ? 'any' : undefined"
          :value="
            field.type === 'date'
              ? dateInput(modelValue[field.key])
              : (modelValue[field.key] ?? '')
          "
          maxlength="16000"
          class="border-input bg-background h-9 min-w-0 w-full rounded-md border px-3"
          @input="input(field, $event)"
        />
      </label>
    </template>
  </div>
</template>
