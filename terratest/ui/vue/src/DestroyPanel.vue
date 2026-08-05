<template>
  <div class="mx-auto max-w-5xl">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Destroy Slots</h2>
        <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
          Destroy one slot immediately, select several slots for a sequential batch, or explicitly destroy all recorded slots.
          A slot record is removed only after its Terraform destroy succeeds; failures remain available to retry.
        </p>
      </div>
      <div :class="cleanupStatusClass">
        <span v-if="cleanupStatusTone === 'running'" class="spinner mr-2"></span>
        {{ cleanupStatusLabel }}
      </div>
    </div>

    <div class="mt-5 inline-flex rounded-xl border border-zinc-200 bg-zinc-50 p-1 dark:border-white/10 dark:bg-white/[0.03]" role="tablist" aria-label="Destroy tabs">
      <button
        type="button"
        @click="setActiveDestroyTab('slots')"
        :class="activeDestroyTab === 'slots' ? activeTabClass : inactiveTabClass"
      >
        Run slots
      </button>
      <button
        type="button"
        @click="setActiveDestroyTab('costs')"
        :class="activeDestroyTab === 'costs' ? activeTabClass : inactiveTabClass"
      >
        Local data
      </button>
    </div>

    <div v-if="activeDestroyTab === 'slots'" id="destroySlotsPane">
      <div
        v-if="runs.length"
        class="mt-5 rounded-xl border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03]"
      >
        <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div class="min-w-0">
            <div class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">
              {{ selectedCount }} of {{ runs.length }} slot{{ runs.length === 1 ? '' : 's' }} selected
            </div>
            <div class="mt-1 text-xs leading-5 text-zinc-500 dark:text-zinc-400">
              Bulk destroys run one slot at a time and continue past failures. Other lifecycle actions stay locked until the batch finishes.
            </div>
          </div>
          <div class="flex flex-wrap gap-2 xl:justify-end">
            <button
              type="button"
              @click="selectAllCleanupRuns"
              :disabled="bulkActionsLocked || allRunsSelected"
              :class="bulkActionsLocked || allRunsSelected ? disabledCompactButtonClass : secondaryCompactButtonClass"
            >
              Select all
            </button>
            <button
              type="button"
              @click="clearCleanupRunSelection"
              :disabled="bulkActionsLocked || selectedCount === 0"
              :class="bulkActionsLocked || selectedCount === 0 ? disabledCompactButtonClass : secondaryCompactButtonClass"
            >
              Clear
            </button>
            <button
              type="button"
              @click="handleDestroySelected"
              :disabled="bulkActionsLocked || selectedCount === 0"
              :title="bulkActionTitle('selected')"
              :class="bulkActionsLocked || selectedCount === 0 ? disabledButtonClass : dangerButtonClass"
            >
              <span v-if="cleanupBatchStarting" class="spinner mr-2 !h-4 !w-4 !border-2"></span>
              Destroy selected<span v-if="selectedCount"> ({{ selectedCount }})</span>
            </button>
            <button
              type="button"
              @click="handleDestroyAll"
              :disabled="bulkActionsLocked || runs.length === 0"
              :title="bulkActionTitle('all')"
              :class="bulkActionsLocked || runs.length === 0 ? disabledButtonClass : destroyAllButtonClass"
            >
              Destroy all ({{ runs.length }})
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="batchVisible"
        class="mt-4 overflow-hidden rounded-xl border"
        :class="batchPanelClass"
        aria-live="polite"
      >
        <div class="p-4">
          <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <span v-if="cleanupBatch.running" class="spinner !h-4 !w-4 !border-2"></span>
                <h3 class="text-sm font-semibold">{{ batchHeading }}</h3>
                <span v-if="cleanupBatch.cancelRequested" class="rounded-full bg-amber-100 px-2 py-1 text-xs font-semibold text-amber-700 dark:bg-amber-500/15 dark:text-amber-300">
                  Cancel requested
                </span>
              </div>
              <p class="mt-1 text-sm leading-6 opacity-80">{{ batchSummary }}</p>
            </div>
            <div class="flex shrink-0 flex-wrap gap-2 sm:justify-end">
              <button
                v-if="cleanupBatch.running"
                type="button"
                @click="handleStopBatch"
                :disabled="batchStopLocked"
                :title="batchStopLocked ? 'The stop request is already being processed.' : 'Interrupt the current Terraform destroy and preserve every queued slot.'"
                :class="batchStopLocked ? disabledCompactButtonClass : stopBatchButtonClass"
              >
                <span v-if="batchStopPending" class="spinner mr-2 !h-3.5 !w-3.5 !border-2"></span>
                {{ batchStopLocked ? 'Stop requested' : 'Stop batch' }}
              </button>
              <button type="button" @click="openCleanupBatchLogs" :class="secondaryCompactButtonClass">
                Open batch logs
              </button>
            </div>
          </div>
          <div v-if="batchTotal" class="mt-4">
            <div class="mb-1.5 flex justify-between gap-3 text-xs font-semibold">
              <span>{{ batchProcessedCount }} of {{ batchTotal }} processed</span>
              <span>{{ batchProgressPercent }}%</span>
            </div>
            <div class="h-2 overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
              <div class="h-full rounded-full bg-current transition-[width] duration-300" :style="{ width: `${batchProgressPercent}%` }"></div>
            </div>
          </div>
          <div v-if="batchFailures.length" class="mt-4 grid gap-2">
            <div class="text-xs font-semibold uppercase tracking-wide">Failed slots</div>
            <div
              v-for="failure in batchFailures"
              :key="failure.runId || failure.error"
              class="rounded-lg border border-rose-300/60 bg-white/60 px-3 py-2 text-sm dark:border-rose-500/30 dark:bg-black/15"
            >
              <span class="font-semibold">{{ failure.runId || 'Unknown run' }}</span>
              <span class="ml-1 break-words opacity-80">— {{ failure.error || 'Destroy failed' }}</span>
            </div>
          </div>
        </div>
      </div>

      <div id="cleanupSlots" class="mt-5 grid gap-3">
        <div
          v-if="!runs.length && bootPending"
          class="rounded-lg border border-sky-200 bg-sky-50 p-4 text-sm text-sky-800 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100"
        >
          <span class="spinner mr-2 align-[-0.15em]"></span>Checking recorded run slots before destroy is enabled.
        </div>
        <div
          v-else-if="!runs.length"
          class="rounded-lg border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400"
        >
          No recorded run slots found. There is nothing for Terraform destroy to target from this panel.
        </div>

        <div
          v-if="selectedRunId && !selectedCount"
          class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-100"
        >
          Selected run {{ selectedRunId }}. Destroy is typed-confirmed and uses the recorded Terraform target for that slot.
        </div>

        <article
          v-for="run in runs"
          :key="run.runId || run.slotId || run.slotName || JSON.stringify(run)"
          class="rounded-xl border p-4"
          :class="slotCardClass(run)"
        >
          <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <label
                  class="inline-flex items-center gap-2 rounded-lg border border-zinc-200 bg-white px-2.5 py-1.5 text-xs font-semibold text-zinc-600 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300"
                  :class="bulkActionsLocked ? 'cursor-not-allowed opacity-55' : 'cursor-pointer'"
                >
                  <input
                    type="checkbox"
                    :checked="isBulkSelected(run)"
                    :disabled="bulkActionsLocked"
                    @change="toggleCleanupRunSelection(run.runId)"
                    class="h-4 w-4 rounded border-zinc-300 accent-emerald-500"
                  />
                  Select
                </label>
                <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Run {{ run.runId || "unknown" }}</h3>
                <span class="rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">
                  {{ (run.status || "recorded").replaceAll("_", " ") }}
                </span>
                <span
                  v-if="isSelected(run) && !destroying(run) && !pendingDestroy(run)"
                  class="rounded-full bg-emerald-100 px-2.5 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300"
                >
                  Selected for destroy
                </span>
                <span
                  v-if="destroying(run)"
                  class="rounded-full bg-sky-100 px-2.5 py-1 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300"
                >
                  Destroy running
                </span>
                <span
                  v-else-if="pendingDestroy(run)"
                  class="rounded-full bg-sky-100 px-2.5 py-1 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300"
                >
                  Starting destroy
                </span>
                <span
                  v-else-if="batchQueued(run)"
                  class="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-semibold text-amber-700 dark:bg-amber-500/15 dark:text-amber-300"
                >
                  Queued
                </span>
                <span
                  v-if="batchFailureForRun(run)"
                  class="rounded-full bg-rose-100 px-2.5 py-1 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300"
                >
                  Batch destroy failed
                </span>
              </div>
              <div v-if="run.updatedAt" class="mt-1 text-xs text-zinc-500 dark:text-zinc-400">Updated {{ timeLabel(run.updatedAt) }}</div>
              <div class="mt-3 grid gap-2 text-sm text-zinc-700 dark:text-zinc-300 md:grid-cols-2">
                <div><span class="font-semibold">Slot:</span> {{ run.slotId || run.slotName || "not recorded" }}</div>
                <div><span class="font-semibold">HAs:</span> {{ run.totalHAs || 1 }}</div>
                <div><span class="font-semibold">Rancher:</span> {{ versionsLabel(run) }}</div>
                <div><span class="font-semibold">Owner:</span> {{ run.owner || "not recorded" }}</div>
                <div><span class="font-semibold">AWS prefix:</span> {{ run.awsPrefix || "not recorded" }}</div>
                <div><span class="font-semibold">Hostname:</span> {{ hostnameLabel(run) }}</div>
                <div class="md:col-span-2">
                  <span class="font-semibold">State:</span>
                  <span :title="run.terraformStatePath || run.terraformBackend || ''">
                    {{ compactPath(run.terraformStatePath || run.terraformBackend || "not recorded") }}
                  </span>
                </div>
              </div>
            </div>
            <div class="flex shrink-0 flex-wrap gap-2 lg:justify-end">
              <button
                type="button"
                @click="handleOpenFolder(run)"
                :disabled="!runFolderAvailable(run)"
                :title="runFolderAvailable(run) ? 'Open this run slot folder in Finder.' : 'Run folder is not available locally.'"
                :class="runFolderAvailable(run) ? secondaryButtonClass : disabledButtonClass"
              >
                Open folder
              </button>
              <button
                type="button"
                @click="handleDestroySlot(run.runId)"
                :disabled="slotDestroyDisabled(run)"
                :title="slotDestroyTitle(run)"
                :class="slotDestroyDisabled(run) ? disabledButtonClass : dangerButtonClass"
              >
                <span v-if="destroying(run)" class="spinner mr-2 !h-4 !w-4 !border-2"></span>
                <span v-else-if="pendingDestroy(run)" class="spinner mr-2 !h-4 !w-4 !border-2"></span>
                {{ slotDestroyLabel(run) }}
              </button>
            </div>
          </div>
        </article>
      </div>

      <div id="cleanupActions" class="mt-5 flex flex-wrap justify-end gap-3">
        <button
          v-if="batchVisible"
          type="button"
          @click="openCleanupBatchLogs"
          :class="secondaryButtonClass"
        >
          Open batch logs
        </button>
        <button
          v-else
          type="button"
          @click="openCleanupLogs(runIsLinodeDocker(activeCleanup))"
          :class="secondaryButtonClass"
        >
          Open cleanup logs
        </button>
        <button
          v-if="cleanupResultFinished"
          type="button"
          @click="dismissedCleanupResultKey = cleanupResultKey(activeCleanup)"
          :class="secondaryButtonClass"
        >
          Clear result
        </button>
      </div>

      <div v-if="cleanupCostVisible" id="cleanupCost" class="mt-5">
        <div
          v-if="cleanupCost"
          class="rounded-2xl border border-emerald-200 bg-emerald-50 p-4 text-left dark:border-emerald-500/20 dark:bg-emerald-500/10"
        >
          <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <div class="text-xs font-semibold uppercase tracking-wide text-emerald-700 dark:text-emerald-300">Estimated infrastructure cost while alive</div>
              <div class="mt-1 text-3xl font-semibold tracking-tight text-emerald-950 dark:text-emerald-100">{{ cleanupCost.total }}</div>
              <div class="mt-1 text-sm text-emerald-800/80 dark:text-emerald-200/80">{{ cleanupCost.region || "AWS region unavailable" }}</div>
            </div>
            <div class="grid gap-2 text-sm text-emerald-950 dark:text-emerald-100 sm:min-w-80">
              <div v-if="cleanupCost.runtime"><span class="font-semibold">Runtime:</span> {{ cleanupCost.runtime }}</div>
              <div v-if="cleanupCost.ec2"><span class="font-semibold">EC2:</span> {{ cleanupCost.ec2 }}</div>
              <div v-if="cleanupCost.ebs"><span class="font-semibold">EBS:</span> {{ cleanupCost.ebs }}</div>
              <div v-if="cleanupCost.rds"><span class="font-semibold">RDS/Aurora:</span> {{ cleanupCost.rds }}</div>
              <div v-if="cleanupCost.loadBalancers"><span class="font-semibold">Load balancers:</span> {{ cleanupCost.loadBalancers }}</div>
            </div>
          </div>
        </div>
        <div
          v-else-if="estimateUnavailable"
          class="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-left text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200"
        >
          Unable to estimate infrastructure cost for this destroy run. Destroy still ran; AWS pricing or Terraform outputs were unavailable.
        </div>
      </div>
    </div>

    <div v-else-if="activeDestroyTab === 'costs'" id="destroyCostsPane" class="mt-5">
      <div class="mb-4 flex flex-col gap-3 rounded-xl border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03] sm:flex-row sm:items-start sm:justify-between">
        <div class="min-w-0">
          <h3 class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">Cost ledger</h3>
          <p class="mt-1 break-words text-sm leading-6 text-zinc-600 dark:text-zinc-400">
            {{ costResetStatusText }}
          </p>
        </div>
        <button
          type="button"
          @click="resetCostLedger"
          :disabled="resetCostsLocked"
          :title="resetCostsTitle"
          class="shrink-0 rounded-lg border border-rose-200 bg-white px-4 py-2.5 text-sm font-semibold text-rose-700 shadow-sm hover:bg-rose-50 disabled:opacity-50 dark:border-rose-500/25 dark:bg-white/[0.06] dark:text-rose-300 dark:hover:bg-rose-500/10"
        >
          <span v-if="costResetting" class="spinner mr-2 !h-4 !w-4 !border-2 align-[-0.15em]"></span>
          {{ resetCostsLabel }}
        </button>
      </div>
      <div class="mb-4 flex flex-col gap-3 rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.03] sm:flex-row sm:items-start sm:justify-between">
        <div class="min-w-0">
          <p class="mt-1 break-words text-sm leading-6 text-zinc-600 dark:text-zinc-400">
            {{ artifactsStatusText }}
          </p>
        </div>
        <button
          type="button"
          @click="cleanLocalArtifacts"
          :disabled="cleanArtifactsLocked"
          :title="cleanArtifactsTitle"
          class="shrink-0 rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 disabled:opacity-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
        >
          <span v-if="localArtifactsCleaning" class="spinner mr-2 !h-4 !w-4 !border-2 align-[-0.15em]"></span>
          {{ cleanArtifactsLabel }}
        </button>
      </div>
      <CostHistoryPanel />
    </div>
  </div>
