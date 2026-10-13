<script setup lang="ts">
import {
  Link2,
  Link2Off,
  Plus,
  RefreshCw,
  Save,
  ShieldAlert,
  Target,
  Trash2,
} from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Identity } from "../reports/reportsRepository";
import { automationStatuses, priorities } from "./enums";
import type { Requirement, Step, Suite, TestCase } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import ErrorNote from "./ErrorNote.vue";
import CaseContextEditor from "./CaseContextEditor.vue";
import CaseChangeHistory from "./CaseChangeHistory.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute();
const { testmgmt, reports } = useServices();
const project = computed(() => String(route.params.project));
const canWrite = useProjectPermission("testmgmt.write");
const caseId = computed(() => String(route.params.case));
const base = () => `/p/${encodeURIComponent(project.value)}`;

const tc = useResource<TestCase>();
const requirements = useResource<Requirement[]>();
const suites = useResource<Suite[]>();
function refresh() {
  void tc.load((signal) =>
    testmgmt.testCase(project.value, caseId.value, signal),
  );
  void requirements.load((signal) =>
    testmgmt.requirements(project.value, signal),
  );
  void suites.load((signal) => testmgmt.suites(project.value, signal));
}
watch(() => [project.value, caseId.value], refresh, { immediate: true });

// Editable copy of the case; Save sends only what changed.
const form = ref<{
  title: string;
  suiteId: string;
  priority: TestCase["priority"];
  isCritical: boolean;
  automationStatus: TestCase["automationStatus"];
  expected: string;
  preconditions: string;
  testData: string;
  steps: Step[];
} | null>(null);
watch(
  () => tc.data.value,
  (c) => {
    form.value = c
      ? {
          title: c.title,
          suiteId: c.suiteId,
          priority: c.priority,
          isCritical: c.isCritical,
          automationStatus: c.automationStatus,
          expected: c.expected,
          preconditions: c.preconditions,
          testData: c.testData,
          steps: c.steps.map((s) => ({ ...s })),
        }
      : null;
  },
  { immediate: true },
);
const dirty = computed(() => {
  const c = tc.data.value,
    f = form.value;
  if (!c || !f) return false;
  return (
    f.title !== c.title ||
    f.suiteId !== c.suiteId ||
    f.priority !== c.priority ||
    f.isCritical !== c.isCritical ||
    f.automationStatus !== c.automationStatus ||
    f.expected !== c.expected ||
    f.preconditions !== c.preconditions ||
    f.testData !== c.testData ||
    JSON.stringify(f.steps) !== JSON.stringify(c.steps)
  );
});
const mutation = useMutation();
const contextValid = computed(
  () =>
    !form.value ||
    [form.value.preconditions, form.value.testData].every(
      (value) => new TextEncoder().encode(value.trim()).length <= 16000,
    ),
);
async function save() {
  if (!form.value || !contextValid.value) return;
  const saved = await mutation.run("save", (signal) =>
    testmgmt.updateTestCase(
      project.value,
      caseId.value,
      {
        ...form.value!,
        expectedUpdatedAt: tc.data.value!.updatedAt,
        steps: form.value!.steps.filter((s) => s.action || s.expected),
      },
      signal,
    ),
  );
  if (saved) tc.data.value = saved;
}

// Automation links: pick among the project's active identities.
const picker = ref(false);
const identities = useResource<Identity[]>();
const identityFilter = ref("");
// Loaded when the picker opens, or right away when the case already has
// links so their titles can be shown instead of ids.
function loadIdentities() {
  if (!identities.data.value && !identities.loading.value)
    void identities.load(
      async (signal) =>
        (await reports.identities(project.value, "", "", signal)).items,
    );
}
watch(picker, (open) => open && loadIdentities());
watch(
  () => tc.data.value?.links.length ?? 0,
  (n) => n > 0 && loadIdentities(),
  { immediate: true },
);
const candidates = computed(() => {
  const q = identityFilter.value.trim().toLowerCase();
  const linked = new Set(tc.data.value?.links.map((l) => l.stableTestId));
  return (identities.data.value ?? []).filter(
    (i) =>
      i.status === "active" &&
      !linked.has(i.id) &&
      (!q ||
        i.canonicalTitle.toLowerCase().includes(q) ||
        i.canonicalFilePath.toLowerCase().includes(q)),
  );
});
async function link(id: string) {
  const ok = await mutation.run(`link:${id}`, (signal) =>
    testmgmt.link(project.value, caseId.value, id, signal),
  );
  if (ok) {
    picker.value = false;
    refresh();
  }
}
async function unlink(linkId: string) {
  const ok = await mutation.run(`unlink:${linkId}`, async (signal) => {
    await testmgmt.unlink(project.value, caseId.value, linkId, signal);
    return true;
  });
  if (ok) refresh();
}
const identityTitle = (id: string) =>
  identities.data.value?.find((i) => i.id === id)?.canonicalTitle;

