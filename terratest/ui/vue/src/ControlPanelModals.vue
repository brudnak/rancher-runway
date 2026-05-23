<template>
  <!-- GPU Reminder Modal -->
  <div
    v-if="gpuReminderModalOpen"
    id="gpuReminderModal"
    @click.self="hideGPUReminderModal"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-zinc-950/55 p-4 backdrop-blur-sm dark:bg-zinc-950/80"
    role="dialog"
    aria-modal="true"
    aria-labelledby="gpuReminderTitle"
  >
    <section class="w-full max-w-xl overflow-hidden rounded-2xl border border-rose-200 bg-white shadow-2xl shadow-zinc-950/20 dark:border-rose-500/25 dark:bg-zinc-900 dark:shadow-black/50">
      <div class="border-b border-rose-100 px-6 py-5 dark:border-rose-500/20">
        <div class="mb-3 inline-flex rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300">Cost reminder</div>
        <h2 id="gpuReminderTitle" class="text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">GPU infrastructure active</h2>
        <p id="gpuReminderBody" class="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">{{ gpuReminderBody }}</p>
      </div>
      <div class="flex flex-wrap justify-end gap-3 px-6 py-4">
        <button
          type="button"
          @click="handleGpuReminderSettings"
          class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
        >
          Reminder settings
        </button>
        <button
          type="button"
          @click="hideGPUReminderModal"
          class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
        >
          Yes, still using
        </button>
        <button
          type="button"
          @click="handleGpuReminderCleanup"
          class="rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400"
        >
          Go to destroy
        </button>
      </div>
    </section>
  </div>

  <!-- Log Modal -->
  <div
    v-if="logs.show"
    id="logModal"
    class="fixed inset-0 z-50 flex bg-zinc-950/70 p-3 backdrop-blur-sm sm:p-5"
    role="dialog"
    aria-modal="true"
    aria-labelledby="logModalTitle"
  >
    <section class="mx-auto flex h-full max-w-[1700px] flex-col overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-2xl shadow-zinc-950/30 dark:border-white/10 dark:bg-zinc-950">
      <header class="sticky top-0 z-10 border-b border-zinc-200 bg-white px-4 py-4 dark:border-white/10 dark:bg-zinc-900 sm:px-5">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
          <div class="min-w-0">
            <div id="logModalKind" class="mb-2 inline-flex items-center rounded-full border border-zinc-200 bg-zinc-50 px-2.5 py-1 text-xs font-semibold text-zinc-500 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-400">
              {{ logModalKind }}
            </div>
            <h2 id="logModalTitle" class="break-words text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">
              {{ logModalTitle }}
            </h2>
            <p id="logModalSubtitle" class="mt-1 break-words text-sm text-zinc-500 dark:text-zinc-400">
              {{ logModalSubtitle }}
            </p>
            <div :class="liveLogStateContainerClass">
              <span :class="liveLogStateIconClass"></span>
              <span>{{ liveLogStateLabel }}</span>
            </div>
          </div>
          <div class="flex shrink-0 flex-wrap gap-2">
            <button
              type="button"
              @click="downloadLogs"
              class="rounded-lg bg-emerald-500 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400"
            >
              Download visible logs
            </button>
            <button
              v-if="!stopStreamBtnHidden"
              type="button"
              @click="stopStream()"
              class="rounded-lg border border-zinc-200 bg-white px-3.5 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
            >
              {{ stopStreamBtnLabel }}
            </button>
            <button
              type="button"
              @click="clearLogs"
              class="rounded-lg border border-zinc-200 bg-white px-3.5 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
            >
              Clear
            </button>
            <button
              type="button"
              @click="closeLogModal"
              class="rounded-lg border border-zinc-200 bg-white px-3.5 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
            >
              Close
            </button>
          </div>
        </div>
        <div class="mt-4 grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
          <div>
            <label for="logSearch" class="sr-only">Search logs</label>
            <input
              id="logSearch"
              type="search"
              v-model="logs.search"
              autocomplete="off"
              placeholder="Grep logs..."
              class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none placeholder:text-zinc-400 focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100 dark:placeholder:text-zinc-500"
            />
          </div>
          <div id="logMatchCount" class="text-sm font-medium text-zinc-500 dark:text-zinc-400">
            {{ logs.matchCountLabel }}
          </div>
        </div>
        <div id="logLevelFilters" class="mt-3 flex flex-wrap gap-2">
          <button
            v-for="lvl in ['all', 'info', 'debug', 'warning', 'error']"
            :key="lvl"
            type="button"
            @click="logs.level = lvl"
            :class="logs.level === lvl ? activeLevelClass : inactiveLevelClass"
          >
            {{ lvl.toUpperCase() }}
          </button>
        </div>
      </header>
      <div id="logBox" ref="logBoxRef" class="min-h-0 flex-1 overflow-auto bg-zinc-50 p-3 font-mono text-xs leading-5 text-zinc-800 dark:bg-zinc-950 dark:text-zinc-200 sm:p-4">
        <template v-if="logEntries.length">
          <div
            v-for="entry in logEntries"
            :key="entry.index"
            class="log-row grid grid-cols-[4.5rem_minmax(0,1fr)] border-b border-zinc-200/70 bg-white/60 last:border-b-0 dark:border-white/5 dark:bg-white/[0.02]"
          >
            <div class="select-none px-3 py-1.5 text-right text-[11px] tabular-nums text-zinc-400 dark:text-zinc-600">
              {{ entry.index }}
            </div>
            <code
              class="min-w-0 whitespace-pre-wrap break-words px-3 py-1.5 text-zinc-800 dark:text-zinc-200"
              v-html="highlightLogLine(entry.line, logs.search)"
            ></code>
          </div>
        </template>
        <div
          v-else
          class="flex h-full min-h-64 items-center justify-center rounded-xl border border-dashed border-zinc-300 bg-white text-sm text-zinc-500 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-400"
        >
          <div class="flex items-center gap-3">
            <span v-if="logWaiting" class="spinner"></span>
            <span>{{ logWaitingMessage }}</span>
          </div>
        </div>
      </div>
    </section>
  </div>

  <!-- Danger Confirmation Modal -->
  <div
    v-if="dangerConfirm.show"
    id="dangerConfirmModal"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-zinc-950/55 p-4 backdrop-blur-sm dark:bg-zinc-950/80"
    role="dialog"
    aria-modal="true"
    aria-labelledby="dangerConfirmTitle"
  >
    <section class="w-full max-w-lg rounded-2xl border border-zinc-200 bg-white p-6 shadow-2xl shadow-zinc-950/20 dark:border-white/10 dark:bg-zinc-900 dark:shadow-black/50">
      <div id="dangerConfirmAccent" class="mb-4 inline-flex rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300">
        {{ dangerConfirm.accentText || "Confirmation required" }}
      </div>
      <h2 id="dangerConfirmTitle" class="text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">
        {{ dangerConfirm.title }}
      </h2>
      <p id="dangerConfirmBody" class="mt-3 text-sm leading-6 text-zinc-600 dark:text-zinc-300">
        {{ dangerConfirm.body }}
      </p>
      <label class="mt-5 grid gap-2 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
        <span id="dangerConfirmPrompt">Type <code class="font-semibold text-rose-600 dark:text-rose-400">{{ dangerConfirm.typedValue }}</code> to confirm</span>
        <input
          id="dangerConfirmInput"
          type="text"
          v-model="dangerConfirm.input"
          @keydown.enter="submitDangerConfirm"
          autocomplete="off"
          class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100"
        />
      </label>
      <div v-if="dangerConfirm.error" id="dangerConfirmError" class="mt-3 min-h-5 text-sm font-semibold text-rose-600 dark:text-rose-300">
        {{ dangerConfirm.error }}
      </div>
      <div class="mt-6 flex flex-wrap justify-end gap-3">
        <button
          type="button"
          @click="closeDangerConfirm(false)"
          class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
        >
          Cancel
        </button>
        <button
          type="button"
          @click="submitDangerConfirm"
          class="rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400"
        >
          Confirm
        </button>
      </div>
    </section>
  </div>

  <!-- Upgrade Command Clipboard Modal -->
  <div
    v-if="upgradeCommandModalOpen"
    id="upgradeCommandModal"
    @click.self="upgradeCommandModalOpen = false"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-zinc-950/55 p-4 backdrop-blur-sm dark:bg-zinc-950/80"
    role="dialog"
    aria-modal="true"
    aria-labelledby="upgradeCommandTitle"
  >
    <section class="w-full max-w-2xl overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-2xl shadow-zinc-950/20 dark:border-white/10 dark:bg-zinc-900 dark:shadow-black/50">
      <div class="border-b border-zinc-200 px-6 py-5 dark:border-white/10">
        <div class="mb-3 inline-flex rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">Clipboard ready</div>
        <h2 id="upgradeCommandTitle" class="text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Prepared upgrade command copied</h2>
        <p class="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">The command is ready to edit before running against the local HA cluster.</p>
      </div>
      <div class="grid gap-3 px-6 py-5 text-sm leading-6 text-zinc-700 dark:text-zinc-300">
        <div class="rounded-xl border border-sky-200 bg-sky-50 p-4 text-sky-900 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
          Review the Rancher target values before running it.
        </div>
        <div class="grid gap-2">
          <div class="flex gap-2"><span class="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-sky-500"></span><span>Set the chart <code>--version</code> to the Rancher version you want.</span></div>
          <div class="flex gap-2"><span class="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-sky-500"></span><span>Update any image registry, repository, tag, or legacy image override values if this upgrade needs them.</span></div>
          <div class="flex gap-2"><span class="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-sky-500"></span><span>Run it with the matching kubeconfig context for this HA.</span></div>
        </div>
      </div>
      <div class="flex flex-wrap justify-end gap-3 border-t border-zinc-200 px-6 py-4 dark:border-white/10">
        <button
          type="button"
          @click="upgradeCommandModalOpen = false"
          class="rounded-lg bg-sky-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-sky-500/20 hover:bg-sky-400"
        >
          Got it
        </button>
      </div>
    </section>
  </div>

  <!-- Toast/Status Notice -->
  <div
    v-if="notice.show"
    id="panelNotice"
    class="fixed bottom-5 right-5 z-[70] flex w-[min(28rem,calc(100vw-2.5rem))] rounded-2xl border border-zinc-200 bg-white p-4 shadow-2xl shadow-zinc-950/20 dark:border-white/10 dark:bg-zinc-900 dark:shadow-black/50"
    role="status"
    aria-live="polite"
  >
    <div class="flex items-start justify-between gap-4 w-full">
      <div class="min-w-0">
        <div id="panelNoticeTitle" class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">
          {{ notice.title }}
        </div>
        <div id="panelNoticeBody" class="mt-1 break-words text-sm leading-6 text-zinc-600 dark:text-zinc-300">
          {{ notice.body }}
        </div>
      </div>
      <button
        type="button"
        @click="hidePanelNotice"
        class="shrink-0 rounded-lg border border-zinc-200 bg-white px-2.5 py-1.5 text-xs font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.1]"
      >
        Dismiss
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, ref, watch, onMounted, onUnmounted } from "vue";
import {
  state,
  logs,
  dangerConfirm,
  upgradeCommandModalOpen,
  notice,
  hideGPUReminderModal,
  stopStream,
  closeLogModal,
  downloadLogs,
  closeDangerConfirm,
  submitDangerConfirm,
  hidePanelNotice,
  setActivePanelTab,
  setActiveDestroyTab,
  gpuReminderBody,
  renderLogViewer,
} from "./store.js";
import { highlightLogLine, lineMatchesLogLevel } from "../../static/control_panel_utils.js";

