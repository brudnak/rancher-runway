// Interleave repositories so the first page can compare the same line across registries.
export function buildReferences(groups) {
  const references = []
  const count = Math.max(0, ...groups.map(group => group.tags?.length || 0))
  for (let i = 0; i < count; i++) {
    for (const group of groups) {
      if (group.tags?.[i]) references.push(`${group.registry}/${group.repository}:${group.tags[i]}`)
    }
  }
  return [...new Set(references)]
}

function versionParts(record) {
  const tag = record.reference.slice(record.reference.lastIndexOf(':') + 1)
  const match = String(record.summary?.version || tag).match(/^v?(\d+)\.(\d+)(?:\.(\d+))?/)
    || tag.match(/^v?(\d+)\.(\d+)(?:\.(\d+))?/)
  return match ? match.slice(1).map(value => Number(value || 0)) : [0, 0, 0]
}

function compareVersions(a, b) {
  const av = versionParts(a), bv = versionParts(b)
  return bv[0] - av[0] || bv[1] - av[1] || bv[2] - av[2]
}

const createdTime = record => Date.parse(record.summary?.created || '')
const displayTime = value => Number.isFinite(Date.parse(value || ''))
  ? new Date(value).toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC') : 'Build date unavailable'

export function headSelectors(references) {
  const selectors = [...new Set(references.flatMap(reference => {
    const match = reference.match(/:v?(2\.\d+\.0)(?:-[a-f0-9]+)?-head$/i)
    return match ? [match[1] + '-head'] : []
  }))]
  return selectors.sort((a, b) => compareVersions({ reference: ':' + a }, { reference: ':' + b }))
}

// Dates come from image configs, never tag lexicography or registry push times.
export function selectBuilds(records, { text = '', registry = '', sort = 'newest', age = '30', from = '', to = '', latest = true, now = Date.now() } = {}) {
  const start = age === 'custom' ? (from ? Date.parse(from + 'T00:00:00Z') : -Infinity)
    : age === 'all' ? -Infinity : now - Number(age) * 86400000
  const end = age === 'custom' && to ? Date.parse(to + 'T00:00:00Z') + 86400000 : Infinity
  const dated = age !== 'all'
  const words = text.trim().toLowerCase().split(/\s+/).filter(Boolean)
  let matches = records.filter(record => {
    if (!record.summary || (registry && !record.reference.startsWith(registry + '/'))) return false
    const timestamp = createdTime(record)
    if (dated && (!Number.isFinite(timestamp) || timestamp < start || timestamp >= end)) return false
    const haystack = [record.reference, record.summary.version, record.summary.revision, record.summary.ossRevision,
      record.summary.digest, record.summary.created, ...Object.entries(record.details?.charts || {}).flat()].join(' ').toLowerCase()
    return words.every(word => haystack.includes(word))
  })
  const newest = (a, b) => {
    const at = createdTime(a), bt = createdTime(b)
    return (Number.isFinite(bt) ? bt : -Infinity) - (Number.isFinite(at) ? at : -Infinity)
      || compareVersions(a, b) || a.reference.localeCompare(b.reference)
  }
  if (latest) {
    const seen = new Set()
    matches = matches.sort(newest).filter(record => {
      const key = record.reference.slice(0, record.reference.lastIndexOf(':')) + ':' + versionParts(record).slice(0, 2).join('.')
      if (seen.has(key)) return false
      seen.add(key)
      return true
    })
  }
  return matches.sort((a, b) => {
    if (sort === 'version') return compareVersions(a, b) || newest(a, b)
    if (sort === 'oldest') {
      const at = createdTime(a), bt = createdTime(b)
      return (Number.isFinite(at) ? at : Infinity) - (Number.isFinite(bt) ? bt : Infinity) || compareVersions(a, b) || a.reference.localeCompare(b.reference)
    }
    return newest(a, b)
  })
}

