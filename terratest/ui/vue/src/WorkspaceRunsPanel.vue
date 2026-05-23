<template>
  <div class="mb-5 flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
    <div class="min-w-0">
      <div class="inline-flex max-w-full items-center rounded-full border border-zinc-200 bg-zinc-50 px-3 py-1.5 text-xs font-semibold text-zinc-600 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300">
        {{ workspace.mode || "single-run workspace" }}
      </div>
      <h2 class="mt-3 text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">{{ title }}</h2>
      <p class="mt-2 max-w-4xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">{{ summary }}</p>
    </div>
    <div class="grid shrink-0 gap-2 sm:grid-cols-3 lg:min-w-[26rem]">
      <div class="run-summary-stat">
        <div class="run-summary-label">Slots</div>
        <div class="run-summary-value">{{ runs.length }}</div>
        <div class="run-summary-help">{{ totalHAs }} HA target{{ totalHAs === 1 ? "" : "s" }}</div>
      </div>
      <div class="run-summary-stat">
        <div class="run-summary-label">Discovered</div>
        <div class="run-summary-value">{{ clusters.length }}</div>
        <div class="run-summary-help">cluster records</div>
      </div>
      <div class="run-summary-stat" :data-tone="canStart ? 'ready' : 'locked'">
        <div class="run-summary-label">Next setup</div>
        <div class="run-summary-value">{{ nextSetupLabel }}</div>
        <div class="run-summary-help">{{ awsItems.length }} AWS resource{{ awsItems.length === 1 ? "" : "s" }} visible</div>
      </div>
      <div v-if="sharedPaths.length && !runs.length" class="run-summary-stat">
        <div class="run-summary-label">Workspace guard</div>
        <div class="run-summary-value">{{ sharedPaths.length }}</div>
        <div class="run-summary-help">watched paths</div>
      </div>
    </div>
  </div>

  <div v-if="activeOperations.length" class="mb-5">
    <ActiveOperationsBanner :operations="activeOperations" />
  </div>

  <div v-if="!runs.length && !activeOperations.length" class="rounded-xl border border-zinc-200 bg-zinc-50 p-5 dark:border-white/10 dark:bg-white/[0.03]">
    <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">No run slots yet</h3>
    <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
      Setup is the only place that can create a new AWS run. Resolve the plan there, review the Helm commands, then approve AWS setup.
    </p>
    <div class="mt-4">
      <button
        type="button"
        @click="handleRunAction(null, 'open-setup')"
        class="run-action-button run-action-button--primary"
        :class="{ 'run-action-button--disabled': bootPending || lifecycleRunning }"
        :disabled="bootPending || lifecycleRunning"
        :title="bootPending ? 'Startup safety check is still running.' : lifecycleRunning ? 'Wait for the active lifecycle operation to finish.' : ''"
      >
        Open setup
      </button>
    </div>
  </div>

  <template v-else-if="runs.length">
    <div v-if="failedReadinessRun" class="mb-5">
      <ReadinessFailedBanner :run="failedReadinessRun" />
    </div>

    <div class="grid gap-3">
      <article
        v-for="run in runs"
        :key="run.runId || run.slotId || run.slotName || JSON.stringify(run)"
        class="run-row"
        :data-tone="runTone(run)"
      >
        <div class="run-row-main">
          <div class="run-row-titlebar">
            <h3 class="run-title">Run {{ run.runId || "unknown" }}</h3>
            <span class="run-status-pill" :data-tone="runTone(run)">
              {{ (operationForRun(run) ? "running" : run.status || "recorded").replaceAll("_", " ") }}
            </span>
            <span v-if="isCurrent(run)" class="run-current-pill">current slot</span>
            <span v-if="operationForRun(run)" class="run-live-pill">
              <span class="spinner run-progress-spinner"></span>{{ operationForRun(run).label }} running{{ operationStartedLabel(operationForRun(run).operation) }}
            </span>
          </div>
          <div v-if="run.updatedAt" class="run-muted run-updated">Updated {{ timeLabel(run.updatedAt) }}</div>
          <div class="run-progress" aria-label="Run lifecycle">
            <div
              v-for="step in timelineSteps(run)"
              :key="step.label"
              class="run-progress-step"
              :data-state="step.state"
            >
              <span :class="step.state === 'active' ? 'spinner run-progress-spinner' : 'run-progress-dot'"></span>
              <span>{{ step.label }}</span>
            </div>
          </div>
          <div class="run-kpi-grid">
            <div class="run-kpi">
              <div class="run-kpi-label">Rancher</div>
              <div class="run-kpi-value" :title="runVersionsLabel(run)">{{ runVersionsLabel(run) }}</div>
            </div>
            <div class="run-kpi">
              <div class="run-kpi-label">Clusters</div>
              <div class="run-kpi-value">{{ runStats(run).management }} management, {{ runStats(run).downstream }} downstream</div>
            </div>
            <div class="run-kpi">
              <div class="run-kpi-label">AWS prefix</div>
              <div class="run-kpi-value">{{ run.awsPrefix || "not recorded" }}</div>
            </div>
            <div class="run-kpi">
              <div class="run-kpi-label">Owner</div>
              <div class="run-kpi-value">{{ run.owner || "not recorded" }}</div>
            </div>
          </div>
          <div class="run-footline">
            <div><strong>Hostname:</strong> {{ runHostnameLabel(run) }}</div>
            <div><strong>Terraform:</strong> <span :title="run.terraformStatePath || run.terraformBackend || ''">{{ compactPath(run.terraformStatePath || run.terraformBackend || "not recorded") }}</span></div>
          </div>
          <div
            v-if="run.gpuWorkerEnabled"
            class="mt-3 rounded-lg border border-rose-200 bg-rose-50 px-3.5 py-3 text-sm font-semibold text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200"
          >
            GPU worker node{{ Number(run.totalHAs || 1) === 1 ? "" : "s" }} requested:
            {{ run.totalHAs || 1 }} x {{ run.gpuWorkerInstanceType || "GPU instance" }}. Do not leave running unused.
          </div>
        </div>

        <div class="run-command-panel">
          <div class="run-primary-actions">
            <RunAction
              action="view-clusters"
              :run-id="run.runId"
              label="View clusters"
              :variant="runStats(run).total ? 'primary' : 'secondary'"
              :disabled="!runStats(run).total"
              :title="runStats(run).total ? 'Open cluster details for this run.' : 'No cluster records discovered for this run yet.'"
              @click="handleRunAction(run, 'view-clusters')"
            />
            <RunAction
              v-if="isCurrent(run) || readinessRunningForRun(run)"
              action="check-readiness"
              :run-id="run.runId"
              label="Readiness"
              variant="blue"
              :disabled="readinessDisabled(run)"
              :title="readinessTitle(run)"
              @click="handleRunAction(run, 'check-readiness')"
            />
            <RunAction
              v-bind="lifecycleAction(run)"
              @click="handleRunAction(run, lifecycleAction(run).action)"
            />
          </div>
          <div class="run-utility-strip" aria-label="Run utilities">
            <RunAction
              action="open-run-folder"
              :run-id="run.runId"
              label="Folder"
              variant="utility"
              :disabled="!runFolderAvailable(run)"
              :title="runFolderAvailable(run) ? 'Open this run slot folder in Finder.' : 'Run folder is not available locally.'"
              @click="handleRunAction(run, 'open-run-folder')"
            />
            <RunAction
              action="copy-terraform-path"
              :run-id="run.runId"
              label="TF path"
              variant="utility"
              :disabled="!runTerraformPath(run)"
              :title="runTerraformPath(run) ? 'Copy the Terraform module/state path for this run.' : 'Terraform path is not recorded yet.'"
              @click="handleRunAction(run, 'copy-terraform-path')"
            />
            <RunAction
              action="open-setup-logs"
              :run-id="run.runId"
              label="Setup log"
              variant="utility"
              @click="handleRunAction(run, 'open-setup-logs')"
            />
            <RunAction
              action="open-readiness-logs"
              :run-id="run.runId"
              label="Ready log"
              variant="utility"
              @click="handleRunAction(run, 'open-readiness-logs')"
            />
          </div>
        </div>
      </article>
    </div>
  </template>
