<script setup lang="ts">
import { Activity, ArrowLeft, ArrowRight } from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import ThemeToggle from "../../app/ThemeToggle.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Forgot password (§11.2): /forgot asks for the email and always answers
// the same; /reset#<token> (the mailed link — a fragment, never sent to a
// server) chooses the new password. The token is read once and removed
// from the address bar and history.
const { session } = useServices();
const route = useRoute();
const step = computed(() => (route.path === "/reset" ? "reset" : "forgot"));
const email = ref(""),
  password = ref(""),
  confirm = ref(""),
  token = ref(""),
  busy = ref(false),
  done = ref(false),
  message = ref("");
const mismatch = computed(
  () => confirm.value !== "" && confirm.value !== password.value,
);

onMounted(() => {
  if (step.value !== "reset") return;
  token.value = decodeURIComponent(location.hash.replace(/^#/, ""));
  history.replaceState(history.state, "", location.pathname);
  if (!token.value)
    message.value =
      "This page needs the link from the email. Ask for a new one below.";
});

function explain(e: unknown) {
  if (!(e instanceof ApiError))
    return "Something went wrong. Please try again.";
  switch (e.code) {
    case "RATE_LIMITED":
      return "Too many requests from here. Wait 15 minutes and try again.";
    case "PASSWORD_RESET_OFF":
      return "Password reset by email is not set up here. Ask an admin.";
    case "RESET_NOT_FOUND":
      return "This link is no longer valid: it was used, replaced by a newer one, or is older than 30 minutes. Ask for a new one.";
    case "INVALID_PASSWORD":
      return "Choose a password of at least 12 characters.";
    default:
      return e.message;
  }
}

async function submit() {
  if (busy.value || mismatch.value) return;
  busy.value = true;
  message.value = "";
  try {
    if (step.value === "forgot")
      await session.forgotPassword(email.value.trim());
    else await session.resetPassword(token.value, password.value);
    password.value = confirm.value = "";
    done.value = true;
  } catch (e) {
    message.value = explain(e);
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

    <section class="password-page m-auto w-full max-w-sm space-y-5 py-10">
      <template v-if="done">
        <h1 class="text-2xl font-semibold tracking-tight">
          {{ step === "forgot" ? "Check your email" : "Password changed" }}
        </h1>
        <p class="text-muted-foreground text-sm">
          <template v-if="step === 'forgot'">
            If an account uses that email, a link to choose a new password is on
            its way. It works once, for 30 minutes.
          </template>
          <template v-else>
            Every session was signed out. Sign in with your new password; if
            two-factor authentication is on, it still asks for your code.
          </template>
        </p>
        <Button as-child class="w-full"
          ><RouterLink to="/sign-in">Go to sign in<ArrowRight /></RouterLink
        ></Button>
      </template>

      <form v-else class="flex flex-col gap-5" @submit.prevent="submit">
        <div class="space-y-1.5">
          <p
            class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
          >
            Password
          </p>
          <h1 class="text-2xl font-semibold tracking-tight">
            {{
              step === "forgot"
                ? "Forgot your password?"
                : "Choose a new password"
            }}
          </h1>
          <p class="text-muted-foreground text-sm">
            {{
              step === "forgot"
                ? "Enter the email you sign in with. We will send a link to choose a new password."
                : "At least 12 characters. Changing it signs out every session."
            }}
          </p>
        </div>
        <div v-if="step === 'forgot'" class="space-y-2">
          <Label for="forgot-email">Email</Label>
          <Input
            id="forgot-email"
            v-model="email"
            type="email"
            autocomplete="username"
            required
            maxlength="254"
          />
        </div>
        <template v-else-if="token">
          <div class="space-y-2">
            <Label for="reset-password">New password</Label>
            <Input
              id="reset-password"
              v-model="password"
              type="password"
              autocomplete="new-password"
              minlength="12"
              required
            />
          </div>
          <div class="space-y-2">
            <Label for="reset-confirm">New password again</Label>
            <Input
              id="reset-confirm"
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
        </template>
        <p
          v-if="message"
          class="bg-danger-soft text-danger border-danger/20 rounded-md border px-3 py-2 text-sm"
          role="alert"
        >
          {{ message }}
        </p>
        <Button
          v-if="step === 'forgot' || token"
          type="submit"
          class="w-full"
          :disabled="busy || mismatch"
        >
          {{
            busy
              ? "Sending…"
              : step === "forgot"
                ? "Send the link"
                : "Change password"
          }}<ArrowRight />
        </Button>
        <Button as-child variant="ghost" class="w-full">
          <RouterLink :to="step === 'reset' && !token ? '/forgot' : '/sign-in'"
            ><ArrowLeft />{{
              step === "reset" && !token
                ? "Ask for a new link"
                : "Back to sign in"
            }}</RouterLink
          >
        </Button>
      </form>
    </section>
  </main>
</template>
