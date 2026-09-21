<script setup lang="ts">
import { ClipboardList, ListChecks, Rocket, Target } from "@lucide/vue";
import ViewTab from "../../shared/ViewTab.vue";
import ViewTabs from "../../shared/ViewTabs.vue";
// The four screens of the Test Management Plane (§6.8) share one switcher.
defineProps<{
  project: string;
  current: "cases" | "requirements" | "plans" | "releases";
}>();
const base = (p: string) => `/p/${encodeURIComponent(p)}`;
</script>
<template>
  <ViewTabs label="Test management views">
    <ViewTab :to="`${base(project)}/cases`" :selected="current === 'cases'"
      ><ClipboardList />Test cases</ViewTab
    >
    <ViewTab
      :to="`${base(project)}/requirements`"
      :selected="current === 'requirements'"
      ><Target />Requirements</ViewTab
    >
    <ViewTab :to="`${base(project)}/plans`" :selected="current === 'plans'"
      ><ListChecks />Test plans</ViewTab
    >
    <ViewTab
      :to="`${base(project)}/releases`"
      :selected="current === 'releases'"
      ><Rocket />Releases</ViewTab
    >
  </ViewTabs>
</template>