const activeLevelClass = "rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300";
const inactiveLevelClass = "rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.1]";

const logBoxRef = ref(null);

const handleGpuReminderSettings = () => {
  hideGPUReminderModal();
  setActivePanelTab("settings");
};

const handleGpuReminderCleanup = () => {
  hideGPUReminderModal();
  setActivePanelTab("destroy");
  setActiveDestroyTab("slots");
};

const logModalKind = computed(() => {
  if (logs.mode === "docker") return "Docker logs";
  if (logs.mode === "setup") return "Setup logs";
  if (logs.mode === "linodeSetup") return "Linode setup logs";
  if (logs.mode === "readiness") return "Readiness logs";
  if (logs.mode === "cleanup") return "Destroy logs";
  if (logs.mode === "linodeCleanup") return "Linode destroy logs";
  return "Pod logs";
});

const logModalTitle = computed(() => {
  if (logs.mode === "docker") return state.value?.clusters?.items?.find(c => c.id === logs.clusterId)?.name || "Rancher container";
  if (logs.mode === "setup") return "Setup";
  if (logs.mode === "linodeSetup") return "Linode setup";
  if (logs.mode === "readiness") return "Readiness";
  if (logs.mode === "cleanup") return "Destroy run";
  if (logs.mode === "linodeCleanup") return "Linode destroy run";
  return logs.podName || "No pod selected";
});

