import {
  clusterItems,
  compactPath,
  escapeHtml,
  highlightLogLine,
  lineMatchesLogLevel,
  operationOutput,
  parseCleanupCost,
} from './control_panel_utils.js'
import {
  createBasicModal,
  createNoticeController,
  createTypedConfirmation
} from './control_panel_modals.js'
import {
  runFolderAvailable,
  runFolderPath,
  runTerraformPath,
  sameRunKey
} from './control_panel_runs.js'
import {
  createClusterPanel
} from './control_panel_clusters.js'

const setupData = JSON.parse(document.getElementById('control-panel-data')?.textContent || '{}')
const token = setupData.token || ''

const commandDeckEl = document.getElementById('commandDeck')
const bootStatusEl = document.getElementById('bootStatus')
const bootStatusDetailEl = document.getElementById('bootStatusDetail')
const panelTabsEl = document.getElementById('panelTabs')
const tabPanelEls = Array.from(document.querySelectorAll('[data-tab-panel]'))
const workspaceRunMetaEl = document.getElementById('workspaceRunMeta')
const clustersSectionEl = document.getElementById('clustersSection')
const clustersEl = document.getElementById('clusters')
const refreshStatusEl = document.getElementById('refreshStatus')
const logStatusEl = document.getElementById('logStatus')
const logBoxEl = document.getElementById('logBox')
const logModalEl = document.getElementById('logModal')
const logModalKindEl = document.getElementById('logModalKind')
const logModalTitleEl = document.getElementById('logModalTitle')
const logModalSubtitleEl = document.getElementById('logModalSubtitle')
const logSearchEl = document.getElementById('logSearch')
const logMatchCountEl = document.getElementById('logMatchCount')
const logLevelFiltersEl = document.getElementById('logLevelFilters')
const liveLogStateEl = document.getElementById('liveLogState')
const liveLogStateIconEl = document.getElementById('liveLogStateIcon')
const liveLogStateLabelEl = document.getElementById('liveLogStateLabel')
const openLogViewerBtnEl = document.getElementById('openLogViewerBtn')
const stopStreamBtnEl = document.getElementById('stopStreamBtn')
const refreshPreflightBtnEl = document.getElementById('refreshPreflightBtn')
const setupStatusEl = document.getElementById('setupStatus')
const setupMetaEl = document.getElementById('setupMeta')
const setupBtnEl = document.getElementById('setupBtn')
const openSetupLogsBtnEl = document.getElementById('openSetupLogsBtn')
const readinessStatusEl = document.getElementById('readinessStatus')
const readinessMetaEl = document.getElementById('readinessMeta')
const readinessBtnEl = document.getElementById('readinessBtn')
const openReadinessLogsBtnEl = document.getElementById('openReadinessLogsBtn')
const cleanupStatusEl = document.getElementById('cleanupStatus')
const cleanupActionsEl = document.getElementById('cleanupActions')
const cleanupSlotsEl = document.getElementById('cleanupSlots')
const cleanupConfirmEl = document.getElementById('cleanupConfirm')
const cleanupBtnEl = document.getElementById('cleanupBtn')
const openCleanupLogsBtnEl = document.getElementById('openCleanupLogsBtn')
const cleanupClearResultBtnEl = document.getElementById('cleanupClearResultBtn')
const cleanupCostEl = document.getElementById('cleanupCost')
const destroySlotsTabBtnEl = document.getElementById('destroySlotsTabBtn')
const destroyCostsTabBtnEl = document.getElementById('destroyCostsTabBtn')
const destroySlotsPaneEl = document.getElementById('destroySlotsPane')
const destroyCostsPaneEl = document.getElementById('destroyCostsPane')
const resetCostLedgerBtnEl = document.getElementById('resetCostLedgerBtn')
const costResetStatusEl = document.getElementById('costResetStatus')
const cleanLocalArtifactsBtnEl = document.getElementById('cleanLocalArtifactsBtn')
const localArtifactsStatusEl = document.getElementById('localArtifactsStatus')
const fullscreenToggleEl = document.getElementById('fullscreenToggle')
const fullscreenEnterIconEl = document.getElementById('fullscreenEnterIcon')
const fullscreenExitIconEl = document.getElementById('fullscreenExitIcon')
const fullscreenToggleLabelEl = document.getElementById('fullscreenToggleLabel')
const themeToggleEl = document.getElementById('themeToggle')
const themeSunIconEl = document.getElementById('themeSunIcon')
const themeMoonIconEl = document.getElementById('themeMoonIcon')
const themeToggleLabelEl = document.getElementById('themeToggleLabel')
const stopBtnEl = document.getElementById('stopBtn')
const refreshBtnEl = document.getElementById('refreshBtn')
const dangerConfirmModalEl = document.getElementById('dangerConfirmModal')
const dangerConfirmAccentEl = document.getElementById('dangerConfirmAccent')
const dangerConfirmTitleEl = document.getElementById('dangerConfirmTitle')
const dangerConfirmBodyEl = document.getElementById('dangerConfirmBody')
const dangerConfirmPromptEl = document.getElementById('dangerConfirmPrompt')
const dangerConfirmInputEl = document.getElementById('dangerConfirmInput')
const dangerConfirmErrorEl = document.getElementById('dangerConfirmError')
const dangerConfirmCancelEl = document.getElementById('dangerConfirmCancel')
const dangerConfirmSubmitEl = document.getElementById('dangerConfirmSubmit')
const panelNoticeEl = document.getElementById('panelNotice')
const panelNoticeTitleEl = document.getElementById('panelNoticeTitle')
const panelNoticeBodyEl = document.getElementById('panelNoticeBody')
const panelNoticeCloseEl = document.getElementById('panelNoticeClose')
const gpuReminderIntervalEls = Array.from(document.querySelectorAll('[data-gpu-reminder-interval]'))
const gpuReminderEnableBtnEl = document.getElementById('gpuReminderEnableBtn')
const gpuReminderDisableBtnEl = document.getElementById('gpuReminderDisableBtn')
const gpuReminderModalEl = document.getElementById('gpuReminderModal')
const gpuReminderBodyEl = document.getElementById('gpuReminderBody')
const gpuReminderSettingsBtnEl = document.getElementById('gpuReminderSettingsBtn')
const gpuReminderDismissBtnEl = document.getElementById('gpuReminderDismissBtn')
const gpuReminderCleanupBtnEl = document.getElementById('gpuReminderCleanupBtn')
const upgradeCommandModalEl = document.getElementById('upgradeCommandModal')
const upgradeCommandModalCloseEl = document.getElementById('upgradeCommandModalClose')

let stream = null
let streamPollTimer = null
let livePollGeneration = 0
let lastState = null
let activeDownloadClusterId = ''
let activeCopyClusterId = ''
let activeCopyHelmClusterId = ''
let activeCopyHelmUpgradeClusterId = ''
let activeOpenKubeconfigPathClusterId = ''
let activeCopyKubeconfigPathClusterId = ''
let activeCopyLinodeIPClusterId = ''
let activeDockerLogsClusterId = ''
let lastLeaderChangeMessage = ''
let refreshInFlight = false
let rawLogText = ''
let visibleLogText = ''
let activeLogContext = null
let activeLogLevel = 'all'
let liveLogState = 'idle'
let panelFullscreen = false
let preflightState = { ready: false, summary: 'Preflight has not run yet.', items: [] }
let preflightInFlight = false
let selectedCleanupRunId = ''
let pendingAbortOperation = ''
let cleanupStarting = false
let cleanupDismissedResultKey = ''
let costResetting = false
let localArtifactsCleaning = false
let setupLaunchPendingUntil = 0
let activeClusterRunKey = ''
let activeClusterHAKey = ''
let activePanelTab = localStorage.getItem('rancherControlPanelTab') || 'setup'
if (activePanelTab === 'lifecycle') {
  activePanelTab = 'runs'
}
let activeDestroyTab = localStorage.getItem('rancherDestroyTab') || 'slots'
let bootStatePending = true

const gpuReminderSettingsKey = 'rancherGpuReminderSettings'
const gpuReminderIntervals = [15, 30, 60]
const loadGPUReminderSettings = () => {
  try {
    const parsed = JSON.parse(localStorage.getItem(gpuReminderSettingsKey) || '{}')
    const intervalMinutes = gpuReminderIntervals.includes(Number(parsed.intervalMinutes)) ? Number(parsed.intervalMinutes) : 15
    return {
      intervalMinutes,
      disabled: Boolean(parsed.disabled),
      lastReminderAt: Number(parsed.lastReminderAt || 0)
    }
  } catch {
    return { intervalMinutes: 15, disabled: false, lastReminderAt: 0 }
  }
}
let gpuReminderSettings = loadGPUReminderSettings()
window.rancherGpuReminderSettings = gpuReminderSettings

const requestTypedConfirmation = createTypedConfirmation({
  modalEl: dangerConfirmModalEl,
  accentEl: dangerConfirmAccentEl,
  titleEl: dangerConfirmTitleEl,
  bodyEl: dangerConfirmBodyEl,
  promptEl: dangerConfirmPromptEl,
  inputEl: dangerConfirmInputEl,
  errorEl: dangerConfirmErrorEl,
  cancelEl: dangerConfirmCancelEl,
  submitEl: dangerConfirmSubmitEl
})
const panelNotice = createNoticeController({
  noticeEl: panelNoticeEl,
  titleEl: panelNoticeTitleEl,
  bodyEl: panelNoticeBodyEl,
  closeEl: panelNoticeCloseEl,
  fallback: (title, body) => {
    refreshStatusEl.textContent = `${title}: ${body}`
  }
})
const showPanelNotice = panelNotice.show
const upgradeCommandModal = createBasicModal({
  modalEl: upgradeCommandModalEl,
  closeEl: upgradeCommandModalCloseEl,
  unavailable: () => showPanelNotice(
    'Prepared upgrade copied',
    'Edit the chart version and any image override values before running the copied command.'
  )
})
const closeUpgradeCommandModal = upgradeCommandModal.close
const showUpgradeCommandModal = upgradeCommandModal.show
const clusterPanel = createClusterPanel({
  clustersEl,
  getActionState: () => ({
    activeDownloadClusterId,
    activeCopyClusterId,
    activeCopyHelmClusterId,
    activeCopyHelmUpgradeClusterId,
    activeOpenKubeconfigPathClusterId,
    activeCopyKubeconfigPathClusterId,
    activeCopyLinodeIPClusterId,
    activeDockerLogsClusterId
  }),
  getActiveSelection: () => ({
    runKey: activeClusterRunKey,
    haKey: activeClusterHAKey
  }),
  setActiveSelection: selection => {
    activeClusterRunKey = selection.runKey || ''
    activeClusterHAKey = selection.haKey || ''
  },
  getLastState: () => lastState,
  renderLastState: () => {
    if (lastState) {
      renderClusters(lastState)
    }
  },
  cleanupResultDismissed: cleanup => cleanupResultDismissed(cleanup)
})
const renderClusters = state => clusterPanel.renderClusters(state)
const updateLeaderTracking = state => {
  lastLeaderChangeMessage = clusterPanel.updateLeaderTracking(state)
}

const currentTheme = () => document.documentElement.classList.contains('dark') ? 'dark' : 'light'

const wailsRuntime = () => window.runtime || null

const syncFullscreenButton = async () => {
  let nativeFullscreen = false
  const runtime = wailsRuntime()
  if (runtime?.WindowIsFullscreen) {
    try {
      nativeFullscreen = Boolean(await runtime.WindowIsFullscreen())
    } catch (_) {
      nativeFullscreen = false
    }
  }

  panelFullscreen = Boolean(nativeFullscreen || document.fullscreenElement)
  document.body.dataset.panelFullscreen = panelFullscreen ? 'true' : 'false'
  fullscreenEnterIconEl?.classList.toggle('hidden', panelFullscreen)
  fullscreenExitIconEl?.classList.toggle('hidden', !panelFullscreen)
  if (fullscreenToggleLabelEl) {
    fullscreenToggleLabelEl.textContent = panelFullscreen ? 'Exit full screen' : 'Fullscreen'
  }
  if (fullscreenToggleEl) {
    fullscreenToggleEl.title = panelFullscreen ? 'Exit fullscreen' : 'Enter fullscreen'
    fullscreenToggleEl.setAttribute('aria-pressed', panelFullscreen ? 'true' : 'false')
  }
}

