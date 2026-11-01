<script setup lang="ts">
import { Activity, ArrowLeft, ArrowRight, MailCheck } from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import ThemeToggle from "../../app/ThemeToggle.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import { roleHelp, type Level, type Role } from "../members/roles";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Self-serve sign-up (QĐ-17). /signup asks to join an organization,
// workspace or project — usually opened from the link an admin shares,
// /signup?project=<id> — and always gets the same answer. The mailed link
// /signup#<token> finishes it: choose a password, signed in, and then wait
// for an admin of that node to approve. The token is read once, removed
// from the address bar and the history, and only ever sent in a body.
const { session } = useServices();
const route = useRoute();
const router = useRouter();
const token = ref("");
const state = ref<
  | "loading"
  | "unavailable"
  | "off"
  | "form"
  | "sent"
  | "invalid"
  | "exists"
  | "pending"
>("loading");
const message = ref("");
const busy = ref(false);

// The roles a visitor may ask for at each level; owners are added by owners.
const offered: Record<Level, Role[]> = {
  project: ["member", "viewer", "maintainer"],
  workspace: ["workspace_admin"],
  organization: ["org_admin"],
};
const levelWords: Record<Level, string> = {
  project: "a project",
  workspace: "a workspace",
  organization: "an organization",
};
const label = (s: string) => s.replaceAll("_", " ");

// From the shared link: ?project=<id>, ?workspace=<id> or ?organization=<id>.
const linked = computed<{ level: Level; id: string } | null>(() => {
  for (const level of ["project", "workspace", "organization"] as const) {
    const id = route.query[level];
    if (typeof id === "string" && id) return { level, id };
  }
  return null;
});
const form = ref({
  email: "",
  level: (linked.value?.level ?? "project") as Level,
  id: linked.value?.id ?? "",
  role: offered[linked.value?.level ?? "project"][0]!,
  note: "",
});
function pickLevel(value: string) {
  const level = (["project", "workspace", "organization"] as const).find(
    (l) => l === value,
  );
  if (!level) return;
  form.value.level = level;
  form.value.role = offered[level][0]!;
}

const password = ref(""),
  confirm = ref("");
const mismatch = computed(
  () => confirm.value !== "" && confirm.value !== password.value,
);

function explain(e: unknown): typeof state.value | "error" {
  if (e instanceof ApiError) {
    switch (e.code) {
      case "SIGNUP_OFF":
        return "off";
      case "SIGNUP_NOT_FOUND":
        return "invalid";
      case "ACCOUNT_EXISTS":
        return "exists";
      case "RATE_LIMITED":
        message.value =
          "Too many requests from here. Wait 15 minutes and try again.";
        break;
      case "INVALID_SIGNUP":
        message.value =
          "Check the email and the id from your admin, and pick a role.";
        break;
      case "INVALID_PASSWORD":
        message.value = "Choose a password of at least 12 characters.";
        break;
      case "ALREADY_LOGGED_IN":
        message.value =
          "You are signed in already. Sign out first, then open the link again.";
        break;
      default:
        message.value = e.message;
    }
  } else message.value = "Something went wrong. Please try again.";
  return "error";
}

