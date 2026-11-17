<script setup lang="ts">
import { Download, FileUp, Plus, RefreshCw } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import PageHeader from "../../shared/PageHeader.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import AsyncState from "../../shared/AsyncState.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import BackLink from "../../shared/BackLink.vue";
import ErrorNote from "./ErrorNote.vue";
import TmTabs from "./TmTabs.vue";
import ReportFieldsEditor from "./ReportFieldsEditor.vue";
import SpecializedResultForm from "./SpecializedResultForm.vue";
import {
  reportFields,
  reportKinds,
  reportOutcomes,
} from "./specializedReports";
import { useSpecializedResults } from "./useSpecializedResults";
import { time } from "../../shared/format";
const metadata = [
  { key: "title", label: "Report title", max: 500 },
  { key: "environment", label: "Environment", max: 128 },
  { key: "applicationVersion", label: "Application version", max: 128 },
  { key: "source", label: "Source / tool", max: 128 },
  { key: "toolVersion", label: "Tool version", max: 128 },
  { key: "adapterVersion", label: "Adapter version", max: 128 },
] as const;

const {
  testmgmt,
  project,
  selected,
  kind,
  outcome,
  cursor,
  base,
  canWrite,
  list,
  detail,
  mutation,
  panel,
  draft,
  refresh,
  filter,
  openNew,
  retest,
  persist,
  importFile,
} = useSpecializedResults();
</script>
<template>
  <BackLink v-if="selected" :to="base()">Specialized results</BackLink>
  <PageHeader
    eyebrow="Test management"
    :title="
      selected ? (detail.data.value?.title ?? 'Result') : 'Specialized results'
    "
  >
    <Button
      v-if="canWrite"
      size="sm"
      :disabled="mutation.busy.value !== null"
      @click="openNew"
      ><Plus />New result</Button
    >
    <Button v-if="canWrite" as-child variant="outline" size="sm"
      ><label
        ><FileUp />Import ATP JSON<input
          type="file"
          class="sr-only"
          accept=".json,application/json"
          :disabled="mutation.busy.value !== null"
          @change="importFile" /></label
    ></Button>
    <Button
      variant="outline"
      size="icon-sm"
      title="Refresh results"
      aria-label="Refresh results"
      @click="
        selected
          ? detail.load((signal) =>
              testmgmt.specializedResult(project, selected, signal),
            )
          : refresh()
      "
      ><RefreshCw
    /></Button>
  </PageHeader>
  <TmTabs :project="project" current="specialized" />
  <ErrorNote
    v-if="mutation.error.value"
    class="mb-4"
    :error="mutation.error.value.error"
  />
  <SpecializedResultForm
    v-if="panel && !selected"
    :key="draft.idempotencyKey"
    :initial="draft"
    :disabled="mutation.busy.value !== null"
    @submit="persist"
  />
  <AsyncState
    v-if="selected"
    :loading="detail.loading.value"
    :error="detail.error.value"
    @retry="
      detail.load((signal) =>
        testmgmt.specializedResult(project, selected, signal),
      )
    "
  >
    <article
      v-if="detail.data.value"
      class="min-w-0 space-y-5"
      aria-label="Specialized result"
    >
      <div class="flex flex-wrap items-center gap-3">
        <StatusBadge :status="detail.data.value.outcome" /><span
          class="text-muted-foreground text-sm"
          >{{ detail.data.value.kind }} · {{ detail.data.value.createdBy }} ·
          {{ time(detail.data.value.createdAt) }}</span
        >
      </div>
      <ul
        v-if="detail.data.value.reasons.length"
        class="border-l-2 pl-4 text-sm"
      >
        <li
          v-for="reason in detail.data.value.reasons"
          :key="reason"
          class="break-words"
        >
          {{ reason }}
        </li>
      </ul>
      <dl
        class="grid min-w-0 gap-3 border-y py-4 text-sm sm:grid-cols-2 xl:grid-cols-3"
      >
        <div
          v-for="field in metadata.slice(1)"
          :key="field.key"
          class="min-w-0"
        >
          <dt class="text-muted-foreground text-xs">{{ field.label }}</dt>
          <dd class="break-words">{{ detail.data.value.input[field.key] }}</dd>
        </div>
        <div>
          <dt class="text-muted-foreground text-xs">Execution</dt>
          <dd v-if="detail.data.value.input.state === 'completed'">
            {{ time(detail.data.value.input.startedAt) }} →
            {{ time(detail.data.value.input.finishedAt) }}
          </dd>
          <dd v-else>Not recorded</dd>
        </div>
      </dl>
      <p v-if="detail.data.value.input.testCaseId" class="text-sm">
        <RouterLink
          :to="`/p/${project}/cases/${detail.data.value.input.testCaseId}`"
          class="underline"
          >Case {{ detail.data.value.input.testCaseId }}</RouterLink
        >
      </p>
      <p v-if="detail.data.value.input.retestOf" class="text-sm">
        <RouterLink
          :to="{
            path: base(),
            query: { result: detail.data.value.input.retestOf },
          }"
          class="underline"
          >Previous result</RouterLink
        >
      </p>
      <ReportFieldsEditor
        :fields="reportFields[detail.data.value.kind]"
        :model-value="detail.data.value.input.details"
        disabled
      />
      <p
        v-if="detail.data.value.input.notes"
        class="whitespace-pre-wrap break-words text-sm"
      >
        {{ detail.data.value.input.notes }}
      </p>
      <div class="flex flex-wrap gap-2">
        <Button as-child variant="outline" size="sm"
          ><a :href="testmgmt.specializedOriginalUrl(project, selected)"
            ><Download />Original report</a
          ></Button
        ><Button v-if="canWrite" size="sm" @click="retest"
          ><Plus />Record retest</Button
        >
      </div>
    </article>
  </AsyncState>
  <template v-else>
    <div class="mb-4 flex flex-wrap gap-3">
      <label class="grid gap-1 text-xs"
        >Group<NativeSelect
          :model-value="kind"
          aria-label="Filter group"
          @update:model-value="filter({ kind: $event })"
          ><option value="">All groups</option>
          <option v-for="item in reportKinds" :key="item" :value="item">
            {{ item }}
          </option></NativeSelect
        ></label
      ><label class="grid gap-1 text-xs"
        >Outcome<NativeSelect
          :model-value="outcome"
          aria-label="Filter outcome"
          @update:model-value="filter({ outcome: $event })"
          ><option value="">All outcomes</option>
          <option v-for="item in reportOutcomes" :key="item" :value="item">
            {{ item.replaceAll("_", " ") }}
          </option></NativeSelect
        ></label
      >
    </div>
    <AsyncState
      :loading="list.loading.value"
      :error="list.error.value"
      @retry="refresh()"
    >
      <p
        v-if="!list.data.value?.items.length"
        class="text-muted-foreground border-y py-8 text-sm"
      >
        No results
      </p>
      <ul v-else class="divide-y border-y">
        <li v-for="result in list.data.value.items" :key="result.id">
          <RouterLink
            :to="{ path: base(), query: { kind, outcome, result: result.id } }"
            class="hover:bg-accent grid min-w-0 gap-2 px-2 py-3 sm:grid-cols-[minmax(0,1fr)_auto]"
            :data-kind="result.kind"
            :data-outcome="result.outcome"
            ><div class="min-w-0">
              <strong class="block truncate text-sm font-medium">{{
                result.title
              }}</strong
              ><span class="text-muted-foreground block truncate text-xs"
                >{{ result.kind }} · {{ result.input.environment }} ·
                {{ result.input.applicationVersion }} ·
                {{ time(result.createdAt) }}</span
              >
            </div>
            <StatusBadge :status="result.outcome"
          /></RouterLink>
        </li>
      </ul>
      <Button
        v-if="list.data.value?.nextCursor"
        variant="outline"
        size="sm"
        class="mt-4"
        @click="filter({ cursor: list.data.value!.nextCursor })"
        >Next page</Button
      >
      <Button
        v-if="cursor"
        variant="outline"
        size="sm"
        class="mt-4 ml-2"
        @click="filter({ cursor: '' })"
        >First page</Button
      >
    </AsyncState>
  </template>
</template>
