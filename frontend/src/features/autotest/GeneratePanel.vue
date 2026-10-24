<script setup lang="ts">
// AI drafts runnable cases (QĐ-16): the tester names where to start (a path
// with {{inputs}}), signs in as a test account if the page needs it, gives
// the inputs and says what to cover. The server opens the page in a real
// browser, the model reads it and writes cases made of steps. Drafts are
// suggestions: the tester ticks, edits and keeps them — only then are they
// test cases. While drafting, the request is read again every 3 s.
import { Pencil, Play, Save, Sparkles, X } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { useServices } from "../../app/providers/services";
import type { AutoDraft } from "./autotestRepository";
import { useMutation } from "../testmgmt/useMutation";
import { explain } from "../testmgmt/explain";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import { testTypes } from "../testmgmt/testTypes";
import CaseEditor from "./CaseEditor.vue";
import InputsEditor from "./InputsEditor.vue";
import { describeStep, type CaseForm } from "./steps";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const props = defineProps<{
  project: string;
  environmentId: string;
  base: string;
  accounts: string[];
  model: string;
  keySource: string;
}>();
const emit = defineEmits<{ saved: [caseIds: string[], run: boolean]; close: [] }>();
const { autotest } = useServices();

const target = ref("/");
const account = ref("");
const inputs = ref<Record<string, string>>({});
const context = ref("");
const count = ref("5");
const testType = ref("functional");
const request = shallowRef<AutoDraft | null>(null);
const mutation = useMutation();