</template>

<script setup>
import { computed } from "vue";
import CostHistoryPanel from "./CostHistoryPanel.vue";
import {
  state,
  bootPending,
  activeDestroyTab,
  selectedCleanupRunId as selectedRunId,
  selectedCleanupRunIds,
  cleanupStarting,
  cleanupBatchStarting,
  cleanupSelectionLocked,
  pendingAbortOperation,
  dismissedCleanupResultKey,
  costResetting,
  localArtifactsCleaning,
  lifecycleRunning,
  setActiveDestroyTab,
  openCleanupLogs,
  openCleanupBatchLogs,
  openLocalPath,
  runCleanup,
  runCleanupBatch,
  abortOperation,
  toggleCleanupRunSelection,
  selectAllCleanupRuns,
  clearCleanupRunSelection,
  resetCostLedger,
  cleanLocalArtifacts,
} from "./store.js";

const secondaryButtonClass = "rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]";
const disabledButtonClass = "rounded-lg bg-zinc-200 px-4 py-2.5 text-sm font-semibold text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400";
const dangerButtonClass = "rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400";
const destroyAllButtonClass = "rounded-lg border border-rose-300 bg-white px-4 py-2.5 text-sm font-semibold text-rose-700 shadow-sm hover:bg-rose-50 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-300 dark:hover:bg-rose-500/20";
const secondaryCompactButtonClass = "rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]";
const disabledCompactButtonClass = "rounded-lg bg-zinc-200 px-3 py-2 text-xs font-semibold text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400";
const stopBatchButtonClass = "rounded-lg border border-amber-300 bg-white px-3 py-2 text-xs font-semibold text-amber-800 shadow-sm hover:bg-amber-50 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200 dark:hover:bg-amber-500/20";

