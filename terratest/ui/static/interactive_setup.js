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

let versions = Array.isArray(setupData.versions) ? setupData.versions : ['']
let config = setupData.config || {
  distro: 'auto',
  bootstrapPassword: '',
  preloadImages: false,
  tfVars: {}
}
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

const rowClass = 'grid gap-3 rounded-xl border border-zinc-200 bg-white p-3 shadow-sm dark:border-white/10 dark:bg-white/[0.03] dark:shadow-none sm:grid-cols-[auto_minmax(0,1fr)_auto] sm:items-center'
const inputClass = 'w-full rounded-lg border border-zinc-200 bg-white px-3.5 py-2.5 font-medium text-zinc-950 outline-none focus:border-emerald-400 dark:border-white/10 dark:bg-zinc-950/50 dark:text-zinc-100'
const removeButtonClass = 'rounded-lg border border-zinc-200 bg-zinc-50 px-3.5 py-2.5 text-sm font-medium text-rose-600 hover:bg-zinc-100 disabled:cursor-default disabled:opacity-60 dark:border-white/10 dark:bg-white/[0.04] dark:text-rose-300 dark:hover:bg-white/[0.08]'
const lockIcon = '<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="18" height="11" x="3" y="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>'
const unlockIcon = '<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="18" height="11" x="3" y="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 9.9-1"></path></svg>'

