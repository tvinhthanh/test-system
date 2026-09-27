<script setup lang="ts">
import { Calendar, GitMerge, RefreshCw, Split, Undo2 } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import { useResource } from "../../core/state/useResource";
import { useIdentityActions } from "./useIdentityActions";
import { useProjectPermission } from "./useProjectPermission";
import { time, tone } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import StatTile from "../../shared/StatTile.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
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
import { cn } from "@/lib/utils";
const route = useRoute(),
  router = useRouter();
const { reports } = useServices();
const project = computed(() => String(route.params.project));
const identityId = computed(() => String(route.params.identity));
const identity = useResource<Awaited<ReturnType<typeof reports.identity>>>();
const history = useResource<Awaited<ReturnType<typeof reports.runs>>>();
const identityData = computed(() => identity.data.value);
const historyData = computed(() => history.data.value);
const base = () => `/p/${encodeURIComponent(project.value)}`;
function refresh() {
  void identity.load((signal) =>
    reports.identity(project.value, identityId.value, signal),
  );
  void history.load((signal) =>
    reports.identityHistory(project.value, identityId.value, "", signal),
  );
}
refresh();

// §9.4 tools. Admin-only on the server; everyone sees the aliases, the
// actions answer with the server's 403 for anyone else.
const actions = useIdentityActions(project, identityId, refresh);
const canMerge = useProjectPermission("identity.merge");
void actions.loadAliases();
const tools = ref<"" | "merge" | "split">("");
watch(tools, (t) => {
  if (t === "merge" && !actions.candidates.data.value)
    void actions.loadCandidates();
});
const candidateFilter = ref("");
const visibleCandidates = computed(() => {
  const q = candidateFilter.value.trim().toLowerCase();
  return (actions.candidates.data.value ?? []).filter(
    (c) =>
      !q ||
      c.canonicalTitle.toLowerCase().includes(q) ||
      c.canonicalFilePath.toLowerCase().includes(q),
  );
});
async function doSplit() {
  const newId = await actions.split();
  if (newId)
    await router.push(`${base()}/tests/${encodeURIComponent(newId)}/history`);
}
const reason = (e: unknown) =>
  e instanceof ApiError ? e.message : "The action could not be completed.";
