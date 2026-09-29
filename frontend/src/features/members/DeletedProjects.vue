<script setup lang="ts">
// A workspace's deleted projects still in their restore window (§11.3.2):
// who deleted each, when it will be purged, and Restore.
import { ArchiveRestore } from "@lucide/vue";
import { onMounted } from "vue";
import { useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { DeletedProject } from "./membersRepository";
import { day } from "../../shared/format";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { Button } from "@/components/ui/button";

const props = defineProps<{ workspace: string }>();
const { members, projectAccess } = useServices();
const router = useRouter();
const list = useResource<DeletedProject[]>();
const refresh = () =>
  list.load((signal) => members.deletedProjects(props.workspace, signal));
onMounted(refresh);
const mutation = useMutation();

async function restore(p: DeletedProject) {
  const out = await mutation.run(p.id, (signal) =>
    members.restoreProject(props.workspace, p.id, signal),
  );
  if (!out) return;
  projectAccess.clear();
  await router.push(`/p/${encodeURIComponent(out.id)}/runs`);
}
</script>
<template>
  <section
    v-if="list.data.value?.length"
    class="deleted-projects bg-card mt-8 space-y-3 rounded-lg border p-5"
    aria-labelledby="deleted-projects-title"
  >
    <h2 id="deleted-projects-title" class="font-semibold">
      Recently deleted projects
    </h2>
    <ul class="divide-y">
      <li
        v-for="p in list.data.value"
        :key="p.id"
        class="flex flex-wrap items-center gap-3 py-2"
      >
        <span class="min-w-0 flex-1">
          <span class="block font-medium">{{ p.name }}</span>
          <span class="text-muted-foreground block text-xs"
            >Deleted {{ day(p.deletedAt) }} by {{ p.deletedBy }} · removed for
            good on {{ day(p.purgeAfter) }}</span
          >
        </span>
        <Button
          variant="outline"
          size="sm"
          :disabled="mutation.busy.value === p.id"
          @click="restore(p)"
          ><ArchiveRestore />Restore</Button
        >
        <ErrorNote
          v-if="mutation.error.value?.key === p.id"
          class="w-full"
          :error="mutation.error.value.error"
        />
      </li>
    </ul>
  </section>
</template>