// Requirement coverage.
const covered = computed(() => {
  const ids = new Set(tc.data.value?.requirements ?? []);
  return (requirements.data.value ?? []).filter((r) => ids.has(r.id));
});
const uncovered = computed(() => {
  const ids = new Set(tc.data.value?.requirements ?? []);
  return (requirements.data.value ?? []).filter((r) => !ids.has(r.id));
});
const pickRequirement = ref("");
async function cover() {
  if (!pickRequirement.value) return;
  const ok = await mutation.run("cover", async (signal) => {
    await testmgmt.cover(
      project.value,
      caseId.value,
      pickRequirement.value,
      signal,
    );
    return true;
  });
  if (ok) {
    pickRequirement.value = "";
    refresh();
  }
}
async function uncover(requirementId: string) {
  const ok = await mutation.run(`uncover:${requirementId}`, async (signal) => {
    await testmgmt.uncover(project.value, caseId.value, requirementId, signal);
    return true;
  });
  if (ok) refresh();
}
const label = (s: string) => s.replaceAll("_", " ");
</script>
<template>
  <BackLink :to="`${base()}/cases`">Test cases</BackLink>
  <PageHeader eyebrow="Test case" :title="tc.data.value?.title ?? caseId">
    <template #description>
      <span
        v-if="tc.data.value"
        class="inline-flex flex-wrap items-center gap-2"
      >
        <code v-if="tc.data.value.externalKey" class="text-xs">{{
          tc.data.value.externalKey
        }}</code>
        <Badge variant="outline" class="font-mono uppercase">{{
          tc.data.value.priority
        }}</Badge>
        <StatusBadge :status="tc.data.value.automationStatus" />
        <Badge
          v-if="tc.data.value.isCritical"
          variant="outline"
          class="border-danger/30 bg-danger-soft text-danger"
          ><ShieldAlert />Critical flow</Badge
        >
        <span class="text-muted-foreground text-xs"
          >{{ tc.data.value.suiteName || "No suite" }} · updated
          {{ time(tc.data.value.updatedAt) }}</span
        >
      </span>
    </template>
    <Button
      v-if="canWrite"
      size="sm"
      :disabled="!dirty || !contextValid || mutation.busy.value !== null"
      @click="save"
      ><Save />Save changes</Button
    >
    <Button
      variant="outline"
      size="icon-sm"
      aria-label="Refresh test case"
      title="Refresh test case"
      @click="refresh"
      ><RefreshCw
    /></Button>
  </PageHeader>

  <AsyncState
    :loading="tc.loading.value"
    :error="tc.error.value"
    :rows="4"
    @retry="refresh"
  >
    <template v-if="tc.data.value && form">
      <ErrorNote
        v-if="mutation.error.value?.key === 'save'"
        class="mb-4"
        :error="mutation.error.value.error"
      />
      <section class="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <div class="space-y-6">
          <div class="bg-card space-y-4 rounded-lg border p-4">
            <div class="space-y-2">
              <Label for="case-title">Title</Label>
              <Input
                id="case-title"
                v-model="form.title"
                maxlength="500"
                :readonly="!canWrite"
              />
            </div>
            <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <div class="space-y-2">
                <Label for="case-suite">Suite</Label>
                <NativeSelect
                  id="case-suite"
                  v-model="form.suiteId"
                  :disabled="!canWrite || suites.loading.value"
                  class="w-full"
                >
                  <option value="">No suite</option>
                  <option
                    v-for="s in suites.data.value ?? []"
                    :key="s.id"
                    :value="s.id"
                  >
                    {{ s.name }}
                  </option>
                </NativeSelect>
              </div>
              <div class="space-y-2">
                <Label for="case-priority">Priority</Label>
                <NativeSelect
                  id="case-priority"
                  v-model="form.priority"
                  :disabled="!canWrite"
                  class="w-full"
                >
                  <option v-for="p in priorities" :key="p" :value="p">
                    {{ p }}
                  </option>
                </NativeSelect>
              </div>
              <div class="space-y-2">
                <Label for="case-automation">Automation</Label>
                <NativeSelect
                  id="case-automation"
                  v-model="form.automationStatus"
                  :disabled="!canWrite"
                  class="w-full"
                >
                  <option v-for="s in automationStatuses" :key="s" :value="s">
                    {{ label(s) }}
                  </option>
                </NativeSelect>
              </div>
              <label class="flex items-center gap-2 self-end pb-2 text-sm">
                <input
                  v-model="form.isCritical"
                  type="checkbox"
                  :disabled="!canWrite"
                  class="accent-primary size-4"
                />
                Critical flow (release gate)
              </label>
            </div>
          </div>

          <CaseContextEditor
            v-model:preconditions="form.preconditions"
            v-model:test-data="form.testData"
            :readonly="!canWrite"
          />
          <div class="bg-card overflow-hidden rounded-lg border">
            <div class="flex items-center justify-between border-b px-4 py-2">
              <h2 class="text-sm font-semibold">Steps</h2>
              <Button
                v-if="canWrite"
                variant="ghost"
                size="xs"
                @click="form.steps.push({ action: '', expected: '' })"
                ><Plus />Add step</Button
              >
            </div>
            <p
              v-if="!form.steps.length"
              class="text-muted-foreground px-4 py-6 text-center text-sm"
            >
              No steps yet. A case without steps is a title someone has to
              interpret.
            </p>
            <ol v-else class="divide-y">
              <li
                v-for="(s, i) in form.steps"
                :key="i"
                class="grid gap-2 px-4 py-3 sm:grid-cols-[1.5rem_minmax(0,1fr)_minmax(0,1fr)_2rem] sm:items-start"
              >
                <span class="text-muted-foreground pt-2 font-mono text-xs">{{
                  i + 1
                }}</span>
                <Input
                  v-model="s.action"
                  :readonly="!canWrite"
                  placeholder="Action"
                  maxlength="2000"
                />
                <Input
                  v-model="s.expected"
                  :readonly="!canWrite"
                  placeholder="Expected"
                  maxlength="2000"
                />
                <Button
                  v-if="canWrite"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="`Remove step ${i + 1}`"
                  @click="form.steps.splice(i, 1)"
                  ><Trash2
                /></Button>
              </li>
            </ol>
            <div class="space-y-2 border-t px-4 py-3">
              <Label for="case-expected">Overall expected result</Label>
              <Input
                id="case-expected"
                v-model="form.expected"
                :readonly="!canWrite"
                maxlength="4000"
              />
            </div>
          </div>
        </div>

        <div class="space-y-6">
          <section
            class="bg-card rounded-lg border"
            aria-labelledby="links-title"
          >
            <div class="flex items-center justify-between border-b px-4 py-2">
              <h2 id="links-title" class="text-sm font-semibold">Automation</h2>
              <Button
                v-if="canWrite"
                variant="ghost"
                size="xs"
                @click="picker = !picker"
                ><Link2 />Link a test</Button
              >
            </div>
            <p class="text-muted-foreground px-4 pt-3 text-xs">
              A link points at a test identity, so it survives renames and
              carries the flaky signal into the release gate.
            </p>
            <ul
              v-if="tc.data.value.links.length"
              class="link-list divide-y px-4 pt-2"
            >
              <li
                v-for="l in tc.data.value.links"
                :key="l.id"
                class="flex items-center gap-2 py-2.5 text-sm"
              >
                <span class="min-w-0 flex-1">
                  <RouterLink
                    class="block truncate font-medium hover:underline"
                    :to="`${base()}/tests/${encodeURIComponent(l.stableTestId)}/history`"
                    >{{
                      identityTitle(l.stableTestId) ?? l.stableTestId
                    }}</RouterLink
                  >
                  <span class="text-muted-foreground block text-xs"
                    >{{ l.linkType }} · {{ l.createdBy }} ·
                    {{ time(l.createdAt) }}</span
                  >
                </span>
                <Button
                  v-if="canWrite"
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Remove link"
                  :disabled="mutation.busy.value !== null"
                  @click="unlink(l.id)"
                  ><Link2Off
                /></Button>
              </li>
            </ul>
            <p v-else class="text-muted-foreground px-4 py-3 text-sm">
              Not linked to automation.
            </p>
            <div v-if="canWrite && picker" class="border-t p-3">
              <Input
                v-model="identityFilter"
                type="search"
                placeholder="Filter tests by title or file"
                class="mb-2"
              />
              <AsyncState
                :loading="identities.loading.value"
                :error="identities.error.value"
                :rows="3"
                @retry="
                  identities.load(
                    async (s) =>
                      (await reports.identities(project, '', '', s)).items,
                  )
                "
              >
                <ul class="max-h-64 divide-y overflow-y-auto rounded-md border">
                  <li v-for="i in candidates.slice(0, 20)" :key="i.id">
                    <button
                      type="button"
                      class="hover:bg-accent flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors"
                      :disabled="mutation.busy.value !== null"
                      @click="link(i.id)"
                    >
                      <span class="min-w-0 flex-1">
                        <span class="block truncate font-medium">{{
                          i.canonicalTitle
                        }}</span>
                        <code
                          class="text-muted-foreground block truncate text-xs"
                          >{{ i.canonicalFilePath }}</code
                        >
                      </span>
                      <StatusBadge :status="i.verdict" />
                    </button>
                  </li>
                  <li
                    v-if="!candidates.length"
                    class="text-muted-foreground px-3 py-4 text-center text-sm"
                  >
                    No unlinked active test matches.
                  </li>
                </ul>
              </AsyncState>
            </div>
            <ErrorNote
              v-if="
                mutation.error.value &&
                /^(un)?link:/.test(mutation.error.value.key)
              "
              class="m-3"
              :error="mutation.error.value.error"
            />
          </section>

          <section
            class="bg-card rounded-lg border"
            aria-labelledby="req-title"
          >
            <div class="border-b px-4 py-2">
              <h2 id="req-title" class="text-sm font-semibold">
                Requirements covered
              </h2>
            </div>
            <ul v-if="covered.length" class="divide-y px-4">
              <li
                v-for="r in covered"
                :key="r.id"
                class="flex items-center gap-2 py-2.5 text-sm"
              >
                <Target :size="14" class="text-muted-foreground shrink-0" />
                <span class="min-w-0 flex-1 truncate">
                  <code
                    v-if="r.externalKey"
                    class="text-muted-foreground mr-1 text-xs"
                    >{{ r.externalKey }}</code
                  >{{ r.title }}
                </span>
                <Badge variant="outline" class="font-mono uppercase">{{
                  r.priority
                }}</Badge>
                <Button
                  v-if="canWrite"
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Remove coverage"
                  :disabled="mutation.busy.value !== null"
                  @click="uncover(r.id)"
                  ><Trash2
                /></Button>
              </li>
            </ul>
            <p v-else class="text-muted-foreground px-4 py-3 text-sm">
              Covers no requirement yet.
            </p>
            <div
              v-if="canWrite && uncovered.length"
              class="flex gap-2 border-t p-3"
            >
              <NativeSelect v-model="pickRequirement" class="min-w-0 flex-1">
                <option value="">Add a requirement…</option>
                <option v-for="r in uncovered" :key="r.id" :value="r.id">
                  {{ r.externalKey ? r.externalKey + " · " : "" }}{{ r.title }}
                </option>
              </NativeSelect>
              <Button
                size="sm"
                :disabled="!pickRequirement || mutation.busy.value !== null"
                @click="cover"
                >Add</Button
              >
            </div>
            <ErrorNote
              v-if="
                mutation.error.value &&
                /^(un)?cover/.test(mutation.error.value.key)
              "
              class="m-3"
              :error="mutation.error.value.error"
            />
          </section>
        </div>
      </section>
    </template>
    <CaseChangeHistory
      v-if="tc.data.value"
      :changes="tc.data.value.audit"
      :truncated="tc.data.value.auditTruncated"
    />
  </AsyncState>
</template>