const activeTabClass = "rounded-lg bg-white px-3.5 py-2 text-sm font-semibold text-zinc-900 shadow-sm dark:bg-white/[0.08] dark:text-zinc-100";
const inactiveTabClass = "rounded-lg px-3.5 py-2 text-sm font-semibold text-zinc-600 hover:bg-white dark:text-zinc-300 dark:hover:bg-white/[0.06]";

const runs = computed(() => Array.isArray(state.value?.workspace?.runs) ? state.value.workspace.runs : []);
const cleanupBatch = computed(() => state.value?.cleanupBatch || {});
const batchRunIds = computed(() => Array.isArray(cleanupBatch.value?.runIds) ? cleanupBatch.value.runIds : []);
const batchCompletedRunIds = computed(() => Array.isArray(cleanupBatch.value?.completedRunIds) ? cleanupBatch.value.completedRunIds : []);
const batchFailures = computed(() => Array.isArray(cleanupBatch.value?.failures)
  ? cleanupBatch.value.failures.filter(failure => failure && (failure.runId || failure.error))
  : []);
const batchStopPending = computed(() => pendingAbortOperation.value === "cleanupBatch");
const batchStopLocked = computed(() => Boolean(cleanupBatch.value?.cancelRequested || batchStopPending.value));
const batchTotal = computed(() => batchRunIds.value.length);
const batchProcessedCount = computed(() => {
  const processed = [];
  for (const runId of batchCompletedRunIds.value) {
    if (!processed.some(existing => sameRunKey(existing, runId))) processed.push(runId);
  }
  for (const failure of batchFailures.value) {
    if (failure.runId && !processed.some(existing => sameRunKey(existing, failure.runId))) processed.push(failure.runId);
  }
  return processed.length;
});
const batchProgressPercent = computed(() => batchTotal.value
  ? Math.min(100, Math.round((batchProcessedCount.value / batchTotal.value) * 100))
  : 0);
