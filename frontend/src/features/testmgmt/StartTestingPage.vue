<script setup lang="ts">
// Start a round of testing (QĐ-15): pick the kind of testing, the site,
// the cases — from the library, from a starter set, or drafted by AI from
// the site — then the plan is made and the run screen opens on the first
// case. Starter and AI cases become real test cases (tagged with the kind)
// only when the plan is created, and only the ones left ticked.
import {
  ChevronRight,
  LoaderCircle,
  Play,
  Sparkles,
  TriangleAlert,
} from "@lucide/vue";
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { AiStatus, CaseSuggestion, TestCase } from "./testmgmtRepository";
import { useMutation } from "./useMutation";
import { useProjectPermission } from "../reports/useProjectPermission";
import { taxonomyFor, testTypes, type TestType } from "./testTypes";
import EnvironmentPicker from "./EnvironmentPicker.vue";
import ErrorNote from "./ErrorNote.vue";
import { explain } from "./explain";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const route = useRoute(),
  router = useRouter();
const { testmgmt } = useServices();
const project = computed(() => String(route.params.project));
const canWrite = useProjectPermission("testmgmt.write");
const base = () => `/p/${encodeURIComponent(project.value)}`;

// 1. Kind of testing
const type = shallowRef<TestType | null>(null);
// 2. Site
const environmentId = ref("");
// 3. Cases
const library = useResource<TestCase[]>();
const pickedLibrary = ref(new Set<string>());
const pickedStarters = ref(new Set<number>());
const source = ref<"library" | "starter" | "ai">("library");
watch(type, async (t) => {
  if (!t) return;
  pickedLibrary.value = new Set();
  pickedStarters.value = new Set();
  suggestion.value = null;
  pickedDrafts.value = new Set();
  if (!nameEdited.value) planName.value = defaultName(t);
  await library.load((signal) =>
    testmgmt.testCases(project.value, t.filter, signal),
  );
  const usable = (library.data.value ?? []).filter(
    (c) => c.automationStatus !== "obsolete",
  );
  pickedLibrary.value = new Set(usable.map((c) => c.id));
  // An empty library starts from the starter set.
  if (!usable.length) {
    pickedStarters.value = new Set(t.starters.map((_, i) => i));
    source.value = "starter";
  } else source.value = "library";
});
const titleKey = (t: string) => t.trim().toLowerCase().replace(/\s+/g, " ");
const libraryTitles = computed(
  () => new Set((library.data.value ?? []).map((c) => titleKey(c.title))),
);
const known = (title: string) => libraryTitles.value.has(titleKey(title));
const libraryCases = computed(() =>
  (library.data.value ?? []).filter((c) => c.automationStatus !== "obsolete"),
);
function toggled<T>(set: Set<T>, item: T) {
  const next = new Set(set);
  if (next.has(item)) next.delete(item);
  else next.add(item);
  return next;
}
const toggleLibrary = (id: string) =>
  (pickedLibrary.value = toggled(pickedLibrary.value, id));
const toggleStarter = (i: number) =>
  (pickedStarters.value = toggled(pickedStarters.value, i));
const toggleDraft = (i: number) =>
  (pickedDrafts.value = toggled(pickedDrafts.value, i));

// AI drafts
const ai = useResource<AiStatus>();
watch(
  project,
  () => void ai.load((signal) => testmgmt.aiStatus(project.value, signal)),
  { immediate: true },
);
const aiContext = ref("");
const aiCount = ref("8");
const suggestion = shallowRef<CaseSuggestion | null>(null);
const pickedDrafts = ref(new Set<number>());
const mutation = useMutation();
let timer: ReturnType<typeof setTimeout> | undefined;
let pollAbort: AbortController | undefined;
async function draft() {
  if (!type.value) return;
  const created = await mutation.run("ai", (signal) =>
    testmgmt.createSuggestion(
      project.value,
      {
        testType: type.value!.id,
        environmentId: environmentId.value,
        url: "",
        context: aiContext.value.trim(),
        count: Number(aiCount.value),
      },
      signal,
    ),
  );
  if (created) suggestion.value = created;
}
watch(suggestion, (s) => {
  clearTimeout(timer);
  // A draft or starter the library already has (same title) is not
  // ticked: keeping it would make a second copy of the case.
  if (s?.status === "done")
    pickedDrafts.value = new Set(
      s.drafts.flatMap((d, i) => (known(d.title) ? [] : [i])),
    );
  if (s?.status !== "queued") return;
  timer = setTimeout(async () => {
    pollAbort = new AbortController();
    try {
      suggestion.value = await testmgmt.suggestion(
        project.value,
        s.id,
        pollAbort.signal,
      );
    } catch {
      suggestion.value = { ...s }; // try again on the next tick
    }
  }, 3000);
});
onBeforeUnmount(() => {
  clearTimeout(timer);
  pollAbort?.abort();
});

