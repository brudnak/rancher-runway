(() => {
const setupRootEl = document.getElementById('interactiveSetupRoot') || document.body
const byId = id => setupRootEl.querySelector(`#${id}`)
const setupQuery = selector => setupRootEl.querySelector(selector)
const setupQueryAll = selector => Array.from(setupRootEl.querySelectorAll(selector))
const setupData = JSON.parse(byId('setup-data').textContent || '{}')
const token = setupData.token || ''
const embeddedSetup = Boolean(setupData.embedded)
const basePath = String(setupData.basePath || '').replace(/\/+$/, '')
const setupEndpoint = path => `${basePath}${path.startsWith('/') ? path : `/${path}`}`

let setupMode = setupData.mode === 'manual' ? 'manual' : 'auto'
let versions = Array.isArray(setupData.versions) ? setupData.versions : ['']
let manualCommands = Array.isArray(setupData.helmCommands) ? setupData.helmCommands : []
let k8sVersions = Array.isArray(setupData.k8sVersions) ? setupData.k8sVersions : []
let installerSHA256s = Array.isArray(setupData.installerSHA256s) ? setupData.installerSHA256s : []
let resolveInstallerSHA = setupData.resolveInstallerSHA !== false
let config = setupData.config || {
  distro: 'auto',
  bootstrapPassword: '',
  preloadImages: false,
  serverCount: 3,
  tfVars: {}
}
const normalizeDeploymentType = value => String(value || '').trim().toLowerCase() === 'hosted-tenant-k3s' ? 'hosted-tenant-k3s' : 'ha-rke2'
let deploymentType = normalizeDeploymentType(setupData.deploymentType || config.deploymentType)
const hostedTenantMinInstances = 2
const hostedTenantMaxInstances = 4
let customHostnameEnabled = Boolean(setupData.customHostnameEnabled)
let customHostname = ''
let submitting = false
let responseSubmitting = false
let pendingCompletionShouldContinue = true
let systemReadiness = null
let setupStatePollTimer = null
let panelBooting = embeddedSetup
let panelLifecycleBusy = false
let panelLifecycleMessage = ''
let manualValidationResults = []
let manualRKE2Recommendations = []
let planCommandCopies = []
let lastResolverFailure = ''

const rowClass = 'grid gap-3 rounded-xl border border-zinc-200 bg-white p-3 shadow-sm dark:border-white/10 dark:bg-white/[0.03] dark:shadow-none sm:grid-cols-[auto_minmax(0,1fr)_auto] sm:items-center'
const inputClass = 'w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 font-medium text-zinc-950 outline-none focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100'
const removeButtonClass = 'rounded-lg border border-zinc-200 bg-zinc-50 px-3.5 py-2.5 text-sm font-medium text-rose-600 hover:bg-zinc-100 disabled:cursor-default disabled:opacity-60 dark:border-white/10 dark:bg-white/[0.04] dark:text-rose-300 dark:hover:bg-white/[0.08]'
const lockIcon = '<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="18" height="11" x="3" y="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>'
const unlockIcon = '<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="18" height="11" x="3" y="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 9.9-1"></path></svg>'

const setupFormEl = byId('setupForm')
const modeInputEl = byId('modeInput')
const deploymentTypeInputEl = byId('deploymentTypeInput')
const haRke2DeploymentBtnEl = byId('haRke2DeploymentBtn')
const hostedTenantDeploymentBtnEl = byId('hostedTenantDeploymentBtn')
const deploymentSummaryEl = byId('deploymentSummary')
const autoModeBtnEl = byId('autoModeBtn')
const manualModeBtnEl = byId('manualModeBtn')
const autoModePanelEl = byId('autoModePanel')
const manualModePanelEl = byId('manualModePanel')
const modeSummaryEl = byId('modeSummary')
const modeValueEl = byId('modeValue')
const rowsEl = byId('rows')
const manualRowsEl = byId('manualRows')
const manualAddBtnEl = byId('manualAddBtn')
const validateHelmBtnEl = byId('validateHelmBtn')
const recommendRKE2BtnEl = byId('recommendRKE2Btn')
const manualChecksumBoxEl = byId('manualChecksumBox')
const resolveInstallerSHAToggleEl = byId('resolveInstallerSHAToggle')
const manualSHAListEl = byId('manualSHAList')
const manualValidationBoxEl = byId('manualValidationBox')
const manualRKE2RecommendationBoxEl = byId('manualRKE2RecommendationBox')
const totalInstancesValueEl = byId('totalInstancesValue')
const editorErrorBoxEl = byId('editorErrorBox')
const editorStatusBoxEl = byId('editorStatusBox')
const themeToggleEl = byId('themeToggle')
const themeSunIconEl = byId('themeSunIcon')
const themeMoonIconEl = byId('themeMoonIcon')
const themeToggleLabelEl = byId('themeToggleLabel')
const addBtnEl = byId('addBtn')
const continueBtnEl = byId('continueBtn')
const editorCancelBtnEl = byId('editorCancelBtn')
const customHostnameBoxEl = byId('customHostnameBox')
const customHostnameToggleEl = byId('customHostnameToggle')
const customHostnameInputEl = byId('customHostnameInput')
const hostedTenantPanelEl = byId('hostedTenantPanel')
const hostedRdsPasswordInputEl = byId('hostedRdsPasswordInput')
const hostedRdsPasswordGenerateBtnEl = byId('hostedRdsPasswordGenerateBtn')
const hostedRdsPasswordToggleEl = byId('hostedRdsPasswordToggle')
const hostedRdsPasswordLockToggleEl = byId('hostedRdsPasswordLockToggle')
const hostedEc2InstanceTypeInputEl = byId('hostedEc2InstanceTypeInput')
const hostedEc2InstanceTypeLockToggleEl = byId('hostedEc2InstanceTypeLockToggle')
const distroSelectEl = byId('distroSelect')
const bootstrapPasswordInputEl = byId('bootstrapPasswordInput')
const bootstrapPasswordToggleEl = byId('bootstrapPasswordToggle')
const preloadImagesToggleEl = byId('preloadImagesToggle')
const preloadImagesTextEl = byId('preloadImagesText')
const serverCountInputEl = byId('serverCountInput')
const serverCountButtonEls = setupQueryAll('button[data-server-count]')
const rke2ServerLayoutFieldsetEl = byId('rke2ServerLayoutFieldset')
const serverTopologyHintEl = byId('serverTopologyHint')
const totalInstancesLabelEl = byId('totalInstancesLabel')
const userFirstNameInputEl = byId('userFirstNameInput')
const userLastNameInputEl = byId('userLastNameInput')
const systemReadinessDetailsEl = byId('systemReadinessDetails')
const systemReadinessBadgeEl = byId('systemReadinessBadge')
const systemReadinessSummaryEl = byId('systemReadinessSummary')
const systemReadinessItemsEl = byId('systemReadinessItems')
const tfVarInputEls = setupQueryAll('input[data-tf-var]')
const lockedFieldInputEls = setupQueryAll('input[data-locked-field]')
const lockToggleEls = setupQueryAll('button[data-lock-toggle]')
const secretToggleEls = setupQueryAll('button[data-secret-toggle]')
const logPanelEl = byId('logPanel')
const reviewLogPanelEl = byId('reviewLogPanel')
const resolvingSummaryEl = byId('resolvingSummary')
const resolvingErrorBoxEl = byId('resolvingErrorBox')
const planCardsEl = byId('planCards')
const planFallbackEl = byId('planFallback')
const resolvedPlanHeadingEl = byId('resolvedPlanHeading')
const reviewResolverLogSummaryEl = byId('reviewResolverLogSummary')
const reviewErrorBoxEl = byId('reviewErrorBox')
const respondActionsEl = byId('respondActions')
const doneAccentEl = byId('doneAccent')
const doneIconEl = byId('doneIcon')
const doneTitleEl = byId('doneTitle')
const doneBodyEl = byId('doneBody')
const doneDetailEl = byId('doneDetail')
const confirmModalEl = byId('confirmModal')
const confirmModalTitleEl = byId('confirmModalTitle')
const confirmModalBodyEl = byId('confirmModalBody')
const confirmModalConfirmEl = byId('confirmModalConfirm')
const confirmModalCancelEl = byId('confirmModalCancel')
const helmValidationModalEl = byId('helmValidationModal')
const helmValidationModalBadgeEl = byId('helmValidationModalBadge')
const helmValidationModalTitleEl = byId('helmValidationModalTitle')
const helmValidationModalSummaryEl = byId('helmValidationModalSummary')
const helmValidationModalBodyEl = byId('helmValidationModalBody')
const helmValidationModalCloseEl = byId('helmValidationModalClose')

if (confirmModalEl && confirmModalEl.parentElement !== document.body) {
  document.body.appendChild(confirmModalEl)
}

const setPhase = phase => {
  if (phase === 'done') {
    renderCompletion(pendingCompletionShouldContinue)
  }

  setupRootEl.dataset.phase = phase
}

const currentTheme = () => document.documentElement.classList.contains('dark') ? 'dark' : 'light'

const persistTheme = theme => {
  localStorage.setItem('rancherSetupTheme', theme)
  document.cookie = `rancherSetupTheme=${theme}; Path=/; Max-Age=31536000; SameSite=Lax`
}

const setTheme = (theme, persist = true) => {
  document.documentElement.classList.toggle('dark', theme === 'dark')

  if (persist) {
    persistTheme(theme)
  }

  if (themeToggleLabelEl && themeSunIconEl && themeMoonIconEl) {
    themeSunIconEl.classList.toggle('hidden', theme !== 'dark')
    themeMoonIconEl.classList.toggle('hidden', theme !== 'light')
    themeToggleLabelEl.textContent = theme === 'dark' ? 'Light' : 'Dark'
  }

}

const escapeHtml = value => String(value)
  .replaceAll('&', '&amp;')
  .replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;')

const copyTextToClipboard = async text => {
  if (!navigator.clipboard) {
    throw new Error('Clipboard access is unavailable in this browser.')
  }
  await navigator.clipboard.writeText(text)
}

const parseResolvedPlanText = planText => {
  const lines = String(planText || '').split(/\r?\n/)
  const cards = []
  let current = null
  let activeCommand = null

  const finishCurrent = () => {
    if (!current) {
      return
    }

    current.commands = current.commands
      .map(command => ({
        ...command,
        text: command.lines.join('\n').trim()
      }))
      .filter(command => command.text)
    delete current.lines
    cards.push(current)
  }

  lines.forEach(rawLine => {
    const line = String(rawLine || '').replace(/\s+$/, '')
    const trimmed = line.trim()
    const haMatch = trimmed.match(/^(HA|Tenant)\s+(\d+)$/)

    if (haMatch) {
      finishCurrent()
      current = {
        title: `${haMatch[1]} ${haMatch[2]}`,
        details: [],
        commands: []
      }
      activeCommand = null
      return
    }

    if (trimmed === 'Host') {
      finishCurrent()
      current = {
        title: 'Host',
        details: [],
        commands: []
      }
      activeCommand = null
      return
    }

    if (!current || trimmed === 'Continue with this Rancher plan?') {
      return
    }

    const commandMatch = trimmed.match(/^Helm command\s+(\d+):$/)
    if (commandMatch) {
      activeCommand = {
        label: `Helm command ${commandMatch[1]}`,
        lines: []
      }
      current.commands.push(activeCommand)
      return
    }

    if (activeCommand) {
      activeCommand.lines.push(line)
      return
    }

    const detailMatch = line.match(/^([^:]+):\s*(.*)$/)
    if (detailMatch) {
      current.details.push({
        label: detailMatch[1].trim(),
        value: detailMatch[2].trim()
      })
    }
  })

  finishCurrent()
  return cards
}

const renderPlanCards = planText => {
  if (!planCardsEl || !planFallbackEl) {
    return
  }

  const text = String(planText || '').trim()
  const cards = parseResolvedPlanText(text)
  const hosted = text.includes('Resolved K3s/K8s:') || cards.some(card => card.title === 'Host' || card.title.startsWith('Tenant '))

  if (resolvedPlanHeadingEl) {
    resolvedPlanHeadingEl.textContent = hosted ? 'Resolved hosted tenant install plan' : 'Resolved HA install plan'
  }
  if (reviewResolverLogSummaryEl) {
    reviewResolverLogSummaryEl.textContent = hosted
      ? 'Version, chart, K3s, and installer resolution output.'
      : 'Version, chart, RKE2, and installer resolution output.'
  }

  if (!text) {
    planCardsEl.innerHTML = ''
    planFallbackEl.classList.add('hidden')
    planFallbackEl.textContent = ''
    return
  }

  if (!cards.length) {
    planCardsEl.innerHTML = ''
    planFallbackEl.classList.remove('hidden')
    planFallbackEl.textContent = text
    return
  }

  planFallbackEl.classList.add('hidden')
  planFallbackEl.textContent = ''
  planCommandCopies = []
  const emptyLabel = hosted ? 'hosted tenant instance' : 'HA'
  const renderCodeLines = commandText => String(commandText || '').split('\n').map((line, index) => `
    <div class="setup-code-line">
      <span class="setup-code-line-number">${index + 1}</span>
      <code class="setup-code-line-code">${escapeHtml(line || ' ')}</code>
    </div>
  `).join('')

  planCardsEl.innerHTML = cards.map(card => {
    const details = card.details.length
      ? card.details.map(detail => `
        <div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3.5 py-3 dark:border-white/10 dark:bg-zinc-950/30">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-600 dark:text-zinc-300">${escapeHtml(detail.label)}</div>
          <div class="mt-1 break-words text-sm font-semibold text-zinc-950 dark:text-zinc-100">${escapeHtml(detail.value || '-')}</div>
        </div>
      `).join('')
      : `<div class="text-sm text-zinc-500 dark:text-zinc-400">No resolved metadata was emitted for this ${emptyLabel}.</div>`

    const commands = card.commands.length
      ? card.commands.map(command => {
        const copyIndex = planCommandCopies.push(command.text) - 1
        return `
        <div class="setup-code-editor">
          <div class="setup-code-editor-header">
            <div class="setup-code-editor-title">${escapeHtml(command.label)}</div>
            <div class="setup-code-editor-actions">
              <div class="setup-code-editor-lang">shell</div>
              <button type="button" data-copy-plan-command="${copyIndex}" class="rounded-md border border-slate-300 bg-white px-2.5 py-1 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800">Copy Helm install command</button>
            </div>
          </div>
          <div class="setup-code-lines" role="region" aria-label="${escapeHtml(command.label)} shell command">
            ${renderCodeLines(command.text)}
          </div>
        </div>
      `
      }).join('')
      : '<div class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">No Helm command was emitted for this HA.</div>'

    return `
      <details class="setup-ha-card" open>
        <summary class="setup-ha-summary">
          <div>
            <div class="flex flex-wrap items-center gap-3">
              <h3 class="text-lg font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(card.title)}</h3>
              <span class="rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-semibold text-zinc-700 dark:bg-white/[0.06] dark:text-zinc-200">Ready for approval</span>
            </div>
            <p class="mt-1 text-sm text-zinc-500 dark:text-zinc-400">${hosted ? 'Resolved hosted tenant install details for review before AWS setup starts.' : 'Resolved install details for review before AWS setup starts.'}</p>
          </div>
          <span class="inline-flex shrink-0 items-center gap-2 rounded-full border border-zinc-200 bg-zinc-50 px-3 py-1.5 text-xs font-semibold text-zinc-700 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-200">
            Details
            <svg xmlns="http://www.w3.org/2000/svg" class="setup-disclosure-icon h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="m6 9 6 6 6-6"></path>
            </svg>
          </span>
        </summary>
        <div class="setup-ha-card-body">
          <div class="grid gap-3 lg:grid-cols-3">${details}</div>
          <div class="mt-4 grid gap-3">${commands}</div>
        </div>
      </details>
    `
  }).join('')
}

const sanitizeDisplayValue = value => {
  let next = String(value || '').trim()

  while (next.length >= 2) {
    const first = next[0]
    const last = next[next.length - 1]

    if ((first === '"' && last === '"') || (first === '\'' && last === '\'')) {
      next = next.slice(1, -1).trim()
      continue
    }

    break
  }

  return next
}

customHostname = sanitizeDisplayValue(setupData.customHostname || '')

const isHostedTenantDeployment = () => deploymentType === 'hosted-tenant-k3s'

const autoRowLabel = index => isHostedTenantDeployment()
  ? index === 0 ? 'Host' : `Tenant ${index}`
  : `HA ${index + 1}`

const activeInstanceLabel = () => isHostedTenantDeployment() ? 'Rancher instances' : 'HAs'

const minimumAutoRows = () => isHostedTenantDeployment() ? hostedTenantMinInstances : 1

const maximumAutoRows = () => isHostedTenantDeployment() ? hostedTenantMaxInstances : Number.POSITIVE_INFINITY

const ensureDeploymentCompatibleRows = () => {
  const minimumRows = minimumAutoRows()
  while (versions.length < minimumRows) {
    versions.push('')
  }
  if (versions.length > maximumAutoRows()) {
    versions = versions.slice(0, maximumAutoRows())
  }
  if (isHostedTenantDeployment()) {
    setupMode = 'auto'
    customHostnameEnabled = false
  }
}

const showValidationError = (message, target) => {
  editorErrorBoxEl.textContent = message
  editorStatusBoxEl.textContent = ''
  editorErrorBoxEl.scrollIntoView({ behavior: 'smooth', block: 'center' })

  if (target) {
    target.focus({ preventScroll: true })
  }
}

const clearValidationError = () => {
  editorErrorBoxEl.textContent = ''
}

const showConfirmModal = ({ title, body, confirmText = 'Continue', cancelText = 'Go back', showCancel = true }) => new Promise(resolve => {
  if (!confirmModalEl || !confirmModalTitleEl || !confirmModalBodyEl || !confirmModalConfirmEl || !confirmModalCancelEl) {
    resolve(true)
    return
  }

  let settled = false
  const previousBodyOverflow = document.body.style.overflow

  const settle = result => {
    if (settled) {
      return
    }

    settled = true
    confirmModalEl.classList.add('hidden')
    confirmModalEl.classList.remove('flex')
    confirmModalConfirmEl.removeEventListener('click', confirm)
    confirmModalCancelEl.removeEventListener('click', cancel)
    confirmModalEl.removeEventListener('click', backdropCancel)
    document.removeEventListener('keydown', escapeCancel)
    document.body.style.overflow = previousBodyOverflow
    resolve(result)
  }

  const confirm = () => settle(true)
  const cancel = () => settle(false)
  const backdropCancel = event => {
    if (event.target === confirmModalEl) {
      cancel()
    }
  }
  const escapeCancel = event => {
    if (event.key === 'Escape') {
      cancel()
    }
  }

  confirmModalTitleEl.textContent = title
  confirmModalBodyEl.textContent = body
  confirmModalConfirmEl.textContent = confirmText
  confirmModalCancelEl.textContent = cancelText
  confirmModalCancelEl.classList.toggle('hidden', !showCancel)
  confirmModalEl.classList.remove('hidden')
  confirmModalEl.classList.add('flex')
  document.body.style.overflow = 'hidden'
  confirmModalConfirmEl.addEventListener('click', confirm)
  confirmModalCancelEl.addEventListener('click', cancel)
  confirmModalEl.addEventListener('click', backdropCancel)
  document.addEventListener('keydown', escapeCancel)
  confirmModalConfirmEl.focus()
})

const showNoticeModal = ({ title, body, confirmText = 'Got it' }) => showConfirmModal({
  title,
  body,
  confirmText,
  showCancel: false
})

const showResolverFailure = message => {
  const normalized = String(message || '').trim()
  if (!normalized) {
    return
  }
  editorErrorBoxEl.textContent = normalized
  editorStatusBoxEl.textContent = ''
  setSubmittingState(false)
  if (lastResolverFailure === normalized) {
    return
  }
  lastResolverFailure = normalized
  showNoticeModal({
    title: 'Rancher plan resolution failed',
    body: normalized,
    confirmText: 'Review setup'
  })
}

const readinessStyles = status => {
  const styles = {
    ok: {
      icon: '<path d="M20 6 9 17l-5-5"></path>',
      iconClass: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
      badgeClass: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    },
    warning: {
      icon: '<path d="M12 9v4"></path><path d="M12 17h.01"></path><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"></path>',
      iconClass: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
      badgeClass: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
    },
    error: {
      icon: '<path d="M18 6 6 18"></path><path d="m6 6 12 12"></path>',
      iconClass: 'bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300',
      badgeClass: 'bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
    },
    checking: {
      icon: '',
      iconClass: 'bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300',
      badgeClass: 'bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300'
    }
  }

  return styles[status] || styles.checking
}

const renderSystemReadiness = readiness => {
  systemReadiness = readiness
  const items = Array.isArray(readiness?.items) ? readiness.items : []
  const hasError = items.some(item => item.status === 'error')
  const hasWarning = items.some(item => item.status === 'warning')
  const status = hasError ? 'error' : hasWarning ? 'warning' : readiness?.ready ? 'ok' : 'checking'
  const style = readinessStyles(status)
  const label = status === 'ok' ? 'Ready' : status === 'warning' ? 'Ready with warnings' : status === 'error' ? 'Action needed' : 'Checking'

  systemReadinessBadgeEl.className = `inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold ${style.badgeClass}`
  systemReadinessBadgeEl.textContent = status === 'ok' ? 'Ready' : label
  systemReadinessSummaryEl.textContent = readiness?.summary || 'Checking local tools, config, and required environment.'

  if (!items.length) {
    systemReadinessItemsEl.innerHTML = '<div class="rounded-lg border border-zinc-200 bg-white px-3.5 py-3 text-sm text-zinc-500 dark:border-white/10 dark:bg-zinc-950/30 dark:text-zinc-400">Checking system readiness...</div>'
    return
  }

  systemReadinessItemsEl.innerHTML = items.map(item => {
    const itemStyle = readinessStyles(item.status)
    const version = item.version ? `<span class="text-zinc-500 dark:text-zinc-400">${escapeHtml(item.version)}</span>` : ''
    const baseline = item.recommended ? `<div class="mt-1 text-xs text-zinc-500 dark:text-zinc-500">Recommended ${escapeHtml(item.recommended)}${item.minimum ? ` • minimum ${escapeHtml(item.minimum)}` : ''}</div>` : ''

    return `
      <div class="grid gap-3 rounded-lg border border-zinc-200 bg-white px-3.5 py-3 dark:border-white/10 dark:bg-zinc-950/30 sm:grid-cols-[auto_minmax(0,1fr)]">
        <div class="flex h-8 w-8 items-center justify-center rounded-full ${itemStyle.iconClass}">
          ${item.status === 'checking' ? '<span class="spinner !h-4 !w-4 !border-2"></span>' : `<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${itemStyle.icon}</svg>`}
        </div>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <div class="font-semibold text-zinc-950 dark:text-zinc-100">${escapeHtml(item.name || 'Check')}</div>
            ${version}
          </div>
          <div class="mt-1 text-sm leading-6 text-zinc-600 dark:text-zinc-400">${escapeHtml(item.detail || '')}</div>
          ${baseline}
        </div>
      </div>
    `
  }).join('')
}

const loadSystemReadiness = async () => {
  renderSystemReadiness({
    ready: false,
    summary: 'Checking local tools, config, and required environment.',
    items: []
  })

  try {
    const response = await fetch(setupEndpoint(`/api/readiness?token=${encodeURIComponent(token)}`), {
      cache: 'no-store',
      headers: { 'Accept': 'application/json' }
    })
    if (!response.ok) {
      throw new Error(await response.text() || 'System readiness check failed.')
    }
    renderSystemReadiness(await response.json())
  } catch (error) {
    renderSystemReadiness({
      ready: false,
      summary: 'System readiness check failed',
      items: [{
        name: 'System readiness',
        status: 'error',
        detail: error instanceof Error ? error.message : 'System readiness check failed.'
      }]
    })
  }
}

const renderEditableConfig = () => {
  distroSelectEl.value = config.distro || 'auto'
  bootstrapPasswordInputEl.value = config.bootstrapPassword || ''
  preloadImagesToggleEl.checked = Boolean(config.preloadImages)
  if (serverCountInputEl) {
    serverCountInputEl.value = String(normalizeServerCount(config.serverCount))
  }
  userFirstNameInputEl.value = config.userFirstName || ''
  userLastNameInputEl.value = config.userLastName || ''
  if (hostedRdsPasswordInputEl) {
    hostedRdsPasswordInputEl.value = config.hostedRDSPassword || ''
    hostedRdsPasswordInputEl.type = 'password'
    if (hostedRdsPasswordToggleEl) {
      hostedRdsPasswordToggleEl.textContent = 'Show'
    }
  }
  if (hostedEc2InstanceTypeInputEl) {
    hostedEc2InstanceTypeInputEl.value = config.hostedEC2InstanceType || 'm5.large'
  }

  tfVarInputEls.forEach(input => {
    const key = input.getAttribute('data-tf-var')
    input.value = (config.tfVars && config.tfVars[key]) || ''
  })

  lockAllAdvancedAWSFields()
  setHostedRDSPasswordLocked(true)
  setHostedEC2InstanceTypeLocked(true)
  renderHostedRDSPasswordGenerateState()
  renderServerTopology()
}

const renderDeploymentType = () => {
  ensureDeploymentCompatibleRows()
  const hosted = isHostedTenantDeployment()
  if (deploymentTypeInputEl) {
    deploymentTypeInputEl.value = deploymentType
  }
  haRke2DeploymentBtnEl?.setAttribute('aria-pressed', hosted ? 'false' : 'true')
  hostedTenantDeploymentBtnEl?.setAttribute('aria-pressed', hosted ? 'true' : 'false')
  hostedTenantPanelEl?.classList.toggle('hidden', !hosted)
  customHostnameBoxEl?.classList.toggle('hidden', hosted)
  rke2ServerLayoutFieldsetEl?.classList.toggle('hidden', hosted)
  if (manualModeBtnEl) {
    manualModeBtnEl.disabled = submitting || hosted
    manualModeBtnEl.title = hosted ? 'Hosted tenant K3s setup currently resolves through auto mode.' : ''
    manualModeBtnEl.classList.toggle('cursor-not-allowed', hosted)
    manualModeBtnEl.classList.toggle('opacity-50', hosted)
  }
  if (addBtnEl) {
    addBtnEl.textContent = hosted ? 'Add tenant' : 'Add HA'
  }
  if (manualAddBtnEl) {
    manualAddBtnEl.textContent = hosted ? 'Add tenant' : 'Add HA'
  }
  if (totalInstancesLabelEl) {
    totalInstancesLabelEl.textContent = hosted ? 'Total Rancher instances for this run:' : 'Total HAs for this run:'
  }
  if (deploymentSummaryEl) {
    deploymentSummaryEl.textContent = hosted
      ? 'Hosted tenant K3s creates one host Rancher first, then one to three tenant Ranchers backed by RDS/Aurora MySQL.'
      : 'HA RKE2 creates standalone Rancher management clusters using the RKE2 server layout below.'
  }
  if (preloadImagesTextEl) {
    preloadImagesTextEl.textContent = hosted ? 'Preload K3s images' : 'Preload RKE2 images'
  }
  renderHostedRDSPasswordGenerateState()
}

const renderRows = () => {
  ensureDeploymentCompatibleRows()
  if (customHostnameEnabled && versions.length !== 1) {
    versions = [versions[0] || '']
  }

  rowsEl.innerHTML = versions.map((version, index) => {
    const removeDisabled = customHostnameEnabled || versions.length <= minimumAutoRows() ? ' disabled' : ''
    const label = autoRowLabel(index)

    return [
      `<div class="${rowClass}">`,
      `<div class="inline-flex w-fit rounded-md bg-zinc-100 px-2.5 py-1 text-sm font-medium text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">${escapeHtml(label)}</div>`,
      `<div><input class="${inputClass}" type="text" name="versions" value="${escapeHtml(version)}" data-index="${index}" placeholder="2.14.1-alpha3" /></div>`,
      `<div><button class="${removeButtonClass}" type="button" data-remove-index="${index}"${removeDisabled}>Remove</button></div>`,
      '</div>'
    ].join('')
  }).join('')

  totalInstancesValueEl.textContent = String(versions.length)
  const addDisabled = customHostnameEnabled || versions.length >= maximumAutoRows()
  addBtnEl.disabled = submitting || addDisabled
  addBtnEl.setAttribute('aria-disabled', addDisabled ? 'true' : 'false')
  addBtnEl.classList.toggle('cursor-not-allowed', addDisabled)
  addBtnEl.classList.toggle('opacity-50', addDisabled)
  addBtnEl.title = isHostedTenantDeployment() && versions.length >= hostedTenantMaxInstances
    ? 'Hosted tenant K3s supports up to 4 total Rancher instances: 1 host plus 3 tenants.'
    : ''

  rowsEl.querySelectorAll('input[data-index]').forEach(input => {
    input.addEventListener('input', event => {
      versions[Number(event.target.getAttribute('data-index'))] = event.target.value
      clearValidationError()
    })
  })

  rowsEl.querySelectorAll('button[data-remove-index]').forEach(button => {
    button.addEventListener('click', () => {
      if (versions.length <= minimumAutoRows() || submitting || customHostnameEnabled) {
        return
      }

      versions.splice(Number(button.getAttribute('data-remove-index')), 1)
      renderRows()
    })
  })
}

const normalizeServerCount = value => {
  const count = Number(value)
  return [1, 3, 5].includes(count) ? count : 3
}

const currentServerCount = () => normalizeServerCount(serverCountInputEl?.value || config.serverCount)

const renderServerTopology = () => {
  const selected = currentServerCount()
  if (serverCountInputEl) {
    serverCountInputEl.value = String(selected)
  }
  serverCountButtonEls.forEach(button => {
    const count = normalizeServerCount(button.dataset.serverCount)
    const active = count === selected
    button.setAttribute('aria-checked', active ? 'true' : 'false')
    button.classList.toggle('border-emerald-300', active)
    button.classList.toggle('bg-emerald-50', active)
    button.classList.toggle('dark:border-emerald-500/30', active)
    button.classList.toggle('dark:bg-emerald-500/10', active)
    button.classList.toggle('border-zinc-200', !active)
    button.classList.toggle('bg-white', !active)
    button.classList.toggle('dark:border-white/10', !active)
    button.classList.toggle('dark:bg-black/20', !active)
    button.classList.toggle('ring-1', active)
    button.classList.toggle('ring-emerald-300', active)
  })
  if (serverTopologyHintEl) {
    serverTopologyHintEl.textContent = selected === 1
      ? 'Single-server RKE2 is valid for a lightweight Rancher install, but it is not highly available.'
      : selected === 5
        ? 'Five RKE2 servers are useful for larger HA-style testing, with higher EC2 cost and longer setup.'
        : 'Three RKE2 servers are the recommended default for normal HA testing.'
  }
}

const renderMode = () => {
  if (isHostedTenantDeployment()) {
    setupMode = 'auto'
  }
  setupMode = setupMode === 'manual' ? 'manual' : 'auto'
  if (modeInputEl) {
    modeInputEl.value = setupMode
  }
  autoModeBtnEl?.setAttribute('aria-pressed', setupMode === 'auto' ? 'true' : 'false')
  manualModeBtnEl?.setAttribute('aria-pressed', setupMode === 'manual' ? 'true' : 'false')
  autoModePanelEl?.classList.toggle('hidden', setupMode !== 'auto')
  manualModePanelEl?.classList.toggle('hidden', setupMode !== 'manual')
  if (modeValueEl) {
    modeValueEl.textContent = setupMode
  }
  if (modeSummaryEl) {
    modeSummaryEl.textContent = isHostedTenantDeployment()
      ? 'Hosted tenant K3s uses auto mode so the host and tenant Rancher plans can resolve before the AWS run starts.'
      : setupMode === 'manual'
      ? 'Manual mode saves one editable Helm command and one RKE2 version per HA, then validates the Helm render before AWS starts.'
      : 'Auto mode resolves Rancher chart, image, RKE2 version, and installer SHA256 from the requested Rancher versions.'
  }
  if (setupMode === 'manual') {
    ensureManualRows()
    renderManualRows()
  } else {
    renderRows()
  }
  if (manualAddBtnEl) {
    manualAddBtnEl.disabled = submitting || customHostnameEnabled || isHostedTenantDeployment()
    manualAddBtnEl.classList.toggle('cursor-not-allowed', customHostnameEnabled || isHostedTenantDeployment())
    manualAddBtnEl.classList.toggle('opacity-50', customHostnameEnabled || isHostedTenantDeployment())
  }
  totalInstancesValueEl.textContent = String(activeHACount())
  setSubmittingState(submitting)
}

const manualValidationResultsHTML = ({ modal = false } = {}) => {
  if (!manualValidationResults.length) {
    return '<div class="rounded-lg border border-zinc-200 bg-zinc-50 px-3.5 py-3 text-sm text-zinc-500 dark:border-white/10 dark:bg-white/[0.03] dark:text-zinc-400">Helm validation has not run for these commands yet.</div>'
  }

  return manualValidationResults.map(result => {
    const ok = Boolean(result.ok)
    const tone = ok
      ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200'
      : 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200'
    const detailClass = modal
      ? 'mt-3 max-h-[56vh] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-white/55 p-3 font-mono text-xs leading-5 opacity-90 dark:bg-black/20'
      : 'mt-2 max-h-36 overflow-auto whitespace-pre-wrap break-words font-mono text-xs leading-5 opacity-90'
    return `
      <div class="rounded-lg border px-3.5 py-3 text-sm ${tone}">
        <div class="font-semibold">HA ${Number(result.index || 0) + 1}: ${escapeHtml(result.summary || (ok ? 'OK' : 'Validation failed'))}</div>
        ${result.detail ? `<pre class="${detailClass}">${escapeHtml(result.detail)}</pre>` : ''}
      </div>
    `
  }).join('')
}

const renderManualValidation = () => {
  if (!manualValidationBoxEl) {
    return
  }
  manualValidationBoxEl.innerHTML = manualValidationResultsHTML()
}

const renderManualRKE2Recommendations = () => {
  if (!manualRKE2RecommendationBoxEl) {
    return
  }
  if (!manualRKE2Recommendations.length) {
    manualRKE2RecommendationBoxEl.innerHTML = ''
    return
  }
  const okResults = manualRKE2Recommendations.filter(result => result.ok && result.recommendedRKE2Version)
  const applyButton = okResults.length
    ? '<button type="button" data-apply-rke2-recommendations class="w-fit rounded-lg bg-emerald-500 px-3.5 py-2 text-sm font-semibold text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400">Apply recommendations</button>'
    : ''
  manualRKE2RecommendationBoxEl.innerHTML = `
    <div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 dark:border-white/10 dark:bg-white/[0.03]">
      <div class="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div>
          <h3 class="text-sm font-semibold text-zinc-950 dark:text-zinc-100">RKE2 recommendation</h3>
          <p class="mt-1 text-sm leading-6 text-zinc-600 dark:text-zinc-400">Uses the Rancher chart version in each Helm command to select the latest RKE2 patch from the supported Kubernetes line.</p>
        </div>
        ${applyButton}
      </div>
      <div class="mt-3 grid gap-2">
        ${manualRKE2Recommendations.map(result => {
          const ok = Boolean(result.ok)
          const tone = ok
            ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200'
            : 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200'
          const pieces = []
          if (result.rancherVersion) pieces.push(`Rancher ${result.rancherVersion}`)
          if (result.compatibilityBaseline && result.compatibilityBaseline !== result.rancherVersion) pieces.push(`baseline ${result.compatibilityBaseline}`)
          if (result.kubernetesVersion) pieces.push(`Kubernetes ${result.kubernetesVersion}`)
          return `
            <div class="rounded-lg border px-3.5 py-3 text-sm ${tone}">
              <div class="font-semibold">HA ${Number(result.index || 0) + 1}: ${escapeHtml(ok ? result.recommendedRKE2Version : result.summary || 'Recommendation failed')}</div>
              ${pieces.length ? `<div class="mt-1 opacity-85">${escapeHtml(pieces.join(' • '))}</div>` : ''}
              ${result.detail ? `<div class="mt-1 opacity-85">${escapeHtml(result.detail)}</div>` : ''}
            </div>
          `
        }).join('')}
      </div>
    </div>
  `
}

const closeHelmValidationModal = () => {
  if (!helmValidationModalEl) {
    return
  }
  helmValidationModalEl.classList.add('hidden')
  helmValidationModalEl.classList.remove('flex')
  document.body.classList.remove('overflow-hidden')
}

const openHelmValidationModal = () => {
  if (!helmValidationModalEl || !helmValidationModalBodyEl) {
    return
  }
  const failedCount = manualValidationResults.filter(result => !result.ok).length
  const totalCount = manualValidationResults.length
  const ok = totalCount > 0 && failedCount === 0
  helmValidationModalTitleEl.textContent = ok ? 'Helm validation passed' : 'Helm validation needs attention'
  helmValidationModalSummaryEl.textContent = ok
    ? `${totalCount} Helm command${totalCount === 1 ? '' : 's'} rendered successfully.`
    : `${failedCount} of ${totalCount} Helm command${totalCount === 1 ? '' : 's'} failed validation.`
  helmValidationModalBadgeEl.className = ok
    ? 'mb-2 inline-flex rounded-full bg-emerald-100 px-2.5 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    : 'mb-2 inline-flex rounded-full bg-rose-100 px-2.5 py-1 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
  helmValidationModalBadgeEl.textContent = ok ? 'Rendered successfully' : 'Validation failed'
  helmValidationModalBodyEl.innerHTML = manualValidationResultsHTML({ modal: true })
  helmValidationModalEl.classList.remove('hidden')
  helmValidationModalEl.classList.add('flex')
  helmValidationModalEl.scrollTop = 0
  document.body.classList.add('overflow-hidden')
  helmValidationModalCloseEl?.focus()
}

const renderManualRows = () => {
  ensureManualRows()
  manualRowsEl.innerHTML = manualCommands.map((command, index) => {
    const removeDisabled = customHostnameEnabled || manualCommands.length <= 1 ? ' disabled' : ''
    return `
      <div class="rounded-xl border border-zinc-200 bg-white p-3 shadow-sm dark:border-white/10 dark:bg-white/[0.03] dark:shadow-none">
        <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
          <div class="inline-flex w-fit rounded-md bg-zinc-100 px-2.5 py-1 text-sm font-medium text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">HA ${index + 1}</div>
          <div class="flex flex-wrap gap-2">
            <button class="rounded-lg border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm font-medium text-zinc-700 hover:bg-zinc-100 disabled:cursor-default disabled:opacity-60 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-200 dark:hover:bg-white/[0.08]" type="button" data-seed-index="${index}">Rebuild base</button>
            <button class="${removeButtonClass}" type="button" data-manual-remove-index="${index}"${removeDisabled}>Remove</button>
          </div>
        </div>
        <div class="grid gap-3 lg:grid-cols-[minmax(0,1fr)_16rem]">
          <label class="grid gap-2 text-sm font-medium text-zinc-600 dark:text-zinc-400">
            Helm command
            <textarea class="manual-code-area w-full rounded-lg border border-slate-300 bg-slate-50 px-3.5 py-3 font-mono text-xs leading-6 text-slate-950 outline-none focus:border-emerald-400 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100" name="helmCommands" data-manual-command-index="${index}" spellcheck="false">${escapeHtml(command)}</textarea>
          </label>
          <div class="grid content-start gap-3">
            <label class="grid gap-2 text-sm font-medium text-zinc-600 dark:text-zinc-400">
              RKE2 version
              <input class="${inputClass}" type="text" name="k8sVersions" value="${escapeHtml(k8sVersions[index] || '')}" data-k8s-index="${index}" placeholder="v1.34.6+rke2r1" />
            </label>
            <label class="manual-sha-field grid gap-2 text-sm font-medium text-zinc-600 dark:text-zinc-400">
              Installer SHA256
              <input class="${inputClass}" type="text" name="installerSHA256s" value="${escapeHtml(installerSHA256s[index] || '')}" data-sha-index="${index}" placeholder="64-character hex checksum" />
            </label>
          </div>
        </div>
      </div>
    `
  }).join('')

  resolveInstallerSHAToggleEl.checked = resolveInstallerSHA
  manualChecksumBoxEl.dataset.autoSha = resolveInstallerSHA ? 'true' : 'false'
  manualSHAListEl.innerHTML = ''
  manualRowsEl.querySelectorAll('.manual-sha-field').forEach(field => {
    field.classList.toggle('hidden', resolveInstallerSHA)
  })
  manualRowsEl.querySelectorAll('textarea[data-manual-command-index]').forEach(textarea => {
    textarea.addEventListener('input', event => {
      manualCommands[Number(event.target.getAttribute('data-manual-command-index'))] = event.target.value
      manualValidationResults = []
      manualRKE2Recommendations = []
      renderManualValidation()
      renderManualRKE2Recommendations()
      clearValidationError()
    })
  })
  manualRowsEl.querySelectorAll('input[data-k8s-index]').forEach(input => {
    input.addEventListener('input', event => {
      k8sVersions[Number(event.target.getAttribute('data-k8s-index'))] = event.target.value
      manualValidationResults = []
      renderManualValidation()
      clearValidationError()
    })
  })
  manualRowsEl.querySelectorAll('input[data-sha-index]').forEach(input => {
    input.addEventListener('input', event => {
      const index = Number(event.target.getAttribute('data-sha-index'))
      installerSHA256s[index] = event.target.value
      clearValidationError()
    })
  })
  manualRowsEl.querySelectorAll('button[data-seed-index]').forEach(button => {
    button.addEventListener('click', () => {
      if (submitting) {
        return
      }
      const index = Number(button.getAttribute('data-seed-index'))
      manualCommands[index] = buildSeedHelmCommand(index)
      manualValidationResults = []
      manualRKE2Recommendations = []
      renderManualRows()
      renderManualValidation()
      renderManualRKE2Recommendations()
    })
  })
  manualRowsEl.querySelectorAll('button[data-manual-remove-index]').forEach(button => {
    button.addEventListener('click', () => {
      if (manualCommands.length <= 1 || submitting || customHostnameEnabled) {
        return
      }
      const index = Number(button.getAttribute('data-manual-remove-index'))
      manualCommands.splice(index, 1)
      k8sVersions.splice(index, 1)
      installerSHA256s.splice(index, 1)
      manualValidationResults = []
      manualRKE2Recommendations = []
      renderManualRows()
      renderManualValidation()
      renderManualRKE2Recommendations()
      totalInstancesValueEl.textContent = String(activeHACount())
    })
  })
  renderManualValidation()
  renderManualRKE2Recommendations()
}

const renderCustomHostname = () => {
  if (isHostedTenantDeployment()) {
    customHostnameEnabled = false
  }
  customHostnameBoxEl.dataset.enabled = customHostnameEnabled ? 'true' : 'false'
  customHostnameToggleEl.checked = customHostnameEnabled
  customHostnameInputEl.value = customHostname
  renderMode()
}

const normalizeVersion = value => String(value || '').trim().replace(/^[vV]/, '')

const normalizedVersions = () => versions.map(version => normalizeVersion(version))

const activeHACount = () => setupMode === 'manual' ? manualCommands.length : versions.length

const defaultK8SVersion = index => k8sVersions[index] || k8sVersions[0] || 'v1.34.6+rke2r1'

const manualChartAliasForVersion = version => {
  const distro = String(distroSelectEl?.value || config.distro || 'auto').toLowerCase()
  if (distro === 'prime') {
    return 'rancher-prime'
  }
  if (String(version || '').includes('alpha')) {
    return 'rancher-alpha'
  }
  return 'rancher-latest'
}

const manualImageTagForVersion = version => {
  const normalized = normalizeVersion(version)
  if (!normalized) {
    return 'v2.14.0'
  }
  return normalized === 'head' ? 'head' : `v${normalized}`
}

const manualHelmSetValue = (command, key) => {
  const normalized = String(command || '').replace(/\\\r?\n/g, ' ')
  const pattern = /--set(?:-string|-json)?(?:=|\s+)(?:"([^"]*)"|'([^']*)'|([^\s\\]+))/g
  let match
  while ((match = pattern.exec(normalized)) !== null) {
    const value = match[1] || match[2] || match[3] || ''
    const parts = value.split(',')
    for (const part of parts) {
      const [name, ...rawValueParts] = part.split('=')
      if (String(name || '').trim() === key && rawValueParts.length) {
        return rawValueParts.join('=').trim().replace(/^['"]|['"]$/g, '')
      }
    }
  }
  return ''
}

const buildSeedHelmCommand = index => {
  const version = normalizeVersion(versions[index] || versions[0] || '2.14.0')
  const chartAlias = manualChartAliasForVersion(version)
  const chartVersion = version && version !== 'head' ? version : '2.14.0'
  const password = String(bootstrapPasswordInputEl?.value || config.bootstrapPassword || 'change-me').replaceAll('\\', '\\\\').replaceAll("'", "'\\''")
  const imageTag = manualImageTagForVersion(version)
  const lines = [
    `helm install rancher ${chartAlias}/rancher \\`,
    '  --namespace cattle-system \\',
    `  --version ${chartVersion} \\`,
    '  --set hostname=placeholder \\',
    `  --set-string 'bootstrapPassword=${password}' \\`,
    '  --set tls=external \\',
    '  --set global.cattle.psp.enabled=false \\',
    `  --set image.tag=${imageTag} \\`,
    ...(currentServerCount() === 1 ? ['  --set replicas=1 \\'] : []),
    '  --set agentTLSMode=system-store'
  ]
  return lines.join('\n')
}

const ensureManualRows = () => {
  if (!manualCommands.length) {
    manualCommands = [buildSeedHelmCommand(0)]
  }
  if (customHostnameEnabled && manualCommands.length !== 1) {
    manualCommands = [manualCommands[0] || buildSeedHelmCommand(0)]
    k8sVersions = [k8sVersions[0] || defaultK8SVersion(0)]
    installerSHA256s = [installerSHA256s[0] || '']
  }
  while (k8sVersions.length < manualCommands.length) {
    k8sVersions.push(defaultK8SVersion(k8sVersions.length))
  }
  while (installerSHA256s.length < manualCommands.length) {
    installerSHA256s.push('')
  }
  if (k8sVersions.length > manualCommands.length) {
    k8sVersions = k8sVersions.slice(0, manualCommands.length)
  }
  if (installerSHA256s.length > manualCommands.length) {
    installerSHA256s = installerSHA256s.slice(0, manualCommands.length)
  }
}

const normalizedManualCommands = () => manualCommands.map(command => String(command || '').trim())

const normalizedK8SVersions = () => k8sVersions.map(version => String(version || '').trim())

const validRKE2Version = value => /^v?1\.\d+\.\d+\+rke2r\d+$/.test(String(value || '').trim())

const hostedRDSPasswordValidationMessage = value => {
  const password = String(value || '').trim()
  if (password.length < 8) {
    return 'Hosted tenant RDS password must be at least 8 characters.'
  }
  if (password.length > 41) {
    return 'Hosted tenant RDS password must be 41 characters or fewer for RDS MySQL/Aurora.'
  }
  if (/[/'"@ ]/.test(password)) {
    return 'Hosted tenant RDS password cannot contain /, \', ", @, or spaces.'
  }
  for (let i = 0; i < password.length; i += 1) {
    const code = password.charCodeAt(i)
    if (code < 32 || code > 126) {
      return 'Hosted tenant RDS password must contain printable ASCII characters only.'
    }
  }
  return ''
}

const hostedRDSPasswordAlphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!#$%&()*+,-.:;<=>?[]^_{|}~'

const generateHostedRDSPassword = () => {
  const length = 32
  const chars = new Uint32Array(length)
  if (window.crypto?.getRandomValues) {
    window.crypto.getRandomValues(chars)
  } else {
    for (let i = 0; i < chars.length; i += 1) {
      chars[i] = Math.floor(Math.random() * hostedRDSPasswordAlphabet.length)
    }
  }
  return Array.from(chars, value => hostedRDSPasswordAlphabet[value % hostedRDSPasswordAlphabet.length]).join('')
}

const normalizedAWSPrefix = () => {
  const input = setupQuery('input[data-tf-var="aws_prefix"]')
  return String((input && input.value) || '').trim().toLowerCase()
}

const collectTFVars = () => {
  const tfVars = {}

  tfVarInputEls.forEach(input => {
    const key = input.getAttribute('data-tf-var')
    tfVars[key] = String(input.value || '').trim()
  })

  tfVars.aws_prefix = normalizedAWSPrefix()

  const prefixInput = setupQuery('input[data-tf-var="aws_prefix"]')
  if (prefixInput) {
    prefixInput.value = tfVars.aws_prefix
  }

  return tfVars
}

const validateSetup = () => {
  const trimmed = normalizedVersions()
  const manualTrimmed = normalizedManualCommands()

  if (setupMode === 'auto' && trimmed.length < 1) {
    return { message: 'At least one HA version is required.', target: rowsEl.querySelector('input[data-index]') }
  }

  if (isHostedTenantDeployment()) {
    if (setupMode !== 'auto') {
      return { message: 'Hosted tenant K3s setup currently supports auto mode only.', target: autoModeBtnEl }
    }
    if (trimmed.length < 2) {
      return { message: 'Hosted tenant K3s needs one host and at least one tenant.', target: rowsEl.querySelector('input[data-index]') }
    }
    if (trimmed.length > hostedTenantMaxInstances) {
      return { message: 'Hosted tenant K3s supports up to 4 total Rancher instances: 1 host plus 3 tenants.', target: rowsEl }
    }
    const passwordMessage = hostedRDSPasswordValidationMessage(hostedRdsPasswordInputEl?.value || '')
    if (passwordMessage) {
      return { message: passwordMessage, target: hostedRdsPasswordInputEl }
    }
  }

  if (setupMode === 'auto') {
    for (let i = 0; i < trimmed.length; i += 1) {
      if (!trimmed[i]) {
        return {
          message: `Version for ${autoRowLabel(i)} cannot be empty.`,
          target: rowsEl.querySelector(`input[data-index="${i}"]`)
        }
      }
    }
  }

  if (setupMode === 'manual') {
    if (manualTrimmed.length < 1) {
      return { message: 'At least one manual Helm command is required.', target: manualRowsEl }
    }
    for (let i = 0; i < manualTrimmed.length; i += 1) {
      if (!manualTrimmed[i]) {
        return { message: `Helm command for HA ${i + 1} cannot be empty.`, target: manualRowsEl.querySelector(`textarea[data-manual-command-index="${i}"]`) }
      }
      if (!String(k8sVersions[i] || '').trim()) {
        return { message: `RKE2 version for HA ${i + 1} cannot be empty.`, target: manualRowsEl.querySelector(`input[data-k8s-index="${i}"]`) }
      }
      if (!validRKE2Version(k8sVersions[i])) {
        return { message: `RKE2 version for HA ${i + 1} must look like v1.34.6+rke2r1.`, target: manualRowsEl.querySelector(`input[data-k8s-index="${i}"]`) }
      }
      if (!resolveInstallerSHA && !/^[a-fA-F0-9]{64}$/.test(String(installerSHA256s[i] || '').trim())) {
        return { message: `Installer SHA256 for HA ${i + 1} must be a 64-character hex checksum.`, target: manualRowsEl.querySelector(`input[data-sha-index="${i}"]`) }
      }
      const manualReplicas = manualHelmSetValue(manualTrimmed[i], 'replicas')
      if (currentServerCount() === 1 && manualReplicas && manualReplicas !== '1') {
        return {
          message: `Single-server layout needs Rancher replicas=1 for HA ${i + 1}. Change replicas or choose the 3/5 server layout.`,
          target: manualRowsEl.querySelector(`textarea[data-manual-command-index="${i}"]`)
        }
      }
    }
  }

  if (customHostnameEnabled) {
    if (activeHACount() !== 1) {
      return { message: 'Custom Rancher URL can only be used with one HA.', target: customHostnameToggleEl }
    }

    if (!String(customHostname || '').trim()) {
      return { message: 'Enter a custom Rancher URL label.', target: customHostnameInputEl }
    }
  }

  const prefixInput = setupQuery('input[data-tf-var="aws_prefix"]')
  const prefix = normalizedAWSPrefix()

  if (!String(userFirstNameInputEl.value || '').trim()) {
    return {
      message: 'First name is required for AWS Owner tags.',
      target: userFirstNameInputEl
    }
  }

  if (!String(userLastNameInputEl.value || '').trim()) {
    return {
      message: 'Last name is required for AWS Owner tags.',
      target: userLastNameInputEl
    }
  }

  if (!/^[a-z]{2,3}$/.test(prefix)) {
    return {
      message: 'AWS prefix must be 2 or 3 letters, usually your initials.',
      target: prefixInput
    }
  }

  const pemKeyInput = setupQuery('input[data-tf-var="aws_pem_key_name"]')

  if (!String((pemKeyInput && pemKeyInput.value) || '').trim()) {
    return {
      message: 'AWS PEM key name is required.',
      target: pemKeyInput,
      notice: true
    }
  }

  if (!bootstrapPasswordInputEl.value.trim()) {
    return {
      message: 'Bootstrap password cannot be empty.',
      target: bootstrapPasswordInputEl
    }
  }

  return null
}

const setFieldLocked = (key, locked) => {
  const input = setupQuery(`input[data-tf-var="${key}"]`)
  const button = setupQuery(`button[data-lock-toggle="${key}"]`)

  if (!input || !button) {
    return
  }

  input.readOnly = locked
  button.innerHTML = locked ? lockIcon : unlockIcon
  button.dataset.state = locked ? 'locked' : 'unlocked'
  button.title = `${locked ? 'Unlock' : 'Lock'} ${input.closest('label')?.firstChild?.textContent.trim() || 'field'}`
  button.setAttribute('aria-label', button.title)

  button.classList.toggle('text-emerald-600', !locked)
  button.classList.toggle('dark:text-emerald-400', !locked)
  button.classList.toggle('text-zinc-500', locked)
  button.classList.toggle('dark:text-zinc-400', locked)
  input.classList.toggle('text-zinc-950', !locked)
  input.classList.toggle('dark:text-zinc-100', !locked)
  input.classList.toggle('text-zinc-500', locked)
  input.classList.toggle('dark:text-zinc-500', locked)
  input.classList.toggle('bg-white', !locked)
  input.classList.toggle('dark:bg-zinc-950/50', !locked)
  input.classList.toggle('bg-zinc-100', locked)
  input.classList.toggle('dark:bg-zinc-950/30', locked)
}

const lockAllAdvancedAWSFields = () => {
  lockedFieldInputEls.forEach(input => {
    setFieldLocked(input.getAttribute('data-tf-var'), true)
  })
}

const setLockedInputTone = (input, locked) => {
  input.classList.toggle('text-zinc-950', !locked)
  input.classList.toggle('dark:text-zinc-100', !locked)
  input.classList.toggle('text-zinc-500', locked)
  input.classList.toggle('dark:text-zinc-500', locked)
  input.classList.toggle('bg-white', !locked)
  input.classList.toggle('dark:bg-zinc-950/50', !locked)
  input.classList.toggle('bg-zinc-100', locked)
  input.classList.toggle('dark:bg-zinc-950/30', locked)
}

const setLockButtonTone = (button, locked) => {
  button.classList.toggle('text-emerald-600', !locked)
  button.classList.toggle('dark:text-emerald-400', !locked)
  button.classList.toggle('text-zinc-500', locked)
  button.classList.toggle('dark:text-zinc-400', locked)
}

const setHostedRDSPasswordLocked = locked => {
  if (!hostedRdsPasswordInputEl || !hostedRdsPasswordLockToggleEl) {
    return
  }

  hostedRdsPasswordInputEl.readOnly = locked
  hostedRdsPasswordLockToggleEl.innerHTML = locked ? lockIcon : unlockIcon
  hostedRdsPasswordLockToggleEl.dataset.state = locked ? 'locked' : 'unlocked'
  hostedRdsPasswordLockToggleEl.title = locked ? 'Unlock RDS MySQL password' : 'Lock RDS MySQL password'
  hostedRdsPasswordLockToggleEl.setAttribute('aria-label', hostedRdsPasswordLockToggleEl.title)
  setLockButtonTone(hostedRdsPasswordLockToggleEl, locked)
  setLockedInputTone(hostedRdsPasswordInputEl, locked)
}

const renderHostedRDSPasswordGenerateState = () => {
  if (!hostedRdsPasswordGenerateBtnEl || !hostedRdsPasswordInputEl) {
    return
  }
  const empty = !String(hostedRdsPasswordInputEl.value || '').trim()
  hostedRdsPasswordGenerateBtnEl.disabled = submitting || !empty
  hostedRdsPasswordGenerateBtnEl.classList.toggle('cursor-not-allowed', !empty)
  hostedRdsPasswordGenerateBtnEl.classList.toggle('opacity-50', !empty)
  hostedRdsPasswordGenerateBtnEl.title = empty ? '' : 'Clear the RDS password before generating a new one.'
}

const setHostedEC2InstanceTypeLocked = locked => {
  if (!hostedEc2InstanceTypeInputEl || !hostedEc2InstanceTypeLockToggleEl) {
    return
  }

  hostedEc2InstanceTypeInputEl.readOnly = locked
  hostedEc2InstanceTypeLockToggleEl.innerHTML = locked ? lockIcon : unlockIcon
  hostedEc2InstanceTypeLockToggleEl.dataset.state = locked ? 'locked' : 'unlocked'
  hostedEc2InstanceTypeLockToggleEl.title = locked ? 'Unlock hosted tenant EC2 type' : 'Lock hosted tenant EC2 type'
  hostedEc2InstanceTypeLockToggleEl.setAttribute('aria-label', hostedEc2InstanceTypeLockToggleEl.title)

  setLockButtonTone(hostedEc2InstanceTypeLockToggleEl, locked)
  setLockedInputTone(hostedEc2InstanceTypeInputEl, locked)
}

const toggleBootstrapPasswordVisibility = () => {
  const showing = bootstrapPasswordInputEl.type === 'text'
  bootstrapPasswordInputEl.type = showing ? 'password' : 'text'
  bootstrapPasswordToggleEl.textContent = showing ? 'Show' : 'Hide'
}

const toggleSecretFieldVisibility = key => {
  const input = setupQuery(`input[data-tf-var="${key}"]`)
  const button = setupQuery(`button[data-secret-toggle="${key}"]`)

  if (!input || !button) {
    return
  }

  const showing = input.type === 'text'
  input.type = showing ? 'password' : 'text'
  button.textContent = showing ? 'Show' : 'Hide'
}

const completionCopy = shouldContinue => shouldContinue
  ? {
      title: 'AWS setup started',
      body: 'The isolated run has been handed to the Lifecycle tab.',
      detail: 'Terraform state and run records are being tracked under a dedicated run slot.',
      accentClass: 'flex h-11 w-11 items-center justify-center rounded-full bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
      icon: '<path d="M20 6 9 17l-5-5"></path>'
    }
  : {
      title: 'Setup canceled',
      body: 'You can close this tab. The local test run will stop with a canceled setup message.',
      detail: 'No Rancher Runway plan was approved from this browser session.',
      accentClass: 'flex h-11 w-11 items-center justify-center rounded-full bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300',
      icon: '<path d="M18 6 6 18"></path><path d="m6 6 12 12"></path>'
    }

const renderCompletion = shouldContinue => {
  const copy = completionCopy(shouldContinue)

  doneAccentEl.className = copy.accentClass
  doneIconEl.innerHTML = copy.icon
  doneTitleEl.textContent = copy.title
  doneBodyEl.textContent = copy.body
  doneDetailEl.textContent = copy.detail
}

const resetEmbeddedSetupFlow = () => {
  pendingCompletionShouldContinue = true
  responseSubmitting = false
  setResponseButtonsDisabled(false)
  setResponseActionPending('')
  setSubmittingState(false)
  stopSetupStatePolling()
  renderResolverLogs([])
  renderPlanCards('')
  resolvingErrorBoxEl.textContent = ''
  reviewErrorBoxEl.textContent = ''
  editorErrorBoxEl.textContent = ''
  editorStatusBoxEl.textContent = ''
  setPhase('editor')
}

const setSubmittingState = nextSubmitting => {
  submitting = nextSubmitting
  const actionDisabled = nextSubmitting || panelBooting || panelLifecycleBusy
  addBtnEl.disabled = actionDisabled
  continueBtnEl.disabled = actionDisabled
  editorCancelBtnEl.disabled = actionDisabled
  const disabledTitle = panelBooting
    ? 'Startup safety check is still loading panel state.'
    : panelLifecycleBusy
      ? panelLifecycleMessage || 'A lifecycle operation is running.'
      : ''
  addBtnEl.title = disabledTitle
  continueBtnEl.title = disabledTitle
  editorCancelBtnEl.title = disabledTitle
  continueBtnEl.innerHTML = panelBooting
    ? '<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>Checking state'
    : panelLifecycleBusy
      ? '<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>Lifecycle running'
      : nextSubmitting
        ? '<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>Resolving plan'
        : 'Resolve Plan'
  ;[addBtnEl, continueBtnEl, editorCancelBtnEl].forEach(button => {
    if (!button) {
      return
    }
    button.classList.toggle('cursor-not-allowed', actionDisabled)
    button.classList.toggle('opacity-60', actionDisabled)
    button.classList.toggle('grayscale', actionDisabled)
  })
  ;[manualAddBtnEl, validateHelmBtnEl, recommendRKE2BtnEl, autoModeBtnEl, manualModeBtnEl, haRke2DeploymentBtnEl, hostedTenantDeploymentBtnEl, hostedRdsPasswordGenerateBtnEl, hostedRdsPasswordLockToggleEl, hostedEc2InstanceTypeLockToggleEl].forEach(button => {
    if (!button) {
      return
    }
    button.disabled = actionDisabled || (button === manualAddBtnEl && customHostnameEnabled) || (button === manualModeBtnEl && isHostedTenantDeployment())
    button.classList.toggle('cursor-not-allowed', actionDisabled)
    button.classList.toggle('opacity-60', actionDisabled)
    button.classList.toggle('grayscale', actionDisabled)
  })
  customHostnameToggleEl.disabled = nextSubmitting || isHostedTenantDeployment()
  customHostnameInputEl.disabled = nextSubmitting || isHostedTenantDeployment()
  if (hostedRdsPasswordInputEl) {
    hostedRdsPasswordInputEl.disabled = nextSubmitting || panelBooting || panelLifecycleBusy
  }
  if (hostedRdsPasswordToggleEl) {
    hostedRdsPasswordToggleEl.disabled = nextSubmitting
  }
  if (hostedEc2InstanceTypeInputEl) {
    hostedEc2InstanceTypeInputEl.disabled = nextSubmitting || panelBooting || panelLifecycleBusy
  }
  renderHostedRDSPasswordGenerateState()
  resolveInstallerSHAToggleEl.disabled = nextSubmitting
  distroSelectEl.disabled = nextSubmitting
  bootstrapPasswordInputEl.disabled = nextSubmitting
  bootstrapPasswordToggleEl.disabled = nextSubmitting
  preloadImagesToggleEl.disabled = nextSubmitting
  serverCountButtonEls.forEach(button => {
    button.disabled = nextSubmitting || isHostedTenantDeployment()
    button.classList.toggle('cursor-not-allowed', nextSubmitting || isHostedTenantDeployment())
    button.classList.toggle('opacity-60', nextSubmitting || isHostedTenantDeployment())
  })
  userFirstNameInputEl.disabled = nextSubmitting
  userLastNameInputEl.disabled = nextSubmitting

  tfVarInputEls.forEach(input => {
    input.disabled = nextSubmitting
  })

  lockToggleEls.forEach(button => {
    button.disabled = nextSubmitting
  })

  secretToggleEls.forEach(button => {
    button.disabled = nextSubmitting
  })

  rowsEl.querySelectorAll('input, button[data-remove-index]').forEach(element => {
    element.disabled = nextSubmitting || (panelBooting && element.hasAttribute('data-remove-index')) ||
      (element.hasAttribute('data-remove-index') && (customHostnameEnabled || versions.length <= minimumAutoRows()))
  })
  manualRowsEl.querySelectorAll('textarea, input, button').forEach(element => {
    element.disabled = nextSubmitting || panelBooting || panelLifecycleBusy ||
      (element.hasAttribute('data-manual-remove-index') && manualCommands.length <= 1)
  })
}

const setPanelBootingState = booting => {
  panelBooting = Boolean(booting)
  if (panelBooting && editorStatusBoxEl && !submitting) {
    editorStatusBoxEl.textContent = 'Checking local state before setup actions are enabled...'
  } else if (!panelBooting && editorStatusBoxEl.textContent === 'Checking local state before setup actions are enabled...' && !panelLifecycleBusy) {
    editorStatusBoxEl.textContent = ''
  }
  setSubmittingState(submitting)
  setResponseButtonsDisabled(responseSubmitting)
}

const setPanelLifecycleState = (busy, message = '') => {
  const previousMessage = panelLifecycleMessage
  panelLifecycleBusy = Boolean(busy)
  panelLifecycleMessage = panelLifecycleBusy
    ? message || 'A lifecycle operation is running. New setup actions are locked until it finishes.'
    : ''
  if (panelLifecycleBusy && editorStatusBoxEl && !submitting) {
    editorStatusBoxEl.textContent = panelLifecycleMessage
  } else if (!panelLifecycleBusy && editorStatusBoxEl.textContent === previousMessage) {
    editorStatusBoxEl.textContent = ''
  }
  if (panelLifecycleBusy && setupRootEl.dataset.phase === 'review') {
    reviewErrorBoxEl.textContent = panelLifecycleMessage
  } else if (!panelLifecycleBusy && reviewErrorBoxEl.textContent === previousMessage) {
    reviewErrorBoxEl.textContent = ''
  }
  setSubmittingState(submitting)
  setResponseButtonsDisabled(responseSubmitting)
}

const submitSetupFormWithoutHTMX = async formData => {
  const response = await fetch(setupFormEl.action, {
    method: 'POST',
    cache: 'no-store',
    credentials: 'same-origin',
    headers: {
      'Accept': 'application/json',
      'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8'
    },
    body: new URLSearchParams(formData).toString()
  })

  if (!response.ok) {
    throw new Error(await response.text() || 'Setup submit failed.')
  }
}

const validateManualHelmCommands = async ({ showModal = false } = {}) => {
  manualValidationResults = []
  renderManualValidation()
  editorStatusBoxEl.textContent = 'Validating Helm commands with helm template...'
  const response = await fetch(setupEndpoint(`/api/validate-helm?token=${encodeURIComponent(token)}`), {
    method: 'POST',
    cache: 'no-store',
    credentials: 'same-origin',
    headers: {
      'Accept': 'application/json',
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      helmCommands: normalizedManualCommands(),
      k8sVersions: normalizedK8SVersions()
    })
  })
  if (!response.ok) {
    throw new Error(await response.text() || 'Helm validation failed.')
  }
  const payload = await response.json()
  manualValidationResults = Array.isArray(payload.results) ? payload.results : []
  renderManualValidation()
  const failed = manualValidationResults.find(result => !result.ok)
  if (showModal) {
    openHelmValidationModal()
  }
  if (failed) {
    throw new Error(`HA ${Number(failed.index || 0) + 1}: ${failed.summary || 'Helm validation failed'}`)
  }
  editorStatusBoxEl.textContent = 'Helm commands rendered successfully.'
}

const recommendManualRKE2Versions = async () => {
  manualRKE2Recommendations = []
  renderManualRKE2Recommendations()
  editorStatusBoxEl.textContent = 'Finding supported RKE2 versions for the manual Helm commands...'
  const response = await fetch(setupEndpoint(`/api/recommend-rke2?token=${encodeURIComponent(token)}`), {
    method: 'POST',
    cache: 'no-store',
    credentials: 'same-origin',
    headers: {
      'Accept': 'application/json',
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      helmCommands: normalizedManualCommands()
    })
  })
  if (!response.ok) {
    throw new Error(await response.text() || 'RKE2 recommendation failed.')
  }
  const payload = await response.json()
  manualRKE2Recommendations = Array.isArray(payload.results) ? payload.results : []
  renderManualRKE2Recommendations()
  const failed = manualRKE2Recommendations.find(result => !result.ok)
  if (failed) {
    throw new Error(`HA ${Number(failed.index || 0) + 1}: ${failed.summary || 'RKE2 recommendation failed'}`)
  }
  editorStatusBoxEl.textContent = 'RKE2 recommendations are ready.'
}

const beginResolutionUI = () => {
  lastResolverFailure = ''
  if (resolvingSummaryEl) {
    resolvingSummaryEl.textContent = isHostedTenantDeployment()
      ? 'No AWS resources are being created yet. This step fetches Helm repos, SUSE support data, K3s patch releases, and installer SHA256 hashes, then shows the final hosted tenant plan for approval.'
      : 'No AWS resources are being created yet. This step fetches Helm repos, SUSE support data, RKE2 patch releases, and installer SHA256 hashes, then shows the final plan for approval.'
  }
  logPanelEl.innerHTML = '<span class="text-zinc-400 dark:text-zinc-500">Waiting for resolver output...</span>'
  if (reviewLogPanelEl) {
    reviewLogPanelEl.innerHTML = '<span class="text-zinc-400 dark:text-zinc-500">Waiting for resolver output...</span>'
  }
  renderPlanCards('')
  resolvingErrorBoxEl.textContent = ''
  reviewErrorBoxEl.textContent = ''
  setPhase('resolving')
  setSubmittingState(true)
  startSetupStatePolling()
}

const prepareSetupSubmit = async event => {
  if (event) {
    event.preventDefault()
    event.stopPropagation()
  }

  if (submitting) {
    return
  }

  if (panelBooting) {
    editorStatusBoxEl.textContent = 'Still checking local state. Setup actions will unlock after the panel reads the first state snapshot.'
    return
  }

  if (panelLifecycleBusy) {
    editorStatusBoxEl.textContent = panelLifecycleMessage || 'A lifecycle operation is running. Setup actions will unlock after it finishes.'
    return
  }

  const validationError = validateSetup()

  if (validationError) {
    showValidationError(validationError.message, validationError.target)
    if (validationError.notice) {
      await showNoticeModal({
        title: 'PEM key name required',
        body: 'Add the AWS PEM key name before resolving the plan. It should match the EC2 key pair name for your AWS account.'
      })
    }
    return
  }

  if (!systemReadiness || systemReadiness.ready !== true) {
    await loadSystemReadiness()
  }

  if (!systemReadiness || systemReadiness.ready !== true) {
    systemReadinessDetailsEl.open = true
    const message = systemReadiness?.summary || 'System readiness checks must pass before resolving the plan.'
    showValidationError(message, systemReadinessDetailsEl)
    await showNoticeModal({
      title: 'System readiness needs attention',
      body: 'Fix the missing required system checks before resolving the Rancher plan. Warnings are okay, but errors block continuing.'
    })
    return
  }

  clearValidationError()

  if (setupMode === 'manual') {
    try {
      await validateManualHelmCommands()
    } catch (error) {
      showValidationError(error instanceof Error ? error.message : 'Helm validation failed.', manualValidationBoxEl)
      await showNoticeModal({
        title: 'Helm validation failed',
        body: 'Fix the manual Helm command validation errors before resolving the plan.'
      })
      return
    }
  }

  const tfVars = collectTFVars()

  const prefixConfirmed = await showConfirmModal({
    title: 'Confirm AWS prefix',
    body: `AWS prefix is "${tfVars.aws_prefix}". This should be your initials and will be used to label AWS resources.`,
    confirmText: 'Use this prefix'
  })

  if (!prefixConfirmed) {
    return
  }

  const pemConfirmed = await showConfirmModal({
    title: 'Confirm PEM key name',
    body: `AWS PEM key name is "${tfVars.aws_pem_key_name}". This must match the EC2 key pair you want the run to use.`,
    confirmText: 'Use this key'
  })

  if (!pemConfirmed) {
    return
  }

  if (isHostedTenantDeployment()) {
    const hostedTenantConfirmed = await showConfirmModal({
      title: 'Confirm hosted tenant setup',
      body: 'Hosted tenant K3s setup takes longer than HA RKE2 because each Rancher instance uses a two-node K3s cluster backed by an Aurora MySQL/RDS datastore. The database resources can take a while to become ready; expect roughly 17 minutes before the run is fully set up.',
      confirmText: 'Create hosted tenant',
      cancelText: 'Go back'
    })

    if (!hostedTenantConfirmed) {
      return
    }
  }

  const formData = new FormData(setupFormEl)
  editorStatusBoxEl.textContent = 'Saving config and kicking off plan resolution...'
  beginResolutionUI()

  try {
    await submitSetupFormWithoutHTMX(formData)
  } catch (error) {
    setPhase('editor')
    showValidationError(error instanceof Error ? error.message : 'Setup submit failed.')
    setSubmittingState(false)
    stopSetupStatePolling()
  }
}

const cancelEditor = () => {
  if (submitting) {
    return
  }

  sendResponse('cancel')
}

const responseErrorBox = () => setupRootEl.dataset.phase === 'review' ? reviewErrorBoxEl : editorErrorBoxEl

const setResponseButtonsDisabled = disabled => {
  if (!respondActionsEl) {
    return
  }

  const actionDisabled = disabled || panelBooting || panelLifecycleBusy
  const disabledTitle = panelBooting
    ? 'Startup safety check is still loading panel state.'
    : panelLifecycleBusy
      ? panelLifecycleMessage || 'A lifecycle operation is running.'
      : ''
  respondActionsEl.querySelectorAll('button[data-response-action]').forEach(button => {
    button.disabled = actionDisabled
    if (disabledTitle) {
      button.title = disabledTitle
    } else {
      button.removeAttribute('title')
    }
    button.classList.toggle('cursor-not-allowed', actionDisabled)
    button.classList.toggle('opacity-60', actionDisabled)
    button.classList.toggle('grayscale', actionDisabled)
  })
}

const setResponseActionPending = action => {
  if (!respondActionsEl) {
    return
  }

  respondActionsEl.querySelectorAll('button[data-response-action]').forEach(button => {
    const buttonAction = button.getAttribute('data-response-action')
    if (action && buttonAction === action) {
      button.innerHTML = `<span class="spinner mr-2 !h-4 !w-4 !border-2"></span>${action === 'continue' ? 'Starting AWS setup...' : 'Canceling...'}`
    } else if (!action) {
      button.textContent = buttonAction === 'continue' ? 'Start AWS setup' : 'Cancel'
    }
  })
}

const sendResponse = async action => {
  if (responseSubmitting) {
    return
  }

  const shouldContinue = action === 'continue'
  if (shouldContinue && panelBooting) {
    responseErrorBox().textContent = 'Still checking local state. AWS setup actions will unlock after the panel reads the first state snapshot.'
    return
  }
  if (shouldContinue && panelLifecycleBusy) {
    responseErrorBox().textContent = panelLifecycleMessage || 'A lifecycle operation is running. AWS setup actions will unlock after it finishes.'
    return
  }

  responseSubmitting = true
  pendingCompletionShouldContinue = shouldContinue
  const body = new URLSearchParams()
  body.set('token', token)
  body.set('action', action)

  setResponseButtonsDisabled(true)
  setResponseActionPending(action)
  reviewErrorBoxEl.textContent = ''
  editorErrorBoxEl.textContent = ''

  try {
    const response = await fetch(setupEndpoint('/respond'), {
      method: 'POST',
      cache: 'no-store',
      credentials: 'same-origin',
      headers: {
        'Accept': 'application/json',
        'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8'
      },
      body: body.toString()
    })

    if (!response.ok) {
      responseErrorBox().textContent = await response.text()
      responseSubmitting = false
      setResponseButtonsDisabled(false)
      setResponseActionPending('')
      return
    }

    if (embeddedSetup) {
      resetEmbeddedSetupFlow()
      if (shouldContinue) {
        window.dispatchEvent(new CustomEvent('rancher-setup-started'))
      }
      return
    }
    renderCompletion(shouldContinue)
    setPhase('done')
  } catch (error) {
    responseErrorBox().textContent = error instanceof Error ? error.message : 'Failed to send setup response.'
    responseSubmitting = false
    setResponseButtonsDisabled(false)
    setResponseActionPending('')
  }
}

const appendLogLine = line => {
  const appendToPanel = panel => {
    if (!panel) {
      return
    }
    const empty = panel.querySelector('span')

    if (empty && (empty.textContent.includes('Waiting for resolver output') || empty.textContent.includes('Resolver output will appear'))) {
      empty.remove()
    }

    const span = document.createElement('span')
    span.className = 'block'
    span.textContent = line
    panel.appendChild(span)
    panel.scrollTop = panel.scrollHeight
  }

  appendToPanel(logPanelEl)
  appendToPanel(reviewLogPanelEl)
}

const renderResolverLogs = logs => {
  const lines = Array.isArray(logs) ? logs : []
  const renderPanel = (panel, emptyText) => {
    if (!panel) {
      return
    }
    if (!lines.length) {
      panel.innerHTML = `<span class="text-zinc-400 dark:text-zinc-500">${escapeHtml(emptyText)}</span>`
      return
    }
    panel.textContent = lines.join('\n')
    panel.scrollTop = panel.scrollHeight
  }

  renderPanel(logPanelEl, 'Waiting for resolver output...')
  renderPanel(reviewLogPanelEl, 'Resolver output will appear here.')
}

const applySetupSnapshot = snapshot => {
  if (!snapshot || typeof snapshot !== 'object') {
    return
  }

  renderResolverLogs(snapshot.logs)
  if (typeof snapshot.plan === 'string' && snapshot.plan) {
    renderPlanCards(snapshot.plan)
  }
  const error = typeof snapshot.error === 'string' ? snapshot.error : ''
  editorErrorBoxEl.textContent = error
  resolvingErrorBoxEl.textContent = error
  reviewErrorBoxEl.textContent = error

  if (snapshot.phase && snapshot.phase !== setupRootEl.dataset.phase) {
    setPhase(snapshot.phase)
  }
  if (snapshot.phase === 'review' || snapshot.phase === 'done' || snapshot.phase === 'editor') {
    setSubmittingState(false)
  }
  if (snapshot.phase === 'editor' && error) {
    showResolverFailure(error)
    stopSetupStatePolling()
  }
}

const pollSetupState = async () => {
  try {
    const response = await fetch(setupEndpoint(`/state?token=${encodeURIComponent(token)}`), {
      cache: 'no-store',
      headers: { 'Accept': 'application/json' }
    })
    if (!response.ok) {
      return
    }
    const snapshot = await response.json()
    applySetupSnapshot(snapshot)
    if (snapshot.phase === 'review' || snapshot.phase === 'done') {
      stopSetupStatePolling()
    }
  } catch (_) {}
}

const startSetupStatePolling = () => {
  if (setupStatePollTimer) {
    return
  }
  pollSetupState()
  setupStatePollTimer = window.setInterval(pollSetupState, 1000)
}

const stopSetupStatePolling = () => {
  if (!setupStatePollTimer) {
    return
  }
  window.clearInterval(setupStatePollTimer)
  setupStatePollTimer = null
}

const connectEventStream = () => {
  const source = new EventSource(setupEndpoint(`/events?token=${encodeURIComponent(token)}`))

  source.onmessage = event => {
    let payload

    try {
      payload = JSON.parse(event.data)
    } catch (_) {
      return
    }

    switch (payload.type) {
      case 'phase':
        setPhase(payload.phase)
        if (payload.phase === 'review' || payload.phase === 'editor') {
          setSubmittingState(false)
          stopSetupStatePolling()
        }
        if (payload.phase === 'done') {
          stopSetupStatePolling()
          source.close()
        }
        break
      case 'log':
        appendLogLine(payload.line)
        break
      case 'plan':
        renderPlanCards(payload.plan)
        break
      case 'error':
        showResolverFailure(payload.error)
        resolvingErrorBoxEl.textContent = payload.error
        reviewErrorBoxEl.textContent = payload.error
        break
    }
  }

  source.onerror = () => {}
}

addBtnEl.addEventListener('click', () => {
  if (submitting) {
    return
  }

  if (customHostnameEnabled) {
    showNoticeModal({
      title: 'Custom URL is limited to one HA',
      body: 'A custom Rancher URL creates exactly one HA because the DNS name must be unique. Turn off "Use a custom Rancher URL" if you want to add more than one HA.',
      confirmText: 'Got it'
    })
    return
  }

  if (versions.length >= maximumAutoRows()) {
    showNoticeModal({
      title: 'Hosted tenant limit reached',
      body: 'Hosted tenant K3s supports up to 4 total Rancher instances: 1 host plus 3 tenants.',
      confirmText: 'Got it'
    })
    return
  }

  versions.push('')
  renderDeploymentType()
  renderRows()
})

manualAddBtnEl.addEventListener('click', () => {
  if (submitting || customHostnameEnabled) {
    if (customHostnameEnabled) {
      showNoticeModal({
        title: 'Custom URL is limited to one HA',
        body: 'A custom Rancher URL creates exactly one HA because the DNS name must be unique. Turn off "Use a custom Rancher URL" if you want to add more than one HA.',
        confirmText: 'Got it'
      })
    }
    return
  }
  const index = manualCommands.length
  manualCommands.push(buildSeedHelmCommand(index))
  k8sVersions.push(defaultK8SVersion(index))
  installerSHA256s.push('')
  manualValidationResults = []
  manualRKE2Recommendations = []
  renderManualRows()
  renderManualRKE2Recommendations()
  totalInstancesValueEl.textContent = String(activeHACount())
})

validateHelmBtnEl.addEventListener('click', async () => {
  if (submitting) {
    return
  }
  try {
    await validateManualHelmCommands({ showModal: true })
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Helm validation failed.'
    if (helmValidationModalEl && !helmValidationModalEl.classList.contains('hidden')) {
      editorErrorBoxEl.textContent = message
      editorStatusBoxEl.textContent = ''
      return
    }
    showValidationError(message, manualValidationBoxEl)
  }
})

recommendRKE2BtnEl?.addEventListener('click', async () => {
  if (submitting) {
    return
  }
  try {
    await recommendManualRKE2Versions()
  } catch (error) {
    showValidationError(error instanceof Error ? error.message : 'RKE2 recommendation failed.', manualRKE2RecommendationBoxEl)
  }
})

manualRKE2RecommendationBoxEl?.addEventListener('click', event => {
  const button = event.target.closest('button[data-apply-rke2-recommendations]')
  if (!button || submitting) {
    return
  }
  manualRKE2Recommendations.forEach(result => {
    if (result.ok && result.recommendedRKE2Version) {
      k8sVersions[Number(result.index || 0)] = result.recommendedRKE2Version
    }
  })
  manualValidationResults = []
  renderManualRows()
  renderManualValidation()
  renderManualRKE2Recommendations()
  clearValidationError()
  editorStatusBoxEl.textContent = 'Applied recommended RKE2 versions.'
})

helmValidationModalCloseEl?.addEventListener('click', closeHelmValidationModal)
helmValidationModalEl?.addEventListener('click', event => {
  if (event.target === helmValidationModalEl) {
    closeHelmValidationModal()
  }
})
document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && helmValidationModalEl && !helmValidationModalEl.classList.contains('hidden')) {
    closeHelmValidationModal()
  }
})

