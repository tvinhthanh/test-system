<script setup lang="ts">
import {
  Check,
  CircleCheck,
  CircleDashed,
  Play,
  RefreshCw,
  Rocket,
  ShieldAlert,
  ShieldBan,
  TriangleAlert,
} from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { GateRule, Release } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
import ErrorNote from "./ErrorNote.vue";
import GateBadge from "./GateBadge.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const canWrite = useProjectPermission("testmgmt.write");
const canDecide = useProjectPermission("release.decide");
const releaseId = computed(() => String(route.params.release));
const base = () => `/p/${encodeURIComponent(project.value)}`;
const release = useResource<Release>();
function refresh() {
  return release.load((signal) =>
    testmgmt.release(project.value, releaseId.value, signal),
  );
}
watch(() => [project.value, releaseId.value], refresh, { immediate: true });

const mutation = useMutation();
async function evaluate() {
  const r = await mutation.run("evaluate", (signal) =>
    testmgmt.evaluate(project.value, releaseId.value, signal),
  );
  if (r) release.data.value = r;
}
const note = ref("");
async function decide(status: "approved" | "blocked" | "shipped") {
  const r = await mutation.run("decide", (signal) =>
    testmgmt.decide(
      project.value,
      releaseId.value,
      { status, note: note.value },
      signal,
    ),
  );
  if (r) {
    note.value = "";
    release.data.value = r;
  }
}
const gate = computed(() => release.data.value?.gate ?? null);
// An approved/shipped decision against a blocked gate is an override — shown
// as such, with the name (§6.8.4 #1).
const override = computed(
  () =>
    !!release.data.value &&
    gate.value?.verdict === "blocked" &&
    (release.data.value.status === "approved" ||
      release.data.value.status === "shipped"),
);
const ruleLabel: Record<string, string> = {
  critical_regression: "Critical flows",
  untriaged_failures: "Untriaged failures",
  flaky_rate: "Suite flaky rate",
  p0_coverage: "p0 requirement coverage",
  prod_smoke_approved: "Production smoke approved",
};
const ruleIcon = (r: GateRule) =>
  !r.applies
    ? CircleDashed
    : r.passed
      ? CircleCheck
      : r.level === "blocked"
        ? ShieldAlert
        : TriangleAlert;
const ruleTone = (r: GateRule) =>
  !r.applies
    ? "text-muted-foreground"
    : r.passed
      ? "text-success"
      : r.level === "blocked"
        ? "text-danger"
        : "text-warning";