</template>

<script setup>
import { computed, defineComponent, h } from "vue";
import {
  state,
  bootPending,
  refreshStatus,
  activeClusterRunKey,
  activeClusterHAKey,
  selectedCleanupRunId,
  setActivePanelTab,
  setActiveDestroyTab,
  runReadiness,
  openLocalPath,
  copyTextToClipboard,
  openSetupLogs,
  openReadinessLogs,
  openCleanupLogs,
  stopOperationThenOpenDestroy,
} from "./store.js";

const sameRunKey = (left, right) => String(left || "").trim() === String(right || "").trim();
const trimTrailingPathSeparator = value => String(value || "").replace(/[\\/]+$/, "");
const parentPath = value => {
  const path = trimTrailingPathSeparator(value);
  const index = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  return index > 0 ? path.slice(0, index) : path;
};

const workspace = computed(() => state.value?.workspace || {});
const runs = computed(() => Array.isArray(workspace.value?.runs) ? workspace.value.runs : []);
const sharedPaths = computed(() => Array.isArray(workspace.value?.sharedPathLabels) ? workspace.value.sharedPathLabels : []);
const clusters = computed(() => state.value?.clusters && Array.isArray(state.value.clusters.items) ? state.value.clusters.items : []);
const awsItems = computed(() => Array.isArray(state.value?.aws?.items) ? state.value.aws.items : []);
const currentRunID = computed(() => workspace.value?.currentRun?.runId || "");
const totalHAs = computed(() => runs.value.reduce((total, run) => total + Number(run.totalHAs || 1), 0));

