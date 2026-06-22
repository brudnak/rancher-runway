<template>
  <div class="grid gap-6">
    <!-- Floating Toast Notifications Overlay -->
    <div class="fixed bottom-5 right-5 z-[9999] flex flex-col gap-2.5 max-w-sm pointer-events-none">
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
          <!-- Status Icon -->
          <span class="shrink-0 mt-0.5">
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
          <div class="flex-1 text-sm font-semibold leading-5">
            {{ toast.message }}
          </div>
        </div>
      </transition-group>
    </div>

    <!-- Page Header and Dashboard KPI Row -->
    <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0">
        <div class="inline-flex items-center rounded-full border border-emerald-200 bg-emerald-50/50 px-3 py-1 text-xs font-semibold text-emerald-700 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-300">
          Local K3d Sandbox Environment
        </div>
        <h2 class="mt-3 text-2xl font-bold tracking-tight text-zinc-950 dark:text-zinc-50">Steve Lab</h2>
        <p class="mt-1.5 max-w-3xl text-sm leading-6 text-zinc-500 dark:text-zinc-400">
          Discover Steve releases or compile exact commits, configure local K3s cluster templates, and run a dedicated Steve HTTPS endpoint for verification.
        </p>
      </div>

      <!-- Stat Cards -->
      <div class="grid shrink-0 gap-3 sm:grid-cols-3 lg:min-w-[30rem]">
        <!-- Stat Card: Tools -->
        <div 
          class="relative overflow-hidden rounded-2xl border bg-white/70 backdrop-blur-md p-4 dark:bg-zinc-900/60 transition-all duration-200 hover:scale-[1.01] flex flex-col gap-1 border-zinc-200/80 dark:border-white/5 shadow-2xs"
          :class="preflight.ready ? 'border-l-4 border-l-emerald-500' : 'border-l-4 border-l-rose-500'"
        >
          <span class="text-[10px] font-extrabold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Local Tools</span>
          <div class="text-base font-bold text-zinc-800 dark:text-zinc-100 flex items-center gap-1.5 mt-0.5">
            <span class="h-2 w-2 rounded-full" :class="preflight.ready ? 'bg-emerald-500' : 'bg-rose-500'"></span>
            <span>{{ preflight.ready ? "Ready" : "Blocked" }}</span>
          </div>
          <span class="text-xs text-zinc-500 dark:text-zinc-400 mt-1 truncate" :title="preflight.summary">{{ preflight.summary || "Checking Docker & k3d" }}</span>
          <button type="button" class="mt-2 self-start inline-flex items-center gap-1 text-[11px] font-bold text-sky-600 dark:text-sky-400 hover:underline" @click="refreshState">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 1121.21 7.89H18" />
            </svg>
            <span>Refresh tools</span>
          </button>
        </div>

        <!-- Stat Card: Startup -->
        <div 
          class="relative overflow-hidden rounded-2xl border bg-white/70 backdrop-blur-md p-4 dark:bg-zinc-900/60 transition-all duration-200 hover:scale-[1.01] flex flex-col gap-1 border-zinc-200/80 dark:border-white/5 shadow-2xs"
          :class="operation.running ? 'border-l-4 border-l-sky-500' : ''"
        >
          <span class="text-[10px] font-extrabold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Startup Status</span>
          <div class="text-base font-bold text-zinc-800 dark:text-zinc-100 flex items-center gap-1.5 mt-0.5">
            <span v-if="operation.running" class="relative flex h-2 w-2">
              <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-sky-400 opacity-75"></span>
              <span class="relative inline-flex rounded-full h-2 w-2 bg-sky-500"></span>
            </span>
            <span v-else class="h-2 w-2 rounded-full bg-zinc-300 dark:bg-zinc-600"></span>
            <span>{{ operation.running ? "Running" : "Idle" }}</span>
          </div>
          <span class="text-xs text-zinc-500 dark:text-zinc-400 mt-1 truncate" :title="operation.runId">{{ operation.runId || "No active startup" }}</span>
        </div>

        <!-- Stat Card: Active -->
        <div 
          class="relative overflow-hidden rounded-2xl border bg-white/70 backdrop-blur-md p-4 dark:bg-zinc-900/60 transition-all duration-200 hover:scale-[1.01] flex flex-col gap-1 border-zinc-200/80 dark:border-white/5 shadow-2xs"
          :class="activeRun ? 'border-l-4 border-l-emerald-500' : ''"
        >
          <span class="text-[10px] font-extrabold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Active Lab</span>
          <div class="text-base font-bold text-zinc-800 dark:text-zinc-100 flex items-center gap-1.5 mt-0.5">
            <span v-if="activeRun" class="h-2 w-2 rounded-full bg-emerald-500 animate-pulse"></span>
            <span v-else class="h-2 w-2 rounded-full bg-zinc-300 dark:bg-zinc-600"></span>
            <span>{{ activeRun ? "Serving" : "None" }}</span>
          </div>
          <span class="text-xs text-zinc-500 dark:text-zinc-400 mt-1 truncate" :title="activeRun ? activeRun.runId : ''">{{ activeRun ? activeRun.runId : "No endpoint serving" }}</span>
        </div>
      </div>
    </div>

    <!-- Preflight Dashboard Section -->
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
          <div :class="preflightStatusClass">
            {{ preflightStatusLabel }}
          </div>
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

      <div v-show="!preflightCollapsed" class="border-t border-zinc-200/60 dark:border-white/5 p-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-5 text-sm">
        <div
          v-for="item in preflightItems"
          :key="item.name"
          class="rounded-xl border px-3.5 py-3 transition-colors duration-150 shadow-2xs"
          :class="preflightItemClass(item.status)"
        >
          <div class="flex items-center justify-between gap-3">
            <span class="min-w-0 truncate font-semibold">{{ item.name }}</span>
            <span class="shrink-0 text-[10px] font-bold uppercase tracking-wide opacity-80">{{ item.status || "unknown" }}</span>
          </div>
          <div class="mt-1.5 text-xs leading-5 opacity-90 truncate" :title="item.detail">{{ item.detail || "Installed & ready." }}</div>
        </div>
      </div>
    </div>

    <!-- Configuration Panels Grid -->
    <div class="grid gap-5 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)]">
      <!-- Choose Steve Card -->
      <section class="rounded-2xl border border-zinc-200/80 bg-zinc-50/50 p-5 shadow-2xs dark:border-white/10 dark:bg-white/[0.02] flex flex-col justify-between">
        <div>
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div class="flex items-center gap-2">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
              </svg>
              <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-50">Choose Steve Release</h3>
            </div>
            <button 
              type="button" 
              class="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-bold rounded-lg border border-zinc-200 bg-white hover:bg-zinc-100 text-zinc-700 dark:border-white/10 dark:bg-white/[0.06] dark:hover:bg-white/[0.1] dark:text-zinc-200 transition"
              @click="loadVersions" 
              :disabled="versionsLoading"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" :class="{ 'animate-spin': versionsLoading }" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 1121.21 7.89H18" />
              </svg>
              <span>{{ versionsLoading ? "Refreshing..." : "Refresh tags" }}</span>
            </button>
          </div>
          <p class="mt-1 text-sm text-zinc-500 dark:text-zinc-400">Select a stable release tag or type a custom commit SHA or branch.</p>

          <div class="mt-4 grid gap-4">
            <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
              <span>Stable release tag</span>
              <select 
                v-model="selectedTag" 
                :disabled="inputsDisabled"
                class="w-full rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-semibold text-zinc-900 outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white disabled:opacity-60 disabled:bg-zinc-50 dark:disabled:bg-zinc-900/50 disabled:cursor-not-allowed"
              >
                <option value="" class="text-zinc-500">Choose a Steve tag</option>
                <option v-for="tag in tagOptions" :key="tag.name" :value="tag.name" class="text-zinc-800 dark:text-zinc-900 dark:bg-zinc-900 dark:text-white">{{ tag.name }}</option>
              </select>
            </label>

            <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
              <span>Exact tag, branch, or commit SHA</span>
              <input 
                v-model.trim="steveRef" 
                type="text" 
                autocomplete="off" 
                :disabled="inputsDisabled"
                placeholder="v0.9.10 or a commit SHA" 
                class="w-full rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-semibold text-zinc-900 outline-none placeholder:text-zinc-400 focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500 disabled:opacity-60 disabled:bg-zinc-50 dark:disabled:bg-zinc-900/50 disabled:cursor-not-allowed" 
              />
            </label>
          </div>
        </div>

        <div class="mt-4 rounded-xl border border-sky-100 bg-sky-500/5 p-3.5 text-xs leading-5 text-sky-900 dark:border-sky-500/10 dark:bg-sky-500/10 dark:text-sky-200 shadow-3xs">
          <div class="font-bold flex items-center gap-1.5">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-4.5 w-4.5 text-sky-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span>{{ refStatusTitle }}</span>
          </div>
          <div class="mt-1 pl-5 font-medium opacity-90">{{ refStatusBody }}</div>
        </div>
      </section>

      <!-- Serve with k3d Card -->
      <section class="rounded-2xl border border-zinc-200/80 bg-zinc-50/50 p-5 shadow-2xs dark:border-white/10 dark:bg-white/[0.02] flex flex-col justify-between">
        <div>
          <div class="flex items-center gap-2">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14M5 12a2 2 0 012-2h2a2 2 0 012 2m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2V12" />
            </svg>
            <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-50">Local Cluster Settings</h3>
          </div>
          <p class="mt-1 text-sm text-zinc-500 dark:text-zinc-400">Launch a private K3s Kubernetes node and deploy the active Steve endpoint on it.</p>

          <div class="mt-4 grid gap-4">
            <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
              <span>K3s engine version</span>
              <input 
                v-model.trim="k3sVersion" 
                list="steve-k3s-versions" 
                type="text" 
                autocomplete="off" 
                :disabled="inputsDisabled"
                class="w-full rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-semibold text-zinc-900 outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white disabled:opacity-60 disabled:bg-zinc-50 dark:disabled:bg-zinc-900/50 disabled:cursor-not-allowed" 
              />
              <datalist id="steve-k3s-versions">
                <option v-for="version in k3sOptions" :key="version" :value="version"></option>
              </datalist>
            </label>

            <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
              <span>HTTPS local port binding</span>
              <input 
                v-model.number="httpsPort" 
                type="number" 
                min="1024" 
                max="65535" 
                :disabled="inputsDisabled"
                placeholder="Auto" 
                class="w-full rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-semibold text-zinc-900 outline-none placeholder:text-zinc-400 focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500 disabled:opacity-60 disabled:bg-zinc-50 dark:disabled:bg-zinc-900/50 disabled:cursor-not-allowed" 
              />
            </label>
          </div>
        </div>

        <div class="mt-4 space-y-3">
          <!-- Warnings Banner -->
          <div v-if="!preflight.ready && preflightItems.length" class="rounded-xl border border-rose-300 bg-rose-500/5 p-3.5 text-xs leading-5 text-rose-900 dark:border-rose-500/15 dark:bg-rose-950/20 dark:text-rose-200">
            <div class="font-bold flex items-center gap-1.5">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-4.5 w-4.5 text-rose-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              <span>Docker or required utilities are offline</span>
            </div>
            <ul class="mt-1.5 list-disc pl-5 space-y-0.5 font-medium opacity-90">
              <li v-for="item in preflightItems.filter(i => i.status === 'error')" :key="item.name">
                <strong>{{ item.name }}:</strong> {{ item.detail || 'Not running.' }}
              </li>
            </ul>
          </div>

          <!-- Actions Toolbar -->
          <div class="flex flex-wrap gap-2 pt-1.5 border-t border-zinc-200/60 dark:border-white/5">
            <button 
              type="button" 
              class="inline-flex min-h-10 items-center justify-center rounded-xl px-4 py-2 text-sm font-bold shadow-md transition-all"
              :class="preflight.ready ? 'bg-emerald-500 hover:bg-emerald-600 text-white shadow-emerald-500/10' : 'bg-rose-500/10 text-rose-500 border border-rose-500/20 hover:bg-rose-500/15'"
              :disabled="startDisabled" 
              @click="startRun"
            >
              <svg v-if="starting" class="animate-spin -ml-1 mr-2 h-4 w-4 text-current" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
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
              :class="operation.running ? 'border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100 dark:border-rose-500/30 dark:bg-rose-950/20 dark:text-rose-300' : 'border-zinc-200 bg-white text-zinc-400 dark:border-white/5 dark:bg-white/[0.04] cursor-not-allowed'"
              :disabled="!operation.running || stopping" 
              @click="stopRun"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="mr-2 h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 005.636 5.636m12.728 12.728L5.636 5.636" />
              </svg>
              <span>{{ stopping ? "Stopping..." : "Stop startup" }}</span>
            </button>
          </div>
          
          <p v-if="notice" class="text-xs font-bold leading-5 transition-all mt-1 p-2 bg-zinc-200/30 rounded-lg dark:bg-zinc-950/50" :class="noticeTone">{{ notice }}</p>
        </div>
      </section>
    </div>

    <!-- Runtime overrides panel -->
    <section class="rounded-2xl border border-zinc-200/80 bg-zinc-50/50 p-5 shadow-2xs dark:border-white/10 dark:bg-white/[0.02]">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9.75 3.104v5.714a2.25 2.25 0 01-.659 1.591L5 14.5M9.75 3.104c-.251.023-.501.05-.75.082m.75-.082a24.301 24.301 0 014.5 0m0 0v5.714a2.25 2.25 0 00.659 1.591L19 14.5m-4.75-11.396c.251.023.501.05.75.082M19 14.5l-3.243 3.243a6 6 0 01-8.486 0L5 14.5m14 0l1.5 1.5M5 14.5L3.5 16" />
            </svg>
            <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-50">Steve Runtime Overrides</h3>
          </div>
          <p class="mt-1 text-sm text-zinc-500 dark:text-zinc-400">Enable Prometheus metrics or add custom launch-time env vars and args.</p>
        </div>
        <div v-if="enableMetrics" class="rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs font-bold text-emerald-700 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-300">
          CATTLE_PROMETHEUS_METRICS=true
        </div>
      </div>

      <div class="mt-4 grid gap-4 lg:grid-cols-[minmax(0,0.75fr)_minmax(0,1fr)_minmax(0,1fr)]">
        <div class="grid gap-3 rounded-xl border border-zinc-200 bg-white p-3.5 dark:border-white/10 dark:bg-zinc-900/50">
          <label class="flex min-h-11 cursor-pointer items-center justify-between gap-3 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
            <span>Prometheus metrics</span>
            <input
              v-model="enableMetrics"
              type="checkbox"
              :disabled="inputsDisabled"
              class="h-4 w-4 rounded border-zinc-300 text-emerald-500 focus:ring-emerald-500 disabled:opacity-50"
            />
          </label>
          <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
            <span>Update interval seconds</span>
            <input
              v-model.number="metricsUpdateInterval"
              type="number"
              min="1"
              max="3600"
              :disabled="inputsDisabled || !enableMetrics"
              class="w-full rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-semibold text-zinc-900 outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 disabled:opacity-60 disabled:bg-zinc-50 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:disabled:bg-zinc-900/50"
            />
          </label>
        </div>

        <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
          <span>Additional env vars</span>
          <textarea
            v-model="extraEnv"
            rows="4"
            spellcheck="false"
            :disabled="inputsDisabled"
            placeholder="KEY=value"
            class="min-h-24 w-full resize-y rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 font-mono text-xs font-semibold text-zinc-900 outline-none placeholder:text-zinc-400 focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 disabled:opacity-60 disabled:bg-zinc-50 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500 dark:disabled:bg-zinc-900/50"
          ></textarea>
        </label>

        <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300">
          <span>Additional Steve args</span>
          <textarea
            v-model="extraArgs"
            rows="4"
            spellcheck="false"
            :disabled="inputsDisabled"
            placeholder="--flag --another-flag=value"
            class="min-h-24 w-full resize-y rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 font-mono text-xs font-semibold text-zinc-900 outline-none placeholder:text-zinc-400 focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 disabled:opacity-60 disabled:bg-zinc-50 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500 dark:disabled:bg-zinc-900/50"
          ></textarea>
        </label>
      </div>
    </section>

    <!-- Terminal Console Output panel -->
    <section id="terminalConsole" class="overflow-hidden rounded-2xl border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-950 shadow-md">
      <!-- Terminal Header -->
      <div class="flex flex-col gap-2.5 bg-zinc-50 px-4 py-3 sm:flex-row sm:items-center sm:justify-between border-b border-zinc-200 dark:bg-zinc-900/90 dark:border-zinc-800">
        <div class="flex items-center gap-3">
          <!-- Window Dots -->
          <div class="flex gap-1.5 shrink-0">
            <span class="h-3 w-3 rounded-full bg-rose-500/80"></span>
            <span class="h-3 w-3 rounded-full bg-amber-500/80"></span>
            <span class="h-3 w-3 rounded-full bg-emerald-500/80"></span>
          </div>
          <!-- Terminal Title -->
          <h3 class="font-mono text-xs font-semibold text-zinc-700 dark:text-zinc-300 flex items-center gap-2 truncate">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 text-zinc-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M8 9l3 3-3 3m5 0h3" />
            </svg>
            <span>{{ streamingLogRunId ? `steve.log — ${streamingLogRunId}` : (operation.command ? `steve-startup.sh` : `local-console`) }}</span>
            <span v-if="streamingLogRunId" class="inline-flex h-1.5 w-1.5 rounded-full bg-emerald-500 animate-ping"></span>
          </h3>
        </div>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-if="streamingLogRunId"
            type="button"
            class="inline-flex items-center gap-1 rounded-md bg-rose-50 px-2.5 py-1 text-[10px] font-extrabold uppercase tracking-wide text-rose-700 border border-rose-200 hover:bg-rose-100 dark:bg-rose-500/10 dark:text-rose-400 dark:border-rose-500/20 dark:hover:bg-rose-500/15 transition-colors"
            @click="stopLogStreaming"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
            <span>Close stream</span>
          </button>
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-md bg-white px-2.5 py-1 text-[10px] font-bold text-zinc-700 border border-zinc-200 hover:bg-zinc-50 dark:bg-zinc-800 dark:text-zinc-300 dark:border-zinc-700/60 dark:hover:bg-zinc-700 transition-colors disabled:opacity-40"
            @click="refreshConsole"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 1121.21 7.89H18" />
            </svg>
            <span>Refresh</span>
          </button>
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-md bg-white px-2.5 py-1 text-[10px] font-bold text-zinc-700 border border-zinc-200 hover:bg-zinc-50 dark:bg-zinc-800 dark:text-zinc-300 dark:border-zinc-700/60 dark:hover:bg-zinc-700 transition-colors disabled:opacity-40"
            :disabled="!hasOutput"
            @click="outputCollapsed = !outputCollapsed"
          >
            <svg v-if="outputCollapsed" xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
            <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7" />
            </svg>
            <span>{{ outputCollapsed ? "Expand" : "Collapse" }}</span>
          </button>
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-md bg-white px-2.5 py-1 text-[10px] font-bold text-zinc-700 border border-zinc-200 hover:bg-zinc-50 dark:bg-zinc-800 dark:text-zinc-300 dark:border-zinc-700/60 dark:hover:bg-zinc-700 transition-colors disabled:opacity-40"
            :disabled="clearDisabled"
            @click="clearOutput"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            <span>Clear console</span>
          </button>
        </div>
      </div>

      <!-- Terminal Body content -->
      <div v-if="outputCollapsed" class="p-5 font-mono text-xs text-zinc-500 bg-zinc-50 border-t border-zinc-200/50 dark:bg-zinc-950 dark:border-zinc-900/40">
        Terminal output collapsed. {{ outputLineCount }} logs statement{{ outputLineCount === 1 ? "" : "s" }} parsed.
      </div>
      <div v-else class="relative bg-zinc-50 p-4 border-t border-zinc-200/50 dark:bg-zinc-950 dark:border-zinc-900/40 font-mono text-[11px] leading-5 text-zinc-800 dark:text-zinc-300">
        <pre class="text-zinc-800 dark:text-zinc-300 max-h-[22rem] max-w-full overflow-auto pr-4 whitespace-pre-wrap break-words scrollbar-thin scrollbar-thumb-zinc-300 dark:scrollbar-thumb-zinc-800">{{ outputText }}<span v-if="streamingLogRunId" class="inline-block w-1.5 h-3 bg-emerald-500 animate-pulse ml-0.5 align-middle"></span></pre>
      </div>
    </section>

  <!-- History list section -->
  <section class="grid gap-3">
    <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-50 flex items-center gap-2">
      <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
      <span>Steve Lab Endpoint History</span>
    </h3>

      <div
        v-for="run in runs"
        :key="run.runId"
        class="relative overflow-hidden rounded-2xl border bg-white p-5 shadow-2xs transition-all duration-200 hover:shadow-sm dark:bg-zinc-900/40 flex flex-col gap-5"
        :class="{
          'border-l-4 border-l-rose-500 border-zinc-200 dark:border-white/10': run.status === 'failed',
          'border-l-4 border-l-sky-500 border-zinc-200 dark:border-white/10': run.status === 'running' || run.status === 'starting',
          'border-l-4 border-l-emerald-500 border-zinc-200 dark:border-white/10': run.status === 'serving',
          'border-l-4 border-l-zinc-300 dark:border-l-zinc-700 border-zinc-200': !['failed', 'running', 'starting', 'serving'].includes(run.status)
        }"
      >
        <!-- Card Header -->
        <div class="flex items-center justify-between border-b border-zinc-200/50 pb-3.5 dark:border-white/5">
          <div class="flex items-center gap-3">
            <!-- Pulsing Status indicator dot -->
            <span class="relative flex h-2.5 w-2.5">
              <span
                v-if="['serving', 'running', 'starting'].includes(run.status)"
                class="animate-ping absolute inline-flex h-full w-full rounded-full opacity-75"
                :class="{
                  'bg-emerald-400': run.status === 'serving',
                  'bg-sky-400': run.status === 'running' || run.status === 'starting'
                }"
              ></span>
              <span
                class="relative inline-flex rounded-full h-2.5 w-2.5"
                :class="{
                  'bg-rose-500': run.status === 'failed',
                  'bg-sky-500': run.status === 'running' || run.status === 'starting',
                  'bg-emerald-500': run.status === 'serving',
                  'bg-zinc-400 dark:bg-zinc-650': !['failed', 'running', 'starting', 'serving'].includes(run.status)
                }"
              ></span>
            </span>

            <h4 class="text-sm font-bold text-zinc-900 dark:text-zinc-50 font-mono tracking-tight">{{ run.runId }}</h4>

            <!-- Status badge pill -->
            <span
              class="inline-flex items-center rounded-full px-2.5 py-0.5 text-[10px] font-bold uppercase tracking-wider border"
              :class="{
                'bg-rose-50 text-rose-700 border-rose-200/50 dark:bg-rose-500/10 dark:text-rose-350 dark:border-rose-500/20': run.status === 'failed',
                'bg-sky-50 text-sky-700 border-sky-200/50 dark:bg-sky-500/10 dark:text-sky-350 dark:border-sky-500/20': run.status === 'running' || run.status === 'starting',
                'bg-emerald-50 text-emerald-700 border-emerald-200/50 dark:bg-emerald-500/10 dark:text-emerald-350 dark:border-emerald-500/20': run.status === 'serving',
                'bg-zinc-50 text-zinc-650 border-zinc-200 dark:bg-zinc-800/40 dark:text-zinc-400 dark:border-white/5': !['failed', 'running', 'starting', 'serving'].includes(run.status)
              }"
            >
              {{ run.status }}
            </span>
          </div>

          <!-- Steve Process PID badge -->
          <div v-if="run.stevePid" class="text-xs text-zinc-550 dark:text-zinc-400 flex items-center gap-1.5 font-semibold bg-zinc-50 dark:bg-zinc-950 px-2.5 py-1 rounded-full border border-zinc-200/60 dark:border-white/5">
            <span class="inline-block h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
            <span>Steve PID: {{ run.stevePid }}</span>
          </div>
        </div>        <!-- KPI Info Grid -->
        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 bg-zinc-100/50 dark:bg-zinc-950/40 p-4 rounded-xl border border-zinc-200/60 dark:border-white/5">
          <!-- Steve Version -->
          <div class="flex flex-col min-w-0">
            <span class="text-[10px] font-bold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Steve Release</span>
            <div class="mt-1 flex items-center gap-1.5 min-w-0">
              <span class="text-sm font-semibold text-zinc-800 dark:text-zinc-200 truncate" :title="run.steveCommit || run.steveRef">
                {{ run.steveRef }}
              </span>
              <span v-if="run.sqlCache" class="inline-flex items-center rounded-full bg-emerald-50 px-2 py-0.5 text-[9px] font-bold text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300 border border-emerald-500/20" title="SQLite Object Cache enabled">
                Cache
              </span>
              <span v-if="run.enableMetrics" class="inline-flex items-center rounded-full bg-sky-50 px-2 py-0.5 text-[9px] font-bold text-sky-700 dark:bg-sky-500/10 dark:text-sky-300 border border-sky-500/20" :title="`Prometheus metrics every ${run.metricsUpdateIntervalSeconds || 15}s`">
                Metrics
              </span>
              <span v-if="runtimeOverrideCount(run)" class="inline-flex items-center rounded-full bg-amber-50 px-2 py-0.5 text-[9px] font-bold text-amber-700 dark:bg-amber-500/10 dark:text-amber-300 border border-amber-500/20" :title="runtimeOverrideTitle(run)">
                Overrides
              </span>
            </div>
          </div>

          <!-- Endpoint Address -->
          <div class="flex flex-col min-w-0 sm:col-span-2">
            <span class="text-[10px] font-bold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">Endpoint URL</span>
            <div class="mt-1 flex items-center gap-2 min-w-0">
              <a v-if="endpointUrl(run) && run.status === 'serving'" href="#" @click.prevent="openEndpoint(run)" class="text-sm font-semibold text-sky-600 dark:text-sky-400 hover:underline truncate block" :title="endpointUrl(run)">
                {{ endpointUrl(run) }}
              </a>
              <span v-else class="text-sm font-semibold text-zinc-400 dark:text-zinc-600 truncate block">not ready</span>
              <button
                v-if="endpointUrl(run) && run.status === 'serving'"
                type="button"
                class="shrink-0 p-1 rounded text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 hover:bg-zinc-200/50 dark:hover:bg-zinc-850 transition-colors"
                title="Copy URL"
                @click="copyEndpoint(run)"
              >
                <!-- Link Copy SVG -->
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                  <path d="M8 3a1 1 0 011-1h2a1 1 0 110 2H9a1 1 0 01-1-1z" />
                  <path d="M6 3a2 2 0 00-2 2v11a2 2 0 002 2h8a2 2 0 002-2V5a2 2 0 00-2-2 3 3 0 01-3 3H9a3 3 0 01-3-3z" />
                </svg>
              </button>
            </div>
          </div>

          <!-- K3s Image version -->
          <div class="flex flex-col min-w-0">
            <span class="text-[10px] font-bold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">K3s version</span>
            <span class="mt-1 text-sm font-semibold text-zinc-800 dark:text-zinc-200 truncate" :title="run.k3sVersion">{{ run.k3sVersion }}</span>
          </div>
        </div>

        <!-- Files and logs section -->
        <div class="flex flex-col gap-4 p-4 bg-zinc-50/30 dark:bg-white/[0.005] rounded-xl border border-zinc-200/60 dark:border-white/5">
          <div class="text-[10px] font-extrabold text-zinc-400 dark:text-zinc-500 uppercase tracking-wider">Resource Files & Logs</div>

          <div class="grid gap-3.5">
            <!-- Kubeconfig block -->
            <div class="flex flex-col gap-2 p-3.5 bg-white dark:bg-zinc-900/50 border border-zinc-200/60 dark:border-white/5 rounded-xl shadow-2xs">
              <div class="flex items-center justify-between gap-3">
                <div class="flex items-center gap-2 min-w-0">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-zinc-400 dark:text-zinc-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                  </svg>
                  <span class="text-xs font-bold text-zinc-700 dark:text-zinc-200">Kubeconfig configuration</span>
                </div>
                <div class="flex items-center gap-1.5 shrink-0">
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] font-semibold rounded border border-zinc-200 bg-white hover:bg-zinc-50 text-zinc-700 dark:border-white/10 dark:bg-zinc-900 dark:hover:bg-zinc-800 dark:text-zinc-200 transition-colors shadow-2xs"
                    @click="openKubeconfigFolder(run)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 19a2 2 0 01-2-2V7a2 2 0 012-2h4l2 2h4a2 2 0 012 2v1M5 19h14a2 2 0 002-2v-5M5 19V9m14 0h2a2 2 0 012 2v3m-2-3H9m12 3H9" />
                    </svg>
                    <span>Open folder</span>
                  </button>
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] font-bold rounded border border-zinc-200 bg-white hover:bg-zinc-50 text-zinc-700 dark:border-white/10 dark:bg-zinc-900 dark:hover:bg-zinc-800 dark:text-zinc-200 transition-colors shadow-2xs"
                    :disabled="savingKubeconfigRunId === run.runId"
                    @click="saveKubeconfig(run)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
                    </svg>
                    <span>{{ savingKubeconfigRunId === run.runId ? 'Downloading...' : 'Download' }}</span>
                  </button>
                </div>
              </div>
              <div class="flex items-center justify-between gap-2.5 bg-zinc-50 dark:bg-zinc-950 px-2.5 py-1.5 rounded-lg border border-zinc-200/50 dark:border-white/5 min-w-0">
                <span class="font-mono text-[10px] text-zinc-500 dark:text-zinc-400 truncate select-all block" :title="run.kubeconfig">
                  {{ run.kubeconfig }}
                </span>
                <button
                  type="button"
                  class="shrink-0 p-1 rounded text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 hover:bg-zinc-200/50 dark:hover:bg-zinc-800/80 transition-colors"
                  title="Copy path"
                  @click="copyText(run.kubeconfig, 'Kubeconfig path copied.')"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                    <path d="M8 3a1 1 0 011-1h2a1 1 0 110 2H9a1 1 0 01-1-1z" />
                    <path d="M6 3a2 2 0 00-2 2v11a2 2 0 002 2h8a2 2 0 002-2V5a2 2 0 00-2-2 3 3 0 01-3 3H9a3 3 0 01-3-3z" />
                  </svg>
                </button>
              </div>
            </div>

            <!-- Steve Log block -->
            <div class="flex flex-col gap-2 p-3.5 bg-white dark:bg-zinc-900/50 border border-zinc-200/60 dark:border-white/5 rounded-xl shadow-2xs">
              <div class="flex items-center justify-between gap-3">
                <div class="flex items-center gap-2 min-w-0">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-zinc-400 dark:text-zinc-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
                  </svg>
                  <span class="text-xs font-bold text-zinc-700 dark:text-zinc-200">Steve logs output</span>
                </div>
                <div class="flex items-center gap-1.5 shrink-0">
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] font-semibold rounded border border-zinc-200 bg-white hover:bg-zinc-50 text-zinc-700 dark:border-white/10 dark:bg-zinc-900 dark:hover:bg-zinc-800 dark:text-zinc-200 transition-colors shadow-2xs"
                    @click="openLogFolder(run)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 19a2 2 0 01-2-2V7a2 2 0 012-2h4l2 2h4a2 2 0 012 2v1M5 19h14a2 2 0 002-2v-5M5 19V9m14 0h2a2 2 0 012 2v3m-2-3H9m12 3H9" />
                    </svg>
                    <span>Open folder</span>
                  </button>
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] font-bold rounded text-white transition-colors shadow-xs"
                    :class="streamingLogRunId === run.runId ? 'bg-amber-500 hover:bg-amber-600' : 'bg-sky-500 hover:bg-sky-600'"
                    @click="startLogStreaming(run)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                      <path stroke-linecap="round" stroke-linejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                    </svg>
                    <span>{{ streamingLogRunId === run.runId ? 'Stop stream' : 'Stream to screen' }}</span>
                  </button>
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] font-semibold rounded border border-zinc-200 bg-white hover:bg-zinc-50 text-zinc-700 dark:border-white/10 dark:bg-zinc-900 dark:hover:bg-zinc-800 dark:text-zinc-200 transition-colors shadow-2xs"
                    @click="streamSteveLogs(run)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 4h4M4 20l5-5m11 5v-4m0 4h-4m4 0l-5-5" />
                    </svg>
                    <span>Stream modal</span>
                  </button>
                </div>
              </div>
              <div class="flex items-center justify-between gap-2.5 bg-zinc-50 dark:bg-zinc-950 px-2.5 py-1.5 rounded-lg border border-zinc-200/50 dark:border-white/5 min-w-0">
                <span class="font-mono text-[10px] text-zinc-500 dark:text-zinc-400 truncate select-all block" :title="run.logPath">
                  {{ run.logPath }}
                </span>
                <button
                  type="button"
                  class="shrink-0 p-1 rounded text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 hover:bg-zinc-200/50 dark:hover:bg-zinc-800/80 transition-colors"
                  title="Copy path"
                  @click="copyText(run.logPath, 'Log path copied.')"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                    <path d="M8 3a1 1 0 011-1h2a1 1 0 110 2H9a1 1 0 01-1-1z" />
                    <path d="M6 3a2 2 0 00-2 2v11a2 2 0 002 2h8a2 2 0 002-2V5a2 2 0 00-2-2 3 3 0 01-3 3H9a3 3 0 01-3-3z" />
                  </svg>
                </button>
              </div>
            </div>

            <!-- SQLite DB Block -->
            <div v-if="run.sqlCache" class="flex flex-col gap-2 p-3.5 bg-white dark:bg-zinc-900/50 border border-zinc-200/60 dark:border-white/5 rounded-xl shadow-2xs">
              <div class="flex items-center justify-between gap-3">
                <div class="flex items-center gap-2 min-w-0">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-zinc-400 dark:text-zinc-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4" />
                  </svg>
                  <span class="text-xs font-bold text-zinc-700 dark:text-zinc-200">SQLite Project Cache DB</span>
                </div>
                <div class="flex items-center gap-1.5 shrink-0">
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] font-semibold rounded border border-zinc-200 bg-white hover:bg-zinc-50 text-zinc-700 dark:border-white/10 dark:bg-zinc-900 dark:hover:bg-zinc-800 dark:text-zinc-200 transition-colors shadow-2xs"
                    :disabled="openingSqliteRunId === run.runId"
                    @click="openSqliteFolder(run)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 19a2 2 0 01-2-2V7a2 2 0 012-2h4l2 2h4a2 2 0 012 2v1M5 19h14a2 2 0 002-2v-5M5 19V9m14 0h2a2 2 0 012 2v3m-2-3H9m12 3H9" />
                    </svg>
                    <span>{{ openingSqliteRunId === run.runId ? "Opening..." : "Open file location" }}</span>
                  </button>
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 px-2.5 py-1 text-[11px] font-bold rounded border border-zinc-200 bg-white hover:bg-zinc-50 text-zinc-700 dark:border-white/10 dark:bg-zinc-900 dark:hover:bg-zinc-800 dark:text-zinc-200 transition-colors shadow-2xs"
                    :disabled="vacuumingRunId === run.runId"
                    @click="vacuumSqlite(run)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M19.428 15.428a2 2 0 00-1.022-.547l-2.387-.477a6 6 0 00-3.86.517l-.318.158a6 6 0 01-3.86.517L6.05 15.21a2 2 0 00-1.806.547M8 4h8l-1 1v5.172a2 2 0 00.586 1.414l5 5c1.26 1.26.367 3.414-1.415 3.414H4.828c-1.782 0-2.674-2.154-1.414-3.414l5-5A2 2 0 009 10.172V5L8 4z" />
                    </svg>
                    <span>{{ vacuumingRunId === run.runId ? "Vacuuming..." : "Vacuum DB copy" }}</span>
                  </button>
                </div>
              </div>
              <div class="flex items-center justify-between gap-2.5 bg-zinc-50 dark:bg-zinc-950 px-2.5 py-1.5 rounded-lg border border-zinc-200/50 dark:border-white/5 min-w-0">
                <span class="font-mono text-[10px] text-zinc-500 dark:text-zinc-400 truncate select-all block" :title="`${run.sourceDir}/informer_object_cache.db`">
                  {{ run.sourceDir }}/informer_object_cache.db
                </span>
                <button
                  type="button"
                  class="shrink-0 p-1 rounded text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 hover:bg-zinc-200/50 dark:hover:bg-zinc-800/80 transition-colors"
                  title="Copy path"
                  @click="copyText(`${run.sourceDir}/informer_object_cache.db`, 'SQLite DB path copied.')"
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

        <!-- Failure error log segment -->
        <div v-if="run.error" class="text-sm font-semibold text-rose-800 dark:text-rose-200 bg-rose-500/5 p-4 rounded-xl border border-rose-500/10 flex items-start gap-2.5">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-rose-500 shrink-0 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span class="leading-6">{{ run.error }}</span>
        </div>

        <!-- Card Footer Actions Bar -->
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between border-t border-zinc-200/50 pt-4 dark:border-white/5">
          <!-- Positive operations -->
          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg bg-emerald-500 hover:bg-emerald-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-sm shadow-emerald-500/10 transition disabled:opacity-50"
              :disabled="!endpointUrl(run) || run.status !== 'serving'"
              @click="openEndpoint(run)"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 mr-1.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
              </svg>
              <span>Open endpoint</span>
            </button>
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white hover:bg-zinc-100 px-3.5 py-1.5 text-xs font-semibold text-zinc-700 dark:border-white/10 dark:bg-white/[0.06] dark:hover:bg-white/[0.1] dark:text-zinc-200 disabled:opacity-50 transition"
              :disabled="!endpointUrl(run)"
              @click="copyEndpoint(run)"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 mr-1.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m-2-4h1a2 2 0 012 2v2m-6 4h3a2 2 0 002-2V9a2 2 0 00-2-2h-3" />
              </svg>
              <span>Copy URL</span>
            </button>
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white hover:bg-zinc-100 px-3.5 py-1.5 text-xs font-semibold text-zinc-700 dark:border-white/10 dark:bg-white/[0.06] dark:hover:bg-white/[0.1] dark:text-zinc-200 disabled:opacity-50 transition"
              :disabled="!run.stevePid || rowActionRunning"
              @click="stopEndpoint(run)"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 mr-1.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
              </svg>
              <span>{{ rowActionLabel(run, "stop", "Stop endpoint", "Stopping...") }}</span>
            </button>
          </div>

          <!-- Cleanups and destructive choices -->
          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white hover:bg-zinc-100 px-3.5 py-1.5 text-xs font-semibold text-zinc-700 dark:border-white/10 dark:bg-white/[0.06] dark:hover:bg-white/[0.1] dark:text-zinc-200 disabled:opacity-50 transition"
              :disabled="rowActionRunning"
              @click="cleanupRun(run, false, true)"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 mr-1.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
              <span>{{ rowActionLabel(run, "delete-k3d", "Delete k3d", "Deleting...") }}</span>
            </button>
            <button
              type="button"
              class="inline-flex min-h-9 items-center justify-center rounded-lg bg-rose-500 hover:bg-rose-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-sm shadow-rose-500/10 transition disabled:opacity-50"
              :disabled="rowActionRunning"
              @click="cleanupRun(run, true, true)"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 mr-1.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              <span>{{ rowActionLabel(run, "delete-all", "Delete all", "Deleting...") }}</span>
            </button>
          </div>
        </div>
      </div>

      <!-- No runs placeholder card -->
      <div v-if="!runs.length" class="rounded-2xl border border-zinc-200 bg-zinc-50 p-5 text-sm text-zinc-500 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-400">
        No Steve Lab runs records found yet.
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { streamSteveLogs } from "./store.js";

