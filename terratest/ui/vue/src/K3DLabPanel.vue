<template>
  <div class="grid gap-6">
    <div class="fixed bottom-5 right-5 z-[9999] flex max-w-sm flex-col gap-2.5 pointer-events-none">
      <transition-group name="toast-fade">
        <div
          v-for="toast in toasts"
          :key="toast.id"
          class="pointer-events-auto flex items-start gap-3 rounded-xl border p-4 shadow-lg backdrop-blur-md transition-all duration-300"
          :class="{
            'border-emerald-200 bg-emerald-50/95 text-emerald-900 dark:border-emerald-500/20 dark:bg-emerald-950/95 dark:text-emerald-200': toast.kind === 'success',
            'border-rose-200 bg-rose-50/95 text-rose-900 dark:border-rose-500/20 dark:bg-rose-950/95 dark:text-rose-200': toast.kind === 'error',
            'border-amber-200 bg-amber-50/95 text-amber-900 dark:border-amber-500/20 dark:bg-amber-950/95 dark:text-amber-200': toast.kind === 'warning',
            'border-zinc-200 bg-white/95 text-zinc-900 dark:border-white/10 dark:bg-zinc-900/95 dark:text-zinc-50': toast.kind === 'info'
          }"
        >
          <span class="mt-0.5 shrink-0">
            <svg v-if="toast.kind === 'success'" xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-emerald-600 dark:text-emerald-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <svg v-else-if="toast.kind === 'error'" xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-rose-600 dark:text-rose-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <svg v-else-if="toast.kind === 'warning'" xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-amber-600 dark:text-amber-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-sky-600 dark:text-sky-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </span>
          <div class="flex-1 text-sm font-semibold leading-5">{{ toast.message }}</div>
        </div>
      </transition-group>
    </div>

    <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0">
        <div class="inline-flex items-center rounded-full border border-emerald-200 bg-emerald-50/50 px-3 py-1 text-xs font-semibold text-emerald-700 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-300">
          Local K3d Sandbox Environment
        </div>
        <h2 class="mt-3 text-2xl font-bold tracking-tight text-zinc-950 dark:text-zinc-50">K3D Lab</h2>
        <p class="mt-1.5 max-w-3xl text-sm leading-6 text-zinc-500 dark:text-zinc-400">
          Start local k3d clusters, keep Kubernetes API endpoints available, and use their kubeconfigs for focused local testing.
        </p>
      </div>

      <div class="grid shrink-0 gap-3 sm:grid-cols-3 lg:min-w-[30rem]">
        <div
          class="relative flex flex-col gap-1 overflow-hidden rounded-2xl border border-zinc-200/80 bg-white/70 p-4 shadow-2xs backdrop-blur-md transition-all duration-200 hover:scale-[1.01] dark:border-white/5 dark:bg-zinc-900/60"
          :class="preflight.ready ? 'border-l-4 border-l-emerald-500' : 'border-l-4 border-l-rose-500'"
        >
          <span class="text-[10px] font-extrabold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Local Tools</span>
          <div class="mt-0.5 flex items-center gap-1.5 text-base font-bold text-zinc-800 dark:text-zinc-100">
            <span class="h-2 w-2 rounded-full" :class="preflight.ready ? 'bg-emerald-500' : 'bg-rose-500'"></span>
            <span>{{ preflight.ready ? "Ready" : "Blocked" }}</span>
          </div>
          <span class="mt-1 truncate text-xs text-zinc-500 dark:text-zinc-400" :title="preflight.summary">{{ preflight.summary || "Checking Docker & k3d" }}</span>
          <div class="mt-2 flex flex-wrap gap-3">
            <button type="button" class="inline-flex items-center gap-1 text-[11px] font-bold text-sky-600 hover:underline dark:text-sky-400" @click="refreshState">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 1121.21 7.89H18" />
              </svg>
              <span>Refresh tools</span>
            </button>
            <button
              v-if="missingK3D"
              type="button"
              class="inline-flex items-center gap-1 text-[11px] font-bold text-emerald-600 hover:underline disabled:opacity-50 dark:text-emerald-400"
              :disabled="operation.running || installing"
              @click="installK3D"
            >
              <span>{{ installing ? "Installing..." : "Install k3d" }}</span>
            </button>
          </div>
        </div>

        <div
          class="relative flex flex-col gap-1 overflow-hidden rounded-2xl border border-zinc-200/80 bg-white/70 p-4 shadow-2xs backdrop-blur-md transition-all duration-200 hover:scale-[1.01] dark:border-white/5 dark:bg-zinc-900/60"
          :class="operation.running ? 'border-l-4 border-l-sky-500' : ''"
        >
          <span class="text-[10px] font-extrabold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Startup Status</span>
          <div class="mt-0.5 flex items-center gap-1.5 text-base font-bold text-zinc-800 dark:text-zinc-100">
            <span v-if="operation.running" class="relative flex h-2 w-2">
              <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-sky-400 opacity-75"></span>
              <span class="relative inline-flex h-2 w-2 rounded-full bg-sky-500"></span>
            </span>
            <span v-else class="h-2 w-2 rounded-full bg-zinc-300 dark:bg-zinc-600"></span>
            <span>{{ operation.running ? "Running" : "Idle" }}</span>
          </div>
          <span class="mt-1 truncate text-xs text-zinc-500 dark:text-zinc-400" :title="operation.runId || actionSummary">{{ actionSummary }}</span>
        </div>

        <div
          class="relative flex flex-col gap-1 overflow-hidden rounded-2xl border border-zinc-200/80 bg-white/70 p-4 shadow-2xs backdrop-blur-md transition-all duration-200 hover:scale-[1.01] dark:border-white/5 dark:bg-zinc-900/60"
          :class="runningClusters.length ? 'border-l-4 border-l-emerald-500' : ''"
        >
          <span class="text-[10px] font-extrabold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Active Clusters</span>
          <div class="mt-0.5 flex items-center gap-1.5 text-base font-bold text-zinc-800 dark:text-zinc-100">
            <span v-if="runningClusters.length" class="h-2 w-2 animate-pulse rounded-full bg-emerald-500"></span>
            <span v-else class="h-2 w-2 rounded-full bg-zinc-300 dark:bg-zinc-600"></span>
            <span>{{ runningClusters.length }}</span>
          </div>
          <span class="mt-1 truncate text-xs text-zinc-500 dark:text-zinc-400" :title="clusterSummary">{{ clusterSummary }}</span>
        </div>
      </div>
    </div>

    <div
      v-if="preflightItems.length"
      class="overflow-hidden rounded-2xl border border-zinc-200 bg-zinc-50 dark:border-white/10 dark:bg-white/[0.02]"
    >
      <button
        type="button"
        class="flex w-full items-center justify-between gap-3 px-4 py-3.5 text-left outline-none hover:bg-zinc-100/50 dark:hover:bg-white/[0.02]"
        @click="preflightCollapsed = !preflightCollapsed"
      >
        <div class="flex items-center gap-3">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
          </svg>
          <span class="text-sm font-bold text-zinc-900 dark:text-zinc-50">Local Tools Preflight Checklist</span>
          <div :class="preflightStatusClass">{{ preflightStatusLabel }}</div>
        </div>
        <svg
          xmlns="http://www.w3.org/2000/svg"
          class="h-4 w-4 text-zinc-400 transition-transform duration-200"
          :class="{ 'rotate-180': !preflightCollapsed }"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2.5"
        >
          <path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      <div v-show="!preflightCollapsed" class="grid gap-3 border-t border-zinc-200/60 p-4 text-sm dark:border-white/5 sm:grid-cols-2 lg:grid-cols-5">
        <div
          v-for="item in preflightItems"
          :key="item.name"
          class="rounded-xl border px-3.5 py-3 shadow-2xs transition-colors duration-150"
          :class="preflightItemClass(item.status)"
        >
          <div class="flex items-center justify-between gap-3">
            <span class="min-w-0 truncate font-semibold">{{ item.name }}</span>
            <span class="shrink-0 text-[10px] font-bold uppercase tracking-wide opacity-80">{{ item.status || "unknown" }}</span>
          </div>
          <div class="mt-1.5 truncate text-xs leading-5 opacity-90" :title="item.detail">{{ item.detail || "Installed & ready." }}</div>
        </div>
      </div>
    </div>

    <div class="grid gap-5 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)]">
      <section class="flex flex-col justify-between rounded-2xl border border-zinc-200/80 bg-zinc-50/50 p-5 shadow-2xs dark:border-white/10 dark:bg-white/[0.02]">
        <div>
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div class="flex items-center gap-2">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14M5 12a2 2 0 012-2h2a2 2 0 012 2m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2V12" />
              </svg>
              <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-50">{{ runningClusters.length ? "Start Another K3D Cluster" : "Start K3D Cluster" }}</h3>
            </div>
          </div>
          <p class="mt-1 text-sm text-zinc-500 dark:text-zinc-400">Each start creates a separate local cluster with its own Kubernetes API port.</p>

          <div class="mt-4 grid gap-4">
            <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
              <span>K3s image tag</span>
              <select
                v-model="k3sVersion"
                :disabled="inputsDisabled"
                class="w-full rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 disabled:cursor-not-allowed disabled:bg-zinc-50 disabled:opacity-60 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:disabled:bg-zinc-900/50"
              >
                <option v-for="version in k3sOptions" :key="version" :value="version">{{ version }}</option>
              </select>
            </label>

            <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
              <span>API port binding</span>
              <input
                v-model.number="apiPort"
                type="number"
                min="1024"
                max="65535"
                placeholder="Auto"
                :disabled="inputsDisabled"
                class="w-full rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-semibold text-zinc-900 outline-none placeholder:text-zinc-400 focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 disabled:cursor-not-allowed disabled:bg-zinc-50 disabled:opacity-60 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500 dark:disabled:bg-zinc-900/50"
              />
            </label>
          </div>
        </div>

        <div class="mt-4 rounded-xl border border-sky-100 bg-sky-500/5 p-3.5 text-xs leading-5 text-sky-900 shadow-3xs dark:border-sky-500/10 dark:bg-sky-500/10 dark:text-sky-200">
          <div class="flex items-center gap-1.5 font-bold">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-4.5 w-4.5 text-sky-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span>Side-by-side local clusters</span>
          </div>
          <div class="mt-1 pl-5 font-medium opacity-90">Leave API port on Auto unless a workflow needs a fixed endpoint.</div>
        </div>
      </section>

      <section class="flex flex-col justify-between rounded-2xl border border-zinc-200/80 bg-zinc-50/50 p-5 shadow-2xs dark:border-white/10 dark:bg-white/[0.02]">
        <div>
          <div class="flex items-center gap-2">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
            <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-50">Lab Controls</h3>
          </div>
          <p class="mt-1 text-sm text-zinc-500 dark:text-zinc-400">Start a cluster, stop a running action, or install k3d when the local tool check reports it missing.</p>
        </div>

        <div class="mt-4 space-y-3">
          <div v-if="!preflight.ready && preflightItems.length" class="rounded-xl border border-rose-300 bg-rose-500/5 p-3.5 text-xs leading-5 text-rose-900 dark:border-rose-500/15 dark:bg-rose-950/20 dark:text-rose-200">
            <div class="flex items-center gap-1.5 font-bold">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-4.5 w-4.5 text-rose-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              <span>Docker or required utilities are offline</span>
            </div>
            <ul class="mt-1.5 list-disc space-y-0.5 pl-5 font-medium opacity-90">
              <li v-for="item in preflightItems.filter(i => i.status === 'error')" :key="item.name">
                <strong>{{ item.name }}:</strong> {{ item.detail || "Not running." }}
              </li>
            </ul>
          </div>

          <div class="flex flex-wrap gap-2 border-t border-zinc-200/60 pt-1.5 dark:border-white/5">
            <button
              type="button"
              class="inline-flex min-h-10 items-center justify-center rounded-xl px-4 py-2 text-sm font-bold shadow-md transition-all"
              :class="preflight.ready ? 'bg-emerald-500 text-white shadow-emerald-500/10 hover:bg-emerald-600' : 'border border-rose-500/20 bg-rose-500/10 text-rose-500 hover:bg-rose-500/15'"
              :disabled="startDisabled"
              @click="startCluster"
            >
              <svg v-if="operation.running" class="-ml-1 mr-2 h-4 w-4 animate-spin text-current" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <svg v-else xmlns="http://www.w3.org/2000/svg" class="-ml-1 mr-2 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
              <span>{{ startButtonLabel }}</span>
            </button>

            <button
              type="button"
              class="inline-flex min-h-10 items-center justify-center rounded-xl border px-4 py-2 text-sm font-bold shadow-sm transition-all"
              :class="operation.running ? 'border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100 dark:border-rose-500/30 dark:bg-rose-950/20 dark:text-rose-300' : 'cursor-not-allowed border-zinc-200 bg-white text-zinc-400 dark:border-white/5 dark:bg-white/[0.04]'"
              :disabled="!operation.running || stopping"
              @click="stopAction"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="mr-2 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
              </svg>
              <span>{{ stopping ? "Stopping..." : "Stop action" }}</span>
            </button>
          </div>

          <p v-if="notice" class="mt-1 rounded-lg bg-zinc-200/30 p-2 text-xs font-bold leading-5 transition-all dark:bg-zinc-950/50" :class="noticeTone">{{ notice }}</p>
        </div>
      </section>
    </div>

    <section id="k3dTerminalConsole" class="overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-md dark:border-zinc-800 dark:bg-zinc-950">
      <div class="flex flex-col gap-2.5 border-b border-zinc-200 bg-zinc-50 px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900/90 sm:flex-row sm:items-center sm:justify-between">
        <div class="flex items-center gap-3">
          <div class="flex shrink-0 gap-1.5">
            <span class="h-3 w-3 rounded-full bg-rose-500/80"></span>
            <span class="h-3 w-3 rounded-full bg-amber-500/80"></span>
            <span class="h-3 w-3 rounded-full bg-emerald-500/80"></span>
          </div>
          <h3 class="flex items-center gap-2 truncate font-mono text-xs font-semibold text-zinc-700 dark:text-zinc-300">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 text-zinc-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M8 9l3 3-3 3m5 0h3" />
            </svg>
            <span>{{ operation.command ? "k3d-lab.sh" : "local-console" }}</span>
            <span v-if="operation.running" class="inline-flex h-1.5 w-1.5 animate-ping rounded-full bg-emerald-500"></span>
          </h3>
        </div>
        <div class="flex flex-wrap gap-1.5">
          <button type="button" class="inline-flex items-center gap-1 rounded-md border border-zinc-200 bg-white px-2.5 py-1 text-[10px] font-bold text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-40 dark:border-zinc-700/60 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700" @click="refreshState">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 1121.21 7.89H18" />
            </svg>
            <span>Refresh</span>
          </button>
          <button type="button" class="inline-flex items-center gap-1 rounded-md border border-zinc-200 bg-white px-2.5 py-1 text-[10px] font-bold text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-40 dark:border-zinc-700/60 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700" :disabled="!hasOutput" @click="outputCollapsed = !outputCollapsed">
            <svg v-if="outputCollapsed" xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
            <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7" />
            </svg>
            <span>{{ outputCollapsed ? "Expand" : "Collapse" }}</span>
          </button>
          <button type="button" class="inline-flex items-center gap-1 rounded-md border border-zinc-200 bg-white px-2.5 py-1 text-[10px] font-bold text-zinc-700 transition-colors hover:bg-zinc-50 disabled:opacity-40 dark:border-zinc-700/60 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700" :disabled="clearDisabled" @click="clearOutput">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            <span>Clear console</span>
          </button>
        </div>
      </div>

      <div v-if="outputCollapsed" class="border-t border-zinc-200/50 bg-zinc-50 p-5 font-mono text-xs text-zinc-500 dark:border-zinc-900/40 dark:bg-zinc-950">
        Terminal output collapsed. {{ outputLineCount }} log statement{{ outputLineCount === 1 ? "" : "s" }} parsed.
      </div>
      <div v-else class="relative border-t border-zinc-200/50 bg-zinc-50 p-4 font-mono text-[11px] leading-5 text-zinc-800 dark:border-zinc-900/40 dark:bg-zinc-950 dark:text-zinc-300">
        <pre class="max-h-[22rem] max-w-full overflow-auto whitespace-pre-wrap break-words pr-4 text-zinc-800 dark:text-zinc-300">{{ outputText }}<span v-if="operation.running" class="ml-0.5 inline-block h-3 w-1.5 animate-pulse bg-emerald-500 align-middle"></span></pre>
      </div>
    </section>

    <section class="grid gap-3">
      <h3 class="flex items-center gap-2 text-base font-bold text-zinc-900 dark:text-zinc-50">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>K3D Lab Cluster History</span>
      </h3>

      <div
        v-for="cluster in clusters"
        :key="cluster.runId"
        class="relative flex flex-col gap-5 overflow-hidden rounded-2xl border bg-white p-5 shadow-2xs transition-all duration-200 hover:shadow-sm dark:bg-zinc-900/40"
        :class="clusterCardClass(cluster)"
      >
        <div class="flex items-center justify-between border-b border-zinc-200/50 pb-3.5 dark:border-white/5">
          <div class="flex min-w-0 items-center gap-3">
            <span class="relative flex h-2.5 w-2.5 shrink-0">
              <span
                v-if="['running', 'creating'].includes(cluster.status)"
                class="absolute inline-flex h-full w-full animate-ping rounded-full opacity-75"
                :class="cluster.status === 'running' ? 'bg-emerald-400' : 'bg-sky-400'"
              ></span>
              <span class="relative inline-flex h-2.5 w-2.5 rounded-full" :class="clusterDotClass(cluster.status)"></span>
            </span>
            <h4 class="truncate font-mono text-sm font-bold tracking-tight text-zinc-900 dark:text-zinc-50">{{ cluster.runId }}</h4>
            <span class="inline-flex items-center rounded-full border px-2.5 py-0.5 text-[10px] font-bold uppercase tracking-wider" :class="clusterStatusBadgeClass(cluster.status)">
              {{ cluster.status }}
            </span>
          </div>
          <div v-if="cluster.apiPort" class="flex items-center gap-1.5 rounded-full border border-zinc-200/60 bg-zinc-50 px-2.5 py-1 text-xs font-semibold text-zinc-500 dark:border-white/5 dark:bg-zinc-950 dark:text-zinc-400">
            <span class="inline-block h-1.5 w-1.5 rounded-full bg-emerald-500"></span>
            <span>API {{ cluster.apiPort }}</span>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 rounded-xl border border-zinc-200/60 bg-zinc-100/50 p-4 dark:border-white/5 dark:bg-zinc-950/40 sm:grid-cols-2 lg:grid-cols-4">
          <div class="flex min-w-0 flex-col sm:col-span-2">
            <span class="text-[10px] font-bold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Endpoint URL</span>
            <div class="mt-1 flex min-w-0 items-center gap-2">
              <span v-if="cluster.apiUrl" class="block truncate text-sm font-semibold text-sky-600 dark:text-sky-400" :title="cluster.apiUrl">{{ cluster.apiUrl }}</span>
              <span v-else class="block truncate text-sm font-semibold text-zinc-400 dark:text-zinc-600">not ready</span>
              <button
                v-if="cluster.apiUrl"
                type="button"
                class="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:bg-zinc-200/50 hover:text-zinc-700 dark:hover:bg-zinc-800/80 dark:hover:text-zinc-200"
                title="Copy endpoint"
                @click="copyText(cluster.apiUrl, 'Endpoint copied.')"
              >
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                  <path d="M8 3a1 1 0 011-1h2a1 1 0 110 2H9a1 1 0 01-1-1z" />
                  <path d="M6 3a2 2 0 00-2 2v11a2 2 0 002 2h8a2 2 0 002-2V5a2 2 0 00-2-2 3 3 0 01-3 3H9a3 3 0 01-3-3z" />
                </svg>
              </button>
            </div>
          </div>

          <div class="flex min-w-0 flex-col">
            <span class="text-[10px] font-bold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">K3s version</span>
            <span class="mt-1 truncate text-sm font-semibold text-zinc-800 dark:text-zinc-200" :title="cluster.k3sVersion">{{ cluster.k3sVersion }}</span>
          </div>

          <div class="flex min-w-0 flex-col">
            <span class="text-[10px] font-bold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Cluster name</span>
            <span class="mt-1 truncate text-sm font-semibold text-zinc-800 dark:text-zinc-200" :title="cluster.clusterName">{{ cluster.clusterName }}</span>
          </div>
        </div>

        <div class="flex flex-col gap-4 rounded-xl border border-zinc-200/60 bg-zinc-50/30 p-4 dark:border-white/5 dark:bg-white/[0.005]">
          <div class="text-[10px] font-extrabold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Resource Files</div>
          <div class="grid gap-3.5">
            <div class="flex flex-col gap-2 rounded-xl border border-zinc-200/60 bg-white p-3.5 shadow-2xs dark:border-white/5 dark:bg-zinc-900/50">
              <div class="flex items-center justify-between gap-3">
                <div class="flex min-w-0 items-center gap-2">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 shrink-0 text-zinc-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                  </svg>
                  <span class="text-xs font-bold text-zinc-700 dark:text-zinc-200">Kubeconfig configuration</span>
                </div>
                <div class="flex shrink-0 items-center gap-1.5">
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 rounded border border-zinc-200 bg-white px-2.5 py-1 text-[11px] font-semibold text-zinc-700 shadow-2xs transition-colors hover:bg-zinc-50 disabled:opacity-50 dark:border-white/10 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:bg-zinc-800"
                    :disabled="!cluster.kubeconfig"
                    @click="openKubeconfigFolder(cluster)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 19a2 2 0 01-2-2V7a2 2 0 012-2h4l2 2h4a2 2 0 012 2v1M5 19h14a2 2 0 002-2v-5M5 19V9m14 0h2a2 2 0 012 2v3m-2-3H9m12 3H9" />
                    </svg>
                    <span>Open folder</span>
                  </button>
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 rounded border border-zinc-200 bg-white px-2.5 py-1 text-[11px] font-bold text-zinc-700 shadow-2xs transition-colors hover:bg-zinc-50 disabled:opacity-50 dark:border-white/10 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:bg-zinc-800"
                    :disabled="!cluster.kubeconfig || savingKubeconfigRunId === cluster.runId"
                    @click="saveKubeconfig(cluster)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
                    </svg>
                    <span>{{ savingKubeconfigRunId === cluster.runId ? "Downloading..." : "Download" }}</span>
                  </button>
                </div>
              </div>
              <div class="flex min-w-0 items-center justify-between gap-2.5 rounded-lg border border-zinc-200/50 bg-zinc-50 px-2.5 py-1.5 dark:border-white/5 dark:bg-zinc-950">
                <span class="block truncate select-all font-mono text-[10px] text-zinc-500 dark:text-zinc-400" :title="cluster.kubeconfig">
                  {{ cluster.kubeconfig || "not ready" }}
                </span>
                <button
                  type="button"
                  class="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:bg-zinc-200/50 hover:text-zinc-700 disabled:opacity-40 dark:hover:bg-zinc-800/80 dark:hover:text-zinc-200"
                  title="Copy path"
                  :disabled="!cluster.kubeconfig"
                  @click="copyText(cluster.kubeconfig, 'Kubeconfig path copied.')"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                    <path d="M8 3a1 1 0 011-1h2a1 1 0 110 2H9a1 1 0 01-1-1z" />
                    <path d="M6 3a2 2 0 00-2 2v11a2 2 0 002 2h8a2 2 0 002-2V5a2 2 0 00-2-2 3 3 0 01-3 3H9a3 3 0 01-3-3z" />
                  </svg>
                </button>
              </div>
            </div>

            <div class="flex flex-col gap-2 rounded-xl border border-zinc-200/60 bg-white p-3.5 shadow-2xs dark:border-white/5 dark:bg-zinc-900/50">
              <div class="flex items-center justify-between gap-3">
                <div class="flex min-w-0 items-center gap-2">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 shrink-0 text-zinc-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z" />
                  </svg>
                  <span class="text-xs font-bold text-zinc-700 dark:text-zinc-200">Run folder</span>
                </div>
                <button
                  type="button"
                  class="inline-flex items-center gap-1 rounded border border-zinc-200 bg-white px-2.5 py-1 text-[11px] font-semibold text-zinc-700 shadow-2xs transition-colors hover:bg-zinc-50 disabled:opacity-50 dark:border-white/10 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:bg-zinc-800"
                  :disabled="!cluster.runDir"
                  @click="openRunFolder(cluster)"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M5 19a2 2 0 01-2-2V7a2 2 0 012-2h4l2 2h4a2 2 0 012 2v1M5 19h14a2 2 0 002-2v-5M5 19V9m14 0h2a2 2 0 012 2v3m-2-3H9m12 3H9" />
                  </svg>
                  <span>Open folder</span>
                </button>
              </div>
              <div class="flex min-w-0 items-center justify-between gap-2.5 rounded-lg border border-zinc-200/50 bg-zinc-50 px-2.5 py-1.5 dark:border-white/5 dark:bg-zinc-950">
                <span class="block truncate select-all font-mono text-[10px] text-zinc-500 dark:text-zinc-400" :title="cluster.runDir">
                  {{ cluster.runDir || "not recorded" }}
                </span>
                <button
                  type="button"
                  class="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:bg-zinc-200/50 hover:text-zinc-700 disabled:opacity-40 dark:hover:bg-zinc-800/80 dark:hover:text-zinc-200"
                  title="Copy path"
                  :disabled="!cluster.runDir"
                  @click="copyText(cluster.runDir, 'Run folder path copied.')"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                    <path d="M8 3a1 1 0 011-1h2a1 1 0 110 2H9a1 1 0 01-1-1z" />
                    <path d="M6 3a2 2 0 00-2 2v11a2 2 0 002 2h8a2 2 0 002-2V5a2 2 0 00-2-2 3 3 0 01-3 3H9a3 3 0 01-3-3z" />
                  </svg>
                </button>
              </div>
            </div>
          </div>
        </div>

        <div v-if="cluster.error" class="flex items-start gap-2.5 rounded-xl border border-rose-500/10 bg-rose-500/5 p-4 text-sm font-semibold text-rose-800 dark:text-rose-200">
          <svg xmlns="http://www.w3.org/2000/svg" class="mt-0.5 h-5 w-5 shrink-0 text-rose-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span class="leading-6">{{ cluster.error }}</span>
        </div>

        <div class="flex flex-col gap-3 border-t border-zinc-200/50 pt-4 dark:border-white/5 sm:flex-row sm:items-center sm:justify-between">
          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg bg-emerald-500 px-3.5 py-1.5 text-xs font-semibold text-white shadow-sm shadow-emerald-500/10 transition hover:bg-emerald-600 disabled:opacity-50"
              :disabled="!cluster.apiUrl"
              @click="copyText(cluster.apiUrl, 'Endpoint copied.')"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="mr-1.5 h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m-2-4h1a2 2 0 012 2v2m-6 4h3a2 2 0 002-2V9a2 2 0 00-2-2h-3" />
              </svg>
              <span>Copy endpoint</span>
            </button>
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white px-3.5 py-1.5 text-xs font-semibold text-zinc-700 transition hover:bg-zinc-100 disabled:opacity-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
              :disabled="cluster.status !== 'running' || operation.running || rowActionRunning"
              @click="clusterAction(cluster, 'stop')"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="mr-1.5 h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
              </svg>
              <span>{{ rowActionLabel(cluster, "stop", "Stop", "Stopping...") }}</span>
            </button>
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white px-3.5 py-1.5 text-xs font-semibold text-zinc-700 transition hover:bg-zinc-100 disabled:opacity-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
              :disabled="cluster.status !== 'stopped' || operation.running || rowActionRunning"
              @click="clusterAction(cluster, 'restart')"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="mr-1.5 h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M14.752 11.168l-5.197-3.027A1 1 0 008 9.027v5.946a1 1 0 001.555.832l5.197-2.973a1 1 0 000-1.664z" />
              </svg>
              <span>{{ rowActionLabel(cluster, "restart", "Start", "Starting...") }}</span>
            </button>
          </div>

          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg bg-rose-500 px-3.5 py-1.5 text-xs font-semibold text-white shadow-sm shadow-rose-500/10 transition hover:bg-rose-600 disabled:opacity-50"
              :disabled="operation.running || rowActionRunning"
              @click="clusterAction(cluster, 'delete', true)"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="mr-1.5 h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
              <span>{{ rowActionLabel(cluster, "delete", "Delete", "Deleting...") }}</span>
            </button>
          </div>
        </div>
      </div>

      <div v-if="!clusters.length" class="rounded-2xl border border-zinc-200 bg-zinc-50 p-5 text-sm text-zinc-500 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-400">
        No K3D Lab cluster records found yet.
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
const preflightCollapsed = ref(true);
const toasts = ref([]);
let timer = null;