const setPanelFullscreen = async nextFullscreen => {
  const runtime = wailsRuntime()
  try {
    if (runtime?.WindowFullscreen && runtime?.WindowUnfullscreen) {
      if (nextFullscreen) {
        await runtime.WindowFullscreen()
      } else {
        await runtime.WindowUnfullscreen()
      }
    } else if (document.fullscreenEnabled) {
      if (nextFullscreen && !document.fullscreenElement) {
        await document.documentElement.requestFullscreen()
      } else if (!nextFullscreen && document.fullscreenElement) {
        await document.exitFullscreen()
      }
    } else {
      document.body.dataset.panelFullscreen = nextFullscreen ? 'true' : 'false'
      panelFullscreen = Boolean(nextFullscreen)
    }
  } catch (error) {
    refreshStatusEl.textContent = error instanceof Error ? error.message : 'Fullscreen request failed'
  } finally {
    window.setTimeout(syncFullscreenButton, 120)
  }
}

const setActivePanelTab = tab => {
  const availableTabs = new Set(['setup', 'runs', 'clusters', 'aws', 'destroy', 'settings', 'k3d', 'steve'])
  activePanelTab = availableTabs.has(tab) ? tab : 'runs'
  localStorage.setItem('rancherControlPanelTab', activePanelTab)
  tabPanelEls.forEach(panel => {
    panel.classList.toggle('hidden', panel.dataset.tabPanel !== activePanelTab)
  })
  window.dispatchEvent(new CustomEvent('rancher-control-panel:tab', {
    detail: { tab: activePanelTab }
  }))
  if (activePanelTab === 'setup' && lastState) {
    dispatchSetupLifecycleState(lastState)
  }
}

const setActiveDestroyTab = tab => {
  activeDestroyTab = tab === 'costs' ? 'costs' : 'slots'
  localStorage.setItem('rancherDestroyTab', activeDestroyTab)
  destroySlotsPaneEl?.classList.toggle('hidden', activeDestroyTab !== 'slots')
  destroyCostsPaneEl?.classList.toggle('hidden', activeDestroyTab !== 'costs')
  const activeClass = 'rounded-lg bg-white px-3.5 py-2 text-sm font-semibold text-zinc-900 shadow-sm dark:bg-white/[0.08] dark:text-zinc-100'
  const inactiveClass = 'rounded-lg px-3.5 py-2 text-sm font-semibold text-zinc-600 hover:bg-white dark:text-zinc-300 dark:hover:bg-white/[0.06]'
  if (destroySlotsTabBtnEl) {
    destroySlotsTabBtnEl.className = activeDestroyTab === 'slots' ? activeClass : inactiveClass
  }
  if (destroyCostsTabBtnEl) {
    destroyCostsTabBtnEl.className = activeDestroyTab === 'costs' ? activeClass : inactiveClass
  }
}

window.addEventListener('message', event => {
  const data = event.data || {}
  if (data.type !== 'ha-rancher-open-panel-tab') {
    return
  }
  if (data.tab === 'destroy') {
    openDestroySlots()
  }
})

const setTheme = (theme, persist = true) => {
  document.documentElement.classList.toggle('dark', theme === 'dark')
  document.body.classList.toggle('dark', theme === 'dark')
  if (persist) {
    localStorage.setItem('rancherControlPanelTheme', theme)
  }

  themeSunIconEl.classList.toggle('hidden', theme !== 'dark')
  themeMoonIconEl.classList.toggle('hidden', theme !== 'light')
  themeToggleLabelEl.textContent = theme === 'dark' ? 'Light' : 'Dark'
}

const setBootState = (pending, detail = '') => {
  bootStatePending = Boolean(pending)
  document.body.dataset.booting = bootStatePending ? 'true' : 'false'
  if (bootStatusEl) {
    bootStatusEl.classList.toggle('hidden', !bootStatePending)
  }
  if (bootStatusDetailEl && detail) {
    bootStatusDetailEl.textContent = detail
  }

  const disabled = bootStatePending
  const actionButtons = [
    stopBtnEl,
    setupBtnEl,
    openSetupLogsBtnEl,
    readinessBtnEl,
    openReadinessLogsBtnEl,
    cleanupBtnEl,
    openCleanupLogsBtnEl,
    cleanupClearResultBtnEl,
    openLogViewerBtnEl
  ].filter(Boolean)

  actionButtons.forEach(button => {
    button.disabled = disabled
    if (disabled) {
      button.dataset.bootDisabled = 'true'
      button.title = 'Startup safety check is still loading panel state.'
    } else if (button.dataset.bootDisabled === 'true') {
      delete button.dataset.bootDisabled
      button.removeAttribute('title')
    }
  })

  if (readinessBtnEl) {
    if (!readinessBtnEl.dataset.defaultLabel) {
      readinessBtnEl.dataset.defaultLabel = readinessBtnEl.innerHTML
    }
    if (bootStatePending) {
      readinessBtnEl.innerHTML = '<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>Checking state'
    } else if (readinessBtnEl.dataset.defaultLabel) {
      readinessBtnEl.innerHTML = readinessBtnEl.dataset.defaultLabel
    }
  }

  if (stopBtnEl) {
    if (!stopBtnEl.dataset.defaultLabel) {
      stopBtnEl.dataset.defaultLabel = stopBtnEl.textContent || 'Stop panel'
    }
    stopBtnEl.textContent = bootStatePending ? 'Checking state' : stopBtnEl.dataset.defaultLabel
  }

  setupRootElDispatch({
    booting: bootStatePending,
    detail: detail || 'Checking local config, run slots, Terraform state, lifecycle processes, clusters, and AWS inventory.'
  })
  publishControlPanelVueState(lastState || {})
}

const setupRootElDispatch = detail => {
  dispatchSetupRootEvent('rancher-control-panel-booting', detail)
}

const dispatchSetupRootEvent = (eventName, detail) => {
  const root = document.getElementById('interactiveSetupRoot')
  if (!root) {
    return
  }
  root.dispatchEvent(new CustomEvent(eventName, { detail }))
}

const setActiveLogLevel = level => {
  activeLogLevel = level
  logLevelFiltersEl.querySelectorAll('button[data-level]').forEach(button => {
    const active = button.dataset.level === level
    button.className = active
      ? 'rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300'
      : 'rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.1]'
  })
  renderLogViewer()
}

const setLiveLogState = state => {
  liveLogState = state

  const states = {
    idle: {
      label: 'Idle',
      container: 'border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-400',
      icon: 'bg-zinc-400',
      button: 'Start live'
    },
    connecting: {
      label: 'Connecting to logs...',
      container: 'border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300',
      icon: 'bg-sky-500 animate-ping',
      button: 'Stop live'
    },
    live: {
      label: 'Live logs refreshing',
      container: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300',
      icon: 'bg-emerald-500 animate-pulse',
      button: 'Stop live'
    },
    stopped: {
      label: 'Live refresh paused',
      container: 'border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-300',
      icon: 'bg-zinc-400',
      button: 'Resume live'
    },
    error: {
      label: 'Live refresh interrupted',
      container: 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300',
      icon: 'bg-rose-500',
      button: 'Resume live'
    },
    setupRunning: {
      label: 'Setup running',
      container: 'border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300',
      icon: 'bg-sky-500 animate-pulse',
      button: 'Live disabled'
    },
    setupDone: {
      label: 'Setup completed',
      container: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300',
      icon: 'bg-emerald-500',
      button: 'Live disabled'
    },
    setupError: {
      label: 'Setup failed',
      container: 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300',
      icon: 'bg-rose-500',
      button: 'Live disabled'
    },
    readinessRunning: {
      label: 'Readiness running',
      container: 'border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300',
      icon: 'bg-sky-500 animate-pulse',
      button: 'Live disabled'
    },
    readinessDone: {
      label: 'Readiness completed',
      container: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300',
      icon: 'bg-emerald-500',
      button: 'Live disabled'
    },
    readinessError: {
      label: 'Readiness failed',
      container: 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300',
      icon: 'bg-rose-500',
      button: 'Live disabled'
    },
    cleanupRunning: {
      label: 'Destroy running',
      container: 'border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300',
      icon: 'bg-sky-500 animate-pulse',
      button: 'Live disabled'
    },
    cleanupDone: {
      label: 'Destroy completed',
      container: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300',
      icon: 'bg-emerald-500',
      button: 'Live disabled'
    },
    cleanupError: {
      label: 'Destroy failed',
      container: 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-300',
      icon: 'bg-rose-500',
      button: 'Live disabled'
    }
  }
  const selected = states[state] || states.idle

  liveLogStateEl.className = `mt-3 inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-semibold ${selected.container}`
  liveLogStateIconEl.className = `h-2.5 w-2.5 rounded-full ${selected.icon}`
  liveLogStateLabelEl.textContent = selected.label
  stopStreamBtnEl.textContent = selected.button
  const operationContext = ['setup', 'readiness', 'cleanup'].includes(activeLogContext?.mode)
  stopStreamBtnEl.classList.toggle('hidden', operationContext || state.startsWith('cleanup') || state.startsWith('setup') || state.startsWith('readiness'))
}

const logFilename = () => {
  if (activeLogContext?.mode === 'readiness') {
    const filter = logSearchEl.value.trim() ? '-filtered' : ''
    return `readiness${filter}.log`
  }

  if (activeLogContext?.mode === 'setup' || activeLogContext?.mode === 'linodeSetup') {
    const filter = logSearchEl.value.trim() ? '-filtered' : ''
    return `${activeLogContext.mode === 'linodeSetup' ? 'linode-setup' : 'setup'}${filter}.log`
  }

  if (activeLogContext?.mode === 'cleanup' || activeLogContext?.mode === 'linodeCleanup') {
    const filter = logSearchEl.value.trim() ? '-filtered' : ''
    return `${activeLogContext.mode === 'linodeCleanup' ? 'linode-cleanup' : 'cleanup'}${filter}.log`
  }

  const pod = activeLogContext?.podName || 'pod'
  const mode = activeLogContext?.mode || 'logs'
  const filter = logSearchEl.value.trim() ? '-filtered' : ''
  const safePod = pod.toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '') || 'pod'
  return `${safePod}-${mode}${filter}.log`
}

const openLogModal = () => {
  logModalEl.classList.remove('hidden')
  document.body.classList.add('overflow-hidden')
}

const closeLogModal = () => {
  logModalEl.classList.add('hidden')
  document.body.classList.remove('overflow-hidden')
}

const setActiveLogContext = (mode, clusterId, namespace, podName) => {
  activeLogContext = { mode, clusterId, namespace, podName }
  logModalKindEl.textContent = 'Pod logs'
  logModalTitleEl.textContent = podName
  logModalSubtitleEl.textContent = `${namespace} • ${clusterId} • ${mode === 'live' ? 'live stream' : 'tail snapshot'}`
  openLogViewerBtnEl.classList.remove('hidden')
}

const setDockerLogContext = cluster => {
  const clusterId = cluster?.id || ''
  activeLogContext = { mode: 'docker', clusterId, namespace: 'linode', podName: 'rancher' }
  logModalKindEl.textContent = 'Docker logs'
  logModalTitleEl.textContent = cluster?.name || 'Rancher container'
  logModalSubtitleEl.textContent = cluster?.loadBalancer ? `root@${cluster.loadBalancer} • docker logs rancher` : `${clusterId} • docker logs rancher`
  openLogViewerBtnEl.classList.remove('hidden')
}

const setSetupLogContext = (linode = false) => {
  activeLogContext = { mode: linode ? 'linodeSetup' : 'setup', clusterId: linode ? 'linode' : 'local', namespace: 'terratest', podName: 'setup' }
  logModalKindEl.textContent = linode ? 'Linode setup logs' : 'Setup logs'
  logModalTitleEl.textContent = linode ? 'Linode setup' : 'Setup'
  logModalSubtitleEl.textContent = 'go test -v -run ^TestHaSetup$ -timeout 90m -count=1 ./terratest'
  openLogViewerBtnEl.classList.remove('hidden')
}

const setReadinessLogContext = () => {
  activeLogContext = { mode: 'readiness', clusterId: 'local', namespace: 'terratest', podName: 'readiness' }
  logModalKindEl.textContent = 'Readiness logs'
  logModalTitleEl.textContent = 'Readiness'
  logModalSubtitleEl.textContent = lastState?.readiness?.command || 'go test -v -run ^TestHAWaitReady$ -timeout 35m -count=1 ./terratest'
  openLogViewerBtnEl.classList.remove('hidden')
}

const setCleanupLogContext = (linode = false) => {
  activeLogContext = { mode: linode ? 'linodeCleanup' : 'cleanup', clusterId: linode ? 'linode' : 'local', namespace: 'terratest', podName: 'cleanup' }
  logModalKindEl.textContent = linode ? 'Linode destroy logs' : 'Destroy logs'
  logModalTitleEl.textContent = linode ? 'Linode destroy run' : 'Destroy run'
  logModalSubtitleEl.textContent = 'go test -v -run TestHACleanup -timeout 20m ./terratest'
  openLogViewerBtnEl.classList.remove('hidden')
}

