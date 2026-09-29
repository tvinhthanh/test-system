<script setup lang="ts">
// Deleted runs still in their restore window (§11.3.2), for those who may
// delete them: who, when, until when, Restore.
import { ArchiveRestore } from "@lucide/vue";
import { onMounted } from "vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { DeletedRun } from "./reportsRepository";
import { day, short, time } from "../../shared/format";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { Button } from "@/components/ui/button";

const props = defineProps<{ project: string }>();
const emit = defineEmits<{ restored: [] }>();
const { reports } = useServices();
const list = useResource<DeletedRun[]>();
const refresh = () =>
  list.load((signal) => reports.deletedRuns(props.project, signal));
onMounted(refresh);
const mutation = useMutation();

async function restore(r: DeletedRun) {
  const done = await mutation.run(r.id, async (signal) => {
    await reports.restoreRun(props.project, r.id, signal);
    return true;
  });
  if (!done) return;
  await refresh();
  emit("restored");
}
</script>
<template>
  <section
    v-if="list.data.value?.length"
    class="deleted-runs bg-card mt-6 space-y-3 rounded-lg border p-5"
    aria-labelledby="deleted-runs-title"
  >
    <h2 id="deleted-runs-title" class="font-semibold">Recently deleted runs</h2>
    <ul class="divide-y">
      <li
        v-for="r in list.data.value"
        :key="r.id"
        class="flex flex-wrap items-center gap-3 py-2"
      >
        <span class="min-w-0 flex-1">
          <span class="block font-medium"
            >{{ r.branch || "(no branch)" }}
            <code class="text-muted-foreground text-xs">{{
              short(r.id)
            }}</code></span
          >
          <span class="text-muted-foreground block text-xs"
            >Started {{ time(r.startedAt) }} · deleted {{ day(r.deletedAt) }} by
            {{ r.deletedBy }} · removed for good on
            {{ day(r.purgeAfter) }}</span
          >
        </span>
        <Button
          variant="outline"
          size="sm"
          :disabled="mutation.busy.value === r.id"
          @click="restore(r)"
          ><ArchiveRestore />Restore</Button
        >
        <ErrorNote
          v-if="mutation.error.value?.key === r.id"
          class="w-full"
          :error="mutation.error.value.error"
        />
      </li>
    </ul>
  </section>
</template>
