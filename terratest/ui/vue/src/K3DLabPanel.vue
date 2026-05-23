<template>
  <div class="grid gap-5">
    <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0">
        <div class="inline-flex items-center rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-semibold text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200">
          Standalone local k3d
        </div>
        <h2 class="mt-3 text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">K3D Lab</h2>
        <p class="mt-2 max-w-4xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
          Start one or more local k3d clusters, keep their Kubernetes API endpoints available, and use the kubeconfig paths for local testing.
        </p>
      </div>
      <div class="grid shrink-0 gap-2 sm:grid-cols-3 lg:min-w-[28rem]">
        <div class="run-summary-stat" :data-tone="preflight.ready ? 'ready' : 'locked'">
          <div class="run-summary-label">Local tools</div>
          <div class="run-summary-value">{{ preflight.ready ? "Ready" : "Blocked" }}</div>
          <div class="run-summary-help">{{ preflight.summary || "Checking Docker and k3d" }}</div>
          <div class="mt-2 flex flex-wrap gap-2">
            <button type="button" class="text-xs font-semibold text-sky-600 hover:text-sky-500 dark:text-sky-300" @click="refreshState">Refresh tools</button>
            <button v-if="missingK3D" type="button" class="text-xs font-semibold text-emerald-600 hover:text-emerald-500 dark:text-emerald-300" :disabled="operation.running || installing" @click="installK3D">
              {{ installing ? "Installing..." : "Install k3d" }}
            </button>
          </div>
        </div>
        <div class="run-summary-stat" :data-tone="operation.running ? 'ready' : ''">
          <div class="run-summary-label">Action</div>
          <div class="run-summary-value">{{ operation.running ? "Running" : "Idle" }}</div>
          <div class="run-summary-help">{{ actionSummary }}</div>
        </div>
        <div class="run-summary-stat">
          <div class="run-summary-label">Running</div>
          <div class="run-summary-value">{{ runningClusters.length }}</div>
          <div class="run-summary-help">{{ clusterSummary }}</div>
        </div>
      </div>
    </div>

    <div class="grid gap-5 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
      <section class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03]">
        <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">{{ runningClusters.length ? "Start another k3d" : "Start k3d" }}</h3>
        <p class="mt-1 text-sm leading-6 text-zinc-600 dark:text-zinc-400">Each start creates a separate local cluster with its own Kubernetes API port.</p>

        <div class="mt-4 grid gap-4">
          <label class="grid gap-2 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
            <span>K3s image tag</span>
            <select v-model="k3sVersion" class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100">
              <option v-for="version in k3sOptions" :key="version" :value="version">{{ version }}</option>
            </select>
          </label>

          <label class="grid gap-2 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
            <span>API port</span>
            <input v-model.number="apiPort" type="number" min="1024" max="65535" placeholder="Auto" class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none placeholder:text-zinc-400 focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100 dark:placeholder:text-zinc-500" />
          </label>

          <div class="rounded-lg border border-sky-200 bg-sky-50 p-3 text-sm leading-6 text-sky-900 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
            Multiple k3d clusters can run side by side. Leave API port on Auto unless you need a fixed endpoint.
          </div>

          <div v-if="preflightItems.length" class="grid gap-2 rounded-lg border border-zinc-200 bg-white p-3 text-sm dark:border-white/10 dark:bg-zinc-950/30">
            <div v-for="item in preflightItems" :key="item.name" class="grid grid-cols-[5.75rem_4.25rem_minmax(0,1fr)] items-start gap-3">
              <div class="font-semibold text-zinc-800 dark:text-zinc-100">{{ item.name }}</div>
              <span class="inline-flex justify-center rounded-full px-2 py-0.5 text-xs font-semibold" :class="toolStatusClass(item.status)">{{ item.status }}</span>
              <div class="min-w-0 text-zinc-600 dark:text-zinc-400">{{ item.detail }}</div>
            </div>
          </div>

          <div class="flex flex-wrap gap-2">
            <button type="button" class="run-action-button run-action-button--primary" :disabled="startDisabled" @click="startCluster">{{ runningClusters.length ? "Start another k3d" : "Start k3d" }}</button>
            <button type="button" class="run-action-button run-action-button--danger" :disabled="!operation.running || stopping" @click="stopAction">
              {{ stopping ? "Stopping..." : "Stop action" }}
            </button>
          </div>
          <p v-if="notice" class="text-sm font-semibold" :class="noticeTone">{{ notice }}</p>
        </div>
      </section>

      <section class="rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Live output</h3>
            <p class="mt-1 text-sm leading-6 text-zinc-600 dark:text-zinc-400">{{ operation.command || "No K3D Lab action has started yet." }}</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="run-action-button run-action-button--utility" @click="refreshState">Refresh</button>
            <button type="button" class="run-action-button run-action-button--utility" :disabled="!hasOutput" @click="outputCollapsed = !outputCollapsed">
              {{ outputCollapsed ? "Expand" : "Collapse" }}
            </button>
            <button type="button" class="run-action-button run-action-button--utility" :disabled="clearingOutput || !hasOutput" @click="clearOutput">
              {{ clearingOutput ? "Clearing..." : "Clear" }}
            </button>
          </div>
        </div>
        <div v-if="outputCollapsed" class="mt-4 rounded-lg border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-zinc-950/30 dark:text-zinc-400">
          Output collapsed. {{ outputLineCount }} line{{ outputLineCount === 1 ? "" : "s" }} available.
        </div>
        <pre v-else class="mt-4 max-h-[21rem] max-w-full whitespace-pre-wrap break-words overflow-auto rounded-lg border border-zinc-200 bg-zinc-50 p-4 text-xs leading-5 text-zinc-800 dark:border-transparent dark:bg-zinc-950 dark:text-zinc-100">{{ outputText }}</pre>
      </section>
    </div>

    <section class="grid gap-3">
      <div class="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
        <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">K3D clusters</h3>
        <div class="text-sm text-zinc-500 dark:text-zinc-400">{{ clusterSummary }}</div>
      </div>
      <article v-for="cluster in clusters" :key="cluster.runId" class="run-row" :data-tone="clusterTone(cluster)">
        <div class="run-row-main">
          <div class="run-row-titlebar">
            <h4 class="run-title">{{ cluster.runId }}</h4>
            <span class="run-status-pill" :data-tone="clusterTone(cluster)">{{ cluster.status }}</span>
          </div>
          <div class="run-kpi-grid">
            <div class="run-kpi sm:col-span-2">
              <div class="run-kpi-label">Endpoint</div>
              <div class="mt-1 break-all text-sm font-semibold leading-6 text-zinc-900 dark:text-zinc-100" :title="cluster.apiUrl">{{ cluster.apiUrl || "not ready" }}</div>
            </div>
            <div class="run-kpi">
              <div class="run-kpi-label">K3s</div>
              <div class="run-kpi-value">{{ cluster.k3sVersion }}</div>
            </div>
            <div class="run-kpi">
              <div class="run-kpi-label">Cluster</div>
              <div class="run-kpi-value" :title="cluster.clusterName">{{ cluster.clusterName }}</div>
            </div>
            <div class="run-kpi">
              <div class="run-kpi-label">Updated</div>
              <div class="run-kpi-value">{{ timeLabel(cluster.updatedAt) }}</div>
            </div>
          </div>
          <div class="run-footline">
            <div><strong>Kubeconfig:</strong> <span :title="cluster.kubeconfig">{{ compactPath(cluster.kubeconfig) }}</span></div>
          </div>
          <p v-if="cluster.error" class="mt-3 text-sm font-semibold text-rose-600 dark:text-rose-300">{{ cluster.error }}</p>
        </div>
        <div class="run-command-panel">
          <div class="run-primary-actions">
            <button type="button" class="run-action-button run-action-button--primary" :disabled="!cluster.apiUrl" @click="copyText(cluster.apiUrl, 'Endpoint copied.')">Copy endpoint</button>
            <button type="button" class="run-action-button run-action-button--primary" :disabled="!cluster.kubeconfig || savingKubeconfigRunId === cluster.runId" @click="saveKubeconfig(cluster)">
              {{ savingKubeconfigRunId === cluster.runId ? "Saving..." : "Download kubeconfig" }}
            </button>
            <button type="button" class="run-action-button run-action-button--utility" :disabled="!cluster.kubeconfig" @click="copyText(cluster.kubeconfig, 'Kubeconfig path copied.')">Copy kubeconfig</button>
            <button type="button" class="run-action-button run-action-button--secondary" :disabled="cluster.status !== 'running' || operation.running || rowActionRunning" @click="clusterAction(cluster, 'stop')">
              {{ rowActionLabel(cluster, "stop", "Stop", "Stopping...") }}
            </button>
            <button type="button" class="run-action-button run-action-button--secondary" :disabled="cluster.status !== 'stopped' || operation.running || rowActionRunning" @click="clusterAction(cluster, 'restart')">
              {{ rowActionLabel(cluster, "restart", "Start", "Starting...") }}
            </button>
            <button type="button" class="run-action-button run-action-button--danger" :disabled="operation.running || rowActionRunning" @click="clusterAction(cluster, 'delete', true)">
              {{ rowActionLabel(cluster, "delete", "Delete", "Deleting...") }}
            </button>
          </div>
        </div>
      </article>
      <div v-if="!clusters.length" class="rounded-xl border border-zinc-200 bg-zinc-50 p-5 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-400">
        No K3D Lab clusters yet.
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";

