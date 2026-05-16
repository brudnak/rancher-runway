import {
  clusterItems,
  escapeHtml,
  parentPath,
  sameRunKey,
  trimTrailingPathSeparator
} from './control_panel_utils.js'

export { sameRunKey }

export const runVersionsLabel = run => Array.isArray(run?.rancherVersions) && run.rancherVersions.length
  ? run.rancherVersions.join(', ')
  : 'not recorded'

export const runHostnameLabel = run => {
  if (!run) {
    return 'not recorded'
  }
  if (run.customHostnamePrefix) {
    return `${run.customHostnamePrefix}.${run.route53Fqdn || ''}`.replace(/\.$/, '')
  }
  return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-h*.${run.route53Fqdn}` : run.route53Fqdn || 'generated per slot'
}

export const operationForRun = (run, state) => {
  const runId = run?.runId || ''
  const operations = [
    ['setup', 'Setup', state?.setup],
    ['readiness', 'Readiness', state?.readiness],
    ['cleanup', 'Destroy', state?.cleanup]
  ]
  return operations.find(([, , operation]) => operation?.running && sameRunKey(operation.runId, runId)) || null
}

export const operationBadgeHTML = operation => {
  if (!operation) {
    return ''
  }
  const [, label, snapshot] = operation
  const started = snapshot?.startedAt ? ` since ${new Date(snapshot.startedAt).toLocaleTimeString()}` : ''
  return `<span class="inline-flex items-center rounded-full bg-sky-100 px-2.5 py-1 text-xs font-semibold text-sky-700 dark:bg-sky-500/15 dark:text-sky-300"><span class="spinner mr-1.5 !h-3 !w-3 !border-[1.5px]"></span>${escapeHtml(label)} running${escapeHtml(started)}</span>`
}

export const runHasFailure = run => {
  const status = String(run?.status || '').toLowerCase()
  return status.includes('failed') || status.includes('error')
}

export const readinessFailedRun = (runs, state) => {
  const readiness = state?.readiness || {}
  if (readiness.running || !readiness.error) {
    return runs.find(run => runHasFailure(run)) || null
  }
  const failedRunId = readiness.runId || ''
  return runs.find(run => sameRunKey(run.runId, failedRunId)) || runs.find(run => runHasFailure(run)) || null
}

export const runClusterStats = (run, state) => {
  const runId = run?.runId || ''
  const items = clusterItems(state).filter(cluster => sameRunKey(cluster.runId, runId))
  const management = items.filter(cluster => cluster.type !== 'downstream').length
  const downstream = items.filter(cluster => cluster.type === 'downstream').length
  const reachable = items.filter(cluster => cluster.reachable).length
  return { management, downstream, reachable, total: items.length }
}

export const runTone = (run, operation) => {
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

export const runStatusClasses = tone => ({
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

export const runTimelineHTML = (run, state) => {
  const status = String(run?.status || '').toLowerCase()
  const setupRunning = state?.setup?.running && sameRunKey(state.setup.runId, run.runId)
  const readinessRunning = state?.readiness?.running && sameRunKey(state.readiness.runId, run.runId)
  const cleanupRunning = state?.cleanup?.running && sameRunKey(state.cleanup.runId, run.runId)
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

export const runFolderPath = run => {
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

export const runTerraformPath = run => {
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

export const renderRunActionButton = ({ action, runId, label, variant = 'secondary', disabled = false, title = '' }) => {
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
