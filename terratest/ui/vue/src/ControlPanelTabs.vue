<template>
  <!-- Keep a real layout box here so WebKit repaints the row when refresh badges change its width. -->
  <div class="panel-tabs-track">
    <button
      v-for="tab in tabs"
      :key="tab.id"
      type="button"
      @click="setActivePanelTab(tab.id)"
      class="panel-tab flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-semibold whitespace-nowrap"
      :class="tabButtonClass(tab.id)"
      :aria-current="activeTab === tab.id ? 'page' : undefined"
      :aria-label="tabAriaLabel(tab)"
      :title="tabBadge(tab.id).label || undefined"
    >
      <span>{{ tab.label }}</span>
      <span
        v-if="tab.badgeSlot"
        :data-tab-count="tab.id"
        :data-empty="tabBadge(tab.id).visible ? undefined : 'true'"
        aria-hidden="true"
        class="tab-count"
        :class="[
          tabBadgeClass(tab),
          `tab-count--${tab.badgeSlot}`,
          { 'tab-count-loading': tabBadge(tab.id).loading },
        ]"
      >
        {{ tabBadge(tab.id).value }}
      </span>
    </button>
  </div>
</template>

<script setup>
import { computed } from "vue";
import {
  state,
  activeTab,
  setActivePanelTab,
} from "./store.js";

const tabs = [
  // Every changing badge keeps a fixed slot so the first state refresh cannot
  // move the active tab in WebKit. Operational tabs use a tiny one-character
  // slot instead of reserving enough room for a full textual pill.
  { id: "setup", label: "Setup", badgeSlot: "micro" },
  { id: "runs", label: "Runs", badgeSlot: "count" },
  { id: "clusters", label: "Clusters", badgeSlot: "count" },
  { id: "aws", label: "AWS Inventory", badgeSlot: "count" },
  { id: "images", label: "Image Lookup" },
  { id: "pr-builds", label: "PR Image Check" },
  { id: "destroy", label: "Destroy", badgeSlot: "count" },
  { id: "settings", label: "Settings" },
  { id: "k3d", label: "K3D Lab", badgeSlot: "micro" },
  { id: "steve", label: "Steve Lab", badgeSlot: "micro" },
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

const badge = (value = "", label = "", loading = false) => ({
  value,
  label,
  loading,
  visible: Boolean(value || loading),
});

const countLabel = (count, singular, plural = `${singular}s`) => (
  count ? `${count} ${Number(count) === 1 ? singular : plural}` : ""
);

const cappedCount = (count, maximum) => (
  count ? (Number(count) > maximum ? `${maximum}+` : String(count)) : ""
);

const badges = computed(() => {
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
      ? badge("A", "AWS setup running")
      : linodeSetupRunning
        ? badge("L", "Linode setup running")
        : badge(),
    runs: badge(
      cappedCount(runs.length, 99),
      countLabel(runs.length, "recorded run"),
    ),
    clusters: badge(
      cappedCount(clusters.length, 99),
      countLabel(clusters.length, "cluster record"),
    ),
    aws: badge(
      cappedCount(awsItems.length, 99),
      countLabel(awsItems.length, "visible AWS resource"),
    ),
    destroy: badge(
      cappedCount(runs.length, 99),
      countLabel(runs.length, "run available to destroy", "runs available to destroy"),
    ),
    k3d: badge(
      k3dRunning ? "" : cappedCount(k3dCount, 9),
      k3dRunning ? "K3D operation running" : countLabel(k3dCount, "active K3D cluster"),
      k3dRunning,
    ),
    steve: badge("", steveRunning ? "Steve Lab operation running" : "", steveRunning),
  };
});

const tabBadge = tab => badges.value[tab] || badge();

const tabAriaLabel = tab => {
  const label = tabBadge(tab.id).label;
  return label ? `${tab.label}, ${label}` : tab.label;
};

const tabButtonClass = tab => activeTab.value === tab
  ? "bg-emerald-500 text-white shadow-sm shadow-emerald-500/20"
  : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-white/[0.06]";

const tabBadgeClass = tab => {
  if (tab.badgeSlot === "micro" && tabBadge(tab.id).visible && !tabBadge(tab.id).loading) {
    return activeTab.value === tab.id
      ? "bg-white/25 text-white"
      : "bg-emerald-100 text-emerald-700 dark:bg-emerald-400/15 dark:text-emerald-200";
  }
  return activeTab.value === tab.id
    ? "bg-white/20 text-white"
    : "bg-zinc-100 text-zinc-600 dark:bg-white/[0.08] dark:text-zinc-300";
};
</script>
