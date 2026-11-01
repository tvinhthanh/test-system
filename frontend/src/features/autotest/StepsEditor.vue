<script setup lang="ts">
// The steps of a runnable case as a form (QĐ-16): one row per step — what
// to do, which element (by role and name, label, placeholder, text, test id
// or CSS), the value — with the words a person reads. No code: the server
// checks each step and the checker runs it with Playwright's own locators.
// {{name}} in a name or value is one of the case's inputs.
import { ArrowDown, ArrowUp, Plus, Trash2 } from "@lucide/vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import {
  blankStep,
  commonRoles,
  describeStep,
  mayHaveElement,
  needsElement,
  stepActions,
  takesValue,
  type AutoStep,
  type Locator,
  type StepAction,
} from "./steps";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const steps = defineModel<AutoStep[]>({ required: true });
const props = withDefaults(
  defineProps<{ id: string; disabled?: boolean; max?: number }>(),
  { disabled: false, max: 40 },
);

const actionLabel: Record<StepAction, string> = {
  goto: "Open a page",
  click: "Click",
  fill: "Type into",
  select: "Choose in",
  check: "Tick",
  uncheck: "Untick",
  press: "Press a key",
  hover: "Point at",
  wait: "Wait (ms)",
  expect_visible: "Check: is shown",
  expect_hidden: "Check: is gone",
  expect_text: "Check: text is there",
  expect_url: "Check: address contains",
  expect_value: "Check: field holds",
  expect_title: "Check: title contains",
};
const locatorLabel: Record<Exclude<Locator, "">, string> = {
  role: "Role and name",
  label: "Field label",
  placeholder: "Placeholder",
  text: "Visible text",
  testid: "Test id",
  css: "CSS selector",
};
const valueHint: Partial<Record<StepAction, string>> = {
  goto: "/inbox",
  fill: "{{message}}",
  select: "Option label",
  press: "Enter",
  wait: "1000",
  expect_text: "Text that should be there",
  expect_url: "/done",
  expect_value: "Expected value",
  expect_title: "Title text",
};

