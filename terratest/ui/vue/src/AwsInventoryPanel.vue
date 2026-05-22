<template>
  <div class="mb-4 flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
    <div>
      <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">AWS Inventory</h2>
      <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
        {{ summary }}
      </p>
    </div>
    <div class="text-sm font-medium text-zinc-500 dark:text-zinc-400">
      {{ updatedLabel }}
    </div>
  </div>

  <div class="grid min-w-0 gap-3">
    <div
      v-if="inventory?.error"
      class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200"
    >
      {{ inventory.error }}
    </div>

    <div
      v-if="!items.length"
      class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400"
    >
      No matching AWS resources found for the recorded run prefixes or Owner tag.
    </div>

    <template v-else>
      <div class="flex flex-wrap gap-2">
        <span
          v-for="badge in countBadges"
          :key="badge.type"
          class="inline-flex items-center rounded-md bg-zinc-100 px-2 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300"
        >
          {{ badge.type }}: {{ badge.count }}
        </span>
      </div>

      <div class="overflow-hidden rounded-xl border border-zinc-200 dark:border-white/10">
        <table class="w-full table-fixed border-collapse text-left">
          <colgroup>
            <col class="w-[11rem]" />
            <col class="w-[18rem]" />
            <col class="w-[9rem]" />
            <col class="w-[9rem]" />
            <col />
          </colgroup>
          <thead class="bg-zinc-50 dark:bg-white/[0.04]">
            <tr>
              <th
                v-for="label in tableLabels"
                :key="label"
                class="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400"
              >
                {{ label }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-zinc-200 dark:divide-white/10">
            <tr v-for="item in items" :key="itemKey(item)">
              <td class="break-words px-3 py-3 align-top text-sm font-semibold text-zinc-900 dark:text-zinc-100">
                {{ item.type || "AWS resource" }}
              </td>
              <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">
                <div class="font-medium">{{ item.name || item.id || "" }}</div>
                <div class="mt-1 text-xs text-zinc-500 dark:text-zinc-500">{{ item.region || "" }}</div>
              </td>
              <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">
                {{ item.status || "" }}
              </td>
              <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">
                {{ item.runId || "" }}
              </td>
              <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">
                <div>{{ item.details || item.id || "" }}</div>
                <div v-if="item.owner" class="mt-1 text-xs text-zinc-500 dark:text-zinc-500">
                  Owner {{ item.owner }}
                </div>
                <div v-if="tagsFor(item)" class="mt-1 text-xs text-zinc-500 dark:text-zinc-500">
                  {{ tagsFor(item) }}
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";

const state = ref(window.rancherControlPanelState || {});
const tableLabels = ["Type", "Name", "Status", "Run", "Details"];

const inventory = computed(() => state.value?.aws || {});
const items = computed(() => Array.isArray(inventory.value?.items) ? inventory.value.items : []);

const updatedLabel = computed(() => (
  inventory.value?.updatedAt ? `Updated ${new Date(inventory.value.updatedAt).toLocaleTimeString()}` : ""
));

const summary = computed(() => {
  if (!state.value?.aws) {
    return "Loading AWS inventory...";
  }

  const queries = Array.isArray(inventory.value?.queries) ? inventory.value.queries : [];
  const queryText = queries.length ? queries.join(" • ") : "No scoped AWS query yet";
  const owner = inventory.value?.owner ? `Owner ${inventory.value.owner}` : "Owner tag not configured";
  const region = inventory.value?.region || "region unavailable";
  const count = items.value.length;
  return `${count} matching AWS resource${count === 1 ? "" : "s"} in ${region}. ${owner}. ${queryText}.`;
});

const countBadges = computed(() => {
  const counts = items.value.reduce((acc, item) => {
    const type = item.type || "AWS resource";
    acc[type] = (acc[type] || 0) + 1;
    return acc;
  }, {});

  return Object.entries(counts)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([type, count]) => ({ type, count }));
});

const tagsFor = item => (
  item.tags
    ? Object.entries(item.tags).slice(0, 5).map(([key, value]) => `${key}=${value}`).join(" • ")
    : ""
);

const itemKey = item => [
  item.type || "AWS resource",
  item.id || item.name || "",
  item.region || "",
  item.runId || "",
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
