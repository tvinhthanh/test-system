<script setup lang="ts">
// The shadow meter (SPEC §10.6, QĐ-13) for an organization's owner: what a
// bill would count, day by day, and whether the active-test count is
// trustworthy enough to bill by. Nothing here is charged.
import { computed, watch } from "vue";
import { useRoute } from "vue-router";
import { CircleAlert, CircleCheck, Gauge } from "@lucide/vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Meter } from "./meterRepository";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import StatTile from "../../shared/StatTile.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const route = useRoute();
const { meter: repo } = useServices();
const org = computed(() => String(route.params.organization ?? ""));
const meter = useResource<Meter>();
const refresh = () => meter.load((signal) => repo.meter(org.value, 35, signal));
watch(org, refresh, { immediate: true });

const data = computed(() => meter.data.value);
const latest = computed(() => data.value?.days.at(-1) ?? null);
const newestFirst = computed(() => [...(data.value?.days ?? [])].reverse());
const bytes = (n: number) =>
  n < 1 << 20
    ? `${Math.round(n / 1024)} KB`
    : n < 1 << 30
      ? `${(n / (1 << 20)).toFixed(1)} MB`
      : `${(n / (1 << 30)).toFixed(2)} GB`;
const pct = (v: number) => `${(v * 100).toFixed(1)}%`;
</script>

<template>
  <BackLink to="/team">Team</BackLink>
  <PageHeader
    eyebrow="Administration"
    title="Usage (shadow meter)"
    description="What a bill would count, recorded once a day. Nothing is charged: this is the four-week check that decides whether active tests can be the billing unit."
  />
  <AsyncState
    :loading="meter.loading.value"
    :error="meter.error.value"
    :rows="3"
    @retry="refresh"
  >
    <template v-if="data">
      <Alert class="usage-verdict mb-6">
        <CircleCheck v-if="data.ready" />
        <CircleAlert v-else />
        <AlertTitle>{{
          data.ready
            ? "Active tests are stable enough to bill by"
            : "Not yet a billing meter"
        }}</AlertTitle>
        <AlertDescription>
          <template v-if="data.ready"
            >{{ data.requiredDays }} days recorded and none moved more than
            {{ pct(data.driftLimit) }} away from the raw test count.</template
          >
          <ul v-else class="list-disc pl-4">
            <li v-for="r in data.reasons" :key="r">{{ r }}</li>
          </ul>
        </AlertDescription>
      </Alert>

      <EmptyState
        v-if="!latest"
        :icon="Gauge"
        title="Nothing recorded yet"
        description="The worker records each day after it ends; the first row appears tomorrow."
      />
      <template v-else>
        <section
          class="usage-latest grid grid-cols-2 gap-3 lg:grid-cols-4"
          aria-label="Latest day"
        >
          <StatTile
            label="Active tests"
            :value="latest.activeTests"
            :note="`with a result in the 30 days to ${latest.day}`"
          />
          <StatTile
            label="Raw test fingerprints"
            :value="latest.rawFingerprints"
            note="what reporters sent, before identity"
          />
          <StatTile
            label="Runs that day"
            :value="latest.runs"
            :note="`${latest.results} results`"
          />
          <StatTile
            label="Artifact storage"
            :value="bytes(latest.artifactBytes)"
            note="verified, not expired"
          />
        </section>

        <SectionHeading title="By day" />
        <div class="table-scroll bg-card overflow-x-auto rounded-lg border">
          <Table class="usage-days min-w-[720px]">
            <TableHeader>
              <TableRow class="hover:bg-transparent">
                <TableHead>Day</TableHead>
                <TableHead class="text-right">Active tests</TableHead>
                <TableHead class="text-right">Raw</TableHead>
                <TableHead class="text-right">Identity changes</TableHead>
                <TableHead class="text-right">Drift</TableHead>
                <TableHead class="text-right">Runs</TableHead>
                <TableHead class="text-right">Storage</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="d in newestFirst" :key="d.day">
                <TableCell class="tabular-nums">{{ d.day }}</TableCell>
                <TableCell class="text-right tabular-nums">{{
                  d.activeTests
                }}</TableCell>
                <TableCell class="text-right tabular-nums">{{
                  d.rawFingerprints
                }}</TableCell>
                <TableCell class="text-right tabular-nums">{{
                  d.corrections
                }}</TableCell>
                <TableCell class="text-right tabular-nums">
                  <span :class="d.flagged ? 'text-warning font-medium' : ''"
                    >{{ pct(d.drift)
                    }}<span v-if="d.flagged"> · flagged</span></span
                  >
                </TableCell>
                <TableCell class="text-right tabular-nums">{{
                  d.runs
                }}</TableCell>
                <TableCell class="text-right tabular-nums">{{
                  bytes(d.artifactBytes)
                }}</TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </template>
    </template>
  </AsyncState>
</template>