const addToast = (message, kind = "success") => {
  if (!message) return;
  const id = Date.now() + Math.random().toString(36).slice(2, 11);
  toasts.value.push({ id, message, kind });
  window.setTimeout(() => {
    toasts.value = toasts.value.filter(toast => toast.id !== id);
  }, 4000);
};

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
const runningClusters = computed(() => clusters.value.filter(cluster => cluster.status === "running"));
const stoppedClusters = computed(() => clusters.value.filter(cluster => cluster.status === "stopped"));
const creatingClusters = computed(() => clusters.value.filter(cluster => cluster.status === "creating"));
const k3sOptions = computed(() => [...new Set((state.value.k3sVersions || []).filter(Boolean))]);
const inputsDisabled = computed(() => operation.value.running || installing.value || stopping.value);
const startDisabled = computed(() => !preflight.value.ready || operation.value.running || !k3sVersion.value);
const startButtonLabel = computed(() => {
  if (operation.value.running) return "Starting...";
  return runningClusters.value.length ? "Start another k3d" : "Start k3d";
});
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
  if (creatingClusters.value.length) parts.push(`${creatingClusters.value.length} creating`);
  parts.push(`${runningClusters.value.length} running`);
  if (stoppedClusters.value.length) parts.push(`${stoppedClusters.value.length} stopped`);
  return `${parts.join(", ")}.`;
});
const outputText = computed(() => (Array.isArray(operation.value.output) && operation.value.output.length)
  ? operation.value.output.join("\n")
  : "K3D Lab output will appear here.");
