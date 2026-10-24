<script setup lang="ts">
import { RefreshCw, Sparkles, Trash2, UserPlus, Users } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Invitation, Member } from "./membersRepository";
import { grantable, type MemberNode, type Role } from "./roles";
import NewProject from "./NewProject.vue";
import DeleteProject from "./DeleteProject.vue";
import DeletedProjects from "./DeletedProjects.vue";
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
const { members, session, teamAccess, projectAccess } = useServices();
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
// §11.3.2 "Tạo project": workspace admins, org admins and owners.
// §11.3.2 "xóa project": the same admins, on the project's own page.
const canDeleteProject = computed(
  () =>
    node.value.level === "project" &&
    projectAccess.can(node.value.id, "project.manage") &&
    !!projectAccess.name(node.value.id),
);
const canCreateProject = computed(
  () =>
    node.value.level === "workspace" &&
    !!scope.value?.actions.includes("project.manage"),
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
  sent.value = "";
  const done = await mutation.run("new", (signal) =>
    members.set(node.value, email, role, signal),
  );
  if (done) {
    draft.value = { email: "", role };
    if (done.kind === "invited") {
      sent.value = done.invitation.email;
      await refreshInvites();
    } else await refresh();
  }
}

// Invitations (mail on, §11.2): an email with no account is invited; the
// open ones are listed under the members and can be withdrawn.
const sent = ref("");
const invites = useResource<Invitation[]>();
function refreshInvites() {
  if (!session.invitations || !canManage.value) return Promise.resolve();
  return invites.load((signal) => members.invitations(node.value, signal));
}
watch([node, canManage], refreshInvites, { immediate: true });
async function withdraw(i: Invitation) {
  if (confirming.value !== i.id) {
    confirming.value = i.id;
    return;
  }
  confirming.value = null;
  const done = await mutation.run(i.id, async (signal) => {
    await members.revokeInvitation(node.value, i.id, signal);
    return true;
  });
  if (done) await refreshInvites();
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
    <!-- QĐ-15: the organization's own AI key and model, for its admins. -->
    <Button
      v-if="node.level === 'organization' && canManage"
      as-child
      variant="outline"
      size="sm"
      ><RouterLink :to="`/team/organizations/${encodeURIComponent(node.id)}/ai`"
        ><Sparkles />AI settings</RouterLink
      ></Button
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

  <NewProject v-if="canCreateProject" :workspace="node.id" />

  <form
    v-if="canManage && offered.length"
    class="member-form bg-card mb-5 grid gap-4 rounded-lg border p-4 sm:grid-cols-[minmax(0,1fr)_11rem_auto] sm:items-end"
    @submit.prevent="add"
  >
    <div class="space-y-2">
      <Label for="member-email">{{
        session.invitations ? "Email" : "Email of an existing account"
      }}</Label>
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
      <template v-if="session.invitations">
        Someone without an account gets an email invitation, valid seven days.
      </template>
    </p>
    <p
      v-if="sent"
      class="invite-sent bg-success-soft text-success border-success/30 rounded-md border px-3 py-2 text-sm sm:col-span-3"
      role="status"
    >
      Invitation sent to <strong class="font-medium">{{ sent }}</strong
      >. They join when they open the link and choose a password.
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

  <section
    v-if="session.invitations && canManage && invites.data.value?.length"
    class="pending-invitations mt-6"
    aria-labelledby="pending-title"
  >
    <h2 id="pending-title" class="mb-2 text-sm font-semibold">
      Pending invitations
    </h2>
    <ul class="bg-card divide-y rounded-lg border">
      <li
        v-for="i in invites.data.value"
        :key="i.id"
        class="flex flex-wrap items-center gap-3 px-4 py-2.5"
      >
        <span class="min-w-0 flex-1">
          <span class="block text-sm font-medium">{{ i.email }}</span>
          <span class="text-muted-foreground block text-xs">
            {{ label(i.role) }} · invited by {{ i.invitedBy }} · expires
            {{ time(i.expiresAt) }}
          </span>
          <ErrorNote
            v-if="mutation.error.value?.key === i.id"
            class="mt-2"
            :error="mutation.error.value.error"
          />
        </span>
        <Button
          v-if="i.role !== 'org_owner' || canOwners"
          :variant="confirming === i.id ? 'destructive' : 'ghost'"
          size="xs"
          :disabled="mutation.busy.value !== null"
          :aria-label="
            confirming === i.id
              ? `Confirm withdrawing the invitation to ${i.email}`
              : `Withdraw the invitation to ${i.email}`
          "
          @click="withdraw(i)"
          @blur="confirming = null"
          >{{ confirming === i.id ? "Confirm" : "Withdraw" }}</Button
        >
      </li>
    </ul>
  </section>
  <DeletedProjects v-if="canCreateProject" :workspace="node.id" />
  <DeleteProject
    v-if="canDeleteProject"
    :project="node.id"
    :name="projectAccess.name(node.id)"
  />
</template>