const setupData = JSON.parse(document.getElementById("control-panel-data")?.textContent || "{}");
const token = setupData.token || "";

const state = ref({ preflight: { ready: false, summary: "Checking...", items: [] }, operation: { output: [] }, runs: [], k3sVersions: [] });
const versions = ref({ tags: [] });
const refDetails = ref({});
const selectedTag = ref("");
const steveRef = ref("");
const k3sVersion = ref("");
const httpsPort = ref("");
const enableMetrics = ref(false);
const metricsUpdateInterval = ref(15);
const extraEnv = ref("");
const extraArgs = ref("");
const versionsLoading = ref(false);
const notice = ref("");
const noticeKind = ref("info");
const starting = ref(false);
const stopping = ref(false);
const savingKubeconfigRunId = ref("");
const clearingOutput = ref(false);
const outputCollapsed = ref(false);
const activeRowAction = ref({ runId: "", action: "" });
const preflightCollapsed = ref(true);
const toasts = ref([]);
let timer = null;
let refTimer = null;

const addToast = (message, kind = "success") => {
  const id = Date.now() + Math.random().toString(36).substr(2, 9);
  toasts.value.push({ id, message, kind });
  window.setTimeout(() => {
    toasts.value = toasts.value.filter(t => t.id !== id);
  }, 4000);
};

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

