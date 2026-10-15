<script setup lang="ts">
import {
  Activity,
  ChevronsUpDown,
  ClipboardList,
  FolderGit2,
  Gauge,
  GitBranch,
  KeyRound,
  LogOut,
  Scale,
  ShieldCheck,
  UserCog,
  Users,
} from "@lucide/vue";
import { computed, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "./providers/services";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
// One nav, rendered in the desktop rail and inside the mobile sheet.
const props = defineProps<{ project: string; busy: boolean }>();
const emit = defineEmits<{ navigate: []; logout: [] }>();
const { session, projectAccess, teamAccess } = useServices();
const route = useRoute();
// "Team" only for admins of an organization or workspace; asked again on
// navigation and when the MFA flags change, until it answers (an owner is
// refused until enrolled). Never once signed out: the sidebar outlives the
// session for a tick, and an anonymous ask is a 401 (CI live check, 28-09).
watch(
  () => [route.path, session.user?.mfa?.enabled],
  () => {
    if (session.user) void teamAccess.ensure();
  },
  { immediate: true },
);
const team = computed(() => teamAccess.scopes.length > 0);
const p = computed(() => encodeURIComponent(props.project));
const items = computed(() =>
  props.project
    ? [
        {
          to: `/p/${p.value}/overview`,
          label: "Overview",
          icon: Gauge,
          active: route.path.endsWith("/overview"),
        },
        {
          to: `/p/${p.value}/roi`,
          label: "ROI",
          icon: Scale,
          active: route.path.endsWith("/roi"),
        },
        {
          to: `/p/${p.value}/runs`,
          label: "Test runs",
          icon: GitBranch,
          active: route.path.includes("/runs"),
        },
        {
          to: `/p/${p.value}/tests`,
          label: "Test intelligence",
          icon: Activity,
          active:
            route.path.includes("/tests") ||
            route.path.includes("/flaky") ||
            route.path.endsWith("/review"),
        },
        {
          to: `/p/${p.value}/cases`,
          label: "Test management",
          icon: ClipboardList,
          active:
            route.path.includes("/cases") ||
            route.path.includes("/requirements") ||
            route.path.includes("/specialized-results") ||
            route.path.includes("/plans") ||
            route.path.includes("/releases"),
        },
        // CI tokens: maintainers and up (§11.3.2 token.manage).
        ...(projectAccess.can(props.project, "token.manage")
          ? [
              {
                to: `/p/${p.value}/tokens`,
                label: "CI tokens",
                icon: KeyRound,
                active: route.path.endsWith("/tokens"),
              },
            ]
          : []),
        // Only for those who may manage members (§11.3.2); others would
        // meet a 403 there.
        ...(projectAccess.can(props.project, "member.manage")
          ? [
              {
                to: `/p/${p.value}/members`,
                label: "Members",
                icon: Users,
                active: route.path.endsWith("/members"),
              },
            ]
          : []),
      ]
    : [],
);
const link =
  "flex h-9 items-center gap-2.5 rounded-md px-2.5 text-sm font-medium transition-colors [&_svg]:size-4 [&_svg]:shrink-0";
const idle =
  "text-muted-foreground hover:bg-accent hover:text-accent-foreground";
const active = "bg-accent text-accent-foreground";
</script>
<template>
  <div class="flex h-full flex-col gap-5 p-3">
    <RouterLink
      to="/"
      class="flex h-10 items-center gap-2.5 rounded-md px-1.5"
      aria-label="ATP projects"
      @click="emit('navigate')"
    >
      <span
        class="bg-primary text-primary-foreground inline-flex size-7 items-center justify-center rounded-md"
        ><Activity :size="16" aria-hidden="true"
      /></span>
      <span class="leading-none">
        <strong class="block text-sm font-semibold tracking-tight">ATP</strong>
        <span class="text-muted-foreground text-[11px]">Report Center</span>
      </span>
    </RouterLink>

    <div class="space-y-1">
      <p
        class="text-muted-foreground px-2.5 text-[11px] font-semibold tracking-[0.12em] uppercase"
      >
        Project
      </p>
      <RouterLink
        to="/"
        :class="
          cn(
            'bg-card hover:bg-accent flex h-9 items-center gap-2 rounded-md border px-2.5 text-sm shadow-xs transition-colors',
          )
        "
        @click="emit('navigate')"
      >
        <FolderGit2 :size="15" class="text-muted-foreground" />
        <span class="min-w-0 flex-1 truncate font-medium">{{
          project || "Choose a project"
        }}</span>
        <ChevronsUpDown :size="14" class="text-muted-foreground" />
      </RouterLink>
    </div>

    <nav aria-label="Main navigation" class="flex flex-col gap-0.5">
      <RouterLink
        to="/"
        :class="cn(link, project ? idle : active)"
        :aria-current="!project ? 'page' : undefined"
        @click="emit('navigate')"
        ><FolderGit2 />Projects</RouterLink
      >
      <RouterLink
        v-if="team"
        to="/team"
        :class="cn(link, route.path.startsWith('/team') ? active : idle)"
        :aria-current="route.path.startsWith('/team') ? 'page' : undefined"
        @click="emit('navigate')"
        ><UserCog />Team</RouterLink
      >
      <RouterLink
        v-for="item in items"
        :key="item.to"
        :to="item.to"
        :class="cn(link, item.active ? active : idle)"
        :aria-current="item.active ? 'page' : undefined"
        @click="emit('navigate')"
        ><component :is="item.icon" />{{ item.label }}</RouterLink
      >
    </nav>

    <div class="mt-auto flex items-center gap-2.5 border-t pt-3">
      <span
        class="bg-card inline-flex size-8 shrink-0 items-center justify-center rounded-full border text-xs font-semibold"
        aria-hidden="true"
        >{{ session.user?.name.slice(0, 1).toUpperCase() }}</span
      >
      <div class="min-w-0 flex-1 leading-tight">
        <strong class="block truncate text-sm font-medium">{{
          session.user?.name
        }}</strong>
        <span class="text-muted-foreground block truncate text-xs">{{
          session.user?.email
        }}</span>
      </div>
      <Button
        as-child
        variant="ghost"
        size="icon-sm"
        :class="
          session.user?.mfa?.required && !session.user?.mfa?.enabled
            ? 'text-warning'
            : ''
        "
      >
        <RouterLink
          to="/account/security"
          aria-label="Account security"
          title="Account security"
          @click="emit('navigate')"
          ><ShieldCheck
        /></RouterLink>
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="Sign out"
        title="Sign out"
        :disabled="busy"
        @click="emit('logout')"
        ><LogOut
      /></Button>
    </div>
  </div>
</template>
