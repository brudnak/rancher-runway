<template>
  <header class="panel-header mb-5 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
    <div class="min-w-0">
      <ControlPanelHeader />
      <div v-if="bootPending" class="mt-4 max-w-4xl rounded-xl border border-sky-200 bg-white px-4 py-3 text-sm text-sky-900 shadow-sm dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start">
          <span class="spinner mt-0.5 shrink-0 text-sky-600 dark:text-sky-300"></span>
          <div class="min-w-0">
            <div class="font-semibold">Startup safety check running</div>
            <div class="mt-1 leading-6 text-sky-800/80 dark:text-sky-100/75">
              {{ bootDetail }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="chrome-toolbar flex shrink-0 flex-wrap">
      <button
        type="button"
        @click="setPanelFullscreen(!fullscreen)"
        class="chrome-button inline-flex items-center justify-center gap-2"
        aria-label="Toggle fullscreen"
        :title="fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'"
        :aria-pressed="fullscreen ? 'true' : 'false'"
      >
        <!-- Fullscreen Enter Icon -->
        <svg v-if="!fullscreen" xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M8 3H5a2 2 0 0 0-2 2v3"></path>
          <path d="M21 8V5a2 2 0 0 0-2-2h-3"></path>
          <path d="M3 16v3a2 2 0 0 0 2 2h3"></path>
          <path d="M16 21h3a2 2 0 0 0 2-2v-3"></path>
        </svg>
        <!-- Fullscreen Exit Icon -->
        <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M8 3v3a2 2 0 0 1-2 2H3"></path>
          <path d="M21 8h-3a2 2 0 0 1-2-2V3"></path>
          <path d="M3 16h3a2 2 0 0 1 2 2v3"></path>
          <path d="M16 21v-3a2 2 0 0 1 2-2h3"></path>
        </svg>
        <span>{{ fullscreen ? 'Exit full screen' : 'Fullscreen' }}</span>
      </button>

      <button
        type="button"
        @click="setTheme(theme === 'dark' ? 'light' : 'dark')"
        class="chrome-button inline-flex items-center justify-center gap-2"
        aria-label="Toggle color theme"
      >
        <!-- Sun Icon -->
        <svg v-if="theme === 'dark'" xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="4"></circle>
          <path d="M12 2v2"></path>
          <path d="M12 20v2"></path>
          <path d="m4.93 4.93 1.41 1.41"></path>
          <path d="m17.66 17.66 1.41 1.41"></path>
          <path d="M2 12h2"></path>
          <path d="M20 12h2"></path>
          <path d="m6.34 17.66-1.41 1.41"></path>
          <path d="m19.07 4.93-1.41 1.41"></path>
        </svg>
        <!-- Moon Icon -->
        <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 3a6 6 0 0 0 9 7.5A9 9 0 1 1 12 3Z"></path>
        </svg>
        <span>{{ theme === 'dark' ? 'Light' : 'Dark' }}</span>
      </button>

      <button
        type="button"
        @click="refresh"
        class="chrome-button chrome-button-primary"
      >
        Refresh
      </button>

      <button
        type="button"
        @click="stopPanel"
        :disabled="stopBtnDisabled"
        :title="stopBtnTitle"
        class="chrome-button"
        :class="stopBtnClass"
      >
        {{ stopBtnText }}
      </button>
    </div>
  </header>
</template>

<script setup>
import { computed } from "vue";
import ControlPanelHeader from "./ControlPanelHeader.vue";
import {
  bootPending,
  bootDetail,
  fullscreen,
  theme,
  setTheme,
  setPanelFullscreen,
  refresh,
  stopPanel,
  lifecycleRunning,
} from "./store.js";

const stopBtnDisabled = computed(() => bootPending.value || lifecycleRunning.value);

const stopBtnText = computed(() => {
  if (bootPending.value) {
    return 'Checking state';
  }
  if (lifecycleRunning.value) {
    return 'Run in progress';
  }
  return 'Stop panel';
});

const stopBtnTitle = computed(() => {
  if (bootPending.value) {
    return 'Startup safety check is still loading panel state.';
  }
  if (lifecycleRunning.value) {
    return 'Setup, readiness, or destroy is running. Leave the panel open until it finishes.';
  }
  return 'Stop the local control panel.';
});

const stopBtnClass = computed(() => {
  if (lifecycleRunning.value) {
    return 'opacity-55 cursor-not-allowed';
  }
  return '';
});
</script>
