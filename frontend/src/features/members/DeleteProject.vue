<script setup lang="ts">
// §11.3.2 "xóa project" for its admins: soft, restorable from the
// workspace for 30 days, then purged with its runs, artifacts and tokens.
// The project's name is typed back before anything happens.
import { Trash2 } from "@lucide/vue";
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const props = defineProps<{ project: string; name: string }>();
const { members, projectAccess } = useServices();
const router = useRouter();
const typed = ref("");
const mutation = useMutation();
const matches = computed(() => typed.value.trim() === props.name);

async function remove() {
  const out = await mutation.run("delete", (signal) =>
    members.deleteProject(props.project, typed.value.trim(), signal),
  );
  if (!out) return;
  projectAccess.clear();
  await router.push("/");
}
</script>
<template>
  <section
    class="delete-project border-destructive/40 bg-card mt-8 space-y-3 rounded-lg border p-5"
    aria-labelledby="delete-project-title"
  >
    <h2 id="delete-project-title" class="text-destructive font-semibold">
      Delete this project
    </h2>
    <p class="text-muted-foreground text-sm">
      Runs, results, artifacts, test cases and CI tokens go with it. Its CI
      tokens stop working at once. It can be restored from its workspace's Team
      page for 30 days; after that it is removed for good.
    </p>
    <form
      class="grid max-w-lg gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end"
      @submit.prevent="remove"
    >
      <div class="space-y-2">
        <Label for="delete-confirm"
          >Type <strong class="font-mono">{{ name }}</strong> to confirm</Label
        >
        <Input id="delete-confirm" v-model="typed" autocomplete="off" />
      </div>
      <Button
        type="submit"
        variant="destructive"
        :disabled="!matches || !!mutation.busy.value"
        ><Trash2 />Delete project</Button
      >
    </form>
    <ErrorNote
      v-if="mutation.error.value"
      :error="mutation.error.value.error"
    />
  </section>
</template>