onMounted(async () => {
  token.value = decodeURIComponent(location.hash.replace(/^#/, ""));
  if (token.value)
    history.replaceState(
      history.state,
      "",
      location.pathname + location.search,
    );
  await load();
});
// No answer from the bootstrap is not "sign-up is off".
async function load() {
  state.value = "loading";
  await session.ensure(true);
  state.value = !session.ready ? "unavailable" : session.signup ? "form" : "off";
}

async function ask() {
  if (busy.value) return;
  busy.value = true;
  message.value = "";
  try {
    await session.requestSignup({
      email: form.value.email.trim(),
      scopeLevel: form.value.level,
      scopeId: form.value.id.trim(),
      requestedRole: form.value.role,
      note: form.value.note.trim(),
    });
    state.value = "sent";
  } catch (e) {
    const s = explain(e);
    if (s !== "error") state.value = s;
  } finally {
    busy.value = false;
  }
}

async function finish() {
  if (busy.value || mismatch.value) return;
  busy.value = true;
  message.value = "";
  try {
    if (await session.completeSignup(token.value, password.value)) {
      password.value = confirm.value = "";
      state.value = "pending";
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

    <section class="signup-page m-auto w-full max-w-sm space-y-5 py-10">
      <p v-if="state === 'loading'" class="text-muted-foreground text-sm">
        Loading…
      </p>

      <template v-else-if="state === 'unavailable'">
        <h1 class="text-2xl font-semibold tracking-tight">
          The server did not answer
        </h1>
        <p class="text-muted-foreground text-sm">
          Check your connection and try again.
        </p>
        <Button variant="outline" class="w-full" @click="load"
          >Try again</Button
        >
      </template>

      <template v-else-if="state === 'off'">
        <h1 class="text-2xl font-semibold tracking-tight">
          Sign-up is not open here
        </h1>
        <p class="text-muted-foreground text-sm">
          Ask an admin of your team to add you or send you an invitation.
        </p>
        <Button as-child variant="outline" class="w-full"
          ><RouterLink to="/sign-in">Go to sign in</RouterLink></Button
        >
      </template>

      <template v-else-if="state === 'sent'">
        <MailCheck class="text-primary size-8" aria-hidden="true" />
        <h1 class="text-2xl font-semibold tracking-tight">Check your email</h1>
        <p class="text-muted-foreground text-sm">
          If
          <strong class="text-foreground font-medium">{{
            form.email.trim()
          }}</strong>
          can sign up here, a link to finish is on its way. It works once, for
          24 hours. After you choose a password, an admin of that team approves
          your request.
        </p>
        <Button as-child variant="outline" class="w-full"
          ><RouterLink to="/sign-in"><ArrowLeft />Back to sign in</RouterLink></Button
        >
      </template>

      <template v-else-if="state === 'invalid'">
        <h1 class="text-2xl font-semibold tracking-tight">
          This link cannot be used
        </h1>
        <p class="text-muted-foreground text-sm">
          It was already used, replaced by a newer one, or is older than 24
          hours. Ask to join again to get a new link.
        </p>
        <Button as-child variant="outline" class="w-full"
          ><RouterLink to="/signup">Ask to join again</RouterLink></Button
        >
      </template>

      <template v-else-if="state === 'exists'">
        <h1 class="text-2xl font-semibold tracking-tight">
          You already have an account
        </h1>
        <p class="text-muted-foreground text-sm">
          That email has an account by now. Sign in with its password, and ask
          an admin of the team to add you.
        </p>
        <Button as-child class="w-full"
          ><RouterLink to="/sign-in">Sign in<ArrowRight /></RouterLink
        ></Button>
      </template>

      <template v-else-if="state === 'pending'">
        <h1 class="text-2xl font-semibold tracking-tight">
          Your account is ready
        </h1>
        <p class="text-muted-foreground text-sm">
          You are signed in. Your request now waits for an admin of the team you
          asked to join; once they approve it, the project shows up on your
          dashboard.
        </p>
        <Button class="w-full" @click="router.replace('/')"
          >Open the dashboard<ArrowRight
        /></Button>
      </template>

      <!-- From the mailed link: choose the password. -->
      <form
        v-else-if="token"
        class="flex flex-col gap-5"
        @submit.prevent="finish"
      >
        <div class="space-y-1.5">
          <p
            class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
          >
            Sign up
          </p>
          <h1 class="text-2xl font-semibold tracking-tight">
            Choose your password
          </h1>
          <p class="text-muted-foreground text-sm">
            This creates your account. It sees nothing until an admin of the
            team approves your request.
          </p>
        </div>
        <div class="space-y-2">
          <Label for="signup-password">Password</Label>
          <Input
            id="signup-password"
            v-model="password"
            type="password"
            autocomplete="new-password"
            minlength="12"
            required
          />
          <p class="text-muted-foreground text-xs">At least 12 characters.</p>
        </div>
        <div class="space-y-2">
          <Label for="signup-confirm">Password again</Label>
          <Input
            id="signup-confirm"
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
          {{ busy ? "Creating your account…" : "Create account"
          }}<ArrowRight />
        </Button>
      </form>

      <!-- Asking to join. -->
      <form v-else class="flex flex-col gap-5" @submit.prevent="ask">
        <div class="space-y-1.5">
          <p
            class="text-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase"
          >
            Sign up
          </p>
          <h1 class="text-2xl font-semibold tracking-tight">Ask to join</h1>
          <p class="text-muted-foreground text-sm">
            We email you a link to choose a password. An admin of the team then
            approves your request before you can see anything.
          </p>
        </div>
        <div class="space-y-2">
          <Label for="signup-email">Email</Label>
          <Input
            id="signup-email"
            v-model="form.email"
            type="email"
            autocomplete="username"
            required
            maxlength="254"
          />
        </div>
        <p
          v-if="linked"
          class="signup-target bg-muted rounded-md px-3 py-2 text-sm"
        >
          Joining {{ levelWords[linked.level] }} from the link your admin
          shared.
        </p>
        <template v-else>
          <div class="space-y-2">
            <Label for="signup-level">What to join</Label>
            <NativeSelect
              id="signup-level"
              :model-value="form.level"
              class="w-full"
              @update:model-value="pickLevel($event)"
            >
              <option value="project">Project</option>
              <option value="workspace">Workspace</option>
              <option value="organization">Organization</option>
            </NativeSelect>
          </div>
          <div class="space-y-2">
            <Label for="signup-id">Its id</Label>
            <Input
              id="signup-id"
              v-model="form.id"
              required
              maxlength="36"
              autocapitalize="none"
              spellcheck="false"
              class="font-mono text-xs"
              placeholder="00000000-0000-0000-0000-000000000000"
            />
            <p class="text-muted-foreground text-xs">
              Ask an admin of the team for the sign-up link, or for this id.
            </p>
          </div>
        </template>
        <div class="space-y-2">
          <Label for="signup-role">Role you ask for</Label>
          <NativeSelect id="signup-role" v-model="form.role" class="w-full">
            <option v-for="r in offered[form.level]" :key="r" :value="r">
              {{ label(r) }}
            </option>
          </NativeSelect>
          <p class="text-muted-foreground text-xs">
            {{ label(form.role) }}: {{ roleHelp[form.role] }}. The admin may
            grant another role.
          </p>
        </div>
        <div class="space-y-2">
          <Label for="signup-note">Note for the admin (optional)</Label>
          <textarea
            id="signup-note"
            v-model="form.note"
            rows="3"
            maxlength="1000"
            class="border-input bg-background focus-visible:border-ring focus-visible:ring-ring/50 w-full rounded-md border px-3 py-2 text-sm shadow-xs outline-none focus-visible:ring-3"
            placeholder="Who you are and why you need access"
          />
        </div>
        <p
          v-if="message"
          class="bg-danger-soft text-danger border-danger/20 rounded-md border px-3 py-2 text-sm"
          role="alert"
        >
          {{ message }}
        </p>
        <Button type="submit" class="w-full" :disabled="busy">
          {{ busy ? "Sending…" : "Send me the link" }}<ArrowRight />
        </Button>
        <Button as-child variant="ghost" class="w-full">
          <RouterLink to="/sign-in"><ArrowLeft />Back to sign in</RouterLink>
        </Button>
      </form>
    </section>
  </main>
</template>