const hasOutput = computed(() => Array.isArray(operation.value.output) && operation.value.output.length > 0);
const outputLineCount = computed(() => Array.isArray(operation.value.output) ? operation.value.output.length : 0);
const clearDisabled = computed(() => clearingOutput.value || !hasOutput.value);
const rowActionRunning = computed(() => Boolean(activeRowAction.value.runId));
const noticeTone = computed(() => noticeKind.value === "error" ? "text-rose-600 dark:text-rose-300" : "text-emerald-600 dark:text-emerald-300");

const preflightStatusLabel = computed(() => {
  if (!preflightItems.value.length) return "Checking...";
  const errors = preflightItems.value.filter(item => item.status === "error").length;
  const warnings = preflightItems.value.filter(item => item.status === "warning").length;
  if (errors > 0) return `${errors} blocking tool${errors === 1 ? "" : "s"}`;
  if (warnings > 0) return `${warnings} warning${warnings === 1 ? "" : "s"}`;
  return "Ready";
});

const preflightStatusClass = computed(() => {
  const errors = preflightItems.value.filter(item => item.status === "error").length;
  const warnings = preflightItems.value.filter(item => item.status === "warning").length;
  if (errors > 0) {
    return "inline-flex items-center justify-center rounded-full bg-rose-100 px-3 py-1 text-xs font-bold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300 border border-rose-500/20";
  }
  if (warnings > 0) {
    return "inline-flex items-center justify-center rounded-full bg-amber-100 px-3 py-1 text-xs font-bold text-amber-700 dark:bg-amber-500/15 dark:text-amber-300 border border-amber-500/20";
  }
  return "inline-flex items-center justify-center rounded-full bg-emerald-100 px-3 py-1 text-xs font-bold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300 border border-emerald-500/20";
});

