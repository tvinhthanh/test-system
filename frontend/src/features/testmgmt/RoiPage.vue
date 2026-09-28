<script setup lang="ts">
// SPEC §6.8.3 (QĐ-13): what triage on this platform saved, in hours, for
// the person who approves the budget (§2.5). Three rules shape the page:
// coverage and untriaged rate come first (a fast TTRC over a few triaged
// failures is not a saving); hours, and money only from a rate the reader
// types, which stays on this page; no measured baseline → "not enough
// data", never a default.
import { computed } from "vue";
import { CircleAlert, RefreshCw, Scale } from "@lucide/vue";
import {
  currentMonth,
  hours,
  minutesLabel,
  useHourlyRate,
  useRoi,
} from "./useRoi";
import BaselineSheet from "./BaselineSheet.vue";
import { useProjectPermission } from "../reports/useProjectPermission";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import StatTile from "../../shared/StatTile.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const { project, month, setMonth, roi, baselines, refresh } = useRoi();
const canRecord = useProjectPermission("project.configure");
const { rate, money } = useHourlyRate();
const data = computed(() => roi.data.value);
const percent = (v: number) => `${Math.round(v * 100)}%`;
// Money is a note under the measured hours, never the headline figure.
const atRate = (h: number) => (money(h) ? `= ${money(h)} at your rate · ` : "");
</script>

<template>
  <PageHeader
    eyebrow="Engineering overview"
    title="Return on triage"
    description="Hours measured from triage events against a TTRC baseline your team measured. Nothing on this page is estimated."
  >
    <label class="flex items-center gap-2 text-xs font-medium">
      <span class="text-muted-foreground">Month</span>
      <Input
        class="roi-month h-8 w-40"
        type="month"
        :max="currentMonth()"
        :model-value="month"
        @change="setMonth(($event.target as HTMLInputElement).value)"
      />
    </label>
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh"
      title="Refresh"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>

  <AsyncState
    :loading="roi.loading.value"
    :error="roi.error.value"
    :rows="3"
    @retry="refresh"
  >
    <template v-if="data">
      <!-- Rule 1: the denominator first. -->
      <section
        class="roi-coverage mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4"
        aria-label="Triage coverage"
      >
        <StatTile
          label="Failures this month"
          :value="data.failures"
          :note="`${data.triaged} with an accepted root cause`"
        />
        <StatTile
          label="Triage coverage"
          :value="percent(data.triageCoverage)"
          note="failures with a decision"
        />
        <StatTile
          label="Untriaged"
          :value="percent(data.untriagedRate)"
          note="left out of every figure below"
        />
        <StatTile
          label="Median time to root cause"
          :value="minutesLabel(data.ttrcMedianMinutes)"
          :note="
            data.baseline
              ? `baseline ${minutesLabel(data.baseline.medianMinutes)} (${data.baseline.kind})`
              : 'no baseline to compare with'
          "
        />
      </section>

      <Alert v-if="data.fewTriaged" class="roi-few mb-6">
        <CircleAlert />
        <AlertTitle>Few triaged failures</AlertTitle>
        <AlertDescription>
          {{ data.triaged }} this month, under {{ data.minTriaged }}. The
          figures are real but not yet a trend.
        </AlertDescription>
      </Alert>

      <!-- Rule 3: no measured baseline, no ROI. -->
      <EmptyState
        v-if="!data.baseline"
        class="roi-no-baseline"
        :icon="Scale"
        title="Not enough data"
        description="Hours saved need a time-to-root-cause baseline measured before the platform (a study of real failures, SPEC §5.4). Nothing is assumed in its place."
      >
        <BaselineSheet v-if="canRecord" :project="project" @saved="refresh" />
      </EmptyState>

      <template v-else>
        <SectionHeading title="Hours" />
        <section
          class="roi-hours mb-3 grid gap-3 md:grid-cols-2"
          aria-label="Hours"
        >
          <StatTile
            label="Triage hours saved"
            :value="hours(data.savedHours ?? 0)"
            :note="`${atRate(data.savedHours ?? 0)}${data.triaged} triaged × baseline ${minutesLabel(data.baseline.medianMinutes)} − their actual time to root cause`"
          />
          <StatTile
            label="Hours spent on flaky tests"
            :value="hours(data.flakyHours)"
            :note="
              atRate(data.flakyHours) +
              (data.investigationCounted
                ? `${hours(data.flakyRetryHours)} CI reruns + ${data.markedFlaky} marked flaky × ${minutesLabel(data.baseline.flakyInvestigationMinutes)} investigation`
                : `${hours(data.flakyRetryHours)} CI reruns; investigation time not measured in the baseline, so not counted`)
            "
          />
        </section>
        <div class="mb-8 flex flex-wrap items-end gap-3">
          <div class="flex flex-col gap-1.5">
            <Label for="roi-rate" class="text-xs"
              >Your hourly cost (optional)</Label
            >
            <Input
              id="roi-rate"
              v-model="rate"
              class="h-8 w-40"
              type="number"
              min="0"
              inputmode="decimal"
            />
          </div>
          <p class="text-muted-foreground max-w-md text-xs">
            Stays on this page: it is not saved or sent. Hours are the measured
            figure; money is only hours × the rate you type.
          </p>
        </div>
      </template>

      <SectionHeading title="Baselines">
        <BaselineSheet
          v-if="canRecord && data.baseline"
          :project="project"
          @saved="refresh"
        />
      </SectionHeading>
      <p class="text-muted-foreground mb-3 text-xs">
        Newest first. The newest prospective one applies; a retrospective stands
        in until there is one. Entries are never edited — a correction is a new
        entry.
      </p>
      <div
        v-if="baselines.data.value?.length"
        class="table-scroll bg-card overflow-x-auto rounded-lg border"
      >
        <Table class="roi-baselines min-w-[640px]">
          <TableHeader>
            <TableRow class="hover:bg-transparent">
              <TableHead>Kind</TableHead>
              <TableHead>Period</TableHead>
              <TableHead>Failures</TableHead>
              <TableHead>Median / p90</TableHead>
              <TableHead>Rerun-to-pass</TableHead>
              <TableHead>Entered by</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="b in baselines.data.value" :key="b.id">
              <TableCell class="capitalize">{{ b.kind }}</TableCell>
              <TableCell class="text-xs tabular-nums"
                >{{ b.periodFrom }} – {{ b.periodTo }}</TableCell
              >
              <TableCell class="tabular-nums">{{ b.failures }}</TableCell>
              <TableCell class="tabular-nums"
                >{{ minutesLabel(b.medianMinutes) }} /
                {{ minutesLabel(b.p90Minutes) }}</TableCell
              >
              <TableCell class="tabular-nums">{{
                b.rerunPassRate === null ? "—" : percent(b.rerunPassRate)
              }}</TableCell>
              <TableCell class="text-muted-foreground text-xs"
                >{{ b.enteredBy
                }}<span v-if="b.note"> · {{ b.note }}</span></TableCell
              >
            </TableRow>
          </TableBody>
        </Table>
      </div>
      <p v-else class="text-muted-foreground text-sm">None recorded yet.</p>
    </template>
  </AsyncState>
</template>