const batchVisible = computed(() => Boolean(
  cleanupBatch.value?.running ||
  cleanupBatch.value?.startedAt ||
  cleanupBatch.value?.finishedAt ||
  cleanupBatch.value?.error ||
  batchRunIds.value.length ||
  batchFailures.value.length
));
const batchHeading = computed(() => {
  if (cleanupBatch.value?.running) return cleanupBatch.value.cancelRequested ? "Stopping destroy batch" : "Destroy batch running";
  if (batchFailures.value.length) return "Destroy batch finished with failures";
  if (cleanupBatch.value?.error) return "Destroy batch stopped with an error";
  if (cleanupBatch.value?.finishedAt) return "Destroy batch completed";
  return "Destroy batch";
});
const batchSummary = computed(() => {
  const currentRunId = String(cleanupBatch.value?.currentRunId || cleanupBatch.value?.runId || "").trim();
  if (cleanupBatch.value?.running && currentRunId) {
    return `Currently destroying run ${currentRunId}. ${batchCompletedRunIds.value.length} succeeded and ${batchFailures.value.length} failed so far.`;
  }
  if (cleanupBatch.value?.running) {
    return `Preparing the next slot. ${batchCompletedRunIds.value.length} succeeded and ${batchFailures.value.length} failed so far.`;
  }
  if (cleanupBatch.value?.error) {
    return `${batchCompletedRunIds.value.length} succeeded and ${batchFailures.value.length} failed. ${cleanupBatch.value.error}`;
  }
  return `${batchCompletedRunIds.value.length} succeeded and ${batchFailures.value.length} failed${cleanupBatch.value?.finishedAt ? `; finished ${timeLabel(cleanupBatch.value.finishedAt)}` : ""}.`;
});
const batchPanelClass = computed(() => cleanupBatch.value?.running
  ? cleanupBatch.value?.cancelRequested
    ? "border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-100"
    : "border-sky-200 bg-sky-50 text-sky-900 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100"
  : batchFailures.value.length || cleanupBatch.value?.error
    ? "border-rose-200 bg-rose-50 text-rose-900 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-100"
    : "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-100");
