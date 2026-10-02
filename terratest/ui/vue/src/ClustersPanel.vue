<template>
  <div class="cluster-home grid min-w-0 gap-4">
    <header class="cluster-home-hero"><div><span class="cluster-home-eyebrow"><Icon name="layers"/>RANCHER RUNWAY / CLUSTER WORKSPACES</span><h2>Clusters<span>.</span></h2><p>Cluster access, health, and test history.</p></div><div class="cluster-home-totals"><span><strong>{{ items.length }}</strong> discovered</span><button type="button" @click="setActivePanelTab('history')"><strong>{{ retainedWorkspaces.length }}</strong> retained · Open History →</button></div></header>
    <div class="cluster-home-search"><label><Icon name="search"/><input v-model="clusterSearch" type="search" aria-label="Search cluster workspaces" placeholder="Find a cluster, nickname, hostname, or run…"/></label><button type="button" @click="refreshClusterWorkspaces"><Icon name="refresh"/>Refresh history</button></div>
    <div v-if="clusterWorkspaceError" class="cluster-home-error" role="alert">{{ clusterWorkspaceError }} <button type="button" @click="refreshClusterWorkspaces">Try again</button></div>
    <template v-if="!clusterSearch.trim()">
    <RefreshStatus v-if="!cleanupRunning" :refreshing="state.clusters?.refreshing" label="cluster status" />
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
      <div
        v-if="state.cleanup?.finishedAt && !state.cleanup?.error && !cleanupDismissed"
        :class="cleanupWarning ? 'text-amber-800 dark:text-amber-200' : 'text-emerald-800 dark:text-emerald-200'"
      >
        <template v-if="cleanupWarning">
          AWS management destroy finished for the selected run, but downstream Linode cleanup did not. The remaining Linode resources require manual cleanup.
        </template>
        <template v-else>
          Destroy finished for the selected run. Downstream cleanup and management Terraform destroy both succeeded.
        </template>
      </div>
      <div v-else>{{ initialDiscovery(state.clusters) ? 'Waiting for cluster discovery to finish.' : 'No clusters discovered yet.' }}</div>
    </div>

    <!-- Cluster groups available -->
    <div v-else class="grid min-w-0 gap-4">
      <!-- Run Slot selection -->
      <div class="min-w-0 rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Run slot</div>
        <div class="mt-2 flex flex-wrap gap-2">
          <button
            v-for="group in clusterGroups"
            :key="group.runKey"
            type="button"
            @click="selectRun(group.runKey)"
            class="min-w-0 max-w-full break-words rounded-md border px-3 py-1.5 text-sm font-semibold shadow-sm"
            :class="group.runKey === selectedRunKey ? activeTabClass : inactiveTabClass"
          >
            {{ group.label }}
            <span class="ml-2 opacity-80 font-medium">{{ deploymentKindLabel(groupDeploymentType(group)) }}</span>
            <span class="ml-2 text-xs opacity-90">{{ getClusterCount(group) }}</span>
          </button>
        </div>
      </div>

      <!-- HA / Tenant selector -->
      <div v-if="activeRunGroup && activeRunGroup.has.length" class="min-w-0 rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
          {{ clusterGroupLabel(groupDeploymentType(activeRunGroup)) }}
        </div>
        <div class="mt-2 flex flex-wrap gap-2">
          <button
            v-for="ha in activeRunGroup.has"
            :key="ha.haKey"
            type="button"
            @click="selectHA(ha.haKey)"
            class="min-w-0 max-w-full break-words rounded-md border px-3 py-1.5 text-sm font-semibold shadow-sm"
            :class="ha.haKey === selectedHAKey ? activeTabClass : inactiveTabClass"
          >
            {{ haTabLabel(ha) }}
            <span class="ml-2 text-xs opacity-90">{{ haCountLabel(ha) }}</span>
          </button>
        </div>
      </div>

      <!-- Cluster cards and pods lists -->
      <div v-if="activeHA" class="grid min-w-0 gap-4">
        <!-- Local/Management Cluster Section -->
        <div class="min-w-0">
          <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">
            {{ managementSectionLabel(groupDeploymentType(activeRunGroup)) }}
          </div>
          <div v-if="activeHA.local" class="min-w-0">
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
        <div v-if="activeHA.local?.deploymentType !== 'linode-docker-cattle'" class="min-w-0">
          <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">
            {{ activeHA.local?.deploymentType === 'hosted-tenant-k3s' ? 'Imported cluster records' : 'Downstream clusters' }}
          </div>
          <div v-if="activeHA.downstreams.length" class="grid min-w-0 gap-4">
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
    </template>
    <div v-else-if="!cleanupRunning" class="cluster-home-results"><p class="cluster-home-caption">{{ matchingLive.length }} matching live workspaces</p><ClusterCard v-for="cluster in matchingLive" :key="cluster.id" :cluster="cluster"/><div v-if="!matchingLive.length" class="cluster-home-empty"><Icon name="search"/><h3>No workspaces match.</h3><p>Try a nickname, cluster ID, Rancher URL, or run ID.</p><button type="button" @click="clusterSearch=''">Clear search</button></div></div>

  </div>
</template>

<script setup>
import RefreshStatus from './RefreshStatus.vue';
import { initialDiscovery } from './panel-presentation.mjs';
import { computed, nextTick, ref, watch } from "vue";
import Icon from "./HelmLabIcon.vue";
import { refreshTestPackageSummaries } from "./test-packages-store.mjs";
import { useClusterWorkspaces,refreshClusterWorkspaces,selectedClusterWorkspaceId,clusterDisplayName } from "./cluster-workspace-store.mjs";
import {
  state,
  activeTab,
  setActivePanelTab,
  activeClusterRunKey,
  activeClusterHAKey,
  openCleanupLogs,
} from "./store.js";
import {
  clusterItems,
  sameRunKey,
} from "../../static/control_panel_utils.js";
import ClusterCard from "./ClusterCard.vue";

