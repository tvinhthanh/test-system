<script setup lang="ts">
import { Link2, Trash2 } from "@lucide/vue";
import { onMounted, ref } from "vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { LinkedIdentity } from "./identitiesRepository";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import AsyncState from "../../shared/AsyncState.vue";
import { time } from "../../shared/format";
import { Button } from "@/components/ui/button";

// Google / GitHub sign-ins linked to this account (§11.2). A link is made
// by signing in with a provider whose verified email matches the account;
// it is removed here, except the account's last way in.
const { identities, session } = useServices();
const list = useResource<LinkedIdentity[]>();
function refresh() {
  return list.load((signal) => identities.list(signal));
}
onMounted(refresh);

const label = { google: "Google", github: "GitHub" } as const;
const mutation = useMutation();
const confirming = ref<string | null>(null);
async function unlink(li: LinkedIdentity) {
  if (confirming.value !== li.id) {
    confirming.value = li.id;
    return;
  }
  confirming.value = null;
  const done = await mutation.run(li.id, async (signal) => {
    await identities.unlink(li.id, signal);
    return true;
  });
  if (done) await refresh();
}
</script>
<template>
  <section
    class="linked-accounts bg-card mt-5 space-y-4 rounded-lg border p-5"
    aria-labelledby="linked-title"
  >
    <div class="flex items-center gap-2">
      <Link2 :size="18" class="text-muted-foreground" aria-hidden="true" />
      <h2 id="linked-title" class="font-semibold">Connected accounts</h2>
    </div>
    <AsyncState
      :loading="list.loading.value"
      :error="list.error.value"
      @retry="refresh"
    >
      <p v-if="!list.data.value?.length" class="text-muted-foreground text-sm">
        None yet.
        <template v-if="session.providers.length">
          Sign out and choose “Continue with
          {{ session.providers.map((p) => label[p]).join(" or ") }}” with a
          provider account that uses
          <strong class="text-foreground font-medium">{{
            session.user?.email
          }}</strong>
          (verified there) to connect it.
        </template>
      </p>
      <ul v-else class="divide-y rounded-md border">
        <li
          v-for="li in list.data.value"
          :key="li.id"
          class="flex flex-wrap items-center gap-3 px-3 py-2.5"
        >
          <span class="min-w-0 flex-1">
            <span class="block text-sm font-medium">{{
              label[li.provider]
            }}</span>
            <span class="text-muted-foreground block text-xs wrap-break-word">
              {{ li.email }} · connected {{ time(li.createdAt) }}
              <template v-if="li.lastLoginAt">
                · last used {{ time(li.lastLoginAt) }}</template
              >
            </span>
            <ErrorNote
              v-if="mutation.error.value?.key === li.id"
              class="mt-2"
              :error="mutation.error.value.error"
            />
          </span>
          <Button
            :variant="confirming === li.id ? 'destructive' : 'ghost'"
            size="xs"
            :disabled="mutation.busy.value !== null"
            :aria-label="
              confirming === li.id
                ? `Confirm disconnecting ${label[li.provider]}`
                : `Disconnect ${label[li.provider]}`
            "
            @click="unlink(li)"
            @blur="confirming = null"
            ><Trash2 />{{
              confirming === li.id ? "Confirm" : "Disconnect"
            }}</Button
          >
        </li>
      </ul>
    </AsyncState>
  </section>
</template>
