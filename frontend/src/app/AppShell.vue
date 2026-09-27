<script setup lang="ts">
import { ChevronRight, Menu } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "./providers/services";
import { ApiError } from "../core/api/client";
import SidebarNav from "./SidebarNav.vue";
import ThemeToggle from "./ThemeToggle.vue";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
const { session, fixture, projectAccess } = useServices();
const route = useRoute(),
  router = useRouter();
const project = computed(() =>
  typeof route.params.project === "string" ? route.params.project : "",
);
const section = computed(() => {
  const path = route.path;
  if (path.endsWith("/overview")) return "Overview";
  if (path.includes("/runs")) return "Test runs";
  if (path.endsWith("/review")) return "Review queue";
  if (path.includes("/cases")) return "Test cases";
  if (path.includes("/requirements")) return "Requirements";
  if (path.includes("/plans")) return "Test plans";
  if (path.includes("/releases")) return "Releases";
  if (path.includes("/flaky")) return "Flaky tests";
  if (path.includes("/tests")) return "Test intelligence";
  if (path.endsWith("/members")) return "Members";
  if (path === "/account/security") return "Security";
  if (path.startsWith("/team")) return "Team";
  return "";
});
const menu = ref(false);
watch(project, (id) => void projectAccess.ensure(id).catch(() => undefined), {
  immediate: true,
});
async function logout() {
  menu.value = false;
  if (await session.logout()) await router.replace("/sign-in");
}
</script>
<template>
  <div
    class="bg-background min-h-svh lg:grid lg:grid-cols-[240px_minmax(0,1fr)]"
  >
    <a
      href="#main"
      class="bg-card focus:ring-ring fixed top-3 left-3 z-50 -translate-y-24 rounded-md border px-3 py-2 text-sm shadow transition focus:translate-y-0 focus:ring-2"
      >Skip to content</a
    >

    <!-- Desktop rail -->
    <aside
      class="bg-sidebar text-sidebar-foreground border-sidebar-border sticky top-0 hidden h-svh border-r lg:block"
    >
      <SidebarNav :project="project" :busy="session.busy" @logout="logout" />
    </aside>

    <!-- Mobile drawer: same nav, focus-trapped -->
    <Sheet v-model:open="menu">
      <SheetContent side="left" class="w-72 p-0">
        <SheetTitle class="sr-only">Navigation</SheetTitle>
        <SidebarNav
          :project="project"
          :busy="session.busy"
          @navigate="menu = false"
          @logout="logout"
        />
      </SheetContent>
    </Sheet>

    <div class="flex min-w-0 flex-col">
      <header
        class="bg-background/85 supports-backdrop-filter:bg-background/70 sticky top-0 z-30 flex h-14 items-center gap-2 border-b px-4 backdrop-blur lg:px-8"
      >
        <Button
          variant="ghost"
          size="icon-sm"
          class="lg:hidden"
          aria-label="Open navigation"
          @click="menu = true"
          ><Menu
        /></Button>
        <nav
          aria-label="Breadcrumb"
          class="text-muted-foreground flex min-w-0 items-center gap-1 text-sm"
        >
          <RouterLink to="/" class="hover:text-foreground transition-colors"
            >Workspace</RouterLink
          >
          <template v-if="project">
            <ChevronRight :size="14" aria-hidden="true" />
            <RouterLink
              :to="`/p/${encodeURIComponent(project)}/runs`"
              class="text-foreground truncate font-medium"
              >{{ project }}</RouterLink
            >
          </template>
          <template v-if="section">
            <ChevronRight :size="14" aria-hidden="true" />
            <span class="truncate" aria-current="page">{{ section }}</span>
          </template>
        </nav>
        <div class="ml-auto flex items-center gap-2">
          <Badge
            v-if="fixture"
            variant="outline"
            class="border-warning/40 bg-warning-soft text-warning hidden sm:inline-flex"
            >Fixture preview</Badge
          >
          <ThemeToggle />
        </div>
      </header>

      <p
        v-if="session.error"
        class="bg-danger-soft text-danger border-danger/20 mx-4 mt-4 rounded-md border px-3 py-2 text-sm lg:mx-8"
        role="alert"
      >
        {{
          session.error instanceof ApiError
            ? session.error.message
            : "The action could not be completed."
        }}
      </p>

      <main
        id="main"
        tabindex="-1"
        class="mx-auto w-full max-w-6xl flex-1 px-4 py-6 outline-none lg:px-8 lg:py-8"
      >
        <!-- Keyed so a user or project switch remounts the page; v-if so that
             sign-out (user → null, key changes) does not remount a protected
             page for an instant and fire unauthenticated fetches before the
             router reaches /sign-in (live-check found two 401s here). -->
        <RouterView
          v-if="session.user"
          :key="`${session.user.id}:${project}`"
        />
      </main>

      <footer
        class="text-muted-foreground flex items-center justify-between border-t px-4 py-3 text-xs lg:px-8"
      >
        <span>ATP / Report Center</span><span>P1-min</span>
      </footer>
    </div>
  </div>
</template>
