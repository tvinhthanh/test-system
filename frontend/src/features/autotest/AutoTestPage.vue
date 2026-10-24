<script setup lang="ts">
// Auto test (QĐ-16): cases the server runs by itself in a real browser on
// an environment's site — written by a tester as steps, or drafted by AI
// from the page. A run records each case in a test plan with screenshots,
// and the whole run in Test runs, so release gates and flaky detection read
// it like CI. Site scans check every page a site links to. CI starts runs
// with a token. Everything here needs the checker service (URLCHECK=on).
import {
  Bot,
  ChevronRight,
  Download,
  KeyRound,
  Pencil,
  Play,
  Plus,
  Radar,
  RefreshCw,
  Sparkles,
} from "@lucide/vue";
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type {
  AutoRun,
  AutoSpec,
  Listing,
  SiteScan,
  TestAccount,
} from "./autotestRepository";
import type { AiStatus, Environment } from "../testmgmt/testmgmtRepository";
import { useMutation } from "../testmgmt/useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { explain } from "../testmgmt/explain";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import EnvironmentPicker from "../testmgmt/EnvironmentPicker.vue";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
import { time } from "../../shared/format";
import AccountsPanel from "./AccountsPanel.vue";
import CaseEditor from "./CaseEditor.vue";
import GeneratePanel from "./GeneratePanel.vue";
import { blankStep, type CaseForm } from "./steps";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute(),
  router = useRouter();
