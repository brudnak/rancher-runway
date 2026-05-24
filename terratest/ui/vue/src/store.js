import { ref, reactive, computed, watch } from "vue";
import {
  escapeHtml,
  highlightLogLine,
  lineMatchesLogLevel,
  clusterItems,
  operationOutput,
  sameRunKey,
  trimTrailingPathSeparator,
  parentPath,
} from "../../static/control_panel_utils.js";
import {
  runFolderPath,
  runFolderAvailable,
  runTerraformPath,
} from "../../static/control_panel_runs.js";

// Token & Config retrieval
const setupData = JSON.parse(document.getElementById("control-panel-data")?.textContent || "{}");
export const token = setupData.token || "";

// Shared Reactive State variables
export const state = ref(window.rancherControlPanelState || {});
export const bootPending = ref(true);
export const bootDetail = ref("Checking local config, run slots, Terraform state, lifecycle processes, clusters, and AWS inventory before enabling actions.");
export const refreshedAt = ref(null);
export const activeTab = ref(localStorage.getItem("rancherControlPanelTab") || "setup");
if (activeTab.value === "lifecycle") {
  activeTab.value = "runs";
}
export const activeDestroyTab = ref(localStorage.getItem("rancherDestroyTab") || "slots");
export const refreshStatus = ref("Waiting for first refresh...");

// Theme management
const savedTheme = localStorage.getItem("rancherControlPanelTheme");
const hintedTheme = new URLSearchParams(window.location.search).get("systemTheme");
const systemDark = hintedTheme === "dark" || (!hintedTheme && window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches);
export const theme = ref(savedTheme ? savedTheme : (systemDark ? "dark" : "light"));

// Fullscreen management
export const fullscreen = ref(false);

// Logs State
export const logs = reactive({
  show: false,
  mode: "", // 'live', 'tail', 'docker', 'setup', 'linodeSetup', 'readiness', 'cleanup', 'linodeCleanup'
  clusterId: "",
  namespace: "",
  podName: "",
  rawText: "",
  visibleText: "",
  matchCountLabel: "0 lines",
  level: "all", // 'all', 'info', 'debug', 'warning', 'error'
  search: "",
  liveState: "idle", // 'idle', 'connecting', 'live', 'stopped', 'error', and operation specific ones
  statusText: "",
});

// Dangerous confirmation modal state
export const dangerConfirm = reactive({
  show: false,
  title: "",
  body: "",
  typedValue: "",
  confirmText: "",
  accentText: "Confirmation required",
  input: "",
  error: "",
  resolve: null,
});

// Upgrade command warning notice state
export const upgradeCommandModalOpen = ref(false);

// Toast notice state
export const notice = reactive({
  show: false,
  title: "",
  body: "",
  timer: null,
});

// Preflight checks state
export const preflight = ref(window.rancherControlPanelPreflight || { ready: false, summary: "Preflight has not run yet.", items: [] });
export const preflightChecking = ref(false);

// Cost DB reset & Cleanup states
export const selectedCleanupRunId = ref("");
export const cleanupStarting = ref(false);
export const dismissedCleanupResultKey = ref("");
export const costResetting = ref(false);
export const localArtifactsCleaning = ref(false);

// Active cluster viewing context
export const activeClusterRunKey = ref("");
export const activeClusterHAKey = ref("");

// Setup pending safety locks
export const setupLaunchPendingUntil = ref(0);
export const pendingAbortOperation = ref("");
export const refreshInFlight = ref(false);

// Action trackers (to disable individual buttons when busy)
export const activeDownloadClusterId = ref("");
export const activeCopyClusterId = ref("");
export const activeCopyHelmClusterId = ref("");
export const activeCopyHelmUpgradeClusterId = ref("");
export const activeOpenKubeconfigPathClusterId = ref("");
export const activeCopyKubeconfigPathClusterId = ref("");
export const activeCopyLinodeIPClusterId = ref("");
export const activeDockerLogsClusterId = ref("");

// GPU cost reminders state
const gpuReminderSettingsKey = "rancherGpuReminderSettings";
const gpuReminderIntervals = [15, 30, 60];
const loadGPUReminderSettings = () => {
  try {
    const parsed = JSON.parse(localStorage.getItem(gpuReminderSettingsKey) || "{}");
    const intervalMinutes = gpuReminderIntervals.includes(Number(parsed.intervalMinutes)) ? Number(parsed.intervalMinutes) : 15;
    return {
      intervalMinutes,
      disabled: Boolean(parsed.disabled),
      lastReminderAt: Number(parsed.lastReminderAt || 0),
    };
  } catch {
    return { intervalMinutes: 15, disabled: false, lastReminderAt: 0 };
  }
};
export const gpuReminderSettings = ref(loadGPUReminderSettings());
export const gpuReminderModalOpen = ref(false);
export const gpuReminderBody = ref("");

// Helper: standard API fetch
export const apiFetch = async (path, options = {}) => {
  const response = await fetch(path, {
    ...options,
    headers: {
      "X-Control-Panel-Token": token,
      "Content-Type": "application/json",
      Accept: "application/json",
      ...(options.headers || {}),
    },
  });
  if (!response.ok) {
    throw new Error((await response.text()) || "Request failed.");
  }
  return response;
};

// Lifecycle checkers
export const awsLifecycleRunning = computed(() =>
  Boolean(state.value?.setup?.running || state.value?.readiness?.running || state.value?.cleanup?.running)
);
export const linodeLifecycleRunning = computed(() =>
  Boolean(state.value?.linodeSetup?.running || state.value?.linodeCleanup?.running)
);
export const lifecycleRunning = computed(() =>
  Boolean(awsLifecycleRunning.value || linodeLifecycleRunning.value)
);

