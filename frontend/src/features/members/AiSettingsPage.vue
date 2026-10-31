<script setup lang="ts">
// An organization's AI settings (QĐ-15, QĐ-18): the provider and model that
// draft test cases — always chosen here — and whose API key pays: the
// server's (the operator's LLM_API_KEY, for its LLM_PROVIDER only) or the
// organization's own, for any provider the server lists. A pasted key whose
// beginning is one provider's alone picks that provider. The key is checked
// with the provider before it is saved, stored sealed, and never shown
// again — only its last four characters. Organization admins and owners.
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
import NativeSelect from "../../shared/NativeSelect.vue";
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

const providerId = ref("");
const keySource = ref<"organization" | "server">("organization");
const apiKey = ref("");
const model = ref("");
const detected = ref("");
const providers = computed(() => settings.data.value?.providers ?? []);
const provider = computed(
  () =>
    providers.value.find((p) => p.id === providerId.value) ??
    providers.value[0],
);
const providerLabel = (id: string) =>
  providers.value.find((p) => p.id === id)?.label ?? id;
watch(
  () => settings.data.value,
  (s) => {
    if (!s) return;
    providerId.value = s.organization?.provider ?? s.server.provider;
    model.value = s.organization?.model ?? s.server.model;
    keySource.value =
      s.organization?.keySource ??
      (s.server.available ? "server" : "organization");
  },
);
// The server's key pays only for the server's own provider.
const serverFits = computed(
  () =>
    !!settings.data.value?.server.available &&
    settings.data.value.server.provider === providerId.value,
);
const ownKeyStored = computed(
  () => settings.data.value?.organization?.keySource === "organization",
);
// A saved key is kept only while the provider stays the one it is for.
const keyKept = computed(
  () =>
    ownKeyStored.value &&
    settings.data.value?.organization?.provider === providerId.value,
);
// The model list's own pick shows there; the field below holds an id of
// one's own.
const suggested = computed(
  () => !!provider.value?.models.some((m) => m.id === model.value),
);
function typeModel(value: string | number) {
  model.value = String(value);
}
function choose(id: string) {
  providerId.value = id;
  const saved = settings.data.value?.organization;
  model.value =
    saved?.provider === id ? saved.model : (provider.value?.defaultModel ?? "");
  if (!serverFits.value) keySource.value = "organization";
  detected.value = "";
  mutation.error.value = null; // it was about the provider left behind
}
watch(apiKey, (typed) => {
  const key = typed.trim();
  const owner = providers.value.find((p) =>
    p.keyPrefixes.some((prefix) => key.startsWith(prefix)),
  );
  detected.value = "";
  if (owner && owner.id !== providerId.value && providerId.value !== "openai-compatible") {
    choose(owner.id);
    keySource.value = "organization";
    detected.value = owner.label;
  }
});
const consoleHost = computed(() => {
  try {
    return new URL(provider.value?.console ?? "").host;
  } catch {
    return "";
  }
});

