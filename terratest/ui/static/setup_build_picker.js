// Interleave repositories so the first page can compare the same line across registries.
function buildReferences(groups) {
  const references = []
  const count = Math.max(0, ...groups.map(group => group.tags?.length || 0))
  for (let i = 0; i < count; i++) {
    for (const group of groups) {
      if (group.tags?.[i]) references.push(`${group.registry}/${group.repository}:${group.tags[i]}`)
    }
  }
  return references
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
  const query = dialog.querySelector('input')
  const status = dialog.querySelector('[data-status]')
  const warnings = dialog.querySelector('[data-warnings]')
  const cards = dialog.querySelector('[data-builds]')
  const more = dialog.querySelector('[data-more]')
  const events = new AbortController()
  let request
  let references = []
  let offset = 0
  let selection = ''

  const load = async (params, signal) => {
    const url = new URL(endpoint, window.location.href)
    url.search = new URLSearchParams({ ...params, token })
    const response = await fetch(url, { signal, credentials: 'same-origin', cache: 'no-store' })
    if (!response.ok) throw new Error((await response.text()).trim() || 'Lookup failed.')
    return response.json()
  }

  const inspectCard = async (reference, signal) => {
    const card = document.createElement('article')
    const tag = reference.slice(reference.lastIndexOf(':') + 1)
    const repository = reference.slice(0, reference.lastIndexOf(':'))
    card.innerHTML = `<h3>${escapeHtml(tag)}</h3><code>${escapeHtml(repository)}</code>
      <div data-evidence><p role="status">Reading build metadata…</p></div>`
    cards.append(card)
    const evidence = card.querySelector('[data-evidence]')
    try {
      const build = await load({ reference }, signal)
      if (signal.aborted) return
      evidence.innerHTML = buildEvidence(build, escapeHtml)
      const use = document.createElement('button')
      use.type = 'button'
      use.textContent = build.moving ? 'Use moving tag' : 'Use this build'
      if (reference !== build.reference) {
        const pin = document.createElement('p')
        pin.textContent = 'Matching build tag · ' + build.reference.slice(build.reference.lastIndexOf(':') + 1)
        evidence.append(pin)
      }
      use.addEventListener('click', () => {
        selection = build.reference
        dialog.close()
      }, { signal })
      evidence.append(use)
    } catch (error) {
      if (!signal.aborted) evidence.textContent = `Could not inspect this build. ${error.message}`
    }
  }

  const showMore = async () => {
    const signal = request.signal
    const next = references.slice(offset, offset + 4)
    offset += next.length
    more.hidden = offset >= references.length
    more.disabled = true
    await Promise.all(next.map(reference => inspectCard(reference, signal)))
    if (!signal.aborted) more.disabled = false
  }

  const search = async () => {
    request?.abort()
    request = new AbortController()
    const signal = request.signal
    cards.replaceChildren()
    more.hidden = true
    warnings.hidden = true
    warnings.open = false
    offset = 0
    status.textContent = 'Checking Rancher registries…'
    try {
      const groups = await load({ version: query.value.trim(), docker: docker ? '1' : '0' }, signal)
      if (signal.aborted) return
      const notes = []
      for (const group of groups) {
        const source = group.registry + '/' + group.repository
        if (group.error) notes.push(source + ' — ' + group.error)
        if (group.matched > group.tags?.length) notes.push(`${source} — Showing ${group.tags.length} of ${group.matched} tags. Enter a narrower version or commit tag to see others.`)
      }
      warnings.querySelector('ul').replaceChildren(...notes.map(note => {
        const li = document.createElement('li')
        li.textContent = note
        return li
      }))
      warnings.hidden = !notes.length
      references = buildReferences(groups)
      if (!references.length) {
        status.textContent = groups.length && groups.every(group => group.error) ? 'Registry lookup failed. Search again to retry.'
          : notes.length ? 'No matches in the repositories that responded.' : 'No matching Rancher builds.'
        return
      }
      status.textContent = `${references.length} matching tag${references.length === 1 ? '' : 's'}. Chart versions are build defaults.${docker ? '' : ' Plan review checks the Helm-selected assets.'}`
      await showMore()
    } catch (error) {
      if (!signal.aborted) status.textContent = error.message
    }
  }

  dialog.querySelector('[data-target]').textContent = docker
    ? `Choose a tag for ${label}. Its image repository will apply to all Linode rows.`
    : `Choose the server image for ${label}.`
  const seed = String(version || '').trim()
  query.value = seed.includes('/') ? seed.slice(seed.lastIndexOf(':') + 1) : seed
  const options = { signal: events.signal }
  dialog.querySelector('form').addEventListener('submit', event => { event.preventDefault(); search() }, options)
  dialog.querySelector('[data-close]').addEventListener('click', () => dialog.close(), options)
  more.addEventListener('click', showMore, options)
  return new Promise(resolve => {
    dialog.addEventListener('close', () => {
      events.abort()
      request?.abort()
      cards.replaceChildren()
      resolve(selection)
    }, { once: true })
    dialog.showModal()
    query.focus()
    search()
  })
}
