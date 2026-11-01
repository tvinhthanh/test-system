<script setup lang="ts">
// An environment's test accounts (QĐ-16): what a case signs in with. The
// password is sent once and never shown again; the sign-in steps are found
// by AI from the sign-in page (it sees {{username}} / {{password}}, never
// the values) or written here. While a search runs the list looks again
// every 3 s.
import { KeyRound, Pencil, Plus, Search, Trash2 } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { TestAccount } from "./autotestRepository";
import { useMutation } from "../testmgmt/useMutation";
import { explain } from "../testmgmt/explain";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import { time } from "../../shared/format";
import StepsEditor from "./StepsEditor.vue";
import { blankStep, type AutoStep } from "./steps";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const props = defineProps<{
  project: string;
  environmentId: string;
  canWrite: boolean;
  aiAvailable: boolean;
}>();
const emit = defineEmits<{ changed: [keys: string[]] }>();
const { autotest } = useServices();
const list = useResource<TestAccount[]>();
const load = () =>
  list.load((signal) =>
    autotest.accounts(props.project, props.environmentId, signal),
  );
watch(() => [props.project, props.environmentId], () => void load(), {
  immediate: true,
});
watch(
  () => list.data.value,
  (v) => emit("changed", (v ?? []).map((a) => a.key)),
);
let timer: ReturnType<typeof setTimeout> | undefined;
watch(
  () => list.data.value,
  (v) => {
    clearTimeout(timer);
    if (v?.some((a) => a.loginStatus === "finding"))
      timer = setTimeout(() => void load(), 3000);
  },
);
onBeforeUnmount(() => clearTimeout(timer));

