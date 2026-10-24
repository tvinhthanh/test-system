<script setup lang="ts">
// An organization's AI settings (QĐ-15): the model drafting test cases
// uses — always chosen here — and whose Anthropic API key pays: the
// server's (the operator's LLM_API_KEY, when set) or the organization's
// own. An own key is checked with Anthropic before it is saved, stored
// sealed, and never shown again — only its last four characters.
// Organization admins and owners.
import { KeyRound, Server, Sparkles, Trash2 } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { AiSettings } from "../testmgmt/testmgmtRepository";
import { useMutation } from "../testmgmt/useMutation";
import { time } from "../../shared/format";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { explain } from "../testmgmt/explain";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import PageHeader from "../../shared/PageHeader.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute();
const { testmgmt } = useServices();
const org = computed(() => String(route.params.organization));
const settings = useResource<AiSettings>();
const refresh = () =>
  settings.load((signal) => testmgmt.aiSettings(org.value, signal));
watch(org, refresh, { immediate: true });

const keySource = ref<"organization" | "server">("organization");
const apiKey = ref("");
const model = ref("");
watch(
  () => settings.data.value,
  (s) => {
    if (!s) return;
    model.value = s.organization?.model ?? s.defaultModel;
    keySource.value =
      s.organization?.keySource ??
      (s.server.available ? "server" : "organization");
  },
);
const ownKeyStored = computed(
  () => settings.data.value?.organization?.keySource === "organization",
);
const mutation = useMutation();
const saved = ref(false);
async function save() {
  saved.value = false;
  const out = await mutation.run("save", (signal) =>
    testmgmt.saveAiSettings(
      org.value,
      {
        model: model.value,
        keySource: keySource.value,
        apiKey: apiKey.value.trim(),
      },
      signal,
    ),
  );
  if (out) {
    settings.data.value = out;
    apiKey.value = "";
    saved.value = true;
  }
}
const confirming = ref(false);
async function reset() {
  if (!confirming.value) {
    confirming.value = true;
    return;
  }
  confirming.value = false;
  const out = await mutation.run("remove", (signal) =>
    testmgmt.deleteAiSettings(org.value, signal),
  );
  if (out) settings.data.value = out;
}
const modelLabel = (id: string) =>
  settings.data.value?.models.find((m) => m.id === id)?.label ?? id;
const choice = (on: boolean) =>
  [
    "flex cursor-pointer items-start gap-3 rounded-md border p-3",
    on ? "border-primary bg-accent/40" : "",
  ].join(" ");