function update(i: number, patch: Partial<AutoStep>) {
  const next = steps.value.slice();
  let s: AutoStep = { ...next[i]!, ...patch };
  // Keep the fields the action takes; the server refuses the rest.
  if (patch.do) {
    if (needsElement.has(s.do) && !s.by) s = { ...s, by: "role", role: "button" };
    if (!needsElement.has(s.do) && !mayHaveElement.has(s.do))
      s = { ...s, by: "", role: "", name: "" };
    if (!takesValue.has(s.do)) s = { ...s, value: "" };
  }
  if (patch.by !== undefined) {
    if (s.by !== "role") s = { ...s, role: "" };
    else if (!s.role) s = { ...s, role: "button" };
    if (!s.by) s = { ...s, name: "" };
  }
  next[i] = s;
  steps.value = next;
}
function add() {
  if (steps.value.length < props.max) steps.value = [...steps.value, blankStep()];
}
function remove(i: number) {
  steps.value = steps.value.filter((_, j) => j !== i);
}
function move(i: number, by: -1 | 1) {
  const j = i + by;
  if (j < 0 || j >= steps.value.length) return;
  const next = steps.value.slice();
  [next[i], next[j]] = [next[j]!, next[i]!];
  steps.value = next;
}
const isCheck = (a: StepAction) => a.startsWith("expect_");
</script>
<template>
  <div class="steps-editor space-y-2">
    <ol class="space-y-2">
      <li
        v-for="(s, i) in steps"
        :key="i"
        class="steps-editor-row bg-card rounded-md border p-3"
        :class="isCheck(s.do) ? 'border-l-primary/60 border-l-4' : ''"
      >
        <div class="flex flex-wrap items-start gap-2">
          <span class="text-muted-foreground w-6 pt-2 text-xs tabular-nums"
            >{{ i + 1 }}.</span
          >
          <div class="grid min-w-0 flex-1 gap-2 sm:grid-cols-[12rem_10rem_minmax(0,1fr)]">
            <NativeSelect
              :id="`${id}-do-${i}`"
              :model-value="s.do"
              :disabled="disabled"
              :aria-label="`Step ${i + 1}: action`"
              @update:model-value="update(i, { do: $event as StepAction })"
            >
              <option v-for="a in stepActions" :key="a" :value="a">
                {{ actionLabel[a] }}
              </option>
            </NativeSelect>
            <template v-if="needsElement.has(s.do) || mayHaveElement.has(s.do)">
              <NativeSelect
                :model-value="s.by"
                :disabled="disabled"
                :aria-label="`Step ${i + 1}: find the element by`"
                @update:model-value="update(i, { by: $event as Locator })"
              >
                <option v-if="mayHaveElement.has(s.do)" value="">
                  {{ s.do === "press" ? "The page" : "Anywhere on the page" }}
                </option>
                <option
                  v-for="(label, key) in locatorLabel"
                  :key="key"
                  :value="key"
                >
                  {{ label }}
                </option>
              </NativeSelect>
              <div v-if="s.by" class="flex min-w-0 gap-2">
                <NativeSelect
                  v-if="s.by === 'role'"
                  class="w-32 shrink-0"
                  :model-value="s.role"
                  :disabled="disabled"
                  :aria-label="`Step ${i + 1}: role`"
                  @update:model-value="update(i, { role: $event })"
                >
                  <option
                    v-if="s.role && !(commonRoles as readonly string[]).includes(s.role)"
                    :value="s.role"
                  >
                    {{ s.role }}
                  </option>
                  <option v-for="r in commonRoles" :key="r" :value="r">
                    {{ r }}
                  </option>
                </NativeSelect>
                <Input
                  :model-value="s.name"
                  :disabled="disabled"
                  maxlength="2000"
                  :placeholder="s.by === 'role' ? 'Name, e.g. Send' : locatorLabel[s.by as Exclude<Locator, ''>]"
                  :aria-label="`Step ${i + 1}: ${s.by === 'role' ? 'name' : locatorLabel[s.by as Exclude<Locator, ''>]}`"
                  @update:model-value="update(i, { name: String($event) })"
                />
              </div>
              <span v-else class="hidden sm:block" />
            </template>
            <span v-else class="hidden sm:block" />
          </div>
          <div class="flex shrink-0 gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :disabled="disabled || i === 0"
              :aria-label="`Move step ${i + 1} up`"
              @click="move(i, -1)"
              ><ArrowUp
            /></Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :disabled="disabled || i === steps.length - 1"
              :aria-label="`Move step ${i + 1} down`"
              @click="move(i, 1)"
              ><ArrowDown
            /></Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :disabled="disabled || steps.length === 1"
              :aria-label="`Remove step ${i + 1}`"
              @click="remove(i)"
              ><Trash2
            /></Button>
          </div>
        </div>
        <div class="mt-2 grid gap-2 pl-8 sm:grid-cols-3">
          <Input
            v-if="takesValue.has(s.do)"
            :model-value="s.value"
            :disabled="disabled"
            maxlength="2000"
            :placeholder="valueHint[s.do] ?? 'Value'"
            :aria-label="`Step ${i + 1}: value`"
            @update:model-value="update(i, { value: String($event) })"
          />
          <Input
            :model-value="s.say"
            :disabled="disabled"
            maxlength="500"
            :placeholder="describeStep(s)"
            :aria-label="`Step ${i + 1}: in words`"
            @update:model-value="update(i, { say: String($event) })"
          />
          <Input
            :model-value="s.expected"
            :disabled="disabled"
            maxlength="1000"
            placeholder="What should be seen (optional)"
            :aria-label="`Step ${i + 1}: expected`"
            @update:model-value="update(i, { expected: String($event) })"
          />
        </div>
      </li>
    </ol>
    <Button
      type="button"
      variant="outline"
      size="sm"
      :disabled="disabled || steps.length >= max"
      @click="add"
      ><Plus />Add step</Button
    >
  </div>
</template>
