<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Activity, ArrowLeft, ArrowRight, RefreshCw } from "@lucide/vue";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import type { OAuthProvider } from "./sessionRepository";
import ThemeToggle from "../../app/ThemeToggle.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
const { session, fixture } = useServices();
const router = useRouter(),
  route = useRoute();
const email = ref(""),
  password = ref(""),
  code = ref("");
async function submit() {
  const done = await session.login(email.value.trim(), password.value);
  // With MFA on, the password alone opens a challenge: the form turns into
  // the code step (session.challenge) and the password is not kept.
  if (done || session.challenge) password.value = "";
  if (done) await enter();
}
async function submitCode() {
  const done = await session.completeMfa(code.value.trim());
  code.value = "";
  if (done) await enter();
}
const target = computed(() =>
  typeof route.query.redirect === "string" &&
  route.query.redirect.startsWith("/p/")
    ? route.query.redirect
    : "/",
);
async function enter() {
  await router.replace(target.value);
}

// Google / GitHub (§11.2): a full-page trip to the provider and back. The
// api decides which exist (bootstrap `providers`) and where the trip ends:
// in the app, on the code step (?step=mfa), or here with ?oauth=<reason>.
const providerLabel = { google: "Google", github: "GitHub" } as const;
const oauthHref = (p: OAuthProvider) =>
  `/v1/session/oauth/${p}/start?redirect=${encodeURIComponent(target.value)}`;
