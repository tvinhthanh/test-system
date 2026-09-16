<script setup lang="ts">
import { ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowRight, Activity, RefreshCw } from "@lucide/vue";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
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
  <main class="sign-in">
    <div class="brand">
      <Activity :size="24" /><strong>ATP</strong><span>Report Center</span>
    </div>
    <form class="sign-in-form" @submit.prevent="submit">
      <p class="eyebrow">YOUR TEST WORKSPACE</p>
      <h1>Sign in</h1>
      <p class="muted">Welcome back to your Report Center.</p>
      <label for="email">Email</label
      ><input
        id="email"
        v-model="email"
        type="email"
        autocomplete="username"
        required
        maxlength="254"
      /><label for="password">Password</label
      ><input
        id="password"
        v-model="password"
        type="password"
        autocomplete="current-password"
        required
        aria-describedby="login-error"
      />
      <p v-if="session.error" id="login-error" class="error-text" role="alert">
        {{
          session.error instanceof ApiError
            ? session.error.message
            : "Sign-in could not be completed."
        }}
      </p>
      <button class="button primary" type="submit" :disabled="session.busy">
        {{ session.busy ? "Signing in..." : "Sign in"
        }}<ArrowRight :size="16" /></button
      ><button
        v-if="!session.ready"
        class="button"
        type="button"
        :disabled="session.busy"
        @click="session.ensure(true)"
      >
        <RefreshCw :size="16" />Retry connection
      </button>
      <p v-if="fixture" class="fixture-note">
        Fixture preview. Use any email and a non-empty password.
      </p>
    </form>
  </main>
</template>
