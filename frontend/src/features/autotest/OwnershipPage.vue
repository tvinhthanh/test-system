<script setup lang="ts">
import { ShieldCheck } from "@lucide/vue";
import { computed, onBeforeUnmount, ref } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import { ApiError } from "../../core/api/client";
import AsyncState from "../../shared/AsyncState.vue";
import BackLink from "../../shared/BackLink.vue";
import EmptyState from "../../shared/EmptyState.vue";
import PageHeader from "../../shared/PageHeader.vue";
import StatusBadge from "../../shared/StatusBadge.vue";
import TechnicalBlock from "../../shared/TechnicalBlock.vue";
import { time } from "../../shared/format";
import { useProjectPermission } from "../reports/useProjectPermission";
import ErrorNote from "../testmgmt/ErrorNote.vue";
import { usePolledDetail } from "./usePolledDetail";
import { explainLoadError } from "./loadErrors";
import { Button } from "@/components/ui/button";

const route = useRoute();
const { autotest } = useServices();
const project = computed(() => String(route.params.project));
const environment = computed(() => String(route.params.environment));
const canWrite = useProjectPermission("testmgmt.write");
const proofId = computed(() => (canWrite.value ? environment.value : ""));
const base = computed(() => `/p/${encodeURIComponent(project.value)}`);
const proof = usePolledDetail(
  project,
  proofId,
  (p, id, signal) => autotest.ownership(p, id, signal),
  (value) => value.status === "checking",
);
const busy = ref<"file" | "dns" | null>(null);
const actionError = ref<unknown>(null);
const featureOff = ref(false);
let actionAbort: AbortController | undefined;
onBeforeUnmount(() => actionAbort?.abort());

async function check(method: "file" | "dns") {
  if (busy.value || !canWrite.value || featureOff.value) return;
  busy.value = method;
  actionError.value = null;
  const controller = new AbortController();
  actionAbort = controller;
  try {
    proof.replace(
      await autotest.checkOwnership(
        project.value,
        environment.value,
        method,
        controller.signal,
      ),
    );
  } catch (error) {
    if (!controller.signal.aborted) {
      actionError.value = explainLoadError(error);
      if (error instanceof ApiError && error.code === "AUTO_TEST_OFF")
        featureOff.value = true;
    }
  } finally {
    if (actionAbort === controller) {
      actionAbort = undefined;
      busy.value = null;
    }
  }
}
</script>

<template>
  <BackLink :to="`${base}/load-runs`">Load tests</BackLink>
  <PageHeader
    eyebrow="Auto test / host ownership"
    :title="proof.data.value?.host || 'Prove site ownership'"
    description="The organization must prove it owns this exact host before sending load. The server checks again before each test."
  />
  <EmptyState
    v-if="!canWrite"
    :icon="ShieldCheck"
    title="Permission required"
    description="A project tester with test management write access can request and check the proof."
  />
  <template v-else>
    <AsyncState
      :loading="proof.loading.value"
      :error="proof.error.value"
      @retry="proof.reload"
    >
      <template v-if="proof.data.value">
        <div class="mb-4 flex flex-wrap items-center gap-2 text-sm">
          <StatusBadge :status="proof.data.value.status" />
          <span v-if="proof.data.value.method" class="text-muted-foreground"
            >via {{ proof.data.value.method }}</span
          >
          <span v-if="proof.data.value.verifiedAt" class="text-muted-foreground"
            >verified {{ time(proof.data.value.verifiedAt) }}</span
          >
          <span
            v-if="proof.data.value.status === 'checking'"
            class="text-muted-foreground"
            >Checking proof…</span
          >
        </div>
        <ErrorNote
          v-if="proof.data.value.status === 'failed'"
          :error="
            new Error(proof.data.value.error || 'Ownership check failed.')
          "
          :fallback="proof.data.value.error || 'Ownership check failed.'"
          class="mb-4"
        />
        <p
          v-if="featureOff"
          class="bg-warning-soft text-warning mb-4 rounded-md border p-3 text-sm"
        >
          Load tests are off on this server. Ask the operator to enable the
          checker.
        </p>
        <ErrorNote v-if="actionError" :error="actionError" class="mb-4" />
        <ErrorNote
          v-if="proof.pollError.value"
          :error="explainLoadError(proof.pollError.value)"
          class="mb-4"
        />
        <div class="grid gap-4 lg:grid-cols-2">
          <section class="space-y-3 rounded-lg border p-4">
            <div>
              <h2 class="text-base font-semibold">Verification file</h2>
              <p class="text-muted-foreground text-sm">
                Serve this exact text at the URL below. The file must return 200
                without a redirect.
              </p>
            </div>
            <TechnicalBlock title="File URL" :text="proof.data.value.fileUrl" />
            <TechnicalBlock
              title="File contents"
              :text="proof.data.value.fileText"
            />
            <Button
              v-if="!featureOff"
              type="button"
              variant="outline"
              :disabled="
                busy !== null || proof.data.value.status === 'checking'
              "
              @click="check('file')"
              >{{ busy === "file" ? "Checking…" : "Check file" }}</Button
            >
          </section>
          <section class="space-y-3 rounded-lg border p-4">
            <div>
              <h2 class="text-base font-semibold">DNS TXT record</h2>
              <p class="text-muted-foreground text-sm">
                Publish a TXT record with this name and value, then check it.
              </p>
            </div>
            <TechnicalBlock title="DNS name" :text="proof.data.value.dnsName" />
            <TechnicalBlock
              title="TXT value"
              :text="proof.data.value.dnsText"
            />
            <Button
              v-if="!featureOff"
              type="button"
              variant="outline"
              :disabled="
                busy !== null || proof.data.value.status === 'checking'
              "
              @click="check('dns')"
              >{{ busy === "dns" ? "Checking…" : "Check DNS" }}</Button
            >
          </section>
        </div>
        <div class="mt-4">
          <Button
            variant="ghost"
            size="sm"
            :disabled="proof.loading.value"
            @click="proof.reload"
            >Refresh status</Button
          >
        </div>
      </template>
    </AsyncState>
  </template>
</template>
