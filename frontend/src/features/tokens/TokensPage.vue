<script setup lang="ts">
// CI tokens for a project (SPEC §11.3.2, §11.4): issue one per pipeline,
// see when each was last used, revoke one that leaked or is no longer
// needed. The secret is shown once, right after it is issued — the server
// keeps only its hash. Needs token.manage (maintainer and up).
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { Check, Copy, KeyRound, Plus } from "@lucide/vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type {
  IssuedToken,
  ProjectToken,
  TokenPermission,
} from "./tokensRepository";
import { useMutation } from "../testmgmt/useMutation";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import AsyncState from "../../shared/AsyncState.vue";
import EmptyState from "../../shared/EmptyState.vue";
import NativeSelect from "../../shared/NativeSelect.vue";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";
import { day, time } from "../../shared/format";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
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

const route = useRoute();
const { tokens: repo } = useServices();
const project = computed(() => String(route.params.project ?? ""));
const list = useResource<ProjectToken[]>();
const refresh = () => list.load((signal) => repo.list(project.value, signal));
onMounted(refresh);

const name = ref("");
const canReadGate = ref(false);
const days = ref("365");
const issued = ref<IssuedToken | null>(null);
const copied = ref(false);
const mutation = useMutation();
const confirming = ref<string | null>(null);

async function issue() {
  copied.value = false;
  const permissions: TokenPermission[] = canReadGate.value
    ? ["project:ingest", "project:read"]
    : ["project:ingest"];
  const out = await mutation.run("issue", (signal) =>
    repo.issue(
      project.value,
      {
        name: name.value.trim(),
        permissions,
        expiresInDays: Number(days.value),
      },
      signal,
    ),
  );
  if (out) {
    issued.value = out;
    name.value = "";
    await refresh();
  }
}
async function copy() {
  if (!issued.value) return;
  try {
    await navigator.clipboard.writeText(issued.value.secret);
    copied.value = true;
  } catch {
    copied.value = false; // the field stays selectable
  }
}
async function revoke(t: ProjectToken) {
  if (confirming.value !== t.id) {
    confirming.value = t.id;
    return;
  }
  confirming.value = null;
  const done = await mutation.run(t.id, async (signal) => {
    await repo.revoke(project.value, t.id, signal);
    return true;
  });
  if (done) await refresh();
}
const state = (t: ProjectToken) =>
  t.revokedAt
    ? "revoked"
    : Date.parse(t.expiresAt) < Date.now()
      ? "expired"
      : "active";
</script>

<template>
  <PageHeader
    eyebrow="Project settings"
    title="CI tokens"
    description="One token per pipeline, so one can be revoked without stopping the others. The reporter reads it from ATP_TOKEN."
  />

  <section
    class="token-new bg-card mb-6 space-y-4 rounded-lg border p-5"
    aria-labelledby="token-new-title"
  >
    <h2 id="token-new-title" class="font-semibold">New token</h2>
    <form
      class="grid gap-3 md:grid-cols-[1fr_auto_auto] md:items-end"
      @submit.prevent="issue"
    >
      <div class="flex flex-col gap-1.5">
        <Label for="token-name">Name</Label>
        <Input
          id="token-name"
          v-model="name"
          maxlength="100"
          placeholder="github-actions main"
          required
        />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="token-days">Expires in</Label>
        <NativeSelect id="token-days" v-model="days" class="w-36">
          <option value="30">30 days</option>
          <option value="90">90 days</option>
          <option value="365">1 year</option>
        </NativeSelect>
      </div>
      <Button type="submit" :disabled="!!mutation.busy.value"
        ><Plus />Issue token</Button
      >
      <label class="flex items-center gap-2 text-sm md:col-span-3">
        <input v-model="canReadGate" type="checkbox" class="size-4" />
        Also read release gates (for a pipeline that checks
        <code>/v1/releases/…/gate</code> before deploying)
      </label>
    </form>
    <ErrorNote
      v-if="mutation.error.value?.key === 'issue'"
      :error="mutation.error.value.error"
    />
    <Alert v-if="issued" role="status" class="token-secret">
      <KeyRound />
      <AlertTitle>Copy “{{ issued.token.name }}” now</AlertTitle>
      <AlertDescription class="block space-y-2">
        <p>It is shown only this once; the server keeps only its hash.</p>
        <div class="flex gap-2">
          <Input
            :model-value="issued.secret"
            readonly
            class="font-mono text-xs"
            aria-label="Token"
            @focus="($event.target as HTMLInputElement).select()"
          />
          <Button variant="outline" size="sm" type="button" @click="copy"
            ><Check v-if="copied" /><Copy v-else />{{
              copied ? "Copied" : "Copy"
            }}</Button
          >
        </div>
        <p class="text-xs">
          In CI: set a secret <code>ATP_TOKEN</code> to this value; the reporter
          reads it with <code>ATP_URL</code>.
        </p>
      </AlertDescription>
    </Alert>
  </section>

  <SectionHeading title="Tokens" />
  <AsyncState
    :loading="list.loading.value"
    :error="list.error.value"
    @retry="refresh"
  >
    <EmptyState
      v-if="!list.data.value?.length"
      :icon="KeyRound"
      title="No tokens yet"
      description="Issue one above for each CI pipeline that reports to this project."
    />
    <div v-else class="table-scroll bg-card overflow-x-auto rounded-lg border">
      <Table class="token-table min-w-[760px]">
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead>Name</TableHead>
            <TableHead>Token</TableHead>
            <TableHead>Can</TableHead>
            <TableHead>Last used</TableHead>
            <TableHead>Expires</TableHead>
            <TableHead class="w-40"
              ><span class="sr-only">Revoke</span></TableHead
            >
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="t in list.data.value" :key="t.id">
            <TableCell class="font-medium">
              {{ t.name }}
              <Badge
                v-if="state(t) !== 'active'"
                variant="outline"
                class="ml-2"
                >{{ state(t) }}</Badge
              >
            </TableCell>
            <TableCell class="font-mono text-xs">{{ t.prefix }}…</TableCell>
            <TableCell class="text-xs">{{
              t.permissions
                .map((p) =>
                  p === "project:ingest" ? "send runs" : "read gates",
                )
                .join(", ")
            }}</TableCell>
            <TableCell class="text-muted-foreground text-xs">{{
              t.lastUsedAt ? time(t.lastUsedAt) : "never"
            }}</TableCell>
            <TableCell class="text-muted-foreground text-xs">{{
              day(t.expiresAt)
            }}</TableCell>
            <TableCell class="text-right">
              <Button
                v-if="state(t) === 'active'"
                :variant="confirming === t.id ? 'destructive' : 'ghost'"
                size="sm"
                :disabled="mutation.busy.value === t.id"
                @click="revoke(t)"
                >{{ confirming === t.id ? "Confirm revoke" : "Revoke" }}</Button
              >
              <ErrorNote
                v-if="mutation.error.value?.key === t.id"
                :error="mutation.error.value.error"
              />
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </AsyncState>
</template>
