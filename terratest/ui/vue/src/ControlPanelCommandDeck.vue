<template>
  <article
    v-for="tile in tiles"
    :key="tile.key"
    class="command-tile p-4"
    :data-tone="tile.tone"
  >
    <div class="flex h-full min-w-0 flex-col gap-3">
      <div class="flex min-w-0 items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="text-[11px] font-extrabold uppercase tracking-[0.18em] text-zinc-500 dark:text-zinc-400">
            {{ tile.eyebrow }}
          </div>
          <div
            class="mt-2 truncate text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50"
            :title="tile.title"
          >
            {{ tile.title }}
          </div>
        </div>
        <div
          v-if="tile.meta"
          class="shrink-0 rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-bold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300"
        >
          {{ tile.meta }}
        </div>
      </div>
      <p class="min-h-[2.5rem] text-sm leading-5 text-zinc-600 dark:text-zinc-400">
        {{ tile.detail }}
      </p>
      <div v-if="tile.action && tile.actionLabel" class="mt-auto">
        <button
          type="button"
          :data-command-action="tile.action"
          class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs font-bold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
        >
          {{ tile.actionLabel }}
        </button>
      </div>
    </div>
  </article>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";

const state = ref(window.rancherControlPanelState || {});
const bootPending = ref(true);

const clusterItems = currentState => (
  currentState && currentState.clusters && Array.isArray(currentState.clusters.items)
    ? currentState.clusters.items
    : []
);

const sameRunKey = (left, right) => String(left || "").trim() === String(right || "").trim();

const runVersionsLabel = run => Array.isArray(run?.rancherVersions) && run.rancherVersions.length
  ? run.rancherVersions.join(", ")
  : "not recorded";

const runClusterStats = run => {
  const runId = run?.runId || "";
  const items = clusterItems(state.value).filter(cluster => sameRunKey(cluster.runId, runId));
  return {
    total: items.length,
    reachable: items.filter(cluster => cluster.reachable).length,
  };
};

const operationSummary = computed(() => {
  const active = [
    ["setup", "Setup", state.value?.setup],
    ["readiness", "Readiness", state.value?.readiness],
    ["cleanup", "Destroy", state.value?.cleanup],
    ["linodeSetup", "Linode setup", state.value?.linodeSetup],
    ["linodeCleanup", "Linode destroy", state.value?.linodeCleanup],
  ].find(([, , operation]) => operation?.running);

  if (active) {
    const [, label, operation] = active;
    return {
      label,
      value: operation?.runId ? `Run ${operation.runId}` : "Running",
      tone: "sky",
      running: true,
    };
  }

  if (bootPending.value) {
    return { label: "Safety check", value: "Loading state", tone: "sky", running: true };
  }

  return { label: "Operation", value: "Idle", tone: "zinc", running: false };
});

const lifecycleRunning = currentState => Boolean(
  currentState?.setup?.running ||
  currentState?.readiness?.running ||
  currentState?.cleanup?.running ||
  currentState?.linodeSetup?.running ||
  currentState?.linodeCleanup?.running
);

const deploymentTargetLabel = run => {
  if (run?.deploymentType === "hosted-tenant-k3s") {
    return "hosted-tenant Rancher instance";
  }
  if (run?.deploymentType === "linode-docker-cattle") {
    return "Docker Rancher instance";
  }
  return "HA target";
};

const tiles = computed(() => {
  const runs = Array.isArray(state.value?.workspace?.runs) ? state.value.workspace.runs : [];
  const currentRun = state.value?.workspace?.currentRun || runs[0] || null;
  const awsItems = Array.isArray(state.value?.aws?.items) ? state.value.aws.items : [];
  const operation = operationSummary.value;
  const lifecycleBusy = lifecycleRunning(state.value);
  const readyForSetup = Boolean(state.value?.workspace?.canStartIsolatedRun && !lifecycleBusy && !bootPending.value);
  const currentStats = currentRun ? runClusterStats(currentRun) : { total: 0, reachable: 0 };

  const safetyTile = bootPending.value
    ? {
        key: "safety",
        tone: "sky",
        eyebrow: "Safety gate",
        title: "Inspecting local state",
        detail: "Actions stay disabled while the panel checks config, run slots, Terraform state, and active lifecycle processes.",
        meta: "Locked",
        action: "runs",
        actionLabel: "View runs",
      }
    : lifecycleBusy
      ? {
          key: "safety",
          tone: "sky",
          eyebrow: "Safety gate",
          title: `${operation.label} is active`,
          detail: "Setup, readiness, and destroy are serialized so the run state and AWS target stay unambiguous.",
          meta: "Busy",
          action: "runs",
          actionLabel: "Inspect run",
        }
      : {
          key: "safety",
          tone: readyForSetup ? "emerald" : "zinc",
          eyebrow: "Safety gate",
          title: readyForSetup ? "Ready for a new setup" : "Operator actions ready",
          detail: readyForSetup
            ? "Setup is available from the Setup tab after plan resolution and approval."
            : "No lifecycle operation is running. Existing slots remain individually inspectable and destroyable.",
          meta: "Ready",
          action: readyForSetup ? "setup" : "runs",
          actionLabel: readyForSetup ? "Open setup" : "View runs",
        };

  const runTile = currentRun
    ? {
        key: "run",
        tone: currentStats.total ? "emerald" : "amber",
        eyebrow: "Current slot",
        title: `Run ${currentRun.runId || "unknown"}`,
        detail: `${currentRun.totalHAs || 1} ${deploymentTargetLabel(currentRun)}${Number(currentRun.totalHAs || 1) === 1 ? "" : "s"} for ${runVersionsLabel(currentRun)}. ${currentStats.total ? `${currentStats.reachable}/${currentStats.total} cluster records reachable.` : "Cluster records are not visible yet."}`,
        meta: currentRun.status || "recorded",
        action: currentStats.total ? "clusters" : "runs",
        actionLabel: currentStats.total ? "Open clusters" : "View slot",
      }
    : {
        key: "run",
        tone: "zinc",
        eyebrow: "Current slot",
        title: "No run slot yet",
        detail: "Resolve a plan in Setup, approve the Helm/AWS gate, and the slot will appear before Terraform creates resources.",
        meta: "Empty",
        action: "setup",
        actionLabel: "Start setup flow",
      };

  const exposureTile = awsItems.length
    ? {
        key: "exposure",
        tone: "amber",
        eyebrow: "AWS exposure",
        title: `${awsItems.length} resource${awsItems.length === 1 ? "" : "s"} visible`,
        detail: "Inventory is read-only. Destructive actions remain per-slot and require typed confirmation before Terraform destroy starts.",
        meta: "Live",
        action: "aws",
        actionLabel: "Open inventory",
      }
    : {
        key: "exposure",
        tone: runs.length ? "emerald" : "zinc",
        eyebrow: "AWS exposure",
        title: "No resources shown",
        detail: runs.length
          ? "Recorded slots are available; AWS inventory currently has no matching visible resources."
          : "No AWS resources are expected before an approved setup run.",
        meta: "Quiet",
        action: runs.length ? "destroy" : "setup",
        actionLabel: runs.length ? "Open destroy" : "Open setup",
      };

  return [safetyTile, runTile, exposureTile];
});

const handleStateEvent = event => {
  state.value = event.detail?.state || {};
  bootPending.value = Boolean(event.detail?.bootPending);
};

onMounted(() => {
  window.addEventListener("rancher-control-panel:state", handleStateEvent);
});

onUnmounted(() => {
  window.removeEventListener("rancher-control-panel:state", handleStateEvent);
});
</script>