const selectedCount = computed(() => selectedCleanupRunIds.value.length);
const allRunsSelected = computed(() => runs.value.length > 0 && runs.value.every(run =>
  selectedCleanupRunIds.value.some(runId => sameRunKey(runId, run?.runId))
));
const bulkActionsLocked = computed(() => cleanupSelectionLocked.value);
const activeCleanup = computed(() => {
  const linodeCleanup = state.value?.linodeCleanup || {};
  if (linodeCleanup.running || linodeCleanup.finishedAt || linodeCleanup.error) {
    return linodeCleanup;
  }
  return state.value?.cleanup || {};
});
const cleanupOutput = computed(() => Array.isArray(activeCleanup.value?.output) ? activeCleanup.value.output : []);

const sameRunKey = (left, right) => String(left || "").trim() === String(right || "").trim();
const runIsLinodeDocker = run => run?.deploymentType === "linode-docker-cattle";

const timeLabel = value => value ? new Date(value).toLocaleTimeString() : "";
const trimTrailingPathSeparator = value => String(value || "").replace(/[\\/]+$/, "");
const compactPath = value => {
  const path = String(value || "").trim();
  if (!path) {
    return "";
  }
  const parts = path.split("/").filter(Boolean);
  return parts.length <= 4 ? path : `.../${parts.slice(-4).join("/")}`;
};
const runFolderPath = run => {
  if (run?.runFolderPath) {
    return run.runFolderPath;
  }
  const terraformModule = trimTrailingPathSeparator(run?.terraformModuleDir || "");
  if (terraformModule) {
    return terraformModule.replace(/[\\/]terraform[\\/]module$/, "");
  }
  const terraformState = trimTrailingPathSeparator(run?.terraformStatePath || "");
  if (terraformState) {
    return terraformState.replace(/[\\/]terraform[\\/]terraform\.tfstate$/, "");
  }
  const haRoot = trimTrailingPathSeparator(run?.haOutputRoot || "");
  return haRoot ? haRoot.replace(/[\\/]ha$/, "") : "";
};
const runFolderAvailable = run => Boolean(runFolderPath(run) && run?.runFolderExists !== false);
const versionsLabel = run => Array.isArray(run?.rancherVersions) && run.rancherVersions.length
  ? run.rancherVersions.join(", ")
  : "not recorded";
