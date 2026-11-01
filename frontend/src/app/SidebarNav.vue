<script setup lang="ts">
import {
  Activity,
  BookOpen,
  Bot,
  ChevronsUpDown,
  ClipboardList,
  FolderGit2,
  Gauge,
  GitBranch,
  KeyRound,
  LogOut,
  Play,
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
const projectName = computed(
  () => projectAccess.name(props.project) || props.project,
);
const items = computed(() =>
  props.project
    ? [
        {
          to: `/p/${p.value}/overview`,
          label: "Overview",
          icon: Gauge,
          section: "Results",
          active: route.path.endsWith("/overview"),
        },
        // QĐ-15: the start of a round of testing, for those who can test.
        ...(projectAccess.can(props.project, "testmgmt.write")
          ? [
              {
                to: `/p/${p.value}/start`,
                label: "Start testing",
                icon: Play,
                section: "Work",
                active: route.path.endsWith("/start"),
              },
            ]
          : []),
        // QĐ-16: cases the server runs by itself, site scans, runs from CI.
        {
          to: `/p/${p.value}/auto`,
          label: "Auto test",
          icon: Bot,
          section: "Work",
          active: route.path.includes("/auto"),
        },
        {
          to: `/p/${p.value}/roi`,
          label: "ROI",
          icon: Scale,
          section: "Results",
          active: route.path.endsWith("/roi"),
        },
        {
          to: `/p/${p.value}/runs`,
          label: "Test runs",
          icon: GitBranch,
          section: "Results",
          active:
            route.path.includes("/runs") && !route.path.includes("/auto/"),
        },
        {
          to: `/p/${p.value}/tests`,
          label: "Test intelligence",
          icon: Activity,
          section: "Results",
          active:
            route.path.includes("/tests") ||
            route.path.includes("/flaky") ||
            route.path.endsWith("/review"),
        },
        {
          to: `/p/${p.value}/cases`,
          label: "Test management",
          icon: ClipboardList,
          section: "Work",
          active:
            route.path.includes("/cases") ||
            route.path.includes("/requirements") ||
            route.path.includes("/specialized-results") ||
            route.path.includes("/plans") ||
            route.path.includes("/releases") ||
            route.path.includes("/url-checks"),
        },
        {
          to: `/p/${p.value}/guide`,
          label: "How to test",
          icon: BookOpen,
          section: "Work",
          active: route.path.endsWith("/guide"),
        },
        // CI tokens: maintainers and up (§11.3.2 token.manage).
        ...(projectAccess.can(props.project, "token.manage")
          ? [
              {
                to: `/p/${p.value}/tokens`,
                label: "CI tokens",
                icon: KeyRound,
                section: "Manage",
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
                section: "Manage",
                active: route.path.endsWith("/members"),
              },
            ]
          : []),
      ]
    : [],
);
const groups = computed(() =>
  ["Work", "Results", "Manage"]
    .map((title) => ({
      title,
      links: items.value.filter((item) => item.section === title),
    }))
    .filter((group) => group.links.length),
);
const link =
  "flex h-11 shrink-0 items-center gap-2.5 rounded-md px-2.5 text-sm font-medium transition-colors lg:h-9 [&_svg]:size-4 [&_svg]:shrink-0";
const idle =
  "text-muted-foreground hover:bg-accent hover:text-accent-foreground";
const active = "bg-accent text-accent-foreground";
</script>
<template>
  <div class="flex h-full min-h-0 flex-col gap-4 p-3 lg:gap-5">
    <RouterLink
      to="/"
      class="flex h-11 shrink-0 items-center gap-2.5 rounded-md px-1.5 lg:h-10"
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

    <div class="shrink-0 space-y-1">
      <p
        class="text-muted-foreground px-2.5 text-[11px] font-semibold tracking-[0.12em] uppercase"
      >
        Project
      </p>
      <RouterLink
        to="/"
        :class="
          cn(
            'bg-card hover:bg-accent flex h-11 items-center gap-2 rounded-md border px-2.5 text-sm shadow-xs transition-colors lg:h-9',
          )
        "
        @click="emit('navigate')"
      >
        <FolderGit2 :size="15" class="text-muted-foreground" />
        <span
          class="min-w-0 flex-1 truncate font-medium"
          :title="projectName"
          >{{ projectName || "Choose a project" }}</span
        >
        <ChevronsUpDown :size="14" class="text-muted-foreground" />
      </RouterLink>
    </div>

    <nav
      aria-label="Main navigation"
      class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1"
    >
      <div class="shrink-0 space-y-0.5">
        <p
          class="text-muted-foreground px-2.5 pb-1 text-[10px] font-semibold tracking-[0.12em] uppercase"
        >
          Workspace
        </p>
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
      </div>
      <div
        v-for="group in groups"
        :key="group.title"
        class="shrink-0 space-y-0.5"
      >
        <p
          class="text-muted-foreground px-2.5 pb-1 text-[10px] font-semibold tracking-[0.12em] uppercase"
        >
          {{ group.title }}
        </p>
        <RouterLink
          v-for="item in group.links"
          :key="item.to"
          :to="item.to"
          :class="cn(link, item.active ? active : idle)"
          :aria-current="item.active ? 'page' : undefined"
          @click="emit('navigate')"
          ><component :is="item.icon" />{{ item.label }}</RouterLink
        >
      </div>
    </nav>

    <div class="flex shrink-0 items-center gap-2.5 border-t pt-3">
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
