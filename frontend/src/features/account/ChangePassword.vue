<script setup lang="ts">
// §11.2 "đổi password → revoke mọi session khác": the current password,
// then a new one; this browser stays signed in, every other one is signed
// out. Accounts that sign in only with Google/GitHub are sent to "Forgot
// password" for a first password.
import { KeyRound } from "@lucide/vue";
import { ref } from "vue";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const { session } = useServices();
const current = ref("");
const next = ref("");
const repeat = ref("");
const busy = ref(false);
const error = ref("");
const done = ref(false);

function explain(e: unknown) {
  if (!(e instanceof ApiError)) return "Not changed. Try again.";
  switch (e.code) {
    case "WRONG_PASSWORD":
      return "The current password is not right.";
    case "INVALID_PASSWORD":
      return e.detail ?? "Choose a password of at least 12 characters.";
    case "NO_PASSWORD":
      return "This account signs in with Google or GitHub and has no password yet. Sign out and use “Forgot password?” to set one.";
    case "RATE_LIMITED":
      return "Too many wrong attempts. Wait a while before trying again.";
    default:
      return e.message;
  }
}

async function submit() {
  error.value = "";
  done.value = false;
  if (next.value !== repeat.value) {
    error.value = "The two new passwords are not the same.";
    return;
  }
  busy.value = true;
  try {
    await session.changePassword(current.value, next.value);
    done.value = true;
    current.value = next.value = repeat.value = "";
  } catch (e) {
    error.value = explain(e);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <section
    class="change-password bg-card mt-5 space-y-4 rounded-lg border p-5"
    aria-labelledby="password-title"
  >
    <div class="flex items-center gap-2">
      <KeyRound :size="18" class="text-muted-foreground" aria-hidden="true" />
      <h2 id="password-title" class="font-semibold">Password</h2>
    </div>
    <p class="text-muted-foreground text-sm">
      Changing it signs out every other browser where you are signed in. This
      one stays signed in.
    </p>
    <form class="grid max-w-md gap-3" @submit.prevent="submit">
      <div class="flex flex-col gap-1.5">
        <Label for="pw-current">Current password</Label>
        <Input
          id="pw-current"
          v-model="current"
          type="password"
          autocomplete="current-password"
          required
        />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="pw-new">New password</Label>
        <Input
          id="pw-new"
          v-model="next"
          type="password"
          autocomplete="new-password"
          minlength="12"
          required
        />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="pw-repeat">New password again</Label>
        <Input
          id="pw-repeat"
          v-model="repeat"
          type="password"
          autocomplete="new-password"
          minlength="12"
          required
        />
      </div>
      <Alert v-if="error" variant="destructive" role="alert">
        <AlertDescription>{{ error }}</AlertDescription>
      </Alert>
      <Alert v-if="done" role="status" class="password-changed">
        <AlertDescription
          >Password changed. Other browsers have been signed
          out.</AlertDescription
        >
      </Alert>
      <div>
        <Button type="submit" :disabled="busy">{{
          busy ? "Changing…" : "Change password"
        }}</Button>
      </div>
    </form>
  </section>
</template>
