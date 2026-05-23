<template>
  <button
    v-for="tab in tabs"
    :key="tab.id"
    type="button"
    :data-tab="tab.id"
    class="panel-tab rounded-lg px-3.5 py-2 text-sm font-semibold"
    :class="tabButtonClass(tab.id)"
    :aria-current="activeTab === tab.id ? 'page' : undefined"
  >
    <span>{{ tab.label }}</span>
    <span
      v-if="tabBadge(tab.id)"
      :data-tab-count="tab.id"
      class="tab-count"
      :class="tabBadgeClass(tab.id)"
    >
      {{ tabBadge(tab.id) }}
    </span>
  </button>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";

const savedTab = localStorage.getItem("rancherControlPanelTab") || "setup";
const activeTab = ref(savedTab === "lifecycle" ? "runs" : savedTab);
const state = ref(window.rancherControlPanelState || {});

const tabs = [
  { id: "setup", label: "Setup" },
  { id: "runs", label: "Runs" },
  { id: "clusters", label: "Clusters" },
  { id: "aws", label: "AWS Inventory" },
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
  const active = clusters.filter(cluster => ["creating", "running"].includes(cluster.status));
  return active.length ? String(active.length) : "";
};

const badges = computed(() => {
  const runs = Array.isArray(state.value?.workspace?.runs) ? state.value.workspace.runs : [];
  const clusters = clusterItems(state.value);
  const awsItems = Array.isArray(state.value?.aws?.items) ? state.value.aws.items : [];

  return {
    setup: state.value?.setup?.running ? "AWS" : state.value?.linodeSetup?.running ? "Linode" : "",
    runs: runs.length ? String(runs.length) : "",
    clusters: clusters.length ? String(clusters.length) : "",
    aws: awsItems.length ? String(awsItems.length) : "",
    destroy: runs.length ? String(runs.length) : "",
    settings: "",
    k3d: state.value?.k3d?.operation?.running ? "Run" : activeK3DClusterCount(state.value),
    steve: state.value?.steve?.operation?.running ? "Run" : "",
  };
});

const tabBadge = tab => badges.value[tab] || "";

const tabButtonClass = tab => activeTab.value === tab
  ? "bg-emerald-500 text-white shadow-sm shadow-emerald-500/20"
  : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-white/[0.06]";

const tabBadgeClass = tab => activeTab.value === tab
  ? "bg-white/20 text-white"
  : "bg-zinc-100 text-zinc-600 dark:bg-white/[0.08] dark:text-zinc-300";

const handleStateEvent = event => {
  state.value = event.detail?.state || {};
};

const handleTabEvent = event => {
  const nextTab = event.detail?.tab;
  if (tabs.some(tab => tab.id === nextTab)) {
    activeTab.value = nextTab;
  }
};

onMounted(() => {
  window.addEventListener("rancher-control-panel:state", handleStateEvent);
  window.addEventListener("rancher-control-panel:tab", handleTabEvent);
});

onUnmounted(() => {
  window.removeEventListener("rancher-control-panel:state", handleStateEvent);
  window.removeEventListener("rancher-control-panel:tab", handleTabEvent);
});
</script>