autoModeBtnEl.addEventListener('click', () => {
  if (submitting || setupMode === 'auto') {
    return
  }
  setupMode = 'auto'
  clearValidationError()
  renderMode()
})

manualModeBtnEl.addEventListener('click', () => {
  if (submitting || setupMode === 'manual') {
    return
  }
  if (isHostedTenantDeployment()) {
    showNoticeModal({
      title: 'Hosted tenant uses auto mode',
      body: 'Hosted tenant K3s needs the resolver to build the host and tenant plans before setup, so this path currently stays in auto mode.',
      confirmText: 'Got it'
    })
    return
  }
  setupMode = 'manual'
  clearValidationError()
  ensureManualRows()
  renderMode()
})

haRke2DeploymentBtnEl?.addEventListener('click', () => {
  if (submitting || deploymentType === 'ha-rke2') {
    return
  }
  deploymentType = 'ha-rke2'
  clearValidationError()
  renderDeploymentType()
  renderCustomHostname()
})

hostedTenantDeploymentBtnEl?.addEventListener('click', () => {
  if (submitting || deploymentType === 'hosted-tenant-k3s') {
    return
  }
  deploymentType = 'hosted-tenant-k3s'
  clearValidationError()
  renderDeploymentType()
  renderCustomHostname()
})