const startDisabled = computed(() => starting.value || operation.value.running || !steveRef.value || !k3sVersion.value);
const startButtonLabel = computed(() => {
  if (starting.value) {
    return activeRun.value ? "Replacing..." : "Launching...";
  }
  return activeRun.value ? "Replace endpoint" : "Launch endpoint";
});
const outputText = computed(() => {
  if (streamingLogRunId.value) {
    return steveLogsText.value;
  }
  return (Array.isArray(operation.value.output) && operation.value.output.length)
    ? operation.value.output.join("\n")
    : "Steve Lab output will appear here.";
});
const hasOutput = computed(() => {
  if (streamingLogRunId.value) return Boolean(steveLogsText.value);
  const out = operation.value?.output;
  return Array.isArray(out) && out.length > 0;
});
const clearDisabled = computed(() => {
  if (clearingOutput.value) return true;
  if (streamingLogRunId.value) {
    return !steveLogsText.value;
  }
  return !hasOutput.value;
});
const inputsDisabled = computed(() => starting.value || operation.value.running || stopping.value);

const tagOptions = computed(() => {
  const list = [...(versions.value?.tags || [])];
  if (selectedTag.value && !list.some(t => t.name === selectedTag.value)) {
    list.unshift({ name: selectedTag.value });
  }
  if (steveRef.value && !list.some(t => t.name === steveRef.value)) {
    list.unshift({ name: steveRef.value });
  }
  const seen = new Set();
  return list.filter(item => {
    if (!item?.name) return false;
    if (seen.has(item.name)) return false;
    seen.add(item.name);
    return true;
  });
});
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

