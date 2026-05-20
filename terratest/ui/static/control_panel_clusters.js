import {
  badge,
  clusterItems,
  escapeHtml,
  podsFor
} from './control_panel_utils.js'

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
      className: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-200'
    }
  }
  if (cluster.available) {
    return {
      label: 'Unavailable',
      className: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-200'
    }
  }
  return {
    label: 'Missing',
    className: 'bg-zinc-100 text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300'
  }
}

const emptyPodsText = cluster => cluster.type === 'downstream'
  ? 'Pods are unavailable until the downstream kubeconfig is reachable.'
  : cluster.deploymentType === 'hosted-tenant-k3s'
    ? 'Pods are unavailable until the hosted-tenant kubeconfig exists and kubectl can reach the cluster.'
    : cluster.deploymentType === 'linode-docker-cattle'
      ? 'Docker Rancher does not expose Kubernetes pods from this panel.'
    : 'Pods are unavailable until kubeconfig exists and kubectl can reach the cluster.'

const deploymentKindLabel = deploymentType => {
  switch (deploymentType) {
    case 'hosted-tenant-k3s':
      return 'Hosted tenant K3s'
    case 'linode-docker-cattle':
      return 'Linode Docker'
    default:
      return 'RKE2 HA'
  }
}

const clusterGroupLabel = deploymentType => deploymentType === 'hosted-tenant-k3s'
  ? 'Hosted tenant instance'
  : deploymentType === 'linode-docker-cattle'
    ? 'Docker Rancher'
    : 'HA cluster'

const managementSectionLabel = deploymentType => deploymentType === 'hosted-tenant-k3s'
  ? 'Rancher instance'
  : deploymentType === 'linode-docker-cattle'
    ? 'Docker Rancher'
    : 'Management cluster'

const metaItem = (label, value) => `
  <div class="min-w-0">
    <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">${escapeHtml(label)}</div>
    <div class="mt-1 break-words text-sm font-medium text-zinc-800 [overflow-wrap:anywhere] dark:text-zinc-200">${value}</div>
  </div>
`

const clusterRunKey = cluster => String(cluster?.runId || 'default')

const clusterHAKey = cluster => String(cluster?.haIndex || 0)

const hostedTenantInstanceLabel = clusterOrHA => {
  const index = Number(clusterOrHA?.haIndex || 0)
  const role = clusterOrHA?.role || clusterOrHA?.local?.role
  if (role === 'host' || index === 1) {
    return 'Host'
  }
  if (index > 1) {
    return `Tenant ${index - 1}`
  }
  return 'Tenant'
}

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