const oauthReasons: Record<string, string> = {
  denied: "Sign-in was cancelled at the provider.",
  state:
    "That sign-in link expired or came from another tab. Please try again.",
  provider:
    "The provider did not confirm who you are. Please try again in a moment.",
  no_account:
    "No verified account here uses that email. Ask an admin to add you, or sign in with your password.",
  conflict:
    "Your account is already linked to a different account at that provider. Use that one, or sign in with your password.",
  disabled: "That sign-in option is turned off.",
  unavailable: "Sign-in is unavailable right now. Please try again.",
};
const oauthError = computed(() => {
  const reason = route.query.oauth;
  return typeof reason === "string"
    ? (oauthReasons[reason] ?? oauthReasons.unavailable)
    : "";
});
onMounted(() => {
  if (route.query.step === "mfa") session.resumeMfa();
});
</script>
<template>
  <main class="bg-background grid min-h-svh lg:grid-cols-[5fr_4fr]">
    <!-- Product statement; the form is the only interactive thing on the page -->
    <section
      class="bg-sidebar relative hidden flex-col justify-between border-r p-10 lg:flex"
    >
      <div class="flex items-center gap-2.5">
        <span
          class="bg-primary text-primary-foreground inline-flex size-8 items-center justify-center rounded-md"
          ><Activity :size="17" aria-hidden="true"
        /></span>
        <span class="text-sm font-semibold tracking-tight">ATP</span>
        <span class="text-muted-foreground text-sm">Report Center</span>
      </div>
      <div class="max-w-md space-y-4">
        <h2 class="text-3xl font-semibold tracking-tight text-balance">
          Long-term memory for your end-to-end suite.
        </h2>
        <p class="text-muted-foreground text-base leading-relaxed">
          Playwright tells you how this run failed. The Report Center tells you
          how this test has behaved across the last five hundred runs — whether
          it is flaky, which earlier failure it resembles, and who decided what.
        </p>
        <dl class="grid grid-cols-3 gap-4 pt-2 text-sm">
          <div>
            <dt class="text-muted-foreground text-xs">Identity</dt>
            <dd class="font-medium">Survives renames</dd>
          </div>
          <div>
            <dt class="text-muted-foreground text-xs">Flaky signal</dt>
            <dd class="font-medium">Across runs, not within</dd>
          </div>
          <div>
            <dt class="text-muted-foreground text-xs">Triage</dt>
            <dd class="font-medium">Decisions, audited</dd>
          </div>
        </dl>
      </div>
      <p class="text-muted-foreground text-xs">P1-min · reporter-first</p>
    </section>

    <section class="flex flex-col p-6 sm:p-10">
      <div class="flex items-center justify-between lg:justify-end">
        <div class="flex items-center gap-2 lg:hidden">
          <span
            class="bg-primary text-primary-foreground inline-flex size-7 items-center justify-center rounded-md"
            ><Activity :size="15" aria-hidden="true"
          /></span>
          <span class="text-sm font-semibold">ATP</span>
        </div>
        <ThemeToggle />
      </div>
      <form
        v-if="session.challenge"
        class="m-auto flex w-full max-w-sm flex-col gap-5 py-10"
        @submit.prevent="submitCode"
      >
        <div class="space-y-1.5">
          <p
            class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
          >
            Two-factor authentication
          </p>
          <h1 class="text-2xl font-semibold tracking-tight">Enter your code</h1>
          <p class="text-muted-foreground text-sm">
            Open your authenticator app and type the 6-digit code for ATP. Lost
            your phone? A recovery code works here too, once.
          </p>
        </div>
        <div class="space-y-2">
          <Label for="mfa-code">Code</Label>
          <Input
            id="mfa-code"
            v-model="code"
            autocomplete="one-time-code"
            autocapitalize="none"
            spellcheck="false"
            required
            maxlength="20"
            class="font-mono tracking-widest"
            aria-describedby="login-error"
          />
        </div>
        <p
          v-if="session.error"
          id="login-error"
          class="bg-danger-soft text-danger border-danger/20 rounded-md border px-3 py-2 text-sm"
          role="alert"
        >
          {{
            session.error instanceof ApiError
              ? session.error.message
              : "Sign-in could not be completed."
          }}
        </p>
        <Button type="submit" class="w-full" :disabled="session.busy">
          {{ session.busy ? "Checking…" : "Verify" }}<ArrowRight />
        </Button>
        <Button
          variant="ghost"
          type="button"
          :disabled="session.busy"
          @click="session.cancelMfa()"
        >
          <ArrowLeft />Use another account
        </Button>
      </form>
      <form
        v-else
        class="m-auto flex w-full max-w-sm flex-col gap-5 py-10"
        @submit.prevent="submit"
      >
        <div class="space-y-1.5">
          <p
            class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
          >
            Your test workspace
          </p>
          <h1 class="text-2xl font-semibold tracking-tight">Sign in</h1>
          <p class="text-muted-foreground text-sm">
            Welcome back to your Report Center.
          </p>
        </div>
        <div class="space-y-2">
          <Label for="email">Email</Label>
          <Input
            id="email"
            v-model="email"
            type="email"
            autocomplete="username"
            required
            maxlength="254"
          />
        </div>
        <div class="space-y-2">
          <Label for="password">Password</Label>
          <Input
            id="password"
            v-model="password"
            type="password"
            autocomplete="current-password"
            required
            aria-describedby="login-error"
          />
        </div>
        <p
          v-if="oauthError && !session.error"
          class="oauth-error bg-danger-soft text-danger border-danger/20 rounded-md border px-3 py-2 text-sm"
          role="alert"
        >
          {{ oauthError }}
        </p>
        <p
          v-if="session.error"
          id="login-error"
          class="bg-danger-soft text-danger border-danger/20 rounded-md border px-3 py-2 text-sm"
          role="alert"
        >
          {{
            session.error instanceof ApiError
              ? session.error.message
              : "Sign-in could not be completed."
          }}
        </p>
        <Button type="submit" class="w-full" :disabled="session.busy">
          {{ session.busy ? "Signing in…" : "Sign in" }}<ArrowRight />
        </Button>
        <Button
          v-if="!session.ready"
          variant="outline"
          type="button"
          :disabled="session.busy"
          @click="session.ensure(true)"
        >
          <RefreshCw />Retry connection
        </Button>
        <template v-if="session.providers.length">
          <div
            class="text-muted-foreground flex items-center gap-3 text-xs"
            aria-hidden="true"
          >
            <span class="bg-border h-px flex-1" />or<span
              class="bg-border h-px flex-1"
            />
          </div>
          <Button
            v-for="p in session.providers"
            :key="p"
            as-child
            variant="outline"
            class="oauth-button w-full"
          >
            <a :href="oauthHref(p)">
              <svg
                v-if="p === 'google'"
                viewBox="0 0 24 24"
                aria-hidden="true"
                class="size-4"
              >
                <path
                  fill="#4285F4"
                  d="M23.5 12.3c0-.8-.1-1.6-.2-2.3H12v4.5h6.5a5.6 5.6 0 0 1-2.4 3.6v3h3.9c2.2-2.1 3.5-5.1 3.5-8.8Z"
                />
                <path
                  fill="#34A853"
                  d="M12 24c3.2 0 6-1.1 8-2.9l-3.9-3c-1.1.7-2.5 1.2-4.1 1.2-3.1 0-5.8-2.1-6.7-5H1.3v3.1A12 12 0 0 0 12 24Z"
                />
                <path
                  fill="#FBBC05"
                  d="M5.3 14.3a7.2 7.2 0 0 1 0-4.6V6.6h-4a12 12 0 0 0 0 10.8l4-3.1Z"
                />
                <path
                  fill="#EA4335"
                  d="M12 4.8c1.8 0 3.3.6 4.6 1.8l3.4-3.4A12 12 0 0 0 1.3 6.6l4 3.1c.9-2.9 3.6-4.9 6.7-4.9Z"
                />
              </svg>
              <svg
                v-else
                viewBox="0 0 24 24"
                aria-hidden="true"
                class="size-4"
                fill="currentColor"
              >
                <path
                  d="M12 .3a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2.2c-3.3.7-4-1.4-4-1.4-.6-1.4-1.4-1.8-1.4-1.8-1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.8 1.3 3.5 1 .1-.8.4-1.3.7-1.6-2.7-.3-5.5-1.3-5.5-6 0-1.2.5-2.3 1.2-3.1-.1-.4-.5-1.6.1-3.2 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17.3 4.7 18.3 5 18.3 5c.7 1.6.3 2.8.1 3.2.8.8 1.2 1.9 1.2 3.1 0 4.6-2.8 5.6-5.5 5.9.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6A12 12 0 0 0 12 .3Z"
                />
              </svg>
              Continue with {{ providerLabel[p] }}
            </a>
          </Button>
        </template>
        <p
          v-if="fixture"
          class="text-muted-foreground border-t pt-4 text-center text-xs"
        >
          Fixture preview. Use any email and a non-empty password.
        </p>
      </form>
    </section>
  </main>
</template>
