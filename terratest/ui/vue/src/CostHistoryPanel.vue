<template>
  <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
    <div
      v-for="card in totalCards"
      :key="card.label"
      class="rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-3 dark:border-white/10 dark:bg-white/[0.03]"
    >
      <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
        {{ card.label }}
      </div>
      <div class="mt-1 text-2xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">
        {{ formatUSD(card.value) }}
      </div>
      <div class="mt-1 text-xs text-zinc-500 dark:text-zinc-400">Estimated AWS cleanup cost</div>
    </div>
  </div>

  <div class="mt-4 overflow-hidden rounded-xl border border-zinc-200 dark:border-white/10">
    <div
      v-if="costs?.error"
      class="border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-200"
    >
      Cost history unavailable: {{ costs.error }}
    </div>

    <div
      v-else-if="!entries.length"
      class="bg-zinc-50 p-4 text-sm text-zinc-600 dark:bg-white/[0.03] dark:text-zinc-400"
    >
      No persisted cost estimates yet. Successful destroys will add estimated EC2, EBS, RDS/Aurora, and load balancer cost rows here.
    </div>

    <div v-else class="overflow-x-auto">
      <table class="min-w-full divide-y divide-zinc-200 text-left text-sm dark:divide-white/10">
        <thead class="bg-zinc-50 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:bg-white/[0.03] dark:text-zinc-400">
          <tr>
            <th v-for="label in tableLabels" :key="label" class="px-4 py-3">{{ label }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-zinc-200 bg-white dark:divide-white/10 dark:bg-white/[0.02]">
          <tr v-for="entry in entries" :key="entryKey(entry)">
            <td class="px-4 py-3 font-semibold text-zinc-900 dark:text-zinc-100">
              {{ entry.runId || "unknown" }}
              <div v-if="entry.awsPrefix" class="mt-1 text-xs font-medium text-zinc-500 dark:text-zinc-400">
                {{ entry.awsPrefix }}
              </div>
            </td>
            <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">{{ finishedAt(entry) }}</td>
            <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">{{ entry.owner || "not recorded" }}</td>
            <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">{{ entry.region || "unknown" }}</td>
            <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">{{ Number(entry.totalRuntimeHours || 0).toFixed(2) }}h</td>
            <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">{{ formatUSD(entry.ec2CostUsd) }}</td>
            <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">{{ formatUSD(entry.ebsCostUsd) }}</td>
            <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">{{ formatUSD(entry.rdsCostUsd) }}</td>
            <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">{{ formatUSD(entry.loadBalancerCostUsd) }}</td>
            <td class="px-4 py-3 font-semibold text-zinc-950 dark:text-zinc-50">{{ formatUSD(entry.totalCostUsd) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";

const state = ref(window.rancherControlPanelState || {});
const tableLabels = ["Run", "Finished", "Owner", "Region", "Runtime", "EC2", "EBS", "RDS", "LB", "Total"];

const costs = computed(() => state.value?.costs || {});
const entries = computed(() => Array.isArray(costs.value?.entries) ? costs.value.entries : []);
const totals = computed(() => costs.value?.totals || {});
const totalCards = computed(() => [
  { label: "Lifetime", value: totals.value.lifetime },
  { label: "This month", value: totals.value.month },
  { label: "This week", value: totals.value.week },
  { label: "Today", value: totals.value.today },
]);

const formatUSD = value => {
  const number = Number(value || 0);
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: number >= 100 ? 0 : 2,
  }).format(number);
};

const finishedAt = entry => entry.finishedAt ? new Date(entry.finishedAt).toLocaleString() : "not recorded";

const entryKey = entry => [
  entry.runId || "unknown",
  entry.finishedAt || "",
  entry.totalCostUsd || "",
].join(":");

const handleStateEvent = event => {
  state.value = event.detail?.state || {};
};

onMounted(() => {
  window.addEventListener("rancher-control-panel:state", handleStateEvent);
});

onUnmounted(() => {
  window.removeEventListener("rancher-control-panel:state", handleStateEvent);
});
</script>