const setupData = JSON.parse(document.getElementById("control-panel-data")?.textContent || "{}");
const token = setupData.token || "";

const state = ref({ preflight: { ready: false, summary: "Checking...", items: [] }, operation: { output: [] }, clusters: [], k3sVersions: [] });
const k3sVersion = ref("");
const apiPort = ref("");
const notice = ref("");
const noticeKind = ref("info");
const stopping = ref(false);
const savingKubeconfigRunId = ref("");
const installing = ref(false);
const clearingOutput = ref(false);
const outputCollapsed = ref(false);
const activeRowAction = ref({ runId: "", action: "" });
let timer = null;

const headers = computed(() => ({
  "Content-Type": "application/json",
  "X-Control-Panel-Token": token,
}));

const preflight = computed(() => state.value.preflight || { ready: false, summary: "", items: [] });
const preflightItems = computed(() => Array.isArray(preflight.value.items) ? preflight.value.items : []);
const operation = computed(() => state.value.operation || { output: [] });
const clusters = computed(() => (
  Array.isArray(state.value.clusters)
    ? state.value.clusters.filter(cluster => cluster.status !== "deleted")
    : []
));
const activeClusters = computed(() => clusters.value.filter(cluster => ["creating", "running"].includes(cluster.status)));
const runningClusters = computed(() => clusters.value.filter(cluster => cluster.status === "running"));
const stoppedClusters = computed(() => clusters.value.filter(cluster => cluster.status === "stopped"));
const creatingClusters = computed(() => clusters.value.filter(cluster => cluster.status === "creating"));
const k3sOptions = computed(() => state.value.k3sVersions || []);
const startDisabled = computed(() => !preflight.value.ready || operation.value.running || !k3sVersion.value);
const missingK3D = computed(() => preflightItems.value.some(item => item.name === "k3d" && item.status === "error"));
const actionSummary = computed(() => {
  if (operation.value.running) {
    return operation.value.runId || "K3D action running";
  }
  if (runningClusters.value.length) {
    return "Ready to start another cluster";
  }
  return "No k3d action running";
});
const clusterSummary = computed(() => {
  const parts = [];
  if (creatingClusters.value.length) {
    parts.push(`${creatingClusters.value.length} creating`);
  }
  parts.push(`${runningClusters.value.length} running`);
  if (stoppedClusters.value.length) {
    parts.push(`${stoppedClusters.value.length} stopped`);
  }
  return `${parts.join(", ")}.`;
});
const outputText = computed(() => (Array.isArray(operation.value.output) && operation.value.output.length)
  ? operation.value.output.join("\n")
  : "K3D Lab output will appear here.");