const hostnameLabel = run => {
  if (run?.deploymentType === "hosted-tenant-k3s") {
    return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-t*.${run.route53Fqdn}` : run.route53Fqdn || "generated per slot";
  }
  if (run?.deploymentType === "linode-docker-cattle") {
    return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-*.${run.route53Fqdn}` : run.route53Fqdn || "generated per slot";
  }
  if (run?.customHostnamePrefix) {
    return `${run.customHostnamePrefix}.${run.route53Fqdn || ""}`.replace(/\.$/, "");
  }
  return run?.awsPrefix && run?.route53Fqdn ? `${run.awsPrefix}-h*.${run.route53Fqdn}` : run?.route53Fqdn || "generated per slot";
};

const cleanupForRun = run => runIsLinodeDocker(run) ? state.value?.linodeCleanup || {} : state.value?.cleanup || {};
const setupRunningForRun = run => runIsLinodeDocker(run) ? Boolean(state.value?.linodeSetup?.running) : Boolean(state.value?.setup?.running);
const cleanupRunningForRun = run => runIsLinodeDocker(run) ? Boolean(state.value?.linodeCleanup?.running) : Boolean(state.value?.cleanup?.running);
const readinessRunningForRun = run => !runIsLinodeDocker(run) && Boolean(state.value?.readiness?.running);
const pendingDestroy = run => Boolean(cleanupStarting.value && sameRunKey(selectedRunId.value, run?.runId));
const batchDestroying = run => Boolean(cleanupBatch.value?.running && sameRunKey(cleanupBatch.value?.currentRunId || cleanupBatch.value?.runId, run?.runId));
const destroying = run => Boolean(batchDestroying(run) || (cleanupRunningForRun(run) && sameRunKey(cleanupForRun(run)?.runId, run?.runId)));
const isBulkSelected = run => selectedCleanupRunIds.value.some(runId => sameRunKey(runId, run?.runId));
const isSelected = run => isBulkSelected(run) || Boolean(selectedRunId.value && sameRunKey(selectedRunId.value, run?.runId));
const batchFailureForRun = run => batchFailures.value.find(failure => sameRunKey(failure.runId, run?.runId));
const batchQueued = run => Boolean(
  cleanupBatch.value?.running &&
  batchRunIds.value.some(runId => sameRunKey(runId, run?.runId)) &&
  !batchDestroying(run) &&
  !batchCompletedRunIds.value.some(runId => sameRunKey(runId, run?.runId)) &&
  !batchFailureForRun(run)
);
const slotCardClass = run => batchFailureForRun(run)
  ? "border-rose-200 bg-rose-50/60 dark:border-rose-500/25 dark:bg-rose-500/10"
  : destroying(run) || pendingDestroy(run)
  ? "border-sky-200 bg-sky-50/60 dark:border-sky-500/25 dark:bg-sky-500/10"
  : isSelected(run)
    ? "border-emerald-200 bg-emerald-50/60 dark:border-emerald-500/25 dark:bg-emerald-500/10"
    : "border-zinc-200 bg-white dark:border-white/10 dark:bg-white/[0.03]";
const slotDestroyDisabled = run => Boolean(
  bulkActionsLocked.value ||
  setupRunningForRun(run) ||
  readinessRunningForRun(run) ||
  cleanupRunningForRun(run)
);
const slotDestroyTitle = run => bootPending.value
  ? "Startup safety check is still loading run slots and operation state."
  : setupRunningForRun(run)
    ? "Wait for setup to finish before destroying a run slot."
    : readinessRunningForRun(run)
      ? "Wait for readiness checks to finish before destroying a run slot."
      : cleanupRunningForRun(run)
        ? "Wait for the current destroy to finish before starting another one."
        : cleanupBatch.value?.running || cleanupBatchStarting.value
          ? "Wait for the destroy batch to finish before starting another destroy."
        : cleanupStarting.value
          ? "Destroy request is being submitted."
          : `Destroy run ${run?.runId || "slot"}`;
