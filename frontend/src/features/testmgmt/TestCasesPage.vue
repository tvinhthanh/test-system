<script setup lang="ts">
import {
  ChevronRight,
  ClipboardList,
  FileUp,
  FolderPlus,
  ListFilter,
  Plus,
  RefreshCw,
  Search,
  ShieldAlert,
} from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import { automationStatuses, priorities, type Priority } from "./enums";
import type { Suite, TestCase } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import TmTabs from "./TmTabs.vue";
import CaseContextEditor from "./CaseContextEditor.vue";
import CaseBulkTools from "./CaseBulkTools.vue";
import CaseTaxonomyEditor from "./CaseTaxonomyEditor.vue";
import TaxonomyFilters from "./TaxonomyFilters.vue";
import {
  copyTaxonomy,
  taxonomyFilterOptions,
  taxonomySummary,
  type TaxonomyFilter,
} from "./taxonomy";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute(),
  router = useRouter();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const base = () => `/p/${encodeURIComponent(project.value)}`;
const canWrite = useProjectPermission("testmgmt.write");

// Filters are URL state so a filtered list is a shareable link.
const q = computed(() => String(route.query.q ?? ""));
const suite = computed(() => String(route.query.suite ?? ""));
const automation = computed(() => String(route.query.automation ?? ""));
const critical = computed(() => String(route.query.critical ?? ""));
const taxonomyFilters = computed<TaxonomyFilter>(() =>
  Object.fromEntries(
    Object.keys(taxonomyFilterOptions).map((key) => [
      key,
      String(route.query[key] ?? ""),
    ]),
  ),
);
const clearTaxonomyFilters = Object.fromEntries(
  Object.keys(taxonomyFilterOptions).map((key) => [key, ""]),
);
const filtered = computed(
  () =>
    !!(
      q.value ||
      suite.value ||
      automation.value ||
      critical.value ||
      Object.values(taxonomyFilters.value).some(Boolean)
    ),
);
function setFilter(patch: Record<string, string>) {
  const query = { ...route.query, ...patch };
  for (const k of Object.keys(query)) if (!query[k]) delete query[k];
  void router.replace({ query });
}
const search = ref(q.value);
let timer: ReturnType<typeof setTimeout> | undefined;
watch(search, (v) => {
  clearTimeout(timer);
  timer = setTimeout(() => setFilter({ q: v.trim() }), 300);
});

const cases = useResource<TestCase[]>();
const suites = useResource<Suite[]>();
const selectedIds = ref<string[]>([]);
const bulkBusy = ref(false);
const selectedCases = computed(() =>
  (cases.data.value ?? []).filter((item) =>
    selectedIds.value.includes(item.id),
  ),
);
const allVisibleSelected = computed(
  () =>
    !!cases.data.value?.length &&
    selectedCases.value.length === cases.data.value.length,
);
function selectAll(event: Event) {
  selectedIds.value = (event.target as HTMLInputElement).checked
    ? (cases.data.value ?? []).map((item) => item.id)
    : [];
}
async function bulkUpdated(ids: string[]) {
  selectedIds.value = selectedIds.value.filter((id) => !ids.includes(id));
  await refresh();
}
function refresh() {
  return cases.load((signal) =>
    testmgmt.testCases(
      project.value,
      {
        ...taxonomyFilters.value,
        q: q.value,
        suite: suite.value,
        automation: automation.value as never,
        critical: critical.value as never,
      },
      signal,
    ),
  );
}
function refreshSuites() {
  return suites.load((signal) => testmgmt.suites(project.value, signal));
}
watch(
  () => [
    project.value,
    q.value,
    suite.value,
    automation.value,
    critical.value,
    JSON.stringify(taxonomyFilters.value),
  ],
  () => {
    selectedIds.value = [];
    void refresh();
  },
  {
    immediate: true,
  },
);
watch(project, refreshSuites, { immediate: true });

