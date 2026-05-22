<template>
  <iframe
    id="panelFrame"
    title="Rancher Runway Control Panel"
    :hidden="!panelVisible"
    :src="panelURL || undefined"
    @load="handlePanelLoad"
  ></iframe>

  <main v-if="!panelVisible" id="loadingShell" class="shell">
    <section class="mark" aria-hidden="true">
      <span class="mark-ring"></span>
    </section>
    <section class="copy">
      <p class="eyebrow">Rancher Runway</p>
      <h1>Opening the local control panel</h1>
      <p class="build-badge" :title="buildBadgeTitle">{{ buildBadgeText }}</p>
      <p id="status" :data-error="statusError ? 'true' : 'false'">{{ statusMessage }}</p>
      <button v-if="retryVisible" class="retry-button" type="button" @click="attachPanel">
        Try again
      </button>
    </section>
  </main>
</template>

<script setup>
import { computed, onMounted, ref } from "vue";

const initialStatus = "Starting the local Go panel and attaching this native window.";

const statusMessage = ref(initialStatus);
const statusError = ref(false);
const retryVisible = ref(false);
const panelURL = ref("");
const panelVisible = ref(false);
const build = ref(null);

const buildBadgeText = computed(() => {
  const shortCommit = String(build.value?.commitShort || "").trim();
  const modified = Boolean(build.value?.modified);
  return shortCommit ? `Build ${shortCommit}${modified ? "*" : ""}` : "Build unknown";
});

const buildBadgeTitle = computed(() => {
  const fullCommit = String(build.value?.commit || "").trim();
  const buildDate = String(build.value?.buildDate || "").trim();
  const modified = Boolean(build.value?.modified);
  const titleParts = [];

  if (fullCommit) {
    titleParts.push(`Commit: ${fullCommit}`);
  }
  if (buildDate) {
    titleParts.push(`Built: ${buildDate}`);
  }
  if (modified) {
    titleParts.push("Working tree had local changes when this binary was built.");
  }

  return titleParts.length ? titleParts.join("\n") : "No build commit was embedded in this binary.";
});

const setStatus = (message, error = false) => {
  statusMessage.value = message;
  statusError.value = error;
};

const browserSystemTheme = () => (
  window.matchMedia?.("(prefers-color-scheme: dark)")?.matches ? "dark" : "light"
);

const nativeSystemTheme = async () => {
  try {
    const theme = await window.go?.main?.App?.SystemTheme?.();
    if (theme === "dark" || theme === "light") {
      return theme;
    }
  } catch (_) {
    // Fall back to browser media detection below.
  }
  return browserSystemTheme();
};

const panelURLWithSystemTheme = async url => {
  if (localStorage.getItem("rancherControlPanelTheme")) {
    return url;
  }

  const urlWithTheme = new URL(url, window.location.href);
  urlWithTheme.searchParams.set("systemTheme", await nativeSystemTheme());
  return urlWithTheme.toString();
};

const waitForPanelStatus = async () => {
  for (let attempt = 0; attempt < 120; attempt += 1) {
    const panelStatus = window.go?.main?.App?.PanelStatus;
    if (panelStatus) {
      return panelStatus;
    }
    await new Promise(resolve => window.setTimeout(resolve, 100));
  }
  throw new Error("Wails did not expose the Rancher Runway panel bridge.");
};

const handlePanelLoad = () => {
  if (panelURL.value) {
    panelVisible.value = true;
  }
};

const attachPanel = async () => {
  try {
    retryVisible.value = false;
    panelVisible.value = false;
    panelURL.value = "";
    setStatus(initialStatus);

    const panelStatus = await waitForPanelStatus();
    const result = await panelStatus();
    build.value = result?.build || null;

    if (result?.error) {
      throw new Error(result.error);
    }
    if (!result?.url) {
      throw new Error("The local control panel did not return a URL.");
    }

    panelURL.value = await panelURLWithSystemTheme(result.url);
    setStatus("Opening the control panel.");
  } catch (error) {
    setStatus(error instanceof Error ? error.message : String(error), true);
    retryVisible.value = true;
  }
};

onMounted(() => {
  void attachPanel();
});
</script>
