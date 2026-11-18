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
  const chartRows = Object.entries(build.charts || {}).map(([chart, version]) => {
    const catalog = build.catalogVersions?.[chart]
    const match = version && catalog ? catalog.includes(version) : undefined
    const evidence = match === undefined ? '' : `<small class="${match ? '' : 'setup-chart-missing'}">${match ? 'In' : 'Missing from'} ${catalogLabel}</small>`
    return `<div><dt>${escapeHtml(chart)}</dt><dd><code>${escapeHtml(version || 'Unavailable')}</code>${evidence}</dd></div>`
  }).join('')
  const catalog = Object.entries(build.catalogVersions || {}).map(([chart, versions]) =>
    field(escapeHtml(chart), versions.join(', ') || 'No versions in index')
  ).join('')
  return `${chartRows ? `<h6>System charts</h6><dl class="setup-chart-facts">${chartRows}</dl>` : ''}
    ${build.catalogStatus ? `<p>${escapeHtml(build.catalogStatus)}</p>` : ''}
    <details><summary>Image and catalog details</summary><dl class="setup-build-facts">
    ${field('Build version', build.version)}
    ${field('Image digest', build.digest)}${field('Source commit', build.revision)}
    ${build.ossRevision ? field('OSS commit', build.ossRevision) : ''}${field('Platform', build.platform)}
    ${build.catalogImage ? field('Catalog image', build.catalogImage) : ''}
    ${build.chartRevision ? field('Charts commit', build.chartRevision) : ''}
    </dl>
    ${catalog ? `<h6>Versions in this catalog</h6><dl class="setup-build-facts">${catalog}</dl>` : ''}</details>`
}

const historyPageSize = 5

function mountHistory(section, { head, urlFor, days, refresh, signal, onPreview }) {
  const list = section.querySelector('ol')
  const status = section.querySelector('[data-history-status]')
  const filter = section.querySelector('label')
  const search = filter.querySelector('input')
  const more = section.querySelector('[data-more]')
  const error = section.querySelector('[data-history-error]')
  section.querySelector('[data-history-title]').textContent = `Other builds in ${head.line}`
  let request, timer, nextOffset = 0, viewing = head.version
  const rows = new Map()

  const markViewing = version => {
    viewing = version
    for (const [tag, row] of rows) {
      if (tag === version) row.setAttribute('aria-current', 'true')
      else row.removeAttribute('aria-current')
      row.querySelector('[data-action]').textContent = tag === version ? 'Viewing' : 'View'
    }
  }

  const appendBuild = build => {
    if (rows.has(build.version) || build.version === head.version) return
    const item = document.createElement('li')
    item.innerHTML = `<button type="button" class="setup-history-row">
      <time></time><code></code><span data-webhook></span><span data-action></span></button>`
    const button = item.querySelector('button')
    const date = item.querySelector('time')
    date.dateTime = build.created
    // The history index reports calendar days, without a time of day.
    date.textContent = build.created.slice(0, 10)
    item.querySelector('code').textContent = build.version
    item.querySelector('[data-webhook]').textContent = build.webhook ? `Webhook ${build.webhook}` : 'Webhook unavailable'
    button.addEventListener('click', () => onPreview(build))
    rows.set(build.version, button)
    list.append(item)
  }

  const load = async (offset, force = false) => {
    request?.abort()
    const current = request = new AbortController()
    error.hidden = true
    if (!offset) more.hidden = true
    more.setAttribute('aria-disabled', 'true')
    list.setAttribute('aria-busy', 'true')
    status.textContent = offset ? 'Loading older builds…' : 'Loading build history…'
    if (!offset) { rows.clear(); list.replaceChildren() }
    try {
      const data = await loadBuildIndex(urlFor({
        catalog: '1', days, line: head.line, exclude: head.version, limit: historyPageSize,
        offset, q: search.value.trim(),
      }), current.signal, force)
      if (current.signal.aborted || signal.aborted) return
      const previousCount = rows.size
      data.builds.forEach(appendBuild)
      markViewing(viewing)
      nextOffset = data.nextOffset
      if (data.more) more.textContent = `Show ${Math.min(historyPageSize, data.total - nextOffset)} more`
      if (data.total > historyPageSize) filter.hidden = false
      const count = data.more ? `${rows.size} of ${data.total}` : String(rows.size)
      status.textContent = rows.size ? `${count} ${data.total === 1 ? 'build' : 'builds'} · Newest first`
        : search.value.trim() ? 'No matching builds in this line.' : 'No other builds in this date range.'
      if (data.stale) status.textContent += ' · Showing saved history'
      // Continue keyboard traversal at the first newly revealed row.
      if (offset && document.activeElement === more) list.children[previousCount]?.querySelector('button').focus()
      more.hidden = !data.more
    } catch (cause) {
      if (current.signal.aborted || signal.aborted) return
      status.textContent = rows.size ? `${rows.size} builds shown` : ''
      error.querySelector('p').textContent = cause.message
      more.hidden = true
      error.hidden = false
      error.querySelector('button').onclick = () => load(offset, true)
    } finally {
      if (request === current && !signal.aborted) {
        list.setAttribute('aria-busy', 'false')
        more.removeAttribute('aria-disabled')
      }
    }
  }

  more.addEventListener('click', () => {
    if (!more.hasAttribute('aria-disabled')) load(nextOffset)
  })
  search.addEventListener('input', () => {
    request?.abort()
    clearTimeout(timer)
    more.hidden = true
    timer = setTimeout(() => load(0), 180)
  })
  signal.addEventListener('abort', () => { request?.abort(); clearTimeout(timer) }, { once: true })
  load(0, refresh)
  return markViewing
}