resolveInstallerSHAToggleEl.addEventListener('change', event => {
  if (submitting) {
    return
  }
  resolveInstallerSHA = event.target.checked
  clearValidationError()
  renderManualRows()
})

serverCountButtonEls.forEach(button => {
  button.addEventListener('click', () => {
    if (button.disabled || !serverCountInputEl) {
      return
    }
    serverCountInputEl.value = String(normalizeServerCount(button.dataset.serverCount))
    clearValidationError()
    renderServerTopology()
  })
})

bootstrapPasswordToggleEl.addEventListener('click', toggleBootstrapPasswordVisibility)
hostedRdsPasswordToggleEl?.addEventListener('click', () => {
  if (!hostedRdsPasswordInputEl || !hostedRdsPasswordToggleEl) {
    return
  }
  const showing = hostedRdsPasswordInputEl.type === 'text'
  hostedRdsPasswordInputEl.type = showing ? 'password' : 'text'
  hostedRdsPasswordToggleEl.textContent = showing ? 'Show' : 'Hide'
})

hostedRdsPasswordInputEl?.addEventListener('input', clearValidationError)
hostedRdsPasswordInputEl?.addEventListener('input', renderHostedRDSPasswordGenerateState)
hostedRdsPasswordInputEl?.addEventListener('blur', () => {
  if (submitting) {
    return
  }
  setHostedRDSPasswordLocked(true)
})
hostedEc2InstanceTypeInputEl?.addEventListener('input', clearValidationError)
hostedEc2InstanceTypeInputEl?.addEventListener('blur', () => {
  if (submitting) {
    return
  }
  setHostedEC2InstanceTypeLocked(true)
})
hostedRdsPasswordGenerateBtnEl?.addEventListener('click', () => {
  if (submitting || !hostedRdsPasswordInputEl) {
    return
  }
  if (String(hostedRdsPasswordInputEl.value || '').trim()) {
    renderHostedRDSPasswordGenerateState()
    return
  }
  hostedRdsPasswordInputEl.value = generateHostedRDSPassword()
  hostedRdsPasswordInputEl.type = 'password'
  if (hostedRdsPasswordToggleEl) {
    hostedRdsPasswordToggleEl.textContent = 'Show'
  }
  setHostedRDSPasswordLocked(true)
  renderHostedRDSPasswordGenerateState()
  clearValidationError()
  editorStatusBoxEl.textContent = 'Generated an RDS MySQL password that fits AWS character and length rules.'
})
hostedRdsPasswordLockToggleEl?.addEventListener('mousedown', event => {
  event.preventDefault()
})
hostedRdsPasswordLockToggleEl?.addEventListener('click', () => {
  if (submitting || !hostedRdsPasswordInputEl) {
    return
  }
  const willUnlock = hostedRdsPasswordInputEl.readOnly
  setHostedRDSPasswordLocked(!willUnlock)
  if (willUnlock) {
    hostedRdsPasswordInputEl.focus()
    hostedRdsPasswordInputEl.select()
  }
})
hostedEc2InstanceTypeLockToggleEl?.addEventListener('mousedown', event => {
  event.preventDefault()
})
hostedEc2InstanceTypeLockToggleEl?.addEventListener('click', () => {
  if (submitting || !hostedEc2InstanceTypeInputEl) {
    return
  }
  const willUnlock = hostedEc2InstanceTypeInputEl.readOnly
  setHostedEC2InstanceTypeLocked(!willUnlock)
  if (willUnlock) {
    hostedEc2InstanceTypeInputEl.focus()
    hostedEc2InstanceTypeInputEl.select()
  }
})

