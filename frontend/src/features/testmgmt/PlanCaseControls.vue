<script setup lang="ts">
import { ref, watch } from "vue";
import { Play, UserRoundCheck } from "@lucide/vue";
import type { PlanCase, PlanCaseUpdate } from "./testmgmtRepository";
import { time } from "../../shared/format";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const props = defineProps<{ item: PlanCase; disabled: boolean }>();
const emit = defineEmits<{ update: [update: PlanCaseUpdate] }>();
const email = ref("");
watch(
  () => props.item,
  (item) => {
    email.value = item.assigneeEmail;
  },
  { immediate: true },
);
</script>

<template>
  <div class="grid min-w-0 gap-2">
    <div class="flex flex-wrap items-center gap-2 text-xs">
      <span>{{
        item.workStatus === "in_progress"
          ? "In progress"
          : item.workStatus === "completed"
            ? "Completed"
            : "Not run"
      }}</span>
      <span v-if="item.assigneeEmail" class="break-all">{{
        item.assigneeEmail
      }}</span>
      <span v-if="item.assignedAt" class="text-muted-foreground"
        >Assigned by {{ item.assignedBy }} · {{ time(item.assignedAt) }}</span
      >
    </div>
    <form
      class="grid min-w-0 gap-2 sm:flex sm:flex-wrap sm:items-center"
      @submit.prevent="
        emit('update', {
          testCaseId: item.testCase.id,
          assigneeEmail: email.trim(),
        })
      "
    >
      <Input
        v-model="email"
        type="email"
        maxlength="254"
        :disabled="disabled"
        :aria-label="`Assignee email for ${item.testCase.title}`"
        placeholder="Assignee email"
        class="w-full min-w-0 sm:flex-1 sm:max-w-xs"
      />
      <div class="flex flex-wrap gap-2">
        <Button
          type="submit"
          variant="outline"
          size="sm"
          :disabled="disabled || email.trim() === item.assigneeEmail"
          ><UserRoundCheck />Assign</Button
        >
        <Button
          v-if="item.workStatus !== 'in_progress'"
          variant="outline"
          size="sm"
          :disabled="disabled || item.testCase.automationStatus === 'obsolete'"
          @click="
            emit('update', {
              testCaseId: item.testCase.id,
              workStatus: 'in_progress',
            })
          "
          ><Play />{{
            item.workStatus === "completed" ? "Start retest" : "Start"
          }}</Button
        >
      </div>
    </form>
  </div>
</template>
