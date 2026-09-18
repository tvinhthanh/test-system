<script setup lang="ts">
import {
  ArrowLeft,
  Calendar,
  GitMerge,
  RefreshCw,
  Split,
  Undo2,
} from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import { useResource } from "../../core/state/useResource";
import { useIdentityActions } from "./useIdentityActions";
import { time } from "../../shared/format";
import AsyncState from "../../shared/AsyncState.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
const route = useRoute(),
  router = useRouter();
const { reports } = useServices();
const project = computed(() => String(route.params.project));
const identityId = computed(() => String(route.params.identity));
const identity = useResource<Awaited<ReturnType<typeof reports.identity>>>();
const history = useResource<Awaited<ReturnType<typeof reports.runs>>>();
const identityData = computed(() => identity.data.value);
const historyData = computed(() => history.data.value);
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
    await router.push(
      `/p/${encodeURIComponent(project.value)}/tests/${encodeURIComponent(newId)}/history`,
    );
}
const reason = (e: unknown) =>
  e instanceof ApiError ? e.message : "The action could not be completed.";
</script>
<template>
  <RouterLink class="back-link" :to="`/p/${encodeURIComponent(project)}/tests`"
    ><ArrowLeft :size="16" />Test history</RouterLink
  >
  <header class="page-heading">
    <div>
      <p class="eyebrow">IDENTITY HISTORY</p>
      <h1>{{ identityData?.canonicalTitle ?? identityId }}</h1>
      <p class="muted">
        <code>{{ identityData?.canonicalFilePath }}</code>
      </p>
    </div>
    <button
      class="icon-button"
      aria-label="Refresh identity history"
      title="Refresh identity history"
      @click="refresh"
    >
      <RefreshCw :size="18" />
    </button>
  </header>
  <AsyncState
    :loading="identity.loading.value"
    :error="identity.error.value"
    @retry="refresh"
    ><template v-if="identityData"
      ><div
        v-if="identityData.status === 'merged'"
        class="notice"
        role="status"
      >
        <GitMerge :size="16" />
        <span
          >This identity was merged into
          <RouterLink
            v-if="identityData.mergedIntoId"
            :to="`/p/${encodeURIComponent(project)}/tests/${encodeURIComponent(identityData.mergedIntoId)}/history`"
            >another test</RouterLink
          ><template v-else>another test</template>. Its history lives there
          now.</span
        >
        <button
          type="button"
          class="button quiet"
          :disabled="actions.busy.value !== null"
          @click="actions.undo"
        >
          <Undo2 :size="16" />Undo merge
        </button>
      </div>
      <div
        v-else-if="identityData.status === 'retired'"
        class="notice"
        role="status"
      >
        <Split :size="16" />
        <span
          >Retired: this test was split and a newer identity owns its
          fingerprint now. History before the split stays here.</span
        >
      </div>
      <StatusBadge :status="identityData.verdict" />
      <div class="metrics">
        <div>
          <span>Runs</span><strong>{{ identityData.totalRuns }}</strong>
        </div>
        <div>
          <span>Flaky score</span
          ><strong>{{
            identityData.flakyScore === null
              ? "N/A"
              : `${Math.round(identityData.flakyScore * 100)}%`
          }}</strong>
        </div>
      </div>
      <div class="history-large">
        <i
          v-for="(status, index) in identityData.recentStatuses"
          :key="`${status}-${index}`"
          :class="status.toLowerCase().replace('_', '-')"
          :title="status"
        /></div></template
  ></AsyncState>

  <div class="section-heading">
    <h2>Fingerprints</h2>
    <span class="muted"
      >{{ actions.inForce.value.length }} in force ·
      {{
        (actions.aliases.data.value?.length ?? 0) - actions.inForce.value.length
      }}
      historical</span
    >
  </div>
  <AsyncState
    :loading="actions.aliases.loading.value"
    :error="actions.aliases.error.value"
    @retry="actions.loadAliases"
    ><table v-if="actions.aliases.data.value?.length" class="alias-table">
      <thead>
        <tr>
          <th scope="col">Fingerprint</th>
          <th scope="col">Source</th>
          <th scope="col">Match</th>
          <th scope="col">From</th>
          <th scope="col">To</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="a in actions.aliases.data.value" :key="a.id">
          <td>
            <code>{{ a.fingerprint }}</code>
          </td>
          <td>{{ a.source }}</td>
          <td>
            {{ a.source === "exact" ? "—" : `${Math.round(a.matchScore * 100)}%` }}
          </td>
          <td>{{ time(a.validFrom) }}</td>
          <td>{{ a.validTo ? time(a.validTo) : "in force" }}</td>
        </tr>
      </tbody>
    </table>
    <p v-else class="muted">No fingerprint recorded.</p>
  </AsyncState>

  <section
    v-if="identityData && identityData.status === 'active'"
    class="tools"
    aria-labelledby="tools-title"
  >
    <div class="section-heading">
      <h2 id="tools-title">Identity tools</h2>
      <span class="muted">admin · audited</span>
    </div>
    <div class="tool-tabs">
      <button
        type="button"
        class="button quiet"
        :class="{ selected: tools === 'merge' }"
        @click="tools = tools === 'merge' ? '' : 'merge'"
      >
        <GitMerge :size="16" />Merge into another test
      </button>
      <button
        type="button"
        class="button quiet"
        :class="{ selected: tools === 'split' }"
        @click="tools = tools === 'split' ? '' : 'split'"
      >
        <Split :size="16" />Split from a point in time
      </button>
    </div>
    <div v-if="tools === 'merge'" class="tool-panel">
      <p class="muted">
        Use when the resolver missed a rename: every fingerprint of this
        identity moves to the chosen test and the two histories become one. Undo
        puts it back exactly.
      </p>
      <label class="field"
        ><span>Filter tests</span
        ><input
          v-model="candidateFilter"
          type="search"
          placeholder="title or file path"
      /></label>
      <AsyncState
        :loading="actions.candidates.loading.value"
        :error="actions.candidates.error.value"
        @retry="actions.loadCandidates"
        ><ul class="candidate-list" role="radiogroup" aria-label="Merge into">
          <li v-for="c in visibleCandidates.slice(0, 20)" :key="c.id">
            <label
              ><input
                v-model="actions.mergeTarget.value"
                type="radio"
                name="merge-target"
                :value="c.id"
              /><strong>{{ c.canonicalTitle }}</strong
              ><code>{{ c.canonicalFilePath }}</code></label
            >
          </li>
        </ul></AsyncState
      >
      <button
        type="button"
        class="button primary"
        :disabled="!actions.mergeTarget.value || actions.busy.value !== null"
        @click="actions.merge"
      >
        <GitMerge :size="16" />Merge this identity into the selected test
      </button>
    </div>
    <div v-if="tools === 'split'" class="tool-panel">
      <p class="muted">
        Use when the same fingerprint started meaning a different test at some
        point: the alias closes there and a new identity owns it from then on.
        History before that point stays here. This does not undo a merge — use
        Undo for that.
      </p>
      <label class="field"
        ><span>Fingerprint</span
        ><select v-model="actions.splitFingerprint.value">
          <option value="">Choose a fingerprint in force</option>
          <option
            v-for="a in actions.inForce.value"
            :key="a.id"
            :value="a.fingerprint"
          >
            {{ a.fingerprint }}
          </option>
        </select></label
      >
      <label class="field"
        ><span>Diverged at</span
        ><input v-model="actions.splitAt.value" type="datetime-local"
      /></label>
      <button
        type="button"
        class="button primary"
        :disabled="
          !actions.splitFingerprint.value ||
          !actions.splitAt.value ||
          actions.busy.value !== null
        "
        @click="doSplit"
      >
        <Split :size="16" />Split here
      </button>
    </div>
    <p v-if="actions.actionError.value" class="error-text" role="alert">
      {{ reason(actions.actionError.value) }}
    </p>
  </section>

  <div class="section-heading">
    <h2>Recent runs</h2>
    <Calendar :size="18" />
  </div>
  <AsyncState
    :loading="history.loading.value"
    :error="history.error.value"
    @retry="refresh"
    ><div v-if="!historyData?.items.length" class="empty-state compact">
      <p>No matching history runs.</p>
    </div>
    <div v-else class="history-runs">
      <RouterLink
        v-for="run in historyData.items"
        :key="run.id"
        class="event-row"
        :to="`/p/${encodeURIComponent(project)}/runs/${encodeURIComponent(run.id)}`"
        ><StatusBadge :status="run.status" :flaky="run.flakyCount" /><span
          >{{ run.branch }} / {{ run.environmentName }}</span
        ><span class="muted">{{ run.startedAt }}</span></RouterLink
      >
    </div></AsyncState
  >
</template>