const awsLifecycleRunning = computed(() => Boolean(state.value?.setup?.running || state.value?.readiness?.running || state.value?.cleanup?.running));
const linodeLifecycleRunning = computed(() => Boolean(state.value?.linodeSetup?.running || state.value?.linodeCleanup?.running));
const lifecycleRunning = computed(() => Boolean(awsLifecycleRunning.value || linodeLifecycleRunning.value));
const canStart = computed(() => Boolean(workspace.value?.canStartIsolatedRun && !lifecycleRunning.value && !bootPending.value));

const activeOperations = computed(() => [
  { mode: "setup", label: "Setup", operation: state.value?.setup },
  { mode: "readiness", label: "Readiness", operation: state.value?.readiness },
  { mode: "cleanup", label: "Destroy", operation: state.value?.cleanup },
  { mode: "linodeSetup", label: "Linode setup", operation: state.value?.linodeSetup },
  { mode: "linodeCleanup", label: "Linode destroy", operation: state.value?.linodeCleanup },
].filter(item => item.operation?.running));

const title = computed(() => runs.value.length
  ? `${runs.value.length} recorded run slot${runs.value.length === 1 ? "" : "s"}`
  : "No recorded runs");

const summary = computed(() => activeOperations.value.length
  ? `${activeOperations.value.length} lifecycle operation${activeOperations.value.length === 1 ? "" : "s"} active: ${activeOperations.value.map(item => item.label).join(", ")}. Each provider lane stays serialized against its own state.`
  : runs.value.length
    ? "Every slot below has isolated Terraform state, deployment output, kubeconfigs, AWS names, logs, and a dedicated destroy target."
    : "Use Setup to resolve and approve a Rancher Runway plan. The run will appear here before AWS resources are created.");

const nextSetupLabel = computed(() => canStart.value
  ? "Ready"
  : bootPending.value
    ? "Checking state"
    : lifecycleRunning.value
      ? "Running"
      : "Locked");

const runHasFailure = run => {
  const status = String(run?.status || "").toLowerCase();
  return status.includes("failed") || status.includes("error");
};

const failedReadinessRun = computed(() => {
  const readiness = state.value?.readiness || {};
  if (readiness.running || !readiness.error) {
    return runs.value.find(run => runHasFailure(run)) || null;
  }
  const failedRunId = readiness.runId || "";
  return runs.value.find(run => sameRunKey(run.runId, failedRunId)) || runs.value.find(run => runHasFailure(run)) || null;
});

const operationForRun = run => {
  const runId = run?.runId || "";
  return activeOperations.value.find(item => sameRunKey(item.operation?.runId, runId)) || null;
};

const runTone = run => {
  const status = String(run?.status || "").toLowerCase();
  if (operationForRun(run)) {
    return "sky";
  }
  if (runHasFailure(run)) {
    return "rose";
  }
  if (status === "ready" || status.includes("complete")) {
    return "emerald";
  }
  return "zinc";
};

