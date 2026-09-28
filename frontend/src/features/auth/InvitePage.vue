<script setup lang="ts">
import { Activity, ArrowRight } from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import type { InvitationInfo } from "./sessionRepository";
import ThemeToggle from "../../app/ThemeToggle.vue";
import { time } from "../../shared/format";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Accepting an invitation (§11.2). The mailed link is /invite#<token>: the
// fragment never reaches a server. It is read once, then removed from the
// address bar and the history, and only ever sent in a request body.
const { session } = useServices();
const router = useRouter();
const token = ref("");
const info = ref<InvitationInfo | null>(null);
const state = ref<"loading" | "ready" | "invalid" | "exists" | "error">(
  "loading",
);
const message = ref("");
const password = ref(""),
  confirm = ref(""),
  busy = ref(false);

const roleWords: Record<string, string> = {
  org_owner: "organization owner",
  org_admin: "organization admin",
  workspace_admin: "workspace admin",
  maintainer: "maintainer",
  member: "member",
  viewer: "viewer",
};
const mismatch = computed(
  () => confirm.value !== "" && confirm.value !== password.value,
);

function explain(e: unknown) {
  if (e instanceof ApiError) {
    if (e.code === "INVITATION_NOT_FOUND") return "invalid";
    if (e.code === "ACCOUNT_EXISTS") return "exists";
    if (e.code === "RATE_LIMITED")
      message.value = "Too many attempts from here. Wait 15 minutes.";
    else if (e.code === "INVALID_PASSWORD")
      message.value = "Choose a password of at least 12 characters.";
    else message.value = e.message;
  } else message.value = "The invitation could not be opened.";
  return "error";
}

onMounted(async () => {
  token.value = decodeURIComponent(location.hash.replace(/^#/, ""));
  history.replaceState(history.state, "", location.pathname);
  if (!token.value) {
    state.value = "invalid";
    return;
  }
  try {
    info.value = await session.lookupInvitation(token.value);
    state.value = "ready";
  } catch (e) {
    state.value = explain(e);
  }
});

async function accept() {
  if (mismatch.value || busy.value) return;
  busy.value = true;
  message.value = "";
  try {
    if (await session.acceptInvitation(token.value, password.value)) {
      password.value = confirm.value = "";
      await router.replace("/");
    }
  } catch (e) {
    const s = explain(e);
    if (s !== "error") state.value = s;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <main class="bg-background flex min-h-svh flex-col p-6 sm:p-10">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-2">
        <span
          class="bg-primary text-primary-foreground inline-flex size-7 items-center justify-center rounded-md"
          ><Activity :size="15" aria-hidden="true"
        /></span>
        <span class="text-sm font-semibold">ATP</span>
        <span class="text-muted-foreground text-sm">Report Center</span>
      </div>
      <ThemeToggle />
    </div>

    <section class="invite-page m-auto w-full max-w-sm space-y-5 py-10">
      <p v-if="state === 'loading'" class="text-muted-foreground text-sm">
        Opening your invitation…
      </p>

      <template v-else-if="state === 'invalid'">
        <h1 class="text-2xl font-semibold tracking-tight">
          This invitation cannot be used
        </h1>
        <p class="text-muted-foreground text-sm">
          It was already accepted, withdrawn, or is older than seven days. Ask
          the person who invited you to send a new one.
        </p>
        <Button as-child variant="outline" class="w-full"
          ><RouterLink to="/sign-in">Go to sign in</RouterLink></Button
        >
      </template>

      <template v-else-if="state === 'exists'">
        <h1 class="text-2xl font-semibold tracking-tight">
          You already have an account
        </h1>
        <p class="text-muted-foreground text-sm">
          The role from this invitation was added to it. Sign in with your
          password.
        </p>
        <Button as-child class="w-full"
          ><RouterLink to="/sign-in">Sign in<ArrowRight /></RouterLink
        ></Button>
      </template>

      <form
        v-else-if="info"
        class="flex flex-col gap-5"
        @submit.prevent="accept"
      >
        <div class="space-y-1.5">
          <p
            class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
          >
            Invitation
          </p>
          <h1 class="text-2xl font-semibold tracking-tight">
            Join {{ info.scopeName }}
          </h1>
          <p class="text-muted-foreground text-sm">
            {{ info.invitedBy }} invited you as
            <strong class="text-foreground font-medium">{{
              roleWords[info.role] ?? info.role
            }}</strong
            >. Choose a password to create your account. The link expires
            {{ time(info.expiresAt) }}.
          </p>
        </div>
        <div class="space-y-2">
          <Label for="invite-email">Email</Label>
          <Input
            id="invite-email"
            :model-value="info.email"
            type="email"
            autocomplete="username"
            readonly
          />
        </div>
        <div class="space-y-2">
          <Label for="invite-password">Password</Label>
          <Input
            id="invite-password"
            v-model="password"
            type="password"
            autocomplete="new-password"
            minlength="12"
            required
          />
          <p class="text-muted-foreground text-xs">At least 12 characters.</p>
        </div>
        <div class="space-y-2">
          <Label for="invite-confirm">Password again</Label>
          <Input
            id="invite-confirm"
            v-model="confirm"
            type="password"
            autocomplete="new-password"
            required
            :aria-invalid="mismatch"
          />
          <p v-if="mismatch" class="text-danger text-xs">
            The two passwords differ.
          </p>
        </div>
        <p
          v-if="message"
          class="bg-danger-soft text-danger border-danger/20 rounded-md border px-3 py-2 text-sm"
          role="alert"
        >
          {{ message }}
        </p>
        <Button type="submit" class="w-full" :disabled="busy || mismatch">
          {{ busy ? "Creating your account…" : "Create account and join"
          }}<ArrowRight />
        </Button>
      </form>

      <p
        v-else
        class="bg-danger-soft text-danger border-danger/20 rounded-md border px-3 py-2 text-sm"
        role="alert"
      >
        {{ message }}
      </p>
    </section>
  </main>
</template>
