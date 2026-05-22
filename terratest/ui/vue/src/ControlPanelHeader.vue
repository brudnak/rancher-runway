<template>
  <div class="min-w-0">
    <div class="mb-4 flex flex-wrap items-center gap-2">
      <div class="inline-flex items-center rounded-full border border-zinc-200 bg-white px-3 py-1 text-xs font-medium text-zinc-500 shadow-sm dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">
        Local control panel
      </div>
      <div
        class="inline-flex items-center rounded-full border border-zinc-200 bg-zinc-50 px-3 py-1 text-xs font-semibold text-zinc-600 shadow-sm dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300"
        :title="buildTitle"
      >
        {{ buildLabel }}
      </div>
    </div>

    <h1 class="text-2xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50 sm:text-3xl">
      Rancher Runway Control Panel
    </h1>
    <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
      Local-only viewer for active Rancher Runway runs, downstream clusters, kubeconfigs, logs, and cleanup.
    </p>
    <div
      v-if="sessionMeta"
      class="mt-3 text-xs font-medium text-zinc-500 dark:text-zinc-400"
      :title="panel?.configPath || ''"
    >
      {{ sessionMeta }}
    </div>

    <div class="mt-4 flex flex-wrap gap-2" aria-live="polite">
      <span
        v-for="chip in chips"
        :key="chip.key"
        class="panel-chip"
        :class="chipToneClass(chip.tone)"
      >
        <span v-if="chip.running" class="spinner !h-3 !w-3 !border-[1.5px]"></span>
        <span>{{ chip.label }}</span>
        <span class="panel-chip-value">{{ chip.value }}</span>
      </span>
    </div>

    <div
      v-if="panel?.starterConfigCreated"
      class="mt-4 max-w-3xl rounded-xl border border-sky-200 bg-sky-50 px-4 py-3 text-sm font-medium text-sky-800 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-200"
    >
      Created starter config at {{ panel.configPath }}. Fill in the blocked setup values below before starting setup.
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";

const state = ref(window.rancherControlPanelState || {});
const bootPending = ref(true);
const refreshedAt = ref(null);

const panel = computed(() => state.value?.panel || {});
const build = computed(() => panel.value?.build || {});

const buildLabel = computed(() => {
  const shortCommit = String(build.value?.commitShort || "").trim();
  const modified = Boolean(build.value?.modified);
  return shortCommit ? `Build ${shortCommit}${modified ? "*" : ""}` : "Build unknown";
});

const buildTitle = computed(() => {
  const fullCommit = String(build.value?.commit || "").trim();
  const buildDate = String(build.value?.buildDate || "").trim();
  const modified = Boolean(build.value?.modified);
  const titleParts = [];

  if (fullCommit) {
    titleParts.push(`Commit: ${fullCommit}`);
  }
  if (buildDate) {
    titleParts.push(`Built: ${buildDate}`);
  }
  if (modified) {
    titleParts.push("Working tree had local changes when this binary was built.");
  }

  return titleParts.length ? titleParts.join("\n") : "No build commit was embedded in this binary.";
});

const sessionMeta = computed(() => {
  if (!panel.value?.sessionId) {
    return "";
  }

  const started = panel.value.startedAt ? new Date(panel.value.startedAt).toLocaleTimeString() : "";
  const pieces = [`Panel ${panel.value.sessionId}`];
  if (started) {
    pieces.push(`started ${started}`);
  }
  if (panel.value.repoRoot) {
    pieces.push(panel.value.repoRoot);
  }
  return pieces.join(" • ");
});

const clusterItems = currentState => (
  currentState && currentState.clusters && Array.isArray(currentState.clusters.items)
    ? currentState.clusters.items
    : []
);

const activeGPUClusters = currentState => clusterItems(currentState).filter(cluster =>
  cluster?.type === "local" && (cluster.gpuWorkerIp || cluster.gpuWorkerPrivateIp)
);

const activeOperation = computed(() => [
  ["setup", "Setup", state.value?.setup],
  ["readiness", "Readiness", state.value?.readiness],
  ["cleanup", "Destroy", state.value?.cleanup],
  ["linodeSetup", "Linode setup", state.value?.linodeSetup],
  ["linodeCleanup", "Linode destroy", state.value?.linodeCleanup],
].find(([, , operation]) => operation?.running));

const operationChip = computed(() => {
  if (activeOperation.value) {
    const [, label, operation] = activeOperation.value;
    return {
      key: "operation",
      label,
      value: operation?.runId ? `Run ${operation.runId}` : "Running",
      tone: "sky",
      running: true,
    };
  }

  if (bootPending.value) {
    return {
      key: "operation",
      label: "Safety check",
      value: "Loading state",
      tone: "sky",
      running: true,
    };
  }

  return { key: "operation", label: "Operation", value: "Idle", tone: "zinc", running: false };
});

const gpuChip = computed(() => {
  const clusters = activeGPUClusters(state.value);
  if (!clusters.length) {
    return null;
  }

  const instanceTypes = [...new Set(clusters.map(cluster => cluster.gpuWorkerInstanceType).filter(Boolean))];
  return {
    key: "gpu",
    label: clusters.length === 1 ? "GPU node deployed" : "GPU nodes deployed",
    value: instanceTypes.length === 1 ? `${clusters.length} ${instanceTypes[0]}` : `${clusters.length} deployed`,
    tone: "rose",
    running: false,
  };
});

const chips = computed(() => {
  const runs = Array.isArray(state.value?.workspace?.runs) ? state.value.workspace.runs : [];
  const totalHAs = runs.reduce((total, run) => total + Number(run.totalHAs || 1), 0);
  const clusters = clusterItems(state.value);
  const reachable = clusters.filter(cluster => cluster.reachable).length;
  const awsItems = Array.isArray(state.value?.aws?.items) ? state.value.aws.items : [];
  const freshness = refreshedAt.value ? new Date(refreshedAt.value).toLocaleTimeString() : "Waiting";

  return [
    {
      key: "runs",
      label: "Runs",
      value: `${runs.length} slot${runs.length === 1 ? "" : "s"} / ${totalHAs} HA`,
      tone: runs.length ? "emerald" : "zinc",
    },
    {
      key: "clusters",
      label: "Clusters",
      value: clusters.length ? `${reachable}/${clusters.length} reachable` : "None yet",
      tone: clusters.length ? (reachable === clusters.length ? "emerald" : "amber") : "zinc",
    },
    gpuChip.value,
    {
      key: "aws",
      label: "AWS view",
      value: awsItems.length ? `${awsItems.length} resources` : "No resources shown",
      tone: awsItems.length ? "amber" : "zinc",
    },
    operationChip.value,
    { key: "refreshed", label: "Refreshed", value: freshness, tone: "zinc" },
  ].filter(Boolean);
});

const chipToneClass = tone => ({
  emerald: "panel-chip-tone-emerald",
  sky: "panel-chip-tone-sky",
  amber: "panel-chip-tone-amber",
  rose: "panel-chip-tone-rose",
  zinc: "",
})[tone] || "";

const handleStateEvent = event => {
  state.value = event.detail?.state || {};
  bootPending.value = Boolean(event.detail?.bootPending);
  refreshedAt.value = event.detail?.refreshedAt || new Date().toISOString();
};

onMounted(() => {
  window.addEventListener("rancher-control-panel:state", handleStateEvent);
});

onUnmounted(() => {
  window.removeEventListener("rancher-control-panel:state", handleStateEvent);
});
</script>