const ciUrl = computed(() =>
  testmgmt.ciGateUrl(window.location.origin, releaseId.value),
);
const ciSnippet = computed(
  () =>
    `# fails the job unless the release may ship (.ci.reason says why)\n` +
    `curl -fsS -H "Authorization: Bearer $ATP_READ_TOKEN" ${ciUrl.value} \\\n` +
    `  | jq -e '.ci.pass'`,
);
</script>
<template>
  <BackLink :to="`${base()}/releases`">Releases</BackLink>
  <PageHeader
    eyebrow="Release"
    :title="release.data.value?.version ?? releaseId"
  >
    <template #description>
      <span
        v-if="release.data.value"
        class="inline-flex flex-wrap items-center gap-2"
      >
        <GateBadge :gate="gate" />
        <StatusBadge :status="release.data.value.status" />
        <span class="text-muted-foreground text-xs"
          >{{ release.data.value.branch || "any branch" }} · opened
          {{ time(release.data.value.createdAt) }}</span
        >
      </span>
    </template>
    <Button
      v-if="canWrite"
      size="sm"
      :disabled="mutation.busy.value !== null"
      @click="evaluate"
      ><Play />Evaluate gate</Button
    >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh release"
      title="Refresh release"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>

  <AsyncState
    :loading="release.loading.value"
    :error="release.error.value"
    :rows="4"
    @retry="refresh"
  >
    <template v-if="release.data.value">
      <ErrorNote
        v-if="mutation.error.value?.key === 'evaluate'"
        class="mb-4"
        :error="mutation.error.value.error"
      />
      <Alert v-if="override" class="mb-4" role="status">
        <ShieldAlert />
        <AlertTitle>Gate overridden</AlertTitle>
        <AlertDescription>
          The gate said <strong>blocked</strong>;
          {{ release.data.value.decidedBy }} decided
          <strong>{{ release.data.value.status }}</strong> on
          {{ time(release.data.value.decidedAt!) }}.
          <template v-if="release.data.value.decisionNote">
            Note: “{{ release.data.value.decisionNote }}”.</template
          >
        </AlertDescription>
      </Alert>
      <Alert
        v-else-if="release.data.value.decidedBy"
        class="mb-4"
        role="status"
      >
        <Check />
        <AlertTitle>Decided: {{ release.data.value.status }}</AlertTitle>
        <AlertDescription>
          {{ release.data.value.decidedBy }} ·
          {{ time(release.data.value.decidedAt!) }}
          <template v-if="release.data.value.decisionNote">
            — “{{ release.data.value.decisionNote }}”</template
          >
        </AlertDescription>
      </Alert>

      <section aria-labelledby="gate-title">
        <SectionHeading id="gate-title" title="Quality gate">
          <span v-if="gate" class="text-xs"
            >evaluated {{ time(gate.evaluatedAt)
            }}<template v-if="gate.runId">
              · run
              <RouterLink
                class="font-mono hover:underline"
                :to="`${base()}/runs/${encodeURIComponent(gate.runId)}`"
                >{{ gate.runId.slice(0, 8) }}</RouterLink
              ></template
            ></span
          >
        </SectionHeading>
        <div
          v-if="!gate"
          class="empty-state rounded-lg border border-dashed px-6 py-10 text-center"
        >
          <h2 class="text-base font-semibold">Not evaluated yet</h2>
          <p class="text-muted-foreground mt-1 text-sm">
            Evaluate reads the newest finished run and applies the five §6.8.4
            rules. It never decides — you do.
          </p>
          <Button
            v-if="canWrite"
            class="mt-4"
            size="sm"
            :disabled="mutation.busy.value !== null"
            @click="evaluate"
            ><Play />Evaluate gate</Button
          >
        </div>
        <ul
          v-else
          class="gate-rules bg-card divide-y overflow-hidden rounded-lg border"
        >
          <li
            v-for="r in gate.rules"
            :key="r.id"
            class="grid grid-cols-[1.25rem_minmax(0,1fr)_auto] items-start gap-3 px-4 py-3"
            :data-rule="r.id"
            :data-passed="r.passed"
            :data-applies="r.applies"
          >
            <component
              :is="ruleIcon(r)"
              :size="18"
              :class="ruleTone(r)"
              aria-hidden="true"
              class="mt-0.5"
            />
            <div class="min-w-0">
              <strong class="block text-sm font-medium">{{
                ruleLabel[r.id] ?? r.id
              }}</strong>
              <span class="text-muted-foreground block text-sm">{{
                r.summary
              }}</span>
              <details v-if="Object.keys(r.details).length" class="mt-1">
                <summary
                  class="text-muted-foreground cursor-pointer text-xs hover:underline"
                >
                  Details
                </summary>
                <TechnicalBlock
                  class="mt-2"
                  title="Rule details"
                  :text="JSON.stringify(r.details, null, 2)"
                />
              </details>
            </div>
            <span
              class="text-muted-foreground text-xs uppercase tracking-wide"
              :title="r.applies ? `Fails as ${r.level}` : 'Not applicable'"
              >{{ r.applies ? r.level : "n/a" }}</span
            >
          </li>
        </ul>
      </section>

      <section aria-labelledby="decision-title">
        <SectionHeading id="decision-title" title="Decision"
          ><span class="text-xs">admin · recorded by name</span></SectionHeading
        >
        <div
          v-if="release.data.value.status === 'shipped'"
          class="text-muted-foreground bg-card rounded-lg border px-4 py-3 text-sm"
        >
          Shipped by {{ release.data.value.decidedBy }} — final.
        </div>
        <div
          v-else-if="canDecide"
          class="bg-card space-y-3 rounded-lg border p-4"
        >
          <div class="space-y-2">
            <Label for="decision-note">Note (kept with the decision)</Label>
            <Input
              id="decision-note"
              v-model="note"
              maxlength="4000"
              placeholder="Why, especially when overriding the gate"
            />
          </div>
          <div class="flex flex-wrap gap-2">
            <Button
              size="sm"
              :disabled="mutation.busy.value !== null"
              @click="decide('approved')"
              ><Check />Approve</Button
            >
            <Button
              variant="outline"
              size="sm"
              :disabled="mutation.busy.value !== null"
              @click="decide('blocked')"
              ><ShieldBan />Block</Button
            >
            <Button
              variant="outline"
              size="sm"
              :disabled="mutation.busy.value !== null"
              @click="decide('shipped')"
              ><Rocket />Mark shipped</Button
            >
          </div>
          <ErrorNote
            v-if="mutation.error.value?.key === 'decide'"
            :error="mutation.error.value.error"
          />
        </div>
      </section>

      <section aria-labelledby="ci-title">
        <SectionHeading id="ci-title" title="Gate for CI"
          ><span class="text-xs">token with project:read</span></SectionHeading
        >
        <TechnicalBlock title="Deploy step" :text="ciSnippet" />
      </section>
    </template>
  </AsyncState>
</template>