watch(k3sOptions, values => {
  if (!k3sVersion.value && values.length) {
    k3sVersion.value = values[0];
  }
});

const setNotice = (message, kind = "info", toast = true) => {
  notice.value = message;
  noticeKind.value = kind;
  if (toast && message) {
    addToast(message, kind === "error" ? "error" : kind === "warning" ? "warning" : "success");
  }
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
  if (!preflight.value.ready) {
    setNotice("Preflight checks failed: please verify that Docker is running and k3d is installed.", "error");
    return;
  }
  setNotice("", "info", false);
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
  setNotice("", "info", false);
  try {
    await apiFetch("/api/k3d/install", { method: "POST", body: "{}" });
    setNotice("k3d install started. Watch the console, then refresh tools.", "info");
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
    setNotice("Stop requested for K3D Lab action.", "warning");
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
    if (state.value.operation) {
      state.value.operation.output = [];
    }
    addToast("Console output cleared.", "success");
    await refreshState();
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to clear K3D Lab output.", "error");
  } finally {
    clearingOutput.value = false;
  }
};

const clusterAction = async (cluster, action, deleteDir = false) => {
  activeRowAction.value = { runId: cluster.runId, action };
  const actionMsg = action === "delete" ? "Deleting K3D cluster..." : `${action === "restart" ? "Starting" : "Stopping"} K3D cluster...`;
  setNotice(actionMsg, "info");
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
  if (!value) return;
  try {
    await navigator.clipboard.writeText(value);
    setNotice(message);
  } catch {
    setNotice(value, "info");
  }
};