// 4. Plan
const defaultName = (t: TestType) =>
  `${t.label} — ${new Date().toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}`;
const planName = ref("");
const nameEdited = ref(false);
const chosen = computed(() => {
  const drafts =
    suggestion.value?.status === "done"
      ? suggestion.value.drafts.filter((_, i) => pickedDrafts.value.has(i))
      : [];
  const starters = (type.value?.starters ?? []).filter((_, i) =>
    pickedStarters.value.has(i),
  );
  return { library: [...pickedLibrary.value], starters, drafts };
});
const total = computed(
  () =>
    chosen.value.library.length +
    chosen.value.starters.length +
    chosen.value.drafts.length,
);
const progress = ref("");
async function start() {
  const t = type.value;
  if (!t || !planName.value.trim()) return;
  const plan = await mutation.run("start", async (signal) => {
    const ids = [...chosen.value.library];
    const fresh = [
      ...chosen.value.starters.map((c) => ({ ...c, why: "" })),
      ...chosen.value.drafts,
    ];
    for (const [i, c] of fresh.entries()) {
      progress.value = `Adding case ${i + 1} of ${fresh.length}…`;
      const made = await testmgmt.createTestCase(
        project.value,
        {
          title: c.title,
          priority: c.priority,
          preconditions: c.preconditions,
          steps: c.steps,
          taxonomy: taxonomyFor(t.id),
          automationStatus: "manual_only",
        },
        signal,
      );
      ids.push(made.id);
    }
    progress.value = "Creating the plan…";
    return testmgmt.createPlan(
      project.value,
      planName.value.trim(),
      signal,
      ids,
      environmentId.value,
      t.id,
    );
  });
  progress.value = "";
  if (plan)
    await router.push(`${base()}/plans/${encodeURIComponent(plan.id)}/run`);
}
const tabClass = (on: boolean) =>
  [
    "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
    on
      ? "bg-accent text-accent-foreground"
      : "text-muted-foreground hover:text-foreground",
  ].join(" ");
