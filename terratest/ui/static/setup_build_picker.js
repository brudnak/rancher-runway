// The browse view uses dated Ranch Hand reports. Registry inspection happens
// only after a person chooses a build to review.
const indexCache = new Map()
const indexCacheTTL = 2 * 60 * 1000

export function filterIndexedBuilds(builds, query = '') {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  return builds.filter(build => words.every(word =>
    [build.line, build.version, build.webhook, build.reference, 'SUSE staging'].join(' ').toLowerCase().includes(word)
  ))
}

export function buildDateLabel(build, now = Date.now()) {
  const value = build.created
  const time = Date.parse(value || '')
  if (!Number.isFinite(time)) return 'Date unavailable'
  const day = new Date(time).toISOString().slice(0, 10)
  const today = new Date(now).toISOString().slice(0, 10)
  const days = Math.round((Date.parse(today) - Date.parse(day)) / 86400000)
  if (days === 0) return 'Today'
  if (days === 1) return 'Yesterday'
  return days > 1 && days < 8 ? `${days} days ago` : day
}

export async function loadBuildIndex(url, signal, refresh = false, fetcher = fetch) {
  const key = String(url)
  const cached = indexCache.get(key)
  if (!refresh && cached && Date.now() - cached.at < indexCacheTTL) return cached.data
  const target = new URL(url)
  if (refresh) target.searchParams.set('refresh', '1')
  const response = await fetcher(target, { signal, credentials: 'same-origin', cache: 'no-store' })
  if (!response.ok) throw new Error((await response.text()).trim() || 'Could not load the build index.')
  const data = await response.json()
  if (signal.aborted) return null
  if (indexCache.size >= 16) indexCache.delete(indexCache.keys().next().value)
  indexCache.set(key, { data, at: Date.now() })
  return data
}

function buildEvidence(build, escapeHtml) {
  const field = (label, value) => `<div><dt>${label}</dt><dd>${escapeHtml(value || 'Unavailable')}</dd></div>`
  const catalogLabel = build.catalogImage === build.reference ? 'server catalog' : 'same-tag assets catalog'
  const date = new Date(build.created)
  const built = Number.isFinite(date.getTime()) ? date.toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC') : ''
  const chartRows = Object.entries(build.charts || {}).map(([chart, version]) => {
    const catalog = build.catalogVersions?.[chart]
    const match = version && catalog ? catalog.includes(version) : undefined
    const evidence = match === true ? ` · In ${catalogLabel}` : match === false ? ` · Missing from ${catalogLabel}` : ''
    return field(escapeHtml(chart), (version || 'Unavailable') + evidence)
  }).join('')
  const catalog = Object.entries(build.catalogVersions || {}).map(([chart, versions]) =>
    field(escapeHtml(chart), versions.join(', ') || 'No versions in index')
  ).join('')
  return `<dl class="setup-build-facts">
    ${field('Built', built)}${field('Build version', build.version)}
    </dl><h4>System chart versions</h4><dl class="setup-build-facts">${chartRows}</dl>
    ${build.catalogStatus ? `<p>${escapeHtml(build.catalogStatus)}</p>` : ''}
    <details><summary>Hashes and catalog evidence</summary><dl class="setup-build-facts">
    ${field('Image digest', build.digest)}${field('Source commit', build.revision)}
    ${build.ossRevision ? field('OSS commit', build.ossRevision) : ''}${field('Platform', build.platform)}
    ${build.catalogImage ? field('Catalog image', build.catalogImage) : ''}
    ${build.chartRevision ? field('Charts commit', build.chartRevision) : ''}
    </dl>
    ${catalog ? `<h4>Versions in this catalog</h4><dl class="setup-build-facts">${catalog}</dl>` : ''}</details>`
}

