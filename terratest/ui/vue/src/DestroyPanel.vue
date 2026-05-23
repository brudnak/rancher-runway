<template>
  <div class="mx-auto max-w-5xl">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Destroy Slots</h2>
        <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
          Choose exactly one recorded run slot to destroy. Setup, readiness, and destroy are serialized so Terraform state, AWS actions, and logs stay unambiguous.
          The slot record is removed only after Terraform destroy succeeds.
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
          v-if="selectedRunId"
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
          <h3 class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">Post-destroy artifact cleanup</h3>
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
  cleanupStarting,
  dismissedCleanupResultKey,
  costResetting,
  localArtifactsCleaning,
  lifecycleRunning,
  setActiveDestroyTab,
  openCleanupLogs,
  openLocalPath,
  runCleanup,
  resetCostLedger,
  cleanLocalArtifacts,
} from "./store.js";

const secondaryButtonClass = "rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]";
const disabledButtonClass = "rounded-lg bg-zinc-200 px-4 py-2.5 text-sm font-semibold text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400";
const dangerButtonClass = "rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400";

const activeTabClass = "rounded-lg bg-white px-3.5 py-2 text-sm font-semibold text-zinc-900 shadow-sm dark:bg-white/[0.08] dark:text-zinc-100";
const inactiveTabClass = "rounded-lg px-3.5 py-2 text-sm font-semibold text-zinc-600 hover:bg-white dark:text-zinc-300 dark:hover:bg-white/[0.06]";

const runs = computed(() => Array.isArray(state.value?.workspace?.runs) ? state.value.workspace.runs : []);
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
const destroying = run => Boolean(cleanupRunningForRun(run) && sameRunKey(cleanupForRun(run)?.runId, run?.runId));
const isSelected = run => Boolean(selectedRunId.value && sameRunKey(selectedRunId.value, run?.runId));
const slotCardClass = run => destroying(run) || pendingDestroy(run)
  ? "border-sky-200 bg-sky-50/60 dark:border-sky-500/25 dark:bg-sky-500/10"
  : isSelected(run)
    ? "border-emerald-200 bg-emerald-50/60 dark:border-emerald-500/25 dark:bg-emerald-500/10"
    : "border-zinc-200 bg-white dark:border-white/10 dark:bg-white/[0.03]";
const slotDestroyDisabled = run => Boolean(
  bootPending.value ||
  setupRunningForRun(run) ||
  readinessRunningForRun(run) ||
  cleanupRunningForRun(run) ||
  cleanupStarting.value
);
const slotDestroyTitle = run => bootPending.value
  ? "Startup safety check is still loading run slots and operation state."
  : setupRunningForRun(run)
    ? "Wait for setup to finish before destroying a run slot."
    : readinessRunningForRun(run)
      ? "Wait for readiness checks to finish before destroying a run slot."
      : cleanupRunningForRun(run)
        ? "Wait for the current destroy to finish before starting another one."
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

const cleanupStatusTone = computed(() => {
  const cleanup = activeCleanup.value;
  if (cleanup?.running) return "running";
  if (cleanup?.finishedAt && !cleanup?.error && !cleanupDismissed.value) return "success";
  if (cleanup?.error && !cleanupDismissed.value) return "error";
  return "idle";
});

const cleanupStatusLabel = computed(() => {
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
  return Boolean(cleanup?.finishedAt && !cleanupDismissed.value);
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