const mutation = useMutation();
type Form = {
  key: string;
  username: string;
  password: string;
  loginUrl: string;
  steps: AutoStep[] | null; // null: leave the steps as they are
  editing: boolean;
};
const empty = (): Form => ({
  key: "",
  username: "",
  password: "",
  loginUrl: "",
  steps: null,
  editing: false,
});
const form = ref<Form | null>(null);
function edit(a?: TestAccount) {
  form.value = a
    ? {
        key: a.key,
        username: a.username,
        password: "",
        loginUrl: a.loginUrl,
        steps: null,
        editing: true,
      }
    : empty();
}
function editSteps(a: TestAccount) {
  edit(a);
  form.value!.steps = a.loginSteps?.length
    ? a.loginSteps.map((s) => ({ ...s }))
    : [
        { ...blankStep(), do: "fill", by: "label", role: "", name: "Email", value: "{{username}}" },
        { ...blankStep(), do: "fill", by: "label", role: "", name: "Password", value: "{{password}}" },
        { ...blankStep(), do: "click", by: "role", role: "button", name: "Sign in" },
      ];
}
async function save() {
  const f = form.value;
  if (!f) return;
  const saved = await mutation.run("save", (signal) =>
    autotest.saveAccount(
      props.project,
      props.environmentId,
      {
        key: f.key.trim(),
        username: f.username.trim(),
        password: f.password,
        loginUrl: f.loginUrl.trim(),
        loginSteps: f.steps ?? undefined,
      },
      signal,
    ),
  );
  if (saved) {
    form.value = null;
    await load();
  }
}
async function find(key: string) {
  const a = await mutation.run(`find:${key}`, (signal) =>
    autotest.findLogin(props.project, props.environmentId, key, signal),
  );
  if (a) await load();
}
const confirming = ref<string | null>(null);
async function remove(key: string) {
  if (confirming.value !== key) {
    confirming.value = key;
    return;
  }
  confirming.value = null;
  const ok = await mutation.run(`delete:${key}`, async (signal) => {
    await autotest.deleteAccount(props.project, props.environmentId, key, signal);
    return true;
  });
  if (ok) await load();
}
const statusText: Record<string, string> = {
  unknown: "Sign-in not tried yet",
  finding: "Finding sign-in steps…",
  ok: "Signs in",
  failed: "Sign-in failed",
};
const accounts = computed(() => list.data.value ?? []);
</script>
<template>
  <section class="accounts-panel bg-card space-y-4 rounded-lg border p-4" aria-labelledby="accounts-title">
    <div class="flex flex-wrap items-center gap-2">
      <h2 id="accounts-title" class="flex items-center gap-2 text-sm font-semibold">
        <KeyRound class="size-4" />Test accounts
      </h2>
      <span class="text-muted-foreground text-xs"
        >For pages that need signing in. Passwords are encrypted and never shown
        again, and never sent to AI.</span
      >
      <Button
        v-if="canWrite && !form"
        class="ml-auto"
        size="sm"
        variant="outline"
        @click="edit()"
        ><Plus />Add account</Button
      >
    </div>

    <p v-if="list.loading.value && !list.data.value" class="text-muted-foreground text-sm">
      Loading…
    </p>
    <ErrorNote v-else-if="list.error.value" :error="list.error.value" />
    <p v-else-if="!accounts.length && !form" class="text-muted-foreground text-sm">
      No test account on this environment yet.
    </p>
    <ul v-else class="divide-y">
      <li v-for="a in accounts" :key="a.key" class="flex flex-wrap items-center gap-3 py-2">
        <span class="min-w-0 flex-1">
          <strong class="block text-sm font-medium">{{ a.key }}</strong>
          <span class="text-muted-foreground block truncate text-xs"
            >{{ a.username }} · sign-in page {{ a.loginUrl || "the environment's address" }}
            <template v-if="a.loginSteps"> · {{ a.loginSteps.length }} sign-in steps</template>
            <template v-if="a.loginCheckedAt"> · checked {{ time(a.loginCheckedAt) }}</template></span
          >
          <span v-if="a.loginError" class="text-danger block text-xs">{{ a.loginError }}</span>
        </span>
        <StatusBadge :status="a.loginStatus" class="account-status" :title="statusText[a.loginStatus]" />
        <template v-if="canWrite">
          <Button
            v-if="aiAvailable"
            size="sm"
            variant="outline"
            :disabled="mutation.busy.value !== null || a.loginStatus === 'finding'"
            :title="'AI reads the sign-in page and writes the steps; a browser then proves they work'"
            @click="find(a.key)"
            ><Search />Find sign-in steps</Button
          >
          <Button size="sm" variant="ghost" @click="editSteps(a)"><Pencil />Steps</Button>
          <Button size="sm" variant="ghost" @click="edit(a)"><Pencil />Edit</Button>
          <Button
            size="sm"
            :variant="confirming === a.key ? 'destructive' : 'ghost'"
            :aria-label="confirming === a.key ? `Confirm removing ${a.key}` : `Remove ${a.key}`"
            @blur="confirming = null"
            @click="remove(a.key)"
            ><Trash2 />{{ confirming === a.key ? "Confirm" : "" }}</Button
          >
        </template>
      </li>
    </ul>
    <ErrorNote
      v-if="mutation.error.value && mutation.error.value.key !== 'save'"
      :error="explain(mutation.error.value.error)"
    />

    <form v-if="form" class="account-form space-y-3 border-t pt-4" @submit.prevent="save">
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div class="space-y-1.5">
          <Label for="account-key">Key</Label>
          <Input
            id="account-key"
            v-model="form.key"
            required
            maxlength="40"
            placeholder="default"
            :disabled="form.editing"
          />
        </div>
        <div class="space-y-1.5">
          <Label for="account-user">User name or email</Label>
          <Input id="account-user" v-model="form.username" required maxlength="254" autocomplete="off" />
        </div>
        <div class="space-y-1.5">
          <Label for="account-password">Password</Label>
          <Input
            id="account-password"
            v-model="form.password"
            type="password"
            :required="!form.editing"
            maxlength="1000"
            autocomplete="new-password"
            :placeholder="form.editing ? 'Leave empty to keep it' : ''"
          />
        </div>
        <div class="space-y-1.5">
          <Label for="account-login">Sign-in page</Label>
          <Input id="account-login" v-model="form.loginUrl" maxlength="2048" placeholder="/login" />
        </div>
      </div>
      <div v-if="form.steps" class="space-y-2">
        <p class="text-sm font-medium">Sign-in steps</p>
        <p class="text-muted-foreground text-xs">
          Use <code v-pre>{{username}}</code> and <code v-pre>{{password}}</code> where they are typed.
          The steps run on the sign-in page; signing in has worked when no
          password field is left on screen.
        </p>
        <StepsEditor id="account-steps" v-model="form.steps" :max="10" />
      </div>
      <div class="flex gap-2">
        <Button type="submit" :disabled="mutation.busy.value !== null">Save account</Button>
        <Button type="button" variant="ghost" @click="form = null">Cancel</Button>
      </div>
      <ErrorNote
        v-if="mutation.error.value?.key === 'save'"
        :error="explain(mutation.error.value.error)"
      />
    </form>
  </section>
</template>