export function pickRancherBuild(dialog, { endpoint, token, docker, label, version, escapeHtml }) {
  const get = selector => dialog.querySelector(selector)
  const search = get('[data-search]'), age = get('[data-age]'), list = get('[data-builds]')
  const browse = get('[data-browse]'), review = get('[data-review]'), status = get('[data-status]')
  const events = new AbortController(), options = { signal: events.signal }
  let request, detailRequest, searchTimer, records = [], line = '', nextOffset = 0
  let result, busy = false, error = '', selected = null, verified = null, selection = ''
  let returnFocus = null

  const urlFor = params => {
    const url = new URL(endpoint, window.location.href)
    url.search = new URLSearchParams({ ...params, token })
    return url
  }
  const loadDetails = async (params, signal) => {
    const response = await fetch(urlFor(params), { signal, credentials: 'same-origin', cache: 'no-store' })
    if (!response.ok) throw new Error((await response.text()).trim() || 'Could not verify this image.')
    return response.json()
  }
  const ageText = () => age.value === '0' ? 'all dates' : `last ${age.value} days`

  const render = () => {
    if (events.signal.aborted) return
    const shown = filterIndexedBuilds(records, search.value)
    get('[data-back]').hidden = !line
    get('[data-view-title]').textContent = line ? `Rancher ${line} · build history` : 'Latest reported heads'
    get('[data-view-hint]').textContent = line ? 'Newest builds first. Choose one to inspect its image and charts.' : 'One build per release line. Start with a line, then review its details.'
    get('[data-spinner]').hidden = !busy
    get('[data-skeleton]').hidden = !busy || records.length > 0
    list.setAttribute('aria-busy', String(busy))
    get('[data-error]').hidden = !error
    get('[data-error-message]').textContent = error
    get('[data-empty]').hidden = busy || Boolean(error) || shown.length > 0
    get('[data-empty]').textContent = search.value.trim() ? 'No matching builds. Clear the filter or choose another date range.' : `No reported builds in the ${ageText()}. Try All dates or inspect an exact image below.`
    const more = get('[data-more]')
    more.hidden = !line || !result?.more
    more.disabled = busy
    more.textContent = busy && records.length ? 'Loading…' : 'Show older builds'
    get('[data-refresh]').disabled = busy
    status.textContent = busy ? (records.length ? 'Updating the build index…' : 'Loading recent builds…')
      : `${shown.length}${line && result?.total > records.length ? ` of ${result.total}` : ''} ${line ? 'builds' : 'release lines'} · ${ageText()}`
    get('[data-index-note]').textContent = result?.stale
      ? 'Using a saved index because its source is unavailable. Refresh to try again.'
      : 'From Webhook Ranch Hand, checked three times daily. The selected image is verified live.'
    const warnings = get('[data-warnings]')
    warnings.hidden = !result?.warnings?.length
    warnings.querySelector('ul').replaceChildren(...(result?.warnings || []).map(message => {
      const item = document.createElement('li'); item.textContent = message; return item
    }))
    list.replaceChildren(...shown.map(build => {
      const row = document.createElement('article')
      row.className = 'setup-build-row'
      const hash = build.version.match(/-([a-f0-9]{7,40})-head$/i)?.[1]?.slice(0, 7) || ''
      row.innerHTML = `<div class="setup-build-line"><span class="setup-build-kicker">${line ? 'BUILD' : 'RANCHER'}</span><h3>${escapeHtml(line ? hash : build.line)}</h3><span class="setup-build-source">SUSE staging</span></div>
        <div class="setup-build-summary"><code title="${escapeHtml(build.version)}">${escapeHtml(build.version)}</code><div class="setup-build-meta"><span title="${escapeHtml(build.created)}">Built ${escapeHtml(buildDateLabel(build))}</span><span>Webhook <strong>${escapeHtml(build.webhook || 'Not reported')}</strong></span></div></div>
        <div class="setup-build-row-actions"><button type="button" data-review-build>Review build <span aria-hidden="true">→</span></button>${line ? '' : '<button type="button" data-history>Build history</button>'}</div>`
      const choose = row.querySelector('[data-review-build]')
      choose.addEventListener('click', () => { returnFocus = choose; showReview(build) })
      row.querySelector('[data-history]')?.addEventListener('click', () => { line = build.line; search.value = ''; loadIndex() })
      return row
    }))
  }

  const loadIndex = async ({ refresh = false, more = false } = {}) => {
    clearTimeout(searchTimer)
    request?.abort()
    request = new AbortController()
    const current = request
    busy = true; error = ''
    if (!more) { records = []; result = null; nextOffset = 0 }
    render()
    try {
      const params = { catalog: '1', days: age.value, offset: String(more ? nextOffset : 0) }
      if (line) { params.line = line; params.q = search.value.trim() }
      const data = await loadBuildIndex(urlFor(params), current.signal, refresh)
      if (current.signal.aborted || events.signal.aborted) return
      records = more ? [...records, ...data.builds] : data.builds
      result = data; nextOffset = data.nextOffset
    } catch (cause) {
      if (!current.signal.aborted) error = `${cause.message} You can retry or inspect an exact image below.`
    } finally {
      if (request === current && !events.signal.aborted) { busy = false; render() }
    }
  }

  const verifySelected = async () => {
    detailRequest?.abort()
    detailRequest = new AbortController()
    const current = detailRequest
    verified = null
    get('[data-use]').disabled = true
    get('[data-detail-loading]').hidden = false
    get('[data-detail-error]').hidden = true
    get('[data-evidence]').replaceChildren()
    try {
      const params = selected.reportPath ? { report: selected.reportPath, docker: docker ? '1' : '0' } : { reference: selected.reference }
      const build = await loadDetails(params, current.signal)
      if (current.signal.aborted || events.signal.aborted) return
      // Standalone Docker must never receive a split rancher-server image.
      if (docker && !/\/rancher\/rancher:/.test(build.reference)) throw new Error('Choose a full rancher/rancher image for Linode Docker.')
      verified = build
      get('[data-evidence]').innerHTML = buildEvidence(build, escapeHtml)
      get('[data-verified-reference]').textContent = build.reference
      get('[data-use]').textContent = build.moving ? 'Use moving tag' : 'Use this build'
      get('[data-use]').disabled = false
    } catch (cause) {
      if (!current.signal.aborted) {
        get('[data-detail-error-message]').textContent = cause.message
        get('[data-detail-error]').hidden = false
      }
    } finally {
      if (detailRequest === current && !events.signal.aborted) get('[data-detail-loading]').hidden = true
    }
  }

  const showReview = build => {
    selected = build
    browse.hidden = true; review.hidden = false
    get('[data-review-title]').textContent = build.line ? `Rancher ${build.line}` : 'Inspect Rancher image'
    get('[data-verified-reference]').textContent = build.reference || build.version
    const report = get('[data-report-link]')
    report.hidden = !build.reportURL
    report.href = build.reportURL || '#'
    get('[data-review-back]').focus()
    verifySelected()
  }
  const backFromReview = () => {
    detailRequest?.abort(); verified = null; selected = null
    review.hidden = true; browse.hidden = false
    if (returnFocus?.isConnected) returnFocus.focus()
    else search.focus()
  }

  get('[data-target]').textContent = `Choose a build for ${label}.${docker ? ' Standalone Docker availability is verified when you review a build.' : ''}`
  age.value = '30'; search.value = ''; browse.hidden = false; review.hidden = true
  get('[data-direct-image]').value = String(version || '').includes('/') ? version : ''
  get('[data-direct]').open = false
  get('[data-close]').addEventListener('click', () => dialog.close(), options)
  get('[data-back]').addEventListener('click', () => { line = ''; search.value = ''; loadIndex() }, options)
  get('[data-refresh]').addEventListener('click', () => loadIndex({ refresh: true }), options)
  get('[data-retry]').addEventListener('click', () => loadIndex({ refresh: true }), options)
  get('[data-more]').addEventListener('click', () => loadIndex({ more: true }), options)
  age.addEventListener('change', () => loadIndex(), options)
  search.addEventListener('input', () => {
    clearTimeout(searchTimer)
    if (line) searchTimer = setTimeout(() => loadIndex(), 180)
    else render()
  }, options)
  get('[data-direct-form]').addEventListener('submit', event => {
    event.preventDefault()
    const reference = get('[data-direct-image]').value.trim()
    if (reference) showReview({ reference })
  }, options)
  get('[data-review-back]').addEventListener('click', backFromReview, options)
  get('[data-detail-retry]').addEventListener('click', verifySelected, options)
  get('[data-use]').addEventListener('click', () => {
    if (!verified || get('[data-use]').disabled) return
    selection = verified.reference; dialog.close()
  }, options)
  return new Promise(resolve => {
    dialog.addEventListener('close', () => {
      events.abort(); request?.abort(); detailRequest?.abort(); clearTimeout(searchTimer)
      list.replaceChildren(); get('[data-evidence]').replaceChildren()
      resolve(selection)
    }, { once: true })
    dialog.showModal(); search.focus(); loadIndex()
  })
}