const renderLogViewer = () => {
  const query = logSearchEl.value.trim()
  const entries = rawLogText ? rawLogText.split('\n').map((line, index) => ({ line, index: index + 1 })) : []
  const filteredEntries = entries.filter(entry => {
    const queryMatches = query ? entry.line.toLowerCase().includes(query.toLowerCase()) : true
    return queryMatches && lineMatchesLogLevel(entry.line, activeLogLevel)
  })

  visibleLogText = filteredEntries.map(entry => entry.line).join('\n')
  const filterLabel = activeLogLevel === 'all' ? '' : ` • ${activeLogLevel.toUpperCase()}`
  logMatchCountEl.textContent = query || activeLogLevel !== 'all'
    ? `${filteredEntries.length} of ${entries.length} lines${filterLabel}`
    : `${entries.length} lines`

  if (!filteredEntries.length) {
    const waitingForLive = activeLogContext?.mode === 'live' && (liveLogState === 'connecting' || liveLogState === 'live')
    const waitingForSetup = activeLogContext?.mode === 'setup' && liveLogState === 'setupRunning'
    const waitingForReadiness = activeLogContext?.mode === 'readiness' && liveLogState === 'readinessRunning'
    const waitingForCleanup = activeLogContext?.mode === 'cleanup' && liveLogState === 'cleanupRunning'
    const waiting = waitingForLive || waitingForSetup || waitingForReadiness || waitingForCleanup
    logBoxEl.innerHTML = `
      <div class="flex h-full min-h-64 items-center justify-center rounded-xl border border-dashed border-zinc-300 bg-white text-sm text-zinc-500 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-400">
        <div class="flex items-center gap-3">
          ${waiting ? '<span class="spinner"></span>' : ''}
          <span>${waitingForLive ? 'Waiting for live log lines...' : waitingForSetup ? 'Waiting for setup output...' : waitingForReadiness ? 'Waiting for readiness output...' : waitingForCleanup ? 'Waiting for cleanup output...' : query || activeLogLevel !== 'all' ? 'No matching log lines.' : 'No logs loaded yet.'}</span>
        </div>
      </div>
    `
    return
  }

  logBoxEl.innerHTML = filteredEntries.map(entry => `
    <div class="log-row grid grid-cols-[4.5rem_minmax(0,1fr)] border-b border-zinc-200/70 bg-white/60 last:border-b-0 dark:border-white/5 dark:bg-white/[0.02]">
      <div class="select-none px-3 py-1.5 text-right text-[11px] tabular-nums text-zinc-400 dark:text-zinc-600">${entry.index}</div>
      <code class="min-w-0 whitespace-pre-wrap break-words px-3 py-1.5 text-zinc-800 dark:text-zinc-200">${highlightLogLine(entry.line, query)}</code>
    </div>
  `).join('')
}

const appendLogLine = line => {
  if (activeLogContext?.mode === 'live' && liveLogState !== 'live') {
    setLiveLogState('live')
  }
  rawLogText = rawLogText ? `${rawLogText}\n${line}` : line
  renderLogViewer()
  if (!logSearchEl.value.trim()) {
    logBoxEl.scrollTop = logBoxEl.scrollHeight
  }
}

const activeGPUClusters = state => clusterItems(state).filter(cluster =>
  cluster?.type === 'local' && (cluster.gpuWorkerIp || cluster.gpuWorkerPrivateIp)
)

const gpuReminderIntervalLabel = minutes => minutes === 60 ? '1 hour' : `${minutes} minutes`

const saveGPUReminderSettings = () => {
  localStorage.setItem(gpuReminderSettingsKey, JSON.stringify(gpuReminderSettings))
}

const publishGPUReminderSettings = () => {
  window.rancherGpuReminderSettings = gpuReminderSettings
  window.dispatchEvent(new CustomEvent('rancher-control-panel:gpu-reminders', {
    detail: { settings: gpuReminderSettings }
  }))
}

const hideGPUReminderModal = () => {
  gpuReminderModalEl?.classList.add('hidden')
  gpuReminderModalEl?.classList.remove('flex')
  document.body.classList.remove('overflow-hidden')
}

const showGPUReminderModal = clusters => {
  if (!gpuReminderModalEl || gpuReminderSettings.disabled) {
    return
  }
  const count = clusters.length
  const instanceTypes = [...new Set(clusters.map(cluster => cluster.gpuWorkerInstanceType).filter(Boolean))]
  const instanceText = instanceTypes.length === 1 ? ` (${instanceTypes[0]})` : ''
  if (gpuReminderBodyEl) {
    gpuReminderBodyEl.textContent = count === 1
      ? `Reminder: 1 GPU worker node${instanceText} is active. Are you still using it?`
      : `Reminder: ${count} GPU worker nodes${instanceText} are active. Are you still using them?`
  }
  gpuReminderSettings.lastReminderAt = Date.now()
  saveGPUReminderSettings()
  publishGPUReminderSettings()
  gpuReminderModalEl.classList.remove('hidden')
  gpuReminderModalEl.classList.add('flex')
  document.body.classList.add('overflow-hidden')
}

const maybeShowGPUReminder = state => {
  const clusters = activeGPUClusters(state)
  const lifecycleBusy = Boolean(state?.setup?.running || state?.readiness?.running || state?.cleanup?.running || cleanupStarting || setupLaunchPendingUntil > Date.now())
  if (lifecycleBusy) {
    hideGPUReminderModal()
    return
  }
  if (!clusters.length || gpuReminderSettings.disabled || bootStatePending) {
    return
  }
  if (gpuReminderModalEl && !gpuReminderModalEl.classList.contains('hidden')) {
    return
  }
  const intervalMs = gpuReminderSettings.intervalMinutes * 60 * 1000
  if (Date.now() - gpuReminderSettings.lastReminderAt < intervalMs) {
    return
  }
  showGPUReminderModal(clusters)
}

const publishControlPanelVueState = state => {
  window.rancherControlPanelState = state || {}
  window.dispatchEvent(new CustomEvent('rancher-control-panel:state', {
    detail: {
      state: window.rancherControlPanelState,
      bootPending: bootStatePending,
      refreshedAt: new Date().toISOString()
    }
  }))
}

const readinessBlockedReason = () => {
  const localClusters = clusterItems(lastState).filter(cluster => cluster.type === 'local')
  if (localClusters.length === 0) {
    return 'Run setup first'
  }
  if (localClusters.some(cluster => !cluster.available)) {
    return 'Setup incomplete'
  }
  return ''
}

const fetchState = async () => {
  const response = await fetch('/api/state', {
    headers: {
      'X-Control-Panel-Token': token
    }
  })
  if (!response.ok) {
    throw new Error(await response.text() || 'Failed to load panel state.')
  }
  return response.json()
}

const syncWorkspace = workspace => {
  const runs = Array.isArray(workspace?.runs) ? workspace.runs : []
  if (selectedCleanupRunId && !runs.some(run => run.runId === selectedCleanupRunId)) {
    selectedCleanupRunId = ''
  }
  renderDestroySlots(workspace)
}

