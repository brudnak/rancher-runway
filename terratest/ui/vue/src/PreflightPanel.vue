<template>
  <details
    :class="{ hidden: !items.length }"
    class="mb-5 rounded-xl border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.02]"
  >
    <summary class="flex cursor-pointer list-none flex-wrap items-center justify-between gap-3">
      <div class="flex min-w-0 items-center gap-3">
        <h3 class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">Preflight</h3>
        <div :class="statusClass">
          <span v-if="checking" class="spinner mr-2"></span>{{ statusLabel }}
        </div>
      </div>
      <button
        type="button"
        @click="refreshPreflight"
        :disabled="checking"
        class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
      >
        Refresh
      </button>
    </summary>

    <div class="mt-3 grid gap-2 text-sm sm:grid-cols-2 xl:grid-cols-3">
      <div
        v-if="!items.length"
        class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-500 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400"
      >
        No preflight results yet.
      </div>
      <div
        v-for="item in visibleItems"
        :key="`${item.name || 'Preflight'}:${item.status || 'unknown'}`"
        class="rounded-lg border px-3 py-2"
        :class="itemClass(item.status)"
      >
        <div class="flex items-center justify-between gap-3">
          <span class="min-w-0 truncate font-semibold">{{ item.name }}</span>
          <span class="shrink-0 text-xs uppercase">{{ item.status || "unknown" }}</span>
        </div>
        <div class="mt-1 text-xs leading-5 opacity-90">{{ item.detail || "" }}</div>
      </div>
    </div>
  </details>
</template>

<script setup>
import { computed } from "vue";
import {
  preflight,
  preflightChecking,
  refreshPreflight,
} from "./store.js";

const checking = computed(() => preflightChecking.value);

const items = computed(() => Array.isArray(preflight.value?.items) ? preflight.value.items : []);
const counts = computed(() => ({
  errors: items.value.filter(item => item.status === "error").length,
  blocked: items.value.filter(item => item.status === "blocked").length,
  warnings: items.value.filter(item => item.status === "warning").length,
}));

const statusLabel = computed(() => {
  if (checking.value) {
    return "Checking";
  }
  if (counts.value.errors > 0) {
    return `${counts.value.errors} blocking`;
  }
  if (counts.value.blocked > 0) {
    return "Live run active";
  }
  if (counts.value.warnings > 0) {
    return `${counts.value.warnings} warning${counts.value.warnings === 1 ? "" : "s"}`;
  }
  if (preflight.value?.ready) {
    return "Ready";
  }
  return "Checking...";
});

const statusTone = computed(() => {
  if (checking.value) {
    return "running";
  }
  if (counts.value.errors > 0) {
    return "error";
  }
  if (counts.value.blocked > 0) {
    return "blocked";
  }
  if (counts.value.warnings > 0) {
    return "warning";
  }
  return preflight.value?.ready ? "success" : "idle";
});

const statusClass = computed(() => ({
  idle: "inline-flex items-center justify-center rounded-full bg-zinc-100 px-3 py-1.5 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300",
  running: "inline-flex items-center justify-center rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
  success: "inline-flex items-center justify-center rounded-full bg-emerald-100 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
  blocked: "inline-flex items-center justify-center rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
  warning: "inline-flex items-center justify-center rounded-full bg-amber-100 px-3 py-1.5 text-xs font-semibold text-amber-700 dark:bg-amber-500/15 dark:text-amber-300",
  error: "inline-flex items-center justify-center rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300",
})[statusTone.value]);

const visibleItems = computed(() => {
  const priority = { error: 0, blocked: 1, warning: 2, ok: 3 };
  return [...items.value]
    .sort((left, right) => (priority[left.status] ?? 3) - (priority[right.status] ?? 3) || String(left.name).localeCompare(String(right.name)))
    .slice(0, 5);
});

const itemClass = status => ({
  ok: "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-200",
  warning: "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200",
  blocked: "border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-200",
  error: "border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-200",
})[status] || "border-zinc-200 bg-white text-zinc-700 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300";
</script>
