<template>
  <section data-tab-panel="runs" class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5">
    <div id="workspaceRunMeta" class="grid gap-4">
      <WorkspaceRunsPanel />
    </div>
    <PreflightPanel />
    <div class="hidden" aria-hidden="true">
      <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Setup</h3>
            <p class="mt-1 text-sm leading-5 text-zinc-500 dark:text-zinc-400">Stop preserves run state. Use Destroy after setup has created AWS resources you want removed.</p>
            <div id="setupStatus" class="mt-3 inline-flex items-center justify-center rounded-full bg-zinc-100 px-3 py-1.5 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">Idle</div>
            <div id="setupMeta" class="mt-3 hidden text-xs leading-5 text-zinc-500 dark:text-zinc-400"></div>
          </div>
          <div class="flex shrink-0 flex-wrap gap-2">
            <button id="openSetupLogsBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Open logs</button>
            <button id="setupBtn" type="button" hidden class="rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400">Stop setup</button>
          </div>
        </div>
      </div>
      <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Readiness</h3>
            <div id="readinessStatus" class="mt-3 inline-flex items-center justify-center rounded-full bg-zinc-100 px-3 py-1.5 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">Idle</div>
            <div id="readinessMeta" class="mt-3 hidden text-xs leading-5 text-zinc-500 dark:text-zinc-400"></div>
          </div>
          <div class="flex shrink-0 flex-wrap gap-2">
            <button id="openReadinessLogsBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Open logs</button>
            <button id="readinessBtn" type="button" class="rounded-lg bg-sky-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-sky-500/20 hover:bg-sky-400">Check readiness</button>
          </div>
        </div>
      </div>
    </div>
  </section>

  <section data-tab-panel="logs" class="hidden min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Logs</h2>
        <p id="logStatus" class="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-400">Select Tail or Live on any pod to open the full log viewer.</p>
      </div>
      <button id="openLogViewerBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Open log viewer</button>
    </div>
  </section>

  <section id="clustersSection" data-tab-panel="clusters" class="min-w-0 overflow-hidden rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5">
    <div class="mb-4 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
      <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Clusters</h2>
      <div id="refreshStatus" class="text-sm text-zinc-500 dark:text-zinc-400">Waiting for first refresh...</div>
    </div>
    <div id="clusters" class="grid min-w-0 gap-4">
      <div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">Loading clusters...</div>
    </div>
  </section>

  <section data-tab-panel="aws" class="hidden min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5">
    <AwsInventoryPanel />
  </section>

  <section data-tab-panel="destroy" class="hidden min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5">
    <div class="mx-auto max-w-5xl">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Destroy Slots</h2>
          <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
            Choose exactly one recorded run slot to destroy. Setup, readiness, and destroy are serialized so Terraform state, AWS actions, and logs stay unambiguous.
            The slot record is removed only after Terraform destroy succeeds.
          </p>
        </div>
        <div id="cleanupStatus" class="inline-flex items-center justify-center rounded-full bg-zinc-100 px-3 py-1.5 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">Idle</div>
      </div>
      <div class="mt-5 inline-flex rounded-xl border border-zinc-200 bg-zinc-50 p-1 dark:border-white/10 dark:bg-white/[0.03]" role="tablist" aria-label="Destroy tabs">
        <button id="destroySlotsTabBtn" type="button" data-destroy-tab="slots" class="rounded-lg bg-white px-3.5 py-2 text-sm font-semibold text-zinc-900 shadow-sm dark:bg-white/[0.08] dark:text-zinc-100">Run slots</button>
        <button id="destroyCostsTabBtn" type="button" data-destroy-tab="costs" class="rounded-lg px-3.5 py-2 text-sm font-semibold text-zinc-600 hover:bg-white dark:text-zinc-300 dark:hover:bg-white/[0.06]">Local data</button>
      </div>
      <div id="destroySlotsPane">
        <div id="cleanupSlots" class="mt-5 grid gap-3">
          <div class="rounded-lg border border-sky-200 bg-sky-50 p-4 text-sm text-sky-800 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
            <span class="spinner mr-2 align-[-0.15em]"></span>Checking recorded run slots before destroy is enabled.
          </div>
        </div>
        <div id="cleanupActions" class="mt-5 flex flex-wrap justify-end gap-3">
          <input id="cleanupConfirm" type="hidden" autocomplete="off" value="destroy" />
          <button id="openCleanupLogsBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Open cleanup logs</button>
          <button id="cleanupClearResultBtn" type="button" hidden class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Clear result</button>
          <button id="cleanupBtn" type="button" hidden class="rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400">Destroy selected slot</button>
        </div>
        <div id="cleanupCost" class="mt-5 hidden"></div>
      </div>
      <div id="destroyCostsPane" class="mt-5 hidden">
        <div class="mb-4 flex flex-col gap-3 rounded-xl border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03] sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0">
            <h3 class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">Cost ledger</h3>
            <p id="costResetStatus" class="mt-1 break-words text-sm leading-6 text-zinc-600 dark:text-zinc-400">
              Cost estimates are stored in a local ignored SQLite database.
            </p>
          </div>
          <button id="resetCostLedgerBtn" type="button" class="shrink-0 rounded-lg border border-rose-200 bg-white px-4 py-2.5 text-sm font-semibold text-rose-700 shadow-sm hover:bg-rose-50 disabled:opacity-50 dark:border-rose-500/25 dark:bg-white/[0.06] dark:text-rose-300 dark:hover:bg-rose-500/10">Reset cost DB</button>
        </div>
        <div class="mb-4 flex flex-col gap-3 rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.03] sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0">
            <h3 class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">Post-destroy artifact cleanup</h3>
            <p id="localArtifactsStatus" class="mt-1 break-words text-sm leading-6 text-zinc-600 dark:text-zinc-400">
              Backup cleanup stays locked until recorded slots are destroyed.
            </p>
          </div>
          <button id="cleanLocalArtifactsBtn" type="button" class="shrink-0 rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 disabled:opacity-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Clean after destroy</button>
        </div>
        <CostHistoryPanel />
      </div>
    </div>
  </section>

  <section data-tab-panel="settings" class="hidden min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5">
    <SettingsPanel />
  </section>

  <section data-tab-panel="k3d" class="hidden min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5">
    <K3DLabPanel />
  </section>

  <section data-tab-panel="steve" class="hidden min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm shadow-zinc-200/60 dark:border-white/10 dark:bg-zinc-900/80 dark:shadow-black/20 sm:p-5">
    <SteveLabPanel />
  </section>
</template>

<script setup>
import AwsInventoryPanel from "./AwsInventoryPanel.vue";
import CostHistoryPanel from "./CostHistoryPanel.vue";
import K3DLabPanel from "./K3DLabPanel.vue";
import PreflightPanel from "./PreflightPanel.vue";
import SettingsPanel from "./SettingsPanel.vue";
import SteveLabPanel from "./SteveLabPanel.vue";
import WorkspaceRunsPanel from "./WorkspaceRunsPanel.vue";
</script>
