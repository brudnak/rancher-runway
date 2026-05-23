<template>
  <div class="grid gap-5">
    <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0">
        <div class="inline-flex items-center rounded-full border border-sky-200 bg-sky-50 px-3 py-1 text-xs font-semibold text-sky-700 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-200">
          Local k3d lab
        </div>
        <h2 class="mt-3 text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Steve Lab</h2>
        <p class="mt-2 max-w-4xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
          Discover Steve releases or enter an exact commit, choose a local k3d Kubernetes version, and keep one local Steve HTTPS endpoint running.
        </p>
      </div>
      <div class="grid shrink-0 gap-2 sm:grid-cols-3 lg:min-w-[28rem]">
        <div class="run-summary-stat" :data-tone="preflight.ready ? 'ready' : 'locked'">
          <div class="run-summary-label">Local tools</div>
          <div class="run-summary-value">{{ preflight.ready ? "Ready" : "Blocked" }}</div>
          <div class="run-summary-help">{{ preflight.summary || "Checking Docker and k3d" }}</div>
          <button type="button" class="mt-2 text-xs font-semibold text-sky-600 hover:text-sky-500 dark:text-sky-300" @click="refreshState">Refresh tools</button>
        </div>
        <div class="run-summary-stat" :data-tone="operation.running ? 'ready' : ''">
          <div class="run-summary-label">Startup</div>
          <div class="run-summary-value">{{ operation.running ? "Running" : "Idle" }}</div>
          <div class="run-summary-help">{{ operation.runId || "No active startup" }}</div>
        </div>
        <div class="run-summary-stat">
          <div class="run-summary-label">Active</div>
          <div class="run-summary-value">{{ activeRun ? "1" : "0" }}</div>
          <div class="run-summary-help">{{ activeRun ? activeRun.runId : "No endpoint serving" }}</div>
        </div>
      </div>
    </div>

    <div class="grid gap-5 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)]">
      <section class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Choose Steve</h3>
            <p class="mt-1 text-sm leading-6 text-zinc-600 dark:text-zinc-400">Pick a release tag or paste an exact commit SHA.</p>
          </div>
          <button type="button" class="run-action-button run-action-button--utility" @click="loadVersions" :disabled="versionsLoading">
            {{ versionsLoading ? "Refreshing..." : "Refresh tags" }}
          </button>
        </div>

        <div class="mt-4 grid gap-4">
          <label class="grid gap-2 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
            <span>Release tag</span>
            <select v-model="selectedTag" class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100">
              <option value="">Choose a Steve tag</option>
              <option v-for="tag in versions.tags" :key="tag.name" :value="tag.name">{{ tag.name }}</option>
            </select>
          </label>

          <label class="grid gap-2 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
            <span>Exact tag, branch, or commit</span>
            <input v-model.trim="steveRef" type="text" autocomplete="off" placeholder="v0.9.10 or a commit SHA" class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none placeholder:text-zinc-400 focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100 dark:placeholder:text-zinc-500" />
          </label>

          <div class="rounded-lg border border-sky-200 bg-sky-50 p-3 text-sm leading-6 text-sky-900 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
            <div class="font-semibold">{{ refStatusTitle }}</div>
            <div class="mt-1">{{ refStatusBody }}</div>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03]">
        <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Serve with k3d</h3>
        <p class="mt-1 text-sm leading-6 text-zinc-600 dark:text-zinc-400">Create a local k3d cluster, start Steve over HTTPS, and replace the current endpoint when needed.</p>

        <div class="mt-4 grid gap-4">
          <label class="grid gap-2 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
            <span>K3s image tag</span>
            <input v-model.trim="k3sVersion" list="steve-k3s-versions" type="text" autocomplete="off" class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100" />
            <datalist id="steve-k3s-versions">
              <option v-for="version in k3sOptions" :key="version" :value="version"></option>
            </datalist>
          </label>

          <label class="grid gap-2 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
            <span>HTTPS port</span>
            <input v-model.number="httpsPort" type="number" min="1024" max="65535" placeholder="Auto" class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none placeholder:text-zinc-400 focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100 dark:placeholder:text-zinc-500" />
          </label>

          <div class="rounded-lg border border-emerald-200 bg-emerald-50 p-3 text-sm leading-6 text-emerald-900 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-100">
            Steve Lab keeps one HTTPS endpoint alive. Launching again replaces the current Steve cluster and run files.
          </div>

          <div class="flex flex-wrap gap-2">
            <button type="button" class="run-action-button run-action-button--primary" :disabled="startDisabled" @click="startRun">
              {{ startButtonLabel }}
            </button>
            <button type="button" class="run-action-button run-action-button--danger" :disabled="!operation.running || stopping" @click="stopRun">
              {{ stopping ? "Stopping..." : "Stop startup" }}
            </button>
          </div>
          <p v-if="notice" class="text-sm font-semibold" :class="noticeTone">{{ notice }}</p>
        </div>
      </section>
    </div>

    <section class="rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.03]">
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Live output</h3>
          <p class="mt-1 text-sm leading-6 text-zinc-600 dark:text-zinc-400">{{ operation.command || "No Steve Lab startup has started yet." }}</p>
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
      <pre v-else class="mt-4 max-h-[28rem] max-w-full whitespace-pre-wrap break-words overflow-auto rounded-lg border border-zinc-200 bg-zinc-50 p-4 text-xs leading-5 text-zinc-800 dark:border-transparent dark:bg-zinc-950 dark:text-zinc-100">{{ outputText }}</pre>
    </section>

    <section class="grid gap-3">
      <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Steve Lab endpoint history</h3>
      <article v-for="run in runs" :key="run.runId" class="run-row" :data-tone="run.status === 'failed' ? 'rose' : run.status === 'running' ? 'sky' : 'emerald'">
        <div class="run-row-main">
          <div class="run-row-titlebar">
            <h4 class="run-title">{{ run.runId }}</h4>
            <span class="run-status-pill" :data-tone="run.status === 'failed' ? 'rose' : run.status === 'running' ? 'sky' : 'emerald'">{{ run.status }}</span>
          </div>
          <div class="run-kpi-grid">
            <div class="run-kpi">
              <div class="run-kpi-label">Steve</div>
              <div class="run-kpi-value" :title="run.steveCommit || run.steveRef">{{ run.steveRef }}</div>
            </div>
            <div class="run-kpi sm:col-span-2">
              <div class="run-kpi-label">Endpoint</div>
              <div class="mt-1 max-w-full overflow-x-auto whitespace-nowrap text-sm font-semibold leading-6 text-zinc-900 dark:text-zinc-100" :title="endpointUrl(run)">{{ endpointUrl(run) || "not ready" }}</div>
            </div>
            <div class="run-kpi">
              <div class="run-kpi-label">K3s</div>
              <div class="run-kpi-value">{{ run.k3sVersion }}</div>
            </div>
            <div class="run-kpi">
              <div class="run-kpi-label">Updated</div>
              <div class="run-kpi-value">{{ timeLabel(run.updatedAt) }}</div>
            </div>
          </div>
          <div class="run-footline">
            <div><strong>Kubeconfig:</strong> <span :title="run.kubeconfig">{{ compactPath(run.kubeconfig) }}</span></div>
            <div><strong>Log:</strong> <span :title="run.logPath">{{ compactPath(run.logPath) }}</span></div>
          </div>
          <p v-if="run.error" class="mt-3 text-sm font-semibold text-rose-600 dark:text-rose-300">{{ run.error }}</p>
        </div>
        <div class="run-command-panel">
          <div class="run-primary-actions">
            <button type="button" class="run-action-button run-action-button--primary" :disabled="!endpointUrl(run) || run.status !== 'serving'" @click="openEndpoint(run)">Open endpoint</button>
            <button type="button" class="run-action-button run-action-button--utility" :disabled="!endpointUrl(run)" @click="copyEndpoint(run)">Copy endpoint</button>
            <button type="button" class="run-action-button run-action-button--utility" :disabled="!run.kubeconfig || savingKubeconfigRunId === run.runId" @click="saveKubeconfig(run)">
              {{ savingKubeconfigRunId === run.runId ? "Saving..." : "Download kubeconfig" }}
            </button>
            <button type="button" class="run-action-button run-action-button--secondary" :disabled="!run.stevePid || rowActionRunning" @click="stopEndpoint(run)">
              {{ rowActionLabel(run, "stop", "Stop endpoint", "Stopping...") }}
            </button>
            <button type="button" class="run-action-button run-action-button--secondary" :disabled="rowActionRunning" @click="cleanupRun(run, false, true)">
              {{ rowActionLabel(run, "delete-k3d", "Delete k3d", "Deleting...") }}
            </button>
            <button type="button" class="run-action-button run-action-button--danger" :disabled="rowActionRunning" @click="cleanupRun(run, true, true)">
              {{ rowActionLabel(run, "delete-all", "Delete all", "Deleting...") }}
            </button>
          </div>
        </div>
      </article>
      <div v-if="!runs.length" class="rounded-xl border border-zinc-200 bg-zinc-50 p-5 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-400">
        No Steve Lab runs yet.
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";