const { autotest, testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const canWrite = useProjectPermission("testmgmt.write");
const base = () => `/p/${encodeURIComponent(project.value)}`;

type Tab = "cases" | "runs" | "scan" | "ci";
const tabs: { id: Tab; label: string }[] = [
  { id: "cases", label: "Cases" },
  { id: "runs", label: "Runs" },
  { id: "scan", label: "Site scan" },
  { id: "ci", label: "Run from CI" },
];
const tab = ref<Tab>(
  (["cases", "runs", "scan", "ci"] as Tab[]).includes(route.query.tab as Tab)
    ? (route.query.tab as Tab)
    : "cases",
);
watch(tab, (t) => void router.replace({ query: { ...route.query, tab: t } }));

const specs = useResource<Listing<AutoSpec>>();
const runs = useResource<Listing<AutoRun>>();
const scans = useResource<Listing<SiteScan>>();
const envs = useResource<Environment[]>();
const ai = useResource<AiStatus>();
const enabled = computed(() => specs.data.value?.enabled !== false);
function refresh() {
  void specs.load((signal) => autotest.specs(project.value, signal));
  void runs.load((signal) => autotest.runs(project.value, signal));
  void scans.load((signal) => autotest.scans(project.value, signal));
}
watch(
  project,
  () => {
    refresh();
    void envs.load((signal) => testmgmt.environments(project.value, signal));
    void ai.load((signal) => testmgmt.aiStatus(project.value, signal));
  },
  { immediate: true },
);

// The site: the first environment with an address, unless one is chosen.
const environmentId = ref("");
watch(
  () => envs.data.value,
  (list) => {
    if (!environmentId.value)
      environmentId.value = list?.find((e) => e.baseUrl)?.id ?? "";
  },
);
const env = computed(() =>
  (envs.data.value ?? []).find((e) => e.id === environmentId.value),
);
const accounts = useResource<TestAccount[]>();
watch(environmentId, (id) => {
  if (!id) return;
  // One just added in the picker: read the list again for its address.
  if (!(envs.data.value ?? []).some((e) => e.id === id))
    void envs.load((signal) => testmgmt.environments(project.value, signal));
  void accounts.load((signal) => autotest.accounts(project.value, id, signal));
});
const accountKeys = ref<string[]>([]);
watch(
  () => accounts.data.value,
  (list) => (accountKeys.value = (list ?? []).map((a) => a.key)),
);
const signedInKeys = computed(() =>
  (accounts.data.value ?? [])
    .filter((a) => a.loginStatus === "ok" && a.loginSteps?.length)
    .map((a) => a.key),
);

// Live lists while something runs.
let timer: ReturnType<typeof setTimeout> | undefined;
watch(
  () => [runs.data.value, scans.data.value],
  () => {
    clearTimeout(timer);
    const busy =
      runs.data.value?.items.some((r) => r.status === "queued" || r.status === "running") ||
      scans.data.value?.items.some((s) => s.status === "running");
    if (busy)
      timer = setTimeout(() => {
        void runs.load((signal) => autotest.runs(project.value, signal));
        void scans.load((signal) => autotest.scans(project.value, signal));
      }, 4000);
  },
);
onBeforeUnmount(() => clearTimeout(timer));

const panel = ref<"" | "accounts" | "generate" | "new">("");
const mutation = useMutation();

// Cases.
const picked = ref(new Set<string>());
const toggle = (id: string) => {
  const next = new Set(picked.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  picked.value = next;
};
const allPicked = computed(
  () => !!specs.data.value?.items.length && specs.data.value.items.every((s) => picked.value.has(s.caseId)),
);
function toggleAll() {
  picked.value = allPicked.value
    ? new Set()
    : new Set(specs.data.value?.items.map((s) => s.caseId));
}
const heal = ref(true);
const blank = (): CaseForm => ({
  title: "",
  priority: "p2",
  testType: "functional",
  target: "/",
  account: "",
  inputs: {},
  steps: [{ ...blankStep(), do: "expect_title", by: "", role: "", value: "" }],
});
const newCase = ref<CaseForm>(blank());
const editing = ref<string | null>(null);
const editForm = shallowRef<CaseForm | null>(null);
function edit(s: AutoSpec) {
  editing.value = s.caseId;
  editForm.value = {
    title: s.title,
    priority: s.priority,
    testType: "",
    target: s.target,
    account: s.account,
    inputs: { ...s.inputs },
    steps: s.steps.map((x) => ({ ...x })),
  };
}
async function saveEdit() {
  const f = editForm.value,
    id = editing.value;
  if (!f || !id) return;
  const saved = await mutation.run(`edit:${id}`, (signal) =>
    autotest.saveSpec(
      project.value,
      id,
      { target: f.target, account: f.account, inputs: f.inputs, steps: f.steps },
      signal,
    ),
  );
  if (saved) {
    editing.value = null;
    refresh();
  }
}
async function createCase() {
  const made = await mutation.run("new", (signal) =>
    autotest.createCase(project.value, { ...newCase.value, source: "manual" }, signal),
  );
  if (made) {
    panel.value = "";
    newCase.value = blank();
    refresh();
  }
}
async function run(caseIds?: string[]) {
  if (!environmentId.value) return;
  const started = await mutation.run("run", (signal) =>
    autotest.createRun(
      project.value,
      { environmentId: environmentId.value, caseIds, heal: heal.value },
      signal,
    ),
  );
  if (started)
    await router.push(`${base()}/auto/runs/${encodeURIComponent(started.id)}`);
}
async function onGenerated(ids: string[], andRun: boolean) {
  panel.value = "";
  refresh();
  if (andRun) await run(ids);
}
async function download() {
  const file = await mutation.run("export", (signal) =>
    autotest.exportSpecs(project.value, environmentId.value, [...picked.value], signal),
  );
  if (!file) return;
  const url = URL.createObjectURL(new Blob([file.content], { type: "text/plain" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = file.filename;
  a.click();
  URL.revokeObjectURL(url);
}

// Site scan.
const scanUrl = ref("");
watch(env, (e) => {
  if (e?.baseUrl && !scanUrl.value) scanUrl.value = e.baseUrl;
});
const scanAccount = ref("");
const scanPages = ref("20");
async function startScan() {
  const sc = await mutation.run("scan", (signal) =>
    autotest.createScan(
      project.value,
      {
        environmentId: environmentId.value,
        url: scanUrl.value.trim(),
        account: scanAccount.value,
        maxPages: Number(scanPages.value),
      },
      signal,
    ),
  );
  if (sc) await router.push(`${base()}/auto/scans/${encodeURIComponent(sc.id)}`);
}

const ciSnippet = computed(() => {
  const key = env.value?.key ?? "staging";
  return [
    "# A token with “Start auto runs” (CI tokens page) in ATP_TOKEN, the server in ATP_URL.",
    `RUN=$(curl -sf -X POST "$ATP_URL/v1/auto-runs" -H "Authorization: Bearer $ATP_TOKEN" \\`,
    `  -H "Content-Type: application/json" -d '{"environment":"${key}"}')`,
    `ID=$(echo "$RUN" | jq -r .id)`,
    `until R=$(curl -sf "$ATP_URL/v1/auto-runs/$ID" -H "Authorization: Bearer $ATP_TOKEN") && [ "$(echo "$R" | jq -r .status)" = done ]; do sleep 10; done`,
    `echo "$R" | jq -r '.cases[] | "\\(.status)\\t\\(.title)"'`,
    `echo "$R" | jq -r .url`,
    `[ "$(echo "$R" | jq '.failed + .errored')" = 0 ]`,
  ].join("\n");
});
const counts = (r: AutoRun) =>
  [
    r.passed && `${r.passed} passed`,
    r.failed && `${r.failed} failed`,
    r.errored && `${r.errored} could not run`,
    r.healed && `${r.healed} repaired by AI`,
  ]
    .filter(Boolean)
    .join(" · ") || `${r.total} case${r.total === 1 ? "" : "s"}`;
</script>
<template>
  <PageHeader
    eyebrow="Project"
    title="Auto test"
    description="Cases the server runs by itself in a real browser on your site — written as steps, or drafted by AI from the page. Results land in a test plan and in Test runs."
  >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh auto test"
      title="Refresh"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>

  <Alert v-if="!enabled" class="mb-5">
    <Bot />
    <AlertTitle>Auto test is off on this server</AlertTitle>
    <AlertDescription>
      Cases run in the checker service. An admin turns it on with
      <code>URLCHECK=on</code> in <code>backend/deploy/.env</code> and runs
      <code>server/atp.sh up</code>. Cases and earlier runs stay readable.
    </AlertDescription>
  </Alert>

  <div class="auto-site bg-card mb-5 flex flex-wrap items-end gap-3 rounded-lg border p-4">
    <EnvironmentPicker
      id="auto-environment"
      v-model="environmentId"
      class="min-w-64 flex-1"
      :project="project"
      :can-write="canWrite"
    />
    <Button
      variant="outline"
      :aria-expanded="panel === 'accounts'"
      @click="panel = panel === 'accounts' ? '' : 'accounts'"
      ><KeyRound />Test accounts ({{ accountKeys.length }})</Button
    >
  </div>
  <p v-if="environmentId && env && !env.baseUrl" class="text-warning mb-4 text-sm" role="status">
    This environment has no address yet — add one above to run cases on it.
  </p>
  <AccountsPanel
    v-if="panel === 'accounts' && environmentId"
    class="mb-5"
    :project="project"
    :environment-id="environmentId"
    :can-write="canWrite"
    :ai-available="!!ai.data.value?.available && enabled"
    @changed="accountKeys = $event; accounts.load((s) => autotest.accounts(project, environmentId, s))"
  />

  <div role="tablist" aria-label="Auto test views" class="mb-4 flex gap-1 border-b">
    <button
      v-for="t in tabs"
      :key="t.id"
      type="button"
      role="tab"
      :aria-selected="tab === t.id"
      class="-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors"
      :class="tab === t.id ? 'border-primary text-foreground' : 'text-muted-foreground hover:text-foreground border-transparent'"
      @click="tab = t.id"
    >
      {{ t.label }}
    </button>
  </div>

  <!-- Cases -->
  <section v-if="tab === 'cases'" role="tabpanel" aria-label="Cases" class="space-y-4">
    <div v-if="canWrite && enabled" class="flex flex-wrap items-center gap-2">
      <Button
        v-if="ai.data.value?.available"
        :variant="panel === 'generate' ? 'secondary' : 'default'"
        :disabled="!env?.baseUrl"
        @click="panel = panel === 'generate' ? '' : 'generate'"
        ><Sparkles />Generate with AI</Button
      >
      <Button
        :variant="panel === 'new' ? 'secondary' : 'outline'"
        :disabled="!env?.baseUrl"
        @click="panel = panel === 'new' ? '' : 'new'"
        ><Plus />New case</Button
      >
      <span class="ml-auto flex flex-wrap items-center gap-2">
        <label class="text-muted-foreground flex items-center gap-1.5 text-xs" title="When a button or field moved or was renamed, AI may find it again for that one step. Checks are never repaired.">
          <input v-model="heal" type="checkbox" />AI repairs a step whose element moved
        </label>
        <Button
          :disabled="mutation.busy.value !== null || !env?.baseUrl || !specs.data.value?.items.length"
          @click="run(picked.size ? [...picked] : undefined)"
          ><Play />{{ picked.size ? `Run ${picked.size} selected` : "Run all" }}</Button
        >
      </span>
    </div>
    <p v-if="!ai.data.value?.available && canWrite && enabled" class="text-muted-foreground text-xs">
      AI drafting and repairs are off: an organization admin adds an API key
      under Team → organization → AI settings. Cases you write still run.
    </p>
    <ErrorNote
      v-if="mutation.error.value && ['run', 'export'].includes(mutation.error.value.key)"
      :error="explain(mutation.error.value.error)"
    />

    <GeneratePanel
      v-if="panel === 'generate' && env"
      :project="project"
      :environment-id="environmentId"
      :base="env.baseUrl"
      :accounts="accountKeys"
      :model="ai.data.value?.model ?? ''"
      :key-source="ai.data.value?.source ?? ''"
      @saved="onGenerated"
      @close="panel = ''"
    />
    <section v-if="panel === 'new' && env" class="bg-card rounded-lg border p-4" aria-label="New case">
      <CaseEditor
        id="new-case"
        v-model="newCase"
        mode="new"
        :accounts="accountKeys"
        :base="env.baseUrl"
        :busy="mutation.busy.value !== null"
        save-label="Create case"
        @save="createCase"
        @cancel="panel = ''"
      />
      <ErrorNote
        v-if="mutation.error.value?.key === 'new'"
        class="mt-3"
        :error="explain(mutation.error.value.error)"
      />
    </section>

    <AsyncState :loading="specs.loading.value && !specs.data.value" :error="specs.error.value" @retry="refresh">
      <EmptyState
        v-if="!specs.data.value?.items.length"
        :icon="Bot"
        title="No runnable cases yet"
        description="Generate them with AI from your site, or write one: where it starts, the inputs, then steps such as “Type into Message”, “Click Send”, “Check: text is there”."
      />
      <div v-else class="bg-card overflow-hidden rounded-lg border">
        <div class="text-muted-foreground flex items-center gap-3 border-b px-4 py-2 text-xs">
          <input type="checkbox" :checked="allPicked" aria-label="Select every case" @change="toggleAll" />
          <span class="flex-1">{{ specs.data.value.items.length }} runnable case{{ specs.data.value.items.length === 1 ? "" : "s" }}</span>
          <Button size="xs" variant="ghost" :disabled="mutation.busy.value !== null" @click="download"
            ><Download />Playwright file{{ picked.size ? ` (${picked.size})` : "" }}</Button
          >
        </div>
        <ul class="divide-y">
          <li v-for="s in specs.data.value.items" :key="s.caseId" class="auto-case px-4 py-3">
            <div class="flex flex-wrap items-center gap-3">
              <input
                type="checkbox"
                :checked="picked.has(s.caseId)"
                :aria-label="`Select ${s.title}`"
                @change="toggle(s.caseId)"
              />
              <span class="min-w-0 flex-1">
                <RouterLink
                  :to="`${base()}/cases/${encodeURIComponent(s.caseId)}`"
                  class="block truncate text-sm font-medium hover:underline"
                  >{{ s.title }}</RouterLink
                >
                <span class="text-muted-foreground block truncate text-xs"
                  >{{ s.target }}<template v-if="s.account"> · signed in as {{ s.account }}</template>
                  · {{ s.steps.length }} step{{ s.steps.length === 1 ? "" : "s" }}</span
                >
              </span>
              <Badge v-if="s.source === 'ai'" variant="outline"><Sparkles class="size-3" />AI</Badge>
              <RouterLink
                v-if="s.last"
                :to="`${base()}/auto/runs/${encodeURIComponent(s.last.runId)}`"
                :title="`Last run ${time(s.last.finishedAt)}`"
                ><StatusBadge :status="s.last.status"
              /></RouterLink>
              <span v-else class="text-muted-foreground text-xs">not run yet</span>
              <Button v-if="canWrite" size="sm" variant="ghost" @click="editing === s.caseId ? (editing = null) : edit(s)"
                ><Pencil />Edit</Button
              >
            </div>
            <div v-if="editing === s.caseId && editForm && env" class="mt-3 border-t pt-3">
              <CaseEditor
                :id="`edit-${s.caseId}`"
                v-model="editForm"
                mode="edit"
                :accounts="accountKeys"
                :base="env.baseUrl"
                :busy="mutation.busy.value !== null"
                @save="saveEdit"
                @cancel="editing = null"
              />
              <ErrorNote
                v-if="mutation.error.value?.key === `edit:${s.caseId}`"
                class="mt-3"
                :error="explain(mutation.error.value.error)"
              />
            </div>
          </li>
        </ul>
      </div>
    </AsyncState>
  </section>

  <!-- Runs -->
  <section v-else-if="tab === 'runs'" role="tabpanel" aria-label="Runs">
    <AsyncState :loading="runs.loading.value && !runs.data.value" :error="runs.error.value" @retry="refresh">
      <EmptyState
        v-if="!runs.data.value?.items.length"
        :icon="Play"
        title="No runs yet"
        description="Run your cases from the Cases tab, or from CI."
      />
      <ul v-else class="bg-card divide-y overflow-hidden rounded-lg border">
        <li v-for="r in runs.data.value.items" :key="r.id">
          <RouterLink
            :to="`${base()}/auto/runs/${encodeURIComponent(r.id)}`"
            class="hover:bg-accent flex items-center gap-3 px-4 py-3 transition-colors"
          >
            <StatusBadge :status="r.status === 'done' ? (r.failed || r.errored ? 'failed' : 'passed') : r.status" />
            <span class="min-w-0 flex-1">
              <strong class="block truncate text-sm font-medium">{{ counts(r) }}</strong>
              <span class="text-muted-foreground block truncate text-xs"
                >{{ time(r.createdAt) }} · {{ r.trigger === "ci" ? "from CI" : r.requestedBy }}</span
              >
            </span>
            <ChevronRight class="text-muted-foreground size-4" />
          </RouterLink>
        </li>
      </ul>
    </AsyncState>
  </section>

  <!-- Site scan -->
  <section v-else-if="tab === 'scan'" role="tabpanel" aria-label="Site scan" class="space-y-4">
    <form
      v-if="canWrite && enabled"
      class="site-scan-new bg-card grid gap-3 rounded-lg border p-4 md:grid-cols-[minmax(0,1fr)_12rem_7rem_auto] md:items-end"
      @submit.prevent="startScan"
    >
      <div class="space-y-1.5">
        <Label for="scan-url">Start page</Label>
        <Input id="scan-url" v-model="scanUrl" type="url" required maxlength="2048" placeholder="https://staging.example.com" />
      </div>
      <div class="space-y-1.5">
        <Label for="scan-account">Signed in as</Label>
        <NativeSelect id="scan-account" v-model="scanAccount" class="w-full">
          <option value="">Not signed in</option>
          <option v-for="a in signedInKeys" :key="a" :value="a">{{ a }}</option>
        </NativeSelect>
      </div>
      <div class="space-y-1.5">
        <Label for="scan-pages">Pages</Label>
        <NativeSelect id="scan-pages" v-model="scanPages" class="w-full">
          <option v-for="n in ['10', '20', '30', '50']" :key="n" :value="n">{{ n }}</option>
        </NativeSelect>
      </div>
      <Button type="submit" :disabled="mutation.busy.value !== null"><Radar />Scan site</Button>
      <p class="text-muted-foreground text-xs md:col-span-4">
        A URL check of the start page and every page its links reach on the
        same site, up to three links deep — load, script errors, broken links,
        accessibility, speed, phone fit. Sign-out and delete links are never
        followed. Only accounts whose sign-in works are offered.
      </p>
      <ErrorNote
        v-if="mutation.error.value?.key === 'scan'"
        class="md:col-span-4"
        :error="explain(mutation.error.value.error)"
      />
    </form>
    <AsyncState :loading="scans.loading.value && !scans.data.value" :error="scans.error.value" @retry="refresh">
      <EmptyState
        v-if="!scans.data.value?.items.length"
        :icon="Radar"
        title="No site scans yet"
        description="Scan a site to find the pages that fail, break links or load slowly."
      />
      <ul v-else class="bg-card divide-y overflow-hidden rounded-lg border">
        <li v-for="s in scans.data.value.items" :key="s.id">
          <RouterLink
            :to="`${base()}/auto/scans/${encodeURIComponent(s.id)}`"
            class="hover:bg-accent flex items-center gap-3 px-4 py-3 transition-colors"
          >
            <StatusBadge :status="s.status === 'done' ? (s.failed ? 'failed' : s.warnings ? 'warning' : 'passed') : s.status" />
            <span class="min-w-0 flex-1">
              <strong class="block truncate text-sm font-medium">{{ s.startUrl }}</strong>
              <span class="text-muted-foreground block truncate text-xs"
                >{{ s.checked }} of {{ s.pages }} pages checked · {{ s.failed }} failed · {{ s.warnings }} with warnings ·
                {{ time(s.createdAt) }}<template v-if="s.account"> · as {{ s.account }}</template></span
              >
            </span>
            <ChevronRight class="text-muted-foreground size-4" />
          </RouterLink>
        </li>
      </ul>
    </AsyncState>
  </section>

  <!-- CI -->
  <section v-else role="tabpanel" aria-label="Run from CI" class="bg-card space-y-3 rounded-lg border p-4 text-sm">
    <p>
      Run every case after each deploy: issue a token with
      <strong>Start auto runs</strong> on the
      <RouterLink :to="`${base()}/tokens`" class="text-primary underline underline-offset-2">CI tokens</RouterLink>
      page, then add this step after the deploy. It starts a run, waits, prints
      each case and fails the job when one failed.
    </p>
    <TechnicalBlock title="Deploy step (bash, curl, jq)" :text="ciSnippet" />
    <p class="text-muted-foreground">
      Prefer to keep the tests in your own repository? Select cases on the
      Cases tab and download the <strong>Playwright file</strong>: the same
      steps as Playwright code, signing in with
      <code>ATP_USER_&lt;KEY&gt;</code> / <code>ATP_PASS_&lt;KEY&gt;</code>. With
      the ATP reporter, its runs land in Test runs too.
    </p>
  </section>
</template>
