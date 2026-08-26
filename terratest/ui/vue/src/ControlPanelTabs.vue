<template>
  <!-- Status markers are positioned within each tab so refreshes never change tab geometry. -->
  <div class="panel-tabs-track">
    <button
      v-for="tab in tabs"
      :key="tab.id"
      type="button"
      @click="setActivePanelTab(tab.id)"
      class="panel-tab rounded-lg text-sm font-semibold whitespace-nowrap"
      :class="tabButtonClass(tab.id)"
      :aria-current="activeTab === tab.id ? 'page' : undefined"
      :aria-label="tabAriaLabel(tab)"
      :aria-busy="tabStatus(tab.id).busy ? 'true' : undefined"
      :data-operation-status="tabStatus(tab.id).kind || undefined"
      :title="tabStatus(tab.id).label || undefined"
    >
      <span>{{ tab.label }}</span>
      <span
        v-if="tabStatus(tab.id).visible"
        :data-tab-status="tab.id"
        :data-status-kind="tabStatus(tab.id).kind"
        aria-hidden="true"
        class="tab-status"
        :class="`tab-status--${tabStatus(tab.id).kind}`"
      ></span>
    </button>
  </div>
  <span class="sr-only" aria-live="polite" aria-atomic="true">{{ busyStatusAnnouncement }}</span>
</template>

<script setup>
import { computed } from "vue";
import {
  state,
  activeTab,
  setActivePanelTab,
} from "./store.js";

const tabs = [
  { id: "setup", label: "Setup" },
  { id: "runs", label: "Runs" },
  { id: "clusters", label: "Clusters" },
  { id: "aws", label: "AWS Inventory" },
  { id: "images", label: "Image Lookup" },
  { id: "pr-builds", label: "PR Image Check" },
  { id: "destroy", label: "Destroy" },
  { id: "settings", label: "Settings" },
  { id: "k3d", label: "K3D Lab" },
  { id: "steve", label: "Steve Lab" },
];

const clusterItems = currentState => (
  currentState && currentState.clusters && Array.isArray(currentState.clusters.items)
    ? currentState.clusters.items
    : []
);

const activeK3DClusterCount = currentState => {
  const clusters = Array.isArray(currentState?.k3d?.clusters) ? currentState.k3d.clusters : [];
  return clusters.filter(cluster => ["creating", "running"].includes(cluster.status)).length;
};

const status = (kind = "", label = "") => ({
  kind,
  label,
  busy: kind === "busy",
  visible: Boolean(kind && label),
});

const countLabel = (count, singular, plural = `${singular}s`) => (
  count ? `${count} ${Number(count) === 1 ? singular : plural}` : ""
);

const contentStatus = (count, singular, plural = `${singular}s`) => (
  count ? status("content", countLabel(count, singular, plural)) : status()
);

const statuses = computed(() => {
  const runs = Array.isArray(state.value?.workspace?.runs) ? state.value.workspace.runs : [];
  const clusters = clusterItems(state.value);
  const awsItems = Array.isArray(state.value?.aws?.items) ? state.value.aws.items : [];
  const k3dCount = activeK3DClusterCount(state.value);
  const k3dRunning = Boolean(state.value?.k3d?.operation?.running);
  const steveRunning = Boolean(state.value?.steve?.operation?.running);
  const awsSetupRunning = Boolean(state.value?.setup?.running);
  const linodeSetupRunning = Boolean(state.value?.linodeSetup?.running);

  return {
    setup: awsSetupRunning
      ? status("busy", "AWS setup running; lifecycle actions are locked")
      : linodeSetupRunning
        ? status("busy", "Linode setup running; lifecycle actions are locked")
        : status(),
    runs: contentStatus(runs.length, "recorded run"),
    clusters: contentStatus(clusters.length, "cluster record"),
    aws: contentStatus(awsItems.length, "visible AWS resource"),
    destroy: contentStatus(runs.length, "run available to destroy", "runs available to destroy"),
    k3d: k3dRunning
      ? status("busy", "K3D operation running; K3D controls are locked")
      : contentStatus(k3dCount, "active K3D cluster"),
    steve: steveRunning
      ? status("busy", "Steve Lab operation running; Steve Lab controls are locked")
      : status(),
  };
});

const tabStatus = tab => statuses.value[tab] || status();

const tabAriaLabel = tab => {
  const label = tabStatus(tab.id).label;
  return label ? `${tab.label}, ${label}` : tab.label;
};

const busyStatusAnnouncement = computed(() => tabs
  .map(tab => tabStatus(tab.id))
  .filter(item => item.busy)
  .map(item => item.label)
  .join(". "));

const tabButtonClass = tab => activeTab.value === tab
  ? "bg-emerald-500 text-white shadow-sm shadow-emerald-500/20"
  : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-white/[0.06]";

</script>