export const runIsLinodeDocker = run => run?.deploymentType === "linode-docker-cattle";
export const runDestroyBlocked = run =>
  runIsLinodeDocker(run) ? linodeLifecycleRunning.value : awsLifecycleRunning.value;

export const lifecycleBusyDetail = () => {
  const curState = state.value;
  if (curState?.setup?.running) {
    return {
      busy: true,
      operation: "setup",
      message: "Setup is running. New AWS setup actions are locked, but Linode Docker setup can run in parallel.",
      busyByDeployment: {
        "ha-rke2": true,
        "hosted-tenant-k3s": true,
        "linode-docker-cattle": linodeLifecycleRunning.value,
      },
    };
  }
  if (curState?.readiness?.running) {
    return {
      busy: true,
      operation: "readiness",
      message: "Readiness checks are running. AWS actions are locked, but Linode Docker setup can run in parallel.",
      busyByDeployment: {
        "ha-rke2": true,
        "hosted-tenant-k3s": true,
        "linode-docker-cattle": linodeLifecycleRunning.value,
      },
    };
  }
  if (curState?.cleanup?.running) {
    return {
      busy: true,
      operation: "destroy",
      message: "Destroy is running. AWS actions are locked, but Linode Docker setup can run in parallel.",
      busyByDeployment: {
        "ha-rke2": true,
        "hosted-tenant-k3s": true,
        "linode-docker-cattle": linodeLifecycleRunning.value,
      },
    };
  }
  if (curState?.linodeSetup?.running) {
    return {
      busy: true,
      operation: "linodeSetup",
      message: "Linode setup is running. AWS setup and destroy can still run in their own lane.",
      busyByDeployment: {
        "ha-rke2": awsLifecycleRunning.value,
        "hosted-tenant-k3s": awsLifecycleRunning.value,
        "linode-docker-cattle": true,
      },
    };
  }
  if (curState?.linodeCleanup?.running) {
    return {
      busy: true,
      operation: "linodeCleanup",
      message: "Linode destroy is running. AWS setup and destroy can still run in their own lane.",
      busyByDeployment: {
        "ha-rke2": awsLifecycleRunning.value,
        "hosted-tenant-k3s": awsLifecycleRunning.value,
        "linode-docker-cattle": true,
      },
    };
  }
  return {
    busy: false,
    operation: "",
    message: "",
    busyByDeployment: {
      "ha-rke2": awsLifecycleRunning.value,
      "hosted-tenant-k3s": awsLifecycleRunning.value,
      "linode-docker-cattle": linodeLifecycleRunning.value,
    },
  };
};

export const dispatchSetupLifecycleState = () => {
  dispatchSetupRootEvent("rancher-control-panel-lifecycle", lifecycleBusyDetail());
};

const dispatchSetupRootEvent = (eventName, detail) => {
  const root = document.getElementById("interactiveSetupRoot");
  if (!root) return;
  root.dispatchEvent(new CustomEvent(eventName, { detail }));
};

// Theme Action
export const setTheme = (nextTheme, persist = true) => {
  theme.value = nextTheme;
  document.documentElement.classList.toggle("dark", nextTheme === "dark");
  document.body.classList.toggle("dark", nextTheme === "dark");
  if (persist) {
    localStorage.setItem("rancherControlPanelTheme", nextTheme);
  }
  window.dispatchEvent(new CustomEvent("rancher-control-panel:theme", { detail: { theme: nextTheme } }));
};

// Fullscreen Actions
const wailsRuntime = () => window.runtime || null;

export const syncFullscreenButton = async () => {
  let nativeFullscreen = false;
  const runtime = wailsRuntime();
  if (runtime?.WindowIsFullscreen) {
    try {
      nativeFullscreen = Boolean(await runtime.WindowIsFullscreen());
    } catch (_) {
      nativeFullscreen = false;
    }
  }
  fullscreen.value = Boolean(nativeFullscreen || document.fullscreenElement);
  document.body.dataset.panelFullscreen = fullscreen.value ? "true" : "false";
  window.dispatchEvent(new CustomEvent("rancher-control-panel:fullscreen", { detail: { fullscreen: fullscreen.value } }));
};

export const setPanelFullscreen = async nextFullscreen => {
  const runtime = wailsRuntime();
  try {
    if (runtime?.WindowFullscreen && runtime?.WindowUnfullscreen) {
      if (nextFullscreen) {
        await runtime.WindowFullscreen();
      } else {
        await runtime.WindowUnfullscreen();
      }
    } else if (document.fullscreenEnabled) {
      if (nextFullscreen && !document.fullscreenElement) {
        await document.documentElement.requestFullscreen();
      } else if (!nextFullscreen && document.fullscreenElement) {
        await document.exitFullscreen();
      }
    } else {
      document.body.dataset.panelFullscreen = nextFullscreen ? "true" : "false";
      fullscreen.value = Boolean(nextFullscreen);
    }
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Fullscreen request failed";
  } finally {
    window.setTimeout(syncFullscreenButton, 120);
  }
};

// Tab management
export const setActivePanelTab = tab => {
  const availableTabs = new Set(["setup", "runs", "clusters", "aws", "destroy", "settings", "k3d", "steve"]);
  activeTab.value = availableTabs.has(tab) ? tab : "runs";
  localStorage.setItem("rancherControlPanelTab", activeTab.value);
  window.dispatchEvent(new CustomEvent("rancher-control-panel:tab", { detail: { tab: activeTab.value } }));
  if (activeTab.value === "setup" && state.value) {
    dispatchSetupLifecycleState();
  }
};