export function pickRancherBuild(dialog, { endpoint, token, docker, label, version, escapeHtml }) {
  const get = selector => dialog.querySelector(selector)
  const clone = selector => get(selector).content.firstElementChild.cloneNode(true)
  const search = get('[data-search]'), age = get('[data-age]'), list = get('[data-builds]')
  const status = get('[data-status]'), direct = get('[data-direct]'), directPreview = get('[data-direct-preview]')
  const events = new AbortController(), options = { signal: events.signal }
  // Only immutable reported images can be reused within this picker session.
  const verifiedReports = new Map()
  let request, detailRequest, expanded, selection = ''
  let cards = []

  const urlFor = params => {
    const url = new URL(endpoint, window.location.href)
    url.search = new URLSearchParams({ ...params, token })
    return url
  }
  const ageText = () => age.value === '0' ? 'All dates' : `Last ${age.value} days`

  const verifyBuild = async (build, signal) => {
    const cached = verifiedReports.get(build.reportPath)
    if (cached && Date.now() - cached.at < indexCacheTTL) return cached.build

    const params = build.reportPath ? { report: build.reportPath, docker: docker ? '1' : '0' } : { reference: build.reference }
    const response = await fetch(urlFor(params), { signal, credentials: 'same-origin', cache: 'no-store' })
    if (!response.ok) throw new Error((await response.text()).trim() || 'Could not verify this image.')
    const verified = await response.json()
    if (!verified.reference) throw new Error('The registry did not return an image reference.')
    if (docker && !/\/rancher\/rancher:/.test(verified.reference)) throw new Error('Choose a full rancher/rancher image for Linode Docker.')
    if (build.reportPath && !verified.moving && !signal.aborted) {
      verifiedReports.set(build.reportPath, { build: verified, at: Date.now() })
    }
    return verified
  }

  const showReview = (host, build, title, { focus = false, backToLatest } = {}) => {
    detailRequest?.abort()
    const current = detailRequest = new AbortController()
    const review = clone('[data-review-template]')
    const field = selector => review.querySelector(selector)
    const heading = field('[data-review-title]'), use = field('[data-use]')
    const status = field('[data-detail-status]'), error = field('[data-detail-error]')
    const showDate = value => {
      const date = Date.parse(value || '')
      const built = Number.isFinite(date) ? new Date(date).toISOString().replace('T', ' ').slice(0, 19) + ' UTC' : ''
      field('[data-preview-meta]').textContent = [
        built && `Built ${value.length === 10 ? value : built}`, build.webhook && `Webhook ${build.webhook}`,
      ].filter(Boolean).join(' · ')
    }
    heading.textContent = title
    showDate(build.created)
    field('[data-reference]').textContent = build.reference || build.version
    status.textContent = 'Verifying image and chart versions…'
    if (build.reportURL) {
      const link = field('[data-report-link]')
      link.href = build.reportURL
      link.hidden = false
    }
    if (backToLatest) {
      const back = field('[data-latest]')
      back.hidden = false
      back.onclick = backToLatest
    }
    host.replaceChildren(review)
    if (focus) {
      heading.focus({ preventScroll: true })
      heading.scrollIntoView({ block: 'nearest' })
    }

    const verify = async () => {
      try {
        const verified = await verifyBuild(build, current.signal)
        if (current.signal.aborted) return
        showDate(verified.created || build.created)
        field('[data-reference]').textContent = verified.reference
        field('[data-evidence]').innerHTML = buildEvidence(verified, escapeHtml)
        status.textContent = 'Image verified'
        status.dataset.verified = 'true'
        use.textContent = verified.moving ? 'Use moving tag' : 'Use this build'
        use.disabled = false
        use.onclick = () => {
          if (current.signal.aborted) return
          selection = verified.reference
          dialog.close()
        }
      } catch (cause) {
        if (current.signal.aborted) return
        status.textContent = ''
        field('[data-detail-error-message]').textContent = cause.message
        error.hidden = false
        field('[data-detail-retry]').onclick = () => showReview(host, build, title, { focus: true, backToLatest })
      }
    }
    verify()
  }

  const collapse = () => {
    if (!expanded) return
    detailRequest?.abort()
    expanded.controller.abort()
    expanded.toggle.setAttribute('aria-expanded', 'false')
    expanded.panel.hidden = true
    expanded.panel.replaceChildren()
    expanded = null
  }

  const openLine = (card, refresh = false) => {
    collapse()
    direct.open = false
    directPreview.replaceChildren()
    const { head, toggle, panel } = card
    const controller = new AbortController()
    expanded = { ...card, controller }
    toggle.setAttribute('aria-expanded', 'true')
    const preview = document.createElement('div')
    const history = clone('[data-history-template]')
    panel.replaceChildren(preview, history)
    panel.hidden = false
    const previewBuild = (build, focus = true) => {
      const latest = build.version === head.version
      markViewing(build.version)
      showReview(preview, build, latest ? 'Latest build' : 'Selected build', {
        focus, backToLatest: latest ? undefined : () => previewBuild(head),
      })
    }
    const markViewing = mountHistory(history, {
      head, urlFor, days: age.value, refresh, signal: controller.signal, onPreview: previewBuild,
    })
    previewBuild(head, false)
  }

  const makeCard = (head, index) => {
    const row = clone('[data-line-template]')
    const toggle = row.querySelector('button'), panel = row.querySelector('[data-panel]')
    toggle.id = `setup-line-${index}`
    panel.id = `${toggle.id}-builds`
    toggle.setAttribute('aria-controls', panel.id)
    panel.setAttribute('aria-labelledby', toggle.id)
    row.querySelector('[data-line]').textContent = head.line
    row.querySelector('[data-version]').textContent = head.version
    const date = row.querySelector('[data-built]')
    date.textContent = `Built ${buildDateLabel(head)}`
    date.title = head.created
    row.querySelector('[data-webhook]').textContent = head.webhook || 'Not reported'
    const card = { head, row, toggle, panel }
    toggle.onclick = () => {
      toggle.focus({ preventScroll: true })
      if (expanded?.toggle === toggle) collapse()
      else {
        openLine(card)
        const viewport = get('[data-browse]').getBoundingClientRect()
        const header = toggle.getBoundingClientRect()
        if (header.top < viewport.top || viewport.bottom - header.bottom < 160) {
          toggle.scrollIntoView({ block: 'start' })
        }
      }
    }
    return card
  }

  const filterCards = () => {
    const matches = new Set(filterIndexedBuilds(cards.map(card => card.head), search.value))
    for (const { head, row, toggle } of cards) {
      row.hidden = !matches.has(head)
      if (row.hidden && toggle === expanded?.toggle) collapse()
    }
    status.textContent = `${matches.size} ${matches.size === 1 ? 'line' : 'lines'} · SUSE staging · ${ageText()}`
    const empty = get('[data-empty]')
    empty.hidden = matches.size > 0
    empty.textContent = search.value.trim() ? 'No matching release lines. Try another version or webhook.'
      : 'No builds reported in this date range. Try All dates or inspect an exact image below.'
  }

  const loadIndex = async (refresh = false) => {
    if (refresh) {
      indexCache.clear()
      verifiedReports.clear()
    }
    const openLineName = expanded?.head.line
    request?.abort()
    const current = request = new AbortController()
    collapse()
    cards = []
    list.replaceChildren()
    list.setAttribute('aria-busy', 'true')
    get('[data-refresh]').disabled = true
    for (const selector of ['[data-error]', '[data-warnings]', '[data-empty]']) get(selector).hidden = true
    get('[data-index-note]').textContent = ''
    get('[data-skeleton]').hidden = false
    get('[data-spinner]').hidden = false
    status.textContent = 'Loading release lines…'
    try {
      const data = await loadBuildIndex(urlFor({ catalog: '1', days: age.value }), current.signal, refresh)
      if (current.signal.aborted) return
      cards = data.builds.map(makeCard)
      list.replaceChildren(...cards.map(card => card.row))
      filterCards()
      const previous = cards.find(card => card.head.line === openLineName && !card.row.hidden)
      if (previous && !direct.open) openLine(previous, refresh)
      get('[data-index-note]').textContent = data.stale
        ? 'Using a saved index because its source is unavailable. Refresh to try again.'
        : 'Build dates and webhook versions come from Webhook Ranch Hand.'
      const warnings = get('[data-warnings]')
      warnings.hidden = !data.warnings?.length
      warnings.querySelector('ul').replaceChildren(...(data.warnings || []).map(message => {
        const item = document.createElement('li'); item.textContent = message; return item
      }))
    } catch (cause) {
      if (current.signal.aborted) return
      status.textContent = 'Release lines unavailable'
      get('[data-error-message]').textContent = cause.message
      get('[data-error]').hidden = false
    } finally {
      if (!current.signal.aborted) {
        list.setAttribute('aria-busy', 'false')
        get('[data-refresh]').disabled = false
        get('[data-skeleton]').hidden = true
        get('[data-spinner]').hidden = true
      }
    }
  }

  get('[data-target]').textContent = `Choose a build for ${label}.${docker ? ' Standalone Docker availability is checked when you open a build.' : ''}`
  age.value = '30'; search.value = ''
  get('[data-direct-image]').value = String(version || '').includes('/') ? version : ''
  direct.open = false
  get('[data-close]').addEventListener('click', () => dialog.close(), options)
  get('[data-refresh]').addEventListener('click', () => loadIndex(true), options)
  get('[data-retry]').addEventListener('click', () => loadIndex(true), options)
  age.addEventListener('change', () => loadIndex(), options)
  search.addEventListener('input', () => {
    if (list.getAttribute('aria-busy') === 'false' && get('[data-error]').hidden) filterCards()
  }, options)
  const clearDirectPreview = () => {
    if (!directPreview.firstElementChild) return
    detailRequest?.abort()
    directPreview.replaceChildren()
  }
  direct.addEventListener('toggle', () => {
    if (direct.open) collapse()
    else clearDirectPreview()
  }, options)
  get('[data-direct-image]').addEventListener('input', clearDirectPreview, options)
  get('[data-direct-form]').addEventListener('submit', event => {
    event.preventDefault()
    const reference = get('[data-direct-image]').value.trim()
    if (reference) {
      collapse()
      showReview(directPreview, { reference }, 'Image details', { focus: true })
    }
  }, options)
  return new Promise(resolve => {
    dialog.addEventListener('close', () => {
      events.abort(); request?.abort(); detailRequest?.abort(); collapse()
      list.replaceChildren(); directPreview.replaceChildren()
      resolve(selection)
    }, { once: true })
    dialog.showModal(); search.focus(); loadIndex()
  })
}