const setupFormEl = byId('setupForm')
const rowsEl = byId('rows')
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
const distroSelectEl = byId('distroSelect')
const bootstrapPasswordInputEl = byId('bootstrapPasswordInput')
const bootstrapPasswordToggleEl = byId('bootstrapPasswordToggle')
const preloadImagesToggleEl = byId('preloadImagesToggle')
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
const resolvingErrorBoxEl = byId('resolvingErrorBox')
const planCardsEl = byId('planCards')
const planFallbackEl = byId('planFallback')
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
    const haMatch = trimmed.match(/^HA\s+(\d+)$/)

    if (haMatch) {
      finishCurrent()
      current = {
        title: `HA ${haMatch[1]}`,
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
      : '<div class="text-sm text-zinc-500 dark:text-zinc-400">No resolved metadata was emitted for this HA.</div>'

    const commands = card.commands.length
      ? card.commands.map(command => `
        <div class="setup-code-editor">
          <div class="setup-code-editor-header">
            <div class="setup-code-editor-title">${escapeHtml(command.label)}</div>
            <div class="setup-code-editor-lang">shell</div>
          </div>
          <div class="setup-code-lines" role="region" aria-label="${escapeHtml(command.label)} shell command">
            ${renderCodeLines(command.text)}
          </div>
        </div>
      `).join('')
      : '<div class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">No Helm command was emitted for this HA.</div>'

    return `
      <details class="setup-ha-card" open>
        <summary class="setup-ha-summary">
          <div>
            <div class="flex flex-wrap items-center gap-3">
              <h3 class="text-lg font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(card.title)}</h3>
              <span class="rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-semibold text-zinc-700 dark:bg-white/[0.06] dark:text-zinc-200">Ready for approval</span>
            </div>
            <p class="mt-1 text-sm text-zinc-500 dark:text-zinc-400">Resolved install details for review before AWS setup starts.</p>
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
  userFirstNameInputEl.value = config.userFirstName || ''
  userLastNameInputEl.value = config.userLastName || ''

  tfVarInputEls.forEach(input => {
    const key = input.getAttribute('data-tf-var')
    input.value = (config.tfVars && config.tfVars[key]) || ''
  })

  lockAllAdvancedAWSFields()
}

const renderRows = () => {
  if (customHostnameEnabled && versions.length !== 1) {
    versions = [versions[0] || '']
  }

  rowsEl.innerHTML = versions.map((version, index) => {
    const removeDisabled = customHostnameEnabled || versions.length <= 1 ? ' disabled' : ''

    return [
      `<div class="${rowClass}">`,
      `<div class="inline-flex w-fit rounded-md bg-zinc-100 px-2.5 py-1 text-sm font-medium text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">HA ${index + 1}</div>`,
      `<div><input class="${inputClass}" type="text" name="versions" value="${escapeHtml(version)}" data-index="${index}" placeholder="2.14.1-alpha3" /></div>`,
      `<div><button class="${removeButtonClass}" type="button" data-remove-index="${index}"${removeDisabled}>Remove</button></div>`,
      '</div>'
    ].join('')
  }).join('')

  totalInstancesValueEl.textContent = String(versions.length)
  addBtnEl.disabled = submitting
  addBtnEl.setAttribute('aria-disabled', customHostnameEnabled ? 'true' : 'false')
  addBtnEl.classList.toggle('cursor-not-allowed', customHostnameEnabled)
  addBtnEl.classList.toggle('opacity-50', customHostnameEnabled)

  rowsEl.querySelectorAll('input[data-index]').forEach(input => {
    input.addEventListener('input', event => {
      versions[Number(event.target.getAttribute('data-index'))] = event.target.value
      clearValidationError()
    })
  })

  rowsEl.querySelectorAll('button[data-remove-index]').forEach(button => {
    button.addEventListener('click', () => {
      if (versions.length <= 1 || submitting || customHostnameEnabled) {
        return
      }

      versions.splice(Number(button.getAttribute('data-remove-index')), 1)
      renderRows()
    })
  })
}

const renderCustomHostname = () => {
  customHostnameBoxEl.dataset.enabled = customHostnameEnabled ? 'true' : 'false'
  customHostnameToggleEl.checked = customHostnameEnabled
  customHostnameInputEl.value = customHostname
  renderRows()
}

const normalizeVersion = value => String(value || '').trim().replace(/^[vV]/, '')

const normalizedVersions = () => versions.map(version => normalizeVersion(version))

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

  if (trimmed.length < 1) {
    return { message: 'At least one HA version is required.', target: rowsEl.querySelector('input[data-index]') }
  }

  for (let i = 0; i < trimmed.length; i += 1) {
    if (!trimmed[i]) {
      return {
        message: `Version for HA ${i + 1} cannot be empty.`,
        target: rowsEl.querySelector(`input[data-index="${i}"]`)
      }
    }
  }

  if (customHostnameEnabled) {
    if (trimmed.length !== 1) {
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
      detail: 'No Rancher HA plan was approved from this browser session.',
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
    button.classList.toggle('cursor-not-allowed', actionDisabled)
    button.classList.toggle('opacity-60', actionDisabled)
    button.classList.toggle('grayscale', actionDisabled)
  })
  customHostnameToggleEl.disabled = nextSubmitting
  customHostnameInputEl.disabled = nextSubmitting
  distroSelectEl.disabled = nextSubmitting
  bootstrapPasswordInputEl.disabled = nextSubmitting
  bootstrapPasswordToggleEl.disabled = nextSubmitting
  preloadImagesToggleEl.disabled = nextSubmitting
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
      (element.hasAttribute('data-remove-index') && (customHostnameEnabled || versions.length <= 1))
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

const beginResolutionUI = () => {
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
  resolvingErrorBoxEl.textContent = error
  reviewErrorBoxEl.textContent = error

  if (snapshot.phase && snapshot.phase !== setupRootEl.dataset.phase) {
    setPhase(snapshot.phase)
  }
  if (snapshot.phase === 'review' || snapshot.phase === 'done') {
    setSubmittingState(false)
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
        if (payload.phase === 'review') {
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

  versions.push('')
  renderRows()
})

bootstrapPasswordToggleEl.addEventListener('click', toggleBootstrapPasswordVisibility)

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

renderCustomHostname()
renderEditableConfig()
setPanelBootingState(panelBooting)
setTheme(currentTheme(), false)
loadSystemReadiness()
connectEventStream()

})()