const hasOutput = computed(() => Array.isArray(operation.value.output) && operation.value.output.length > 0);
const outputLineCount = computed(() => Array.isArray(operation.value.output) ? operation.value.output.length : 0);
const rowActionRunning = computed(() => Boolean(activeRowAction.value.runId));
const noticeTone = computed(() => noticeKind.value === "error" ? "text-rose-600 dark:text-rose-300" : "text-emerald-600 dark:text-emerald-300");

watch(k3sOptions, values => {
  if (!k3sVersion.value && values.length) {
    k3sVersion.value = values[0];
  }
});

const setNotice = (message, kind = "info") => {
  notice.value = message;
  noticeKind.value = kind;
};

const apiFetch = async (path, options = {}) => {
  const response = await fetch(path, {
    ...options,
    headers: {
      ...headers.value,
      ...(options.headers || {}),
    },
  });
  if (!response.ok) {
    throw new Error((await response.text()) || "K3D Lab request failed.");
  }
  return response.json();
};

const refreshState = async () => {
  state.value = await apiFetch("/api/k3d/state");
  if (!k3sVersion.value && state.value.k3sVersions?.length) {
    k3sVersion.value = state.value.k3sVersions[0];
  }
};

const startCluster = async () => {
  setNotice("");
  try {
    await apiFetch("/api/k3d/start", {
      method: "POST",
      body: JSON.stringify({
        k3sVersion: k3sVersion.value,
        apiPort: Number(apiPort.value || 0),
      }),
    });
    setNotice("K3D startup started.");
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to start K3D Lab.", "error");
  }
};