const renderDestroySlots = workspace => {
  if (!cleanupSlotsEl) {
    return
  }

  const runs = Array.isArray(workspace?.runs) ? workspace.runs : []
  const cleanup = lastState?.cleanup || {}
  const linodeCleanup = lastState?.linodeCleanup || {}
  const cleanupRunning = Boolean(cleanup.running)
  const linodeCleanupRunning = Boolean(linodeCleanup.running)
  const setupRunning = Boolean(lastState?.setup?.running)
  const linodeSetupRunning = Boolean(lastState?.linodeSetup?.running)
  const readinessRunning = Boolean(lastState?.readiness?.running)

  if (!runs.length) {
    if (bootStatePending) {
      cleanupSlotsEl.innerHTML = `
        <div class="rounded-lg border border-sky-200 bg-sky-50 p-4 text-sm text-sky-800 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
          <span class="spinner mr-2 align-[-0.15em]"></span>Checking recorded run slots before destroy is enabled.
        </div>
      `
      return
    }
    cleanupSlotsEl.innerHTML = `
      <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">
        No recorded run slots found. There is nothing for Terraform destroy to target from this panel.
      </div>
    `
    return
  }

  const cards = runs.map(run => {
    const linodeRun = runIsLinodeDocker(run)
    const runSetupRunning = linodeRun ? linodeSetupRunning : setupRunning
    const runCleanupRunning = linodeRun ? linodeCleanupRunning : cleanupRunning
    const runCleanup = linodeRun ? linodeCleanup : cleanup
    const pendingDestroy = cleanupStarting && selectedCleanupRunId === run.runId
    const destroying = runCleanupRunning && runCleanup.runId === run.runId
    const selected = selectedCleanupRunId && sameRunKey(selectedCleanupRunId, run.runId)
    const versions = Array.isArray(run.rancherVersions) && run.rancherVersions.length
      ? run.rancherVersions.join(', ')
      : 'not recorded'
    const hostname = run.customHostnamePrefix
      ? `${run.customHostnamePrefix}.${run.route53Fqdn || ''}`.replace(/\.$/, '')
      : run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-h*.${run.route53Fqdn}` : run.route53Fqdn || 'generated per slot'
    const updated = run.updatedAt ? new Date(run.updatedAt).toLocaleTimeString() : ''
    const buttonLabel = destroying
      ? '<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>Destroy running'
        : pendingDestroy
          ? '<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>Starting destroy'
          : bootStatePending
            ? 'Checking state'
            : runSetupRunning
              ? 'Setup running'
              : !linodeRun && readinessRunning
                ? 'Readiness running'
                : runCleanupRunning
                  ? 'Destroy running'
                  : 'Destroy this slot'
    const disabled = bootStatePending || runSetupRunning || (!linodeRun && readinessRunning) || runCleanupRunning || cleanupStarting
    const disabledTitle = bootStatePending
      ? 'Startup safety check is still loading run slots and operation state.'
      : runSetupRunning
      ? 'Wait for setup to finish before destroying a run slot.'
      : !linodeRun && readinessRunning
        ? 'Wait for readiness checks to finish before destroying a run slot.'
        : runCleanupRunning
          ? 'Wait for the current destroy to finish before starting another one.'
          : cleanupStarting
            ? 'Destroy request is being submitted.'
            : `Destroy run ${run.runId || 'slot'}`
    const cardClass = destroying || pendingDestroy
      ? 'border-sky-200 bg-sky-50/60 dark:border-sky-500/25 dark:bg-sky-500/10'
      : selected
        ? 'border-emerald-200 bg-emerald-50/60 dark:border-emerald-500/25 dark:bg-emerald-500/10'
      : 'border-zinc-200 bg-white dark:border-white/10 dark:bg-white/[0.03]'
    const activityBadge = destroying
      ? '<span class="rounded-full bg-sky-100 px-2.5 py-1 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">Destroy running</span>'
      : pendingDestroy
        ? '<span class="rounded-full bg-sky-100 px-2.5 py-1 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">Starting destroy</span>'
        : ''

    return `
      <article class="rounded-xl border ${cardClass} p-4">
        <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Run ${escapeHtml(run.runId || 'unknown')}</h3>
              <span class="rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">${escapeHtml((run.status || 'recorded').replaceAll('_', ' '))}</span>
              ${selected && !destroying && !pendingDestroy ? '<span class="rounded-full bg-emerald-100 px-2.5 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300">Selected for destroy</span>' : ''}
              ${activityBadge}
            </div>
            ${updated ? `<div class="mt-1 text-xs text-zinc-500 dark:text-zinc-400">Updated ${escapeHtml(updated)}</div>` : ''}
            <div class="mt-3 grid gap-2 text-sm text-zinc-700 dark:text-zinc-300 md:grid-cols-2">
              <div><span class="font-semibold">Slot:</span> ${escapeHtml(run.slotId || run.slotName || 'not recorded')}</div>
              <div><span class="font-semibold">HAs:</span> ${escapeHtml(String(run.totalHAs || 1))}</div>
              <div><span class="font-semibold">Rancher:</span> ${escapeHtml(versions)}</div>
              <div><span class="font-semibold">Owner:</span> ${escapeHtml(run.owner || 'not recorded')}</div>
              <div><span class="font-semibold">AWS prefix:</span> ${escapeHtml(run.awsPrefix || 'not recorded')}</div>
              <div><span class="font-semibold">Hostname:</span> ${escapeHtml(hostname)}</div>
              <div class="md:col-span-2"><span class="font-semibold">State:</span> <span title="${escapeHtml(run.terraformStatePath || run.terraformBackend || '')}">${escapeHtml(compactPath(run.terraformStatePath || run.terraformBackend || 'not recorded'))}</span></div>
            </div>
          </div>
          <div class="flex shrink-0 flex-wrap gap-2 lg:justify-end">
            <button type="button" data-action="open-run-folder" data-run-id="${escapeHtml(run.runId || '')}" ${runFolderAvailable(run) ? '' : 'disabled'} title="${escapeHtml(runFolderAvailable(run) ? 'Open this run slot folder in Finder.' : 'Run folder is not available locally.')}" class="${runFolderAvailable(run) ? 'rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]' : 'rounded-lg bg-zinc-200 px-4 py-2.5 text-sm font-semibold text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400'}">Open folder</button>
            <button type="button" data-action="destroy-slot" data-run-id="${escapeHtml(run.runId || '')}" title="${escapeHtml(disabledTitle)}" ${disabled ? 'disabled' : ''} class="${disabled ? 'rounded-lg bg-zinc-200 px-4 py-2.5 text-sm font-semibold text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400' : 'rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400'}">${buttonLabel}</button>
          </div>
        </div>
      </article>
    `
  })

  cleanupSlotsEl.innerHTML = `
    ${selectedCleanupRunId ? `
      <div class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-100">
        Selected run ${escapeHtml(selectedCleanupRunId)}. Destroy is typed-confirmed and uses the recorded Terraform target for that slot.
      </div>
    ` : ''}
    ${cards.join('')}
  `
}

const awsLifecycleRunning = state => Boolean(state?.setup?.running || state?.readiness?.running || state?.cleanup?.running)
const linodeLifecycleRunning = state => Boolean(state?.linodeSetup?.running || state?.linodeCleanup?.running)
const lifecycleRunning = state => Boolean(awsLifecycleRunning(state) || linodeLifecycleRunning(state))
const runIsLinodeDocker = run => run?.deploymentType === 'linode-docker-cattle'
const runDestroyBlocked = run => runIsLinodeDocker(run) ? linodeLifecycleRunning(lastState) : awsLifecycleRunning(lastState)

const lifecycleBusyDetail = state => {
  if (state?.setup?.running) {
    return {
      busy: true,
      operation: 'setup',
      message: 'Setup is running. New AWS setup actions are locked, but Linode Docker setup can run in parallel.',
      busyByDeployment: {
        'ha-rke2': true,
        'hosted-tenant-k3s': true,
        'linode-docker-cattle': linodeLifecycleRunning(state)
      }
    }
  }
  if (state?.readiness?.running) {
    return {
      busy: true,
      operation: 'readiness',
      message: 'Readiness checks are running. AWS actions are locked, but Linode Docker setup can run in parallel.',
      busyByDeployment: {
        'ha-rke2': true,
        'hosted-tenant-k3s': true,
        'linode-docker-cattle': linodeLifecycleRunning(state)
      }
    }
  }
  if (state?.cleanup?.running) {
    return {
      busy: true,
      operation: 'destroy',
      message: 'Destroy is running. AWS actions are locked, but Linode Docker setup can run in parallel.',
      busyByDeployment: {
        'ha-rke2': true,
        'hosted-tenant-k3s': true,
        'linode-docker-cattle': linodeLifecycleRunning(state)
      }
    }
  }
  if (state?.linodeSetup?.running) {
    return {
      busy: true,
      operation: 'linodeSetup',
      message: 'Linode setup is running. AWS setup and destroy can still run in their own lane.',
      busyByDeployment: {
        'ha-rke2': awsLifecycleRunning(state),
        'hosted-tenant-k3s': awsLifecycleRunning(state),
        'linode-docker-cattle': true
      }
    }
  }
  if (state?.linodeCleanup?.running) {
    return {
      busy: true,
      operation: 'linodeCleanup',
      message: 'Linode destroy is running. AWS setup and destroy can still run in their own lane.',
      busyByDeployment: {
        'ha-rke2': awsLifecycleRunning(state),
        'hosted-tenant-k3s': awsLifecycleRunning(state),
        'linode-docker-cattle': true
      }
    }
  }
  return {
    busy: false,
    operation: '',
    message: '',
    busyByDeployment: {
      'ha-rke2': awsLifecycleRunning(state),
      'hosted-tenant-k3s': awsLifecycleRunning(state),
      'linode-docker-cattle': linodeLifecycleRunning(state)
    }
  }
}

const dispatchSetupLifecycleState = state => {
  dispatchSetupRootEvent('rancher-control-panel-lifecycle', lifecycleBusyDetail(state))
}

const updateStopPanelState = state => {
  if (!stopBtnEl) {
    return
  }
  const running = lifecycleRunning(state)
  stopBtnEl.disabled = running
  stopBtnEl.textContent = running ? 'Run in progress' : 'Stop panel'
  stopBtnEl.title = running
    ? 'Setup, readiness, or destroy is running. Leave the panel open until it finishes.'
    : 'Stop the local control panel.'
  stopBtnEl.className = running
    ? 'rounded-lg bg-zinc-200 px-4 py-2 text-sm font-medium text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400'
    : 'rounded-lg border border-zinc-200 bg-white px-4 py-2 text-sm font-medium text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'
}

const renderOperationMeta = (operation, config) => {
  if (!config.metaEl) {
    return
  }

  const parts = []
  if (operation?.runId) {
    parts.push(`Run ${operation.runId}`)
  }
  if (operation?.updatedAt) {
    parts.push(`Updated ${new Date(operation.updatedAt).toLocaleTimeString()}`)
  } else if (operation?.finishedAt) {
    parts.push(`Finished ${new Date(operation.finishedAt).toLocaleTimeString()}`)
  } else if (operation?.startedAt) {
    parts.push(`Started ${new Date(operation.startedAt).toLocaleTimeString()}`)
  }

  if (!parts.length) {
    config.metaEl.classList.add('hidden')
    config.metaEl.innerHTML = ''
    config.metaEl.removeAttribute('title')
    return
  }

  config.metaEl.classList.remove('hidden')
  const command = operation?.command ? `<div class="mt-1 break-words font-mono text-[11px] text-zinc-400 dark:text-zinc-500">${escapeHtml(operation.command)}</div>` : ''
  config.metaEl.innerHTML = `<div>${escapeHtml(parts.join(' • '))}</div>${command}`
  if (operation?.command) {
    config.metaEl.title = operation.command
  } else {
    config.metaEl.removeAttribute('title')
  }
}

const statusClass = tone => {
  const classes = {
    idle: 'mt-3 inline-flex items-center justify-center rounded-full bg-zinc-100 px-3 py-1.5 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300',
    running: 'mt-3 inline-flex items-center justify-center rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300',
    success: 'mt-3 inline-flex items-center justify-center rounded-full bg-emerald-100 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
    blocked: 'mt-3 inline-flex items-center justify-center rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300',
    error: 'mt-3 inline-flex items-center justify-center rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
  }
  return classes[tone] || classes.idle
}

const publishPreflightState = (checking = false) => {
  window.rancherControlPanelPreflight = preflightState
  window.dispatchEvent(new CustomEvent('rancher-control-panel:preflight', {
    detail: {
      preflight: preflightState,
      checking
    }
  }))
}

const setPreflightState = readiness => {
  preflightState = readiness
  publishPreflightState()

  if (lastState?.setup) {
    renderSetup(lastState.setup)
  }
}

const refreshPreflight = async () => {
  if (preflightInFlight) {
    return
  }

  preflightInFlight = true
  publishPreflightState(true)
  if (refreshPreflightBtnEl) {
    refreshPreflightBtnEl.disabled = true
  }

  try {
    const response = await fetch('/api/preflight', {
      cache: 'no-store',
      headers: {
        'Accept': 'application/json',
        'X-Control-Panel-Token': token
      }
    })
    if (!response.ok) {
      throw new Error(await response.text() || 'Preflight failed')
    }
    setPreflightState(await response.json())
  } catch (error) {
    setPreflightState({
      ready: false,
      summary: 'Preflight failed',
      items: [{
        name: 'Preflight',
        status: 'error',
        detail: error instanceof Error ? error.message : 'Preflight failed'
      }]
    })
  } finally {
    preflightInFlight = false
    publishPreflightState(false)
    if (refreshPreflightBtnEl) {
      refreshPreflightBtnEl.disabled = false
    }
  }
}

const renderOperation = (operation, config) => {
  const output = operationOutput(operation)
  const running = Boolean(operation?.running)
  const otherRunning = ['setup', 'readiness', 'cleanup'].some(mode => mode !== config.mode && lastState?.[mode]?.running)
  const blockedByPreflight = config.mode === 'setup' && preflightState && preflightState.ready !== true
  const blockedByBoot = bootStatePending
  const blockedReason = typeof config.blockedReason === 'function' ? config.blockedReason(operation) : ''
  const success = Boolean(operation?.finishedAt && !operation?.error)
  const failed = Boolean(operation?.error)

  if (running) {
    config.statusEl.className = statusClass('running')
    config.statusEl.innerHTML = `<span class="spinner mr-2"></span>${config.label} running${operation.startedAt ? ` since ${new Date(operation.startedAt).toLocaleTimeString()}` : ''}`
  } else if (failed) {
    config.statusEl.className = statusClass('error')
    config.statusEl.textContent = `${config.label} finished with error`
  } else if (success) {
    config.statusEl.className = statusClass('success')
    config.statusEl.textContent = `${config.label} finished successfully at ${new Date(operation.finishedAt).toLocaleTimeString()}`
  } else {
    config.statusEl.className = statusClass('idle')
    config.statusEl.textContent = 'Idle'
  }

  if (config.startDisabled && !running) {
    config.buttonEl.hidden = true
  } else {
    const stopPending = pendingAbortOperation === config.mode
    config.buttonEl.hidden = false
    config.buttonEl.disabled = running ? stopPending || blockedByBoot : blockedByBoot || otherRunning || blockedByPreflight || Boolean(blockedReason)
    config.buttonEl.innerHTML = running
      ? stopPending
        ? '<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>Stop requested'
        : (config.stopButtonText || `Stop ${config.label.toLowerCase()}`)
      : otherRunning
        ? 'Lifecycle running'
        : blockedByBoot
          ? 'Checking state'
          : blockedByPreflight
            ? 'Preflight blocked'
            : blockedReason || (success ? config.successButtonText : config.startButtonText)
    config.buttonEl.className = running && !blockedByBoot
      ? 'rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400'
      : blockedByBoot || otherRunning || blockedByPreflight || Boolean(blockedReason)
        ? 'rounded-lg bg-zinc-200 px-4 py-2.5 text-sm font-semibold text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400'
        : config.buttonClassName
  }
  renderOperationMeta(operation, config)

  if (activeLogContext?.mode === config.mode) {
    const wasNearBottom = logBoxEl.scrollHeight - logBoxEl.scrollTop - logBoxEl.clientHeight < 80
    rawLogText = output.join('\n')
    setLiveLogState(operation?.running ? `${config.mode}Running` : operation?.error ? `${config.mode}Error` : operation?.finishedAt ? `${config.mode}Done` : 'idle')
    renderLogViewer()
    if (wasNearBottom && !logSearchEl.value.trim()) {
      logBoxEl.scrollTop = logBoxEl.scrollHeight
    }
  }
}

const setupOperationConfig = {
  mode: 'setup',
  label: 'Setup',
  statusEl: setupStatusEl,
  metaEl: setupMetaEl,
  buttonEl: setupBtnEl,
  startDisabled: true,
  startButtonText: '',
  stopButtonText: 'Stop setup process',
  successButtonText: '',
  buttonClassName: 'rounded-lg bg-emerald-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400'
}

const readinessOperationConfig = {
  mode: 'readiness',
  label: 'Readiness',
  statusEl: readinessStatusEl,
  metaEl: readinessMetaEl,
  buttonEl: readinessBtnEl,
  blockedReason: readinessBlockedReason,
  startButtonText: 'Check readiness',
  successButtonText: 'Check again',
  buttonClassName: 'rounded-lg bg-sky-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-sky-500/20 hover:bg-sky-400'
}

const renderSetup = setup => renderOperation(setup, setupOperationConfig)

const renderReadiness = readiness => renderOperation(readiness, readinessOperationConfig)

const cleanupResultKey = cleanup => {
  if (!cleanup || cleanup.running || (!cleanup.finishedAt && !cleanup.error)) {
    return ''
  }
  return [
    cleanup.runId || 'unknown-run',
    cleanup.finishedAt || 'unfinished',
    cleanup.error || 'ok'
  ].join('|')
}

const cleanupResultDismissed = cleanup => {
  const key = cleanupResultKey(cleanup)
  return Boolean(key && cleanupDismissedResultKey === key)
}

const renderCleanupCost = (cleanup, output) => {
  if (cleanupResultDismissed(cleanup)) {
    cleanupCostEl.classList.add('hidden')
    cleanupCostEl.innerHTML = ''
    return
  }

  const cost = parseCleanupCost(output)
  if (cost) {
    cleanupCostEl.classList.remove('hidden')
    cleanupCostEl.innerHTML = `
      <div class="rounded-2xl border border-emerald-200 bg-emerald-50 p-4 text-left dark:border-emerald-500/20 dark:bg-emerald-500/10">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <div class="text-xs font-semibold uppercase tracking-wide text-emerald-700 dark:text-emerald-300">Estimated infrastructure cost while alive</div>
            <div class="mt-1 text-3xl font-semibold tracking-tight text-emerald-950 dark:text-emerald-100">${escapeHtml(cost.total)}</div>
            <div class="mt-1 text-sm text-emerald-800/80 dark:text-emerald-200/80">${escapeHtml(cost.region || 'AWS region unavailable')}</div>
          </div>
          <div class="grid gap-2 text-sm text-emerald-950 dark:text-emerald-100 sm:min-w-80">
            ${cost.runtime ? `<div><span class="font-semibold">Runtime:</span> ${escapeHtml(cost.runtime)}</div>` : ''}
            ${cost.ec2 ? `<div><span class="font-semibold">EC2:</span> ${escapeHtml(cost.ec2)}</div>` : ''}
            ${cost.ebs ? `<div><span class="font-semibold">EBS:</span> ${escapeHtml(cost.ebs)}</div>` : ''}
            ${cost.rds ? `<div><span class="font-semibold">RDS/Aurora:</span> ${escapeHtml(cost.rds)}</div>` : ''}
            ${cost.loadBalancers ? `<div><span class="font-semibold">Load balancers:</span> ${escapeHtml(cost.loadBalancers)}</div>` : ''}
          </div>
        </div>
      </div>
    `
    return
  }

  const estimateUnavailable = output.some(line => line.includes('Could not estimate EC2/EBS cost') || line.includes('Could not estimate AWS cost') || line.includes('Terraform outputs unavailable'))
  if (cleanup && cleanup.finishedAt && estimateUnavailable) {
    cleanupCostEl.classList.remove('hidden')
    cleanupCostEl.innerHTML = `
      <div class="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-left text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">
        Unable to estimate infrastructure cost for this destroy run. Destroy still ran; AWS pricing or Terraform outputs were unavailable.
      </div>
    `
    return
  }

  cleanupCostEl.classList.add('hidden')
  cleanupCostEl.innerHTML = ''
}

const renderCostControls = costs => {
  renderLocalArtifactCleanup(lastState?.workspace)

  if (resetCostLedgerBtnEl) {
    const locked = costResetting || bootStatePending || lifecycleRunning(lastState)
    resetCostLedgerBtnEl.disabled = locked
    resetCostLedgerBtnEl.innerHTML = costResetting
      ? '<span class="spinner mr-2 !h-4 !w-4 !border-2 align-[-0.15em]"></span>Resetting'
      : 'Reset cost DB'
    resetCostLedgerBtnEl.title = bootStatePending
      ? 'Startup safety check is still loading panel state.'
      : lifecycleRunning(lastState)
        ? 'Wait for setup, readiness, or destroy to finish before resetting the cost ledger.'
        : 'Delete the local ignored SQLite cost ledger and recreate it empty.'
  }
  if (costResetStatusEl && !costResetting) {
    const dbPath = costs?.dbPath || 'terratest/automation-output/control-panel/cost-ledger.sqlite'
    costResetStatusEl.textContent = `${dbPath} is local cache under automation-output/ and is ignored by Git.`
  }
}

const renderLocalArtifactCleanup = workspace => {
  if (!cleanLocalArtifactsBtnEl || !localArtifactsStatusEl) {
    return
  }

  const runCount = Array.isArray(workspace?.runs) ? workspace.runs.length : 0
  const residueCount = Array.isArray(workspace?.sharedPathLabels) ? workspace.sharedPathLabels.length : 0
  const locked = localArtifactsCleaning || bootStatePending || lifecycleRunning(lastState) || runCount > 0
  cleanLocalArtifactsBtnEl.disabled = locked
  cleanLocalArtifactsBtnEl.innerHTML = localArtifactsCleaning
    ? '<span class="spinner mr-2 !h-4 !w-4 !border-2 align-[-0.15em]"></span>Cleaning'
    : 'Clean after destroy'
  cleanLocalArtifactsBtnEl.title = bootStatePending
    ? 'Startup safety check is still loading panel state.'
    : lifecycleRunning(lastState)
      ? 'Wait for setup, readiness, or destroy to finish before cleaning local artifacts.'
      : runCount > 0
        ? 'Locked while recorded run slots exist so Terraform destroy targets stay available.'
        : 'Remove ignored local run residue left after destroy. Cost history is kept.'

  if (localArtifactsCleaning) {
    localArtifactsStatusEl.textContent = 'Cleaning ignored local run residue...'
  } else if (runCount > 0) {
    localArtifactsStatusEl.textContent = `Locked: ${runCount} recorded run slot${runCount === 1 ? '' : 's'} still exist. Destroy slots first so Terraform targets stay intact.`
  } else if (residueCount > 0) {
    localArtifactsStatusEl.textContent = `Ready: no recorded run slots remain. ${residueCount} leftover local artifact${residueCount === 1 ? '' : 's'} can be cleaned.`
  } else {
    localArtifactsStatusEl.textContent = 'Ready: no recorded run slots remain and no shared workspace residue is blocking setup.'
  }
}

const cleanLocalArtifacts = async () => {
  if (localArtifactsCleaning) {
    return
  }
  if (bootStatePending || lifecycleRunning(lastState)) {
    renderLocalArtifactCleanup(lastState?.workspace)
    return
  }

  const runCount = Array.isArray(lastState?.workspace?.runs) ? lastState.workspace.runs.length : 0
  if (runCount > 0) {
    renderLocalArtifactCleanup(lastState?.workspace)
    return
  }

  const confirmed = await requestTypedConfirmation({
    title: 'Clean artifacts after destroy?',
    body: 'This backup cleanup removes ignored local run residue only after recorded slots are gone. It keeps cost history and will not destroy AWS resources.',
    typedValue: 'clean local artifacts',
    confirmText: 'Clean artifacts',
    accentText: 'Local cleanup'
  })
  if (!confirmed) {
    return false
  }

  localArtifactsCleaning = true
  renderLocalArtifactCleanup(lastState?.workspace)

  const response = await fetch('/api/local-artifacts/clean', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Control-Panel-Token': token
    },
    body: JSON.stringify({ confirm: 'clean local artifacts' })
  })

  localArtifactsCleaning = false
  if (!response.ok) {
    localArtifactsStatusEl.textContent = await response.text()
    renderLocalArtifactCleanup(lastState?.workspace)
    return
  }

  const payload = await response.json()
  lastState = {
    ...(lastState || {}),
    workspace: payload.workspace || lastState?.workspace,
    costs: payload.costs || lastState?.costs
  }
  const removed = Array.isArray(payload.removed) ? payload.removed.length : 0
  syncWorkspace(lastState.workspace)
  renderLocalArtifactCleanup(lastState.workspace)
  publishControlPanelVueState(lastState)
  renderCostControls(lastState.costs)
  localArtifactsStatusEl.textContent = removed
    ? `Cleaned ${removed} local artifact${removed === 1 ? '' : 's'}.`
    : 'No local artifacts needed cleaning.'
  refresh()
}

const resetCostLedger = async () => {
  if (costResetting) {
    return
  }
  if (bootStatePending || lifecycleRunning(lastState)) {
    if (costResetStatusEl) {
      costResetStatusEl.textContent = bootStatePending
        ? 'Wait for the startup safety check to finish before resetting cost history.'
        : 'Wait for the active lifecycle operation to finish before resetting cost history.'
    }
    return
  }

  const confirmed = await requestTypedConfirmation({
    title: 'Reset cost history database?',
    body: 'This deletes the local SQLite cost ledger and starts a fresh empty one. It does not destroy AWS resources, remove run slots, or change Terraform state.',
    typedValue: 'reset costs',
    confirmText: 'Reset cost DB',
    accentText: 'Local data reset'
  })
  if (!confirmed) {
    return false
  }

  costResetting = true
  if (costResetStatusEl) {
    costResetStatusEl.textContent = 'Resetting local cost ledger...'
  }
  renderCostControls(lastState?.costs)

  const response = await fetch('/api/costs/reset', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Control-Panel-Token': token
    },
    body: JSON.stringify({ confirm: 'reset costs' })
  })

  costResetting = false
  if (!response.ok) {
    if (costResetStatusEl) {
      costResetStatusEl.textContent = await response.text()
    }
    renderCostControls(lastState?.costs)
    return
  }

  const payload = await response.json()
  lastState = {
    ...(lastState || {}),
    costs: payload.costs || { entries: [], totals: {} }
  }
  if (costResetStatusEl) {
    costResetStatusEl.textContent = 'Cost history reset. A fresh empty SQLite ledger is ready.'
  }
  publishControlPanelVueState(lastState)
  renderCostControls(lastState.costs)
  refresh()
}

const renderCleanup = cleanup => {
  const output = cleanup && Array.isArray(cleanup.output) ? cleanup.output : []
  const running = Boolean(cleanup?.running)
  const blockedByLifecycle = Boolean(bootStatePending || lastState?.setup?.running || lastState?.readiness?.running)
  const dismissed = cleanupResultDismissed(cleanup)
  const success = Boolean(cleanup?.finishedAt && !cleanup?.error && !dismissed)
  const failed = Boolean(cleanup?.error && !dismissed)
  renderDestroySlots(lastState?.workspace)

  if (running) {
    cleanupStatusEl.className = 'inline-flex items-center justify-center rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300'
    cleanupStatusEl.innerHTML = `<span class="spinner mr-2"></span>Destroy running${cleanup.runId ? ` for ${escapeHtml(cleanup.runId)}` : ''}${cleanup.startedAt ? ` since ${new Date(cleanup.startedAt).toLocaleTimeString()}` : ''}`
  } else if (failed) {
    cleanupStatusEl.className = 'inline-flex items-center justify-center rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
    cleanupStatusEl.textContent = 'Destroy finished with error'
  } else if (success) {
    cleanupStatusEl.className = 'inline-flex items-center justify-center rounded-full bg-emerald-100 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    cleanupStatusEl.textContent = `Destroy finished successfully at ${new Date(cleanup.finishedAt).toLocaleTimeString()}`
  } else {
    cleanupStatusEl.className = 'inline-flex items-center justify-center rounded-full bg-zinc-100 px-3 py-1.5 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300'
    cleanupStatusEl.textContent = 'Idle'
  }

  cleanupActionsEl.className = 'mt-5 flex flex-wrap justify-end gap-3'

  cleanupConfirmEl.hidden = true
  cleanupBtnEl.hidden = true
  if (cleanupClearResultBtnEl) {
    cleanupClearResultBtnEl.hidden = dismissed || running || (!success && !failed)
    cleanupClearResultBtnEl.disabled = bootStatePending
  }
  cleanupConfirmEl.disabled = running || blockedByLifecycle || cleanupStarting
  cleanupBtnEl.disabled = running || blockedByLifecycle || cleanupStarting
  cleanupBtnEl.innerHTML = running
    ? 'Destroy running'
    : cleanupStarting
      ? '<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>Starting destroy'
      : blockedByLifecycle
        ? bootStatePending ? 'Checking state' : 'Lifecycle running'
        : selectedCleanupRunId
          ? `Destroy selected run`
          : 'Destroy run'
  cleanupBtnEl.className = running || blockedByLifecycle || cleanupStarting
    ? 'rounded-lg bg-zinc-200 px-4 py-2.5 text-sm font-semibold text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400'
    : 'rounded-lg bg-rose-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400'

  renderCleanupCost(cleanup, output)

  if (activeLogContext?.mode === 'cleanup' || activeLogContext?.mode === 'linodeCleanup') {
    const wasNearBottom = logBoxEl.scrollHeight - logBoxEl.scrollTop - logBoxEl.clientHeight < 80
    rawLogText = output.join('\n')
    setLiveLogState(cleanup?.running ? (activeLogContext.mode === 'linodeCleanup' ? 'linodeCleanupRunning' : 'cleanupRunning') : cleanup?.error ? 'cleanupError' : cleanup?.finishedAt ? 'cleanupDone' : 'idle')
    renderLogViewer()
    if (wasNearBottom && !logSearchEl.value.trim()) {
      logBoxEl.scrollTop = logBoxEl.scrollHeight
    }
  }
}

const refresh = async () => {
  if (refreshInFlight) {
    return
  }

  refreshInFlight = true
  refreshStatusEl.textContent = 'Refreshing...'
  if (bootStatePending) {
    setBootState(true, 'Checking local config, run slots, Terraform state, lifecycle processes, clusters, and AWS inventory before enabling actions.')
  }

  try {
    const state = await fetchState()
    if (
      setupLaunchPendingUntil > Date.now() &&
      !state?.setup?.running &&
      !state?.setup?.finishedAt &&
      !state?.setup?.error
    ) {
      state.setup = {
        ...(state.setup || {}),
        running: true,
        output: ['[control-panel] AWS setup accepted. Waiting for lifecycle state to publish the run record...'],
        startedAt: new Date().toISOString()
      }
    } else if (state?.setup?.running || state?.setup?.finishedAt || state?.setup?.error) {
      setupLaunchPendingUntil = 0
    }
    lastState = state
    dispatchSetupLifecycleState(state)
    if (pendingAbortOperation && !state?.[pendingAbortOperation]?.running) {
      pendingAbortOperation = ''
    }
    if (cleanupStarting && (state?.cleanup?.running || state?.linodeCleanup?.running)) {
      cleanupStarting = false
    }
    if (bootStatePending) {
      setBootState(false)
    }
    publishControlPanelVueState(state)
    syncWorkspace(state.workspace)
    updateLeaderTracking(state)
    renderClusters(state)
    renderSetup(state.setup)
    renderReadiness(state.readiness)
    renderCleanup(state.linodeCleanup?.running || state.linodeCleanup?.finishedAt || state.linodeCleanup?.error ? state.linodeCleanup : state.cleanup)
    renderCostControls(state.costs)
    publishGPUReminderSettings()
    maybeShowGPUReminder(state)
    updateStopPanelState(state)
    refreshStatusEl.textContent = lastLeaderChangeMessage
      ? `${lastLeaderChangeMessage} • ${new Date().toLocaleTimeString()}`
      : `Last refreshed at ${new Date().toLocaleTimeString()}`
  } catch (error) {
    refreshStatusEl.textContent = error instanceof Error ? error.message : 'Refresh failed'
    if (bootStatePending) {
      setBootState(true, `State check failed: ${error instanceof Error ? error.message : 'refresh failed'}. Actions stay disabled until the panel can read local state.`)
    }
  } finally {
    refreshInFlight = false
  }
}

const stopStream = (options = {}) => {
  if (!options.internal && activeLogContext?.mode === 'live' && (liveLogState === 'stopped' || liveLogState === 'error')) {
    if (stream) {
      stream.close()
      stream = null
    }
    streamLogs(activeLogContext.clusterId, activeLogContext.namespace, activeLogContext.podName, { preserveLogs: true })
    return
  }

  if (streamPollTimer) {
    window.clearInterval(streamPollTimer)
    streamPollTimer = null
  }
  livePollGeneration += 1

  if (!stream) {
    if (!options.internal && activeLogContext?.mode === 'live') {
      logStatusEl.textContent = 'Live log refresh stopped.'
      setLiveLogState('stopped')
      logModalSubtitleEl.textContent = activeLogContext
        ? `${activeLogContext.namespace} • ${activeLogContext.clusterId} • live refresh stopped`
        : 'Live log refresh stopped.'
    }
    return
  }

  stream.close()
  stream = null
  logStatusEl.textContent = 'Live log stream stopped.'
  setLiveLogState('stopped')
  logModalSubtitleEl.textContent = activeLogContext
    ? `${activeLogContext.namespace} • ${activeLogContext.clusterId} • live stream stopped`
    : 'Live log stream stopped.'
}

const fetchPodLogTail = async (clusterId, namespace, podName) => {
  const params = new URLSearchParams({ cluster: clusterId, namespace, pod: podName })
  const response = await fetch(`/api/logs?${params.toString()}`, {
    headers: {
      'Accept': 'application/json',
      'X-Control-Panel-Token': token
    }
  })
  const raw = await response.text()
  let payload

  try {
    payload = JSON.parse(raw)
  } catch (_) {
    payload = { text: raw }
  }

  if (!response.ok) {
    throw new Error(payload.text || raw || 'Failed to load logs')
  }

  return payload.text || ''
}

const fetchDockerLogs = async clusterId => {
  const params = new URLSearchParams({ cluster: clusterId })
  const response = await fetch(`/api/docker-logs?${params.toString()}`, {
    headers: {
      'Accept': 'application/json',
      'X-Control-Panel-Token': token
    }
  })
  const raw = await response.text()
  let payload

  try {
    payload = JSON.parse(raw)
  } catch (_) {
    payload = { text: raw }
  }

  if (!response.ok) {
    throw new Error(payload.text || raw || 'Failed to load Docker logs')
  }

  return payload.text || ''
}

const loadDockerLogs = async cluster => {
  const clusterId = cluster?.id || ''
  if (!clusterId || activeDockerLogsClusterId) {
    return
  }
  stopStream({ internal: true })
  activeDockerLogsClusterId = clusterId
  setDockerLogContext(cluster)
  setLiveLogState('idle')
  openLogModal()
  rawLogText = ''
  renderLogViewer()
  if (lastState) {
    renderClusters(lastState)
  }
  logStatusEl.textContent = `Loading Docker logs for ${cluster?.name || 'Rancher'}...`

  try {
    rawLogText = await fetchDockerLogs(clusterId)
    renderLogViewer()
    logBoxEl.scrollTop = logBoxEl.scrollHeight
    logStatusEl.textContent = 'Showing recent Docker logs'
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Failed to load Docker logs'
    logStatusEl.textContent = message
    rawLogText = `[error] ${message}`
    renderLogViewer()
  } finally {
    activeDockerLogsClusterId = ''
    if (lastState) {
      renderClusters(lastState)
    }
  }
}

const loadLogs = async (clusterId, namespace, podName) => {
  stopStream({ internal: true })
  setActiveLogContext('tail', clusterId, namespace, podName)
  setLiveLogState('idle')
  openLogModal()
  rawLogText = ''
  renderLogViewer()
  logStatusEl.textContent = `Loading logs for ${podName}...`

  try {
    rawLogText = await fetchPodLogTail(clusterId, namespace, podName)
    renderLogViewer()
    logBoxEl.scrollTop = logBoxEl.scrollHeight
    logStatusEl.textContent = `Showing recent logs for ${podName}`
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Failed to load logs'
    logStatusEl.textContent = message
    rawLogText = `[error] ${message}`
    renderLogViewer()
  }
}

const streamLogs = (clusterId, namespace, podName, options = {}) => {
  stopStream({ internal: true })
  setActiveLogContext('live', clusterId, namespace, podName)
  setLiveLogState('connecting')
  openLogModal()
  if (!options.preserveLogs) {
    rawLogText = ''
  }
  renderLogViewer()
  logStatusEl.textContent = `Refreshing live logs for ${podName}...`

  const generation = livePollGeneration
  const poll = async () => {
    try {
      const text = await fetchPodLogTail(clusterId, namespace, podName)
      if (generation !== livePollGeneration || activeLogContext?.mode !== 'live') {
        return
      }
      rawLogText = text
      setLiveLogState('live')
      renderLogViewer()
      if (!logSearchEl.value.trim()) {
        logBoxEl.scrollTop = logBoxEl.scrollHeight
      }
      logStatusEl.textContent = `Live logs auto-refreshing for ${podName}`
    } catch (error) {
      if (generation !== livePollGeneration || activeLogContext?.mode !== 'live') {
        return
      }
      const message = error instanceof Error ? error.message : 'Failed to refresh live logs'
      setLiveLogState('error')
      rawLogText = rawLogText ? `${rawLogText}\n[error] ${message}` : `[error] ${message}`
      renderLogViewer()
      logStatusEl.textContent = message
    }
  }

  poll()
  streamPollTimer = window.setInterval(poll, 3000)
}

const downloadKubeconfig = async clusterId => {
  if (activeDownloadClusterId) {
    return
  }

  activeDownloadClusterId = clusterId
  if (lastState) {
    renderClusters(lastState)
  }
  refreshStatusEl.textContent = 'Preparing kubeconfig download...'

  try {
    const response = await fetch('/api/kubeconfig/save', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Control-Panel-Token': token
      },
      body: JSON.stringify({ cluster: clusterId })
    })

    if (!response.ok) {
      refreshStatusEl.textContent = await response.text()
      return
    }

    const saved = await response.json()
    const filename = saved.filename || 'kubeconfig.yaml'
    const path = saved.path || '~/Downloads'
    refreshStatusEl.textContent = `Saved ${filename} to Downloads.`
    showPanelNotice('Kubeconfig saved', `${filename} was saved to ${path}`)
  } catch (error) {
    refreshStatusEl.textContent = error instanceof Error ? error.message : 'Failed to save kubeconfig.'
  } finally {
    activeDownloadClusterId = ''
    if (lastState) {
      renderClusters(lastState)
    }
  }
}

const openExternalURL = async url => {
  const rawURL = String(url || '').trim()
  if (!rawURL) {
    return
  }

  refreshStatusEl.textContent = 'Opening Rancher URL in your browser...'
  try {
    const response = await fetch('/api/open-url', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Control-Panel-Token': token
      },
      body: JSON.stringify({ url: rawURL })
    })
    if (!response.ok) {
      throw new Error(await response.text() || 'Failed to open browser.')
    }
    refreshStatusEl.textContent = 'Opened Rancher URL in your browser.'
  } catch (error) {
    window.open(rawURL, '_blank', 'noopener,noreferrer')
    refreshStatusEl.textContent = error instanceof Error ? error.message : 'Fell back to opening a new window.'
  }
}

const openLocalPath = async (path, options = {}) => {
  const rawPath = String(path || '').trim()
  if (!rawPath) {
    refreshStatusEl.textContent = 'No local path is available yet.'
    return false
  }

  refreshStatusEl.textContent = options.reveal ? 'Revealing local path...' : 'Opening local folder...'
  try {
    const response = await fetch('/api/open-path', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Control-Panel-Token': token
      },
      body: JSON.stringify({ path: rawPath, reveal: Boolean(options.reveal) })
    })
    if (!response.ok) {
      throw new Error(await response.text() || 'Failed to open local path.')
    }
    refreshStatusEl.textContent = options.reveal ? 'Revealed local path.' : 'Opened local folder.'
    return true
  } catch (error) {
    refreshStatusEl.textContent = error instanceof Error ? error.message : 'Failed to open local path.'
    return false
  }
}

const copyTextToClipboard = async (text, successMessage) => {
  if (!navigator.clipboard) {
    refreshStatusEl.textContent = 'Clipboard access is unavailable in this browser.'
    return false
  }
  const value = String(text || '').trim()
  if (!value) {
    refreshStatusEl.textContent = 'No value is available to copy yet.'
    return false
  }
  try {
    await navigator.clipboard.writeText(value)
    refreshStatusEl.textContent = successMessage
    return true
  } catch (error) {
    refreshStatusEl.textContent = error instanceof Error ? error.message : 'Failed to copy to clipboard.'
    return false
  }
}

const openKubeconfigFolder = async cluster => {
  const clusterId = cluster?.id || ''
  if (!clusterId || activeOpenKubeconfigPathClusterId) {
    return
  }

  activeOpenKubeconfigPathClusterId = clusterId
  if (lastState) {
    renderClusters(lastState)
  }

  const opened = await openLocalPath(cluster?.kubeconfigPath || '', { reveal: true })
  activeOpenKubeconfigPathClusterId = ''
  clusterPanel.flashKubeconfigPathAction(clusterId, 'open', opened ? 'success' : 'error')
}

const copyKubeconfigPath = async cluster => {
  const clusterId = cluster?.id || ''
  if (!clusterId || activeCopyKubeconfigPathClusterId) {
    return
  }

  activeCopyKubeconfigPathClusterId = clusterId
  if (lastState) {
    renderClusters(lastState)
  }

  const copied = await copyTextToClipboard(cluster?.kubeconfigPath || '', 'Copied kubeconfig path to clipboard.')
  activeCopyKubeconfigPathClusterId = ''
  clusterPanel.flashKubeconfigPathAction(clusterId, 'copy', copied ? 'success' : 'error')
}

const copyLinodeIP = async cluster => {
  const clusterId = cluster?.id || ''
  if (!clusterId || activeCopyLinodeIPClusterId) {
    return
  }

  activeCopyLinodeIPClusterId = clusterId
  if (lastState) {
    renderClusters(lastState)
  }

  const copied = await copyTextToClipboard(cluster?.loadBalancer || '', 'Copied Linode IP to clipboard.')
  activeCopyLinodeIPClusterId = ''
  clusterPanel.flashKubeconfigPathAction(clusterId, 'copy-linode-ip', copied ? 'success' : 'error')
}

const copyKubeconfig = async clusterId => {
  if (activeCopyClusterId) {
    return
  }

  if (!navigator.clipboard) {
    refreshStatusEl.textContent = 'Clipboard access is unavailable in this browser.'
    return
  }

  activeCopyClusterId = clusterId
  if (lastState) {
    renderClusters(lastState)
  }
  refreshStatusEl.textContent = 'Copying kubeconfig...'

  try {
    const response = await fetch(`/api/kubeconfig?cluster=${encodeURIComponent(clusterId)}`, {
      headers: {
        'X-Control-Panel-Token': token
      }
    })

    if (!response.ok) {
      refreshStatusEl.textContent = await response.text()
      return
    }

    await navigator.clipboard.writeText(await response.text())
    refreshStatusEl.textContent = 'Copied kubeconfig to clipboard.'
  } catch (error) {
    refreshStatusEl.textContent = error instanceof Error ? error.message : 'Failed to copy kubeconfig.'
  } finally {
    activeCopyClusterId = ''
    if (lastState) {
      renderClusters(lastState)
    }
  }
}

const copyHelmInstallCommand = async (clusterId, mode = 'install') => {
  const upgradeMode = mode === 'upgrade'
  if (upgradeMode ? activeCopyHelmUpgradeClusterId : activeCopyHelmClusterId) {
    return
  }

  if (!navigator.clipboard) {
    refreshStatusEl.textContent = 'Clipboard access is unavailable in this browser.'
    return
  }

  if (upgradeMode) {
    activeCopyHelmUpgradeClusterId = clusterId
  } else {
    activeCopyHelmClusterId = clusterId
  }
  if (lastState) {
    renderClusters(lastState)
  }
  refreshStatusEl.textContent = upgradeMode ? 'Copying prepared Helm upgrade command...' : 'Copying Helm install command...'

  try {
    const response = await fetch(`/api/helm-command?cluster=${encodeURIComponent(clusterId)}${upgradeMode ? '&mode=upgrade' : ''}`, {
      headers: {
        'X-Control-Panel-Token': token
      }
    })

    if (!response.ok) {
      refreshStatusEl.textContent = await response.text()
      return
    }

    await navigator.clipboard.writeText(await response.text())
    refreshStatusEl.textContent = upgradeMode ? 'Copied prepared Helm upgrade command to clipboard.' : 'Copied Helm install command to clipboard.'
    if (upgradeMode) {
      showUpgradeCommandModal()
    }
  } catch (error) {
    refreshStatusEl.textContent = error instanceof Error ? error.message : 'Failed to copy Helm command.'
  } finally {
    if (upgradeMode) {
      activeCopyHelmUpgradeClusterId = ''
    } else {
      activeCopyHelmClusterId = ''
    }
    if (lastState) {
      renderClusters(lastState)
    }
  }
}

const downloadLogs = () => {
  const text = visibleLogText || rawLogText
  if (!text) {
    logStatusEl.textContent = 'No logs to download yet.'
    return
  }

  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = logFilename()
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
  logStatusEl.textContent = `Downloaded ${link.download}`
}

const openSetupLogs = (linode = false) => {
  stopStream({ internal: true })
  setSetupLogContext(linode)
  const setup = linode ? lastState?.linodeSetup || {} : lastState?.setup || {}
  const output = operationOutput(setup)
  rawLogText = output.join('\n')
  setLiveLogState(setup.running ? (linode ? 'linodeSetupRunning' : 'setupRunning') : setup.error ? 'setupError' : setup.finishedAt ? 'setupDone' : 'idle')
  renderLogViewer()
  openLogModal()
  logBoxEl.scrollTop = logBoxEl.scrollHeight
}

const openReadinessLogs = () => {
  stopStream({ internal: true })
  setReadinessLogContext()
  const readiness = lastState?.readiness || {}
  const output = operationOutput(readiness)
  rawLogText = output.join('\n')
  setLiveLogState(readiness.running ? 'readinessRunning' : readiness.error ? 'readinessError' : readiness.finishedAt ? 'readinessDone' : 'idle')
  renderLogViewer()
  openLogModal()
  logBoxEl.scrollTop = logBoxEl.scrollHeight
}

const openCleanupLogs = (linode = false) => {
  stopStream({ internal: true })
  setCleanupLogContext(linode)
  const cleanup = linode ? lastState?.linodeCleanup || {} : lastState?.cleanup || {}
  const output = operationOutput(cleanup)
  rawLogText = output.join('\n')
  setLiveLogState(cleanup.running ? (linode ? 'linodeCleanupRunning' : 'cleanupRunning') : cleanup.error ? 'cleanupError' : cleanup.finishedAt ? 'cleanupDone' : 'idle')
  renderLogViewer()
  openLogModal()
  logBoxEl.scrollTop = logBoxEl.scrollHeight
}

const runSetup = async () => {
  if (!lastState?.setup?.running) {
    return
  }
  await abortOperation('setup', lastState.setup.runId)
}

const abortOperation = async (operation, runId = '', options = {}) => {
  if (!options.skipConfirmation) {
    const confirmed = await requestTypedConfirmation({
      title: operation === 'setup' || operation === 'linodeSetup' ? 'Stop setup process?' : `Stop ${operation}?`,
      body: operation === 'setup' || operation === 'linodeSetup'
        ? 'This asks the local setup test process to stop and preserves Terraform state plus the run record. It does not destroy AWS resources.'
        : 'This asks the local operation process to stop and preserves Terraform state plus run records. It does not destroy AWS resources.',
      typedValue: 'stop',
      confirmText: 'Request stop'
    })
    if (!confirmed) {
      return false
    }
  }

  pendingAbortOperation = operation
  if (lastState?.[operation] && (operation === 'setup' || operation === 'readiness')) {
    renderOperation(lastState[operation], operation === 'setup' ? setupOperationConfig : readinessOperationConfig)
  }
  refreshStatusEl.textContent = `Requesting stop for ${operation}...`

  const response = await fetch('/api/operations/abort', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Control-Panel-Token': token
    },
    body: JSON.stringify({ operation, runId, confirm: 'stop' })
  })

  if (!response.ok) {
    refreshStatusEl.textContent = await response.text()
    pendingAbortOperation = ''
    return false
  }

  refreshStatusEl.textContent = `Stop requested for ${operation}.`
  refresh()
  return true
}

const stopOperationThenOpenDestroy = async (operation, runId = '') => {
  const targetRunId = String(runId || '').trim()
  const label = operation === 'setup' ? 'setup' : 'readiness'
  const confirmed = await requestTypedConfirmation({
    title: `Stop ${label}, then open destroy?`,
    body: `This requests a stop for the running ${label} process and moves run ${targetRunId || 'this slot'} into the Destroy tab. Terraform destroy still requires its own typed "destroy" confirmation before AWS cleanup starts.`,
    typedValue: 'confirm',
    confirmText: 'Stop and open destroy',
    accentText: 'Stop before destroy'
  })
  if (!confirmed) {
    return
  }

  const stopped = await abortOperation(operation, targetRunId, { skipConfirmation: true })
  if (!stopped) {
    return
  }
  selectedCleanupRunId = targetRunId
  setActiveDestroyTab('slots')
  setActivePanelTab('destroy')
  renderDestroySlots(lastState?.workspace)
}

const runReadiness = async () => {
  const response = await fetch('/api/readiness', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Control-Panel-Token': token
    }
  })

  if (!response.ok) {
    readinessStatusEl.textContent = await response.text()
    return
  }

  readinessStatusEl.textContent = 'Readiness requested...'
  lastState = {
    ...(lastState || {}),
    readiness: {
      ...(lastState?.readiness || {}),
      running: true,
      output: ['[control-panel] Readiness requested...'],
      startedAt: new Date().toISOString()
    }
  }
  dispatchSetupLifecycleState(lastState)
  renderReadiness(lastState.readiness)
  setReadinessLogContext()
  setLiveLogState('readinessRunning')
  rawLogText = '[control-panel] Readiness requested...'
  renderLogViewer()
  openLogModal()
  refresh()
}

const runCleanup = async (runId = selectedCleanupRunId) => {
  const targetRunId = String(runId || '').trim()
  if (!targetRunId) {
    cleanupStatusEl.textContent = 'Select a run before starting destroy.'
    return
  }

  if (bootStatePending) {
    cleanupStatusEl.className = 'inline-flex items-center justify-center rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300'
    cleanupStatusEl.textContent = 'Checking state'
    return
  }

  const targetRun = (lastState?.workspace?.runs || []).find(run => sameRunKey(run.runId, targetRunId))
  const linodeRun = runIsLinodeDocker(targetRun)
  const destroyBlocked = linodeRun
    ? lastState?.linodeCleanup?.running || lastState?.linodeSetup?.running
    : lastState?.cleanup?.running || lastState?.setup?.running || lastState?.readiness?.running
  if (cleanupStarting || destroyBlocked) {
    cleanupStatusEl.className = 'inline-flex items-center justify-center rounded-full bg-amber-100 px-3 py-1.5 text-xs font-semibold text-amber-800 dark:bg-amber-500/15 dark:text-amber-200'
    cleanupStatusEl.textContent = linodeRun
      ? lastState?.linodeSetup?.running ? 'Linode setup is running' : 'Linode destroy is running'
      : lastState?.setup?.running
        ? 'Setup is running'
        : lastState?.readiness?.running
          ? 'Readiness is running'
          : 'Destroy is running'
    return
  }

  const confirmed = await requestTypedConfirmation({
    title: `Destroy run ${targetRunId}?`,
    body: linodeRun
      ? 'This runs Terraform destroy from the selected Linode run state. It deletes the Linode instance and its AWS Route53 record, then removes the run slot only after destroy succeeds.'
      : 'This runs Terraform destroy from the selected run state. It is intended to delete AWS resources for that run, then remove the run slot only after destroy succeeds.',
    typedValue: 'destroy',
    confirmText: 'Start destroy',
    accentText: 'AWS destroy confirmation'
  })
  if (!confirmed) {
    return
  }

  selectedCleanupRunId = targetRunId
  cleanupDismissedResultKey = ''
  cleanupStarting = true
  renderCleanup(lastState?.cleanup || {})

  const response = await fetch('/api/cleanup', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Control-Panel-Token': token
    },
    body: JSON.stringify({ confirm: 'destroy', runId: targetRunId })
  })

  if (!response.ok) {
    cleanupStatusEl.textContent = await response.text()
    cleanupStarting = false
    renderCleanup(lastState?.cleanup || {})
    return
  }

  cleanupConfirmEl.value = ''
  cleanupStarting = false
  cleanupStatusEl.textContent = 'Destroy requested...'
  lastState = {
    ...(lastState || {}),
    [linodeRun ? 'linodeCleanup' : 'cleanup']: {
      ...(lastState?.[linodeRun ? 'linodeCleanup' : 'cleanup'] || {}),
      running: true,
      runId: targetRunId,
      output: ['[control-panel] Destroy requested...'],
      startedAt: new Date().toISOString()
    }
  }
  dispatchSetupLifecycleState(lastState)
  renderClusters(lastState)
  renderCleanup(lastState.cleanup)
  setCleanupLogContext(linodeRun)
  setLiveLogState(linodeRun ? 'linodeCleanupRunning' : 'cleanupRunning')
  rawLogText = '[control-panel] Destroy requested...'
  renderLogViewer()
  openLogModal()
  refresh()
}

const stopPanel = async () => {
  if (lifecycleRunning(lastState)) {
    stopBtnEl.textContent = 'Run in progress'
    return
  }
  stopBtnEl.disabled = true
  stopBtnEl.textContent = 'Stopping...'

  try {
    const response = await fetch('/api/shutdown', {
      method: 'POST',
      headers: {
        'X-Control-Panel-Token': token
      }
    })
    if (!response.ok) {
      stopBtnEl.disabled = false
      stopBtnEl.textContent = 'Stop panel'
      refreshStatusEl.textContent = (await response.text()) || 'Stop panel blocked'
      return
    }
    window.setTimeout(() => window.close(), 250)
  } finally {
    refresh()
  }
}

clustersEl.addEventListener('click', event => {
  const externalLink = event.target.closest('a[data-external-url]')
  if (externalLink) {
    event.preventDefault()
    openExternalURL(externalLink.dataset.externalUrl || externalLink.href)
    return
  }

  const button = event.target.closest('button[data-action]')
  if (!button) {
    return
  }

  const action = button.dataset.action
  const clusterId = button.dataset.cluster

  if (action === 'open-cleanup-logs') {
    openCleanupLogs()
    return
  }

  if (action === 'select-cluster-run') {
    activeClusterRunKey = button.dataset.runKey || ''
    activeClusterHAKey = ''
    if (lastState) {
      renderClusters(lastState)
    }
    return
  }

  if (action === 'select-cluster-ha') {
    activeClusterHAKey = button.dataset.haKey || ''
    if (lastState) {
      renderClusters(lastState)
    }
    return
  }

  if (action === 'toggle-cluster') {
    clusterPanel.toggleCluster(clusterId)
    return
  }

  if (action === 'toggle-pods') {
    clusterPanel.togglePods(clusterId)
    return
  }

  if (action === 'toggle-gpu-commands') {
    clusterPanel.toggleGPUCommands(clusterId)
    return
  }

  if (action === 'copy-gpu-command') {
    const commandIndex = button.dataset.commandIndex || ''
    const commandText = button.closest('[data-gpu-command-row]')?.querySelector('code[data-gpu-command-text]')?.textContent || ''
    copyTextToClipboard(commandText, 'Copied GPU setup command to clipboard.').then(copied => {
      clusterPanel.flashGPUCommandCopy(clusterId, commandIndex, copied ? 'success' : 'error')
    })
    return
  }

  if (action === 'download') {
    downloadKubeconfig(clusterId)
    return
  }

  if (action === 'copy-kubeconfig') {
    copyKubeconfig(clusterId)
    return
  }

  if (action === 'open-kubeconfig-folder') {
    const cluster = clusterItems(lastState).find(item => item.id === clusterId)
    openKubeconfigFolder(cluster)
    return
  }

  if (action === 'copy-kubeconfig-path') {
    const cluster = clusterItems(lastState).find(item => item.id === clusterId)
    copyKubeconfigPath(cluster)
    return
  }

  if (action === 'copy-linode-ip') {
    const cluster = clusterItems(lastState).find(item => item.id === clusterId)
    copyLinodeIP(cluster)
    return
  }

  if (action === 'docker-logs') {
    const cluster = clusterItems(lastState).find(item => item.id === clusterId)
    loadDockerLogs(cluster)
    return
  }

  if (action === 'copy-helm-command') {
    copyHelmInstallCommand(clusterId)
    return
  }

  if (action === 'copy-helm-upgrade-command') {
    copyHelmInstallCommand(clusterId, 'upgrade')
    return
  }

  if (action === 'tail') {
    loadLogs(clusterId, button.dataset.namespace, button.dataset.pod)
    return
  }

  if (action === 'live') {
    streamLogs(clusterId, button.dataset.namespace, button.dataset.pod)
  }
})

workspaceRunMetaEl?.addEventListener('click', event => {
  const button = event.target.closest('button[data-run-action]')
  if (!button) {
    return
  }

  const action = button.dataset.runAction
  const runId = button.dataset.runId || ''
  const run = (lastState?.workspace?.runs || []).find(candidate => sameRunKey(candidate.runId, runId))
  if (action === 'open-setup') {
    setActivePanelTab('setup')
    return
  }
  if (action === 'view-clusters') {
    activeClusterRunKey = runId
    activeClusterHAKey = ''
    setActivePanelTab('clusters')
    renderClusters(lastState)
    return
  }
  if (action === 'check-readiness') {
    runReadiness()
    return
  }
  if (action === 'open-run-folder') {
    if (!runFolderAvailable(run)) {
      refreshStatusEl.textContent = 'Run folder is not available locally.'
      return
    }
    openLocalPath(runFolderPath(run))
    return
  }
  if (action === 'copy-terraform-path') {
    copyTextToClipboard(runTerraformPath(run), 'Copied Terraform path to clipboard.')
    return
  }
  if (action === 'open-setup-logs') {
    openSetupLogs(runIsLinodeDocker(run))
    return
  }
  if (action === 'open-readiness-logs') {
    openReadinessLogs()
    return
  }
  if (action === 'open-cleanup-logs') {
    openCleanupLogs(runIsLinodeDocker(run))
    return
  }
  if (action === 'open-destroy') {
    selectedCleanupRunId = runId
    setActiveDestroyTab('slots')
    setActivePanelTab('destroy')
    renderDestroySlots(lastState?.workspace)
    return
  }
  if (action === 'stop-setup') {
    abortOperation('setup', runId)
    return
  }
  if (action === 'stop-setup-open-destroy') {
    stopOperationThenOpenDestroy(runIsLinodeDocker(run) ? 'linodeSetup' : 'setup', runId)
    return
  }
  if (action === 'stop-readiness') {
    abortOperation('readiness', runId)
    return
  }
  if (action === 'stop-readiness-open-destroy') {
    stopOperationThenOpenDestroy('readiness', runId)
  }
})

cleanupSlotsEl?.addEventListener('click', event => {
  const button = event.target.closest('button[data-action]')
  if (!button) {
    return
  }
  const runId = button.dataset.runId || ''
  if (button.dataset.action === 'open-run-folder') {
    const run = (lastState?.workspace?.runs || []).find(candidate => sameRunKey(candidate.runId, runId))
    if (!runFolderAvailable(run)) {
      refreshStatusEl.textContent = 'Run folder is not available locally.'
      return
    }
    openLocalPath(runFolderPath(run))
    return
  }
  if (button.dataset.action === 'destroy-slot') {
    selectedCleanupRunId = runId
    renderDestroySlots(lastState?.workspace)
    runCleanup(runId)
  }
})

;[destroySlotsTabBtnEl, destroyCostsTabBtnEl].forEach(button => {
  button?.addEventListener('click', () => setActiveDestroyTab(button.dataset.destroyTab))
})

resetCostLedgerBtnEl?.addEventListener('click', resetCostLedger)
cleanLocalArtifactsBtnEl?.addEventListener('click', cleanLocalArtifacts)

gpuReminderIntervalEls.forEach(button => {
  button.addEventListener('click', () => {
    gpuReminderSettings = {
      ...gpuReminderSettings,
      disabled: false,
      intervalMinutes: Number(button.dataset.gpuReminderInterval) || 15,
      lastReminderAt: Date.now()
    }
    saveGPUReminderSettings()
    publishGPUReminderSettings()
    showPanelNotice('GPU reminders updated', `Reminder interval set to ${gpuReminderIntervalLabel(gpuReminderSettings.intervalMinutes)}.`)
  })
})

gpuReminderEnableBtnEl?.addEventListener('click', () => {
  gpuReminderSettings = {
    ...gpuReminderSettings,
    disabled: false,
    lastReminderAt: Date.now()
  }
  saveGPUReminderSettings()
  publishGPUReminderSettings()
  showPanelNotice('GPU reminders enabled', `Next reminder after ${gpuReminderIntervalLabel(gpuReminderSettings.intervalMinutes)} if GPU infrastructure remains active.`)
})

gpuReminderDisableBtnEl?.addEventListener('click', async () => {
  const confirmed = await requestTypedConfirmation({
    title: 'Disable GPU reminders?',
    body: 'GPU worker nodes can create meaningful cloud cost when left running. Close-time GPU warnings still appear, but timed reminders will stop until you enable them again.',
    typedValue: 'disable gpu reminders',
    confirmText: 'Disable reminders',
    accentText: 'GPU cost warning'
  })
  if (!confirmed) {
    return
  }
  gpuReminderSettings = {
    ...gpuReminderSettings,
    disabled: true,
    lastReminderAt: Date.now()
  }
  saveGPUReminderSettings()
  hideGPUReminderModal()
  publishGPUReminderSettings()
  showPanelNotice('GPU reminders disabled', 'Close-time GPU warnings remain active.')
})

gpuReminderDismissBtnEl?.addEventListener('click', hideGPUReminderModal)

gpuReminderCleanupBtnEl?.addEventListener('click', () => {
  hideGPUReminderModal()
  setActivePanelTab('destroy')
  setActiveDestroyTab('slots')
  cleanupSlotsEl?.scrollIntoView({ behavior: 'smooth', block: 'start' })
})

gpuReminderSettingsBtnEl?.addEventListener('click', () => {
  hideGPUReminderModal()
  setActivePanelTab('settings')
})

gpuReminderModalEl?.addEventListener('click', event => {
  if (event.target === gpuReminderModalEl) {
    hideGPUReminderModal()
  }
})

upgradeCommandModalEl?.addEventListener('click', event => {
  if (event.target === upgradeCommandModalEl) {
    closeUpgradeCommandModal()
  }
})
document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && upgradeCommandModalEl && !upgradeCommandModalEl.classList.contains('hidden')) {
    closeUpgradeCommandModal()
  }
  if (event.key === 'Escape' && gpuReminderModalEl && !gpuReminderModalEl.classList.contains('hidden')) {
    hideGPUReminderModal()
  }
})

themeToggleEl.addEventListener('click', () => {
  setTheme(currentTheme() === 'dark' ? 'light' : 'dark')
})

fullscreenToggleEl?.addEventListener('click', () => {
  setPanelFullscreen(!panelFullscreen)
})

panelTabsEl?.addEventListener('click', event => {
  const button = event.target.closest('button[data-tab]')
  if (button) {
    setActivePanelTab(button.dataset.tab)
  }
})

commandDeckEl?.addEventListener('click', event => {
  const button = event.target.closest('button[data-command-action]')
  if (!button) {
    return
  }
  setActivePanelTab(button.dataset.commandAction || 'runs')
})

window.addEventListener('rancher-setup-started', () => {
  const now = new Date().toISOString()
  setupLaunchPendingUntil = Date.now() + 15000
  lastState = {
    ...(lastState || {}),
    setup: {
      ...(lastState?.setup || {}),
      running: true,
      output: ['[control-panel] AWS setup accepted. Waiting for lifecycle state to publish the run record...'],
      startedAt: now
    }
  }
  dispatchSetupLifecycleState(lastState)
  publishControlPanelVueState(lastState)
  renderSetup(lastState.setup)
  refreshStatusEl.textContent = 'AWS setup accepted. Waiting for run state to appear...'
  setActivePanelTab('runs')
  refresh()
})

document.getElementById('refreshBtn').addEventListener('click', refresh)
document.getElementById('stopBtn').addEventListener('click', stopPanel)
refreshPreflightBtnEl?.addEventListener('click', refreshPreflight)
setupBtnEl.addEventListener('click', runSetup)
openSetupLogsBtnEl.addEventListener('click', openSetupLogs)
readinessBtnEl.addEventListener('click', runReadiness)
openReadinessLogsBtnEl.addEventListener('click', openReadinessLogs)
document.getElementById('cleanupBtn').addEventListener('click', () => runCleanup())
openCleanupLogsBtnEl.addEventListener('click', openCleanupLogs)
cleanupClearResultBtnEl?.addEventListener('click', () => {
  cleanupDismissedResultKey = cleanupResultKey(lastState?.cleanup)
  renderCleanup(lastState?.cleanup || {})
  if (lastState) {
    renderClusters(lastState)
  }
})
document.getElementById('stopStreamBtn').addEventListener('click', stopStream)
document.getElementById('clearLogsBtn').addEventListener('click', () => {
  rawLogText = ''
  visibleLogText = ''
  renderLogViewer()
  logStatusEl.textContent = 'Logs cleared.'
})
document.getElementById('downloadLogsBtn').addEventListener('click', downloadLogs)
document.getElementById('closeLogModalBtn').addEventListener('click', closeLogModal)
openLogViewerBtnEl.addEventListener('click', openLogModal)
logSearchEl.addEventListener('input', renderLogViewer)
logLevelFiltersEl.addEventListener('click', event => {
  const button = event.target.closest('button[data-level]')
  if (button) {
    setActiveLogLevel(button.dataset.level)
  }
})

logModalEl.addEventListener('click', event => {
  if (event.target === logModalEl) {
    closeLogModal()
  }
})

document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && !logModalEl.classList.contains('hidden')) {
    closeLogModal()
  }
  if ((event.metaKey || event.ctrlKey) && event.shiftKey && event.key.toLowerCase() === 'f') {
    event.preventDefault()
    setPanelFullscreen(!panelFullscreen)
  }
})

document.addEventListener('fullscreenchange', syncFullscreenButton)

setLiveLogState('idle')
setTheme(currentTheme(), false)
syncFullscreenButton()
setActivePanelTab(activePanelTab)
setActiveDestroyTab(activeDestroyTab)
publishGPUReminderSettings()
setBootState(true, 'Checking local config, run slots, Terraform state, lifecycle processes, clusters, and AWS inventory before enabling actions.')
if ('scrollRestoration' in history) {
  history.scrollRestoration = 'manual'
}
window.requestAnimationFrame(() => window.scrollTo({ top: 0, left: 0 }))
refreshPreflight()
refresh()
window.setInterval(refresh, 5000)