const parsedExtraEnv = () => extraEnv.value
  .split(/\r?\n/)
  .map(line => line.trim())
  .filter(Boolean);

const parsedExtraArgs = () => extraArgs.value
  .trim()
  .split(/\s+/)
  .map(arg => arg.trim())
  .filter(Boolean);

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
    const msg = error instanceof Error ? error.message : "Failed to load Steve tags.";
    setNotice(msg, "error");
    addToast(msg, "error");
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
  if (!preflight.value.ready) {
    const msg = "Preflight checks failed: please verify that Docker is running and all required tools are installed.";
    setNotice(msg, "error");
    addToast(msg, "error");
    return;
  }
  const replacing = Boolean(activeRun.value);
  starting.value = true;
  setNotice("");
  stopLogStreaming();
  try {
    await apiFetch("/api/steve/start", {
      method: "POST",
      body: JSON.stringify({
        steveRef: steveRef.value,
        k3sVersion: k3sVersion.value,
        keepCluster: true,
        httpsPort: Number(httpsPort.value || 0),
        headerAuth: true,
        enableMetrics: Boolean(enableMetrics.value),
        metricsUpdateIntervalSeconds: Number(metricsUpdateInterval.value || 15),
        extraEnv: parsedExtraEnv(),
        extraArgs: parsedExtraArgs(),
        replace: replacing,
      }),
    });
    const successMsg = replacing ? "Replacing Steve endpoint." : "Steve endpoint startup started.";
    setNotice(successMsg);
    addToast(successMsg, "success");
    await refreshState();
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to start Steve Lab.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
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
    const stopMsg = "Stop requested for Steve Lab startup.";
    setNotice(stopMsg);
    addToast(stopMsg, "warning");
    await refreshState();
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to stop Steve Lab.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
  } finally {
    stopping.value = false;
  }
};