const installK3D = async () => {
  installing.value = true;
  setNotice("");
  try {
    await apiFetch("/api/k3d/install", { method: "POST", body: "{}" });
    setNotice("k3d install started. Watch Live output, then refresh tools.");
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to start k3d install.", "error");
  } finally {
    installing.value = false;
  }
};

const stopAction = async () => {
  stopping.value = true;
  try {
    await apiFetch("/api/operations/abort", {
      method: "POST",
      body: JSON.stringify({ operation: "k3dLab", runId: operation.value.runId || "", confirm: "stop" }),
    });
    setNotice("Stop requested for K3D Lab action.");
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to stop K3D Lab action.", "error");
  } finally {
    stopping.value = false;
  }
};

const clearOutput = async () => {
  clearingOutput.value = true;
  try {
    await apiFetch("/api/k3d/output/clear", { method: "POST", body: "{}" });
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to clear K3D Lab output.", "error");
  } finally {
    clearingOutput.value = false;
  }
};

const clusterAction = async (cluster, action, deleteDir = false) => {
  activeRowAction.value = { runId: cluster.runId, action };
  setNotice(action === "delete" ? "Deleting K3D cluster..." : `${action === "restart" ? "Starting" : "Stopping"} K3D cluster...`);
  try {
    await apiFetch(`/api/k3d/${action}`, {
      method: "POST",
      body: JSON.stringify({ runId: cluster.runId, deleteDir }),
    });
    setNotice(action === "restart" ? "K3D cluster started." : `K3D cluster ${action} complete.`);
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : `K3D ${action} failed.`, "error");
    await refreshState();
  } finally {
    activeRowAction.value = { runId: "", action: "" };
  }
};

const rowActionLabel = (cluster, action, idle, busy) => (
  activeRowAction.value.runId === cluster.runId && activeRowAction.value.action === action ? busy : idle
);

const copyText = async (value, message) => {
  if (!value) {
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    setNotice(message);
  } catch {
    setNotice(value);
  }
};

const saveKubeconfig = async cluster => {
  savingKubeconfigRunId.value = cluster.runId;
  try {
    const saved = await apiFetch("/api/k3d/kubeconfig/save", {
      method: "POST",
      body: JSON.stringify({ runId: cluster.runId }),
    });
    setNotice(`${saved.filename || "Kubeconfig"} saved to Downloads.`);
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to save kubeconfig.", "error");
  } finally {
    savingKubeconfigRunId.value = "";
  }
};

const toolStatusClass = status => {
  if (status === "ok") return "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-200";
  if (status === "warning") return "bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-200";
  return "bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-200";
};

const clusterTone = cluster => {
  if (cluster.status === "failed") return "rose";
  if (cluster.status === "running" || cluster.status === "creating") return "sky";
  return "emerald";
};

const compactPath = value => {
  const text = String(value || "");
  return text.length <= 72 ? text : `${text.slice(0, 28)}...${text.slice(-36)}`;
};

const timeLabel = value => value ? new Date(value).toLocaleString() : "";

onMounted(async () => {
  await refreshState();
  timer = window.setInterval(refreshState, 4000);
});

onUnmounted(() => {
  window.clearInterval(timer);
});
</script>