const slotDestroyLabel = run => destroying(run)
  ? "Destroy running"
  : pendingDestroy(run)
    ? "Starting destroy"
    : bootPending.value
      ? "Checking state"
      : setupRunningForRun(run)
        ? "Setup running"
        : readinessRunningForRun(run)
          ? "Readiness running"
          : cleanupRunningForRun(run)
            ? "Destroy running"
            : cleanupBatch.value?.running || cleanupBatchStarting.value
              ? "Batch destroy running"
            : "Destroy this slot";

const cleanupResultKey = cleanup => {
  if (!cleanup || cleanup.running || (!cleanup.finishedAt && !cleanup.error)) {
    return "";
  }
  return [
    cleanup.runId || "unknown-run",
    cleanup.finishedAt || "unfinished",
    cleanup.error || "ok",
  ].join("|");
};
const cleanupDismissed = computed(() => {
  const key = cleanupResultKey(activeCleanup.value);
  return Boolean(key && dismissedCleanupResultKey.value === key);
});
const extractCleanupLineValue = (output, label) => {
  const line = output.find(item => item.includes(label));
  return line ? line.slice(line.indexOf(label) + label.length).trim() : "";
};
const parseCleanupCost = output => {
  const total = extractCleanupLineValue(output, "Estimated total:")
    || extractCleanupLineValue(output, "Estimated total (EC2 + EBS only):");
  if (!total) {
    return null;
  }
  return {
    total,
    region: extractCleanupLineValue(output, "Region:"),
    runtime: extractCleanupLineValue(output, "Total runtime across instances:"),
    ec2: extractCleanupLineValue(output, "EC2:"),
    ebs: extractCleanupLineValue(output, "EBS:"),
    rds: extractCleanupLineValue(output, "RDS/Aurora:"),
    loadBalancers: extractCleanupLineValue(output, "Load balancers:"),
  };
};
const cleanupCost = computed(() => cleanupDismissed.value ? null : parseCleanupCost(cleanupOutput.value));
const estimateUnavailable = computed(() => !cleanupDismissed.value && Boolean(activeCleanup.value?.finishedAt) && cleanupOutput.value.some(line =>
  line.includes("Could not estimate EC2/EBS cost") ||
  line.includes("Could not estimate AWS cost") ||
  line.includes("Terraform outputs unavailable")
));
const cleanupCostVisible = computed(() => Boolean(cleanupCost.value || estimateUnavailable.value));

const handleOpenFolder = run => {
  if (!runFolderAvailable(run)) {
    return;
  }
  openLocalPath(runFolderPath(run));
};

const handleDestroySlot = runId => {
  selectedRunId.value = runId;
  runCleanup(runId);
};

const handleDestroySelected = () => runCleanupBatch({ runIds: selectedCleanupRunIds.value });
const handleDestroyAll = () => runCleanupBatch({ all: true });
const handleStopBatch = () => {
  if (batchStopLocked.value) return;
  abortOperation("cleanupBatch");
};
const bulkActionTitle = mode => {
  if (bootPending.value) return "Startup safety check is still loading run slots and operation state.";
  if (bulkActionsLocked.value) return "Wait for the active lifecycle operation or destroy request to finish.";
  if (mode === "selected" && selectedCount.value === 0) return "Select at least one run slot first.";
  return mode === "all"
    ? `Destroy all ${runs.value.length} recorded run slots sequentially.`
    : `Destroy ${selectedCount.value} selected run slot${selectedCount.value === 1 ? "" : "s"} sequentially.`;
};

const cleanupStatusTone = computed(() => {
  if (cleanupBatch.value?.running || cleanupBatchStarting.value) return "running";
  if (batchVisible.value && (batchFailures.value.length || cleanupBatch.value?.error)) return "error";
  if (batchVisible.value && cleanupBatch.value?.finishedAt) return "success";
  const cleanup = activeCleanup.value;
  if (cleanup?.running) return "running";
  if (cleanup?.finishedAt && !cleanup?.error && !cleanupDismissed.value) return "success";
  if (cleanup?.error && !cleanupDismissed.value) return "error";
  return "idle";
});