const logModalSubtitle = computed(() => {
  if (logs.mode === "docker") {
    const cluster = state.value?.clusters?.items?.find(c => c.id === logs.clusterId);
    return cluster?.loadBalancer ? `root@${cluster.loadBalancer} • docker logs rancher` : `${logs.clusterId} • docker logs rancher`;
  }
  if (logs.mode === "setup") return "go test -v -run ^TestHaSetup$ -timeout 90m -count=1 ./terratest";
  if (logs.mode === "linodeSetup") return "go test -v -run ^TestHaSetup$ -timeout 90m -count=1 ./terratest";
  if (logs.mode === "readiness") return state.value?.readiness?.command || "go test -v -run ^TestHAWaitReady$ -timeout 35m -count=1 ./terratest";
  if (logs.mode === "cleanup" || logs.mode === "linodeCleanup") return "go test -v -run TestHACleanup -timeout 20m ./terratest";
  return `${logs.namespace} • ${logs.clusterId} • ${logs.mode === "live" ? "live stream" : "tail snapshot"}`;
});

const liveLogStateLabel = computed(() => {
  const states = {
    idle: "Idle",
    connecting: "Connecting to logs...",
    live: "Live logs refreshing",
    stopped: "Live refresh paused",
    error: "Live refresh interrupted",
    setupRunning: "Setup running",
    setupDone: "Setup completed",
    setupError: "Setup failed",
    readinessRunning: "Readiness running",
    readinessDone: "Readiness completed",
    readinessError: "Readiness failed",
    cleanupRunning: "Destroy running",
    cleanupDone: "Destroy completed",
    cleanupError: "Destroy failed",
    linodeSetupRunning: "Linode setup running",
    linodeSetupDone: "Linode setup completed",
    linodeSetupError: "Linode setup failed",
    linodeCleanupRunning: "Linode destroy running",
    linodeCleanupDone: "Linode destroy completed",
    linodeCleanupError: "Linode destroy failed",
  };
  return states[logs.liveState] || "Idle";
});

