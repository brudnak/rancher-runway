<template>
  <section
    v-show="activeTab === 'runs'"
    class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5"
  >
    <div id="workspaceRunMeta" class="grid gap-4">
      <WorkspaceRunsPanel />
    </div>
    <PreflightPanel />
  </section>

  <section
    v-show="activeTab === 'logs'"
    class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5"
  >
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Logs</h2>
        <p class="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-400">Select Tail or Live on any pod to open the full log viewer.</p>
      </div>
      <button
        type="button"
        @click="openLogModal"
        class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
      >
        Open log viewer
      </button>
    </div>
  </section>

  <section
    v-show="activeTab === 'clusters'"
    class="min-w-0 overflow-hidden rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5"
  >
    <div class="mb-4 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
      <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Clusters</h2>
      <div class="text-sm text-zinc-500 dark:text-zinc-400">{{ refreshStatus }}</div>
    </div>
    <ClustersPanel />
  </section>

  <section
    v-show="activeTab === 'aws'"
    class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5"
  >
    <AwsInventoryPanel />
  </section>

  <section
    v-show="activeTab === 'destroy'"
    class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5"
  >
    <DestroyPanel />
  </section>

  <section
    v-show="activeTab === 'settings'"
    class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5"
  >
    <SettingsPanel />
  </section>

  <section
    v-show="activeTab === 'k3d'"
    class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5"
  >
    <K3DLabPanel />
  </section>

  <section
    v-show="activeTab === 'steve'"
    class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5"
  >
    <SteveLabPanel />
  </section>
</template>

<script setup>
import { activeTab, refreshStatus, openLogModal } from "./store.js";
import AwsInventoryPanel from "./AwsInventoryPanel.vue";
import ClustersPanel from "./ClustersPanel.vue";
import DestroyPanel from "./DestroyPanel.vue";
import K3DLabPanel from "./K3DLabPanel.vue";
import PreflightPanel from "./PreflightPanel.vue";
import SettingsPanel from "./SettingsPanel.vue";
import SteveLabPanel from "./SteveLabPanel.vue";
import WorkspaceRunsPanel from "./WorkspaceRunsPanel.vue";
</script>
