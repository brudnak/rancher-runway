<template>
  <div id="gpuReminderModal" class="fixed inset-0 z-[60] hidden items-center justify-center bg-zinc-950/55 p-4 backdrop-blur-sm dark:bg-zinc-950/80" role="dialog" aria-modal="true" aria-labelledby="gpuReminderTitle">
    <section class="w-full max-w-xl overflow-hidden rounded-2xl border border-rose-200 bg-white shadow-2xl shadow-zinc-950/20 dark:border-rose-500/25 dark:bg-zinc-900 dark:shadow-black/50">
      <div class="border-b border-rose-100 px-6 py-5 dark:border-rose-500/20">
        <div class="mb-3 inline-flex rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300">Cost reminder</div>
        <h2 id="gpuReminderTitle" class="text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">GPU infrastructure active</h2>
        <p id="gpuReminderBody" class="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">Are you still using the GPU worker node?</p>
      </div>
      <div class="flex flex-wrap justify-end gap-3 px-6 py-4">
        <button id="gpuReminderSettingsBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Reminder settings</button>
        <button id="gpuReminderDismissBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Yes, still using</button>
        <button id="gpuReminderCleanupBtn" type="button" class="rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400">Go to destroy</button>
      </div>
    </section>
  </div>

  <div id="logModal" class="fixed inset-0 z-50 hidden bg-zinc-950/70 p-3 backdrop-blur-sm sm:p-5" role="dialog" aria-modal="true" aria-labelledby="logModalTitle">
    <section class="mx-auto flex h-full max-w-[1700px] flex-col overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-2xl shadow-zinc-950/30 dark:border-white/10 dark:bg-zinc-950">
      <header class="sticky top-0 z-10 border-b border-zinc-200 bg-white px-4 py-4 dark:border-white/10 dark:bg-zinc-900 sm:px-5">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
          <div class="min-w-0">
            <div id="logModalKind" class="mb-2 inline-flex items-center rounded-full border border-zinc-200 bg-zinc-50 px-2.5 py-1 text-xs font-semibold text-zinc-500 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-400">Pod logs</div>
            <h2 id="logModalTitle" class="break-words text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">No pod selected</h2>
            <p id="logModalSubtitle" class="mt-1 break-words text-sm text-zinc-500 dark:text-zinc-400">Choose Tail or Live from the pod table.</p>
            <div id="liveLogState" class="mt-3 inline-flex items-center gap-2 rounded-full border border-zinc-200 bg-zinc-50 px-3 py-1.5 text-xs font-semibold text-zinc-500 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-400">
              <span id="liveLogStateIcon" class="h-2.5 w-2.5 rounded-full bg-zinc-400"></span>
              <span id="liveLogStateLabel">Idle</span>
            </div>
          </div>
          <div class="flex shrink-0 flex-wrap gap-2">
            <button id="downloadLogsBtn" type="button" class="rounded-lg bg-emerald-500 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400">Download visible logs</button>
            <button id="stopStreamBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-3.5 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Stop live</button>
            <button id="clearLogsBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-3.5 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Clear</button>
            <button id="closeLogModalBtn" type="button" class="rounded-lg border border-zinc-200 bg-white px-3.5 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Close</button>
          </div>
        </div>
        <div class="mt-4 grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
          <div>
            <label for="logSearch" class="sr-only">Search logs</label>
            <input id="logSearch" type="search" autocomplete="off" placeholder="Grep logs..." class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none placeholder:text-zinc-400 focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100 dark:placeholder:text-zinc-500" />
          </div>
          <div id="logMatchCount" class="text-sm font-medium text-zinc-500 dark:text-zinc-400">0 lines</div>
        </div>
        <div id="logLevelFilters" class="mt-3 flex flex-wrap gap-2">
          <button type="button" data-level="all" class="rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300">All</button>
          <button type="button" data-level="info" class="rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.1]">INFO</button>
          <button type="button" data-level="debug" class="rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.1]">DEBUG</button>
          <button type="button" data-level="warning" class="rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.1]">WARNING</button>
          <button type="button" data-level="error" class="rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.1]">ERROR</button>
        </div>
      </header>
      <div id="logBox" class="min-h-0 flex-1 overflow-auto bg-zinc-50 p-3 font-mono text-xs leading-5 text-zinc-800 dark:bg-zinc-950 dark:text-zinc-200 sm:p-4"></div>
    </section>
  </div>

  <div id="dangerConfirmModal" class="fixed inset-0 z-[60] hidden items-center justify-center bg-zinc-950/55 p-4 backdrop-blur-sm dark:bg-zinc-950/80" role="dialog" aria-modal="true" aria-labelledby="dangerConfirmTitle">
    <section class="w-full max-w-lg rounded-2xl border border-zinc-200 bg-white p-6 shadow-2xl shadow-zinc-950/20 dark:border-white/10 dark:bg-zinc-900 dark:shadow-black/50">
      <div id="dangerConfirmAccent" class="mb-4 inline-flex rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300">Confirmation required</div>
      <h2 id="dangerConfirmTitle" class="text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50"></h2>
      <p id="dangerConfirmBody" class="mt-3 text-sm leading-6 text-zinc-600 dark:text-zinc-300"></p>
      <label class="mt-5 grid gap-2 text-sm font-semibold text-zinc-700 dark:text-zinc-200">
        <span id="dangerConfirmPrompt">Type confirm to continue</span>
        <input id="dangerConfirmInput" type="text" autocomplete="off" class="w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 text-sm font-medium text-zinc-950 outline-none focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100" />
      </label>
      <div id="dangerConfirmError" class="mt-3 min-h-5 text-sm font-semibold text-rose-600 dark:text-rose-300"></div>
      <div class="mt-6 flex flex-wrap justify-end gap-3">
        <button id="dangerConfirmCancel" type="button" class="rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Cancel</button>
        <button id="dangerConfirmSubmit" type="button" class="rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400">Confirm</button>
      </div>
    </section>
  </div>

  <div id="upgradeCommandModal" class="fixed inset-0 z-[60] hidden items-center justify-center bg-zinc-950/55 p-4 backdrop-blur-sm dark:bg-zinc-950/80" role="dialog" aria-modal="true" aria-labelledby="upgradeCommandTitle">
    <section class="w-full max-w-2xl overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-2xl shadow-zinc-950/20 dark:border-white/10 dark:bg-zinc-900 dark:shadow-black/50">
      <div class="border-b border-zinc-200 px-6 py-5 dark:border-white/10">
        <div class="mb-3 inline-flex rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">Clipboard ready</div>
        <h2 id="upgradeCommandTitle" class="text-xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Prepared upgrade command copied</h2>
        <p class="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">The command is ready to edit before running against the local HA cluster.</p>
      </div>
      <div class="grid gap-3 px-6 py-5 text-sm leading-6 text-zinc-700 dark:text-zinc-300">
        <div class="rounded-xl border border-sky-200 bg-sky-50 p-4 text-sky-900 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
          Review the Rancher target values before running it.
        </div>
        <div class="grid gap-2">
          <div class="flex gap-2"><span class="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-sky-500"></span><span>Set the chart <code>--version</code> to the Rancher version you want.</span></div>
          <div class="flex gap-2"><span class="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-sky-500"></span><span>Update any image registry, repository, tag, or legacy image override values if this upgrade needs them.</span></div>
          <div class="flex gap-2"><span class="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-sky-500"></span><span>Run it with the matching kubeconfig context for this HA.</span></div>
        </div>
      </div>
      <div class="flex flex-wrap justify-end gap-3 border-t border-zinc-200 px-6 py-4 dark:border-white/10">
        <button id="upgradeCommandModalClose" type="button" class="rounded-lg bg-sky-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-sky-500/20 hover:bg-sky-400">Got it</button>
      </div>
    </section>
  </div>

  <div id="panelNotice" class="fixed bottom-5 right-5 z-[70] hidden w-[min(28rem,calc(100vw-2.5rem))] rounded-2xl border border-zinc-200 bg-white p-4 shadow-2xl shadow-zinc-950/20 dark:border-white/10 dark:bg-zinc-900 dark:shadow-black/50" role="status" aria-live="polite">
    <div class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <div id="panelNoticeTitle" class="text-sm font-semibold text-zinc-950 dark:text-zinc-50"></div>
        <div id="panelNoticeBody" class="mt-1 break-words text-sm leading-6 text-zinc-600 dark:text-zinc-300"></div>
      </div>
      <button id="panelNoticeClose" type="button" class="shrink-0 rounded-lg border border-zinc-200 bg-white px-2.5 py-1.5 text-xs font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.1]">Dismiss</button>
    </div>
  </div>
</template>
