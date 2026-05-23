<template>
  <header class="panel-header mb-5 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
    <div class="min-w-0">
      <ControlPanelHeader />
      <div id="bootStatus" class="mt-4 max-w-4xl rounded-xl border border-sky-200 bg-white px-4 py-3 text-sm text-sky-900 shadow-sm dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start">
          <span class="spinner mt-0.5 shrink-0 text-sky-600 dark:text-sky-300"></span>
          <div class="min-w-0">
            <div class="font-semibold">Startup safety check running</div>
            <div id="bootStatusDetail" class="mt-1 leading-6 text-sky-800/80 dark:text-sky-100/75">
              Checking local config, run slots, Terraform state, lifecycle processes, clusters, and AWS inventory before enabling actions.
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="chrome-toolbar flex shrink-0 flex-wrap">
      <button id="fullscreenToggle" type="button" class="chrome-button inline-flex items-center justify-center gap-2" aria-label="Toggle fullscreen" title="Toggle fullscreen">
        <svg id="fullscreenEnterIcon" xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M8 3H5a2 2 0 0 0-2 2v3"></path>
          <path d="M21 8V5a2 2 0 0 0-2-2h-3"></path>
          <path d="M3 16v3a2 2 0 0 0 2 2h3"></path>
          <path d="M16 21h3a2 2 0 0 0 2-2v-3"></path>
        </svg>
        <svg id="fullscreenExitIcon" xmlns="http://www.w3.org/2000/svg" class="hidden h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M8 3v3a2 2 0 0 1-2 2H3"></path>
          <path d="M21 8h-3a2 2 0 0 1-2-2V3"></path>
          <path d="M3 16h3a2 2 0 0 1 2 2v3"></path>
          <path d="M16 21v-3a2 2 0 0 1 2-2h3"></path>
        </svg>
        <span id="fullscreenToggleLabel">Fullscreen</span>
      </button>

      <button id="themeToggle" type="button" class="chrome-button inline-flex items-center justify-center gap-2" aria-label="Toggle color theme">
        <svg id="themeSunIcon" xmlns="http://www.w3.org/2000/svg" class="hidden h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
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
        <svg id="themeMoonIcon" xmlns="http://www.w3.org/2000/svg" class="hidden h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 3a6 6 0 0 0 9 7.5A9 9 0 1 1 12 3Z"></path>
        </svg>
        <span id="themeToggleLabel">Theme</span>
      </button>

      <button id="refreshBtn" type="button" class="chrome-button chrome-button-primary">Refresh</button>
      <button id="stopBtn" type="button" class="chrome-button">Stop panel</button>
    </div>
  </header>
</template>

<script setup>
import ControlPanelHeader from "./ControlPanelHeader.vue";
</script>