const setupData = JSON.parse(document.getElementById("control-panel-data")?.textContent || "{}");
const token = setupData.token || "";

const state = ref({ preflight: { ready: false, summary: "Checking...", items: [] }, operation: { output: [] }, runs: [], k3sVersions: [] });
const versions = ref({ tags: [] });
const refDetails = ref({});
const selectedTag = ref("");
const steveRef = ref("");
const k3sVersion = ref("");
const httpsPort = ref("");
const versionsLoading = ref(false);
const notice = ref("");
const noticeKind = ref("info");
const starting = ref(false);
const stopping = ref(false);
const savingKubeconfigRunId = ref("");
const clearingOutput = ref(false);
const outputCollapsed = ref(false);
const activeRowAction = ref({ runId: "", action: "" });
let timer = null;
let refTimer = null;

const headers = computed(() => ({
  "Content-Type": "application/json",
  "X-Control-Panel-Token": token,
}));

const preflight = computed(() => state.value.preflight || { ready: false, summary: "", items: [] });
const operation = computed(() => state.value.operation || { output: [] });
const runs = computed(() => (
  Array.isArray(state.value.runs)
    ? state.value.runs.filter(run => !["cleaned", "deleted"].includes(run.status))
    : []
));
const activeRun = computed(() => runs.value.find(run => run.stevePid || ["running", "starting", "serving"].includes(run.status)));
const k3sOptions = computed(() => {
  const values = refDetails.value?.recommendedK3sVersions?.length ? refDetails.value.recommendedK3sVersions : state.value.k3sVersions || [];
  return [...new Set(values.filter(Boolean))];
});