export const setActiveDestroyTab = tab => {
  activeDestroyTab.value = tab === "costs" ? "costs" : "slots";
  localStorage.setItem("rancherDestroyTab", activeDestroyTab.value);
};

// Toast Notice controller
export const showPanelNotice = (title, body) => {
  if (notice.timer) {
    window.clearTimeout(notice.timer);
  }
  notice.title = title;
  notice.body = body;
  notice.show = true;
  notice.timer = window.setTimeout(() => {
    notice.show = false;
    notice.timer = null;
  }, 9000);
};

export const hidePanelNotice = () => {
  if (notice.timer) {
    window.clearTimeout(notice.timer);
    notice.timer = null;
  }
  notice.show = false;
};

// Dangerous confirmation modal controller
export const requestTypedConfirmation = ({ title, body, typedValue, confirmText, accentText = "Confirmation required" }) =>
  new Promise(resolve => {
    dangerConfirm.title = title;
    dangerConfirm.body = body;
    dangerConfirm.typedValue = typedValue;
    dangerConfirm.confirmText = confirmText;
    dangerConfirm.accentText = accentText;
    dangerConfirm.input = "";
    dangerConfirm.error = "";
    dangerConfirm.resolve = resolve;
    dangerConfirm.show = true;
    document.body.classList.add("overflow-hidden");
  });

export const closeDangerConfirm = result => {
  dangerConfirm.show = false;
  document.body.classList.remove("overflow-hidden");
  if (dangerConfirm.resolve) {
    dangerConfirm.resolve(result);
    dangerConfirm.resolve = null;
  }
};

export const submitDangerConfirm = () => {
  const expected = String(dangerConfirm.typedValue || "").trim().toLowerCase();
  if (String(dangerConfirm.input || "").trim().toLowerCase() !== expected) {
    dangerConfirm.error = `Type ${dangerConfirm.typedValue} to confirm.`;
    return;
  }
  closeDangerConfirm(true);
};

// GPU settings management
export const saveGPUReminderSettings = () => {
  localStorage.setItem(gpuReminderSettingsKey, JSON.stringify(gpuReminderSettings.value));
  window.rancherGpuReminderSettings = gpuReminderSettings.value;
  window.dispatchEvent(new CustomEvent("rancher-control-panel:gpu-reminders", {
    detail: { settings: gpuReminderSettings.value }
  }));
};

export const hideGPUReminderModal = () => {
  gpuReminderModalOpen.value = false;
  document.body.classList.remove("overflow-hidden");
};

export const showGPUReminderModal = clusters => {
  const count = clusters.length;
  const instanceTypes = [...new Set(clusters.map(c => c.gpuWorkerInstanceType).filter(Boolean))];
  const instanceText = instanceTypes.length === 1 ? ` (${instanceTypes[0]})` : "";
  gpuReminderBody.value = count === 1
    ? `Reminder: 1 GPU worker node${instanceText} is active. Are you still using it?`
    : `Reminder: ${count} GPU worker nodes${instanceText} are active. Are you still using them?`;

  gpuReminderSettings.value.lastReminderAt = Date.now();
  saveGPUReminderSettings();
  gpuReminderModalOpen.value = true;
  document.body.classList.add("overflow-hidden");
};

const activeGPUClusters = curState =>
  clusterItems(curState).filter(c => c?.type === "local" && (c.gpuWorkerIp || c.gpuWorkerPrivateIp));

export const maybeShowGPUReminder = curState => {
  const clusters = activeGPUClusters(curState);
  const busy = Boolean(
    curState?.setup?.running ||
    curState?.readiness?.running ||
    curState?.cleanup?.running ||
    cleanupStarting.value ||
    setupLaunchPendingUntil.value > Date.now()
  );
  if (busy) {
    hideGPUReminderModal();
    return;
  }
  if (!clusters.length || gpuReminderSettings.value.disabled || bootPending.value) {
    return;
  }
  if (gpuReminderModalOpen.value) {
    return;
  }
  const intervalMs = gpuReminderSettings.value.intervalMinutes * 60 * 1000;
  if (Date.now() - gpuReminderSettings.value.lastReminderAt < intervalMs) {
    return;
  }
  showGPUReminderModal(clusters);
};

// Leader tracking and cluster highlights
let previousLeaders = new Map();
export const pendingLeaderHighlights = ref(new Map());
export const lastLeaderChangeMessage = ref("");

export const updateLeaderTracking = curState => {
  const messages = [];
  const nextLeaders = new Map();

  clusterItems(curState).forEach(cluster => {
    const pods = Array.isArray(cluster.pods) ? cluster.pods : [];
    const currentLeader = pods.find(pod => pod.leader && pod.leaderLabel === "Leader") || pods.find(pod => pod.leader);
    const currentLeaderName = currentLeader ? currentLeader.name : "";
    const previousLeaderName = previousLeaders.get(cluster.id) || "";

    if (currentLeaderName) {
      nextLeaders.set(cluster.id, currentLeaderName);
    }

    if (currentLeaderName && previousLeaderName && previousLeaderName !== currentLeaderName) {
      pendingLeaderHighlights.value.set(cluster.id, currentLeaderName);
      window.setTimeout(() => {
        if (pendingLeaderHighlights.value.get(cluster.id) === currentLeaderName) {
          pendingLeaderHighlights.value.delete(cluster.id);
        }
      }, 4500);
      messages.push(`${cluster.name} leader changed to ${currentLeaderName}`);
    }
  });

  previousLeaders = nextLeaders;
  lastLeaderChangeMessage.value = messages.join(" • ");
};