const cleanupStatusLabel = computed(() => {
  if (cleanupBatchStarting.value) return "Starting destroy batch";
  if (cleanupBatch.value?.running) {
    const currentRunId = cleanupBatch.value?.currentRunId || cleanupBatch.value?.runId;
    return currentRunId
      ? `Destroying ${currentRunId} (${batchProcessedCount.value}/${batchTotal.value} processed)`
      : `Destroy batch running (${batchProcessedCount.value}/${batchTotal.value} processed)`;
  }
  if (batchVisible.value && (batchFailures.value.length || cleanupBatch.value?.error)) {
    return `Batch finished: ${batchCompletedRunIds.value.length} succeeded, ${batchFailures.value.length} failed`;
  }
  if (batchVisible.value && cleanupBatch.value?.finishedAt) {
    return `Batch finished: ${batchCompletedRunIds.value.length} succeeded`;
  }
  const cleanup = activeCleanup.value;
  if (cleanup?.running) {
    return `Destroy running${cleanup.runId ? ` for ${cleanup.runId}` : ""}${cleanup.startedAt ? ` since ${new Date(cleanup.startedAt).toLocaleTimeString()}` : ""}`;
  }
  if (cleanup?.finishedAt && !cleanup?.error && !cleanupDismissed.value) {
    return `Destroy finished successfully at ${new Date(cleanup.finishedAt).toLocaleTimeString()}`;
  }
  if (cleanup?.error && !cleanupDismissed.value) {
    return "Destroy finished with error";
  }
  return "Idle";
});

const cleanupStatusClass = computed(() => {
  const tones = {
    running: "inline-flex items-center justify-center rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
    success: "inline-flex items-center justify-center rounded-full bg-emerald-100 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
    error: "inline-flex items-center justify-center rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300",
    idle: "inline-flex items-center justify-center rounded-full bg-zinc-100 px-3 py-1.5 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300",
  };
  return tones[cleanupStatusTone.value];
});

const cleanupResultFinished = computed(() => {
  const cleanup = activeCleanup.value;
  return Boolean(!batchVisible.value && cleanup?.finishedAt && !cleanupDismissed.value);
});

// Cost ledger status & artifacts cleanup bindings
const resetCostsLocked = computed(() =>
  costResetting.value || bootPending.value || lifecycleRunning.value
);
const resetCostsTitle = computed(() =>
  bootPending.value
    ? "Startup safety check is still loading panel state."
    : lifecycleRunning.value
      ? "Wait for setup, readiness, or destroy to finish before resetting the cost ledger."
      : "Delete the local ignored SQLite cost ledger and recreate it empty."
);
const resetCostsLabel = computed(() =>
  costResetting.value ? "Resetting" : "Reset cost DB"
);
const costResetStatusText = computed(() => {
  const dbPath = state.value?.costs?.dbPath || "terratest/automation-output/control-panel/cost-ledger.sqlite";
  return `${dbPath} is local cache under automation-output/ and is ignored by Git.`;
});

const runCount = computed(() => runs.value.length);
const residueCount = computed(() => Array.isArray(state.value?.workspace?.sharedPathLabels) ? state.value.workspace.sharedPathLabels.length : 0);
const cleanArtifactsLocked = computed(() =>
  localArtifactsCleaning.value || bootPending.value || lifecycleRunning.value || runCount.value > 0
);
const cleanArtifactsTitle = computed(() =>
  bootPending.value
    ? "Startup safety check is still loading panel state."
    : lifecycleRunning.value
      ? "Wait for setup, readiness, or destroy to finish before cleaning local artifacts."
      : runCount.value > 0
        ? "Locked while recorded run slots exist so Terraform destroy targets stay available."
        : "Remove ignored local run residue left after destroy. Cost history is kept."
);
const cleanArtifactsLabel = computed(() =>
  localArtifactsCleaning.value ? "Cleaning" : "Clean after destroy"
);
const artifactsStatusText = computed(() => {
  if (localArtifactsCleaning.value) {
    return "Cleaning ignored local run residue...";
  }
  if (runCount.value > 0) {
    return `Locked: ${runCount.value} recorded run slot${runCount.value === 1 ? "" : "s"} still exist. Destroy slots first so Terraform targets stay intact.`;
  }
  if (residueCount.value > 0) {
    return `Ready: no recorded run slots remain. ${residueCount.value} leftover local artifact${residueCount.value === 1 ? "" : "s"} can be cleaned.`;
  }
  return "Ready: no recorded run slots remain and no shared workspace residue is blocking setup.";
});
</script>