themeToggleEl?.addEventListener('click', () => {
  setTheme(currentTheme() === 'dark' ? 'light' : 'dark')
})

lockToggleEls.forEach(button => {
  button.addEventListener('mousedown', event => {
    event.preventDefault()
  })

  button.addEventListener('click', () => {
    if (submitting) {
      return
    }

    const key = button.getAttribute('data-lock-toggle')
    const input = setupQuery(`input[data-tf-var="${key}"]`)

    if (!input) {
      return
    }

    const willUnlock = input.readOnly
    setFieldLocked(key, !willUnlock)

    if (willUnlock) {
      input.focus()
      input.select()
    }
  })
})

lockedFieldInputEls.forEach(input => {
  input.addEventListener('blur', () => {
    if (submitting) {
      return
    }

    setFieldLocked(input.getAttribute('data-tf-var'), true)
  })
})

secretToggleEls.forEach(button => {
  button.addEventListener('click', () => {
    if (submitting) {
      return
    }

    toggleSecretFieldVisibility(button.getAttribute('data-secret-toggle'))
  })
})

customHostnameToggleEl.addEventListener('change', event => {
  if (submitting) {
    return
  }

  customHostnameEnabled = event.target.checked
  clearValidationError()
  renderCustomHostname()
})

customHostnameInputEl.addEventListener('input', event => {
  customHostname = event.target.value
  clearValidationError()
})

