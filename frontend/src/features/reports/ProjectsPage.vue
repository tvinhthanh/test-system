<script setup lang="ts">
import { FolderGit2, ArrowRight, RefreshCw } from "@lucide/vue";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import { Button } from "@/components/ui/button";
import { useProjects } from "./useProjects";
const { data, error, loading, cursor, refresh, next, first } = useProjects();
</script>
<template>
  <PageHeader
    eyebrow="Workspace"
    title="Projects"
    description="Every project you can read. Pick one to see its runs."
  >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh projects"
      title="Refresh projects"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <AsyncState :loading="loading" :error="error" :rows="4" @retry="refresh">
    <EmptyState
      v-if="!data?.items.length"
      :icon="FolderGit2"
      title="No projects available"
      description="Contact your workspace administrator for project access."
    />
    <ul v-else class="bg-card divide-y overflow-hidden rounded-lg border">
      <li v-for="project in data.items" :key="project.id">
        <RouterLink
          :to="`/p/${encodeURIComponent(project.id)}/runs`"
          class="hover:bg-accent focus-visible:bg-accent group flex items-center gap-4 px-4 py-3.5 transition-colors outline-none"
        >
          <span
            class="bg-muted text-muted-foreground group-hover:text-foreground inline-flex size-9 shrink-0 items-center justify-center rounded-md transition-colors"
            ><FolderGit2 :size="18" aria-hidden="true"
          /></span>
          <span class="min-w-0 flex-1 leading-tight">
            <strong class="block truncate text-sm font-medium">{{
              project.name
            }}</strong>
            <span class="text-muted-foreground block truncate text-xs">{{
              project.workspaceName
            }}</span>
          </span>
          <ArrowRight
            :size="16"
            aria-hidden="true"
            class="text-muted-foreground transition-transform group-hover:translate-x-0.5"
          />
        </RouterLink>
      </li>
    </ul>
    <footer
      v-if="cursor || data?.nextCursor"
      class="mt-3 flex items-center justify-end gap-2"
    >
      <Button variant="outline" size="sm" :disabled="!cursor" @click="first"
        >First page</Button
      >
      <Button
        variant="outline"
        size="sm"
        :disabled="!data?.nextCursor"
        @click="next"
        >Next page</Button
      >
    </footer>
  </AsyncState>
</template>
