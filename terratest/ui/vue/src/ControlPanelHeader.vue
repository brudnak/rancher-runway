<template>
  <div class="runway-status">
    <div class="runway-status-current" aria-live="polite">
      <span v-if="operationChip.running || operationChip.tone === 'rose'" class="panel-chip" :class="chipToneClass(operationChip.tone)"><span v-if="operationChip.running" class="spinner !h-3 !w-3"></span>{{ operationChip.label }} · {{ operationChip.value }}</span>
      <span v-if="gpuChip" class="panel-chip panel-chip-tone-rose">{{ gpuChip.label }} · {{ gpuChip.value }}</span>
      <details class="runway-status-details">
        <summary>Workspace status</summary>
        <div class="runway-status-popover">
          <div class="runway-status-chips"><span v-for="chip in chips" :key="chip.key" :data-chip="chip.key" class="panel-chip" :class="chipToneClass(chip.tone)"><span v-if="chip.running" class="spinner !h-3 !w-3"></span>{{ chip.label }} <span class="panel-chip-value">{{ chip.value }}</span></span></div>
          <p v-if="sessionMeta" :title="panel?.configPath || ''">{{ sessionMeta }}</p>
          <p v-if="panel?.starterConfigCreated">Starter config: {{ panel.configPath }}. Complete the required values in Setup.</p>
        </div>
      </details>
    </div>
  </div>
</template>

<script setup>
import { computed } from "vue";
import { state, bootPending, refreshedAt, refreshError } from "./store.js";
import { discoveryValue } from './panel-presentation.mjs';

const panel = computed(() => state.value?.panel || {});
const sessionMeta = computed(() => {
  if (!panel.value?.sessionId) {
    return "";
  }

  const started = panel.value.startedAt ? new Date(panel.value.startedAt).toLocaleTimeString() : "";
  const pieces = [`Session ${panel.value.sessionId}`];
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
  ["awsCleanup", "AWS cleanup", state.value?.awsCleanup],
  ["setup", "Setup", state.value?.setup],
  ["readiness", "Readiness", state.value?.readiness],
  ["downstream", "Downstream", state.value?.downstream],
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

  if (bootPending.value && refreshError.value) {
    return { key: "operation", label: "Safety check", value: "Unavailable", tone: "rose", running: false };
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
      value: discoveryValue('clusters', state.value?.clusters),
      tone: clusters.length ? (reachable === clusters.length ? "emerald" : "amber") : "zinc",
      running: state.value?.clusters?.refreshing,
    },
    gpuChip.value,
    {
      key: "aws",
      label: "AWS view",
      value: discoveryValue('aws', state.value?.aws),
      tone: awsItems.length ? "amber" : "zinc",
      running: state.value?.aws?.refreshing,
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

</script>

<style scoped>
.runway-status-current{display:flex;align-items:center;flex-wrap:wrap;gap:10px;min-height:36px}.runway-status-details{position:relative;z-index:30}.runway-status-details summary{cursor:pointer;color:var(--runway-muted);font-size:12px;padding:8px 0}.runway-status-popover{position:absolute;left:0;top:40px;width:min(560px,calc(100vw - 64px));background:var(--runway-card);border:1px solid var(--runway-border);border-radius:12px;padding:18px;box-shadow:var(--runway-shadow)}.runway-status-chips{display:flex;flex-wrap:wrap;gap:8px}.runway-status-popover p{font-size:11px;color:var(--runway-muted);margin-top:14px;overflow-wrap:anywhere}.runway-status-details summary:focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}
</style>