const clearOutput = async () => {
  clearingOutput.value = true;
  try {
    if (streamingLogRunId.value) {
      steveLogsText.value = "";
      addToast("Log stream console cleared.", "success");
    } else {
      await apiFetch("/api/steve/output/clear", { method: "POST", body: "{}" });
      if (state.value.operation) {
        state.value.operation.output = [];
      }
      addToast("Console output cleared.", "success");
      await refreshState();
    }
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to clear Steve Lab output.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
  } finally {
    clearingOutput.value = false;
  }
};

const cleanupRun = async (run, deleteDir, deleteK3d) => {
  activeRowAction.value = { runId: run.runId, action: deleteDir ? "delete-all" : "delete-k3d" };
  const actionMsg = deleteDir ? "Deleting Steve Lab files and k3d cluster..." : "Deleting Steve Lab k3d cluster...";
  setNotice(actionMsg);
  addToast(actionMsg, "info");
  try {
    await apiFetch("/api/steve/cleanup", {
      method: "POST",
      body: JSON.stringify({ runId: run.runId, deleteDir, deleteK3d }),
    });
    
    if (streamingLogRunId.value === run.runId) {
      stopLogStreaming();
    }

    // Auto clear console output in the backend
    try {
      await apiFetch("/api/steve/output/clear", { method: "POST", body: "{}" });
      if (state.value.operation) {
        state.value.operation.output = [];
      }
    } catch (_) {
      // Ignore failures clearing startup output during delete operations
    }

    const doneMsg = deleteDir ? "Steve Lab run deleted." : "Steve Lab k3d cluster deleted.";
    setNotice(doneMsg);
    addToast(doneMsg, "success");
    await refreshState();
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Cleanup failed.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
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
    const stopMsg = "Steve endpoint stopped.";
    setNotice(stopMsg);
    addToast(stopMsg, "success");
    await refreshState();
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to stop endpoint.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
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
    addToast(message, "success");
  } catch {
    setNotice(value);
    addToast(value, "info");
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
    addToast("Endpoint opened.", "success");
  } catch (error) {
    window.open(url, "_blank", "noopener,noreferrer");
    const fallbackMsg = error instanceof Error ? `Tried browser fallback: ${error.message}` : "Tried browser fallback.";
    setNotice(fallbackMsg);
    addToast(fallbackMsg, "info");
  }
};

