<script setup lang="ts">
import { FolderGit2, ArrowRight, RefreshCw } from "@lucide/vue";
import AsyncState from "../../shared/AsyncState.vue";
import { useProjects } from "./useProjects";
const { data, error, loading, cursor, refresh, next, first } = useProjects();
</script>
<template>
  <header class="page-heading">
    <div>
      <p class="eyebrow">WORKSPACE</p>
      <h1>Projects</h1>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh projects"
      title="Refresh projects"
      @click="refresh"
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <AsyncState :loading="loading" :error="error" @retry="refresh"
    ><div v-if="!data?.items.length" class="empty-state">
      <FolderGit2 :size="32" />
      <h2>No projects available</h2>
      <p>Contact your workspace administrator for project access.</p>
    </div>
    <div v-else class="project-list">
      <RouterLink
        v-for="project in data.items"
        :key="project.id"
        :to="`/p/${encodeURIComponent(project.id)}/runs`"
        class="project-row"
        ><FolderGit2 :size="24" />
        <div>
          <strong>{{ project.name }}</strong
          ><span>{{ project.workspaceName }}</span>
        </div>
        <ArrowRight :size="18"
      /></RouterLink>
    </div>
    <footer class="pagination">
      <button class="button" :disabled="!cursor" @click="first">
        First page</button
      ><button class="button" :disabled="!data?.nextCursor" @click="next">
        Next page
      </button>
    </footer></AsyncState
  >
</template>