const saveKubeconfig = async cluster => {
  savingKubeconfigRunId.value = cluster.runId;
  addToast("Requesting kubeconfig download...", "info");
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

const openLocalPath = async (path, successMessage) => {
  if (!path) return;
  try {
    await apiFetch("/api/open-path", {
      method: "POST",
      body: JSON.stringify({ path, reveal: true }),
    });
    setNotice(successMessage);
  } catch (error) {
    setNotice(error instanceof Error ? error.message : "Failed to open folder.", "error");
  }
};

const openKubeconfigFolder = cluster => openLocalPath(cluster.kubeconfig, "Opened kubeconfig location.");
const openRunFolder = cluster => openLocalPath(cluster.runDir, "Opened run folder.");

const preflightItemClass = status => ({
  ok: "border-emerald-200 bg-emerald-50/50 text-emerald-800 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-200",
  warning: "border-amber-200 bg-amber-50/50 text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200",
  error: "border-rose-200 bg-rose-50/50 text-rose-800 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-200",
})[status] || "border-zinc-200 bg-white text-zinc-700 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300";

const clusterCardClass = cluster => ({
  "border-l-4 border-l-rose-500 border-zinc-200 dark:border-white/10": cluster.status === "failed",
  "border-l-4 border-l-sky-500 border-zinc-200 dark:border-white/10": cluster.status === "creating",
  "border-l-4 border-l-emerald-500 border-zinc-200 dark:border-white/10": cluster.status === "running",
  "border-l-4 border-l-zinc-300 border-zinc-200 dark:border-l-zinc-700 dark:border-white/10": !["failed", "creating", "running"].includes(cluster.status),
});

const clusterDotClass = status => ({
  failed: "bg-rose-500",
  creating: "bg-sky-500",
  running: "bg-emerald-500",
  stopped: "bg-zinc-400 dark:bg-zinc-600",
})[status] || "bg-zinc-400 dark:bg-zinc-600";

const clusterStatusBadgeClass = status => ({
  failed: "bg-rose-50 text-rose-700 border-rose-200/50 dark:bg-rose-500/10 dark:text-rose-300 dark:border-rose-500/20",
  creating: "bg-sky-50 text-sky-700 border-sky-200/50 dark:bg-sky-500/10 dark:text-sky-300 dark:border-sky-500/20",
  running: "bg-emerald-50 text-emerald-700 border-emerald-200/50 dark:bg-emerald-500/10 dark:text-emerald-300 dark:border-emerald-500/20",
  stopped: "bg-zinc-50 text-zinc-600 border-zinc-200 dark:bg-zinc-800/40 dark:text-zinc-400 dark:border-white/5",
})[status] || "bg-zinc-50 text-zinc-600 border-zinc-200 dark:bg-zinc-800/40 dark:text-zinc-400 dark:border-white/5";

onMounted(async () => {
  await refreshState();
  timer = window.setInterval(refreshState, 4000);
});

onUnmounted(() => {
  window.clearInterval(timer);
});
</script>

<style scoped>
.toast-fade-enter-active,
.toast-fade-leave-active {
  transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}
.toast-fade-enter-from {
  opacity: 0;
  transform: translateY(20px) scale(0.95);
}
.toast-fade-leave-to {
  opacity: 0;
  transform: scale(0.95);
}
</style>