const saveKubeconfig = async run => {
  savingKubeconfigRunId.value = run.runId;
  addToast("Requesting kubeconfig download...", "info");
  try {
    const saved = await apiFetch("/api/steve/kubeconfig/save", {
      method: "POST",
      body: JSON.stringify({ runId: run.runId }),
    });
    const successMsg = `${saved.filename || "Kubeconfig"} saved to Downloads.`;
    setNotice(successMsg);
    addToast(successMsg, "success");
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to save kubeconfig.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
  } finally {
    savingKubeconfigRunId.value = "";
  }
};

const compactPath = value => {
  const text = String(value || "");
  return text.length <= 72 ? text : `${text.slice(0, 28)}...${text.slice(-36)}`;
};

const timeLabel = value => value ? new Date(value).toLocaleString() : "";

const runtimeOverrideCount = run => (
  (Array.isArray(run?.extraEnv) ? run.extraEnv.length : 0) +
  (Array.isArray(run?.extraArgs) ? run.extraArgs.length : 0)
);

const runtimeOverrideTitle = run => {
  const envCount = Array.isArray(run?.extraEnv) ? run.extraEnv.length : 0;
  const argCount = Array.isArray(run?.extraArgs) ? run.extraArgs.length : 0;
  return `${envCount} env var${envCount === 1 ? "" : "s"}, ${argCount} arg${argCount === 1 ? "" : "s"}`;
};