</script>
<template>
  <BackLink :to="`/team/organizations/${encodeURIComponent(org)}`"
    >Organization</BackLink
  >
  <PageHeader
    eyebrow="Organization"
    title="AI settings"
    description="Which model drafts test cases, and whose Anthropic API key pays for it."
  />
  <AsyncState
    :loading="settings.loading.value"
    :error="settings.error.value"
    @retry="refresh"
  >
    <div v-if="settings.data.value" class="max-w-3xl space-y-6">
      <Alert role="status">
        <Sparkles />
        <AlertTitle>
          <template v-if="settings.data.value.effective.available"
            >Drafting uses
            {{ modelLabel(settings.data.value.effective.model) }} with
            {{
              settings.data.value.effective.source === "organization"
                ? "this organization's key"
                : "the server's key"
            }}</template
          >
          <template v-else>AI drafting is off for this organization</template>
        </AlertTitle>
        <AlertDescription>
          <template v-if="settings.data.value.organization">
            <template v-if="ownKeyStored"
              >Own key ending in
              <code>{{ settings.data.value.organization.keyHint }}</code
              >.
            </template>
            Saved by {{ settings.data.value.organization.updatedBy }}
            {{ time(settings.data.value.organization.updatedAt) }}.
          </template>
          <template v-else-if="!settings.data.value.server.available">
            Add an Anthropic API key below to turn it on.
          </template>
          <template v-else>
            Nothing chosen yet: the server's key with the default model.
          </template>
        </AlertDescription>
      </Alert>

      <form
        class="ai-settings bg-card space-y-6 rounded-lg border p-5"
        @submit.prevent="save"
      >
        <fieldset class="space-y-2">
          <legend class="text-sm font-medium">Model</legend>
          <label
            v-for="m in settings.data.value.models"
            :key="m.id"
            :class="choice(model === m.id)"
          >
            <input
              v-model="model"
              type="radio"
              name="ai-model"
              :value="m.id"
              class="mt-1 size-4"
            />
            <span class="text-sm">
              <strong class="font-medium">{{ m.label }}</strong>
              <code class="text-muted-foreground ml-2 text-xs">{{ m.id }}</code>
              <span class="text-muted-foreground block text-xs">{{
                m.note
              }}</span>
            </span>
          </label>
        </fieldset>

        <fieldset class="space-y-2">
          <legend class="text-sm font-medium">Whose API key pays</legend>
          <label
            :class="[
              choice(keySource === 'server'),
              settings.data.value.server.available
                ? ''
                : 'cursor-not-allowed opacity-60',
            ]"
          >
            <input
              v-model="keySource"
              type="radio"
              name="ai-key-source"
              value="server"
              class="mt-1 size-4"
              :disabled="!settings.data.value.server.available"
            />
            <span class="text-sm">
              <strong class="inline-flex items-center gap-1.5 font-medium"
                ><Server class="size-3.5" />The server's key</strong
              >
              <span class="text-muted-foreground block text-xs">
                <template v-if="settings.data.value.server.available"
                  >The key the operator set on this server
                  (<code>LLM_API_KEY</code>); calls are billed to their
                  account.</template
                >
                <template v-else
                  >Not available: the operator has not set
                  <code>LLM_API_KEY</code> on this server.</template
                >
              </span>
            </span>
          </label>
          <label :class="choice(keySource === 'organization')">
            <input
              v-model="keySource"
              type="radio"
              name="ai-key-source"
              value="organization"
              class="mt-1 size-4"
            />
            <span class="min-w-0 flex-1 space-y-2 text-sm">
              <strong class="inline-flex items-center gap-1.5 font-medium"
                ><KeyRound class="size-3.5" />Our own key</strong
              >
              <span class="text-muted-foreground block text-xs"
                >Billed to your Anthropic account. Create one at
                console.anthropic.com → API keys.</span
              >
              <template v-if="keySource === 'organization'">
                <Label for="ai-key" class="sr-only">Anthropic API key</Label>
                <Input
                  id="ai-key"
                  v-model="apiKey"
                  type="password"
                  autocomplete="off"
                  spellcheck="false"
                  maxlength="500"
                  :placeholder="
                    ownKeyStored
                      ? `Saved key ends in ${settings.data.value.organization?.keyHint} — leave empty to keep it`
                      : 'sk-ant-…'
                  "
                  :required="!ownKeyStored"
                />
              </template>
            </span>
          </label>
          <p class="text-muted-foreground text-xs">
            The key that will pay is checked with Anthropic for the chosen model
            before anything is saved. An own key is stored encrypted and never
            shown again; choosing the server's key forgets it.
          </p>
        </fieldset>

        <div class="flex flex-wrap items-center gap-2">
          <Button type="submit" :disabled="mutation.busy.value !== null"
            ><Sparkles />{{
              mutation.busy.value === "save" ? "Checking…" : "Save"
            }}</Button
          >
          <Button
            v-if="settings.data.value.organization"
            type="button"
            :variant="confirming ? 'destructive' : 'ghost'"
            :disabled="mutation.busy.value !== null"
            @click="reset"
            ><Trash2 />{{
              confirming ? "Confirm: forget the settings" : "Reset"
            }}</Button
          >
          <span v-if="saved" class="text-success text-sm" role="status"
            >Saved.</span
          >
        </div>
        <ErrorNote
          v-if="mutation.error.value"
          :error="explain(mutation.error.value.error)"
        />
      </form>

      <section class="text-muted-foreground space-y-2 text-sm">
        <h2 class="text-foreground font-semibold">What is sent to Anthropic</h2>
        <p>
          When a tester asks for drafts: the kind of testing, what they wrote,
          and — if URL checks are on — the site's page structure (title,
          headings, links, form fields, buttons, the start of the text). Nothing
          else from ATP. Calls are billed to the account of the key in use.
        </p>
      </section>
    </div>
  </AsyncState>
</template>
