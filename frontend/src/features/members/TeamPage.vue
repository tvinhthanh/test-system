<script setup lang="ts">
import { Building2, ChevronRight, FolderTree, Gauge, Users } from "@lucide/vue";
import { computed, onMounted } from "vue";
import { useServices } from "../../app/providers/services";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import { Badge } from "@/components/ui/badge";

// The organizations and workspaces whose members you manage (§11.3.2),
// each linking to its members screen. Project members stay on the project.
const { teamAccess } = useServices();
onMounted(() => void teamAccess.ensure());
const scopes = computed(() => teamAccess.scopes);
const href = (s: (typeof scopes.value)[number]) =>
  `/team/${s.level === "organization" ? "organizations" : "workspaces"}/${encodeURIComponent(s.id)}`;
</script>
<template>
  <PageHeader
    eyebrow="Administration"
    title="Team"
    description="The organizations and workspaces whose members you manage. Project roles are managed from each project's Members page."
  />
  <EmptyState
    v-if="!scopes.length"
    :icon="Users"
    title="Nothing to manage here"
    description="You are not an admin of any organization or workspace."
  />
  <ul v-else class="team-scopes bg-card divide-y rounded-lg border">
    <li v-for="s in scopes" :key="s.id" class="flex items-center">
      <RouterLink
        :to="href(s)"
        class="hover:bg-accent flex min-w-0 flex-1 items-center gap-3 px-4 py-3 transition-colors"
      >
        <component
          :is="s.level === 'organization' ? Building2 : FolderTree"
          :size="18"
          class="text-muted-foreground shrink-0"
          aria-hidden="true"
        />
        <span class="min-w-0 flex-1">
          <span class="block truncate font-medium">{{ s.name }}</span>
          <span class="text-muted-foreground block truncate text-xs">
            {{
              s.level === "organization"
                ? "Organization"
                : `Workspace in ${s.organizationName}`
            }}
          </span>
        </span>
        <Badge v-if="s.actions.includes('org.manage')" variant="outline"
          >Owner</Badge
        >
        <ChevronRight
          :size="16"
          class="text-muted-foreground"
          aria-hidden="true"
        />
      </RouterLink>
      <!-- The §10.6 shadow meter: billing is the owner's (§11.3.1). -->
      <RouterLink
        v-if="s.level === 'organization' && s.actions.includes('org.manage')"
        :to="`${href(s)}/usage`"
        class="text-muted-foreground hover:text-foreground team-usage inline-flex items-center gap-1.5 px-4 text-sm"
        ><Gauge :size="15" aria-hidden="true" />Usage</RouterLink
      >
    </li>
  </ul>
</template>