export async function scanBuildSummaries(records, load, { signal, onProgress = () => {}, concurrency = 4 }) {
  let next = 0, completed = 0
  await Promise.all(Array.from({ length: Math.min(concurrency, records.length) }, async () => {
    while (!signal.aborted && next < records.length) {
      const record = records[next++]
      try {
        const summary = await load({ reference: record.reference, summary: '1' }, signal)
        if (signal.aborted) return
        record.summary = summary
      } catch (error) {
        if (signal.aborted) return
        record.error = error.message
      }
      onProgress(++completed)
    }
  }))
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
  const query = dialog.querySelector('#setupBuildQuery')
  const status = dialog.querySelector('[data-status]')
  const warnings = dialog.querySelector('[data-warnings]')
  const cards = dialog.querySelector('[data-builds]')
  const more = dialog.querySelector('[data-more]')
  const events = new AbortController()
  const filter = dialog.querySelector('[data-filter]')
  const registry = dialog.querySelector('[data-registry]')
  const sort = dialog.querySelector('[data-sort]')
  const age = dialog.querySelector('[data-age]')
  const from = dialog.querySelector('[data-from]')
  const to = dialog.querySelector('[data-to]')
  const latest = dialog.querySelector('[data-latest-only]')
  const spinner = dialog.querySelector('[data-spinner]')
  const stop = dialog.querySelector('[data-stop]')
  const progress = dialog.querySelector('[data-progress]')
  const empty = dialog.querySelector('[data-empty]')
  let state
  let renderTimer
  let limit = 12
  let selection = ''

  const load = async (params, signal) => {
    const url = new URL(endpoint, window.location.href)
    url.search = new URLSearchParams({ ...params, token })
    const response = await fetch(url, { signal, credentials: 'same-origin', cache: 'no-store' })
    if (!response.ok) throw new Error((await response.text()).trim() || 'Lookup failed.')
    return response.json()
  }

  const inspectCard = async (record, evidence, current) => {
    const signal = current.details.signal
    evidence.innerHTML = '<p class="setup-build-detail-loading"><span class="setup-build-spinner" aria-hidden="true"></span>Reading chart versions, hashes and catalog evidence…</p>'
    evidence.setAttribute('aria-busy', 'true')
    try {
      const build = record.details || await load({ reference: record.reference }, signal)
      if (signal.aborted) return
      record.details = build
      record.summary = build
      record.card.querySelector('[data-built]').textContent = displayTime(build.created)
      record.card.querySelector('[data-version]').textContent = build.version || ''
      evidence.innerHTML = buildEvidence(build, escapeHtml)
      const use = document.createElement('button')
      use.type = 'button'
      use.textContent = build.moving ? 'Use moving tag' : 'Use this build'
      if (record.reference !== build.reference) {
        const pin = document.createElement('p')
        pin.textContent = 'Matching build tag · ' + build.reference.slice(build.reference.lastIndexOf(':') + 1)
        evidence.append(pin)
      }
      use.addEventListener('click', () => { selection = build.reference; dialog.close() }, { signal })
      evidence.append(use)
    } catch (error) {
      if (signal.aborted) return
      evidence.textContent = `Could not inspect this build. ${error.message} `
      const retry = document.createElement('button')
      retry.type = 'button'
      retry.textContent = 'Retry details'
      retry.addEventListener('click', () => inspectCard(record, evidence, current), { signal })
      evidence.append(retry)
    } finally {
      evidence.removeAttribute('aria-busy')
    }
  }

  const cardFor = record => {
    if (record.card) return record.card
    const { reference, summary } = record
    const current = state
    const card = document.createElement('article')
    const tag = reference.slice(reference.lastIndexOf(':') + 1)
    const repository = reference.slice(0, reference.lastIndexOf(':'))
    card.innerHTML = `<h3>${escapeHtml(tag)}</h3><code>${escapeHtml(repository)}</code>
      <p data-built class="setup-build-date">${escapeHtml(displayTime(summary.created))}</p>
      <p data-version>${escapeHtml(summary.version || '')}</p>
      <details data-detail><summary>Chart versions, hashes &amp; select build</summary><div data-evidence></div></details>`
    const details = card.querySelector('[data-detail]')
    let started = false
    details.addEventListener('toggle', () => {
      if (!details.open || started) return
      started = true
      inspectCard(record, card.querySelector('[data-evidence]'), current)
    }, { signal: current.details.signal })
    record.card = card
    return card
  }

  const render = () => {
    if (!state || events.signal.aborted) return
    const invalidDates = age.value === 'custom' && from.value && to.value && from.value > to.value
    const error = dialog.querySelector('[data-filter-error]')
    error.hidden = !invalidDates
    error.textContent = invalidDates ? 'The start date must be on or before the end date.' : ''
    const matches = invalidDates ? [] : selectBuilds(state.records, {
      text: filter.value, registry: registry.value, sort: sort.value, age: age.value,
      from: from.value, to: to.value, latest: latest.checked, now: state.now
    })
    const active = cards.contains(document.activeElement) ? document.activeElement : null
    cards.replaceChildren(...matches.slice(0, limit).map(cardFor))
    if (active?.isConnected) active.focus({ preventScroll: true })
    more.hidden = matches.length <= limit
    more.textContent = `Show more builds (${Math.max(0, matches.length - limit)} remaining)`
    const busy = state.busy
    spinner.hidden = !busy
    stop.hidden = !busy
    cards.setAttribute('aria-busy', String(busy))
    progress.hidden = !busy || !state.records.length
    progress.max = Math.max(1, state.records.length)
    progress.value = state.completed
    const failures = state.records.filter(record => record.error)
    const undated = state.records.filter(record => record.summary && !Number.isFinite(createdTime(record))).length
    const counts = `${matches.length} matching build${matches.length === 1 ? '' : 's'} · ${state.completed}/${state.records.length} build dates checked.`
    status.textContent = state.error || (state.discovering ? 'Searching Rancher registries for available tags…'
      : `${busy ? 'Reading image build dates… ' : state.stopped ? 'Search stopped. Partial results. ' : ''}${counts}${busy ? ' Results sort as dates arrive.' : ''}${undated ? ` ${undated} dates unavailable; choose All dates to include them.` : ''}${failures.length ? ` ${failures.length} lookups failed; search again to retry.` : ''}`)
    empty.hidden = matches.length > 0 || Boolean(state.error) || invalidDates
    empty.textContent = busy ? 'Matching builds will appear as their dates are verified.'
      : 'No builds match these filters. Try All dates, another registry, or a specific release line.'
    const notes = [...state.notes, ...failures.slice(0, 10).map(record => record.reference + ' — ' + record.error)]
    if (failures.length > 10) notes.push(`${failures.length - 10} more metadata lookups failed.`)
    warnings.hidden = !notes.length
    warnings.querySelector('ul').replaceChildren(...notes.map(note => {
      const li = document.createElement('li'); li.textContent = note; return li
    }))
  }

  const search = async () => {
    clearTimeout(renderTimer)
    renderTimer = undefined
    state?.request.abort()
    state?.details.abort()
    state = { request: new AbortController(), details: new AbortController(), records: [], notes: [], completed: 0, busy: true, discovering: true, now: Date.now() }
    const current = state
    const signal = current.request.signal
    limit = 12
    warnings.open = false
    dialog.querySelector('[data-heads]').replaceChildren()
    dialog.querySelector('[data-latest]').setAttribute('aria-pressed', String(!query.value.trim()))
    render()
    try {
      const groups = await load({ version: query.value.trim() || 'latest-heads', docker: docker ? '1' : '0' }, signal)
      if (signal.aborted) return
      for (const group of groups) {
        const source = group.registry + '/' + group.repository
        if (group.error) current.notes.push(source + ' — ' + group.error)
      }
      const references = buildReferences(groups)
      current.records = references.map(reference => ({ reference }))
      current.discovering = false
      const selectedRegistry = registry.value
      registry.replaceChildren(...['', ...new Set(groups.map(group => group.registry))].map(value => {
        const option = document.createElement('option'); option.value = value; option.textContent = value || 'All registries'; return option
      }))
      registry.value = selectedRegistry
      for (const selector of headSelectors(references).slice(0, 8)) {
        const button = document.createElement('button')
        button.type = 'button'; button.textContent = selector
        button.addEventListener('click', () => { query.value = selector; search() }, { signal: current.details.signal })
        dialog.querySelector('[data-heads]').append(button)
      }
      if (!references.length && groups.length && groups.every(group => group.error)) current.error = 'Registry lookup failed. Search again to retry.'
      render()
      await scanBuildSummaries(current.records, load, { signal, onProgress: completed => {
        current.completed = completed
        if (!renderTimer) renderTimer = setTimeout(() => { renderTimer = undefined; render() }, 100)
      } })
    } catch (error) {
      if (!signal.aborted) current.error = error.message
    } finally {
      if (state === current) {
        clearTimeout(renderTimer); renderTimer = undefined
        current.busy = false; current.discovering = false; render()
      }
    }
  }

  dialog.querySelector('[data-target]').textContent = docker
    ? `Choose a tag for ${label}. Its image repository will apply to all Linode rows.`
    : `Choose the server image for ${label}.`
  const seed = String(version || '').trim()
  const currentVersion = seed.includes('/') ? seed.slice(seed.lastIndexOf(':') + 1) : seed
  const currentButton = dialog.querySelector('[data-current]')
  currentButton.hidden = !currentVersion
  currentButton.textContent = `Current row: ${currentVersion}`
  query.value = ''; filter.value = ''; sort.value = 'newest'; age.value = '30'
  registry.value = ''; from.value = ''; to.value = ''; latest.checked = true
  dialog.querySelector('[data-dates]').hidden = true
  const options = { signal: events.signal }
  dialog.querySelector('form').addEventListener('submit', event => { event.preventDefault(); search() }, options)
  dialog.querySelector('[data-close]').addEventListener('click', () => dialog.close(), options)
  dialog.querySelector('[data-latest]').addEventListener('click', () => { query.value = ''; search() }, options)
  currentButton.addEventListener('click', () => { query.value = currentVersion; search() }, options)
  for (const control of [filter, registry, sort, age, from, to, latest]) {
    control.addEventListener('input', () => {
      dialog.querySelector('[data-dates]').hidden = age.value !== 'custom'
      limit = 12; render()
    }, options)
  }
  more.addEventListener('click', () => { limit += 12; render() }, options)
  stop.addEventListener('click', () => {
    state.stopped = true; state.busy = false; state.discovering = false
    state.request.abort(); render()
  }, options)
  return new Promise(resolve => {
    dialog.addEventListener('close', () => {
      events.abort()
      clearTimeout(renderTimer)
      state?.request.abort()
      state?.details.abort()
      cards.replaceChildren()
      resolve(selection)
    }, { once: true })
    dialog.showModal()
    query.focus()
    search()
  })
}
