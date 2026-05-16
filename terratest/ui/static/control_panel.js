const setupData = JSON.parse(document.getElementById('control-panel-data')?.textContent || '{}')
const token = setupData.token || ''

const panelSessionMetaEl = document.getElementById('panelSessionMeta')
const buildVersionBadgeEl = document.getElementById('buildVersionBadge')
const headerSummaryEl = document.getElementById('headerSummary')
const commandDeckEl = document.getElementById('commandDeck')
const configNoticeEl = document.getElementById('configNotice')
const bootStatusEl = document.getElementById('bootStatus')
const bootStatusDetailEl = document.getElementById('bootStatusDetail')
const panelTabsEl = document.getElementById('panelTabs')
const tabPanelEls = Array.from(document.querySelectorAll('[data-tab-panel]'))
const workspaceModeEl = document.getElementById('workspaceMode')
const workspaceSlotTitleEl = document.getElementById('workspaceSlotTitle')
const workspaceSlotSummaryEl = document.getElementById('workspaceSlotSummary')
const workspaceSlotGridEl = document.getElementById('workspaceSlotGrid')
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
const preflightStatusEl = document.getElementById('preflightStatus')
const preflightItemsEl = document.getElementById('preflightItems')
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
const costHistorySummaryEl = document.getElementById('costHistorySummary')
const costHistoryTableEl = document.getElementById('costHistoryTable')
const awsInventorySummaryEl = document.getElementById('awsInventorySummary')
const awsInventoryMetaEl = document.getElementById('awsInventoryMeta')
const awsInventoryEl = document.getElementById('awsInventory')
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
const upgradeCommandModalEl = document.getElementById('upgradeCommandModal')
const upgradeCommandModalCloseEl = document.getElementById('upgradeCommandModalClose')

let stream = null
let streamPollTimer = null
let livePollGeneration = 0
let previousLeaders = new Map()
let pendingLeaderHighlights = new Map()
let collapsedClusters = new Map()
let collapsedPods = new Map()
let initializedCollapseState = new Set()
let lastState = null
let activeDownloadClusterId = ''
let activeCopyClusterId = ''
let activeCopyHelmClusterId = ''
let activeCopyHelmUpgradeClusterId = ''
let activeOpenKubeconfigPathClusterId = ''
let activeCopyKubeconfigPathClusterId = ''
let kubeconfigPathActionFeedback = new Map()
let kubeconfigPathActionTimers = new Map()
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
let panelNoticeTimer = null

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
  const available = Boolean(panelTabsEl?.querySelector(`button[data-tab="${tab}"]`))
  activePanelTab = available ? tab : 'runs'
  localStorage.setItem('rancherControlPanelTab', activePanelTab)
  tabPanelEls.forEach(panel => {
    panel.classList.toggle('hidden', panel.dataset.tabPanel !== activePanelTab)
  })
  panelTabsEl?.querySelectorAll('button[data-tab]').forEach(button => {
    const active = button.dataset.tab === activePanelTab
    if (active) {
      button.setAttribute('aria-current', 'page')
    } else {
      button.removeAttribute('aria-current')
    }
    button.className = active
      ? 'panel-tab rounded-lg bg-emerald-500 px-3.5 py-2 text-sm font-semibold text-white shadow-sm shadow-emerald-500/20'
      : 'panel-tab rounded-lg px-3.5 py-2 text-sm font-semibold text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-white/[0.06]'
    const badge = button.querySelector('[data-tab-count]')
    if (badge) {
      const empty = !badge.textContent.trim()
      badge.className = active
        ? `tab-count bg-white/20 text-white ${empty ? 'hidden' : ''}`
        : `tab-count bg-zinc-100 text-zinc-600 dark:bg-white/[0.08] dark:text-zinc-300 ${empty ? 'hidden' : ''}`
    }
  })
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

const hidePanelNotice = () => {
  if (panelNoticeTimer) {
    window.clearTimeout(panelNoticeTimer)
    panelNoticeTimer = null
  }
  panelNoticeEl?.classList.add('hidden')
}

const showPanelNotice = (title, body) => {
  if (!panelNoticeEl) {
    refreshStatusEl.textContent = `${title}: ${body}`
    return
  }

  if (panelNoticeTitleEl) {
    panelNoticeTitleEl.textContent = title
  }
  if (panelNoticeBodyEl) {
    panelNoticeBodyEl.textContent = body
  }
  panelNoticeEl.classList.remove('hidden')
  if (panelNoticeTimer) {
    window.clearTimeout(panelNoticeTimer)
  }
  panelNoticeTimer = window.setTimeout(() => {
    panelNoticeEl.classList.add('hidden')
    panelNoticeTimer = null
  }, 9000)
}

const closeUpgradeCommandModal = () => {
  if (!upgradeCommandModalEl) {
    return
  }
  upgradeCommandModalEl.classList.add('hidden')
  upgradeCommandModalEl.classList.remove('flex')
  document.body.classList.remove('overflow-hidden')
}

const showUpgradeCommandModal = () => {
  if (!upgradeCommandModalEl) {
    showPanelNotice(
      'Prepared upgrade copied',
      'Edit the chart version and any image override values before running the copied command.'
    )
    return
  }
  upgradeCommandModalEl.classList.remove('hidden')
  upgradeCommandModalEl.classList.add('flex')
  document.body.classList.add('overflow-hidden')
  window.setTimeout(() => upgradeCommandModalCloseEl?.focus(), 0)
}

const setTheme = theme => {
  document.documentElement.classList.toggle('dark', theme === 'dark')
  document.body.classList.toggle('dark', theme === 'dark')
  localStorage.setItem('rancherControlPanelTheme', theme)

  themeSunIconEl.classList.toggle('hidden', theme !== 'dark')
  themeMoonIconEl.classList.toggle('hidden', theme !== 'light')
  themeToggleLabelEl.textContent = theme === 'dark' ? 'Light' : 'Dark'
}

const escapeHtml = value => String(value || '')
  .replaceAll('&', '&amp;')
  .replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;')
  .replaceAll('\'', '&#39;')

const requestTypedConfirmation = ({ title, body, typedValue, confirmText, accentText = 'Confirmation required' }) => new Promise(resolve => {
  if (!dangerConfirmModalEl) {
    resolve(false)
    return
  }

  let settled = false
  const expected = String(typedValue || '').trim().toLowerCase()

  const cleanup = result => {
    if (settled) {
      return
    }
    settled = true
    dangerConfirmModalEl.classList.add('hidden')
    dangerConfirmModalEl.classList.remove('flex')
    document.body.classList.remove('overflow-hidden')
    dangerConfirmCancelEl.removeEventListener('click', cancel)
    dangerConfirmSubmitEl.removeEventListener('click', submit)
    dangerConfirmInputEl.removeEventListener('keydown', keydown)
    dangerConfirmModalEl.removeEventListener('click', backdrop)
    document.removeEventListener('keydown', escape)
    resolve(result)
  }

  const cancel = () => cleanup(false)
  const submit = () => {
    if (String(dangerConfirmInputEl.value || '').trim().toLowerCase() !== expected) {
      dangerConfirmErrorEl.textContent = `Type ${typedValue} to confirm.`
      dangerConfirmInputEl.focus()
      dangerConfirmInputEl.select()
      return
    }
    cleanup(true)
  }
  const keydown = event => {
    if (event.key === 'Enter') {
      event.preventDefault()
      submit()
    }
  }
  const backdrop = event => {
    if (event.target === dangerConfirmModalEl) {
      cancel()
    }
  }
  const escape = event => {
    if (event.key === 'Escape') {
      cancel()
    }
  }

  dangerConfirmAccentEl.textContent = accentText
  dangerConfirmTitleEl.textContent = title
  dangerConfirmBodyEl.textContent = body
  dangerConfirmPromptEl.textContent = `Type "${typedValue}" to continue`
  dangerConfirmSubmitEl.textContent = confirmText
  dangerConfirmInputEl.value = ''
  dangerConfirmErrorEl.textContent = ''
  dangerConfirmModalEl.classList.remove('hidden')
  dangerConfirmModalEl.classList.add('flex')
  document.body.classList.add('overflow-hidden')
  dangerConfirmCancelEl.addEventListener('click', cancel)
  dangerConfirmSubmitEl.addEventListener('click', submit)
  dangerConfirmInputEl.addEventListener('keydown', keydown)
  dangerConfirmModalEl.addEventListener('click', backdrop)
  document.addEventListener('keydown', escape)
  window.setTimeout(() => dangerConfirmInputEl.focus(), 0)
})

const escapeRegExp = value => String(value || '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

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
  renderHeaderSummary(lastState || {})
  renderCommandDeck(lastState || {})
  renderPanelTabBadges(lastState || {})
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

const compactPath = value => {
  const path = String(value || '').trim()
  if (!path) {
    return ''
  }
  const parts = path.split('/').filter(Boolean)
  if (parts.length <= 4) {
    return path
  }
  return `.../${parts.slice(-4).join('/')}`
}

const formatUSD = value => {
  const number = Number(value || 0)
  return number.toLocaleString(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  })
}

const highlightLogLine = (line, query) => {
  const escapedLine = escapeHtml(line)
  if (!query) {
    return escapedLine || '&nbsp;'
  }

  const pattern = new RegExp(escapeRegExp(query), 'ig')
  const highlighted = escapedLine.replace(pattern, match => `<mark class="rounded bg-amber-200 px-0.5 text-zinc-950 dark:bg-amber-300">${match}</mark>`)
  return highlighted || '&nbsp;'
}

const lineMatchesLogLevel = (line, level) => {
  if (level === 'all') {
    return true
  }

  const patterns = {
    info: /\b(info|level=info|level="info")\b/i,
    debug: /\b(debug|level=debug|level="debug")\b/i,
    warning: /\b(warn|warning|level=warn|level=warning|level="warn"|level="warning")\b/i,
    error: /\b(error|err|level=error|level=err|level="error"|level="err")\b/i
  }

  return patterns[level]?.test(line) || false
}

const extractCleanupLineValue = (output, label) => {
  const line = output.find(item => item.includes(label))
  if (!line) {
    return ''
  }

  return line.slice(line.indexOf(label) + label.length).trim()
}