// Create + import live in two collapsible panels; both end in a refresh.
const panel = ref<"" | "new" | "suite" | "import">("");
const draft = ref({
  taxonomy: copyTaxonomy(),
  preconditions: "",
  testData: "",
  title: "",
  suiteId: "",
  priority: "p2" as Priority,
  isCritical: false,
});
const suiteName = ref("");
const csv = ref("");
const report = ref<null | Awaited<ReturnType<typeof testmgmt.importCsv>>>(null);
const mutation = useMutation();
async function create() {
  const created = await mutation.run("new", (signal) =>
    testmgmt.createTestCase(project.value, { ...draft.value }, signal),
  );
  if (created) {
    draft.value = {
      taxonomy: copyTaxonomy(),
      preconditions: "",
      testData: "",
      title: "",
      suiteId: "",
      priority: "p2",
      isCritical: false,
    };
    panel.value = "";
    await router.push(`${base()}/cases/${encodeURIComponent(created.id)}`);
  }
}
async function createSuite() {
  const created = await mutation.run("suite", (signal) =>
    testmgmt.createSuite(project.value, suiteName.value.trim(), signal),
  );
  if (created) {
    suiteName.value = "";
    await refreshSuites();
    draft.value.suiteId = created.id;
    panel.value = "new";
  }
}
async function importSheet() {
  report.value = null;
  const rep = await mutation.run("import", (signal) =>
    testmgmt.importCsv(project.value, csv.value, signal),
  );
  if (rep) {
    report.value = rep;
    csv.value = "";
    await refresh();
  }
}
async function readFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (file) csv.value = await file.text();
}
const label = (s: string) => s.replaceAll("_", " ");
</script>
<template>
  <PageHeader
    eyebrow="Test management"
    title="Test cases"
    description="Manual and automated cases in one list. A case linked to a test identity carries that test's history and flaky signal (§6.8.1)."
  >
    <Button
      v-if="canWrite"
      :variant="panel === 'suite' ? 'secondary' : 'outline'"
      size="sm"
      @click="panel = panel === 'suite' ? '' : 'suite'"
      ><FolderPlus />New suite</Button
    >
    <Button
      v-if="canWrite"
      :variant="panel === 'import' ? 'secondary' : 'outline'"
      size="sm"
      @click="panel = panel === 'import' ? '' : 'import'"
      ><FileUp />Import CSV</Button
    >
    <Button
      v-if="canWrite"
      :variant="panel === 'new' ? 'secondary' : 'default'"
      size="sm"
      @click="panel = panel === 'new' ? '' : 'new'"
      ><Plus />New case</Button
    >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh test cases"
      title="Refresh test cases"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>
  <TmTabs :project="project" current="cases" />

  <form
    v-if="canWrite && panel === 'suite'"
    class="bg-card mb-5 grid gap-4 rounded-lg border p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end"
    @submit.prevent="createSuite"
  >
    <div class="space-y-2">
      <Label for="new-suite-name">Suite name</Label>
      <Input
        id="new-suite-name"
        v-model="suiteName"
        required
        maxlength="200"
        placeholder="Checkout"
      />
    </div>
    <Button type="submit" size="sm" :disabled="mutation.busy.value !== null"
      >Create suite</Button
    >
    <ErrorNote
      v-if="mutation.error.value?.key === 'suite'"
      class="sm:col-span-2"
      :error="mutation.error.value.error"
    />
  </form>

  <form
    v-if="canWrite && panel === 'new'"
    class="bg-card mb-5 grid gap-4 rounded-lg border p-4 md:grid-cols-[minmax(0,1fr)_minmax(10rem,16rem)_8rem_auto_auto] md:items-end"
    @submit.prevent="create"
  >
    <div class="space-y-2">
      <Label for="new-title">Title</Label>
      <Input
        id="new-title"
        v-model="draft.title"
        required
        maxlength="500"
        placeholder="What the tester checks"
      />
    </div>
    <div class="space-y-2">
      <Label for="new-suite">Suite</Label>
      <NativeSelect
        id="new-suite"
        v-model="draft.suiteId"
        class="w-full"
        :disabled="suites.loading.value || bulkBusy"
      >
        <option value="">No suite</option>
        <option v-for="s in suites.data.value ?? []" :key="s.id" :value="s.id">
          {{ s.name }}
        </option>
      </NativeSelect>
    </div>
    <div class="space-y-2">
      <Label for="new-priority">Priority</Label>
      <NativeSelect id="new-priority" v-model="draft.priority" class="w-full">
        <option v-for="p in priorities" :key="p" :value="p">{{ p }}</option>
      </NativeSelect>
    </div>
    <label class="flex h-9 items-center gap-2 text-sm">
      <input
        v-model="draft.isCritical"
        type="checkbox"
        class="accent-primary size-4"
      />
      Critical flow
    </label>
    <Button type="submit" size="sm" :disabled="mutation.busy.value !== null"
      >Create</Button
    >
    <CaseTaxonomyEditor v-model="draft.taxonomy" class="md:col-span-5" />
    <CaseContextEditor
      v-model:preconditions="draft.preconditions"
      v-model:test-data="draft.testData"
      class="md:col-span-5"
    />
    <ErrorNote
      v-if="mutation.error.value?.key === 'new'"
      class="md:col-span-5"
      :error="mutation.error.value.error"
    />
  </form>

  <form
    v-if="canWrite && panel === 'import'"
    class="bg-card mb-5 space-y-3 rounded-lg border p-4"
    @submit.prevent="importSheet"
  >
    <p class="text-muted-foreground text-sm">
      Columns (any order, case-insensitive): <code>key</code>,
      <code>title</code>, <code>suite</code>, <code>steps</code> (<code
        >action =&gt; expected</code
      >, one per line or <code>|</code>-separated), <code>expected</code>,
      <code>priority</code> (p0–p3), <code>critical</code>,
      <code>requirement</code>. Rows with a key update the same case on
      re-import; rows without one match by title.
    </p>
    <div class="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-start">
      <textarea
        v-model="csv"
        class="border-input bg-background focus-visible:border-ring focus-visible:ring-ring/50 min-h-32 w-full rounded-md border px-3 py-2 font-mono text-xs outline-none focus-visible:ring-3"
        placeholder="key,title,suite,priority,critical&#10;TC-1,Pay with saved card,Checkout,p0,yes"
        spellcheck="false"
      />
      <div class="flex flex-col gap-2">
        <Button as-child variant="outline" size="sm">
          <label
            >Choose file<input
              type="file"
              accept=".csv,text/csv"
              class="sr-only"
              @change="readFile"
          /></label>
        </Button>
        <Button
          type="submit"
          size="sm"
          :disabled="!csv.trim() || mutation.busy.value !== null"
          >Import</Button
        >
      </div>
    </div>
    <ErrorNote
      v-if="mutation.error.value?.key === 'import'"
      :error="mutation.error.value.error"
    />
    <div
      v-if="report"
      class="bg-muted/50 rounded-md border px-3 py-2 text-sm"
      role="status"
    >
      <strong class="font-medium"
        >{{ report.created }} created · {{ report.updated }} updated ·
        {{ report.skipped }} skipped</strong
      >
      <span class="text-muted-foreground">
        · {{ report.suites }} new suite(s) · {{ report.requirements }} new
        requirement(s)</span
      >
      <ul
        v-if="report.errors.length"
        class="text-danger mt-1 list-disc pl-5 text-xs"
      >
        <li v-for="e in report.errors" :key="e">{{ e }}</li>
      </ul>
    </div>
  </form>

  <form
    class="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-[minmax(14rem,1fr)_minmax(10rem,14rem)_12rem_9rem_auto] xl:items-end"
    role="search"
    @submit.prevent
  >
    <label class="relative min-w-0 flex-1">
      <span class="sr-only">Search title or key</span>
      <Search
        :size="16"
        aria-hidden="true"
        class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2"
      />
      <Input
        v-model="search"
        :disabled="bulkBusy"
        type="search"
        class="pl-9"
        placeholder="Search title or key…"
        maxlength="200"
      />
    </label>
    <label class="flex flex-col gap-1.5 text-xs font-medium">
      <span class="text-muted-foreground">Suite</span>
      <NativeSelect
        class="w-full"
        :model-value="suite"
        :disabled="suites.loading.value"
        @update:model-value="setFilter({ suite: $event })"
      >
        <option value="">All suites</option>
        <option v-for="s in suites.data.value ?? []" :key="s.id" :value="s.id">
          {{ s.name }} ({{ s.cases }})
        </option>
      </NativeSelect>
    </label>
    <label class="flex flex-col gap-1.5 text-xs font-medium">
      <span class="text-muted-foreground">Automation</span>
      <NativeSelect
        class="w-full md:w-48"
        :model-value="automation"
        :disabled="bulkBusy"
        @update:model-value="setFilter({ automation: $event })"
      >
        <option value="">Any status</option>
        <option v-for="s in automationStatuses" :key="s" :value="s">
          {{ label(s) }}
        </option>
      </NativeSelect>
    </label>
    <label class="flex flex-col gap-1.5 text-xs font-medium">
      <span class="text-muted-foreground">Critical</span>
      <NativeSelect
        class="w-full md:w-36"
        :model-value="critical"
        :disabled="bulkBusy"
        @update:model-value="setFilter({ critical: $event })"
      >
        <option value="">All</option>
        <option value="true">Critical only</option>
        <option value="false">Not critical</option>
      </NativeSelect>
    </label>
    <Button
      v-if="filtered"
      :disabled="bulkBusy"
      variant="ghost"
      size="sm"
      type="button"
      @click="
        search = '';
        setFilter({
          q: '',
          suite: '',
          automation: '',
          critical: '',
          ...clearTaxonomyFilters,
        });
      "
      ><ListFilter />Clear</Button
    >
  </form>

  <TaxonomyFilters
    :model-value="taxonomyFilters"
    :disabled="bulkBusy"
    @change="setFilter"
  />
  <CaseBulkTools
    :key="project"
    :project="project"
    :selected="selectedCases"
    :filter="{
      ...taxonomyFilters,
      q,
      suite,
      automation: automation as never,
      critical: critical as never,
    }"
    :suites="suites.data.value ?? []"
    :can-write="canWrite"
    :disabled="cases.loading.value || !!cases.error.value"
    @updated="bulkUpdated"
    @clear="selectedIds = []"
    @busy="bulkBusy = $event"
  />
  <AsyncState
    :loading="cases.loading.value"
    :error="cases.error.value"
    @retry="refresh"
  >
    <label
      v-if="cases.data.value?.length"
      class="mb-3 flex items-center gap-2 text-sm"
      ><input
        type="checkbox"
        :checked="allVisibleSelected"
        :disabled="bulkBusy"
        @change="selectAll"
      />Select visible cases ({{ cases.data.value.length }})</label
    >
    <EmptyState
      v-if="!cases.data.value?.length"
      :icon="ClipboardList"
      :title="filtered ? 'No matching cases' : 'No test cases yet'"
      :description="
        filtered
          ? 'Try another word or filter.'
          : 'Write the first case, or import your sheet — a CSV export from Excel, TestRail or a wiki table is enough to start.'
      "
    >
      <Button v-if="canWrite && !filtered" size="sm" @click="panel = 'new'"
        ><Plus />New case</Button
      >
      <Button
        v-if="canWrite && !filtered"
        variant="outline"
        size="sm"
        @click="panel = 'import'"
        ><FileUp />Import CSV</Button
      >
    </EmptyState>
    <ul
      v-else
      class="case-list bg-card divide-y overflow-hidden rounded-lg border"
    >
      <li
        v-for="c in cases.data.value"
        :key="c.id"
        class="flex min-w-0 items-center"
      >
        <input
          v-model="selectedIds"
          type="checkbox"
          :value="c.id"
          :disabled="bulkBusy"
          :aria-label="`Select ${c.title}`"
          class="ml-3 shrink-0"
        />
        <RouterLink
          :to="`${base()}/cases/${encodeURIComponent(c.id)}`"
          class="case-row hover:bg-accent focus-visible:bg-accent group grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1.5 px-4 py-3 transition-colors outline-none md:grid-cols-[minmax(0,1fr)_4rem_auto_1.25rem]"
        >
          <span class="min-w-0">
            <span class="flex min-w-0 items-center gap-2">
              <code
                v-if="c.externalKey"
                class="text-muted-foreground shrink-0 text-xs"
                >{{ c.externalKey }}</code
              >
              <strong class="truncate text-sm font-medium">{{
                c.title
              }}</strong>
              <ShieldAlert
                v-if="c.isCritical"
                :size="14"
                class="text-danger shrink-0"
                aria-label="Critical flow"
              />
            </span>
            <span class="text-muted-foreground block truncate text-xs">
              {{ c.suiteName || "No suite" }}
              <template v-if="c.steps.length">
                · {{ c.steps.length }} step{{
                  c.steps.length === 1 ? "" : "s"
                }}</template
              >
            </span>
            <span class="text-muted-foreground block truncate text-xs">{{
              taxonomySummary(c.taxonomy)
            }}</span>
          </span>
          <Badge variant="outline" class="font-mono uppercase">{{
            c.priority
          }}</Badge>
          <StatusBadge :status="c.automationStatus" />
          <ChevronRight
            :size="16"
            aria-hidden="true"
            class="text-muted-foreground hidden transition-transform group-hover:translate-x-0.5 md:block"
          />
        </RouterLink>
      </li>
    </ul>
    <p class="text-muted-foreground mt-3 text-xs">
      {{ cases.data.value?.length ?? 0 }} case{{
        (cases.data.value?.length ?? 0) === 1 ? "" : "s"
      }}
    </p>
  </AsyncState>
</template>