editorCancelBtnEl.addEventListener('click', cancelEditor)
setupFormEl.addEventListener('submit', prepareSetupSubmit)
continueBtnEl.addEventListener('click', prepareSetupSubmit)

if (respondActionsEl) {
  respondActionsEl.querySelectorAll('button[data-response-action]').forEach(button => {
    button.addEventListener('click', () => {
      sendResponse(button.getAttribute('data-response-action'))
    })
  })
}

planCardsEl?.addEventListener('click', async event => {
  const button = event.target.closest('button[data-copy-plan-command]')
  if (!button) {
    return
  }
  const index = Number(button.getAttribute('data-copy-plan-command'))
  const command = planCommandCopies[index] || ''
  if (!command) {
    editorStatusBoxEl.textContent = 'No Helm command is available to copy.'
    return
  }
  const originalText = button.textContent
  try {
    await copyTextToClipboard(command)
    button.textContent = 'Copied'
    editorStatusBoxEl.textContent = 'Copied Helm install command to clipboard.'
    window.setTimeout(() => {
      button.textContent = originalText
    }, 1600)
  } catch (error) {
    editorStatusBoxEl.textContent = error instanceof Error ? error.message : 'Failed to copy Helm install command.'
  }
})

setupRootEl.addEventListener('htmx:afterRequest', event => {
  const requestEl = event.detail.elt

  if (requestEl !== setupFormEl && !setupFormEl.contains(requestEl)) {
    return
  }

  if (event.detail.successful) {
    return
  }

  showValidationError(event.detail.xhr.responseText || 'Setup submit failed.')
  setSubmittingState(false)
  stopSetupStatePolling()
})

setupRootEl.addEventListener('rancher-control-panel-booting', event => {
  setPanelBootingState(Boolean(event.detail?.booting))
})

setupRootEl.addEventListener('rancher-control-panel-lifecycle', event => {
  setPanelLifecycleState(Boolean(event.detail?.busy), event.detail?.message || '')
})

renderEditableConfig()
renderDeploymentType()
renderCustomHostname()
setPanelBootingState(panelBooting)
setTheme(currentTheme(), false)
loadSystemReadiness()
connectEventStream()

})()
