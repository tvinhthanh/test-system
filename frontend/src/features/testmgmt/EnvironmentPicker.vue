<script setup lang="ts">
// Which environment — and so which site — a plan is tested on (QĐ-14). Picks
// one of the project's environments, adds a new one with its address, or
// fixes an address. Lives inside other forms (new plan), so it has no
// <form> of its own: its buttons are type="button" and Enter in its fields
// runs its own action instead of submitting the outer form.
import { ExternalLink, Pencil, Plus } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Environment } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import ErrorNote from "./ErrorNote.vue";
import { explain } from "./explain";
import NativeSelect from "../../shared/NativeSelect.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const props = defineProps<{
  project: string;
  modelValue: string;
  canWrite: boolean;
  disabled?: boolean;
  id?: string;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const { testmgmt } = useServices();
const envs = useResource<Environment[]>();
const refresh = () =>
  envs.load((signal) => testmgmt.environments(props.project, signal));
watch(() => props.project, refresh, { immediate: true });

const NEW = "__new";
const choice = ref(props.modelValue);
watch(
  () => props.modelValue,
  (v) => (choice.value = v),
);
const selected = computed(
  () => envs.data.value?.find((e) => e.id === choice.value) ?? null,
);
function pick(value: string) {
  choice.value = value;
  editing.value = false;
  if (value !== NEW) emit("update:modelValue", value);
}
const host = (url: string) => {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
};

const key = ref("");
const address = ref("");
const editing = ref(false);
const mutation = useMutation();
async function add() {
  const created = await mutation.run("add", (signal) =>
    testmgmt.createEnvironment(
      props.project,
      { key: key.value.trim(), baseUrl: address.value.trim() },
      signal,
    ),
  );
  if (!created) return;
  key.value = address.value = "";
  await refresh();
  pick(created.id);
}
function edit() {
  address.value = selected.value?.baseUrl ?? "";
  editing.value = true;
}
async function saveAddress() {
  const env = selected.value;
  if (!env) return;
  const saved = await mutation.run("edit", (signal) =>
    testmgmt.setEnvironmentUrl(
      props.project,
      env.id,
      address.value.trim(),
      signal,
    ),
  );
  if (!saved) return;
  editing.value = false;
  await refresh();
}
</script>
<template>
  <div class="environment-picker space-y-2">
    <Label :for="id ?? 'plan-environment'">Environment (site under test)</Label>
    <NativeSelect
      :id="id ?? 'plan-environment'"
      :model-value="choice"
      class="w-full"
      :disabled="disabled"
      @update:model-value="pick"
    >
      <option value="">None</option>
      <option v-for="e in envs.data.value ?? []" :key="e.id" :value="e.id">
        {{ e.key }} — {{ e.baseUrl ? host(e.baseUrl) : "no address" }}
      </option>
      <option v-if="canWrite" :value="NEW">+ New environment…</option>
    </NativeSelect>
    <ErrorNote v-if="envs.error.value" :error="envs.error.value" />

    <div
      v-if="choice === NEW && canWrite"
      class="bg-muted/40 grid gap-3 rounded-md border p-3 sm:grid-cols-[10rem_minmax(0,1fr)_auto] sm:items-end"
    >
      <div class="space-y-1.5">
        <Label for="env-key">Key</Label>
        <Input
          id="env-key"
          v-model="key"
          maxlength="64"
          placeholder="staging"
          @keydown.enter.prevent="add"
        />
      </div>
      <div class="space-y-1.5">
        <Label for="env-url">Address</Label>
        <Input
          id="env-url"
          v-model="address"
          type="url"
          maxlength="2048"
          placeholder="https://staging.example.com"
          @keydown.enter.prevent="add"
        />
      </div>
      <Button
        type="button"
        size="sm"
        :disabled="!key.trim() || mutation.busy.value !== null"
        @click="add"
        ><Plus />Add</Button
      >
      <p class="text-muted-foreground text-xs sm:col-span-3">
        Key: lower-case letters, digits and hyphens. Reporters that send the
        same key (<code>ATP_ENVIRONMENT</code>) report into this environment.
      </p>
      <ErrorNote
        v-if="mutation.error.value?.key === 'add'"
        class="sm:col-span-3"
        :error="explain(mutation.error.value.error)"
      />
    </div>

    <div
      v-else-if="selected"
      class="text-muted-foreground flex flex-wrap items-center gap-2 text-sm"
    >
      <template v-if="!editing">
        <a
          v-if="selected.baseUrl"
          :href="selected.baseUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="text-primary inline-flex items-center gap-1 underline"
          >{{ selected.baseUrl }}<ExternalLink :size="13" aria-hidden="true"
        /></a>
        <span v-else>No address recorded.</span>
        <Button
          v-if="canWrite && !disabled"
          type="button"
          variant="ghost"
          size="sm"
          @click="edit"
          ><Pencil />{{
            selected.baseUrl ? "Edit address" : "Add address"
          }}</Button
        >
      </template>
      <template v-else>
        <Input
          v-model="address"
          type="url"
          maxlength="2048"
          class="max-w-md"
          aria-label="Address"
          placeholder="https://staging.example.com"
          @keydown.enter.prevent="saveAddress"
        />
        <Button
          type="button"
          size="sm"
          :disabled="mutation.busy.value !== null"
          @click="saveAddress"
          >Save</Button
        >
        <Button type="button" variant="ghost" size="sm" @click="editing = false"
          >Cancel</Button
        >
      </template>
      <ErrorNote
        v-if="mutation.error.value?.key === 'edit'"
        class="basis-full"
        :error="explain(mutation.error.value.error)"
      />
    </div>
  </div>
</template>
