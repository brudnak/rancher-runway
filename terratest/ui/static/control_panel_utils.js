export const escapeHtml = value => String(value || '')
  .replaceAll('&', '&amp;')
  .replaceAll('<', '&lt;')
  .replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;')
  .replaceAll('\'', '&#39;')

export const escapeRegExp = value => String(value || '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

export const compactPath = value => {
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

export const formatUSD = value => {
  const number = Number(value || 0)
  return number.toLocaleString(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  })
}

export const highlightLogLine = (line, query) => {
  const escapedLine = escapeHtml(line)
  if (!query) {
    return escapedLine || '&nbsp;'
  }

  const pattern = new RegExp(escapeRegExp(query), 'ig')
  const highlighted = escapedLine.replace(pattern, match => `<mark class="rounded bg-amber-200 px-0.5 text-zinc-950 dark:bg-amber-300">${match}</mark>`)
  return highlighted || '&nbsp;'
}

export const lineMatchesLogLevel = (line, level) => {
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

export const extractCleanupLineValue = (output, label) => {
  const line = output.find(item => item.includes(label))
  if (!line) {
    return ''
  }

  return line.slice(line.indexOf(label) + label.length).trim()
}

export const parseCleanupCost = output => {
  const total = extractCleanupLineValue(output, 'Estimated total:')
    || extractCleanupLineValue(output, 'Estimated total (EC2 + EBS only):')
  if (!total) {
    return null
  }

  return {
    total,
    region: extractCleanupLineValue(output, 'Region:'),
    runtime: extractCleanupLineValue(output, 'Total runtime across instances:'),
    ec2: extractCleanupLineValue(output, 'EC2:'),
    ebs: extractCleanupLineValue(output, 'EBS:'),
    rds: extractCleanupLineValue(output, 'RDS/Aurora:'),
    loadBalancers: extractCleanupLineValue(output, 'Load balancers:')
  }
}

export const clusterItems = state => state && state.clusters && Array.isArray(state.clusters.items)
  ? state.clusters.items
  : []

export const operationOutput = operation => operation && Array.isArray(operation.output) ? operation.output : []

export const podsFor = cluster => Array.isArray(cluster.pods) ? cluster.pods : []

export const trimTrailingPathSeparator = value => String(value || '').replace(/[\\/]+$/, '')

export const parentPath = value => {
  const path = trimTrailingPathSeparator(value)
  const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'))
  return index > 0 ? path.slice(0, index) : path
}

export const sameRunKey = (left, right) => String(left || '').trim() === String(right || '').trim()

export const badge = label => `<span class="inline-flex items-center rounded-md bg-zinc-100 px-2 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">${escapeHtml(label)}</span>`