</script>
<template>
  <PageHeader
    eyebrow="Start testing"
    title="New test round"
    description="Choose what kind of testing, the site, and the cases — then go straight to running them."
  />

  <EmptyState
    v-if="!canWrite"
    :icon="TriangleAlert"
    title="You can read this project but not test in it"
    description="A maintainer or admin can make you a member of the project."
  />

  <div v-else class="start-testing space-y-8">
    <section aria-labelledby="start-type" class="space-y-3">
      <h2 id="start-type" class="font-semibold">1. What are you testing?</h2>
      <div
        class="grid gap-2 sm:grid-cols-2 lg:grid-cols-5"
        role="radiogroup"
        aria-labelledby="start-type"
      >
        <button
          v-for="t in testTypes"
          :key="t.id"
          type="button"
          role="radio"
          :aria-checked="type?.id === t.id"
          :class="[
            'bg-card hover:border-primary/60 rounded-lg border p-3 text-left transition-colors',
            type?.id === t.id ? 'border-primary ring-primary/30 ring-2' : '',
          ]"
          @click="type = t"
        >
          <strong class="block text-sm font-semibold">{{ t.label }}</strong>
          <span class="text-muted-foreground mt-1 block text-xs leading-snug">{{
            t.summary
          }}</span>
        </button>
      </div>
    </section>

    <template v-if="type">
      <section aria-labelledby="start-site" class="space-y-3">
        <h2 id="start-site" class="font-semibold">2. Which site?</h2>
        <div class="bg-card max-w-3xl rounded-lg border p-4">
          <EnvironmentPicker
            v-model="environmentId"
            id="start-environment"
            :project="project"
            :can-write="canWrite"
          />
          <p class="text-muted-foreground mt-2 text-xs">
            The run screen opens this address in a new tab, and AI drafting
            reads it.
          </p>
        </div>
      </section>

      <section aria-labelledby="start-cases" class="space-y-3">
        <h2 id="start-cases" class="font-semibold">3. Which cases?</h2>
        <div
          class="flex flex-wrap gap-1"
          role="tablist"
          aria-label="Where the cases come from"
        >
          <button
            type="button"
            role="tab"
            :aria-selected="source === 'library'"
            :class="tabClass(source === 'library')"
            @click="source = 'library'"
          >
            Your library ({{ pickedLibrary.size }}/{{ libraryCases.length }})
          </button>
          <button
            type="button"
            role="tab"
            :aria-selected="source === 'starter'"
            :class="tabClass(source === 'starter')"
            @click="source = 'starter'"
          >
            Starter set ({{ pickedStarters.size }}/{{ type.starters.length }})
          </button>
          <button
            type="button"
            role="tab"
            :aria-selected="source === 'ai'"
            :class="tabClass(source === 'ai')"
            @click="source = 'ai'"
          >
            <Sparkles class="mr-1 inline size-3.5" />AI suggestions<template
              v-if="suggestion?.status === 'done'"
            >
              ({{ pickedDrafts.size }}/{{ suggestion.drafts.length }})</template
            >
          </button>
        </div>

        <div
          v-if="source === 'library'"
          role="tabpanel"
          class="bg-card rounded-lg border"
        >
          <p
            v-if="library.loading.value"
            class="text-muted-foreground p-4 text-sm"
          >
            Looking for {{ type.label.toLowerCase() }} cases…
          </p>
          <ErrorNote
            v-else-if="library.error.value"
            class="m-4"
            :error="library.error.value"
          />
          <p
            v-else-if="!libraryCases.length"
            class="text-muted-foreground p-4 text-sm"
          >
            No case in the library is marked {{ type.label }} yet. Use the
            starter set or AI suggestions — the cases you keep are marked for
            next time.
          </p>
          <ul v-else class="max-h-96 divide-y overflow-y-auto">
            <li v-for="c in libraryCases" :key="c.id">
              <label class="flex cursor-pointer items-start gap-3 px-4 py-2.5">
                <input
                  type="checkbox"
                  class="mt-1 size-4"
                  :checked="pickedLibrary.has(c.id)"
                  @change="toggleLibrary(c.id)"
                />
                <span class="min-w-0 flex-1 text-sm">
                  <span class="font-medium">{{ c.title }}</span>
                  <span class="text-muted-foreground block text-xs"
                    >{{ c.priority.toUpperCase() }} ·
                    {{ c.steps.length }} step{{
                      c.steps.length === 1 ? "" : "s"
                    }}</span
                  >
                </span>
              </label>
            </li>
          </ul>
        </div>

        <div
          v-else-if="source === 'starter'"
          role="tabpanel"
          class="bg-card rounded-lg border"
        >
          <p class="text-muted-foreground border-b px-4 py-2.5 text-xs">
            General {{ type.label.toLowerCase() }} checks to adapt to your
            product. Ticked ones are added to the library as new cases.
          </p>
          <ul class="divide-y">
            <li v-for="(c, i) in type.starters" :key="i">
              <label class="flex cursor-pointer items-start gap-3 px-4 py-2.5">
                <input
                  type="checkbox"
                  class="mt-1 size-4"
                  :checked="pickedStarters.has(i)"
                  @change="toggleStarter(i)"
                />
                <span class="min-w-0 flex-1 text-sm">
                  <span class="font-medium">{{ c.title }}</span>
                  <Badge v-if="known(c.title)" variant="outline" class="ml-2"
                    >already in your library</Badge
                  >
                  <ol
                    class="text-muted-foreground mt-1 list-decimal space-y-0.5 pl-5 text-xs"
                  >
                    <li v-for="(st, j) in c.steps" :key="j">
                      {{ st.action }} → {{ st.expected }}
                    </li>
                  </ol>
                </span>
              </label>
            </li>
          </ul>
        </div>

        <div
          v-else
          role="tabpanel"
          class="bg-card space-y-4 rounded-lg border p-4"
        >
          <p v-if="ai.loading.value" class="text-muted-foreground text-sm">
            Checking whether AI drafting is set up…
          </p>
          <template v-else-if="!ai.data.value?.available">
            <p class="text-sm">
              AI drafting is not set up for this organization. An organization
              admin adds an Anthropic API key and picks the model under
              <strong>Team → organization → AI settings</strong>, or the server
              operator sets <code>LLM_API_KEY</code>.
            </p>
          </template>
          <template v-else>
            <div
              class="grid gap-3 md:grid-cols-[minmax(0,1fr)_8rem_auto] md:items-end"
            >
              <div class="space-y-1.5">
                <Label for="ai-context">What should the cases cover?</Label>
                <textarea
                  id="ai-context"
                  v-model="aiContext"
                  rows="3"
                  maxlength="4000"
                  class="border-input bg-background focus-visible:border-ring focus-visible:ring-ring/50 w-full rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-3"
                  placeholder="e.g. Đăng nhập, giỏ hàng và thanh toán bằng thẻ; người dùng mới và người dùng cũ"
                />
              </div>
              <div class="space-y-1.5">
                <Label for="ai-count">How many</Label>
                <NativeSelect id="ai-count" v-model="aiCount" class="w-full">
                  <option value="5">5</option>
                  <option value="8">8</option>
                  <option value="12">12</option>
                </NativeSelect>
              </div>
              <Button
                type="button"
                :disabled="
                  mutation.busy.value !== null ||
                  suggestion?.status === 'queued'
                "
                @click="draft"
                ><Sparkles />Draft cases</Button
              >
            </div>
            <p class="text-muted-foreground text-xs">
              Uses {{ ai.data.value.model }} with the
              {{
                ai.data.value.source === "organization"
                  ? "organization's"
                  : "server's"
              }}
              API key.
              <template v-if="ai.data.value.readsPages && environmentId"
                >The site's page structure is read and sent with your
                description.</template
              >
              <template v-else
                >Only your description is sent (no site page is read).</template
              >
              Drafts are suggestions — check them before you keep them.
            </p>
            <ErrorNote
              v-if="mutation.error.value?.key === 'ai'"
              :error="explain(mutation.error.value.error)"
            />
            <p
              v-if="suggestion?.status === 'queued'"
              class="text-muted-foreground flex items-center gap-2 text-sm"
              role="status"
            >
              <LoaderCircle class="size-4 animate-spin" />Drafting — usually
              under a minute…
            </p>
            <p
              v-else-if="suggestion?.status === 'error'"
              class="bg-danger-soft text-danger rounded-md px-3 py-2 text-sm"
              role="alert"
            >
              {{ suggestion.error }}
            </p>
            <template v-else-if="suggestion?.status === 'done'">
              <p
                v-if="suggestion.url && !suggestion.pageRead"
                class="text-muted-foreground text-xs"
              >
                The site could not be read, so the drafts come from your
                description alone.
              </p>
              <ul class="divide-y rounded-md border">
                <li v-for="(c, i) in suggestion.drafts" :key="i">
                  <label
                    class="flex cursor-pointer items-start gap-3 px-4 py-2.5"
                  >
                    <input
                      type="checkbox"
                      class="mt-1 size-4"
                      :checked="pickedDrafts.has(i)"
                      @change="toggleDraft(i)"
                    />
                    <span class="min-w-0 flex-1 text-sm">
                      <span class="font-medium">{{ c.title }}</span>
                      <Badge
                        v-if="known(c.title)"
                        variant="outline"
                        class="ml-2"
                        >already in your library</Badge
                      >
                      <Badge variant="outline" class="ml-2">{{
                        c.priority.toUpperCase()
                      }}</Badge>
                      <span
                        v-if="c.why"
                        class="text-muted-foreground block text-xs"
                        >{{ c.why }}</span
                      >
                      <span
                        v-if="c.preconditions"
                        class="text-muted-foreground block text-xs"
                        >Before: {{ c.preconditions }}</span
                      >
                      <ol
                        class="text-muted-foreground mt-1 list-decimal space-y-0.5 pl-5 text-xs"
                      >
                        <li v-for="(st, j) in c.steps" :key="j">
                          {{ st.action }} → {{ st.expected }}
                        </li>
                      </ol>
                    </span>
                  </label>
                </li>
              </ul>
            </template>
          </template>
        </div>
      </section>

      <section aria-labelledby="start-plan" class="space-y-3">
        <h2 id="start-plan" class="font-semibold">4. Start</h2>
        <form
          class="bg-card grid gap-3 rounded-lg border p-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-end"
          @submit.prevent="start"
        >
          <div class="space-y-1.5">
            <Label for="start-name">Plan name</Label>
            <Input
              id="start-name"
              v-model="planName"
              required
              maxlength="200"
              @input="nameEdited = true"
            />
          </div>
          <Button
            type="submit"
            :disabled="mutation.busy.value !== null || total === 0"
            ><Play />Create plan and start ({{ total }})</Button
          >
          <p class="text-muted-foreground text-xs md:col-span-2">
            {{ chosen.library.length }} from your library ·
            {{ chosen.starters.length }} new from the starter set ·
            {{ chosen.drafts.length }} new from AI
            <template v-if="progress"> — {{ progress }}</template>
          </p>
          <ErrorNote
            v-if="mutation.error.value?.key === 'start'"
            class="md:col-span-2"
            :error="explain(mutation.error.value.error)"
          />
        </form>
        <p class="text-muted-foreground text-xs">
          Prefer to build the plan by hand?
          <RouterLink :to="`${base()}/plans`" class="text-primary underline"
            >Test plans<ChevronRight class="inline size-3"
          /></RouterLink>
        </p>
      </section>
    </template>
  </div>
</template>