const startDisabled = computed(() => starting.value || !preflight.value.ready || operation.value.running || !steveRef.value || !k3sVersion.value);
const startButtonLabel = computed(() => {
  if (starting.value) {
    return activeRun.value ? "Replacing..." : "Launching...";
  }
  return activeRun.value ? "Replace endpoint" : "Launch endpoint";
});
const outputText = computed(() => (Array.isArray(operation.value.output) && operation.value.output.length)
  ? operation.value.output.join("\n")
  : "Steve Lab output will appear here.");
const hasOutput = computed(() => Array.isArray(operation.value.output) && operation.value.output.length > 0);
const outputLineCount = computed(() => Array.isArray(operation.value.output) ? operation.value.output.length : 0);
const noticeTone = computed(() => noticeKind.value === "error" ? "text-rose-600 dark:text-rose-300" : "text-emerald-600 dark:text-emerald-300");
const rowActionRunning = computed(() => Boolean(activeRowAction.value.runId));

const refStatusTitle = computed(() => {
  if (refDetails.value?.error) {
    return "Could not inspect that ref yet";
  }
  if (refDetails.value?.recommendedMinor) {
    return `Suggested Kubernetes ${refDetails.value.recommendedMinor}`;
  }
  return "Steve ref ready";
});

const refStatusBody = computed(() => {
  if (refDetails.value?.error) {
    return refDetails.value.error;
  }
  if (refDetails.value?.kubernetesModuleVersion) {
    return `${refDetails.value.kubernetesModule} ${refDetails.value.kubernetesModuleVersion} maps to the first suggested k3d option. You can still override it.`;
  }
  return "Choose a tag or paste a commit SHA. The app will inspect Steve's go.mod and suggest a k3d Kubernetes minor when it can.";
});

watch(selectedTag, value => {
  if (value) {
    steveRef.value = value;
  }
});

watch(steveRef, () => {
  window.clearTimeout(refTimer);
  refTimer = window.setTimeout(loadRefDetails, 350);
});

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
    throw new Error((await response.text()) || "Steve Lab request failed.");
  }
  return response.json();
};

const refreshState = async () => {
  state.value = await apiFetch("/api/steve/state");
  if (!k3sVersion.value && state.value.k3sVersions?.length) {
    k3sVersion.value = state.value.k3sVersions[0];
  }
};

const loadVersions = async () => {
  versionsLoading.value = true;
  try {
    versions.value = await apiFetch("/api/steve/versions");
    if (!steveRef.value && versions.value.tags?.length) {
      selectedTag.value = versions.value.tags[0].name;
    }
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to load Steve tags.", "error");
  } finally {
    versionsLoading.value = false;
  }
};

