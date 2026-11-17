<script setup lang="ts">
// A case's inputs (QĐ-16): name = value rows. A step writes {{name}}
// where the value goes, so the same case runs with other data later.
// {{random}} inside a value is fresh each run (test+{{random}}@example.com).
import { Plus, Trash2 } from "@lucide/vue";
import { ref, watch } from "vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const inputs = defineModel<Record<string, string>>({ required: true });
defineProps<{ id: string; disabled?: boolean }>();

type Row = { name: string; value: string };
const rows = ref<Row[]>([]);
// Rows are the source while editing; the model follows them. A model set
// from outside (another case opened) resets the rows.
let echo = false;
watch(
  inputs,
  (v) => {
    if (echo) {
      echo = false;
      return;
    }
    rows.value = Object.entries(v).map(([name, value]) => ({ name, value }));
  },
  { immediate: true },
);
function push() {
  const out: Record<string, string> = {};
  for (const r of rows.value) if (r.name.trim()) out[r.name.trim()] = r.value;
  echo = true;
  inputs.value = out;
}
function set(i: number, patch: Partial<Row>) {
  rows.value = rows.value.map((r, j) => (j === i ? { ...r, ...patch } : r));
  push();
}
function add() {
  if (rows.value.length < 30) rows.value = [...rows.value, { name: "", value: "" }];
}
function remove(i: number) {
  rows.value = rows.value.filter((_, j) => j !== i);
  push();
}
const valid = (name: string) => !name || /^[A-Za-z_][A-Za-z0-9_]{0,39}$/.test(name);
</script>
<template>
  <div class="inputs-editor space-y-2">
    <p v-if="!rows.length" class="text-muted-foreground text-xs">
      No inputs. Add one to use it in a step as <code v-pre>{{name}}</code> —
      e.g. <code>recipient</code> = <code>bob</code> for an address like
      <code v-pre>/message?to={{recipient}}</code>.
    </p>
    <div
      v-for="(r, i) in rows"
      :key="i"
      class="inputs-editor-row grid grid-cols-[minmax(0,10rem)_minmax(0,1fr)_auto] items-center gap-2"
    >
      <Input
        :model-value="r.name"
        :disabled="disabled"
        maxlength="40"
        placeholder="name"
        :aria-label="`Input ${i + 1}: name`"
        :aria-invalid="!valid(r.name)"
        @update:model-value="set(i, { name: String($event) })"
      />
      <Input
        :model-value="r.value"
        :disabled="disabled"
        maxlength="2000"
        placeholder="value"
        :aria-label="`Input ${i + 1}: value`"
        @update:model-value="set(i, { value: String($event) })"
      />
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        :disabled="disabled"
        :aria-label="`Remove input ${i + 1}`"
        @click="remove(i)"
        ><Trash2
      /></Button>
    </div>
    <p
      v-if="rows.some((r) => !valid(r.name))"
      class="text-danger text-xs"
      role="alert"
    >
      A name is letters, digits and _, not starting with a digit.
    </p>
    <Button
      type="button"
      variant="outline"
      size="sm"
      :disabled="disabled || rows.length >= 30"
      @click="add"
      ><Plus />Add input</Button
    >
  </div>
</template>