// Log viewer helpers
export const logFilename = () => {
  if (logs.mode === "readiness") {
    return `readiness${logs.search ? "-filtered" : ""}.log`;
  }
  if (logs.mode === "setup" || logs.mode === "linodeSetup") {
    return `${logs.mode === "linodeSetup" ? "linode-setup" : "setup"}${logs.search ? "-filtered" : ""}.log`;
  }
  if (logs.mode === "cleanup" || logs.mode === "linodeCleanup") {
    return `${logs.mode === "linodeCleanup" ? "linode-cleanup" : "cleanup"}${logs.search ? "-filtered" : ""}.log`;
  }
  const pod = logs.podName || "pod";
  const safePod = pod.toLowerCase().replace(/[^a-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "") || "pod";
  return `${safePod}-${logs.mode}${logs.search ? "-filtered" : ""}.log`;
};

export const openLogModal = () => {
  logs.show = true;
  document.body.classList.add("overflow-hidden");
  window.setTimeout(() => {
    const logBox = document.getElementById("logBox");
    if (logBox && !logs.search) {
      logBox.scrollTop = logBox.scrollHeight;
    }
  }, 50);
};

export const closeLogModal = () => {
  logs.show = false;
  document.body.classList.remove("overflow-hidden");
  stopStream({ internal: true });
};

export const renderLogViewer = () => {
  const query = logs.search.trim();
  const entries = logs.rawText ? logs.rawText.split("\n").map((line, index) => ({ line, index: index + 1 })) : [];
  const filteredEntries = entries.filter(entry => {
    const queryMatches = query ? entry.line.toLowerCase().includes(query.toLowerCase()) : true;
    return queryMatches && lineMatchesLogLevel(entry.line, logs.level);
  });

  logs.visibleText = filteredEntries.map(entry => entry.line).join("\n");
  const filterLabel = logs.level === "all" ? "" : ` • ${logs.level.toUpperCase()}`;
  logs.matchCountLabel = query || logs.level !== "all"
    ? `${filteredEntries.length} of ${entries.length} lines${filterLabel}`
    : `${entries.length} lines`;

  window.setTimeout(() => {
    const logBox = document.getElementById("logBox");
    if (logBox && !query) {
      logBox.scrollTop = logBox.scrollHeight;
    }
  }, 10);
};

export const setActiveLogContext = (mode, clusterId, namespace, podName) => {
  logs.mode = mode;
  logs.clusterId = clusterId;
  logs.namespace = namespace;
  logs.podName = podName;
};

// Log handlers
let livePollGeneration = 0;
let streamPollTimer = null;

export const fetchPodLogTail = async (clusterId, namespace, podName) => {
  const params = new URLSearchParams({ cluster: clusterId, namespace, pod: podName });
  const response = await apiFetch(`/api/logs?${params.toString()}`);
  const payload = await response.json();
  return payload.text || "";
};

export const fetchDockerLogs = async clusterId => {
  const params = new URLSearchParams({ cluster: clusterId });
  const response = await apiFetch(`/api/docker-logs?${params.toString()}`);
  const payload = await response.json();
  return payload.text || "";
};

export const stopStream = (options = {}) => {
  if (!options.internal && logs.mode === "live" && (logs.liveState === "stopped" || logs.liveState === "error")) {
    streamLogs(logs.clusterId, logs.namespace, logs.podName, { preserveLogs: true });
    return;
  }
  if (streamPollTimer) {
    window.clearInterval(streamPollTimer);
    streamPollTimer = null;
  }
  livePollGeneration += 1;

  if (logs.mode === "live") {
    if (!options.internal) {
      logs.statusText = "Live log refresh stopped.";
      logs.liveState = "stopped";
    }
  }
};

export const streamLogs = (clusterId, namespace, podName, options = {}) => {
  stopStream({ internal: true });
  setActiveLogContext("live", clusterId, namespace, podName);
  logs.liveState = "connecting";
  openLogModal();
  if (!options.preserveLogs) {
    logs.rawText = "";
  }
  renderLogViewer();
  logs.statusText = `Refreshing live logs for ${podName}...`;

  const generation = livePollGeneration;
  const poll = async () => {
    try {
      const text = await fetchPodLogTail(clusterId, namespace, podName);
      if (generation !== livePollGeneration || logs.mode !== "live") {
        return;
      }
      logs.rawText = text;
      logs.liveState = "live";
      renderLogViewer();
      logs.statusText = `Live logs auto-refreshing for ${podName}`;
    } catch (error) {
      if (generation !== livePollGeneration || logs.mode !== "live") {
        return;
      }
      const message = error instanceof Error ? error.message : "Failed to refresh live logs";
      logs.liveState = "error";
      logs.rawText = logs.rawText ? `${logs.rawText}\n[error] ${message}` : `[error] ${message}`;
      renderLogViewer();
      logs.statusText = message;
    }
  };

  poll();
  streamPollTimer = window.setInterval(poll, 3000);
};

export const loadLogs = async (clusterId, namespace, podName) => {
  stopStream({ internal: true });
  setActiveLogContext("tail", clusterId, namespace, podName);
  logs.liveState = "idle";
  openLogModal();
  logs.rawText = "";
  renderLogViewer();
  logs.statusText = `Loading logs for ${podName}...`;

  try {
    logs.rawText = await fetchPodLogTail(clusterId, namespace, podName);
    renderLogViewer();
    logs.statusText = `Showing recent logs for ${podName}`;
  } catch (error) {
    const message = error instanceof Error ? error.message : "Failed to load logs";
    logs.statusText = message;
    logs.rawText = `[error] ${message}`;
    renderLogViewer();
  }
};

export const loadDockerLogs = async cluster => {
  const clusterId = cluster?.id || "";
  if (!clusterId || activeDockerLogsClusterId.value) return;

  stopStream({ internal: true });
  activeDockerLogsClusterId.value = clusterId;
  setActiveLogContext("docker", clusterId, "linode", "rancher");
  logs.liveState = "idle";
  openLogModal();
  logs.rawText = "";
  renderLogViewer();
  logs.statusText = `Loading Docker logs for ${cluster?.name || "Rancher"}...`;

  try {
    logs.rawText = await fetchDockerLogs(clusterId);
    renderLogViewer();
    logs.statusText = "Showing recent Docker logs";
  } catch (error) {
    const message = error instanceof Error ? error.message : "Failed to load Docker logs";
    logs.statusText = message;
    logs.rawText = `[error] ${message}`;
    renderLogViewer();
  } finally {
    activeDockerLogsClusterId.value = "";
  }
};

export const openSetupLogs = (linode = false) => {
  stopStream({ internal: true });
  setActiveLogContext(linode ? "linodeSetup" : "setup", linode ? "linode" : "local", "terratest", "setup");
  const setup = linode ? state.value?.linodeSetup || {} : state.value?.setup || {};
  const output = operationOutput(setup);
  logs.rawText = output.join("\n");
  logs.liveState = setup.running ? (linode ? "linodeSetupRunning" : "setupRunning") : setup.error ? "setupError" : setup.finishedAt ? "setupDone" : "idle";
  renderLogViewer();
  openLogModal();
};

export const openReadinessLogs = () => {
  stopStream({ internal: true });
  setActiveLogContext("readiness", "local", "terratest", "readiness");
  const readiness = state.value?.readiness || {};
  const output = operationOutput(readiness);
  logs.rawText = output.join("\n");
  logs.liveState = readiness.running ? "readinessRunning" : readiness.error ? "readinessError" : readiness.finishedAt ? "readinessDone" : "idle";
  renderLogViewer();
  openLogModal();
};

export const openCleanupLogs = (linode = false) => {
  stopStream({ internal: true });
  setActiveLogContext(linode ? "linodeCleanup" : "cleanup", linode ? "linode" : "local", "terratest", "cleanup");
  const cleanup = linode ? state.value?.linodeCleanup || {} : state.value?.cleanup || {};
  const output = operationOutput(cleanup);
  logs.rawText = output.join("\n");
  logs.liveState = cleanup.running ? (linode ? "linodeCleanupRunning" : "cleanupRunning") : cleanup.error ? "cleanupError" : cleanup.finishedAt ? "cleanupDone" : "idle";
  renderLogViewer();
  openLogModal();
};

export const downloadLogs = () => {
  const text = logs.visibleText || logs.rawText;
  if (!text) {
    logs.statusText = "No logs to download yet.";
    return;
  }
  const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = logFilename();
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
  logs.statusText = `Downloaded ${link.download}`;
};

// Clipboard copy helpers
export const copyTextToClipboard = async (text, successMessage) => {
  if (!navigator.clipboard) {
    refreshStatus.value = "Clipboard access is unavailable in this browser.";
    return false;
  }
  const value = String(text || "").trim();
  if (!value) {
    refreshStatus.value = "No value is available to copy yet.";
    return false;
  }
  try {
    await navigator.clipboard.writeText(value);
    refreshStatus.value = successMessage;
    return true;
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Failed to copy to clipboard.";
    return false;
  }
};

// Path operations
export const openLocalPath = async (rawPath, options = {}) => {
  const path = String(rawPath || "").trim();
  if (!path) {
    refreshStatus.value = "No local path recorded to open.";
    return false;
  }
  try {
    const response = await apiFetch("/api/open-path", {
      method: "POST",
      body: JSON.stringify({ path: rawPath, reveal: Boolean(options.reveal) }),
    });
    refreshStatus.value = options.reveal ? "Revealed local path." : "Opened local folder.";
    return true;
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Failed to open local path.";
    return false;
  }
};

// Kubeconfig, Helm, and Linode actions
export const openKubeconfigFolder = async cluster => {
  const clusterId = cluster?.id || "";
  if (!clusterId || activeOpenKubeconfigPathClusterId.value) return;

  activeOpenKubeconfigPathClusterId.value = clusterId;
  const opened = await openLocalPath(cluster?.kubeconfigPath || "", { reveal: true });
  activeOpenKubeconfigPathClusterId.value = "";
  flashKubeconfigPathAction(clusterId, "open", opened ? "success" : "error");
};

export const copyKubeconfigPath = async cluster => {
  const clusterId = cluster?.id || "";
  if (!clusterId || activeCopyKubeconfigPathClusterId.value) return;

  activeCopyKubeconfigPathClusterId.value = clusterId;
  const copied = await copyTextToClipboard(cluster?.kubeconfigPath || "", "Copied kubeconfig path to clipboard.");
  activeCopyKubeconfigPathClusterId.value = "";
  flashKubeconfigPathAction(clusterId, "copy", copied ? "success" : "error");
};

export const copyLinodeIP = async cluster => {
  const clusterId = cluster?.id || "";
  if (!clusterId || activeCopyLinodeIPClusterId.value) return;

  activeCopyLinodeIPClusterId.value = clusterId;
  const copied = await copyTextToClipboard(cluster?.loadBalancer || "", "Copied Linode IP to clipboard.");
  activeCopyLinodeIPClusterId.value = "";
  flashKubeconfigPathAction(clusterId, "copy-linode-ip", copied ? "success" : "error");
};

export const downloadKubeconfig = async clusterId => {
  if (activeDownloadClusterId.value) return;
  activeDownloadClusterId.value = clusterId;
  refreshStatus.value = "Downloading kubeconfig...";
  try {
    const response = await apiFetch("/api/kubeconfig/save", {
      method: "POST",
      body: JSON.stringify({ cluster: clusterId }),
    });
    const data = await response.json();
    refreshStatus.value = `Saved ${data.filename || "kubeconfig"} to Downloads.`;
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Failed download.";
  } finally {
    activeDownloadClusterId.value = "";
  }
};

export const copyKubeconfig = async clusterId => {
  if (activeCopyClusterId.value) return;
  activeCopyClusterId.value = clusterId;
  refreshStatus.value = "Copying kubeconfig...";

  try {
    const response = await apiFetch(`/api/kubeconfig?cluster=${encodeURIComponent(clusterId)}`, { headers: { Accept: "application/x-yaml" } });
    await navigator.clipboard.writeText(await response.text());
    refreshStatus.value = "Copied kubeconfig to clipboard.";
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Failed to copy kubeconfig.";
  } finally {
    activeCopyClusterId.value = "";
  }
};

export const copyHelmInstallCommand = async (clusterId, mode = "install") => {
  const upgradeMode = mode === "upgrade";
  if (upgradeMode ? activeCopyHelmUpgradeClusterId.value : activeCopyHelmClusterId.value) return;

  if (upgradeMode) {
    activeCopyHelmUpgradeClusterId.value = clusterId;
  } else {
    activeCopyHelmClusterId.value = clusterId;
  }
  refreshStatus.value = upgradeMode ? "Copying prepared Helm upgrade command..." : "Copying Helm install command...";

  try {
    const response = await apiFetch(`/api/helm-command?cluster=${encodeURIComponent(clusterId)}${upgradeMode ? "&mode=upgrade" : ""}`);
    await navigator.clipboard.writeText(await response.text());
    refreshStatus.value = upgradeMode ? "Copied prepared Helm upgrade command to clipboard." : "Copied Helm install command to clipboard.";
    if (upgradeMode) {
      upgradeCommandModalOpen.value = true;
    }
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Failed to copy Helm command.";
  } finally {
    if (upgradeMode) {
      activeCopyHelmUpgradeClusterId.value = "";
    } else {
      activeCopyHelmClusterId.value = "";
    }
  }
};

// Kubeconfig Path Action Feedbacks
export const kubeconfigPathActionFeedback = reactive(new Map());
const kubeconfigPathActionTimers = new Map();

export const flashKubeconfigPathAction = (clusterId, action, status) => {
  const key = `${action}:${clusterId}`;
  window.clearTimeout(kubeconfigPathActionTimers.get(key));
  kubeconfigPathActionFeedback.set(key, status);
  kubeconfigPathActionTimers.set(key, window.setTimeout(() => {
    if (kubeconfigPathActionFeedback.get(key) === status) {
      kubeconfigPathActionFeedback.delete(key);
    }
  }, 1800));
};

// GPU Command copying feedback
export const gpuCommandCopyFeedback = reactive(new Map());
const gpuCommandCopyTimers = new Map();

export const flashGPUCommandCopy = (clusterId, commandIndex, status) => {
  const key = `${clusterId}:${commandIndex}`;
  window.clearTimeout(gpuCommandCopyTimers.get(key));
  gpuCommandCopyFeedback.set(key, status);
  gpuCommandCopyTimers.set(key, window.setTimeout(() => {
    if (gpuCommandCopyFeedback.get(key) === status) {
      gpuCommandCopyFeedback.delete(key);
    }
  }, 1800));
};

// Main operational runners
export const runSetup = async () => {
  if (state.value?.setup?.running) {
    await abortOperation("setup", state.value.setup.runId);
  }
};

export const abortOperation = async (operation, runId = "", options = {}) => {
  if (!options.skipConfirmation) {
    const label = operation === "setup" || operation === "linodeSetup" ? "setup" : operation;
    const confirmed = await requestTypedConfirmation({
      title: `Stop ${label} process?`,
      body: `This asks the local ${label} test process to stop and preserves Terraform state plus the run record. It does not destroy AWS resources.`,
      typedValue: "stop",
      confirmText: "Request stop",
    });
    if (!confirmed) {
      return false;
    }
  }

  pendingAbortOperation.value = operation;
  refreshStatus.value = `Requesting stop for ${operation}...`;

  try {
    await apiFetch("/api/operations/abort", {
      method: "POST",
      body: JSON.stringify({ operation, runId, confirm: "stop" }),
    });
    refreshStatus.value = `Stop requested for ${operation}.`;
    refresh();
    return true;
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Abort request failed.";
    pendingAbortOperation.value = "";
    return false;
  }
};

export const stopOperationThenOpenDestroy = async (operation, runId = "") => {
  const targetRunId = String(runId || "").trim();
  const label = operation === "setup" ? "setup" : "readiness";
  const confirmed = await requestTypedConfirmation({
    title: `Stop ${label}, then open destroy?`,
    body: `This requests a stop for the running ${label} process and moves run ${targetRunId || "this slot"} into the Destroy tab. Terraform destroy still requires its own typed "destroy" confirmation before AWS cleanup starts.`,
    typedValue: "confirm",
    confirmText: "Stop and open destroy",
    accentText: "Stop before destroy",
  });
  if (!confirmed) return;

  const stopped = await abortOperation(operation, targetRunId, { skipConfirmation: true });
  if (!stopped) return;

  selectedCleanupRunId.value = targetRunId;
  setActiveDestroyTab("slots");
  setActivePanelTab("destroy");
  refresh();
};

export const runReadiness = async () => {
  try {
    await apiFetch("/api/readiness", { method: "POST", body: "{}" });
    refreshStatus.value = "Readiness requested...";
    state.value = {
      ...(state.value || {}),
      readiness: {
        ...(state.value?.readiness || {}),
        running: true,
        output: ["[control-panel] Readiness requested..."],
        startedAt: new Date().toISOString(),
      },
    };
    dispatchSetupLifecycleState();
    openReadinessLogs();
    refresh();
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Readiness failed.";
  }
};

export const runCleanup = async (runId = selectedCleanupRunId.value) => {
  const targetRunId = String(runId || "").trim();
  if (!targetRunId) {
    refreshStatus.value = "Select a run before starting destroy.";
    return;
  }

  if (bootPending.value) return;

  const targetRun = (state.value?.workspace?.runs || []).find(run => sameRunKey(run.runId, targetRunId));
  const linodeRun = runIsLinodeDocker(targetRun);
  const destroyBlocked = linodeRun
    ? state.value?.linodeCleanup?.running || state.value?.linodeSetup?.running
    : state.value?.cleanup?.running || state.value?.setup?.running || state.value?.readiness?.running;

  if (cleanupStarting.value || destroyBlocked) {
    return;
  }

  const confirmed = await requestTypedConfirmation({
    title: `Destroy run ${targetRunId}?`,
    body: linodeRun
      ? "This runs Terraform destroy from the selected Linode run state. It deletes the Linode instance and its AWS Route53 record, then removes the run slot only after destroy succeeds."
      : "This runs Terraform destroy from the selected run state. It is intended to delete AWS resources for that run, then remove the run slot only after destroy succeeds.",
    typedValue: "destroy",
    confirmText: "Start destroy",
    accentText: "AWS destroy confirmation",
  });
  if (!confirmed) return;

  selectedCleanupRunId.value = targetRunId;
  dismissedCleanupResultKey.value = "";
  cleanupStarting.value = true;

  try {
    await apiFetch("/api/cleanup", {
      method: "POST",
      body: JSON.stringify({ confirm: "destroy", runId: targetRunId }),
    });
    cleanupStarting.value = false;
    refreshStatus.value = "Destroy requested...";
    const modeKey = linodeRun ? "linodeCleanup" : "cleanup";
    state.value = {
      ...(state.value || {}),
      [modeKey]: {
        ...(state.value?.[modeKey] || {}),
        running: true,
        runId: targetRunId,
        output: ["[control-panel] Destroy requested..."],
        startedAt: new Date().toISOString(),
      },
    };
    dispatchSetupLifecycleState();
    openCleanupLogs(linodeRun);
    refresh();
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Cleanup request failed.";
    cleanupStarting.value = false;
  }
};

export const stopPanel = async () => {
  if (lifecycleRunning.value) {
    refreshStatus.value = "Cannot stop control panel while a run is in progress.";
    return;
  }
  bootPending.value = true;
  refreshStatus.value = "Stopping...";

  try {
    await apiFetch("/api/shutdown", { method: "POST", body: "{}" });
    window.setTimeout(() => window.close(), 250);
  } catch (error) {
    bootPending.value = false;
    refreshStatus.value = error instanceof Error ? error.message : "Stop request failed.";
    refresh();
  }
};

export const resetCostLedger = async () => {
  if (costResetting.value) return;
  if (bootPending.value || lifecycleRunning.value) {
    refreshStatus.value = "Wait for operations to finish before resetting cost history.";
    return;
  }

  const confirmed = await requestTypedConfirmation({
    title: "Reset cost history database?",
    body: "This deletes the local SQLite cost ledger and starts a fresh empty one. It does not destroy AWS resources, remove run slots, or change Terraform state.",
    typedValue: "reset costs",
    confirmText: "Reset cost DB",
    accentText: "Local data reset",
  });
  if (!confirmed) return;

  costResetting.value = true;
  refreshStatus.value = "Resetting local cost ledger...";

  try {
    const response = await apiFetch("/api/costs/reset", {
      method: "POST",
      body: JSON.stringify({ confirm: "reset costs" }),
    });
    const payload = await response.json();
    state.value = {
      ...(state.value || {}),
      costs: payload.costs || { entries: [], totals: {} },
    };
    refreshStatus.value = "Cost history reset. A fresh empty SQLite ledger is ready.";
    refresh();
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Reset costs failed.";
  } finally {
    costResetting.value = false;
  }
};

export const cleanLocalArtifacts = async () => {
  if (localArtifactsCleaning.value) return;
  if (bootPending.value || lifecycleRunning.value) return;

  const runCount = Array.isArray(state.value?.workspace?.runs) ? state.value.workspace.runs.length : 0;
  if (runCount > 0) return;

  const confirmed = await requestTypedConfirmation({
    title: "Clean artifacts after destroy?",
    body: "This backup cleanup removes ignored local run residue only after recorded slots are gone. It keeps cost history and will not destroy AWS resources.",
    typedValue: "clean local artifacts",
    confirmText: "Clean artifacts",
    accentText: "Local cleanup",
  });
  if (!confirmed) return;

  localArtifactsCleaning.value = true;

  try {
    const response = await apiFetch("/api/local-artifacts/clean", {
      method: "POST",
      body: JSON.stringify({ confirm: "clean local artifacts" }),
    });
    const payload = await response.json();
    state.value = {
      ...(state.value || {}),
      workspace: payload.workspace || state.value?.workspace,
      costs: payload.costs || state.value?.costs,
    };
    const removed = Array.isArray(payload.removed) ? payload.removed.length : 0;
    refreshStatus.value = removed
      ? `Cleaned ${removed} local artifact${removed === 1 ? "" : "s"}.`
      : "No local artifacts needed cleaning.";
    refresh();
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Clean artifacts failed.";
  } finally {
    localArtifactsCleaning.value = false;
  }
};

export const refreshPreflight = async () => {
  if (preflightChecking.value) return;
  preflightChecking.value = true;
  window.rancherControlPanelPreflight = preflight.value;
  window.dispatchEvent(new CustomEvent("rancher-control-panel:preflight", { detail: { preflight: preflight.value, checking: true } }));

  try {
    const response = await apiFetch("/api/preflight", { cache: "no-store" });
    preflight.value = await response.json();
  } catch (error) {
    preflight.value = {
      ready: false,
      summary: "Preflight failed",
      items: [{
        name: "Preflight",
        status: "error",
        detail: error instanceof Error ? error.message : "Preflight failed",
      }],
    };
  } finally {
    preflightChecking.value = false;
    window.rancherControlPanelPreflight = preflight.value;
    window.dispatchEvent(new CustomEvent("rancher-control-panel:preflight", { detail: { preflight: preflight.value, checking: false } }));
  }
};

export const fetchState = async () => {
  const response = await apiFetch("/api/state", { cache: "no-store" });
  return response.json();
};

export const refresh = async () => {
  if (refreshInFlight.value) return;
  refreshInFlight.value = true;

  try {
    const fetched = await fetchState();
    if (
      setupLaunchPendingUntil.value > Date.now() &&
      !fetched?.setup?.running &&
      !fetched?.setup?.finishedAt &&
      !fetched?.setup?.error
    ) {
      fetched.setup = {
        ...(fetched.setup || {}),
        running: true,
        output: ["[control-panel] AWS setup accepted. Waiting for lifecycle state to publish the run record..."],
        startedAt: new Date().toISOString(),
      };
    } else if (fetched?.setup?.running || fetched?.setup?.finishedAt || fetched?.setup?.error) {
      setupLaunchPendingUntil.value = 0;
    }

    state.value = fetched;
    window.rancherControlPanelState = fetched;
    window.dispatchEvent(new CustomEvent("rancher-control-panel:state", {
      detail: {
        state: fetched,
        bootPending: false,
        refreshedAt: new Date().toISOString(),
      },
    }));

    dispatchSetupLifecycleState();

    if (pendingAbortOperation.value && !fetched?.[pendingAbortOperation.value]?.running) {
      pendingAbortOperation.value = "";
    }
    if (cleanupStarting.value && (fetched?.cleanup?.running || fetched?.linodeCleanup?.running)) {
      cleanupStarting.value = false;
    }
    if (bootPending.value) {
      bootPending.value = false;
    }

    updateLeaderTracking(fetched);
    maybeShowGPUReminder(fetched);

    refreshStatus.value = lastLeaderChangeMessage.value
      ? `${lastLeaderChangeMessage.value} • ${new Date().toLocaleTimeString()}`
      : `Last refreshed at ${new Date().toLocaleTimeString()}`;
  } catch (error) {
    refreshStatus.value = error instanceof Error ? error.message : "Refresh failed";
  } finally {
    refreshInFlight.value = false;
  }
};

// Initial state updates and DOM synchronizations
watch(bootPending, pending => {
  document.body.dataset.booting = pending ? "true" : "false";
  dispatchSetupRootEvent("rancher-control-panel-booting", {
    booting: pending,
    detail: bootDetail.value,
  });
}, { immediate: true });

watch(activeTab, tab => {
  const setupEl = document.getElementById("setupTabPanel") || document.querySelector('[data-tab-panel="setup"]');
  if (setupEl) {
    setupEl.classList.toggle("hidden", tab !== "setup");
  }
}, { immediate: true });

// Setup event listeners
export const initStore = () => {
  setTheme(theme.value, false);
  syncFullscreenButton();
  document.addEventListener("fullscreenchange", syncFullscreenButton);

  window.addEventListener("rancher-setup-started", () => {
    const now = new Date().toISOString();
    setupLaunchPendingUntil.value = Date.now() + 15000;
    state.value = {
      ...(state.value || {}),
      setup: {
        ...(state.value?.setup || {}),
        running: true,
        output: ["[control-panel] AWS setup accepted. Waiting for lifecycle state to publish the run record..."],
        startedAt: now,
      },
    };
    dispatchSetupLifecycleState();
    refreshStatus.value = "AWS setup accepted. Waiting for run state to appear...";
    setActivePanelTab("runs");
    refresh();
  });

  window.setInterval(refresh, 5000);
  refreshPreflight();
  refresh();
};

initStore();