const openingSqliteRunId = ref("");
const vacuumingRunId = ref("");

const openSqliteFolder = async run => {
  if (!run.sourceDir) return;
  openingSqliteRunId.value = run.runId;
  try {
    const dbPath = `${run.sourceDir}/informer_object_cache.db`;
    await apiFetch("/api/open-path", {
      method: "POST",
      body: JSON.stringify({ path: dbPath, reveal: true }),
    });
    const successMsg = "Opened SQLite cache folder.";
    setNotice(successMsg);
    addToast(successMsg, "success");
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to open folder.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
  } finally {
    openingSqliteRunId.value = "";
  }
};

const vacuumSqlite = async run => {
  vacuumingRunId.value = run.runId;
  addToast("Starting database vacuum copy...", "info");
  try {
    const result = await apiFetch("/api/steve/sqlite/vacuum", {
      method: "POST",
      body: JSON.stringify({ runId: run.runId }),
    });
    const successMsg = `Database copy saved: ${result.filename || "steve-cache.db"} in Downloads.`;
    setNotice(successMsg);
    addToast(successMsg, "success");
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to vacuum database.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
  } finally {
    vacuumingRunId.value = "";
  }
};

const streamingLogRunId = ref("");
const steveLogsText = ref("");
let logStreamTimer = null;

