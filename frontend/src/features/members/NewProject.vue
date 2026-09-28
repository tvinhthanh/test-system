<script setup lang="ts">
// §11.3.2 "Tạo project" in this workspace, for its admins (and org
// admins/owners). The next step for a new project is a CI token, so that is
// where it goes.
import { FolderPlus } from "@lucide/vue";
import { ref } from "vue";
import { useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const props = defineProps<{ workspace: string }>();
const { members, projectAccess } = useServices();
const router = useRouter();
const name = ref("");
const mutation = useMutation();

async function create() {
  const out = await mutation.run("create", (signal) =>
    members.createProject(props.workspace, name.value.trim(), signal),
  );
  if (!out) return;
  projectAccess.clear(); // the new project and its actions come with the next list
  await router.push(`/p/${encodeURIComponent(out.id)}/tokens`);
}
</script>
<template>
  <form
    class="new-project bg-card mb-5 grid gap-4 rounded-lg border p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end"
    @submit.prevent="create"
  >
    <div class="space-y-2">
      <Label for="project-name">New project in this workspace</Label>
      <Input
        id="project-name"
        v-model="name"
        maxlength="200"
        placeholder="checkout-web"
        required
      />
    </div>
    <Button type="submit" :disabled="!!mutation.busy.value"
      ><FolderPlus />Create project</Button
    >
    <ErrorNote
      v-if="mutation.error.value"
      class="sm:col-span-2"
      :error="mutation.error.value.error"
    />
  </form>
</template>
