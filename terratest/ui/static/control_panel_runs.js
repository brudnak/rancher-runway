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
  if (run.deploymentType === 'hosted-tenant-k3s') {
    return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-t*.${run.route53Fqdn}` : run.route53Fqdn || 'generated per slot'
  }
  if (run.deploymentType === 'linode-docker-cattle') {
    return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-*.${run.route53Fqdn}` : run.route53Fqdn || 'generated per slot'
  }
  if (run.customHostnamePrefix) {
    return `${run.customHostnamePrefix}.${run.route53Fqdn || ''}`.replace(/\.$/, '')
  }
  return run.awsPrefix && run.route53Fqdn ? `${run.awsPrefix}-h*.${run.route53Fqdn}` : run.route53Fqdn || 'generated per slot'
}

export const activeOperations = state => [
  ['setup', 'Setup', state?.setup],
  ['readiness', 'Readiness', state?.readiness],
  ['cleanup', 'Destroy', state?.cleanup],
  ['linodeSetup', 'Linode setup', state?.linodeSetup],
  ['linodeCleanup', 'Linode destroy', state?.linodeCleanup]
].filter(([, , operation]) => operation?.running)

export const operationForRun = (run, state) => {
  const runId = run?.runId || ''
  return activeOperations(state).find(([, , operation]) => sameRunKey(operation.runId, runId)) || null
}

export const operationBadgeHTML = operation => {
  if (!operation) {
    return ''
  }
  const [, label, snapshot] = operation
  const started = snapshot?.startedAt ? ` since ${new Date(snapshot.startedAt).toLocaleTimeString()}` : ''
  return `<span class="run-live-pill"><span class="spinner run-progress-spinner"></span>${escapeHtml(label)} running${escapeHtml(started)}</span>`
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

export const runTimelineHTML = (run, state) => {
  const status = String(run?.status || '').toLowerCase()
  const setupRunning = (state?.setup?.running && sameRunKey(state.setup.runId, run.runId)) ||
    (state?.linodeSetup?.running && sameRunKey(state.linodeSetup.runId, run.runId))
  const readinessRunning = state?.readiness?.running && sameRunKey(state.readiness.runId, run.runId)
  const cleanupRunning = (state?.cleanup?.running && sameRunKey(state.cleanup.runId, run.runId)) ||
    (state?.linodeCleanup?.running && sameRunKey(state.linodeCleanup.runId, run.runId))
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
    <div class="run-progress" aria-label="Run lifecycle">
      ${steps.map(step => `
        <div class="run-progress-step" data-state="${escapeHtml(step.state)}">
          <span class="${step.state === 'active' ? 'spinner run-progress-spinner' : 'run-progress-dot'}"></span>
          <span>${escapeHtml(step.label)}</span>
        </div>
      `).join('')}
    </div>
  `
}

export const runFolderPath = run => {
  if (run?.runFolderPath) {
    return run.runFolderPath
  }
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

export const runFolderAvailable = run => Boolean(runFolderPath(run) && run?.runFolderExists !== false)

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
  const variantClass = ['primary', 'blue', 'danger', 'utility'].includes(variant) ? variant : 'secondary'
  const classes = `run-action-button run-action-button--${variantClass}${disabled ? ' run-action-button--disabled' : ''}`
  return `<button type="button" data-run-action="${escapeHtml(action)}" data-run-id="${escapeHtml(runId || '')}" ${disabled ? 'disabled' : ''} ${title ? `title="${escapeHtml(title)}"` : ''} class="${classes}">${label}</button>`
}
