<script setup lang="ts">
import {
  Activity,
  FolderGit2,
  Gauge,
  GitBranch,
  LogOut,
  ChevronRight,
} from "@lucide/vue";
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "./providers/services";
import { ApiError } from "../core/api/client";
const { session, fixture } = useServices();
const route = useRoute(),
  router = useRouter();
const project = computed(() =>
  typeof route.params.project === "string" ? route.params.project : "",
);
async function logout() {
  if (await session.logout()) await router.replace("/sign-in");
}
</script>
<template>
  <div class="app-shell">
    <a href="#main" class="skip-link">Skip to content</a>
    <aside class="sidebar">
      <RouterLink to="/" class="brand" aria-label="ATP projects"
        ><span class="brand-icon"><Activity :size="21" /></span
        ><strong>ATP</strong
        ><span class="brand-subtitle">Report Center</span></RouterLink
      >
      <div class="workspace-label">WORKSPACE</div>
      <RouterLink to="/" class="workspace-switch"
        ><FolderGit2 :size="18" /><span>{{ project || "Choose project" }}</span
        ><ChevronRight :size="16"
      /></RouterLink>
      <nav aria-label="Main navigation">
        <RouterLink to="/" :class="{ active: !project }">
          <FolderGit2 :size="18" />Projects
        </RouterLink>
        <RouterLink
          v-if="project"
          :to="`/p/${encodeURIComponent(project)}/overview`"
          :class="{ active: route.path.endsWith('/overview') }"
        >
          <Gauge :size="18" />Overview
        </RouterLink>
        <RouterLink
          v-if="project"
          :to="`/p/${encodeURIComponent(project)}/runs`"
          :class="{ active: route.path.includes('/runs') }"
        >
          <GitBranch :size="18" />Test runs
        </RouterLink>
        <RouterLink
          v-if="project"
          :to="`/p/${encodeURIComponent(project)}/tests`"
          :class="{
            active:
              route.path.includes('/tests') || route.path.includes('/flaky'),
          }"
        >
          <Activity :size="18" />Test intelligence
        </RouterLink>
      </nav>
      <div class="sidebar-footer">
        <span class="avatar">{{ session.user?.name.slice(0, 1) }}</span>
        <div>
          <strong>{{ session.user?.name }}</strong
          ><span>{{ session.user?.email }}</span>
        </div>
        <button
          class="icon-button"
          aria-label="Sign out"
          title="Sign out"
          :disabled="session.busy"
          @click="logout"
        >
          <LogOut :size="17" />
        </button>
      </div>
    </aside>
    <div class="main-column">
      <header class="topbar">
        <span>Workspace</span><ChevronRight :size="14" /><strong>{{
          project || "Projects"
        }}</strong
        ><span v-if="fixture" class="fixture-label">Fixture preview</span>
      </header>
      <p v-if="session.error" class="error-text shell-error" role="alert">
        {{
          session.error instanceof ApiError
            ? session.error.message
            : "The action could not be completed."
        }}
      </p>
      <main id="main" tabindex="-1">
        <!-- Keyed so a user or project switch remounts the page; v-if so that
             sign-out (user → null, key changes) does not remount a protected
             page for an instant and fire unauthenticated fetches before the
             router reaches /sign-in (live-check found two 401s here). -->
        <RouterView
          v-if="session.user"
          :key="`${session.user.id}:${project}`"
        />
      </main>
      <footer class="app-footer">
        <span>ATP / Report Center</span><span>P1-min</span>
      </footer>
    </div>
  </div>
</template>