const startLogStreaming = run => {
  if (logStreamTimer) {
    window.clearInterval(logStreamTimer);
    logStreamTimer = null;
  }

  if (streamingLogRunId.value === run.runId) {
    streamingLogRunId.value = "";
    steveLogsText.value = "";
    addToast("Stopped log streaming.", "info");
    return;
  }

  streamingLogRunId.value = run.runId;
  steveLogsText.value = "Connecting to log stream...";
  addToast(`Streaming steve.log for ${run.runId} to console above.`, "info");

  // Scroll to terminal console smoothly
  const consoleElem = document.getElementById("terminalConsole");
  if (consoleElem) {
    consoleElem.scrollIntoView({ behavior: "smooth", block: "center" });
  }

  const poll = async () => {
    try {
      const params = new URLSearchParams({ runId: run.runId });
      const response = await apiFetch(`/api/steve/logs?${params.toString()}`);
      if (streamingLogRunId.value !== run.runId) return;
      steveLogsText.value = response.text || "(empty log file)";
    } catch (error) {
      if (streamingLogRunId.value !== run.runId) return;
      steveLogsText.value = `Error loading logs: ${error instanceof Error ? error.message : error}`;
    }
  };

  poll();
  logStreamTimer = window.setInterval(poll, 3000);
};

const stopLogStreaming = () => {
  streamingLogRunId.value = "";
  steveLogsText.value = "";
  if (logStreamTimer) {
    window.clearInterval(logStreamTimer);
    logStreamTimer = null;
  }
  addToast("Stopped log streaming.", "info");
};

const refreshConsole = async () => {
  if (streamingLogRunId.value) {
    addToast("Refreshing log stream...", "info");
    try {
      const params = new URLSearchParams({ runId: streamingLogRunId.value });
      const response = await apiFetch(`/api/steve/logs?${params.toString()}`);
      steveLogsText.value = response.text || "(empty log file)";
      addToast("Log stream refreshed.", "success");
    } catch (error) {
      addToast(error instanceof Error ? error.message : "Failed to refresh logs.", "error");
    }
  } else {
    addToast("Refreshing console output...", "info");
    await refreshState();
  }
};

const openKubeconfigFolder = async run => {
  if (!run.kubeconfig) return;
  try {
    await apiFetch("/api/open-path", {
      method: "POST",
      body: JSON.stringify({ path: run.kubeconfig, reveal: true }),
    });
    const successMsg = "Opened kubeconfig location.";
    setNotice(successMsg);
    addToast(successMsg, "success");
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to open folder.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
  }
};

const openLogFolder = async run => {
  if (!run.logPath) return;
  try {
    await apiFetch("/api/open-path", {
      method: "POST",
      body: JSON.stringify({ path: run.logPath, reveal: true }),
    });
    const successMsg = "Opened log location.";
    setNotice(successMsg);
    addToast(successMsg, "success");
  } catch (error) {
    const errMsg = error instanceof Error ? error.message : "Failed to open folder.";
    setNotice(errMsg, "error");
    addToast(errMsg, "error");
  }
};

const preflightItems = computed(() => Array.isArray(preflight.value?.items) ? preflight.value.items : []);

const preflightStatusLabel = computed(() => {
  if (!preflight.value?.items?.length) return "Checking...";
  const errors = preflightItems.value.filter(i => i.status === "error").length;
  const warnings = preflightItems.value.filter(i => i.status === "warning").length;
  if (errors > 0) return `${errors} blocking tool${errors === 1 ? "" : "s"}`;
  if (warnings > 0) return `${warnings} warning${warnings === 1 ? "" : "s"}`;
  return "Ready";
});

const preflightStatusClass = computed(() => {
  const errors = preflightItems.value.filter(i => i.status === "error").length;
  const warnings = preflightItems.value.filter(i => i.status === "warning").length;
  let tone = "success";
  if (errors > 0) tone = "error";
  else if (warnings > 0) tone = "warning";
  
  return {
    success: "inline-flex items-center justify-center rounded-full bg-emerald-100 px-3 py-1 text-xs font-bold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300 border border-emerald-500/20",
    warning: "inline-flex items-center justify-center rounded-full bg-amber-100 px-3 py-1 text-xs font-bold text-amber-700 dark:bg-amber-500/15 dark:text-amber-300 border border-amber-500/20",
    error: "inline-flex items-center justify-center rounded-full bg-rose-100 px-3 py-1 text-xs font-bold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300 border border-rose-500/20"
  }[tone];
});

const preflightItemClass = status => ({
  ok: "border-emerald-200 bg-emerald-50/50 text-emerald-800 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-200",
  warning: "border-amber-200 bg-amber-50/50 text-amber-800 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-200",
  error: "border-rose-200 bg-rose-50/50 text-rose-800 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-200"
})[status] || "border-zinc-200 bg-white text-zinc-700 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300";

onMounted(async () => {
  await refreshState();
  await loadVersions();
  timer = window.setInterval(refreshState, 4000);
});

onUnmounted(() => {
  window.clearInterval(timer);
  window.clearTimeout(refTimer);
  window.clearInterval(logStreamTimer);
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