const liveLogStateIconClass = computed(() => {
  const states = {
    idle: "bg-zinc-400",
    connecting: "bg-sky-500 animate-ping",
    live: "bg-emerald-500 animate-pulse",
    stopped: "bg-zinc-400",
    error: "bg-rose-500",
    setupRunning: "bg-sky-500 animate-pulse",
    setupDone: "bg-emerald-500",
    setupError: "bg-rose-500",
    readinessRunning: "bg-sky-500 animate-pulse",
    readinessDone: "bg-emerald-500",
    readinessError: "bg-rose-500",
    cleanupRunning: "bg-sky-500 animate-pulse",
    cleanupDone: "bg-emerald-500",
    cleanupError: "bg-rose-500",
    linodeSetupRunning: "bg-sky-500 animate-pulse",
    linodeSetupDone: "bg-emerald-500",
    linodeSetupError: "bg-rose-500",
    linodeCleanupRunning: "bg-sky-500 animate-pulse",
    linodeCleanupDone: "bg-emerald-500",
    linodeCleanupError: "bg-rose-500",
  };
  return `h-2.5 w-2.5 rounded-full ${states[logs.liveState] || "bg-zinc-400"}`;
});

const liveLogStateContainerClass = computed(() => {
  const states = {
    idle: "border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-400",
    connecting: "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300",
    live: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300",
    stopped: "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300",
    error: "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300",
    setupRunning: "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300",
    setupDone: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300",
    setupError: "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300",
    readinessRunning: "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300",
    readinessDone: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300",
    readinessError: "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300",
    cleanupRunning: "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300",
    cleanupDone: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300",
    cleanupError: "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300",
    linodeSetupRunning: "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300",
    linodeSetupDone: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300",
    linodeSetupError: "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300",
    linodeCleanupRunning: "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300",
    linodeCleanupDone: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300",
    linodeCleanupError: "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300",
  };
  return `mt-3 inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-semibold ${states[logs.liveState] || states.idle}`;
});

