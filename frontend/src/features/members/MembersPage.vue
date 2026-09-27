<script setup lang="ts">
import { RefreshCw, Trash2, UserPlus, Users } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Member } from "./membersRepository";
import { grantable, type MemberNode, type Role } from "./roles";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { useProjectPermission } from "../reports/useProjectPermission";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
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

// One screen for the members of a project, a workspace or an organization
// (§11.3.2 "Quản lý member (tầng tương ứng)"); the route says which.
const route = useRoute();
const { members, session, teamAccess } = useServices();
const node = computed<MemberNode>(() =>
  route.params.organization
    ? { level: "organization", id: String(route.params.organization) }
    : route.params.workspace
      ? { level: "workspace", id: String(route.params.workspace) }
      : { level: "project", id: String(route.params.project) },
);
const scope = computed(() =>
  node.value.level === "project"
    ? undefined
    : teamAccess.find(node.value.level, node.value.id),
);
// The server answers 403 either way; these only decide what to offer.
const projectManage = useProjectPermission("member.manage");
const canManage = computed(() =>
  node.value.level === "project"
    ? projectManage.value
    : !!scope.value?.actions.includes("member.manage"),
);
const canOwners = computed(() => !!scope.value?.actions.includes("org.manage"));
const offered = computed(() =>
  grantable[node.value.level].filter(
    (r) => r !== "org_owner" || canOwners.value,
  ),
);
void teamAccess.ensure();

const list = useResource<Member[]>();
function refresh() {
  return list.load((signal) => members.all(node.value, signal));
}
watch(node, refresh, { immediate: true });

const heading = computed(() => {
  switch (node.value.level) {
    case "organization":
      return {
        eyebrow: "Organization",
        title: scope.value ? `${scope.value.name} members` : "Members",
        description:
          "Organization admins manage workspaces, members and tokens everywhere in it; owners also hold billing and the owner role. Owners must use two-factor authentication.",
      };
    case "workspace":
      return {
        eyebrow: scope.value
          ? `Workspace · ${scope.value.organizationName}`
          : "Workspace",
        title: scope.value ? `${scope.value.name} members` : "Members",
        description:
          "Workspace admins manage the projects and members in this workspace. Organization roles apply here too and are changed on the organization.",
      };
    default:
      return {
        eyebrow: "Project",
        title: "Members",
        description:
          "Who can open this project, and with which role. Roles held on the workspace or organization apply here too and are changed at their own level.",
      };
  }
});
const roleHelp: Record<Role, string> = {
  org_owner:
    "everything, including billing and other owners; two-factor authentication required",
  org_admin: "workspaces, members and tokens across the organization",
  workspace_admin: "projects and members in this workspace",
  maintainer: "tokens, retention, identity merge, release decisions",
  member: "triage and test management",
  viewer: "read only",
};
const label = (s: string) => s.replaceAll("_", " ");
// A row is changed here only when its role lives on this node, and an
// owner's row only by an owner.
const editable = (m: Member) =>
  canManage.value &&
  m.level === node.value.level &&
  (m.role !== "org_owner" || canOwners.value);

const draft = ref({ email: "", role: "" as Role | "" });
watch(
  offered,
  (roles) => {
    if (!roles.some((r) => r === draft.value.role))
      draft.value.role = roles.includes("member") ? "member" : (roles[0] ?? "");
  },
  { immediate: true },
);
const mutation = useMutation();
async function add() {
  const role = draft.value.role;
  if (!role) return;
  const email = draft.value.email.trim();
  const done = await mutation.run("new", (signal) =>
    members.set(node.value, email, role, signal),
  );
  if (done) {
    draft.value = { email: "", role };
    await refresh();
  }
}
async function pick(m: Member, value: string) {
  const role = offered.value.find((r) => r === value);
  if (!role) return;
  await mutation.run(m.userId, (signal) =>
    members.set(node.value, m.email, role, signal),
  );
  // Refresh either way: a refused change must put the select back.
  await refresh();
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
    await members.revoke(node.value, m.userId, signal);
    return true;
  });
  if (done) await refresh();
}
// A row's select offers the roles of this node, plus its current role (an
// owner row seen by an admin is not editable anyway).
const rowRoles = (m: Member) =>
  offered.value.includes(m.role) ? offered.value : [m.role, ...offered.value];
</script>
<template>
  <BackLink v-if="node.level !== 'project'" to="/team">Team</BackLink>
  <PageHeader
    :eyebrow="heading.eyebrow"
    :title="heading.title"
    :description="heading.description"
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
    v-if="canManage && offered.length"
    class="member-form bg-card mb-5 grid gap-4 rounded-lg border p-4 sm:grid-cols-[minmax(0,1fr)_11rem_auto] sm:items-end"
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
        <option v-for="r in offered" :key="r" :value="r">
          {{ label(r) }}
        </option>
      </NativeSelect>
    </div>
    <Button type="submit" size="sm" :disabled="mutation.busy.value !== null"
      ><UserPlus />Add</Button
    >
    <p v-if="draft.role" class="text-muted-foreground text-xs sm:col-span-3">
      {{ label(draft.role) }}: {{ roleHelp[draft.role] }}. Adding someone who is
      already here changes their role here.
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
            <TableHead class="w-48">Role</TableHead>
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
                v-if="editable(m)"
                :model-value="m.role"
                class="w-44"
                :disabled="mutation.busy.value !== null"
                :aria-label="`Role of ${m.email}`"
                @update:model-value="pick(m, $event)"
              >
                <option v-for="r in rowRoles(m)" :key="r" :value="r">
                  {{ label(r) }}
                </option>
              </NativeSelect>
              <span v-else class="text-sm">
                {{ label(m.role) }}
                <span
                  v-if="m.level !== node.level"
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
                v-if="editable(m)"
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
