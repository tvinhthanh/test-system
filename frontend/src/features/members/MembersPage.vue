<script setup lang="ts">
import { RefreshCw, Trash2, UserPlus, Users } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Member } from "./membersRepository";
import { projectRoles, type ProjectRole } from "./roles";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { useProjectPermission } from "../reports/useProjectPermission";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const route = useRoute();
const { members, session } = useServices();
const project = computed(() => String(route.params.project));
// The server answers 403 either way; this only decides whether to offer
// the form and the row controls.
const canManage = useProjectPermission("member.manage");
const list = useResource<Member[]>();
function refresh() {
  return list.load((signal) => members.all(project.value, signal));
}
watch(project, refresh, { immediate: true });

const roleHelp: Record<ProjectRole, string> = {
  maintainer: "tokens, retention, identity merge, release decisions",
  member: "triage and test management",
  viewer: "read only",
};
const label = (s: string) => s.replaceAll("_", " ");

const draft = ref({ email: "", role: "member" as ProjectRole });
const mutation = useMutation();
async function add() {
  const email = draft.value.email.trim();
  const done = await mutation.run("new", (signal) =>
    members.set(project.value, email, draft.value.role, signal),
  );
  if (done) {
    draft.value = { email: "", role: draft.value.role };
    await refresh();
  }
}
async function change(m: Member, role: ProjectRole) {
  const done = await mutation.run(m.userId, (signal) =>
    members.set(project.value, m.email, role, signal),
  );
  if (done) await refresh();
}
// Removing someone is one click away from the role select, so it asks
// once more on the row instead of a browser dialog.
const confirming = ref<string | null>(null);
async function remove(m: Member) {
  if (confirming.value !== m.userId) {
    confirming.value = m.userId;
    return;
  }
  confirming.value = null;
  const done = await mutation.run(m.userId, async (signal) => {
    await members.revoke(project.value, m.userId, signal);
    return true;
  });
  if (done) await refresh();
}
const isProjectRole = (role: string): role is ProjectRole =>
  projectRoles.some((r) => r === role);
</script>
<template>
  <PageHeader
    eyebrow="Project"
    title="Members"
    description="Who can open this project, and with which role. Roles held on the workspace or organization apply here too and are changed at their own level."
  >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh members"
      title="Refresh members"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>

  <form
    v-if="canManage"
    class="member-form bg-card mb-5 grid gap-4 rounded-lg border p-4 sm:grid-cols-[minmax(0,1fr)_10rem_auto] sm:items-end"
    @submit.prevent="add"
  >
    <div class="space-y-2">
      <Label for="member-email">Email of an existing account</Label>
      <Input
        id="member-email"
        v-model="draft.email"
        type="email"
        required
        maxlength="254"
        autocomplete="off"
        placeholder="name@company.com"
      />
    </div>
    <div class="space-y-2">
      <Label for="member-role">Role</Label>
      <NativeSelect id="member-role" v-model="draft.role" class="w-full">
        <option v-for="r in projectRoles" :key="r" :value="r">{{ r }}</option>
      </NativeSelect>
    </div>
    <Button type="submit" size="sm" :disabled="mutation.busy.value !== null"
      ><UserPlus />Add</Button
    >
    <p class="text-muted-foreground text-xs sm:col-span-3">
      {{ draft.role }}: {{ roleHelp[draft.role] }}. Adding someone who is
      already here changes their project role.
    </p>
    <ErrorNote
      v-if="mutation.error.value?.key === 'new'"
      class="sm:col-span-3"
      :error="mutation.error.value.error"
    />
  </form>

  <AsyncState
    :loading="list.loading.value"
    :error="list.error.value"
    @retry="refresh"
  >
    <EmptyState
      v-if="!list.data.value?.length"
      :icon="Users"
      title="No members yet"
      description="Add people by the email they sign in with."
    />
    <div v-else class="bg-card overflow-x-auto rounded-lg border">
      <Table class="member-table min-w-[560px]">
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead>Email</TableHead>
            <TableHead class="w-44">Role</TableHead>
            <TableHead class="w-40">Since</TableHead>
            <TableHead class="w-28"
              ><span class="sr-only">Actions</span></TableHead
            >
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="m in list.data.value"
            :key="`${m.userId}:${m.level}`"
          >
            <TableCell class="font-medium">
              {{ m.email }}
              <Badge
                v-if="m.userId === session.user?.id"
                variant="outline"
                class="ml-1.5"
                >you</Badge
              >
              <ErrorNote
                v-if="mutation.error.value?.key === m.userId"
                class="mt-2"
                :error="mutation.error.value.error"
              />
            </TableCell>
            <TableCell>
              <NativeSelect
                v-if="canManage && m.level === 'project'"
                :model-value="m.role"
                class="w-32"
                :disabled="mutation.busy.value !== null"
                :aria-label="`Role of ${m.email}`"
                @update:model-value="isProjectRole($event) && change(m, $event)"
              >
                <option v-for="r in projectRoles" :key="r" :value="r">
                  {{ r }}
                </option>
              </NativeSelect>
              <span v-else class="text-sm">
                {{ label(m.role) }}
                <span
                  v-if="m.level !== 'project'"
                  class="text-muted-foreground block text-xs"
                  >via {{ m.level }}</span
                >
              </span>
            </TableCell>
            <TableCell class="text-muted-foreground text-xs">{{
              time(m.createdAt)
            }}</TableCell>
            <TableCell class="text-right">
              <Button
                v-if="canManage && m.level === 'project'"
                :variant="confirming === m.userId ? 'destructive' : 'ghost'"
                size="xs"
                :disabled="mutation.busy.value !== null"
                :aria-label="
                  confirming === m.userId
                    ? `Confirm removing ${m.email}`
                    : `Remove ${m.email}`
                "
                @click="remove(m)"
                @blur="confirming = null"
                ><Trash2 />{{
                  confirming === m.userId ? "Confirm" : "Remove"
                }}</Button
              >
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </AsyncState>
</template>
