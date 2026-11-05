<script setup lang="ts">
import { computed, ref } from "vue";
import type { LoadResult } from "./autotestRepository";
import { Button } from "@/components/ui/button";

const props = defineProps<{ result: LoadResult }>();
const page = ref(0);
const pageSize = 30;
const series = computed(() => props.result.timeline);
const max = computed(() =>
  Math.max(1, ...series.value.map((point) => point.requests)),
);
const coordinates = (key: "requests" | "errors") =>
  series.value
    .map(
      (point, index) =>
        `${series.value.length === 1 ? 0 : (index / (series.value.length - 1)) * 100},${100 - (point[key] / max.value) * 90}`,
    )
    .join(" ");
const requests = computed(() => coordinates("requests"));
const errors = computed(() => coordinates("errors"));
const rows = computed(() =>
  series.value.slice(page.value * pageSize, (page.value + 1) * pageSize),
);
const pages = computed(() => Math.ceil(series.value.length / pageSize));
</script>

<template>
  <section class="space-y-3" aria-label="Per-second load timeline">
    <div class="flex flex-wrap items-center gap-4">
      <h2 class="text-base font-semibold">Requests by second</h2>
      <span class="text-primary text-xs">● Requests</span>
      <span class="text-danger text-xs">● Errors (no response or 5xx)</span>
    </div>
    <div
      v-if="series.length"
      class="bg-card overflow-hidden rounded-lg border p-4"
    >
      <svg
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        class="h-44 w-full"
        role="img"
        :aria-label="`Per-second requests and errors over ${series.length} seconds`"
      >
        <line
          x1="0"
          y1="100"
          x2="100"
          y2="100"
          class="stroke-border"
          stroke-width="1"
        />
        <polyline
          :points="requests"
          class="fill-none stroke-current text-primary"
          stroke-width="1.5"
          vector-effect="non-scaling-stroke"
        />
        <polyline
          :points="errors"
          class="fill-none stroke-current text-danger"
          stroke-width="1.5"
          vector-effect="non-scaling-stroke"
        />
      </svg>
      <div class="text-muted-foreground flex justify-between text-xs">
        <span>0s</span><span>{{ series.length }}s</span>
      </div>
    </div>
    <p v-else class="text-muted-foreground text-sm">
      No per-second samples were returned.
    </p>
    <div v-if="series.length" class="overflow-x-auto rounded-lg border">
      <table class="w-full text-left text-sm tabular-nums">
        <caption class="sr-only">
          Per-second request counts, errors and average response time
        </caption>
        <thead class="bg-muted/60 text-muted-foreground">
          <tr>
            <th scope="col" class="px-3 py-2">Second</th>
            <th scope="col" class="px-3 py-2">Requests</th>
            <th scope="col" class="px-3 py-2">Errors</th>
            <th scope="col" class="px-3 py-2">Average</th>
          </tr>
        </thead>
        <tbody class="divide-y">
          <tr v-for="point in rows" :key="point.t">
            <td class="px-3 py-2">{{ point.t + 1 }}</td>
            <td class="px-3 py-2">{{ point.requests }}</td>
            <td class="px-3 py-2">{{ point.errors }}</td>
            <td class="px-3 py-2">{{ point.avgMs }} ms</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="pages > 1" class="flex items-center justify-end gap-2 text-sm">
      <Button
        type="button"
        variant="outline"
        size="sm"
        :disabled="page === 0"
        @click="page--"
        >Previous</Button
      >
      <span class="text-muted-foreground">{{ page + 1 }} of {{ pages }}</span>
      <Button
        type="button"
        variant="outline"
        size="sm"
        :disabled="page + 1 >= pages"
        @click="page++"
        >Next</Button
      >
    </div>
  </section>
</template>
