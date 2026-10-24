<script setup lang="ts">
// One runnable case as a form (QĐ-16): where it starts (a path on the
// environment's site, with {{inputs}}), which test account signs in, the
// inputs and the steps. A new case also has its title, priority and kind of
// testing; an existing case's title is edited on its test case page.
import { Save, X } from "@lucide/vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import { testTypes } from "../testmgmt/testTypes";
import InputsEditor from "./InputsEditor.vue";
import StepsEditor from "./StepsEditor.vue";
import type { CaseForm } from "./steps";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const form = defineModel<CaseForm>({ required: true });
defineProps<{
  id: string;
  mode: "new" | "edit" | "draft";
  accounts: string[];
  base: string; // the environment's address, shown before the path
  busy?: boolean;
  saveLabel?: string;
}>();
const emit = defineEmits<{ save: []; cancel: [] }>();
const set = <K extends keyof CaseForm>(key: K, value: CaseForm[K]) =>
  (form.value = { ...form.value, [key]: value });
</script>
<template>
  <form class="case-editor space-y-4" @submit.prevent="emit('save')">
    <div
      v-if="mode !== 'edit'"
      class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_8rem_12rem]"
    >
      <div class="space-y-1.5">
        <Label :for="`${id}-title`">Title</Label>
        <Input
          :id="`${id}-title`"
          :model-value="form.title"
          required
          maxlength="500"
          placeholder="Send a message to a contact"
          @update:model-value="set('title', String($event))"
        />
      </div>
      <div class="space-y-1.5">
        <Label :for="`${id}-priority`">Priority</Label>
        <NativeSelect
          :id="`${id}-priority`"
          :model-value="form.priority"
          class="w-full"
          @update:model-value="set('priority', $event)"
        >
          <option v-for="p in ['p0', 'p1', 'p2', 'p3']" :key="p" :value="p">
            {{ p }}
          </option>
        </NativeSelect>
      </div>
      <div class="space-y-1.5">
        <Label :for="`${id}-type`">Kind of testing</Label>
        <NativeSelect
          :id="`${id}-type`"
          :model-value="form.testType"
          class="w-full"
          @update:model-value="set('testType', $event)"
        >
          <option value="">Not set</option>
          <option v-for="t in testTypes" :key="t.id" :value="t.id">
            {{ t.label }}
          </option>
        </NativeSelect>
      </div>
    </div>
    <div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_14rem]">
      <div class="space-y-1.5">
        <Label :for="`${id}-target`">Where it starts</Label>
        <div class="flex min-w-0 items-center gap-1">
          <span
            class="text-muted-foreground hidden max-w-48 truncate text-sm sm:inline"
            :title="base"
            >{{ base.replace(/\/$/, "") }}</span
          >
          <Input
            :id="`${id}-target`"
            :model-value="form.target"
            required
            maxlength="2048"
            placeholder="/message?to={{recipient}}"
            @update:model-value="set('target', String($event))"
          />
        </div>
      </div>
      <div class="space-y-1.5">
        <Label :for="`${id}-account`">Signed in as</Label>
        <NativeSelect
          :id="`${id}-account`"
          :model-value="form.account"
          class="w-full"
          @update:model-value="set('account', $event)"
        >
          <option value="">Not signed in</option>
          <option v-if="form.account && !accounts.includes(form.account)" :value="form.account">
            {{ form.account }} (not on this environment)
          </option>
          <option v-for="a in accounts" :key="a" :value="a">{{ a }}</option>
        </NativeSelect>
      </div>
    </div>
    <fieldset class="space-y-2">
      <legend class="text-sm font-medium">Inputs</legend>
      <InputsEditor
        :id="`${id}-inputs`"
        :model-value="form.inputs"
        :disabled="busy"
        @update:model-value="set('inputs', $event)"
      />
    </fieldset>
    <fieldset class="space-y-2">
      <legend class="text-sm font-medium">Steps</legend>
      <p class="text-muted-foreground text-xs">
        The start page is open before step 1<template v-if="form.account">,
          signed in as {{ form.account }}</template
        >. End with a check (“Check: …”) that proves the result.
      </p>
      <StepsEditor
        :id="`${id}-steps`"
        :model-value="form.steps"
        :disabled="busy"
        @update:model-value="set('steps', $event)"
      />
    </fieldset>
    <div class="flex flex-wrap items-center gap-2">
      <Button type="submit" :disabled="busy || !form.steps.length"
        ><Save />{{ saveLabel ?? "Save" }}</Button
      >
      <Button type="button" variant="ghost" :disabled="busy" @click="emit('cancel')"
        ><X />Cancel</Button
      >
      <slot />
    </div>
  </form>
</template>