const runStats = run => {
  const runId = run?.runId || "";
  const items = clusters.value.filter(cluster => sameRunKey(cluster.runId, runId));
  return {
    total: items.length,
    reachable: items.filter(cluster => cluster.reachable).length,
    management: items.filter(cluster => cluster.type !== "downstream").length,
    downstream: items.filter(cluster => cluster.type === "downstream").length,
  };
};

const runVersionsLabel = run => Array.isArray(run?.rancherVersions) && run.rancherVersions.length
  ? run.rancherVersions.join(", ")
  : "not recorded";

const runHostnameLabel = run => {
  if (!run) {
    return "not recorded";
  }
  if (run.deploymentType === "hosted-tenant-k3s") {
    return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-t*.${run.route53Fqdn}` : run.route53Fqdn || "generated per slot";
  }
  if (run.deploymentType === "linode-docker-cattle") {
    return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-*.${run.route53Fqdn}` : run.route53Fqdn || "generated per slot";
  }
  if (run.customHostnamePrefix) {
    return `${run.customHostnamePrefix}.${run.route53Fqdn || ""}`.replace(/\.$/, "");
  }
  return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-h*.${run.route53Fqdn}` : run.route53Fqdn || "generated per slot";
};

const compactPath = value => {
  const text = String(value || "");
  if (text.length <= 68) {
    return text;
  }
  return `${text.slice(0, 24)}...${text.slice(-36)}`;
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
  if (haRoot) {
    return haRoot.replace(/[\\/]ha$/, "");
  }
  return "";
};

const runFolderAvailable = run => Boolean(runFolderPath(run) && run?.runFolderExists !== false);
const runTerraformPath = run => run?.terraformModuleDir || (run?.terraformStatePath ? parentPath(run.terraformStatePath) : "") || run?.terraformBackend || "";
const runIsLinodeDocker = run => run?.deploymentType === "linode-docker-cattle";
const isCurrent = run => Boolean(currentRunID.value && sameRunKey(run.runId, currentRunID.value));
const readinessRunningForRun = run => Boolean(state.value?.readiness?.running && sameRunKey(state.value.readiness.runId, run.runId));
const setupRunningForRun = run => Boolean(state.value?.setup?.running && sameRunKey(state.value.setup.runId, run.runId));
const linodeSetupRunningForRun = run => Boolean(state.value?.linodeSetup?.running && sameRunKey(state.value.linodeSetup.runId, run.runId));
const providerLifecycleRunning = run => runIsLinodeDocker(run) ? linodeLifecycleRunning.value : awsLifecycleRunning.value;

const readinessDisabled = run => Boolean(providerLifecycleRunning(run) || state.value?.readiness?.running || !isCurrent(run));

const readinessTitle = run => bootPending.value
  ? "Startup safety check is still running."
  : providerLifecycleRunning(run)
    ? "Wait for the active lifecycle operation to finish."
    : state.value?.readiness?.running
      ? "Wait for the active readiness check to finish."
      : !isCurrent(run)
        ? "Readiness currently runs against the active/current slot only."
        : runIsLinodeDocker(run)
          ? "Check Docker Rancher readiness for the current run."
          : "Check readiness for the current run.";

const lifecycleAction = run => {
  if (setupRunningForRun(run) || linodeSetupRunningForRun(run)) {
    return {
      action: "stop-setup-open-destroy",
      runId: run.runId,
      label: "Stop, then destroy",
      variant: "danger",
      title: "Requires typing confirm before stopping setup and opening Destroy.",
    };
  }
  if (readinessRunningForRun(run)) {
    return {
      action: "stop-readiness-open-destroy",
      runId: run.runId,
      label: "Stop, then destroy",
      variant: "danger",
      title: "Requires typing confirm before stopping readiness and opening Destroy.",
    };
  }
  const disabled = Boolean(providerLifecycleRunning(run) || bootPending.value);
  const failed = runHasFailure(run);
  return {
    action: "open-destroy",
    runId: run.runId,
    label: failed ? "Destroy failed run" : "Destroy",
    variant: failed ? "danger" : "secondary",
    disabled,
    title: disabled ? "Wait for the active lifecycle operation to finish." : "Open the Destroy tab for this slot.",
  };
};

const timelineSteps = run => {
  const status = String(run?.status || "").toLowerCase();
  const setupRunning = setupRunningForRun(run) || linodeSetupRunningForRun(run);
  const readinessRunning = readinessRunningForRun(run);
  const cleanupRunning = (state.value?.cleanup?.running && sameRunKey(state.value.cleanup.runId, run.runId)) ||
    (state.value?.linodeCleanup?.running && sameRunKey(state.value.linodeCleanup.runId, run.runId));
  const setupDone = status.includes("setup_complete") || status === "ready" || status.includes("readiness") || status.includes("cleanup");
  const readinessDone = status === "ready";
  return [
    { label: "Setup", state: setupRunning ? "active" : status.includes("setup_failed") ? "failed" : setupDone ? "done" : "waiting" },
    { label: "Readiness", state: readinessRunning ? "active" : status.includes("readiness_failed") ? "failed" : readinessDone ? "done" : "waiting" },
    { label: "Destroy", state: cleanupRunning ? "active" : status.includes("cleanup_failed") ? "failed" : "waiting" },
  ];
};

const timeLabel = value => value ? new Date(value).toLocaleTimeString() : "";
const operationStartedLabel = operation => operation?.startedAt ? ` since ${new Date(operation.startedAt).toLocaleTimeString()}` : "";

const actionClass = (variant = "secondary", disabled = false) => {
  const safeVariant = ["primary", "blue", "danger", "utility"].includes(variant) ? variant : "secondary";
  return `run-action-button run-action-button--${safeVariant}${disabled ? " run-action-button--disabled" : ""}`;
};

const RunAction = defineComponent({
  props: {
    action: { type: String, required: true },
    runId: { type: String, default: "" },
    label: { type: String, required: true },
    variant: { type: String, default: "secondary" },
    disabled: { type: Boolean, default: false },
    title: { type: String, default: "" },
  },
  emits: ["click"],
  setup(props, { emit }) {
    return () => h("button", {
      type: "button",
      "data-run-action": props.action,
      "data-run-id": props.runId || "",
      disabled: props.disabled || undefined,
      title: props.title || undefined,
      class: actionClass(props.variant, props.disabled),
      onClick: (event) => emit("click", event),
    }, props.label);
  },
});

const operationLogAction = mode => {
  if (mode === "readiness") {
    return "open-readiness-logs";
  }
  if (mode === "cleanup" || mode === "linodeCleanup") {
    return "open-cleanup-logs";
  }
  return "open-setup-logs";
};

const handleRunAction = (runOrId, action) => {
  const runId = typeof runOrId === "string" ? runOrId : (runOrId?.runId || "");
  const run = typeof runOrId === "object" ? runOrId : runs.value.find(r => sameRunKey(r.runId, runId));

  if (action === 'open-setup') {
    setActivePanelTab('setup');
  } else if (action === 'view-clusters') {
    activeClusterRunKey.value = runId;
    activeClusterHAKey.value = '';
    setActivePanelTab('clusters');
  } else if (action === 'check-readiness') {
    runReadiness();
  } else if (action === 'open-run-folder') {
    if (!runFolderAvailable(run)) {
      refreshStatus.value = 'Run folder is not available locally.';
      return;
    }
    openLocalPath(runFolderPath(run));
  } else if (action === 'copy-terraform-path') {
    copyTextToClipboard(runTerraformPath(run), 'Copied Terraform path to clipboard.');
  } else if (action === 'open-setup-logs') {
    openSetupLogs(runIsLinodeDocker(run));
  } else if (action === 'open-readiness-logs') {
    openReadinessLogs();
  } else if (action === 'open-cleanup-logs') {
    openCleanupLogs(runIsLinodeDocker(run));
  } else if (action === 'open-destroy') {
    selectedCleanupRunId.value = runId;
    setActiveDestroyTab('slots');
    setActivePanelTab('destroy');
  } else if (action === 'stop-setup-open-destroy') {
    stopOperationThenOpenDestroy(runIsLinodeDocker(run) ? 'linodeSetup' : 'setup', runId);
  } else if (action === 'stop-readiness-open-destroy') {
    stopOperationThenOpenDestroy('readiness', runId);
  }
};

const ActiveOperationsBanner = defineComponent({
  props: { operations: { type: Array, required: true } },
  setup(props) {
    return () => h("div", { class: "rounded-xl border border-sky-200 bg-sky-50 p-4 dark:border-sky-500/25 dark:bg-sky-500/10" }, [
      h("div", { class: "flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between" }, [
        h("div", { class: "min-w-0" }, [
          h("div", { class: "inline-flex items-center rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-sky-700 shadow-sm dark:bg-white/[0.08] dark:text-sky-200" }, [
            h("span", { class: "spinner mr-1.5 !h-3 !w-3 !border-[1.5px]" }),
            `${props.operations.length} lifecycle op${props.operations.length === 1 ? "" : "s"} running`,
          ]),
          h("h3", { class: "mt-2 text-base font-semibold text-sky-950 dark:text-sky-100" }, "Active run work"),
          h("p", { class: "mt-1 text-sm leading-6 text-sky-800/80 dark:text-sky-100/75" }, "Setup, readiness, and destroy actions stay locked only where they would collide with active state."),
        ]),
      ]),
      h("div", { class: "mt-3 grid gap-2 xl:grid-cols-2" }, props.operations.map(({ mode, label, operation }) => {
        const runId = operation?.runId || "";
        const stopAction = mode === "readiness"
          ? { action: "stop-readiness-open-destroy", label: "Stop, then destroy" }
          : mode === "setup" || mode === "linodeSetup"
            ? { action: "stop-setup-open-destroy", label: "Stop, then destroy" }
            : null;
        return h("div", { class: "flex flex-col gap-3 rounded-lg border border-sky-200/80 bg-white/70 p-3 dark:border-sky-400/20 dark:bg-black/10 md:flex-row md:items-center md:justify-between" }, [
          h("div", { class: "min-w-0" }, [
            h("div", { class: "text-sm font-semibold text-sky-950 dark:text-sky-100" }, `${label} · ${runId ? `Run ${runId}` : "Run state publishing"}`),
            h("div", { class: "mt-1 text-xs text-sky-800/70 dark:text-sky-100/65" }, operation?.startedAt ? `Started ${new Date(operation.startedAt).toLocaleTimeString()}` : "Starting now"),
          ]),
          h("div", { class: "flex shrink-0 flex-wrap gap-2" }, [
            h(RunAction, { action: operationLogAction(mode), runId, label: "Logs", onClick: () => handleRunAction(runId, operationLogAction(mode)) }),
            stopAction ? h(RunAction, { ...stopAction, runId, variant: "danger", title: "Requires typing confirm before stopping and opening Destroy.", onClick: () => handleRunAction(runId, stopAction.action) }) : null,
          ]),
        ]);
      })),
    ]);
  },
});

const ReadinessFailedBanner = defineComponent({
  props: { run: { type: Object, required: true } },
  setup(props) {
    return () => h("div", { class: "rounded-xl border border-rose-200 bg-rose-50 p-4 dark:border-rose-500/25 dark:bg-rose-500/10" }, [
      h("div", { class: "flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between" }, [
        h("div", { class: "min-w-0" }, [
          h("div", { class: "inline-flex items-center rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-rose-700 shadow-sm dark:bg-white/[0.08] dark:text-rose-200" }, "Readiness failed"),
          h("h3", { class: "mt-2 text-base font-semibold text-rose-950 dark:text-rose-100" }, `Run ${props.run.runId || "unknown"} did not become ready`),
          h("p", { class: "mt-1 text-sm leading-6 text-rose-800/80 dark:text-rose-100/75" }, "If a manual Helm command left Rancher unhealthy, destroy this slot from the recorded Terraform state and start again with a corrected command."),
        ]),
        h("div", { class: "flex shrink-0 flex-wrap gap-2" }, [
          h(RunAction, { action: "open-readiness-logs", runId: props.run.runId, label: "Readiness logs", onClick: () => handleRunAction(props.run.runId, "open-readiness-logs") }),
          h(RunAction, { action: "open-destroy", runId: props.run.runId, label: "Destroy failed run", variant: "danger", onClick: () => handleRunAction(props.run.runId, "open-destroy") }),
        ]),
      ]),
    ]);
  },
});
</script>
