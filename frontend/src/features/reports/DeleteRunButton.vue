<script setup lang="ts">
// §11.3.2 "Xóa dữ liệu run/artifact" (owner, org admin, maintainer): the
// run leaves every list, trend and flaky rate; it can be restored from the
// Runs page for 30 days. A second click confirms.
import { Trash2 } from "@lucide/vue";
import { ref } from "vue";
import { useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { Button } from "@/components/ui/button";

const props = defineProps<{ project: string; run: string }>();
const { reports } = useServices();
const router = useRouter();
const confirming = ref(false);
const mutation = useMutation();

async function remove() {
  if (!confirming.value) {
    confirming.value = true;
    return;
  }
  const done = await mutation.run("delete", (signal) =>
    reports.deleteRun(props.project, props.run, signal),
  );
  confirming.value = false;
  if (done) await router.push(`/p/${encodeURIComponent(props.project)}/runs`);
}
</script>
<template>
  <Button
    class="delete-run"
    :variant="confirming ? 'destructive' : 'outline'"
    size="sm"
    :disabled="!!mutation.busy.value"
    @click="remove"
    ><Trash2 />{{ confirming ? "Confirm delete" : "Delete run" }}</Button
  >
  <ErrorNote v-if="mutation.error.value" :error="mutation.error.value.error" />
</template>
