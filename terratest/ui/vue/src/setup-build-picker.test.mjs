import test from 'node:test';
import assert from 'node:assert/strict';
import { buildReferences, headSelectors, selectBuilds, scanBuildSummaries, pickRancherBuild } from '../../static/setup_build_picker.js';

const now = Date.parse('2026-11-18T12:00:00Z');
const record = (tag, created, registry = 'stgregistry.suse.com', repository = 'rancher/rancher-server') => ({
  reference: `${registry}/${repository}:${tag}`, summary: { created, version: tag },
});

test('newest uses image timestamps, not SHA order, and keeps registries distinct', () => {
  const older = record('v2.17.0-fffffff-head', '2026-11-16T10:00:00Z');
  const newer = record('v2.17.0-0000000-head', '2026-11-17T10:00:00Z');
  const alias = record('v2.17.0-head', '2026-11-15T10:00:00Z');
  const docker = record('v2.17.0-head', '2026-11-17T09:00:00Z', 'docker.io');
  const legacy = record('v2.17.0-head', '2026-11-17T08:00:00Z', 'stgregistry.suse.com', 'rancher/rancher');
  assert.deepEqual(selectBuilds([older, alias, newer, docker, legacy], { now }), [newer, docker, legacy]);
  assert.deepEqual(selectBuilds([older, alias, newer], { now, latest: false }), [newer, older, alias]);
});

test('version sorting is numeric and date sorting still puts a fresher older line first', () => {
  const v9 = record('v2.9.0-head', '2026-11-17T12:00:00Z');
  const v17 = record('v2.17.0-head', '2026-11-16T12:00:00Z');
  const v16 = record('v2.16.0-head', '2026-11-15T12:00:00Z');
  assert.deepEqual(selectBuilds([v9, v16, v17], { sort: 'version', now }), [v17, v16, v9]);
  assert.deepEqual(selectBuilds([v9, v16, v17], { now }), [v9, v17, v16]);
  assert.deepEqual(selectBuilds([v9, v16, v17], { sort: 'oldest', now }), [v16, v17, v9]);
  assert.deepEqual(headSelectors([v9.reference, v17.reference, v16.reference, v17.reference, 'x:v2.18.1-head']), ['2.17.0-head', '2.16.0-head', '2.9.0-head']);
});

test('default 30-day and inclusive UTC custom dates exclude unknown dates', () => {
  const old = record('v2.15.0-head', '2026-10-18T12:00:00Z');
  const start = record('v2.16.0-head', '2026-11-17T00:00:00Z');
  const end = record('v2.17.0-head', '2026-11-18T01:59:59+02:00');
  const outside = record('v2.18.0-head', '2026-11-18T00:00:00Z');
  const unknown = record('v2.19.0-head', '');
  const data = [old, start, end, outside, unknown];
  assert.deepEqual(selectBuilds(data, { now }), [outside, end, start]);
  assert.deepEqual(selectBuilds(data, { now, age: 'custom', from: '2026-11-17', to: '2026-11-17' }), [end, start]);
  for (const sort of ['newest', 'oldest']) {
    const all = selectBuilds(data, { now, age: 'all', sort });
    assert.equal(all.length, 5);
    assert.equal(all.at(-1), unknown);
  }
});

test('filters combine registry and case-insensitive words across hashes and inspected charts', () => {
  const build = record('v2.17.0-head', '2026-11-17T00:00:00Z');
  build.summary.revision = 'abcd1234';
  build.summary.digest = 'sha256:beefbeef';
  build.details = { charts: { 'rancher-webhook': '105.0.1' } };
  const data = [build, record('v2.16.0-head', '2026-11-17T00:00:00Z', 'docker.io')];
  assert.deepEqual(selectBuilds(data, { now, registry: 'stgregistry.suse.com', text: 'ABCD 105.0.1 BEEF' }), [build]);
  assert.deepEqual(selectBuilds(data, { now, registry: 'docker.io', text: 'ABCD' }), []);
});

test('registry interleaving deduplicates references without losing a source', () => {
  assert.deepEqual(buildReferences([
    { registry: 'stg', repository: 'rancher/rancher', tags: ['a', 'a', 'b'] },
    { registry: 'docker.io', repository: 'rancher/rancher', tags: ['a', 'b'] },
  ]), ['stg/rancher/rancher:a', 'docker.io/rancher/rancher:a', 'docker.io/rancher/rancher:b', 'stg/rancher/rancher:b']);
});

test('metadata scans bound concurrency and keep progress after a registry failure', async () => {
  const records = Array.from({ length: 11 }, (_, i) => ({ reference: String(i) }));
  let active = 0, maximum = 0;
  const progress = [];
  await scanBuildSummaries(records, async ({ reference, summary }) => {
    assert.equal(summary, '1');
    maximum = Math.max(maximum, ++active);
    await new Promise(resolve => setImmediate(resolve));
    active--;
    if (reference === '3') throw new Error('Registry unavailable');
    return { created: '2026-11-17T12:00:00Z' };
  }, { signal: new AbortController().signal, onProgress: count => progress.push(count) });
  assert.equal(maximum, 4);
  assert.equal(progress.at(-1), 11);
  assert.equal(records[3].error, 'Registry unavailable');
  assert.equal(records.filter(record => record.summary).length, 10);
});

