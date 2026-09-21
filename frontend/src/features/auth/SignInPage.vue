<script setup lang="ts">
import { ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Activity, ArrowRight, RefreshCw } from "@lucide/vue";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import ThemeToggle from "../../app/ThemeToggle.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
const { session, fixture } = useServices();
const router = useRouter(),
  route = useRoute();
const email = ref(""),
  password = ref("");
async function submit() {
  if (await session.login(email.value.trim(), password.value)) {
    password.value = "";
    const target =
      typeof route.query.redirect === "string" &&
      route.query.redirect.startsWith("/p/")
        ? route.query.redirect
        : "/";
    await router.replace(target);
  }
}
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
