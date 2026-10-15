<script setup lang="ts">
import {
  ClipboardList,
  ListChecks,
  Rocket,
  Target,
  ChartNoAxesCombined,
} from "@lucide/vue";
import ViewTab from "../../shared/ViewTab.vue";
import ViewTabs from "../../shared/ViewTabs.vue";
// The Test Management Plane screens (§6.8) share one switcher.
defineProps<{
  project: string;
  current: "cases" | "requirements" | "plans" | "releases" | "specialized";
}>();
const base = (p: string) => `/p/${encodeURIComponent(p)}`;
</script>
<template>
  <ViewTabs label="Test management views">
    <ViewTab
      :to="`${base(project)}/specialized-results`"
      :selected="current === 'specialized'"
      ><ChartNoAxesCombined />Specialized results</ViewTab
    >
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
