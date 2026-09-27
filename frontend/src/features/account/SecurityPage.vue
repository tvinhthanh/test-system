<script setup lang="ts">
import {
  Check,
  Copy,
  Download,
  KeyRound,
  ShieldAlert,
  ShieldCheck,
  Smartphone,
} from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { MfaSetup, MfaStatus } from "./mfaRepository";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import AsyncState from "../../shared/AsyncState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const { mfa, session } = useServices();
const status = useResource<MfaStatus>();
function refresh() {
  return status.load((signal) => mfa.status(signal));
}
onMounted(refresh);

const mutation = useMutation();
const setup = ref<MfaSetup | null>(null);
const code = ref("");
// Shown once, right after turning MFA on; gone when the page is left.
const recovery = ref<string[] | null>(null);
const copied = ref(false);

// The key in groups of four, the way authenticator apps show it.
const grouped = computed(
  () => setup.value?.secret.match(/.{1,4}/g)?.join(" ") ?? "",
);
const blocked = computed(
  () =>
    !!session.user?.mfa?.required &&
    !session.user?.mfa?.enabled &&
    !recovery.value,
);

async function begin() {
  setup.value = (await mutation.run("setup", (s) => mfa.setup(s))) ?? null;
  code.value = "";
}
async function enable() {
  const codes = await mutation.run("enable", (s) =>
    mfa.enable(code.value.trim(), s),
  );
  code.value = "";
  if (!codes) return;
  recovery.value = codes;
  setup.value = null;
  // The session's flags open the rest of the app for an owner.
  await session.refresh();
  await refresh();
}
async function disable() {
  const done = await mutation.run("disable", async (s) => {
    await mfa.disable(code.value.trim(), s);
    return true;
  });
  code.value = "";
  if (!done) return;
  await session.refresh();
  await refresh();
}
async function copyCodes() {
  if (!recovery.value) return;
  try {
    await navigator.clipboard.writeText(recovery.value.join("\n"));
    copied.value = true;
  } catch {
    copied.value = false;
  }
}
function downloadCodes() {
  if (!recovery.value) return;
  const text = `ATP recovery codes for ${session.user?.email ?? ""}\nEach code works once.\n\n${recovery.value.join("\n")}\n`;
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = "atp-recovery-codes.txt";
  a.click();
  URL.revokeObjectURL(url);
}
</script>
<template>
  <PageHeader
    eyebrow="Account"
    title="Security"
    description="Two-factor authentication asks for a code from your phone after your password, so a leaked password alone does not open your account."
  />

  <p
    v-if="blocked"
    class="mfa-required bg-warning-soft text-warning border-warning/30 mb-5 flex gap-2 rounded-lg border px-4 py-3 text-sm"
    role="status"
  >
    <ShieldAlert :size="18" class="mt-px shrink-0" aria-hidden="true" />
    <span
      >You own an organization, so two-factor authentication is required.
      Projects open once it is on.</span
    >
  </p>

  <!-- Recovery codes: the one moment they are visible. -->
  <section
    v-if="recovery"
    class="recovery-codes bg-card mb-5 space-y-4 rounded-lg border p-5"
    aria-labelledby="recovery-title"
  >
    <div class="flex items-start gap-3">
      <ShieldCheck
        :size="20"
        class="text-success mt-0.5 shrink-0"
        aria-hidden="true"
      />
      <div class="space-y-1">
        <h2 id="recovery-title" class="font-semibold">
          Two-factor authentication is on. Save your recovery codes.
        </h2>
        <p class="text-muted-foreground text-sm">
          If you lose your phone, each code signs you in once. This is the only
          time they are shown. Your other sessions were signed out.
        </p>
      </div>
    </div>
    <ol
      class="bg-muted grid grid-cols-2 gap-x-6 gap-y-1.5 rounded-md p-4 font-mono text-sm sm:grid-cols-5"
    >
      <li v-for="c in recovery" :key="c">{{ c }}</li>
    </ol>
    <div class="flex flex-wrap gap-2">
      <Button size="sm" variant="outline" @click="copyCodes"
        ><component :is="copied ? Check : Copy" />{{
          copied ? "Copied" : "Copy"
        }}</Button
      >
      <Button size="sm" variant="outline" @click="downloadCodes"
        ><Download />Download .txt</Button
      >
      <Button size="sm" @click="recovery = null">I have saved them</Button>
    </div>
  </section>

  <AsyncState
    :loading="status.loading.value"
    :error="status.error.value"
    @retry="refresh"
  >
    <section
      v-if="status.data.value"
      class="mfa-panel bg-card space-y-5 rounded-lg border p-5"
      aria-labelledby="mfa-title"
    >
      <div class="flex flex-wrap items-center gap-2">
        <Smartphone
          :size="18"
          class="text-muted-foreground"
          aria-hidden="true"
        />
        <h2 id="mfa-title" class="font-semibold">Authenticator app</h2>
        <Badge
          :class="
            status.data.value.enabled
              ? 'bg-success-soft text-success border-success/30'
              : 'bg-muted text-muted-foreground'
          "
          variant="outline"
          >{{ status.data.value.enabled ? "On" : "Off" }}</Badge
        >
        <Badge v-if="status.data.value.required" variant="outline"
          >Required for organization owners</Badge
        >
      </div>

      <!-- On: recovery codes left, and the way out (not for owners). -->
      <template v-if="status.data.value.enabled">
        <p class="text-muted-foreground text-sm">
          <KeyRound :size="14" class="mr-1 inline" aria-hidden="true" />
          {{ status.data.value.recoveryCodesLeft }} of 10 recovery codes left.
        </p>
        <form
          v-if="!status.data.value.required"
          class="grid gap-3 border-t pt-4 sm:grid-cols-[14rem_auto] sm:items-end"
          @submit.prevent="disable"
        >
          <div class="space-y-2">
            <Label for="mfa-off-code">Code or recovery code</Label>
            <Input
              id="mfa-off-code"
              v-model="code"
              autocomplete="one-time-code"
              required
              maxlength="20"
              class="font-mono"
            />
          </div>
          <Button
            type="submit"
            size="sm"
            class="justify-self-start"
            variant="outline"
            :disabled="mutation.busy.value !== null"
            >Turn off</Button
          >
          <ErrorNote
            v-if="mutation.error.value?.key === 'disable'"
            class="sm:col-span-2"
            :error="mutation.error.value.error"
          />
        </form>
      </template>

      <!-- Off: set up, then confirm with the first code. -->
      <template v-else>
        <div v-if="!setup" class="space-y-3">
          <p class="text-muted-foreground text-sm">
            Use any authenticator app — Google Authenticator, 1Password, Authy,
            Microsoft Authenticator.
          </p>
          <Button
            size="sm"
            :disabled="mutation.busy.value !== null"
            @click="begin"
            ><ShieldCheck />Set up two-factor authentication</Button
          >
          <ErrorNote
            v-if="mutation.error.value?.key === 'setup'"
            :error="mutation.error.value.error"
          />
        </div>
        <ol v-else class="mfa-setup space-y-5 text-sm">
          <li class="space-y-2">
            <p class="font-medium">1. Add ATP to your authenticator app</p>
            <p class="text-muted-foreground">
              Choose “enter a setup key” and type this key, or on your phone
              <a class="text-primary underline" :href="setup.otpauthUri"
                >open it in the app</a
              >.
            </p>
            <code
              class="mfa-secret bg-muted block rounded-md px-3 py-2 font-mono text-base tracking-wider break-words select-all"
              >{{ grouped }}</code
            >
          </li>
          <li>
            <form
              class="grid gap-3 sm:grid-cols-[14rem_auto] sm:items-end"
              @submit.prevent="enable"
            >
              <div class="space-y-2">
                <Label for="mfa-on-code"
                  >2. Enter the 6-digit code it shows</Label
                >
                <Input
                  id="mfa-on-code"
                  v-model="code"
                  inputmode="numeric"
                  autocomplete="one-time-code"
                  required
                  maxlength="7"
                  class="font-mono tracking-widest"
                />
              </div>
              <Button
                type="submit"
                size="sm"
                class="justify-self-start"
                :disabled="mutation.busy.value !== null"
                >Turn on</Button
              >
              <ErrorNote
                v-if="mutation.error.value?.key === 'enable'"
                class="sm:col-span-2"
                :error="mutation.error.value.error"
              />
            </form>
          </li>
        </ol>
      </template>
    </section>
  </AsyncState>
</template>