const stopStreamBtnLabel = computed(() => {
  const states = {
    idle: "Start live",
    connecting: "Stop live",
    live: "Stop live",
    stopped: "Resume live",
    error: "Resume live",
  };
  return states[logs.liveState] || "Live disabled";
});

const stopStreamBtnHidden = computed(() => {
  const operationContext = ["setup", "linodeSetup", "readiness", "cleanup", "linodeCleanup"].includes(logs.mode);
  return operationContext || logs.liveState.startsWith("cleanup") || logs.liveState.startsWith("setup") || logs.liveState.startsWith("readiness") || logs.liveState.startsWith("linode");
});

const logEntries = computed(() => {
  const query = logs.search.trim();
  const entries = logs.rawText ? logs.rawText.split("\n").map((line, index) => ({ line, index: index + 1 })) : [];
  return entries.filter(entry => {
    const queryMatches = query ? entry.line.toLowerCase().includes(query.toLowerCase()) : true;
    return queryMatches && lineMatchesLogLevel(entry.line, logs.level);
  });
});

const logWaiting = computed(() => {
  const waitingForLive = logs.mode === "live" && (logs.liveState === "connecting" || logs.liveState === "live");
  const waitingForSetup = logs.mode === "setup" && logs.liveState === "setupRunning";
  const waitingForReadiness = logs.mode === "readiness" && logs.liveState === "readinessRunning";
  const waitingForCleanup = logs.mode === "cleanup" && logs.liveState === "cleanupRunning";
  const waitingForLinodeSetup = logs.mode === "linodeSetup" && logs.liveState === "linodeSetupRunning";
  const waitingForLinodeCleanup = logs.mode === "linodeCleanup" && logs.liveState === "linodeCleanupRunning";
  return waitingForLive || waitingForSetup || waitingForReadiness || waitingForCleanup || waitingForLinodeSetup || waitingForLinodeCleanup;
});

const logWaitingMessage = computed(() => {
  if (logs.mode === "live" && (logs.liveState === "connecting" || logs.liveState === "live")) {
    return "Waiting for live log lines...";
  }
  if (logs.mode === "setup" && logs.liveState === "setupRunning") {
    return "Waiting for setup output...";
  }
  if (logs.mode === "linodeSetup" && logs.liveState === "linodeSetupRunning") {
    return "Waiting for Linode setup output...";
  }
  if (logs.mode === "readiness" && logs.liveState === "readinessRunning") {
    return "Waiting for readiness output...";
  }
  if (logs.mode === "cleanup" && logs.liveState === "cleanupRunning") {
    return "Waiting for cleanup output...";
  }
  if (logs.mode === "linodeCleanup" && logs.liveState === "linodeCleanupRunning") {
    return "Waiting for Linode cleanup output...";
  }
  if (logs.search.trim() || logs.level !== "all") {
    return "No matching log lines.";
  }
  return "No logs loaded yet.";
});

const clearLogs = () => {
  logs.rawText = "";
  renderLogViewer();
};

watch(() => [logs.search, logs.level, logs.rawText], () => {
  renderLogViewer();
});

watch(() => logEntries.value.length, () => {
  if (logBoxRef.value && !logs.search.trim()) {
    nextTick(() => {
      logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight;
    });
  }
});

// Escape key listener for modals
const handleKeyDown = event => {
  if (event.key === "Escape") {
    if (upgradeCommandModalOpen.value) {
      upgradeCommandModalOpen.value = false;
    }
    if (gpuReminderModalOpen.value) {
      hideGPUReminderModal();
    }
    if (logs.show) {
      closeLogModal();
    }
    if (dangerConfirm.show) {
      closeDangerConfirm(false);
    }
  }
};

onMounted(() => {
  window.addEventListener("keydown", handleKeyDown);
});

onUnmounted(() => {
  window.removeEventListener("keydown", handleKeyDown);
});
</script>