const {clusterWorkspaces,clusterWorkspaceError}=useClusterWorkspaces();
watch(activeTab,tab=>{if(tab==='clusters')refreshTestPackageSummaries().catch(()=>{});},{immediate:true});
const clusterSearch=ref('');
const retainedWorkspaces=computed(()=>clusterWorkspaces.value.filter(cluster=>cluster.archived));
const matches=cluster=>{const words=clusterSearch.value.toLowerCase().trim().split(/\s+/).filter(Boolean);const text=[clusterDisplayName(cluster.id,cluster.name),cluster.nickname,cluster.id,cluster.runId,cluster.url,cluster.rancherUrl,cluster.version].filter(Boolean).join(' ').toLowerCase();return words.every(word=>text.includes(word));};
const matchingLive=computed(()=>items.value.filter(matches));


// Styling classes
const activeTabClass = "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200";
const inactiveTabClass = "border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]";

// Global properties
const items = computed(() => clusterItems(state.value));
const workspace = computed(() => state.value?.workspace || {});
const cleanupRunning = computed(() => Boolean(state.value?.cleanup?.running || state.value?.linodeCleanup?.running));
const cleanupWarning = computed(() => String(state.value?.cleanup?.warning || "").trim());

const cleanupDismissed = computed(() => {
  const cleanup = state.value?.cleanup || {};
  if (!cleanup || cleanup.running || (!cleanup.finishedAt && !cleanup.error)) return false;
  const key = [cleanup.runId || "unknown", cleanup.finishedAt || "", cleanup.error || "", cleanup.warning || ""].join("|");
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

watch([selectedClusterWorkspaceId,activeTab,()=>items.value.map(item=>item.id).join(','),()=>clusterWorkspaces.value.map(item=>item.id).join(',')],async([id])=>{
 if(!id||activeTab.value!=='clusters')return;const live=items.value.find(item=>item.id===id);clusterSearch.value='';
 if(live){selectRun(clusterRunKey(live));selectHA(clusterHAKey(live));}
 await nextTick();const target=Array.from(document.querySelectorAll('[data-cluster-workspace-id]')).find(element=>element.dataset.clusterWorkspaceId===id);
 if(target){target.scrollIntoView({block:'start',behavior:'auto'});target.focus({preventScroll:true});selectedClusterWorkspaceId.value='';}
},{flush:'post',immediate:true});

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

<style scoped>
.cluster-home{color:var(--runway-ink)}.cluster-home-hero{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:30px;border:1px solid var(--runway-border);border-radius:20px;background:radial-gradient(ellipse at right top,color-mix(in srgb,var(--runway-accent) 9%,transparent),transparent 58%),var(--runway-card)}.cluster-home-eyebrow{display:flex;gap:9px;align-items:center;letter-spacing:.16em;font-size:10px;font-weight:700;color:var(--runway-muted)}.cluster-home svg{width:18px;height:18px;flex-shrink:0}.cluster-home-hero h2{font-size:42px;letter-spacing:-.05em;line-height:1.1;margin:14px 0}.cluster-home-hero h2 span{color:var(--runway-accent)}.cluster-home-hero p,.cluster-home-retained header p{color:var(--runway-muted);font-size:13px;line-height:1.7;margin:0}.cluster-home-totals{display:flex;gap:25px;flex-shrink:0}.cluster-home-totals span{display:grid;gap:4px;font-size:11px;color:var(--runway-muted)}.cluster-home-totals strong{color:var(--runway-ink);font-size:27px;letter-spacing:-.04em}.cluster-home-search{display:flex;gap:12px;align-items:center}.cluster-home-search label{display:flex;gap:10px;align-items:center;flex:1;background:var(--runway-soft);border:1px solid var(--runway-border);border-radius:10px;padding:0 14px;color:var(--runway-muted)}.cluster-home-search input{width:100%;min-width:0;padding:13px 0;background:transparent;border:0;color:var(--runway-ink);font-size:13px}.cluster-home-search button,.cluster-home-empty button{display:inline-flex;align-items:center;gap:8px;background:var(--runway-raised);color:var(--runway-ink);border:1px solid var(--runway-border);border-radius:10px;padding:12px 15px;font-size:12px;font-weight:600;cursor:pointer}.cluster-home-error{border:1px solid var(--runway-border);border-radius:10px;padding:14px;background:var(--runway-card);color:var(--runway-danger,#df7480);font-size:12px}.cluster-home-error button{margin-left:10px;text-decoration:underline}.cluster-home-caption{font-size:12px;color:var(--runway-muted);margin:0}.cluster-home-results,.cluster-home-retained{display:grid;gap:16px;min-width:0}.cluster-home-retained{padding-top:15px}.cluster-home-retained header h3{font-size:21px;letter-spacing:-.025em;margin:7px 0}.cluster-home-empty{padding:40px;display:grid;justify-items:center;gap:12px;color:var(--runway-muted);border:1px solid var(--runway-border);border-radius:14px}.cluster-home-empty h3{color:var(--runway-ink);font-size:20px;margin:0}.cluster-home-empty p{margin:0;font-size:12px}@media(max-width:800px){.cluster-home-hero{align-items:flex-start;flex-direction:column;padding:24px}.cluster-home-search{align-items:stretch;flex-direction:column}.cluster-home-search button{align-self:flex-start}.cluster-home-hero h2{font-size:36px}}
</style>