const parseCleanupCost = output => {
  const total = extractCleanupLineValue(output, 'Estimated total (EC2 + EBS only):')
  if (!total) {
    return null
  }

  return {
    total,
    region: extractCleanupLineValue(output, 'Region:'),
    runtime: extractCleanupLineValue(output, 'Total runtime across instances:'),
    ec2: extractCleanupLineValue(output, 'EC2:'),
    ebs: extractCleanupLineValue(output, 'EBS:')
  }
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

  if (activeLogContext?.mode === 'setup') {
    const filter = logSearchEl.value.trim() ? '-filtered' : ''
    return `setup${filter}.log`
  }

  if (activeLogContext?.mode === 'cleanup') {
    const filter = logSearchEl.value.trim() ? '-filtered' : ''
    return `cleanup${filter}.log`
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

const setSetupLogContext = () => {
  activeLogContext = { mode: 'setup', clusterId: 'local', namespace: 'terratest', podName: 'setup' }
  logModalKindEl.textContent = 'Setup logs'
  logModalTitleEl.textContent = 'Setup'
  logModalSubtitleEl.textContent = 'go test -v -run ^TestHaSetup$ -timeout 90m -count=1 ./terratest'
  openLogViewerBtnEl.classList.remove('hidden')
}

const setReadinessLogContext = () => {
  activeLogContext = { mode: 'readiness', clusterId: 'local', namespace: 'terratest', podName: 'readiness' }
  logModalKindEl.textContent = 'Readiness logs'
  logModalTitleEl.textContent = 'Readiness'
  logModalSubtitleEl.textContent = 'go test -v -run ^TestHAWaitReady$ -timeout 35m -count=1 ./terratest'
  openLogViewerBtnEl.classList.remove('hidden')
}

const setCleanupLogContext = () => {
  activeLogContext = { mode: 'cleanup', clusterId: 'local', namespace: 'terratest', podName: 'cleanup' }
  logModalKindEl.textContent = 'Destroy logs'
  logModalTitleEl.textContent = 'Destroy run'
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

const clusterItems = state => state && state.clusters && Array.isArray(state.clusters.items)
  ? state.clusters.items
  : []

const operationSummaryForState = state => {
  const operations = [
    ['setup', 'Setup', state?.setup],
    ['readiness', 'Readiness', state?.readiness],
    ['cleanup', 'Destroy', state?.cleanup]
  ]
  const active = operations.find(([, , operation]) => operation?.running)
  if (active) {
    const [, label, operation] = active
    return {
      label,
      value: operation?.runId ? `Run ${operation.runId}` : 'Running',
      tone: 'sky',
      running: true
    }
  }
  if (bootStatePending) {
    return { label: 'Safety check', value: 'Loading state', tone: 'sky', running: true }
  }
  return { label: 'Operation', value: 'Idle', tone: 'zinc', running: false }
}

const headerChipClasses = tone => ({
  emerald: 'panel-chip-tone-emerald',
  sky: 'panel-chip-tone-sky',
  amber: 'panel-chip-tone-amber',
  rose: 'panel-chip-tone-rose',
  zinc: ''
})[tone] || ''

const renderHeaderSummary = state => {
  if (!headerSummaryEl) {
    return
  }
  const runs = Array.isArray(state?.workspace?.runs) ? state.workspace.runs : []
  const totalHAs = runs.reduce((total, run) => total + Number(run.totalHAs || 1), 0)
  const clusters = clusterItems(state)
  const reachable = clusters.filter(cluster => cluster.reachable).length
  const awsItems = Array.isArray(state?.aws?.items) ? state.aws.items : []
  const operation = operationSummaryForState(state)
  const freshness = new Date().toLocaleTimeString()
  const chips = [
    { label: 'Runs', value: `${runs.length} slot${runs.length === 1 ? '' : 's'} / ${totalHAs} HA`, tone: runs.length ? 'emerald' : 'zinc' },
    { label: 'Clusters', value: clusters.length ? `${reachable}/${clusters.length} reachable` : 'None yet', tone: clusters.length ? (reachable === clusters.length ? 'emerald' : 'amber') : 'zinc' },
    { label: 'AWS view', value: awsItems.length ? `${awsItems.length} resources` : 'No resources shown', tone: awsItems.length ? 'amber' : 'zinc' },
    { label: operation.label, value: operation.value, tone: operation.tone, running: operation.running },
    { label: 'Refreshed', value: freshness, tone: 'zinc' }
  ]

  headerSummaryEl.innerHTML = chips.map(chip => `
    <span class="panel-chip ${headerChipClasses(chip.tone)}">
      ${chip.running ? '<span class="spinner !h-3 !w-3 !border-[1.5px]"></span>' : ''}
      <span>${escapeHtml(chip.label)}</span>
      <span class="panel-chip-value">${escapeHtml(chip.value)}</span>
    </span>
  `).join('')
}

const renderPanelTabBadges = state => {
  if (!panelTabsEl) {
    return
  }
  const runs = Array.isArray(state?.workspace?.runs) ? state.workspace.runs : []
  const clusters = clusterItems(state)
  const awsItems = Array.isArray(state?.aws?.items) ? state.aws.items : []
  const values = {
    setup: state?.setup?.running ? 'Running' : '',
    runs: runs.length ? String(runs.length) : '',
    clusters: clusters.length ? String(clusters.length) : '',
    aws: awsItems.length ? String(awsItems.length) : '',
    destroy: runs.length ? String(runs.length) : ''
  }
  panelTabsEl.querySelectorAll('[data-tab-count]').forEach(badge => {
    const tab = badge.dataset.tabCount
    const value = values[tab] || ''
    const active = activePanelTab === tab
    badge.textContent = value
    badge.className = active
      ? `tab-count bg-white/20 text-white ${value ? '' : 'hidden'}`
      : `tab-count bg-zinc-100 text-zinc-600 dark:bg-white/[0.08] dark:text-zinc-300 ${value ? '' : 'hidden'}`
  })
}

const commandTileHTML = ({ tone = 'zinc', eyebrow, title, detail, meta = '', action = '', actionLabel = '' }) => `
  <article class="command-tile p-4" data-tone="${escapeHtml(tone)}">
    <div class="flex h-full min-w-0 flex-col gap-3">
      <div class="flex min-w-0 items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="text-[11px] font-extrabold uppercase tracking-[0.18em] text-zinc-500 dark:text-zinc-400">${escapeHtml(eyebrow)}</div>
          <div class="mt-2 truncate text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50" title="${escapeHtml(title)}">${escapeHtml(title)}</div>
        </div>
        ${meta ? `<div class="shrink-0 rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-bold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">${escapeHtml(meta)}</div>` : ''}
      </div>
      <p class="min-h-[2.5rem] text-sm leading-5 text-zinc-600 dark:text-zinc-400">${escapeHtml(detail)}</p>
      ${action && actionLabel ? `
        <div class="mt-auto">
          <button type="button" data-command-action="${escapeHtml(action)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs font-bold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${escapeHtml(actionLabel)}</button>
        </div>
      ` : ''}
    </div>
  </article>
`

const renderCommandDeck = state => {
  if (!commandDeckEl) {
    return
  }
  const runs = Array.isArray(state?.workspace?.runs) ? state.workspace.runs : []
  const currentRun = state?.workspace?.currentRun || runs[0] || null
  const clusters = clusterItems(state)
  const awsItems = Array.isArray(state?.aws?.items) ? state.aws.items : []
  const operation = operationSummaryForState(state)
  const lifecycleBusy = lifecycleRunning(state)
  const readyForSetup = Boolean(state?.workspace?.canStartIsolatedRun && !lifecycleBusy && !bootStatePending)
  const currentStats = currentRun ? runClusterStats(currentRun, state) : { total: 0, reachable: 0, management: 0, downstream: 0 }

  const safetyTile = bootStatePending
    ? {
        tone: 'sky',
        eyebrow: 'Safety gate',
        title: 'Inspecting local state',
        detail: 'Actions stay disabled while the panel checks config, run slots, Terraform state, and active lifecycle processes.',
        meta: 'Locked',
        action: 'runs',
        actionLabel: 'View runs'
      }
    : lifecycleBusy
      ? {
          tone: 'sky',
          eyebrow: 'Safety gate',
          title: `${operation.label} is active`,
          detail: 'Setup, readiness, and destroy are serialized so the run state and AWS target stay unambiguous.',
          meta: 'Busy',
          action: 'runs',
          actionLabel: 'Inspect run'
        }
      : {
          tone: readyForSetup ? 'emerald' : 'zinc',
          eyebrow: 'Safety gate',
          title: readyForSetup ? 'Ready for a new setup' : 'Operator actions ready',
          detail: readyForSetup ? 'Setup is available from the Setup tab after plan resolution and approval.' : 'No lifecycle operation is running. Existing slots remain individually inspectable and destroyable.',
          meta: 'Ready',
          action: readyForSetup ? 'setup' : 'runs',
          actionLabel: readyForSetup ? 'Open setup' : 'View runs'
        }

  const runTile = currentRun
    ? {
        tone: currentStats.total ? 'emerald' : 'amber',
        eyebrow: 'Current slot',
        title: `Run ${currentRun.runId || 'unknown'}`,
        detail: `${currentRun.totalHAs || 1} HA target${Number(currentRun.totalHAs || 1) === 1 ? '' : 's'} for ${runVersionsLabel(currentRun)}. ${currentStats.total ? `${currentStats.reachable}/${currentStats.total} cluster records reachable.` : 'Cluster records are not visible yet.'}`,
        meta: currentRun.status || 'recorded',
        action: currentStats.total ? 'clusters' : 'runs',
        actionLabel: currentStats.total ? 'Open clusters' : 'View slot'
      }
    : {
        tone: 'zinc',
        eyebrow: 'Current slot',
        title: 'No run slot yet',
        detail: 'Resolve a plan in Setup, approve the Helm/AWS gate, and the slot will appear before Terraform creates resources.',
        meta: 'Empty',
        action: 'setup',
        actionLabel: 'Start setup flow'
      }

  const exposureTile = awsItems.length
    ? {
        tone: 'amber',
        eyebrow: 'AWS exposure',
        title: `${awsItems.length} resource${awsItems.length === 1 ? '' : 's'} visible`,
        detail: 'Inventory is read-only. Destructive actions remain per-slot and require typed confirmation before Terraform destroy starts.',
        meta: 'Live',
        action: 'aws',
        actionLabel: 'Open inventory'
      }
    : {
        tone: runs.length ? 'emerald' : 'zinc',
        eyebrow: 'AWS exposure',
        title: 'No resources shown',
        detail: runs.length ? 'Recorded slots are available; AWS inventory currently has no matching visible resources.' : 'No AWS resources are expected before an approved setup run.',
        meta: 'Quiet',
        action: runs.length ? 'destroy' : 'setup',
        actionLabel: runs.length ? 'Open destroy' : 'Open setup'
      }

  commandDeckEl.innerHTML = [
    safetyTile,
    runTile,
    exposureTile
  ].map(commandTileHTML).join('')
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

const podsFor = cluster => Array.isArray(cluster.pods) ? cluster.pods : []

const statusFor = cluster => {
  if (cluster.reachable) {
    return {
      label: 'Reachable',
      className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    }
  }
  if (cluster.provisioning) {
    return {
      label: 'Provisioning',
      className: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
    }
  }
  if (cluster.available) {
    return {
      label: 'Unavailable',
      className: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
    }
  }
  return {
    label: 'Missing',
    className: 'bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
  }
}

const initializeCollapseState = cluster => {
  if (initializedCollapseState.has(cluster.id)) {
    return
  }

  initializedCollapseState.add(cluster.id)
  if (cluster.type === 'downstream') {
    collapsedClusters.set(cluster.id, true)
    collapsedPods.set(cluster.id, true)
  }
}

const emptyPodsText = cluster => cluster.type === 'downstream'
  ? 'No pods found in the downstream cluster yet.'
  : 'No Rancher/webhook pods found in cattle-system.'

const fetchState = async () => {
  const response = await fetch('/api/state', {
    cache: 'no-store',
    headers: {
      'Accept': 'application/json',
      'X-Control-Panel-Token': token
    }
  })

  if (!response.ok) {
    throw new Error(await response.text() || 'Failed to fetch state')
  }

  return response.json()
}

const badge = label => `<span class="inline-flex items-center rounded-md bg-zinc-100 px-2 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">${escapeHtml(label)}</span>`

const metaItem = (label, value) => `
  <div class="min-w-0">
    <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">${escapeHtml(label)}</div>
    <div class="mt-1 break-words text-sm font-medium text-zinc-800 [overflow-wrap:anywhere] dark:text-zinc-200">${value}</div>
  </div>
`

const pathActionKey = (action, id) => `${action}:${id}`

const flashKubeconfigPathAction = (clusterId, action, status) => {
  const key = pathActionKey(action, clusterId)
  window.clearTimeout(kubeconfigPathActionTimers.get(key))
  kubeconfigPathActionFeedback.set(key, status)
  if (lastState) {
    renderClusters(lastState)
  }
  kubeconfigPathActionTimers.set(key, window.setTimeout(() => {
    if (kubeconfigPathActionFeedback.get(key) === status) {
      kubeconfigPathActionFeedback.delete(key)
      if (lastState) {
        renderClusters(lastState)
      }
    }
  }, 1800))
}

const kubeconfigPathActionContent = (clusterId, action, idleLabel, activeLabel, successLabel, errorLabel) => {
  const active = action === 'open'
    ? activeOpenKubeconfigPathClusterId === clusterId
    : activeCopyKubeconfigPathClusterId === clusterId
  if (active) {
    return `<span class="spinner mr-2 !h-3 !w-3 !border-[1.5px]"></span>${activeLabel}`
  }

  const feedback = kubeconfigPathActionFeedback.get(pathActionKey(action, clusterId))
  if (feedback === 'success') {
    return successLabel
  }
  if (feedback === 'error') {
    return errorLabel
  }
  return idleLabel
}

const renderKubeconfigActions = cluster => {
  if (!cluster.available) {
    return '<span class="text-sm text-zinc-500 dark:text-zinc-400">Kubeconfig unavailable</span>'
  }

  const downloading = activeDownloadClusterId === cluster.id
  const copying = activeCopyClusterId === cluster.id
  const copyingHelm = activeCopyHelmClusterId === cluster.id
  const copyingHelmUpgrade = activeCopyHelmUpgradeClusterId === cluster.id
  const downloadSpinner = downloading ? '<span class="spinner mr-2"></span>' : ''
  const copySpinner = copying ? '<span class="spinner mr-2"></span>' : ''
  const copyHelmSpinner = copyingHelm ? '<span class="spinner mr-2"></span>' : ''
  const copyHelmUpgradeSpinner = copyingHelmUpgrade ? '<span class="spinner mr-2"></span>' : ''
  const downloadLabel = cluster.type === 'downstream' ? 'Download downstream kubeconfig' : 'Download kubeconfig'
  const copyHelmButtons = cluster.type === 'local'
    ? `<div class="flex max-w-full flex-wrap gap-2 rounded-xl border border-zinc-200 bg-zinc-50 p-2 dark:border-white/10 dark:bg-white/[0.03]">
         <button type="button" data-action="copy-helm-command" data-cluster="${escapeHtml(cluster.id)}"${copyingHelm ? ' disabled' : ''} title="Copy the Helm command used during setup." class="inline-flex min-h-11 max-w-full items-center justify-center whitespace-normal rounded-lg border border-zinc-200 bg-white px-4 py-2 text-center text-sm font-semibold text-zinc-700 hover:bg-zinc-50 disabled:cursor-default disabled:opacity-70 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${copyHelmSpinner}${copyingHelm ? 'Copying' : 'Copy install command'}</button>
         <button type="button" data-action="copy-helm-upgrade-command" data-cluster="${escapeHtml(cluster.id)}"${copyingHelmUpgrade ? ' disabled' : ''} title="Copy the setup command converted to helm upgrade --install for editing before an upgrade." class="inline-flex min-h-11 max-w-full items-center justify-center whitespace-normal rounded-lg border border-sky-200 bg-sky-50 px-4 py-2 text-center text-sm font-semibold text-sky-800 hover:bg-sky-100 disabled:cursor-default disabled:opacity-70 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-200 dark:hover:bg-sky-500/15">${copyHelmUpgradeSpinner}${copyingHelmUpgrade ? 'Copying' : 'Copy upgrade draft'}</button>
       </div>`
    : ''

  return `
    <button type="button" data-action="download" data-cluster="${escapeHtml(cluster.id)}"${downloading ? ' disabled' : ''} class="inline-flex min-h-11 max-w-full items-center justify-center whitespace-normal rounded-lg bg-emerald-500 px-4 py-2 text-center text-sm font-semibold text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400 disabled:cursor-default disabled:opacity-70">${downloadSpinner}${downloading ? 'Preparing kubeconfig' : downloadLabel}</button>
    <button type="button" data-action="copy-kubeconfig" data-cluster="${escapeHtml(cluster.id)}"${copying ? ' disabled' : ''} class="inline-flex min-h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 disabled:cursor-default disabled:opacity-70 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${copySpinner}${copying ? 'Copying' : 'Copy kubeconfig'}</button>
    ${copyHelmButtons}
  `
}

const renderPodRows = (cluster, pods, changedLeader) => {
  if (!pods.length) {
    const message = cluster.error ? cluster.error : emptyPodsText(cluster)
    return `<tr><td colspan="8" class="px-3 py-4 text-sm text-zinc-500 dark:text-zinc-400">${escapeHtml(message)}</td></tr>`
  }

  return pods.map(pod => {
    const rowClass = changedLeader && changedLeader === pod.name
      ? 'bg-emerald-50 dark:bg-emerald-500/10'
      : pod.leader
        ? 'bg-emerald-50/70 dark:bg-emerald-500/5'
        : ''
    const leaderBadge = pod.leader && pod.leaderLabel ? badge(pod.leaderLabel) : ''

    return `
      <tr class="${rowClass}">
        <td class="break-words px-3 py-3 align-top text-sm text-zinc-600 dark:text-zinc-400">${escapeHtml(pod.namespace || '')}</td>
        <td class="px-3 py-3 align-top">
          <div class="flex flex-wrap items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100">
            <span>${escapeHtml(pod.name)}</span>
            ${leaderBadge}
          </div>
          <div class="mt-2 flex flex-wrap gap-2">
            <button type="button" data-action="tail" data-cluster="${escapeHtml(cluster.id)}" data-namespace="${escapeHtml(pod.namespace || 'cattle-system')}" data-pod="${escapeHtml(pod.name)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Tail</button>
            <button type="button" data-action="live" data-cluster="${escapeHtml(cluster.id)}" data-namespace="${escapeHtml(pod.namespace || 'cattle-system')}" data-pod="${escapeHtml(pod.name)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Live</button>
          </div>
        </td>
        <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.ready)}</td>
        <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.status)}</td>
        <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${pod.restarts}</td>
        <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.age)}</td>
        <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.node || '')}</td>
        <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.containers)}</td>
      </tr>
    `
  }).join('')
}

const renderPodsTable = (cluster, pods, changedLeader) => {
  const podsCollapsed = collapsedPods.get(cluster.id) === true
  const toggleText = podsCollapsed ? 'Show pods' : 'Hide pods'

  if (cluster.provisioning) {
    return `
      <div class="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm font-medium text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">
        <span class="spinner mr-2"></span>${escapeHtml(cluster.provisioningMessage || 'Provisioning downstream cluster')}
      </div>
    `
  }

  return `
    <div class="mt-4 flex items-center justify-between gap-3">
      <div class="text-sm font-semibold text-zinc-950 dark:text-zinc-100">Pods <span class="text-zinc-500 dark:text-zinc-400">${pods.length}</span></div>
      <button type="button" data-action="toggle-pods" data-cluster="${escapeHtml(cluster.id)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${toggleText}</button>
    </div>
    ${podsCollapsed ? '' : `
      <div class="mt-3 max-w-full overflow-hidden rounded-xl border border-zinc-200 dark:border-white/10">
        <table class="w-full table-fixed border-collapse text-left">
          <colgroup>
            <col class="w-[9rem]" />
            <col class="w-[24rem]" />
            <col class="w-[5rem]" />
            <col class="w-[7rem]" />
            <col class="w-[6rem]" />
            <col class="w-[5rem]" />
            <col class="w-[12rem]" />
            <col />
          </colgroup>
          <thead class="bg-zinc-50 dark:bg-white/[0.04]">
            <tr>
              ${['Namespace', 'Pod', 'Ready', 'Status', 'Restarts', 'Age', 'Node', 'Containers'].map(label => `<th class="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">${label}</th>`).join('')}
            </tr>
          </thead>
          <tbody class="divide-y divide-zinc-200 dark:divide-white/10">
            ${renderPodRows(cluster, pods, changedLeader)}
          </tbody>
        </table>
      </div>
    `}
  `
}

const renderCluster = cluster => {
  initializeCollapseState(cluster)

  const pods = podsFor(cluster)
  const currentLeader = pods.find(pod => pod.leader && pod.leaderLabel === 'Leader') || pods.find(pod => pod.leader)
  const changedLeader = pendingLeaderHighlights.get(cluster.id)
  const isDownstream = cluster.type === 'downstream'
  const status = statusFor(cluster)
  const clusterCollapsed = collapsedClusters.get(cluster.id) === true
  const toggleText = clusterCollapsed ? 'Show details' : 'Hide details'
  const version = cluster.version ? ` <span class="text-zinc-500 dark:text-zinc-400">(${escapeHtml(cluster.version)})</span>` : ''
  const typeBadge = badge(isDownstream ? 'Downstream' : 'Local')
  const contextParts = isDownstream
    ? [`Downstream from HA ${cluster.haIndex}`]
    : [`Management cluster for HA ${cluster.haIndex}`]

  if (isDownstream && cluster.namespace) {
    contextParts.push(`namespace ${cluster.namespace}`)
  }
  if (isDownstream && cluster.managementClusterId) {
    contextParts.push(cluster.managementClusterId)
  }

  const rancherURL = cluster.rancherUrl
    ? `<a href="${escapeHtml(cluster.rancherUrl)}" data-external-url="${escapeHtml(cluster.rancherUrl)}" class="text-emerald-600 hover:text-emerald-500 dark:text-emerald-300">${escapeHtml(cluster.rancherUrl)}</a>`
    : '<span class="text-zinc-500 dark:text-zinc-400">Unavailable</span>'
  const loadBalancer = cluster.loadBalancer ? escapeHtml(cluster.loadBalancer) : '<span class="text-zinc-500 dark:text-zinc-400">Unavailable</span>'
  const openingKubeconfigPath = activeOpenKubeconfigPathClusterId === cluster.id
  const copyingKubeconfigPath = activeCopyKubeconfigPathClusterId === cluster.id
  const openPathFeedback = kubeconfigPathActionFeedback.get(pathActionKey('open', cluster.id))
  const copyPathFeedback = kubeconfigPathActionFeedback.get(pathActionKey('copy', cluster.id))
  const pathButtonBaseClass = 'inline-flex min-h-8 items-center justify-center rounded-md border px-2.5 py-1.5 text-xs font-semibold disabled:cursor-default disabled:opacity-70'
  const openPathToneClass = openPathFeedback === 'error'
    ? 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200'
    : openPathFeedback === 'success'
      ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200'
      : 'border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'
  const copyPathToneClass = copyPathFeedback === 'error'
    ? 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200'
    : copyPathFeedback === 'success'
      ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200'
      : 'border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'
  const kubeconfig = cluster.kubeconfigPath ? `
    <div class="space-y-2">
      <div>${escapeHtml(cluster.kubeconfigPath)}</div>
      <div class="flex flex-wrap gap-2">
        <button type="button" data-action="open-kubeconfig-folder" data-cluster="${escapeHtml(cluster.id)}"${openingKubeconfigPath ? ' disabled' : ''} class="${pathButtonBaseClass} ${openPathToneClass}">${kubeconfigPathActionContent(cluster.id, 'open', 'Open folder', 'Opening', 'Opened', 'Open failed')}</button>
        <button type="button" data-action="copy-kubeconfig-path" data-cluster="${escapeHtml(cluster.id)}"${copyingKubeconfigPath ? ' disabled' : ''} class="${pathButtonBaseClass} ${copyPathToneClass}">${kubeconfigPathActionContent(cluster.id, 'copy', 'Copy path', 'Copying', 'Copied', 'Copy failed')}</button>
      </div>
    </div>
  ` : '<span class="text-zinc-500 dark:text-zinc-400">Generated on download</span>'
  const namespace = cluster.namespace ? metaItem('Namespace', escapeHtml(cluster.namespace)) : ''
  const clusterID = cluster.managementClusterId ? metaItem('Cluster ID', escapeHtml(cluster.managementClusterId)) : ''
  const leaderSummary = currentLeader
    ? `<div class="mt-4 text-sm text-zinc-600 dark:text-zinc-400"><strong class="text-zinc-950 dark:text-zinc-100">Active Leader</strong> ${escapeHtml(currentLeader.name)}</div>`
    : '<div class="mt-4 text-sm text-zinc-500 dark:text-zinc-400">Leader not detected yet.</div>'
  const downstreamClasses = isDownstream
    ? 'border-l-4 border-l-emerald-500 bg-emerald-50/50 dark:bg-emerald-500/[0.04]'
    : 'bg-white dark:bg-white/[0.03]'

  return `
    <article class="min-w-0 overflow-hidden rounded-2xl border border-zinc-200 ${downstreamClasses} p-4 shadow-sm dark:border-white/10">
      <div class="flex min-w-0 flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2 text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">
            <span>${escapeHtml(cluster.name)}${version}</span>
            ${typeBadge}
          </div>
          <div class="mt-1 break-words text-sm font-medium text-zinc-500 dark:text-zinc-400">${escapeHtml(contextParts.join(' • '))}</div>
        </div>
        <div class="flex min-w-0 flex-wrap items-center gap-2 lg:max-w-sm lg:justify-end">
          ${renderKubeconfigActions(cluster)}
          <button type="button" data-action="toggle-cluster" data-cluster="${escapeHtml(cluster.id)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${toggleText}</button>
          <span class="inline-flex items-center rounded-full px-3 py-1.5 text-xs font-semibold ${status.className}">${cluster.provisioning ? '<span class="spinner mr-2"></span>' : ''}${status.label}</span>
        </div>
      </div>
      ${clusterCollapsed ? '' : `
        <div class="mt-4 grid min-w-0 gap-3 sm:grid-cols-2 xl:grid-cols-[repeat(3,minmax(0,1fr))]">
          ${metaItem('Rancher URL', rancherURL)}
          ${metaItem('Load Balancer', loadBalancer)}
          ${metaItem('Kubeconfig', kubeconfig)}
          ${namespace}
          ${clusterID}
        </div>
        ${leaderSummary}
        ${renderPodsTable(cluster, pods, changedLeader)}
      `}
    </article>
  `
}

const clusterRunKey = cluster => String(cluster?.runId || 'default')

const clusterHAKey = cluster => String(cluster?.haIndex || 0)

const runLabelForClusterGroup = (runKey, workspace) => {
  const runs = Array.isArray(workspace?.runs) ? workspace.runs : []
  const run = runs.find(item => String(item.runId || 'default') === runKey)
  if (run?.runId) {
    return `Run ${run.runId}`
  }
  if (run?.slotId) {
    return run.slotId.replace(/^slot-/, 'Slot ')
  }
  if (runKey !== 'default') {
    return `Run ${runKey}`
  }
  return 'Default slot'
}

const buildClusterGroups = (items, workspace) => {
  const runOrder = []
  const groups = new Map()
  const runs = Array.isArray(workspace?.runs) ? workspace.runs : []

  runs.forEach(run => {
    const runKey = String(run.runId || 'default')
    if (!groups.has(runKey)) {
      groups.set(runKey, {
        runKey,
        label: runLabelForClusterGroup(runKey, workspace),
        run,
        haOrder: [],
        has: new Map()
      })
      runOrder.push(runKey)
    }
  })

  items.forEach(cluster => {
    const runKey = clusterRunKey(cluster)
    if (!groups.has(runKey)) {
      groups.set(runKey, {
        runKey,
        label: runLabelForClusterGroup(runKey, workspace),
        run: null,
        haOrder: [],
        has: new Map()
      })
      runOrder.push(runKey)
    }

    const group = groups.get(runKey)
    const haKey = clusterHAKey(cluster)
    if (!group.has.has(haKey)) {
      group.has.set(haKey, {
        haKey,
        haIndex: cluster.haIndex || 0,
        local: null,
        downstreams: []
      })
      group.haOrder.push(haKey)
    }

    const ha = group.has.get(haKey)
    if (cluster.type === 'downstream') {
      ha.downstreams.push(cluster)
    } else {
      ha.local = cluster
    }
  })

  return runOrder
    .map(runKey => groups.get(runKey))
    .filter(Boolean)
    .map(group => ({
      ...group,
      has: group.haOrder
        .map(haKey => group.has.get(haKey))
        .filter(Boolean)
        .sort((left, right) => (left.haIndex || 0) - (right.haIndex || 0))
    }))
}

const renderClusters = state => {
  const cleanup = state?.cleanup || {}

  if (cleanup.running) {
    clustersEl.innerHTML = `
      <div class="rounded-2xl border border-sky-200 bg-sky-50 p-6 text-center dark:border-sky-500/20 dark:bg-sky-500/10">
        <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">
          <span class="spinner"></span>
        </div>
        <h3 class="mt-4 text-lg font-semibold tracking-tight text-sky-950 dark:text-sky-100">Infrastructure is being torn down</h3>
        <p class="mx-auto mt-2 max-w-2xl text-sm leading-6 text-sky-800/80 dark:text-sky-200/80">
          Destroy is removing Terraform resources for the selected run. Cluster details are paused so the panel does not show stale unavailable infrastructure.
        </p>
        <button type="button" data-action="open-cleanup-logs" class="mt-4 rounded-lg border border-sky-200 bg-white px-4 py-2 text-sm font-semibold text-sky-800 shadow-sm hover:bg-sky-50 dark:border-sky-500/30 dark:bg-white/[0.06] dark:text-sky-200 dark:hover:bg-white/[0.1]">Open destroy logs</button>
      </div>
    `
    return
  }

  const items = clusterItems(state)

  if (!items.length) {
    if (cleanup.finishedAt && !cleanup.error && !cleanupResultDismissed(cleanup)) {
      clustersEl.innerHTML = `
        <div class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm font-medium text-emerald-800 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-200">
          Destroy finished for the selected run. Cluster records were cleared after Terraform destroy succeeded.
        </div>
      `
      return
    }
    clustersEl.innerHTML = '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No clusters discovered yet.</div>'
    return
  }

  const groups = buildClusterGroups(items, state?.workspace)
  if (!groups.length) {
    clustersEl.innerHTML = '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No clusters discovered yet.</div>'
    return
  }

  if (!activeClusterRunKey || !groups.some(group => group.runKey === activeClusterRunKey)) {
    activeClusterRunKey = groups[0].runKey
  }
  const activeRunGroup = groups.find(group => group.runKey === activeClusterRunKey) || groups[0]
  if (!activeClusterHAKey || !activeRunGroup.has.some(ha => ha.haKey === activeClusterHAKey)) {
    activeClusterHAKey = activeRunGroup.has[0]?.haKey || ''
  }
  const activeHA = activeRunGroup.has.find(ha => ha.haKey === activeClusterHAKey) || activeRunGroup.has[0]

  const runTabs = groups.map(group => {
    const active = group.runKey === activeRunGroup.runKey
    const clusterCount = group.has.reduce((count, ha) => count + (ha.local ? 1 : 0) + ha.downstreams.length, 0)
    return `
      <button type="button" data-action="select-cluster-run" data-run-key="${escapeHtml(group.runKey)}" class="${active ? 'rounded-md border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-sm font-semibold text-emerald-800 shadow-sm dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200' : 'rounded-md border border-zinc-200 bg-white px-3 py-1.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'}">
        ${escapeHtml(group.label)}
        <span class="${active ? 'ml-2 text-emerald-600 dark:text-emerald-300' : 'ml-2 text-zinc-500 dark:text-zinc-400'}">${clusterCount}</span>
      </button>
    `
  }).join('')

  const haTabs = activeRunGroup.has.map(ha => {
    const active = ha.haKey === activeHA?.haKey
    const downstreamCount = ha.downstreams.length
    const version = ha.local?.version ? ` • ${ha.local.version}` : ''
    return `
      <button type="button" data-action="select-cluster-ha" data-ha-key="${escapeHtml(ha.haKey)}" class="${active ? 'rounded-md border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-sm font-semibold text-emerald-800 shadow-sm dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200' : 'rounded-md border border-zinc-200 bg-white px-3 py-1.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'}">
        HA ${escapeHtml(ha.haIndex || ha.haKey)}${escapeHtml(version)}
        <span class="${active ? 'ml-2 text-emerald-600 dark:text-emerald-300' : 'ml-2 text-zinc-500 dark:text-zinc-400'}">${downstreamCount} downstream</span>
      </button>
    `
  }).join('')

  const localCluster = activeHA?.local
  const downstreams = activeHA?.downstreams || []
  const localHTML = localCluster
    ? renderCluster(localCluster)
    : '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No local cluster record found for this HA yet.</div>'
  const downstreamHTML = downstreams.length
    ? downstreams.map(renderCluster).join('')
    : '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No downstream clusters discovered for this HA yet.</div>'

  clustersEl.innerHTML = `
    <div class="grid gap-4">
      <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Run slot</div>
        <div class="mt-2 flex flex-wrap gap-2">${runTabs}</div>
      </div>
      <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">HA cluster</div>
        <div class="mt-2 flex flex-wrap gap-2">${haTabs}</div>
      </div>
      <div class="grid gap-4">
        <div>
          <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">Management cluster</div>
          ${localHTML}
        </div>
        <div>
          <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">Downstream clusters</div>
          <div class="grid gap-4">${downstreamHTML}</div>
        </div>
      </div>
    </div>
  `
}

const renderAWSInventory = inventory => {
  if (!awsInventoryEl || !awsInventorySummaryEl) {
    return
  }

  const items = Array.isArray(inventory?.items) ? inventory.items : []
  const updated = inventory?.updatedAt ? new Date(inventory.updatedAt).toLocaleTimeString() : ''
  const queries = Array.isArray(inventory?.queries) ? inventory.queries : []
  const queryText = queries.length ? queries.join(' • ') : 'No scoped AWS query yet'
  const owner = inventory?.owner ? `Owner ${inventory.owner}` : 'Owner tag not configured'
  const region = inventory?.region || 'region unavailable'

  awsInventoryMetaEl.textContent = updated ? `Updated ${updated}` : ''
  awsInventorySummaryEl.textContent = `${items.length} matching AWS resource${items.length === 1 ? '' : 's'} in ${region}. ${owner}. ${queryText}.`

  if (inventory?.error) {
    awsInventoryEl.innerHTML = `
      <div class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">
        ${escapeHtml(inventory.error)}
      </div>
    `
    if (!items.length) {
      return
    }
  }

  if (!items.length) {
    awsInventoryEl.innerHTML = '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No matching AWS resources found for the recorded run prefixes or Owner tag.</div>'
    return
  }

  const counts = items.reduce((acc, item) => {
    acc[item.type || 'AWS resource'] = (acc[item.type || 'AWS resource'] || 0) + 1
    return acc
  }, {})
  const countBadges = Object.entries(counts)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([type, count]) => badge(`${type}: ${count}`))
    .join('')

  awsInventoryEl.innerHTML = `
    <div class="flex flex-wrap gap-2">${countBadges}</div>
    <div class="overflow-hidden rounded-xl border border-zinc-200 dark:border-white/10">
      <table class="w-full table-fixed border-collapse text-left">
        <colgroup>
          <col class="w-[11rem]" />
          <col class="w-[18rem]" />
          <col class="w-[9rem]" />
          <col class="w-[9rem]" />
          <col />
        </colgroup>
        <thead class="bg-zinc-50 dark:bg-white/[0.04]">
          <tr>
            ${['Type', 'Name', 'Status', 'Run', 'Details'].map(label => `<th class="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">${label}</th>`).join('')}
          </tr>
        </thead>
        <tbody class="divide-y divide-zinc-200 dark:divide-white/10">
          ${items.map(item => {
            const tags = item.tags
              ? Object.entries(item.tags).slice(0, 5).map(([key, value]) => `${key}=${value}`).join(' • ')
              : ''
            return `
              <tr>
                <td class="break-words px-3 py-3 align-top text-sm font-semibold text-zinc-900 dark:text-zinc-100">${escapeHtml(item.type || 'AWS resource')}</td>
                <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">
                  <div class="font-medium">${escapeHtml(item.name || item.id || '')}</div>
                  <div class="mt-1 text-xs text-zinc-500 dark:text-zinc-500">${escapeHtml(item.region || '')}</div>
                </td>
                <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(item.status || '')}</td>
                <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(item.runId || '')}</td>
                <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">
                  <div>${escapeHtml(item.details || item.id || '')}</div>
                  ${item.owner ? `<div class="mt-1 text-xs text-zinc-500 dark:text-zinc-500">Owner ${escapeHtml(item.owner)}</div>` : ''}
                  ${tags ? `<div class="mt-1 text-xs text-zinc-500 dark:text-zinc-500">${escapeHtml(tags)}</div>` : ''}
                </td>
              </tr>
            `
          }).join('')}
        </tbody>
      </table>
    </div>
  `
}

const updateLeaderTracking = state => {
  const messages = []
  const nextLeaders = new Map()

  clusterItems(state).forEach(cluster => {
    const pods = podsFor(cluster)
    const currentLeader = pods.find(pod => pod.leader && pod.leaderLabel === 'Leader') || pods.find(pod => pod.leader)
    const currentLeaderName = currentLeader ? currentLeader.name : ''
    const previousLeaderName = previousLeaders.get(cluster.id) || ''

    if (currentLeaderName) {
      nextLeaders.set(cluster.id, currentLeaderName)
    }

    if (currentLeaderName && previousLeaderName && previousLeaderName !== currentLeaderName) {
      pendingLeaderHighlights.set(cluster.id, currentLeaderName)
      window.setTimeout(() => {
        if (pendingLeaderHighlights.get(cluster.id) === currentLeaderName) {
          pendingLeaderHighlights.delete(cluster.id)
        }
      }, 4500)
      messages.push(`${cluster.name} leader changed to ${currentLeaderName}`)
    }
  })

  previousLeaders = nextLeaders
  lastLeaderChangeMessage = messages.join(' • ')
}

const operationOutput = operation => operation && Array.isArray(operation.output) ? operation.output : []

const renderBuildVersion = build => {
  if (!buildVersionBadgeEl) {
    return
  }

  const shortCommit = String(build?.commitShort || '').trim()
  const fullCommit = String(build?.commit || '').trim()
  const buildDate = String(build?.buildDate || '').trim()
  const modified = Boolean(build?.modified)
  buildVersionBadgeEl.textContent = shortCommit ? `Build ${shortCommit}${modified ? '*' : ''}` : 'Build unknown'

  const titleParts = []
  if (fullCommit) {
    titleParts.push(`Commit: ${fullCommit}`)
  }
  if (buildDate) {
    titleParts.push(`Built: ${buildDate}`)
  }
  if (modified) {
    titleParts.push('Working tree had local changes when this binary was built.')
  }
  buildVersionBadgeEl.title = titleParts.length ? titleParts.join('\n') : 'No build commit was embedded in this binary.'
}

const renderPanelSession = panel => {
  if (!panelSessionMetaEl || !panel?.sessionId) {
    renderBuildVersion(panel?.build)
    return
  }

  renderBuildVersion(panel.build)

  const started = panel.startedAt ? new Date(panel.startedAt).toLocaleTimeString() : ''
  const pieces = [`Panel ${panel.sessionId}`]
  if (started) {
    pieces.push(`started ${started}`)
  }
  if (panel.repoRoot) {
    pieces.push(panel.repoRoot)
  }
  panelSessionMetaEl.textContent = pieces.join(' • ')
  if (panel.configPath) {
    panelSessionMetaEl.title = panel.configPath
  }

  if (configNoticeEl) {
    if (panel.starterConfigCreated) {
      configNoticeEl.classList.remove('hidden')
      configNoticeEl.textContent = `Created starter config at ${panel.configPath}. Fill in the blocked setup values below before starting setup.`
    } else {
      configNoticeEl.classList.add('hidden')
      configNoticeEl.textContent = ''
    }
  }
}

const runVersionsLabel = run => Array.isArray(run?.rancherVersions) && run.rancherVersions.length
  ? run.rancherVersions.join(', ')
  : 'not recorded'

const runHostnameLabel = run => {
  if (!run) {
    return 'not recorded'
  }
  if (run.customHostnamePrefix) {
    return `${run.customHostnamePrefix}.${run.route53Fqdn || ''}`.replace(/\.$/, '')
  }
  return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-h*.${run.route53Fqdn}` : run.route53Fqdn || 'generated per slot'
}

const sameRunKey = (left, right) => String(left || '').trim() === String(right || '').trim()

const operationForRun = run => {
  const runId = run?.runId || ''
  const operations = [
    ['setup', 'Setup', lastState?.setup],
    ['readiness', 'Readiness', lastState?.readiness],
    ['cleanup', 'Destroy', lastState?.cleanup]
  ]
  return operations.find(([, , operation]) => operation?.running && sameRunKey(operation.runId, runId)) || null
}

const operationBadgeHTML = operation => {
  if (!operation) {
    return ''
  }
  const [, label, snapshot] = operation
  const started = snapshot?.startedAt ? ` since ${new Date(snapshot.startedAt).toLocaleTimeString()}` : ''
  return `<span class="inline-flex items-center rounded-full bg-sky-100 px-2.5 py-1 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300"><span class="spinner mr-1.5 !h-3 !w-3 !border-[1.5px]"></span>${escapeHtml(label)} running${escapeHtml(started)}</span>`
}

const runHasFailure = run => {
  const status = String(run?.status || '').toLowerCase()
  return status.includes('failed') || status.includes('error')
}

const readinessFailedRun = runs => {
  const readiness = lastState?.readiness || {}
  if (readiness.running || !readiness.error) {
    return runs.find(run => runHasFailure(run)) || null
  }
  const failedRunId = readiness.runId || ''
  return runs.find(run => sameRunKey(run.runId, failedRunId)) || runs.find(run => runHasFailure(run)) || null
}

const runClusterStats = (run, state = lastState) => {
  const runId = run?.runId || ''
  const items = clusterItems(state).filter(cluster => sameRunKey(cluster.runId, runId))
  const management = items.filter(cluster => cluster.type !== 'downstream').length
  const downstream = items.filter(cluster => cluster.type === 'downstream').length
  const reachable = items.filter(cluster => cluster.reachable).length
  return { management, downstream, reachable, total: items.length }
}

const runTone = (run, operation) => {
  const status = String(run?.status || '').toLowerCase()
  if (operation) {
    return 'sky'
  }
  if (runHasFailure(run)) {
    return 'rose'
  }
  if (status === 'ready' || status.includes('complete')) {
    return 'emerald'
  }
  return 'zinc'
}

const runStatusClasses = tone => ({
  emerald: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200',
  sky: 'border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-200',
  rose: 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200',
  zinc: 'border-zinc-200 bg-zinc-50 text-zinc-700 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300'
})[tone] || runStatusClasses('zinc')

const runStepClass = state => ({
  done: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200',
  active: 'border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-200',
  failed: 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200',
  waiting: 'border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-400'
})[state] || runStepClass('waiting')

const runTimelineHTML = run => {
  const status = String(run?.status || '').toLowerCase()
  const setupRunning = lastState?.setup?.running && sameRunKey(lastState.setup.runId, run.runId)
  const readinessRunning = lastState?.readiness?.running && sameRunKey(lastState.readiness.runId, run.runId)
  const cleanupRunning = lastState?.cleanup?.running && sameRunKey(lastState.cleanup.runId, run.runId)
  const setupDone = status.includes('setup_complete') || status === 'ready' || status.includes('readiness') || status.includes('cleanup')
  const readinessDone = status === 'ready'

  const steps = [
    {
      label: 'Setup',
      state: setupRunning ? 'active' : status.includes('setup_failed') ? 'failed' : setupDone ? 'done' : 'waiting'
    },
    {
      label: 'Readiness',
      state: readinessRunning ? 'active' : status.includes('readiness_failed') ? 'failed' : readinessDone ? 'done' : 'waiting'
    },
    {
      label: 'Destroy',
      state: cleanupRunning ? 'active' : status.includes('cleanup_failed') ? 'failed' : 'waiting'
    }
  ]

  return `
    <div class="mt-4 grid gap-2 sm:grid-cols-3">
      ${steps.map(step => `
        <div class="rounded-lg border px-3 py-2 ${runStepClass(step.state)}">
          <div class="flex items-center gap-2">
            <span class="${step.state === 'active' ? 'spinner !h-3 !w-3 !border-[1.5px]' : 'h-2.5 w-2.5 rounded-full ' + (step.state === 'done' ? 'bg-emerald-500' : step.state === 'failed' ? 'bg-rose-500' : 'bg-zinc-300 dark:bg-zinc-600')}"></span>
            <span class="text-xs font-semibold uppercase tracking-wide">${escapeHtml(step.label)}</span>
          </div>
        </div>
      `).join('')}
    </div>
  `
}

const trimTrailingPathSeparator = value => String(value || '').replace(/[\\/]+$/, '')

const parentPath = value => {
  const path = trimTrailingPathSeparator(value)
  const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'))
  return index > 0 ? path.slice(0, index) : path
}

const runFolderPath = run => {
  const terraformModule = trimTrailingPathSeparator(run?.terraformModuleDir || '')
  if (terraformModule) {
    return terraformModule.replace(/[\\/]terraform[\\/]module$/, '')
  }
  const terraformState = trimTrailingPathSeparator(run?.terraformStatePath || '')
  if (terraformState) {
    return terraformState.replace(/[\\/]terraform[\\/]terraform\.tfstate$/, '')
  }
  const haRoot = trimTrailingPathSeparator(run?.haOutputRoot || '')
  if (haRoot) {
    return haRoot.replace(/[\\/]ha$/, '')
  }
  return ''
}

const runTerraformPath = run => {
  if (run?.terraformModuleDir) {
    return run.terraformModuleDir
  }
  if (run?.terraformStatePath) {
    return parentPath(run.terraformStatePath)
  }
  if (run?.terraformBackend) {
    return run.terraformBackend
  }
  return ''
}

const renderRunActionButton = ({ action, runId, label, variant = 'secondary', disabled = false, title = '' }) => {
  const classes = disabled
    ? 'run-action-button bg-zinc-200 text-zinc-500 dark:bg-white/[0.06] dark:text-zinc-400'
    : variant === 'danger'
      ? 'run-action-button bg-rose-500 text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400'
      : variant === 'primary'
        ? 'run-action-button bg-emerald-500 text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400'
        : variant === 'blue'
          ? 'run-action-button bg-sky-500 text-white shadow-sm shadow-sky-500/20 hover:bg-sky-400'
          : 'run-action-button border border-zinc-200 bg-white text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'
  return `<button type="button" data-run-action="${escapeHtml(action)}" data-run-id="${escapeHtml(runId || '')}" ${disabled ? 'disabled' : ''} ${title ? `title="${escapeHtml(title)}"` : ''} class="${classes}">${label}</button>`
}

const renderWorkspace = workspace => {
  if (!workspaceModeEl || !workspace) {
    return
  }

  const mode = workspace.mode || 'single-run workspace'
  const sharedPaths = Array.isArray(workspace.sharedPathLabels) ? workspace.sharedPathLabels : []
  const runs = Array.isArray(workspace.runs) ? workspace.runs : []
  const totalHAs = runs.reduce((total, run) => total + Number(run.totalHAs || 1), 0)
  const currentRunID = workspace.currentRun?.runId || ''
  const activeOperation = [
    ['setup', 'Setup', lastState?.setup],
    ['readiness', 'Readiness', lastState?.readiness],
    ['cleanup', 'Destroy', lastState?.cleanup]
  ].find(([, , operation]) => operation?.running)

  workspaceModeEl.textContent = mode
  if (workspaceSlotTitleEl) {
    workspaceSlotTitleEl.textContent = runs.length
      ? `${runs.length} recorded run slot${runs.length === 1 ? '' : 's'}`
      : 'No recorded runs'
  }
  if (workspaceSlotSummaryEl) {
    workspaceSlotSummaryEl.textContent = activeOperation
      ? `${activeOperation[1]} is active. Setup, readiness, and destroy stay serialized so Terraform state and AWS actions remain unambiguous.`
      : runs.length
        ? 'Every slot below has isolated Terraform state, HA output, kubeconfigs, AWS names, logs, and a dedicated destroy target.'
        : 'Use Setup to resolve and approve a Rancher HA plan. The run will appear here before AWS resources are created.'
  }
  if (selectedCleanupRunId && !runs.some(run => run.runId === selectedCleanupRunId)) {
    selectedCleanupRunId = ''
  }

  if (workspaceSlotGridEl) {
    const liveClusters = clusterItems(lastState).length
    const awsResources = Array.isArray(lastState?.aws?.items) ? lastState.aws.items.length : 0
    const canStart = workspace.canStartIsolatedRun && !lifecycleRunning(lastState) && !bootStatePending
    workspaceSlotGridEl.innerHTML = `
      <div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-3 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Slots</div>
        <div class="mt-1 text-xl font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(String(runs.length))}</div>
        <div class="mt-1 text-xs text-zinc-500 dark:text-zinc-400">${escapeHtml(totalHAs)} HA target${totalHAs === 1 ? '' : 's'}</div>
      </div>
      <div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-3 dark:border-white/10 dark:bg-white/[0.03]">
        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Discovered</div>
        <div class="mt-1 text-xl font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(String(liveClusters))}</div>
        <div class="mt-1 text-xs text-zinc-500 dark:text-zinc-400">cluster records</div>
      </div>
      <div class="rounded-lg border ${canStart ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200' : 'border-zinc-200 bg-zinc-50 text-zinc-700 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-300'} px-3 py-3">
        <div class="text-xs font-semibold uppercase tracking-wide opacity-75">Next setup</div>
        <div class="mt-1 text-sm font-semibold">${escapeHtml(canStart ? 'Ready' : bootStatePending ? 'Checking state' : lifecycleRunning(lastState) ? 'Lifecycle running' : workspace.isolatedRunBlockedReason || 'Locked')}</div>
        <div class="mt-1 text-xs opacity-75">${escapeHtml(awsResources)} AWS resource${awsResources === 1 ? '' : 's'} visible</div>
      </div>
      ${sharedPaths.length && !runs.length ? `<div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-3 dark:border-white/10 dark:bg-white/[0.03]"><div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Workspace guard</div><div class="mt-1 text-sm font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(String(sharedPaths.length))} watched paths</div></div>` : ''}
    `
  }

  if (workspaceRunMetaEl) {
    if (!runs.length) {
      workspaceRunMetaEl.innerHTML = `
        <div class="rounded-xl border border-zinc-200 bg-zinc-50 p-5 dark:border-white/10 dark:bg-white/[0.03]">
          <h3 class="text-base font-semibold text-zinc-950 dark:text-zinc-50">No run slots yet</h3>
          <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
            Setup is the only place that can create a new AWS run. Resolve the plan there, review the Helm commands, then approve AWS setup.
          </p>
          <div class="mt-4">
            ${renderRunActionButton({ action: 'open-setup', label: 'Open setup', variant: 'primary', disabled: bootStatePending || lifecycleRunning(lastState), title: bootStatePending ? 'Startup safety check is still running.' : lifecycleRunning(lastState) ? 'Wait for the active lifecycle operation to finish.' : '' })}
          </div>
        </div>
      `
    } else {
      const failedReadinessRun = readinessFailedRun(runs)
      const banner = activeOperation ? (() => {
        const [mode, label, snapshot] = activeOperation
        const started = snapshot?.startedAt ? `Started ${new Date(snapshot.startedAt).toLocaleTimeString()}` : 'Starting now'
        const runText = snapshot?.runId ? `Run ${snapshot.runId}` : 'Run state publishing'
        const logAction = mode === 'setup' ? 'open-setup-logs' : mode === 'readiness' ? 'open-readiness-logs' : 'open-cleanup-logs'
        const stopAction = mode === 'readiness'
          ? renderRunActionButton({ action: 'stop-readiness-open-destroy', runId: snapshot?.runId || '', label: 'Stop readiness, then destroy', variant: 'danger' })
          : mode === 'setup'
            ? renderRunActionButton({ action: 'stop-setup-open-destroy', runId: snapshot?.runId || '', label: 'Stop setup, then destroy', variant: 'danger' })
            : ''
        return `
          <div class="rounded-xl border border-sky-200 bg-sky-50 p-4 dark:border-sky-500/25 dark:bg-sky-500/10">
            <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div class="min-w-0">
                <div class="inline-flex items-center rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-sky-700 shadow-sm dark:bg-white/[0.08] dark:text-sky-200"><span class="spinner mr-1.5 !h-3 !w-3 !border-[1.5px]"></span>${escapeHtml(label)} running</div>
                <h3 class="mt-2 text-base font-semibold text-sky-950 dark:text-sky-100">${escapeHtml(runText)}</h3>
                <p class="mt-1 text-sm leading-6 text-sky-800/80 dark:text-sky-100/75">${escapeHtml(started)}. New setup, readiness, and destroy actions are locked until this operation finishes.</p>
              </div>
              <div class="flex shrink-0 flex-wrap gap-2">
                ${renderRunActionButton({ action: logAction, label: 'Open logs' })}
                ${stopAction}
              </div>
            </div>
          </div>
        `
      })() : failedReadinessRun ? `
        <div class="rounded-xl border border-rose-200 bg-rose-50 p-4 dark:border-rose-500/25 dark:bg-rose-500/10">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div class="min-w-0">
              <div class="inline-flex items-center rounded-full bg-white px-2.5 py-1 text-xs font-semibold text-rose-700 shadow-sm dark:bg-white/[0.08] dark:text-rose-200">Readiness failed</div>
              <h3 class="mt-2 text-base font-semibold text-rose-950 dark:text-rose-100">Run ${escapeHtml(failedReadinessRun.runId || 'unknown')} did not become ready</h3>
              <p class="mt-1 text-sm leading-6 text-rose-800/80 dark:text-rose-100/75">If a manual Helm command left Rancher unhealthy, destroy this slot from the recorded Terraform state and start again with a corrected command.</p>
            </div>
            <div class="flex shrink-0 flex-wrap gap-2">
              ${renderRunActionButton({ action: 'open-readiness-logs', runId: failedReadinessRun.runId, label: 'Readiness logs' })}
              ${renderRunActionButton({ action: 'open-destroy', runId: failedReadinessRun.runId, label: 'Destroy failed run', variant: 'danger' })}
            </div>
          </div>
        </div>
      ` : ''

      workspaceRunMetaEl.innerHTML = `
        ${banner}
        <div class="grid gap-3">
          ${runs.map(run => {
            const operation = operationForRun(run)
            const tone = runTone(run, operation)
            const stats = runClusterStats(run)
            const updated = run.updatedAt ? new Date(run.updatedAt).toLocaleTimeString() : ''
            const isCurrent = currentRunID && sameRunKey(run.runId, currentRunID)
            const lifecycleBusy = lifecycleRunning(lastState) || bootStatePending
            const readinessDisabled = lifecycleBusy || !isCurrent
            const setupRunningForRun = lastState?.setup?.running && sameRunKey(lastState.setup.runId, run.runId)
            const readinessRunningForRun = lastState?.readiness?.running && sameRunKey(lastState.readiness.runId, run.runId)
            const failedRun = runHasFailure(run)
            const readinessTitle = bootStatePending
              ? 'Startup safety check is still running.'
              : lifecycleRunning(lastState)
                ? 'Wait for the active lifecycle operation to finish.'
                : !isCurrent
                  ? 'Readiness currently runs against the active/current slot only.'
                  : 'Check readiness for the current run.'

            return `
              <article class="rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.03]">
                <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <h3 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Run ${escapeHtml(run.runId || 'unknown')}</h3>
                      <span class="rounded-full border px-2.5 py-1 text-xs font-semibold ${runStatusClasses(tone)}">${escapeHtml((operation ? 'running' : run.status || 'recorded').replaceAll('_', ' '))}</span>
                      ${isCurrent ? '<span class="rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">current slot</span>' : ''}
                      ${operationBadgeHTML(operation)}
                    </div>
                    ${updated ? `<div class="mt-1 text-xs text-zinc-500 dark:text-zinc-400">Updated ${escapeHtml(updated)}</div>` : ''}
                    ${runTimelineHTML(run)}
                    <div class="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                      <div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-2.5 dark:border-white/10 dark:bg-zinc-950/30">
                        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Rancher</div>
                        <div class="mt-1 truncate text-sm font-semibold text-zinc-950 dark:text-zinc-50" title="${escapeHtml(runVersionsLabel(run))}">${escapeHtml(runVersionsLabel(run))}</div>
                      </div>
                      <div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-2.5 dark:border-white/10 dark:bg-zinc-950/30">
                        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Clusters</div>
                        <div class="mt-1 text-sm font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(String(stats.management))} management, ${escapeHtml(String(stats.downstream))} downstream</div>
                      </div>
                      <div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-2.5 dark:border-white/10 dark:bg-zinc-950/30">
                        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">AWS prefix</div>
                        <div class="mt-1 truncate text-sm font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(run.awsPrefix || 'not recorded')}</div>
                      </div>
                      <div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-2.5 dark:border-white/10 dark:bg-zinc-950/30">
                        <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Owner</div>
                        <div class="mt-1 truncate text-sm font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(run.owner || 'not recorded')}</div>
                      </div>
                    </div>
                    <div class="mt-3 grid gap-2 text-sm text-zinc-600 dark:text-zinc-400 md:grid-cols-2">
                      <div><span class="font-semibold text-zinc-800 dark:text-zinc-200">Hostname:</span> ${escapeHtml(runHostnameLabel(run))}</div>
                      <div><span class="font-semibold text-zinc-800 dark:text-zinc-200">Terraform:</span> <span title="${escapeHtml(run.terraformStatePath || run.terraformBackend || '')}">${escapeHtml(compactPath(run.terraformStatePath || run.terraformBackend || 'not recorded'))}</span></div>
                    </div>
                  </div>
                  <div class="run-action-rail flex shrink-0 flex-wrap gap-2 xl:max-w-[26rem]">
                    ${renderRunActionButton({ action: 'view-clusters', runId: run.runId, label: 'View clusters', variant: stats.total ? 'primary' : 'secondary', disabled: !stats.total, title: stats.total ? 'Open cluster details for this run.' : 'No cluster records discovered for this run yet.' })}
                    ${renderRunActionButton({ action: 'check-readiness', runId: run.runId, label: 'Readiness', variant: 'blue', disabled: readinessDisabled, title: readinessTitle })}
                    ${renderRunActionButton({ action: 'open-run-folder', runId: run.runId, label: 'Open folder', disabled: !runFolderPath(run), title: runFolderPath(run) ? 'Open this run slot folder in Finder.' : 'Run folder path is not recorded yet.' })}
                    ${renderRunActionButton({ action: 'copy-terraform-path', runId: run.runId, label: 'Copy TF path', disabled: !runTerraformPath(run), title: runTerraformPath(run) ? 'Copy the Terraform module/state path for this run.' : 'Terraform path is not recorded yet.' })}
                    ${renderRunActionButton({ action: 'open-setup-logs', runId: run.runId, label: 'Setup logs' })}
                    ${renderRunActionButton({ action: 'open-readiness-logs', runId: run.runId, label: 'Readiness logs' })}
                    ${setupRunningForRun ? renderRunActionButton({ action: 'stop-setup-open-destroy', runId: run.runId, label: 'Stop setup, then destroy', variant: 'danger' }) : ''}
                    ${readinessRunningForRun ? renderRunActionButton({ action: 'stop-readiness-open-destroy', runId: run.runId, label: 'Stop readiness, then destroy', variant: 'danger' }) : ''}
                    ${renderRunActionButton({ action: 'open-destroy', runId: run.runId, label: failedRun ? 'Destroy failed run' : 'Destroy', variant: 'danger', disabled: lifecycleBusy, title: lifecycleBusy ? 'Wait for the active lifecycle operation to finish.' : 'Open the Destroy tab for this slot.' })}
                  </div>
                </div>
              </article>
            `
          }).join('')}
        </div>
      `
    }
  }

  renderDestroySlots(workspace)
}

const renderDestroySlots = workspace => {
  if (!cleanupSlotsEl) {
    return
  }

  const runs = Array.isArray(workspace?.runs) ? workspace.runs : []
  const cleanup = lastState?.cleanup || {}
  const cleanupRunning = Boolean(cleanup.running)
  const setupRunning = Boolean(lastState?.setup?.running)
  const readinessRunning = Boolean(lastState?.readiness?.running)
  const lifecycleBusy = bootStatePending || setupRunning || readinessRunning || cleanupRunning

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
    const pendingDestroy = cleanupStarting && selectedCleanupRunId === run.runId
    const destroying = cleanupRunning && cleanup.runId === run.runId
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
            : setupRunning
              ? 'Setup running'
              : readinessRunning
                ? 'Readiness running'
                : cleanupRunning
                  ? 'Destroy running'
                  : 'Destroy this slot'
    const disabled = lifecycleBusy || cleanupStarting
    const disabledTitle = bootStatePending
      ? 'Startup safety check is still loading run slots and operation state.'
      : setupRunning
      ? 'Wait for setup to finish before destroying a run slot.'
      : readinessRunning
        ? 'Wait for readiness checks to finish before destroying a run slot.'
        : cleanupRunning
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
            <button type="button" data-action="open-run-folder" data-run-id="${escapeHtml(run.runId || '')}" ${runFolderPath(run) ? '' : 'disabled'} class="${runFolderPath(run) ? 'rounded-lg border border-zinc-200 bg-white px-4 py-2.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]' : 'rounded-lg bg-zinc-200 px-4 py-2.5 text-sm font-semibold text-zinc-500 shadow-sm dark:bg-white/[0.06] dark:text-zinc-400'}">Open folder</button>
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

const lifecycleRunning = state => Boolean(state?.setup?.running || state?.readiness?.running || state?.cleanup?.running)

const lifecycleBusyDetail = state => {
  if (state?.setup?.running) {
    return {
      busy: true,
      operation: 'setup',
      message: 'Setup is running. New setup actions are locked until the current AWS lifecycle operation finishes.'
    }
  }
  if (state?.readiness?.running) {
    return {
      busy: true,
      operation: 'readiness',
      message: 'Readiness checks are running. New setup actions are locked until the current lifecycle operation finishes.'
    }
  }
  if (state?.cleanup?.running) {
    return {
      busy: true,
      operation: 'destroy',
      message: 'Destroy is running. New setup actions are locked until the current lifecycle operation finishes.'
    }
  }
  return { busy: false, operation: '', message: '' }
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

const preflightItemClass = status => {
  const classes = {
    ok: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-200',
    warning: 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200',
    blocked: 'border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-200',
    error: 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-200'
  }
  return classes[status] || 'border-zinc-200 bg-white text-zinc-700 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300'
}

const renderPreflight = readiness => {
  preflightState = readiness
  const items = Array.isArray(readiness?.items) ? readiness.items : []
  const errors = items.filter(item => item.status === 'error').length
  const blocked = items.filter(item => item.status === 'blocked').length
  const warnings = items.filter(item => item.status === 'warning').length
  const badgeClass = tone => statusClass(tone).replace('mt-3 ', '')

  if (errors > 0) {
    preflightStatusEl.className = badgeClass('error')
    preflightStatusEl.textContent = `${errors} blocking`
  } else if (blocked > 0) {
    preflightStatusEl.className = badgeClass('blocked')
    preflightStatusEl.textContent = 'Live run active'
  } else if (warnings > 0) {
    preflightStatusEl.className = 'inline-flex items-center justify-center rounded-full bg-amber-100 px-3 py-1.5 text-xs font-semibold text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
    preflightStatusEl.textContent = `${warnings} warning${warnings === 1 ? '' : 's'}`
  } else if (readiness?.ready) {
    preflightStatusEl.className = badgeClass('success')
    preflightStatusEl.textContent = 'Ready'
  } else {
    preflightStatusEl.className = badgeClass('idle')
    preflightStatusEl.textContent = 'Checking...'
  }

  if (!items.length) {
    preflightItemsEl.innerHTML = '<div class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-500 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No preflight results yet.</div>'
    return
  }

  const priority = { error: 0, blocked: 1, warning: 2, ok: 3 }
  const visible = [...items]
    .sort((left, right) => (priority[left.status] ?? 3) - (priority[right.status] ?? 3) || String(left.name).localeCompare(String(right.name)))
    .slice(0, 5)

  preflightItemsEl.innerHTML = visible.map(item => `
    <div class="rounded-lg border px-3 py-2 ${preflightItemClass(item.status)}">
      <div class="flex items-center justify-between gap-3">
        <span class="min-w-0 truncate font-semibold">${escapeHtml(item.name)}</span>
        <span class="shrink-0 text-xs uppercase">${escapeHtml(item.status || 'unknown')}</span>
      </div>
      <div class="mt-1 text-xs leading-5 opacity-90">${escapeHtml(item.detail || '')}</div>
    </div>
  `).join('')

  if (lastState?.setup) {
    renderSetup(lastState.setup)
  }
}

const refreshPreflight = async () => {
  if (preflightInFlight) {
    return
  }

  preflightInFlight = true
  preflightStatusEl.className = statusClass('running').replace('mt-3 ', '')
  preflightStatusEl.innerHTML = '<span class="spinner mr-2"></span>Checking'
  refreshPreflightBtnEl.disabled = true

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
    renderPreflight(await response.json())
  } catch (error) {
    renderPreflight({
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
    refreshPreflightBtnEl.disabled = false
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
          </div>
        </div>
      </div>
    `
    return
  }

  const estimateUnavailable = output.some(line => line.includes('Could not estimate EC2/EBS cost') || line.includes('Terraform outputs unavailable'))
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

const renderCostHistory = costs => {
  if (!costHistorySummaryEl || !costHistoryTableEl) {
    return
  }

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

  if (costs?.error) {
    costHistorySummaryEl.innerHTML = ''
    costHistoryTableEl.innerHTML = `
      <div class="border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-200">
        Cost history unavailable: ${escapeHtml(costs.error)}
      </div>
    `
    return
  }

  const totals = costs?.totals || {}
  const totalCards = [
    ['Lifetime', totals.lifetime],
    ['This month', totals.month],
    ['This week', totals.week],
    ['Today', totals.today]
  ]
  costHistorySummaryEl.innerHTML = totalCards.map(([label, value]) => `
    <div class="rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-3 dark:border-white/10 dark:bg-white/[0.03]">
      <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">${escapeHtml(label)}</div>
      <div class="mt-1 text-2xl font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">${escapeHtml(formatUSD(value))}</div>
      <div class="mt-1 text-xs text-zinc-500 dark:text-zinc-400">Estimated EC2 + EBS only</div>
    </div>
  `).join('')

  const entries = Array.isArray(costs?.entries) ? costs.entries : []
  if (!entries.length) {
    costHistoryTableEl.innerHTML = `
      <div class="bg-zinc-50 p-4 text-sm text-zinc-600 dark:bg-white/[0.03] dark:text-zinc-400">
        No persisted cost estimates yet. Successful destroys will add estimated EC2 and EBS cost rows here.
      </div>
    `
    return
  }

  costHistoryTableEl.innerHTML = `
    <div class="overflow-x-auto">
      <table class="min-w-full divide-y divide-zinc-200 text-left text-sm dark:divide-white/10">
        <thead class="bg-zinc-50 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:bg-white/[0.03] dark:text-zinc-400">
          <tr>
            <th class="px-4 py-3">Run</th>
            <th class="px-4 py-3">Finished</th>
            <th class="px-4 py-3">Owner</th>
            <th class="px-4 py-3">Region</th>
            <th class="px-4 py-3">Runtime</th>
            <th class="px-4 py-3">EC2</th>
            <th class="px-4 py-3">EBS</th>
            <th class="px-4 py-3">Total</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-zinc-200 bg-white dark:divide-white/10 dark:bg-white/[0.02]">
          ${entries.map(entry => {
            const finished = entry.finishedAt ? new Date(entry.finishedAt).toLocaleString() : 'not recorded'
            return `
              <tr>
                <td class="px-4 py-3 font-semibold text-zinc-900 dark:text-zinc-100">
                  ${escapeHtml(entry.runId || 'unknown')}
                  ${entry.awsPrefix ? `<div class="mt-1 text-xs font-medium text-zinc-500 dark:text-zinc-400">${escapeHtml(entry.awsPrefix)}</div>` : ''}
                </td>
                <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">${escapeHtml(finished)}</td>
                <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">${escapeHtml(entry.owner || 'not recorded')}</td>
                <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">${escapeHtml(entry.region || 'unknown')}</td>
                <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">${escapeHtml(Number(entry.totalRuntimeHours || 0).toFixed(2))}h</td>
                <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">${escapeHtml(formatUSD(entry.ec2CostUsd))}</td>
                <td class="px-4 py-3 text-zinc-600 dark:text-zinc-300">${escapeHtml(formatUSD(entry.ebsCostUsd))}</td>
                <td class="px-4 py-3 font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(formatUSD(entry.totalCostUsd))}</td>
              </tr>
            `
          }).join('')}
        </tbody>
      </table>
    </div>
  `
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
  renderWorkspace(lastState.workspace)
  renderLocalArtifactCleanup(lastState.workspace)
  renderCostHistory(lastState.costs)
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
  renderCostHistory(lastState?.costs)

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
    renderCostHistory(lastState?.costs)
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
  renderCostHistory(lastState.costs)
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

  if (activeLogContext?.mode === 'cleanup') {
    const wasNearBottom = logBoxEl.scrollHeight - logBoxEl.scrollTop - logBoxEl.clientHeight < 80
    rawLogText = output.join('\n')
    setLiveLogState(cleanup?.running ? 'cleanupRunning' : cleanup?.error ? 'cleanupError' : cleanup?.finishedAt ? 'cleanupDone' : 'idle')
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
    if (cleanupStarting && state?.cleanup?.running) {
      cleanupStarting = false
    }
    if (bootStatePending) {
      setBootState(false)
    }
    renderPanelSession(state.panel)
    renderHeaderSummary(state)
    renderCommandDeck(state)
    renderPanelTabBadges(state)
    renderWorkspace(state.workspace)
    updateLeaderTracking(state)
    renderClusters(state)
    renderAWSInventory(state.aws)
    renderSetup(state.setup)
    renderReadiness(state.readiness)
    renderCleanup(state.cleanup)
    renderCostHistory(state.costs)
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
  flashKubeconfigPathAction(clusterId, 'open', opened ? 'success' : 'error')
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
  flashKubeconfigPathAction(clusterId, 'copy', copied ? 'success' : 'error')
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

const openSetupLogs = () => {
  stopStream({ internal: true })
  setSetupLogContext()
  const setup = lastState?.setup || {}
  const output = operationOutput(setup)
  rawLogText = output.join('\n')
  setLiveLogState(setup.running ? 'setupRunning' : setup.error ? 'setupError' : setup.finishedAt ? 'setupDone' : 'idle')
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

const openCleanupLogs = () => {
  stopStream({ internal: true })
  setCleanupLogContext()
  const cleanup = lastState?.cleanup || {}
  const output = operationOutput(cleanup)
  rawLogText = output.join('\n')
  setLiveLogState(cleanup.running ? 'cleanupRunning' : cleanup.error ? 'cleanupError' : cleanup.finishedAt ? 'cleanupDone' : 'idle')
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

const abortOperation = async (operation, runId = '') => {
  const confirmed = await requestTypedConfirmation({
    title: operation === 'setup' ? 'Stop setup process?' : `Stop ${operation}?`,
    body: operation === 'setup'
      ? 'This asks the local setup test process to stop and preserves Terraform state plus the run record. It does not destroy AWS resources.'
      : 'This asks the local operation process to stop and preserves Terraform state plus run records. It does not destroy AWS resources.',
    typedValue: 'stop',
    confirmText: 'Request stop'
  })
  if (!confirmed) {
    return
  }

  pendingAbortOperation = operation
  if (lastState?.[operation]) {
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

  if (cleanupStarting || lastState?.cleanup?.running || lastState?.setup?.running || lastState?.readiness?.running) {
    cleanupStatusEl.className = 'inline-flex items-center justify-center rounded-full bg-amber-100 px-3 py-1.5 text-xs font-semibold text-amber-800 dark:bg-amber-500/15 dark:text-amber-200'
    cleanupStatusEl.textContent = lastState?.setup?.running
      ? 'Setup is running'
      : lastState?.readiness?.running
        ? 'Readiness is running'
        : 'Destroy is running'
    return
  }

  const confirmed = await requestTypedConfirmation({
    title: `Destroy run ${targetRunId}?`,
    body: 'This runs Terraform destroy from the selected run state. It is intended to delete AWS resources for that run, then remove the run slot only after destroy succeeds.',
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
    cleanup: {
      ...(lastState?.cleanup || {}),
      running: true,
      runId: targetRunId,
      output: ['[control-panel] Destroy requested...'],
      startedAt: new Date().toISOString()
    }
  }
  dispatchSetupLifecycleState(lastState)
  renderClusters(lastState)
  renderCleanup(lastState.cleanup)
  setCleanupLogContext()
  setLiveLogState('cleanupRunning')
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
    collapsedClusters.set(clusterId, collapsedClusters.get(clusterId) !== true)
    if (lastState) {
      renderClusters(lastState)
    }
    return
  }

  if (action === 'toggle-pods') {
    collapsedPods.set(clusterId, collapsedPods.get(clusterId) !== true)
    if (lastState) {
      renderClusters(lastState)
    }
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
    openLocalPath(runFolderPath(run))
    return
  }
  if (action === 'copy-terraform-path') {
    copyTextToClipboard(runTerraformPath(run), 'Copied Terraform path to clipboard.')
    return
  }
  if (action === 'open-setup-logs') {
    openSetupLogs()
    return
  }
  if (action === 'open-readiness-logs') {
    openReadinessLogs()
    return
  }
  if (action === 'open-cleanup-logs') {
    openCleanupLogs()
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
    abortOperation('setup', runId).then(stopped => {
      if (!stopped) {
        return
      }
      selectedCleanupRunId = runId
      setActiveDestroyTab('slots')
      setActivePanelTab('destroy')
      renderDestroySlots(lastState?.workspace)
    })
    return
  }
  if (action === 'stop-readiness') {
    abortOperation('readiness', runId)
    return
  }
  if (action === 'stop-readiness-open-destroy') {
    abortOperation('readiness', runId).then(stopped => {
      if (!stopped) {
        return
      }
      selectedCleanupRunId = runId
      setActiveDestroyTab('slots')
      setActivePanelTab('destroy')
      renderDestroySlots(lastState?.workspace)
    })
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

panelNoticeCloseEl?.addEventListener('click', hidePanelNotice)
upgradeCommandModalCloseEl?.addEventListener('click', closeUpgradeCommandModal)
upgradeCommandModalEl?.addEventListener('click', event => {
  if (event.target === upgradeCommandModalEl) {
    closeUpgradeCommandModal()
  }
})
document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && upgradeCommandModalEl && !upgradeCommandModalEl.classList.contains('hidden')) {
    closeUpgradeCommandModal()
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
  renderHeaderSummary(lastState)
  renderCommandDeck(lastState)
  renderPanelTabBadges(lastState)
  renderSetup(lastState.setup)
  refreshStatusEl.textContent = 'AWS setup accepted. Waiting for run state to appear...'
  setActivePanelTab('runs')
  refresh()
})

document.getElementById('refreshBtn').addEventListener('click', refresh)
document.getElementById('stopBtn').addEventListener('click', stopPanel)
refreshPreflightBtnEl.addEventListener('click', refreshPreflight)
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
setTheme(currentTheme())
syncFullscreenButton()
setActivePanelTab(activePanelTab)
setActiveDestroyTab(activeDestroyTab)
setBootState(true, 'Checking local config, run slots, Terraform state, lifecycle processes, clusters, and AWS inventory before enabling actions.')
if ('scrollRestoration' in history) {
  history.scrollRestoration = 'manual'
}
window.requestAnimationFrame(() => window.scrollTo({ top: 0, left: 0 }))
refreshPreflight()
refresh()
window.setInterval(refresh, 5000)
