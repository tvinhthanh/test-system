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
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
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

// "Connect a pipeline": the dashboard and the api share an origin (Caddy in
// a deployment, Vite's /v1 proxy in dev), so the snippets carry the real
// ATP_URL. The token itself never goes into them.
const origin = window.location.origin;
// The reporter tarball this server hands out (QĐ-14); without one, step 2
// tells the reader to ask their ATP admin instead.
const reporterFile = useResource<string | null>();
onMounted(() => reporterFile.load((signal) => repo.reporterPackage(signal)));
const installSnippet = computed(() =>
  reporterFile.data.value
    ? `npm install --save-dev ${origin}/downloads/${reporterFile.data.value}`
    : "",
);
const configSnippet = `// playwright.config.ts
export default defineConfig({
  reporter: [
    ["list"],
    ["@atp/playwright-reporter", { environment: "ci" }],
  ],
});`;
const ciSnippet = `# The token is the repository secret ATP_TOKEN
- run: npx playwright test
  env:
    ATP_URL: ${origin}
    ATP_TOKEN: \${{ secrets.ATP_TOKEN }}`;
const localSnippet = `ATP_URL=${origin} ATP_TOKEN=atp_ing_… npx playwright test`;
const junitSnippet = `curl -X POST "${origin}/v1/imports/junit?environment=ci&framework=junit" \\
  -H "Authorization: Bearer $ATP_TOKEN" \\
  -H "Content-Type: application/xml" \\
  --data-binary @junit.xml`;
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

  <SectionHeading id="token-guide-title" title="Connect a pipeline" />
  <section
    class="token-guide bg-card space-y-5 rounded-lg border p-5 text-sm"
    aria-labelledby="token-guide-title"
  >
    <ol class="space-y-5">
      <li class="space-y-1">
        <p class="font-medium">1. Issue a token above</p>
        <p class="text-muted-foreground">
          Name it after the pipeline that will use it, for example
          <code>github-actions main</code>, and copy it straight away: it is
          shown only once.
        </p>
      </li>
      <li class="space-y-2">
        <p class="font-medium">2. Add the reporter to the Playwright project</p>
        <template v-if="installSnippet">
          <p class="text-muted-foreground">
            This server hands out <code>@atp/playwright-reporter</code>; install
            it from here, no copy of the ATP code needed:
          </p>
          <TechnicalBlock title="Install" :text="installSnippet" />
        </template>
        <p v-else class="text-muted-foreground">
          Install <code>@atp/playwright-reporter</code>. Ask your ATP admin for
          the package — it is packed by <code>server/atp.sh up</code> and served
          at <code>/downloads/</code>.
        </p>
        <p class="text-muted-foreground">Then list it in the config:</p>
        <TechnicalBlock title="playwright.config.ts" :text="configSnippet" />
      </li>
      <li class="space-y-2">
        <p class="font-medium">3. Give the pipeline ATP_URL and ATP_TOKEN</p>
        <p class="text-muted-foreground">
          Keep the token in the CI's secret store, never in the repository.
          ATP_URL is the address of this server.
        </p>
        <TechnicalBlock title="GitHub Actions step" :text="ciSnippet" />
        <TechnicalBlock title="On your own machine" :text="localSnippet" />
      </li>
      <li class="space-y-1">
        <p class="font-medium">4. Run the tests</p>
        <p class="text-muted-foreground">
          The run shows up under
          <RouterLink
            class="text-primary underline"
            :to="`/p/${encodeURIComponent(project)}/runs`"
            >Runs</RouterLink
          >
          as soon as it starts, and the token's “Last used” changes above. Tests
          pass or fail exactly as without the reporter: if ATP_URL or ATP_TOKEN
          is missing, or the server cannot be reached, it only prints a warning.
        </p>
      </li>
    </ol>
    <details class="border-t pt-4">
      <summary class="cursor-pointer font-medium">
        Not Playwright, own certificates, release gates
      </summary>
      <div class="text-muted-foreground mt-3 space-y-3">
        <p>
          Any framework that writes JUnit XML (pytest, Jest, JUnit, …) can send
          its report with the same kind of token:
        </p>
        <TechnicalBlock title="JUnit upload" :text="junitSnippet" />
        <p>
          A server on a local or LAN address uses its own certificate authority.
          Node must trust it: point
          <code>NODE_EXTRA_CA_CERTS</code> at the CA file your ATP admin exports
          with <code>server/atp.sh ca</code>.
        </p>
        <p>
          Tick “Also read release gates” only for a deploy job that asks
          <code>GET /v1/releases/{id}/gate</code> whether a release may ship.
        </p>
      </div>
    </details>
  </section>
</template>