async function draft() {
  const made = await mutation.run("draft", (signal) =>
    autotest.createDraft(
      props.project,
      {
        environmentId: props.environmentId,
        target: target.value.trim(),
        account: account.value,
        inputs: inputs.value,
        context: context.value.trim(),
        count: Number(count.value),
        testType: testType.value,
      },
      signal,
    ),
  );
  if (made) request.value = made;
}
let timer: ReturnType<typeof setTimeout> | undefined;
let pollAbort: AbortController | undefined;
const forms = ref<CaseForm[]>([]);
const picked = ref(new Set<number>());
const editing = ref<number | null>(null);
watch(request, (r) => {
  clearTimeout(timer);
  if (r?.status === "done") {
    forms.value = r.drafts.map((d) => ({
      title: d.title,
      priority: d.priority,
      testType: r.testType,
      target: r.target,
      account: r.account,
      inputs: { ...r.inputs },
      steps: d.steps.map((s) => ({ ...s })),
    }));
    picked.value = new Set(r.drafts.map((_, i) => i));
  }
  if (r?.status !== "queued") return;
  timer = setTimeout(async () => {
    pollAbort = new AbortController();
    try {
      request.value = await autotest.draft(props.project, r.id, pollAbort.signal);
    } catch {
      request.value = { ...r }; // try again on the next tick
    }
  }, 3000);
});
onBeforeUnmount(() => {
  clearTimeout(timer);
  pollAbort?.abort();
});
function toggle(i: number) {
  const next = new Set(picked.value);
  if (next.has(i)) next.delete(i);
  else next.add(i);
  picked.value = next;
}
const progress = ref("");
async function keep(run: boolean) {
  const chosen = forms.value.filter((_, i) => picked.value.has(i));
  const ids = await mutation.run("keep", async (signal) => {
    const out: string[] = [];
    for (const [i, f] of chosen.entries()) {
      progress.value = `Saving case ${i + 1} of ${chosen.length}…`;
      const made = await autotest.createCase(
        props.project,
        { ...f, source: "ai" },
        signal,
      );
      out.push(made.caseId);
    }
    return out;
  });
  progress.value = "";
  if (ids) emit("saved", ids, run);
}
const why = computed(() => request.value?.drafts.map((d) => d.why) ?? []);
</script>
<template>
  <section class="generate-panel bg-card space-y-4 rounded-lg border p-4" aria-labelledby="generate-title">
    <div class="flex items-center gap-2">
      <h2 id="generate-title" class="flex items-center gap-2 text-sm font-semibold">
        <Sparkles class="size-4" />Generate cases with AI
      </h2>
      <Button class="ml-auto" size="icon-sm" variant="ghost" aria-label="Close" @click="emit('close')"
        ><X
      /></Button>
    </div>
    <form class="space-y-3" @submit.prevent="draft">
      <div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_14rem]">
        <div class="space-y-1.5">
          <Label for="gen-target">Where it starts</Label>
          <div class="flex min-w-0 items-center gap-1">
            <span class="text-muted-foreground hidden max-w-48 truncate text-sm sm:inline">{{
              base.replace(/\/$/, "")
            }}</span>
            <Input
              id="gen-target"
              v-model="target"
              required
              maxlength="2048"
              placeholder="/message?to={{recipient}}"
            />
          </div>
        </div>
        <div class="space-y-1.5">
          <Label for="gen-account">Signed in as</Label>
          <NativeSelect id="gen-account" v-model="account" class="w-full">
            <option value="">Not signed in</option>
            <option v-for="a in accounts" :key="a" :value="a">{{ a }}</option>
          </NativeSelect>
        </div>
      </div>
      <fieldset class="space-y-2">
        <legend class="text-sm font-medium">Inputs</legend>
        <InputsEditor id="gen-inputs" v-model="inputs" />
      </fieldset>
      <div class="space-y-1.5">
        <Label for="gen-context">What should the cases cover?</Label>
        <textarea
          id="gen-context"
          v-model="context"
          rows="3"
          maxlength="4000"
          class="border-input bg-background focus-visible:border-ring focus-visible:ring-ring/50 w-full rounded-md border px-3 py-2 text-sm shadow-xs outline-none focus-visible:ring-3"
          placeholder="e.g. Gửi tin nhắn: tin thường, tin dài, emoji, tin rỗng không được gửi"
        />
      </div>
      <div class="flex flex-wrap items-end gap-3">
        <div class="space-y-1.5">
          <Label for="gen-type">Kind of testing</Label>
          <NativeSelect id="gen-type" v-model="testType" class="w-48">
            <option v-for="t in testTypes" :key="t.id" :value="t.id">{{ t.label }}</option>
          </NativeSelect>
        </div>
        <div class="space-y-1.5">
          <Label for="gen-count">How many</Label>
          <NativeSelect id="gen-count" v-model="count" class="w-20">
            <option v-for="n in ['3', '5', '8', '12']" :key="n" :value="n">{{ n }}</option>
          </NativeSelect>
        </div>
        <Button
          type="submit"
          :disabled="mutation.busy.value !== null || request?.status === 'queued'"
          ><Sparkles />Draft cases</Button
        >
        <span class="text-muted-foreground text-xs"
          >Uses {{ model }} with the {{ keySource === "organization" ? "organization's" : "server's" }} key.
          The page is opened on the server{{ account ? `, signed in as ${account}` : "" }}, and its structure
          is sent with your words — never the account's password.</span
        >
      </div>
      <ErrorNote
        v-if="mutation.error.value?.key === 'draft'"
        :error="explain(mutation.error.value.error)"
      />
    </form>

    <p v-if="request?.status === 'queued'" class="text-muted-foreground text-sm" role="status">
      Opening the page and drafting — usually under a minute…
    </p>
    <p v-else-if="request?.status === 'error'" class="text-danger text-sm" role="alert">
      {{ request.error }}
    </p>
    <template v-else-if="request?.status === 'done'">
      <p v-if="!request.pageRead" class="text-warning text-sm" role="status">
        The page could not be read, so the drafts come from your description
        alone — check each element before you keep them.
      </p>
      <p v-if="!forms.length" class="text-muted-foreground text-sm">
        The model wrote no case that can run. Describe the page differently and try again.
      </p>
      <ul class="space-y-2">
        <li v-for="(f, i) in forms" :key="i" class="draft-case rounded-md border p-3">
          <div class="flex items-start gap-2">
            <input
              :id="`draft-${i}`"
              type="checkbox"
              class="mt-1"
              :checked="picked.has(i)"
              @change="toggle(i)"
            />
            <label :for="`draft-${i}`" class="min-w-0 flex-1 cursor-pointer">
              <span class="block text-sm font-medium">{{ f.title }}</span>
              <span v-if="why[i]" class="text-muted-foreground block text-xs">{{ why[i] }}</span>
            </label>
            <Badge variant="outline">{{ f.priority.toUpperCase() }}</Badge>
            <Button size="sm" variant="ghost" @click="editing = editing === i ? null : i"
              ><Pencil />{{ editing === i ? "Done" : "Edit" }}</Button
            >
          </div>
          <ol v-if="editing !== i" class="text-muted-foreground mt-2 list-decimal space-y-0.5 pl-10 text-xs">
            <li v-for="(s, j) in f.steps" :key="j">
              {{ s.say || describeStep(s) }}<template v-if="s.expected"> → {{ s.expected }}</template>
            </li>
          </ol>
          <CaseEditor
            v-else
            :id="`draft-editor-${i}`"
            :model-value="f"
            class="mt-3"
            mode="draft"
            :accounts="accounts"
            :base="base"
            save-label="Done"
            @update:model-value="forms[i] = $event"
            @save="editing = null"
            @cancel="editing = null"
          />
        </li>
      </ul>
      <div v-if="forms.length" class="flex flex-wrap items-center gap-2">
        <Button :disabled="mutation.busy.value !== null || !picked.size" @click="keep(false)"
          ><Save />Keep {{ picked.size }} case{{ picked.size === 1 ? "" : "s" }}</Button
        >
        <Button variant="outline" :disabled="mutation.busy.value !== null || !picked.size" @click="keep(true)"
          ><Play />Keep and run</Button
        >
        <span v-if="progress" class="text-muted-foreground text-xs" role="status">{{ progress }}</span>
        <span class="text-muted-foreground text-xs"
          >Drafts are suggestions — read the steps; a run checks every one.</span
        >
      </div>
      <ErrorNote
        v-if="mutation.error.value?.key === 'keep'"
        :error="explain(mutation.error.value.error)"
      />
    </template>
  </section>
</template>