const mutation = useMutation();
const saved = ref(false);
async function save() {
  saved.value = false;
  const out = await mutation.run("save", (signal) =>
    testmgmt.saveAiSettings(
      org.value,
      {
        provider: providerId.value,
        model: model.value.trim(),
        keySource: keySource.value,
        apiKey: apiKey.value.trim(),
      },
      signal,
    ),
  );
  if (out) {
    settings.data.value = out;
    apiKey.value = "";
    detected.value = "";
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
const modelLabel = (providerId: string, id: string) =>
  providers.value
    .find((p) => p.id === providerId)
    ?.models.find((m) => m.id === id)?.label ?? id;
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
    description="Which AI provider and model draft test cases, and whose API key pays for it."
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
            {{
              modelLabel(
                settings.data.value.effective.provider,
                settings.data.value.effective.model,
              )
            }}
            from {{ providerLabel(settings.data.value.effective.provider) }}
            with
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
              >Own key for
              {{ providerLabel(settings.data.value.organization.provider) }}
              ending in
              <code>{{ settings.data.value.organization.keyHint }}</code
              >.
            </template>
            Saved by {{ settings.data.value.organization.updatedBy }}
            {{ time(settings.data.value.organization.updatedAt) }}.
          </template>
          <template v-else-if="!settings.data.value.server.available">
            Add an API key below to turn it on — from any provider listed.
          </template>
          <template v-else>
            Nothing chosen yet: the server's key with its default model.
          </template>
        </AlertDescription>
      </Alert>

      <form
        v-if="provider"
        class="ai-settings bg-card space-y-6 rounded-lg border p-5"
        @submit.prevent="save"
      >
        <div class="space-y-2">
          <Label for="ai-provider">Provider</Label>
          <NativeSelect
            id="ai-provider"
            :model-value="providerId"
            class="w-full sm:w-80"
            @update:model-value="choose"
          >
            <option v-for="p in providers" :key="p.id" :value="p.id">
              {{ p.label }}
            </option>
          </NativeSelect>
          <p
            v-if="detected"
            class="text-muted-foreground text-xs"
            role="status"
          >
            Switched to {{ detected }} to match the key.
          </p>
        </div>

        <fieldset class="space-y-2">
          <legend class="text-sm font-medium">Model</legend>
          <label
            v-for="m in provider.models"
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
          <div v-if="provider.anyModel" class="space-y-1 pt-1">
            <Label for="ai-model-id" class="text-xs font-normal">{{
              provider.models.length
                ? "Or type any model id this key can use"
                : "Model id"
            }}</Label>
            <Input
              id="ai-model-id"
              :model-value="suggested ? '' : model"
              autocomplete="off"
              spellcheck="false"
              maxlength="200"
              class="sm:w-80"
              :placeholder="provider.defaultModel || 'author/model'"
              :required="!suggested"
              @update:model-value="typeModel"
            />
          </div>
        </fieldset>

        <fieldset class="space-y-2">
          <legend class="text-sm font-medium">Whose API key pays</legend>
          <label
            :class="[
              choice(keySource === 'server'),
              serverFits ? '' : 'cursor-not-allowed opacity-60',
            ]"
          >
            <input
              v-model="keySource"
              type="radio"
              name="ai-key-source"
              value="server"
              class="mt-1 size-4"
              :disabled="!serverFits"
            />
            <span class="text-sm">
              <strong class="inline-flex items-center gap-1.5 font-medium"
                ><Server class="size-3.5" />The server's key</strong
              >
              <span class="text-muted-foreground block text-xs">
                <template v-if="serverFits"
                  >The key the operator set on this server
                  (<code>LLM_API_KEY</code>); calls are billed to their
                  account.</template
                >
                <template v-else-if="settings.data.value.server.available"
                  >The server's key is for
                  {{ providerLabel(settings.data.value.server.provider) }}, not
                  {{ provider.label }}.</template
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
                >Billed to your {{ provider.label }} account.<template
                  v-if="consoleHost"
                >
                  Create one at
                  <a
                    :href="provider.console"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="underline"
                    >{{ consoleHost }}</a
                  >.</template
                ></span
              >
              <template v-if="keySource === 'organization'">
                <Label for="ai-key" class="sr-only"
                  >{{ provider.label }} API key</Label
                >
                <Input
                  id="ai-key"
                  v-model="apiKey"
                  type="password"
                  autocomplete="off"
                  spellcheck="false"
                  maxlength="500"
                  :placeholder="
                    keyKept
                      ? `Saved key ends in ${settings.data.value.organization?.keyHint} — leave empty to keep it`
                      : provider.keyHint || 'API key'
                  "
                  :required="!keyKept"
                />
              </template>
            </span>
          </label>
          <p class="text-muted-foreground text-xs">
            The key that will pay is checked with {{ provider.label }} for the
            chosen model before anything is saved. An own key is stored
            encrypted and never shown again; choosing the server's key forgets
            it.
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
        <h2 class="text-foreground font-semibold">
          What is sent to
          {{
            settings.data.value.effective.available
              ? providerLabel(settings.data.value.effective.provider)
              : "the AI provider"
          }}
        </h2>
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