const loadRefDetails = async () => {
  if (!steveRef.value) {
    refDetails.value = {};
    return;
  }
  try {
    refDetails.value = await apiFetch(`/api/steve/ref?ref=${encodeURIComponent(steveRef.value)}`);
    if (refDetails.value.recommendedK3sVersions?.length) {
      k3sVersion.value = refDetails.value.recommendedK3sVersions[0];
    }
  } catch (error) {
    refDetails.value = { error: error instanceof Error ? error.message : "Failed to inspect ref." };
  }
};

const startRun = async () => {
  const replacing = Boolean(activeRun.value);
  starting.value = true;
  setNotice("");
  try {
    await apiFetch("/api/steve/start", {
      method: "POST",
      body: JSON.stringify({
        steveRef: steveRef.value,
        k3sVersion: k3sVersion.value,
        keepCluster: true,
        httpsPort: Number(httpsPort.value || 0),
        headerAuth: true,
        replace: replacing,
      }),
    });
    setNotice(replacing ? "Replacing Steve endpoint." : "Steve endpoint startup started.");
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to start Steve Lab.", "error");
  } finally {
    starting.value = false;
  }
};

const stopRun = async () => {
  stopping.value = true;
  try {
    await apiFetch("/api/operations/abort", {
      method: "POST",
      body: JSON.stringify({ operation: "steveLab", runId: operation.value.runId || "", confirm: "stop" }),
    });
    setNotice("Stop requested for Steve Lab startup.");
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to stop Steve Lab.", "error");
  } finally {
    stopping.value = false;
  }
};

const clearOutput = async () => {
  clearingOutput.value = true;
  try {
    await apiFetch("/api/steve/output/clear", { method: "POST", body: "{}" });
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to clear Steve Lab output.", "error");
  } finally {
    clearingOutput.value = false;
  }
};

const cleanupRun = async (run, deleteDir, deleteK3d) => {
  activeRowAction.value = { runId: run.runId, action: deleteDir ? "delete-all" : "delete-k3d" };
  setNotice(deleteDir ? "Deleting Steve Lab files and k3d cluster..." : "Deleting Steve Lab k3d cluster...");
  try {
    await apiFetch("/api/steve/cleanup", {
      method: "POST",
      body: JSON.stringify({ runId: run.runId, deleteDir, deleteK3d }),
    });
    setNotice(deleteDir ? "Steve Lab run deleted." : "Steve Lab k3d cluster deleted.");
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Cleanup failed.", "error");
    await refreshState();
  } finally {
    activeRowAction.value = { runId: "", action: "" };
  }
};

const stopEndpoint = async run => {
  activeRowAction.value = { runId: run.runId, action: "stop" };
  try {
    await apiFetch("/api/steve/stop", {
      method: "POST",
      body: JSON.stringify({ runId: run.runId }),
    });
    setNotice("Steve endpoint stopped.");
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to stop endpoint.", "error");
  } finally {
    activeRowAction.value = { runId: "", action: "" };
  }
};

const rowActionLabel = (run, action, idle, busy) => (
  activeRowAction.value.runId === run.runId && activeRowAction.value.action === action ? busy : idle
);

const endpointUrl = run => run?.httpsUrl || run?.httpUrl || "";

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

const copyEndpoint = run => copyText(endpointUrl(run), "Endpoint URL copied.");

const openEndpoint = async run => {
  const url = endpointUrl(run);
  if (!url) {
    return;
  }
  try {
    await apiFetch("/api/open-url", {
      method: "POST",
      body: JSON.stringify({ url }),
    });
    setNotice("Endpoint opened.");
  } catch (error) {
    window.open(url, "_blank", "noopener,noreferrer");
    setNotice(error instanceof Error ? `Tried browser fallback: ${error.message}` : "Tried browser fallback.");
  }
};

const saveKubeconfig = async run => {
  savingKubeconfigRunId.value = run.runId;
  try {
    const saved = await apiFetch("/api/steve/kubeconfig/save", {
      method: "POST",
      body: JSON.stringify({ runId: run.runId }),
    });
    setNotice(`${saved.filename || "Kubeconfig"} saved to Downloads.`);
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to save kubeconfig.", "error");
  } finally {
    savingKubeconfigRunId.value = "";
  }
};

const compactPath = value => {
  const text = String(value || "");
  return text.length <= 72 ? text : `${text.slice(0, 28)}...${text.slice(-36)}`;
};

const timeLabel = value => value ? new Date(value).toLocaleString() : "";

onMounted(async () => {
  await refreshState();
  await loadVersions();
  timer = window.setInterval(refreshState, 4000);
});

onUnmounted(() => {
  window.clearInterval(timer);
  window.clearTimeout(refTimer);
});
</script>