test('cancelled scans stop scheduling and cannot add late metadata to a new search', async () => {
  const controller = new AbortController();
  const records = Array.from({ length: 8 }, (_, i) => ({ reference: String(i) }));
  const pending = [];
  let count = 0;
  const scan = scanBuildSummaries(records, () => new Promise(resolve => pending.push(resolve)), {
    signal: controller.signal, onProgress: () => count++, concurrency: 2,
  });
  assert.equal(pending.length, 2);
  controller.abort();
  pending.forEach(resolve => resolve({ created: '2026-11-17T12:00:00Z' }));
  await scan;
  assert.equal(count, 0);
  assert.equal(pending.length, 2);
  assert.ok(records.every(record => !record.summary));
});

// Exercise async dialog state without a live registry or a browser process.
function dialogFixture(t) {
  class Element extends EventTarget {
    value = ''; hidden = false; textContent = ''; children = []; fields = new Map(); attributes = new Map();
    querySelector(selector) {
      if (!this.fields.has(selector)) this.fields.set(selector, new Element());
      return this.fields.get(selector);
    }
    replaceChildren(...children) { this.children = children; }
    append(child) { this.children.push(child); }
    contains() { return false; }
    setAttribute(name, value) { this.attributes.set(name, value); }
    removeAttribute(name) { this.attributes.delete(name); }
    focus() {}
    showModal() { this.open = true; }
    close() { this.open = false; this.dispatchEvent(new Event('close')); }
  }
  const previous = { document: globalThis.document, window: globalThis.window, fetch: globalThis.fetch };
  const requests = [];
  globalThis.document = { createElement: () => new Element() };
  globalThis.window = { location: { href: 'http://localhost/setup' } };
  globalThis.fetch = (url, options) => new Promise(resolve => requests.push({
    url, signal: options.signal, reply: data => resolve({ ok: true, json: async () => data }),
  }));
  t.after(() => Object.assign(globalThis, previous));
  const dialog = new Element();
  const open = () => pickRancherBuild(dialog, {
    endpoint: '/setup-editor/api/rancher-builds', token: 'fixture', docker: false,
    label: 'Rancher 1', version: '2.16.0-head', escapeHtml: value => value,
  });
  return { dialog, requests, open };
}
const tick = () => new Promise(resolve => setImmediate(resolve));

test('dialog shows discovery and date progress, then loads chart details before selecting', async t => {
  const { dialog, requests, open } = dialogFixture(t);
  const picked = open();
  assert.equal(requests[0].url.searchParams.get('version'), 'latest-heads');
  assert.equal(requests[0].url.searchParams.get('token'), 'fixture');
  assert.equal(dialog.querySelector('[data-age]').value, '30');
  assert.equal(dialog.querySelector('[data-spinner]').hidden, false);
  assert.match(dialog.querySelector('[data-status]').textContent, /Searching Rancher registries/);
  requests[0].reply([{ registry: 'stgregistry.suse.com', repository: 'rancher/rancher-server', tags: ['v2.17.0-head'] }]);
  await tick();
  assert.equal(requests[1].url.searchParams.get('summary'), '1');
  assert.equal(dialog.querySelector('[data-progress]').hidden, false);
  requests[1].reply({ created: new Date().toISOString(), version: 'v2.17.0-abcdef0-head' });
  await tick();
  assert.equal(dialog.querySelector('[data-spinner]').hidden, true);
  assert.match(dialog.querySelector('[data-status]').textContent, /1\/1 build dates checked/);
  const card = dialog.querySelector('[data-builds]').children[0];
  const details = card.querySelector('[data-detail]');
  details.open = true; details.dispatchEvent(new Event('toggle'));
  assert.equal(card.querySelector('[data-evidence]').attributes.get('aria-busy'), 'true');
  assert.equal(requests[2].url.searchParams.has('summary'), false);
  const reference = 'stgregistry.suse.com/rancher/rancher-server:v2.17.0-abcdef0-head';
  requests[2].reply({ reference, created: new Date().toISOString(), version: 'v2.17.0-abcdef0-head', charts: { 'rancher-webhook': '105.0.1' } });
  await tick();
  const evidence = card.querySelector('[data-evidence]');
  assert.match(evidence.innerHTML, /105.0.1/);
  evidence.children.at(-1).dispatchEvent(new Event('click'));
  assert.equal(await picked, reference);
});

test('closing and reopening aborts old requests and prevents stale loading state', async t => {
  const { dialog, requests, open } = dialogFixture(t);
  const first = open();
  dialog.close();
  assert.equal(await first, '');
  assert.equal(requests[0].signal.aborted, true);
  const second = open();
  requests[0].reply([]);
  await tick();
  assert.equal(dialog.querySelector('[data-spinner]').hidden, false);
  assert.match(dialog.querySelector('[data-status]').textContent, /Searching Rancher registries/);
  dialog.querySelector('[data-stop]').dispatchEvent(new Event('click'));
  assert.equal(requests[1].signal.aborted, true);
  assert.equal(dialog.querySelector('[data-spinner]').hidden, true);
  assert.match(dialog.querySelector('[data-status]').textContent, /Search stopped/);
  requests[1].reply([]);
  await tick();
  dialog.close();
  assert.equal(await second, '');
});