const groupDeploymentType = group => {
  if (group?.run?.deploymentType) {
    return group.run.deploymentType
  }
  const local = group?.has?.find(ha => ha.local)?.local
  return local?.deploymentType || 'ha-rke2'
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

export const createClusterPanel = ({
  clustersEl,
  getActionState,
  getActiveSelection,
  setActiveSelection,
  getLastState,
  renderLastState,
  cleanupResultDismissed
}) => {
  let previousLeaders = new Map()
  const pendingLeaderHighlights = new Map()
  const collapsedClusters = new Map()
  const collapsedPods = new Map()
  const initializedCollapseState = new Set()
  const kubeconfigPathActionFeedback = new Map()
  const kubeconfigPathActionTimers = new Map()

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

  const pathActionKey = (action, id) => `${action}:${id}`

  const flashKubeconfigPathAction = (clusterId, action, status) => {
    const key = pathActionKey(action, clusterId)
    window.clearTimeout(kubeconfigPathActionTimers.get(key))
    kubeconfigPathActionFeedback.set(key, status)
    renderLastState()
    kubeconfigPathActionTimers.set(key, window.setTimeout(() => {
      if (kubeconfigPathActionFeedback.get(key) === status) {
        kubeconfigPathActionFeedback.delete(key)
        renderLastState()
      }
    }, 1800))
  }

  const kubeconfigPathActionContent = (clusterId, action, idleLabel, activeLabel, successLabel, errorLabel) => {
    const actionState = getActionState()
    const active = action === 'open'
      ? actionState.activeOpenKubeconfigPathClusterId === clusterId
      : action === 'copy-linode-ip'
        ? actionState.activeCopyLinodeIPClusterId === clusterId
        : actionState.activeCopyKubeconfigPathClusterId === clusterId
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
    if (cluster.deploymentType === 'linode-docker-cattle') {
      const actionState = getActionState()
      const loadingDockerLogs = actionState.activeDockerLogsClusterId === cluster.id
      const dockerLogSpinner = loadingDockerLogs ? '<span class="spinner mr-2"></span>' : ''
      return `
        <span class="text-sm text-zinc-500 dark:text-zinc-400">No kubeconfig for Docker install</span>
        <button type="button" data-action="docker-logs" data-cluster="${escapeHtml(cluster.id)}"${loadingDockerLogs ? ' disabled' : ''} class="inline-flex min-h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 disabled:cursor-default disabled:opacity-70 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${dockerLogSpinner}${loadingDockerLogs ? 'Loading logs' : 'Docker logs'}</button>
      `
    }

    if (!cluster.available) {
      return '<span class="text-sm text-zinc-500 dark:text-zinc-400">Kubeconfig unavailable</span>'
    }

    const actionState = getActionState()
    const downloading = actionState.activeDownloadClusterId === cluster.id
    const copying = actionState.activeCopyClusterId === cluster.id
    const copyingHelm = actionState.activeCopyHelmClusterId === cluster.id
    const copyingHelmUpgrade = actionState.activeCopyHelmUpgradeClusterId === cluster.id
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

    const actionState = getActionState()
    const pods = podsFor(cluster)
    const currentLeader = pods.find(pod => pod.leader && pod.leaderLabel === 'Leader') || pods.find(pod => pod.leader)
    const changedLeader = pendingLeaderHighlights.get(cluster.id)
    const isDownstream = cluster.type === 'downstream'
    const isHostedTenant = cluster.deploymentType === 'hosted-tenant-k3s'
    const isLinodeDocker = cluster.deploymentType === 'linode-docker-cattle'
    const status = statusFor(cluster)
    const clusterCollapsed = collapsedClusters.get(cluster.id) === true
    const toggleText = clusterCollapsed ? 'Show details' : 'Hide details'
    const version = cluster.version ? ` <span class="text-zinc-500 dark:text-zinc-400">(${escapeHtml(cluster.version)})</span>` : ''
    const typeBadge = badge(isDownstream ? 'Downstream' : isHostedTenant ? hostedTenantInstanceLabel(cluster) : isLinodeDocker ? 'Linode Docker' : 'Local')
    const contextParts = isDownstream
      ? [`Downstream from HA ${cluster.haIndex}`]
      : isHostedTenant
        ? [`Hosted-tenant K3s ${hostedTenantInstanceLabel(cluster).toLowerCase()}`]
        : isLinodeDocker
          ? [`Docker Rancher on Linode ${cluster.haIndex}`]
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
    const loadBalancer = cluster.loadBalancer ? escapeHtml(cluster.loadBalancer) : isHostedTenant ? '<span class="text-zinc-500 dark:text-zinc-400">Managed by hosted-tenant ALB</span>' : '<span class="text-zinc-500 dark:text-zinc-400">Unavailable</span>'
    const networkLabel = isLinodeDocker ? 'Linode IP' : 'Load Balancer'
    const openingKubeconfigPath = actionState.activeOpenKubeconfigPathClusterId === cluster.id
    const copyingKubeconfigPath = actionState.activeCopyKubeconfigPathClusterId === cluster.id
    const openPathFeedback = kubeconfigPathActionFeedback.get(pathActionKey('open', cluster.id))
    const copyPathFeedback = kubeconfigPathActionFeedback.get(pathActionKey('copy', cluster.id))
    const copyLinodeIPFeedback = kubeconfigPathActionFeedback.get(pathActionKey('copy-linode-ip', cluster.id))
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
    const copyLinodeIPToneClass = copyLinodeIPFeedback === 'error'
      ? 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200'
      : copyLinodeIPFeedback === 'success'
        ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200'
        : 'border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'
    const networkValue = isLinodeDocker && cluster.loadBalancer
      ? `
        <div class="flex flex-wrap items-center gap-2">
          <span>${escapeHtml(cluster.loadBalancer)}</span>
          <button type="button" data-action="copy-linode-ip" data-cluster="${escapeHtml(cluster.id)}"${actionState.activeCopyLinodeIPClusterId === cluster.id ? ' disabled' : ''} class="${pathButtonBaseClass} ${copyLinodeIPToneClass}">${kubeconfigPathActionContent(cluster.id, 'copy-linode-ip', 'Copy', 'Copying', 'Copied', 'Copy failed')}</button>
        </div>
      `
      : loadBalancer
    const kubeconfig = isLinodeDocker
      ? ''
      : cluster.kubeconfigPath ? `
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
      : isLinodeDocker
        ? ''
        : '<div class="mt-4 text-sm text-zinc-500 dark:text-zinc-400">Leader not detected yet.</div>'
    const downstreamClasses = isDownstream
      ? 'border-l-4 border-l-emerald-500 bg-emerald-50/50 dark:bg-emerald-500/[0.04]'
      : isHostedTenant && cluster.role === 'host'
        ? 'border-l-4 border-l-sky-500 bg-sky-50/50 dark:bg-sky-500/[0.04]'
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
            ${metaItem(networkLabel, networkValue)}
            ${isLinodeDocker ? '' : metaItem('Kubeconfig', kubeconfig)}
            ${namespace}
            ${clusterID}
          </div>
          ${leaderSummary}
          ${isLinodeDocker ? '' : renderPodsTable(cluster, pods, changedLeader)}
        `}
      </article>
    `
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

    const selection = getActiveSelection()
    if (!selection.runKey || !groups.some(group => group.runKey === selection.runKey)) {
      selection.runKey = groups[0].runKey
    }
    const activeRunGroup = groups.find(group => group.runKey === selection.runKey) || groups[0]
    if (!selection.haKey || !activeRunGroup.has.some(ha => ha.haKey === selection.haKey)) {
      selection.haKey = activeRunGroup.has[0]?.haKey || ''
    }
    setActiveSelection(selection)
    const activeHA = activeRunGroup.has.find(ha => ha.haKey === selection.haKey) || activeRunGroup.has[0]

    const runTabs = groups.map(group => {
      const active = group.runKey === activeRunGroup.runKey
      const clusterCount = group.has.reduce((count, ha) => count + (ha.local ? 1 : 0) + ha.downstreams.length, 0)
      const deploymentLabel = deploymentKindLabel(groupDeploymentType(group))
      return `
        <button type="button" data-action="select-cluster-run" data-run-key="${escapeHtml(group.runKey)}" class="${active ? 'rounded-md border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-sm font-semibold text-emerald-800 shadow-sm dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200' : 'rounded-md border border-zinc-200 bg-white px-3 py-1.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'}">
          ${escapeHtml(group.label)}
          <span class="${active ? 'ml-2 text-emerald-700/80 dark:text-emerald-200/80' : 'ml-2 text-zinc-500 dark:text-zinc-400'}">${escapeHtml(deploymentLabel)}</span>
          <span class="${active ? 'ml-2 text-emerald-600 dark:text-emerald-300' : 'ml-2 text-zinc-500 dark:text-zinc-400'}">${clusterCount}</span>
        </button>
      `
    }).join('')

    const haTabs = activeRunGroup.has.map(ha => {
      const active = ha.haKey === activeHA?.haKey
      const downstreamCount = ha.downstreams.length
      const version = ha.local?.version ? ` • ${ha.local.version}` : ''
      const tabLabel = ha.local?.deploymentType === 'hosted-tenant-k3s'
        ? hostedTenantInstanceLabel(ha)
        : ha.local?.deploymentType === 'linode-docker-cattle'
          ? `Docker Rancher ${ha.haIndex || ha.haKey}`
          : `HA ${ha.haIndex || ha.haKey}`
      const countLabel = ha.local?.deploymentType === 'hosted-tenant-k3s'
        ? downstreamCount ? `${downstreamCount} import` : 'K3s'
        : ha.local?.deploymentType === 'linode-docker-cattle'
          ? 'Linode'
        : `${downstreamCount} downstream`
      return `
        <button type="button" data-action="select-cluster-ha" data-ha-key="${escapeHtml(ha.haKey)}" class="${active ? 'rounded-md border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-sm font-semibold text-emerald-800 shadow-sm dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200' : 'rounded-md border border-zinc-200 bg-white px-3 py-1.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'}">
          ${escapeHtml(tabLabel)}${escapeHtml(version)}
          <span class="${active ? 'ml-2 text-emerald-600 dark:text-emerald-300' : 'ml-2 text-zinc-500 dark:text-zinc-400'}">${escapeHtml(countLabel)}</span>
        </button>
      `
    }).join('')

    const localCluster = activeHA?.local
    const downstreams = activeHA?.downstreams || []
    const localHTML = localCluster
      ? renderCluster(localCluster)
      : '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No local cluster record found for this HA yet.</div>'
    const isActiveLinodeDocker = activeHA?.local?.deploymentType === 'linode-docker-cattle'
    const downstreamHTML = downstreams.length
      ? downstreams.map(renderCluster).join('')
      : activeHA?.local?.deploymentType === 'hosted-tenant-k3s'
        ? '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No imported cluster records discovered for this hosted-tenant instance yet.</div>'
        : '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No downstream clusters discovered for this HA yet.</div>'
    const activeDeploymentType = groupDeploymentType(activeRunGroup)

    clustersEl.innerHTML = `
      <div class="grid gap-4">
        <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Run slot</div>
          <div class="mt-2 flex flex-wrap gap-2">${runTabs}</div>
        </div>
        <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">${clusterGroupLabel(activeDeploymentType)}</div>
          <div class="mt-2 flex flex-wrap gap-2">${haTabs}</div>
        </div>
        <div class="grid gap-4">
          <div>
          <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">${managementSectionLabel(activeDeploymentType)}</div>
            ${localHTML}
          </div>
          ${isActiveLinodeDocker ? '' : `
          <div>
            <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">${activeHA?.local?.deploymentType === 'hosted-tenant-k3s' ? 'Imported cluster records' : 'Downstream clusters'}</div>
            <div class="grid gap-4">${downstreamHTML}</div>
          </div>
          `}
        </div>
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
    return messages.join(' • ')
  }

  const toggleCluster = clusterId => {
    collapsedClusters.set(clusterId, collapsedClusters.get(clusterId) !== true)
    renderLastState()
  }

  const togglePods = clusterId => {
    collapsedPods.set(clusterId, collapsedPods.get(clusterId) !== true)
    renderLastState()
  }


  return {
    flashKubeconfigPathAction,
    renderClusters,
    toggleCluster,
    togglePods,
    updateLeaderTracking
  }
}