</script>
<template>
  <BackLink :to="`${base()}/tests`">Test history</BackLink>
  <PageHeader
    eyebrow="Identity history"
    :title="identityData?.canonicalTitle ?? identityId"
  >
    <template #description>
      <span class="inline-flex flex-wrap items-center gap-2">
        <code v-if="identityData?.canonicalFilePath" class="text-xs">{{
          identityData.canonicalFilePath
        }}</code>
        <StatusBadge v-if="identityData" :status="identityData.verdict" />
      </span>
    </template>
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh identity history"
      title="Refresh identity history"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>

  <AsyncState
    :loading="identity.loading.value"
    :error="identity.error.value"
    :rows="2"
    @retry="refresh"
  >
    <template v-if="identityData">
      <Alert
        v-if="identityData.status === 'merged'"
        class="notice mb-4"
        role="status"
      >
        <GitMerge />
        <AlertTitle>Merged into another test</AlertTitle>
        <AlertDescription class="block">
          Its history lives
          <RouterLink
            v-if="identityData.mergedIntoId"
            class="text-foreground font-medium underline underline-offset-4"
            :to="`${base()}/tests/${encodeURIComponent(identityData.mergedIntoId)}/history`"
            >there</RouterLink
          ><template v-else>there</template> now. Undo puts every fingerprint
          back exactly where it was.
          <Button
            v-if="canMerge"
            variant="outline"
            size="xs"
            class="mt-2"
            :disabled="actions.busy.value !== null"
            @click="actions.undo"
            ><Undo2 />Undo merge</Button
          >
        </AlertDescription>
      </Alert>
      <Alert
        v-else-if="identityData.status === 'retired'"
        class="notice mb-4"
        role="status"
      >
        <Split />
        <AlertTitle>Retired</AlertTitle>
        <AlertDescription>
          This test was split and a newer identity owns its fingerprint now.
          History before the split stays here.
        </AlertDescription>
      </Alert>

      <section
        class="grid grid-cols-2 gap-3 sm:grid-cols-[1fr_1fr_2fr]"
        aria-label="Stability figures"
      >
        <StatTile label="Runs" :value="identityData.totalRuns" />
        <StatTile
          label="Flaky score"
          :value="
            identityData.flakyScore === null
              ? 'n/a'
              : `${Math.round(identityData.flakyScore * 100)}%`
          "
          :note="
            identityData.flakyScore === null ? 'Fewer than 2 runs' : undefined
          "
        />
        <article
          class="bg-card col-span-2 flex flex-col gap-2 rounded-lg border p-4 sm:col-span-1"
        >
          <span class="text-muted-foreground text-xs font-medium"
            >Recent outcomes · oldest → latest</span
          >
          <div
            class="history-large flex h-8 items-end gap-1"
            role="img"
            :aria-label="`Last ${identityData.recentStatuses.length} outcomes: ${identityData.recentStatuses.join(', ')}`"
          >
            <i
              v-for="(status, index) in identityData.recentStatuses"
              :key="`${status}-${index}`"
              :class="cn('h-full flex-1 rounded-sm', `tone-${tone(status)}`)"
              :title="status"
            />
          </div>
        </article>
      </section>
    </template>
  </AsyncState>

  <SectionHeading title="Fingerprints">
    <span class="text-xs"
      >{{ actions.inForce.value.length }} in force ·
      {{
        (actions.aliases.data.value?.length ?? 0) - actions.inForce.value.length
      }}
      historical</span
    >
  </SectionHeading>
  <AsyncState
    :loading="actions.aliases.loading.value"
    :error="actions.aliases.error.value"
    :rows="3"
    @retry="actions.loadAliases"
  >
    <div
      v-if="actions.aliases.data.value?.length"
      class="bg-card overflow-x-auto rounded-lg border"
    >
      <Table class="alias-table">
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead>Fingerprint</TableHead>
            <TableHead>Source</TableHead>
            <TableHead>Match</TableHead>
            <TableHead>From</TableHead>
            <TableHead>To</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="a in actions.aliases.data.value" :key="a.id">
            <TableCell
              ><code class="text-xs">{{ a.fingerprint }}</code></TableCell
            >
            <TableCell class="capitalize">{{
              a.source.replaceAll("_", " ")
            }}</TableCell>
            <TableCell class="font-mono text-xs tabular-nums">{{
              a.source === "exact" ? "—" : `${Math.round(a.matchScore * 100)}%`
            }}</TableCell>
            <TableCell class="text-muted-foreground text-xs">{{
              time(a.validFrom)
            }}</TableCell>
            <TableCell class="text-xs">
              <span v-if="a.validTo" class="text-muted-foreground">{{
                time(a.validTo)
              }}</span>
              <span v-else class="text-success font-medium">in force</span>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    <p v-else class="text-muted-foreground text-sm">No fingerprint recorded.</p>
  </AsyncState>

  <section
    v-if="identityData && identityData.status === 'active'"
    class="tools"
    aria-labelledby="tools-title"
  >
    <SectionHeading v-if="canMerge" id="tools-title" title="Identity tools">
      <span class="text-xs">admin · audited</span>
    </SectionHeading>
    <div
      v-if="canMerge"
      class="flex flex-wrap gap-2"
      role="group"
      aria-label="Tool"
    >
      <Button
        type="button"
        :variant="tools === 'merge' ? 'secondary' : 'outline'"
        size="sm"
        :class="tools === 'merge' && 'selected'"
        :aria-pressed="tools === 'merge'"
        @click="tools = tools === 'merge' ? '' : 'merge'"
        ><GitMerge />Merge into another test</Button
      >
      <Button
        type="button"
        :variant="tools === 'split' ? 'secondary' : 'outline'"
        size="sm"
        :class="tools === 'split' && 'selected'"
        :aria-pressed="tools === 'split'"
        @click="tools = tools === 'split' ? '' : 'split'"
        ><Split />Split from a point in time</Button
      >
    </div>

    <div
      v-if="canMerge && tools === 'merge'"
      class="tool-panel bg-card mt-3 space-y-4 rounded-lg border p-4"
    >
      <p class="text-muted-foreground text-sm">
        Use when the resolver missed a rename: every fingerprint of this
        identity moves to the chosen test and the two histories become one. Undo
        puts it back exactly.
      </p>
      <div class="space-y-2">
        <Label for="candidate-filter">Filter tests</Label>
        <Input
          id="candidate-filter"
          v-model="candidateFilter"
          type="search"
          placeholder="title or file path"
          class="max-w-md"
        />
      </div>
      <AsyncState
        :loading="actions.candidates.loading.value"
        :error="actions.candidates.error.value"
        :rows="4"
        @retry="actions.loadCandidates"
      >
        <ul
          class="candidate-list max-h-80 divide-y overflow-y-auto rounded-md border"
          role="radiogroup"
          aria-label="Merge into"
        >
          <li v-for="c in visibleCandidates.slice(0, 20)" :key="c.id">
            <label
              class="has-checked:bg-accent hover:bg-accent/60 flex cursor-pointer items-start gap-3 px-3 py-2.5 transition-colors"
            >
              <input
                v-model="actions.mergeTarget.value"
                type="radio"
                name="merge-target"
                :value="c.id"
                class="accent-primary mt-1 size-3.5"
              />
              <span class="min-w-0">
                <strong class="block truncate text-sm font-medium">{{
                  c.canonicalTitle
                }}</strong>
                <code class="text-muted-foreground block truncate text-xs">{{
                  c.canonicalFilePath
                }}</code>
              </span>
            </label>
          </li>
          <li
            v-if="!visibleCandidates.length"
            class="text-muted-foreground px-3 py-6 text-center text-sm"
          >
            No other active test matches.
          </li>
        </ul>
      </AsyncState>
      <Button
        type="button"
        size="sm"
        :disabled="!actions.mergeTarget.value || actions.busy.value !== null"
        @click="actions.merge"
        ><GitMerge />Merge this identity into the selected test</Button
      >
    </div>

    <div
      v-if="canMerge && tools === 'split'"
      class="tool-panel bg-card mt-3 space-y-4 rounded-lg border p-4"
    >
      <p class="text-muted-foreground text-sm">
        Use when the same fingerprint started meaning a different test at some
        point: the alias closes there and a new identity owns it from then on.
        History before that point stays here. This does not undo a merge — use
        Undo for that.
      </p>
      <div class="grid gap-4 sm:grid-cols-2">
        <div class="space-y-2">
          <Label for="split-fingerprint">Fingerprint</Label>
          <NativeSelect
            id="split-fingerprint"
            v-model="actions.splitFingerprint.value"
            class="w-full"
          >
            <option value="">Choose a fingerprint in force</option>
            <option
              v-for="a in actions.inForce.value"
              :key="a.id"
              :value="a.fingerprint"
            >
              {{ a.fingerprint }}
            </option>
          </NativeSelect>
        </div>
        <div class="space-y-2">
          <Label for="split-at">Diverged at</Label>
          <Input
            id="split-at"
            v-model="actions.splitAt.value"
            type="datetime-local"
          />
        </div>
      </div>
      <Button
        type="button"
        size="sm"
        :disabled="
          !actions.splitFingerprint.value ||
          !actions.splitAt.value ||
          actions.busy.value !== null
        "
        @click="doSplit"
        ><Split />Split here</Button
      >
    </div>
    <p
      v-if="actions.actionError.value"
      class="bg-danger-soft text-danger border-danger/20 mt-3 rounded-md border px-3 py-2 text-sm"
      role="alert"
    >
      {{ reason(actions.actionError.value) }}
    </p>
  </section>

  <SectionHeading title="Recent runs"><Calendar :size="16" /></SectionHeading>
  <AsyncState
    :loading="history.loading.value"
    :error="history.error.value"
    :rows="4"
    @retry="refresh"
  >
    <EmptyState
      v-if="!historyData?.items.length"
      compact
      description="No matching history runs."
    />
    <ul
      v-else
      class="history-runs bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li v-for="run in historyData.items" :key="run.id" class="event-row">
        <RouterLink
          :to="`${base()}/runs/${encodeURIComponent(run.id)}`"
          class="hover:bg-accent focus-visible:bg-accent flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 text-sm transition-colors outline-none"
        >
          <StatusBadge :status="run.status" :flaky="run.flakyCount" />
          <span class="min-w-0 flex-1 truncate font-medium"
            >{{ run.branch }}
            <span class="text-muted-foreground font-normal"
              >· {{ run.environmentName }}</span
            ></span
          >
          <time
            class="text-muted-foreground text-xs"
            :datetime="run.startedAt"
            >{{ time(run.startedAt) }}</time
          >
        </RouterLink>
      </li>
    </ul>
  </AsyncState>
</template>
