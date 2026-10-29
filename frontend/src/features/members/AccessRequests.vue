<script setup lang="ts">
import { Check, Copy, UserCheck, UserX } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { AccessRequest } from "./membersRepository";
import { grantable, type MemberNode, type Role } from "./roles";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

// Requests to join this node (self-serve sign-up, QĐ-17), for the people who
// manage its members. Each email here was proven by the mailed link. The
// owner role is offered only to owners (the api answers 403 OWNER_ONLY,
// including for a request that asked for it).
const props = defineProps<{ node: MemberNode; canOwners: boolean }>();
const emit = defineEmits<{ decided: [] }>();
const { members } = useServices();

const offered = computed(() =>
  grantable[props.node.level].filter(
    (r) => r !== "org_owner" || props.canOwners,
  ),
);
const label = (s: string) => s.replaceAll("_", " ");

// The link to share: the sign-up page with this node filled in.
const link = computed(
  () =>
    `${location.origin}/signup?${props.node.level}=${encodeURIComponent(props.node.id)}`,
);
const copied = ref(false);
async function copy() {
  try {
    await navigator.clipboard.writeText(link.value);
    copied.value = true;
    setTimeout(() => (copied.value = false), 2000);
  } catch {
    copied.value = false;
  }
}

const list = useResource<AccessRequest[]>();
// The role each row would grant: the one asked for when it may be granted
// here, otherwise the first one this person may grant.
const chosen = ref<Record<string, Role>>({});
function refresh() {
  return list.load(async (signal) => {
    const items = await members.accessRequests(props.node, signal);
    chosen.value = Object.fromEntries(
      items.map((a) => [
        a.id,
        offered.value.includes(a.requestedRole)
          ? a.requestedRole
          : (offered.value[0] ?? a.requestedRole),
      ]),
    );
    return items;
  });
}
watch(() => props.node, refresh, { immediate: true });

function pick(a: AccessRequest, value: string) {
  const role = offered.value.find((r) => r === value);
  if (role) chosen.value = { ...chosen.value, [a.id]: role };
}

const mutation = useMutation();
async function approve(a: AccessRequest) {
  const role = chosen.value[a.id] ?? a.requestedRole;
  const done = await mutation.run(a.id, async (signal) => {
    await members.decideAccessRequest(
      props.node,
      a.id,
      "approved",
      role === a.requestedRole ? "" : role,
      signal,
    );
    return true;
  });
  if (done) {
    emit("decided");
    await refresh();
  }
}
// Denying is one click from approving, so it asks once more on the row.
const confirming = ref<string | null>(null);
async function deny(a: AccessRequest) {
  if (confirming.value !== a.id) {
    confirming.value = a.id;
    return;
  }
  confirming.value = null;
  const done = await mutation.run(a.id, async (signal) => {
    await members.decideAccessRequest(props.node, a.id, "denied", "", signal);
    return true;
  });
  if (done) await refresh();
}
</script>
<template>
  <section class="access-requests mt-6" aria-labelledby="requests-title">
    <h2 id="requests-title" class="mb-1 text-sm font-semibold">
      Requests to join
    </h2>
    <p class="text-muted-foreground mb-3 text-xs">
      Anyone with this link can ask to join; nobody gets in until you approve
      them here.
    </p>
    <div class="mb-4 flex gap-2">
      <Input
        :model-value="link"
        readonly
        aria-label="Sign-up link"
        class="font-mono text-xs"
        @focus="($event.target as HTMLInputElement).select()"
      />
      <Button variant="outline" size="sm" type="button" @click="copy"
        ><Check v-if="copied" /><Copy v-else />{{
          copied ? "Copied" : "Copy"
        }}</Button
      >
    </div>

    <AsyncState
      :loading="list.loading.value"
      :error="list.error.value"
      @retry="refresh"
    >
      <p
        v-if="!list.data.value?.length"
        class="text-muted-foreground bg-card rounded-lg border px-4 py-3 text-sm"
      >
        No requests waiting.
      </p>
      <ul v-else class="bg-card divide-y rounded-lg border">
        <li
          v-for="a in list.data.value"
          :key="a.id"
          class="access-request flex flex-wrap items-center gap-3 px-4 py-3"
        >
          <span class="min-w-0 flex-1">
            <span class="block text-sm font-medium">{{ a.email }}</span>
            <span class="text-muted-foreground block text-xs">
              asked for {{ label(a.requestedRole) }} ·
              {{ time(a.createdAt) }}
            </span>
            <span
              v-if="a.note"
              class="text-foreground mt-1 block text-sm whitespace-pre-line"
              >{{ a.note }}</span
            >
            <span
              v-if="!offered.includes(a.requestedRole)"
              class="text-muted-foreground mt-1 block text-xs"
              >Only an organization owner can grant the owner role.</span
            >
            <ErrorNote
              v-if="mutation.error.value?.key === a.id"
              class="mt-2"
              :error="mutation.error.value.error"
            />
          </span>
          <NativeSelect
            v-if="offered.length > 1"
            :model-value="chosen[a.id] ?? a.requestedRole"
            class="w-40"
            :disabled="mutation.busy.value !== null"
            :aria-label="`Role to grant ${a.email}`"
            @update:model-value="pick(a, $event)"
          >
            <option v-for="r in offered" :key="r" :value="r">
              {{ label(r) }}
            </option>
          </NativeSelect>
          <Button
            size="xs"
            :disabled="mutation.busy.value !== null || !offered.length"
            :aria-label="`Approve ${a.email} as ${label(chosen[a.id] ?? a.requestedRole)}`"
            @click="approve(a)"
            ><UserCheck />Approve</Button
          >
          <Button
            :variant="confirming === a.id ? 'destructive' : 'ghost'"
            size="xs"
            :disabled="mutation.busy.value !== null"
            :aria-label="
              confirming === a.id ? `Confirm denying ${a.email}` : `Deny ${a.email}`
            "
            @click="deny(a)"
            @blur="confirming = null"
            ><UserX />{{ confirming === a.id ? "Confirm" : "Deny" }}</Button
          >
        </li>
      </ul>
    </AsyncState>
  </section>
</template>
