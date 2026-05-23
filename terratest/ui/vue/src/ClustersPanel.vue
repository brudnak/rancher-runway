<template>
  <div class="grid gap-4">
    <!-- Active operation is running teardown -->
    <div
      v-if="cleanupRunning"
      class="rounded-2xl border border-sky-200 bg-sky-50 p-6 text-center dark:border-sky-500/20 dark:bg-sky-500/10"
    >
      <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">
        <span class="spinner"></span>
      </div>
      <h3 class="mt-4 text-lg font-semibold tracking-tight text-sky-950 dark:text-sky-100">Infrastructure is being torn down</h3>
      <p class="mx-auto mt-2 max-w-2xl text-sm leading-6 text-sky-800/80 dark:text-sky-200/80">
        Destroy is removing Terraform resources for the selected run. Cluster details are paused so the panel does not show stale unavailable infrastructure.
      </p>
      <button
        type="button"
        @click="openCleanupLogs(state.linodeCleanup?.running || state.linodeCleanup?.finishedAt || state.linodeCleanup?.error)"
        class="mt-4 rounded-lg border border-sky-200 bg-white px-4 py-2 text-sm font-semibold text-sky-800 shadow-sm hover:bg-zinc-50 dark:border-sky-500/30 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
      >
        Open destroy logs
      </button>
    </div>

    <!-- No clusters discovered yet -->
    <div
      v-else-if="!items.length"
      class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400"
    >
      <div v-if="state.cleanup?.finishedAt && !state.cleanup?.error && !cleanupDismissed" class="text-emerald-800 dark:text-emerald-200">
        Destroy finished for the selected run. Cluster records were cleared after Terraform destroy succeeded.
      </div>
      <div v-else>No clusters discovered yet.</div>
    </div>

    <!-- Cluster groups available -->
    <div v-else class="grid gap-4">
      <!-- Run Slot selection -->
      <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Run slot</div>
        <div class="mt-2 flex flex-wrap gap-2">
          <button
            v-for="group in clusterGroups"
            :key="group.runKey"
            type="button"
            @click="selectRun(group.runKey)"
            class="rounded-md border px-3 py-1.5 text-sm font-semibold shadow-sm"
            :class="group.runKey === selectedRunKey ? activeTabClass : inactiveTabClass"
          >
            {{ group.label }}
            <span class="ml-2 opacity-80 font-medium">{{ deploymentKindLabel(groupDeploymentType(group)) }}</span>
            <span class="ml-2 text-xs opacity-90">{{ getClusterCount(group) }}</span>
          </button>
        </div>
      </div>

      <!-- HA / Tenant selector -->
      <div v-if="activeRunGroup && activeRunGroup.has.length" class="rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
          {{ clusterGroupLabel(groupDeploymentType(activeRunGroup)) }}
        </div>
        <div class="mt-2 flex flex-wrap gap-2">
          <button
            v-for="ha in activeRunGroup.has"
            :key="ha.haKey"
            type="button"
            @click="selectHA(ha.haKey)"
            class="rounded-md border px-3 py-1.5 text-sm font-semibold shadow-sm"
            :class="ha.haKey === selectedHAKey ? activeTabClass : inactiveTabClass"
          >
            {{ haTabLabel(ha) }}
            <span class="ml-2 text-xs opacity-90">{{ haCountLabel(ha) }}</span>
          </button>
        </div>
      </div>

      <!-- Cluster cards and pods lists -->
      <div v-if="activeHA" class="grid gap-4">
        <!-- Local/Management Cluster Section -->
        <div>
          <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">
            {{ managementSectionLabel(groupDeploymentType(activeRunGroup)) }}
          </div>
          <div v-if="activeHA.local">
            <ClusterCard :cluster="activeHA.local" />
          </div>
          <div
            v-else
            class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400"
          >
            No local cluster record found for this HA yet.
          </div>
        </div>

        <!-- Downstream Clusters Section -->
        <div v-if="activeHA.local?.deploymentType !== 'linode-docker-cattle'">
          <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">
            {{ activeHA.local?.deploymentType === 'hosted-tenant-k3s' ? 'Imported cluster records' : 'Downstream clusters' }}
          </div>
          <div v-if="activeHA.downstreams.length" class="grid gap-4">
            <ClusterCard
              v-for="downstream in activeHA.downstreams"
              :key="downstream.id"
              :cluster="downstream"
            />
          </div>
          <div
            v-else
            class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400"
          >
            {{ activeHA.local?.deploymentType === 'hosted-tenant-k3s' ? 'No imported cluster records discovered for this hosted-tenant instance yet.' : 'No downstream clusters discovered for this HA yet.' }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import {
  state,
  activeClusterRunKey,
  activeClusterHAKey,
  openCleanupLogs,
} from "./store.js";
import {
  clusterItems,
  sameRunKey,
} from "../../static/control_panel_utils.js";
import ClusterCard from "./ClusterCard.vue";

// Styling classes
const activeTabClass = "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200";
const inactiveTabClass = "border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]";

// Global properties
const items = computed(() => clusterItems(state.value));
const workspace = computed(() => state.value?.workspace || {});
const cleanupRunning = computed(() => Boolean(state.value?.cleanup?.running || state.value?.linodeCleanup?.running));

const cleanupDismissed = computed(() => {
  const cleanup = state.value?.cleanup || {};
  if (!cleanup || cleanup.running || (!cleanup.finishedAt && !cleanup.error)) return false;
  const key = [cleanup.runId || "unknown", cleanup.finishedAt || "", cleanup.error || ""].join("|");
  return Boolean(key && dismissedCleanupResultKey.value === key);
});
const dismissedCleanupResultKey = ref("");

// UI Selection state
const selectedRunKey = ref("");
const selectedHAKey = ref("");

// Tab Group logic
const clusterRunKey = cluster => String(cluster?.runId || "default");
const clusterHAKey = cluster => String(cluster?.haIndex || 0);

const groupDeploymentType = group => {
  if (group?.run?.deploymentType) return group.run.deploymentType;
  const local = group?.has?.find(ha => ha.local)?.local;
  return local?.deploymentType || "ha-rke2";
};

const runLabelForClusterGroup = (runKey, ws) => {
  const runs = Array.isArray(ws?.runs) ? ws.runs : [];
  const run = runs.find(item => String(item.runId || "default") === runKey);
  if (run?.runId) return `Run ${run.runId}`;
  if (run?.slotId) return run.slotId.replace(/^slot-/, "Slot ");
  if (runKey !== "default") return `Run ${runKey}`;
  return "Default slot";
};

const buildClusterGroups = (itemsList, ws) => {
  const runOrder = [];
  const groups = new Map();
  const runs = Array.isArray(ws?.runs) ? ws.runs : [];

  runs.forEach(run => {
    const runKey = String(run.runId || "default");
    if (!groups.has(runKey)) {
      groups.set(runKey, {
        runKey,
        label: runLabelForClusterGroup(runKey, ws),
        run,
        haOrder: [],
        has: new Map(),
      });
      runOrder.push(runKey);
    }
  });

  itemsList.forEach(cluster => {
    const runKey = clusterRunKey(cluster);
    if (!groups.has(runKey)) {
      groups.set(runKey, {
        runKey,
        label: runLabelForClusterGroup(runKey, ws),
        run: null,
        haOrder: [],
        has: new Map(),
      });
      runOrder.push(runKey);
    }

    const group = groups.get(runKey);
    const haKey = clusterHAKey(cluster);
    if (!group.has.has(haKey)) {
      group.has.set(haKey, {
        haKey,
        haIndex: cluster.haIndex || 0,
        local: null,
        downstreams: [],
      });
      group.haOrder.push(haKey);
    }

    const ha = group.has.get(haKey);
    if (cluster.type === "downstream") {
      ha.downstreams.push(cluster);
    } else {
      ha.local = cluster;
    }
  });

  return runOrder
    .map(runKey => groups.get(runKey))
    .filter(Boolean)
    .map(group => ({
      ...group,
      has: group.haOrder
        .map(haKey => group.has.get(haKey))
        .filter(Boolean)
        .sort((left, right) => (left.haIndex || 0) - (right.haIndex || 0)),
    }));
};

const clusterGroups = computed(() => buildClusterGroups(items.value, workspace.value));

const activeRunGroup = computed(() => {
  if (!clusterGroups.value.length) return null;
  const found = clusterGroups.value.find(g => g.runKey === selectedRunKey.value);
  return found || clusterGroups.value[0];
});

const activeHA = computed(() => {
  if (!activeRunGroup.value) return null;
  const found = activeRunGroup.value.has.find(ha => ha.haKey === selectedHAKey.value);
  return found || activeRunGroup.value.has[0];
});

// Selection handlers
const selectRun = runKey => {
  selectedRunKey.value = runKey;
  activeClusterRunKey.value = runKey;
  activeClusterHAKey.value = "";
  const group = clusterGroups.value.find(g => g.runKey === runKey);
  if (group && group.has.length) {
    selectedHAKey.value = group.has[0].haKey;
    activeClusterHAKey.value = group.has[0].haKey;
  }
};

const selectHA = haKey => {
  selectedHAKey.value = haKey;
  activeClusterHAKey.value = haKey;
};

// Synchronize selectors with store states
watch(clusterGroups, (newVal) => {
  if (newVal.length) {
    if (!selectedRunKey.value || !newVal.some(g => g.runKey === selectedRunKey.value)) {
      selectedRunKey.value = newVal[0].runKey;
    }
  }
}, { immediate: true });

watch(activeRunGroup, (newVal) => {
  if (newVal && newVal.has.length) {
    if (!selectedHAKey.value || !newVal.has.some(ha => ha.haKey === selectedHAKey.value)) {
      selectedHAKey.value = newVal.has[0].haKey;
    }
  }
}, { immediate: true });

watch(activeClusterRunKey, (newVal) => {
  if (newVal && newVal !== selectedRunKey.value) {
    selectedRunKey.value = newVal;
  }
});

watch(activeClusterHAKey, (newVal) => {
  if (newVal && newVal !== selectedHAKey.value) {
    selectedHAKey.value = newVal;
  }
});

// Labels and utilities
const deploymentKindLabel = deploymentType => {
  if (deploymentType === "hosted-tenant-k3s") return "Hosted tenant K3s";
  if (deploymentType === "linode-docker-cattle") return "Linode Docker";
  return "RKE2 HA";
};

const getClusterCount = group =>
  group.has.reduce((count, ha) => count + (ha.local ? 1 : 0) + ha.downstreams.length, 0);

const clusterGroupLabel = deploymentType =>
  deploymentType === "hosted-tenant-k3s" ? "Hosted tenant instance" : deploymentType === "linode-docker-cattle" ? "Docker Rancher" : "HA cluster";

const managementSectionLabel = deploymentType =>
  deploymentType === "hosted-tenant-k3s" ? "Rancher instance" : deploymentType === "linode-docker-cattle" ? "Docker Rancher" : "Management cluster";

const hostedTenantInstanceLabel = ha => {
  const index = Number(ha?.haIndex || 0);
  const role = ha?.local?.role || ha?.local?.local?.role;
  return role === "host" || index === 1 ? "Host" : index > 1 ? `Tenant ${index - 1}` : "Tenant";
};

const haTabLabel = ha => {
  const version = ha.local?.version ? ` • ${ha.local.version}` : "";
  const tabLabel = ha.local?.deploymentType === "hosted-tenant-k3s"
    ? hostedTenantInstanceLabel(ha)
    : ha.local?.deploymentType === "linode-docker-cattle"
      ? `Docker Rancher ${ha.haIndex || ha.haKey}`
      : `HA ${ha.haIndex || ha.haKey}`;
  return `${tabLabel}${version}`;
};

const haCountLabel = ha => {
  const downstreamCount = ha.downstreams.length;
  return ha.local?.deploymentType === "hosted-tenant-k3s"
    ? (downstreamCount ? `${downstreamCount} import` : "K3s")
    : ha.local?.deploymentType === "linode-docker-cattle"
      ? "Linode"
      : `${downstreamCount} downstream`;
};
</script>
