import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { setImmediate as tick } from 'node:timers/promises';
import { parseHTML } from 'linkedom';
import { buildDateLabel, filterIndexedBuilds, pickRancherBuild } from '../../static/setup_build_picker.js';

const template = readFileSync(new URL('../../templates/interactive_setup.html', import.meta.url), 'utf8');
const build = (line, sha, day) => {
  const version = `v${line}.0-${sha}-head`;
  return { line, version, webhook: 'v0.12.1', created: `2026-11-${day}`,
    reportPath: `reports/v${line}/${version}.md`, reference: `stgregistry.suse.com/rancher/rancher-server:${version}` };
};
const head = build('2.17', 'aaaaaaa', '18');
const otherLine = build('2.16', 'bbbbbbb', '17');
const older = build('2.17', 'ccccccc', '16');
const page = (builds, total = builds.length, nextOffset = builds.length) =>
  ({ builds, total, nextOffset, more: nextOffset < total });

function fixture(t, docker = false) {
  const { document, Event } = parseHTML(template);
  const dialog = document.querySelector('#setupBuildPicker');
  const get = selector => dialog.querySelector(selector);
  const requests = [];
  // The lightweight DOM has no layout, modal lifecycle, or select-value setter.
  Object.defineProperty(get('[data-age]'), 'value', { writable: true, value: '' });
  t.mock.method(document.defaultView.HTMLElement.prototype, 'focus', function () { document.activeElement = this; });
  document.defaultView.HTMLElement.prototype.scrollIntoView = () => {};
  dialog.showModal = () => { dialog.open = true; };
  dialog.close = () => { dialog.open = false; dialog.dispatchEvent(new Event('close')); };
  const previous = { document: globalThis.document, window: globalThis.window, fetch: globalThis.fetch };
  globalThis.document = document;
  globalThis.window = { location: { href: 'http://localhost/setup-editor' } };
  globalThis.fetch = (url, { signal }) => new Promise(resolve => requests.push({
    q: url.searchParams, signal,
    reply: data => resolve(Response.json(data)), fail: text => resolve(new Response(text, { status: 502 })),
  }));
  t.after(() => Object.assign(globalThis, previous));
  const open = () => pickRancherBuild(dialog, {
    endpoint: '/setup-editor/api/rancher-builds', token: t.name, docker,
    label: 'Rancher 1', version: '', escapeHtml: value => String(value).replaceAll('<', '&lt;'),
  });
  return { dialog, get, open, requests, Event };
}

test('overview filtering combines words and uses UTC calendar dates', () => {
  assert.deepEqual(filterIndexedBuilds([head, otherLine], 'SUSe aaa v0.12'), [head]);
  assert.deepEqual(filterIndexedBuilds([head, otherLine], 'absent'), []);
  assert.equal(buildDateLabel(head, Date.parse('2026-11-18T00:00:01Z')), 'Today');
  assert.equal(buildDateLabel(otherLine, Date.parse('2026-11-18T23:59:59Z')), 'Yesterday');
});

test('only the opened line loads history and only the previewed build can be applied', async t => {
  const { get, open, requests } = fixture(t);
  const picked = open();
  assert.equal(requests[0].q.get('days'), '30');
  requests[0].reply(page([head, otherLine]));
  await tick();
  assert.equal(requests.length, 1);
  const toggle = get('.setup-build-toggle');
  const panel = get('#' + toggle.getAttribute('aria-controls'));
  assert.equal(panel.hidden, true);
  toggle.click();
  assert.equal(toggle.getAttribute('aria-expanded'), 'true');
  assert.equal(panel.hidden, false);
  assert.equal(panel.getAttribute('aria-labelledby'), toggle.id);
  assert.equal(requests[1].q.get('line'), head.line);
  assert.equal(requests[1].q.get('exclude'), head.version);
  assert.equal(requests[1].q.get('limit'), '5');
  assert.equal(requests[2].q.get('report'), head.reportPath);
  assert.equal(get('[data-use]').disabled, true);
  requests[1].reply(page([older]));
  await tick();
  assert.deepEqual([...panel.querySelectorAll('time')].map(time => time.textContent), [older.created]);
  const row = panel.querySelector('li button');
  row.click();
  assert.equal(row.getAttribute('aria-current'), 'true');
  assert.equal(document.activeElement, get('[data-review-title]'));
  assert.equal(get('[data-review-title]').textContent, 'Selected build');
  assert.equal(requests[2].signal.aborted, true);
  assert.equal(requests[3].q.get('report'), older.reportPath);
  requests[2].reply(head);
  await tick();
  assert.equal(get('[data-use]').disabled, true);
  requests[3].reply({ ...older, charts: { fleet: '105+up1' }, catalogVersions: { fleet: ['older'] } });
  await tick();
  assert.match(get('[data-evidence]').textContent, /105\+up1Missing from/);
  assert.equal(get('[data-reference]').textContent, older.reference);
  get('[data-use]').click();
  assert.equal(await picked, older.reference);
});

test('paging keeps focus and line history stays usable after a verification failure', async t => {
  const { dialog, get, open, requests, Event } = fixture(t);
  const picked = open();
  requests[0].reply(page([head, otherLine]));
  await tick();
  get('.setup-build-toggle').click();
  const dates = ['17', '16', '15', '14', '13', '12'];
  const history = dates.map(day => build('2.17', 'abcde' + day, day));
  requests[1].reply(page(history.slice(0, 5), 6));
  requests[2].fail('Registry offline');
  await tick();
  assert.equal(get('[data-use]').disabled, true);
  assert.match(get('[data-detail-error]').textContent, /Registry offline/);
  const more = get('[data-more]');
  more.focus();
  more.click();
  assert.equal(requests[3].q.get('offset'), '5');
  requests[3].reply(page(history.slice(5), 6, 6));
  await tick();
  const rows = [...get('[data-history-list]').querySelectorAll('li button')];
  assert.equal(rows.length, 6);
  assert.equal(document.activeElement, rows.at(-1));
  assert.equal(more.hidden, true);
  rows.at(-1).click();
  get('[data-latest]').click();
  assert.equal(requests[4].signal.aborted, true);
  assert.equal(requests[5].q.get('report'), head.reportPath);
  assert.equal(get('[data-history-list]').querySelector('[aria-current]'), null);
  const filter = get('[data-history-filter] input');
  filter.value = 'old-webhook';
  filter.dispatchEvent(new Event('input'));
  await new Promise(resolve => setTimeout(resolve, 200));
  assert.equal(requests[6].q.get('q'), 'old-webhook');
  const toggles = dialog.querySelectorAll('.setup-build-toggle');
  toggles[1].click();
  assert.equal(toggles[0].getAttribute('aria-expanded'), 'false');
  assert.equal(requests[5].signal.aborted, true);
  assert.equal(requests[6].signal.aborted, true);
  requests[6].reply(page(history));
  requests[7].reply(page([]));
  await tick();
  assert.match(get('[data-history-status]').textContent, /No other builds/);
  toggles[1].click();
  assert.equal(requests[8].signal.aborted, true);
  dialog.close();
  assert.equal(await picked, '');
});

test('exact image verification supports moving tags and enforces Docker compatibility', async t => {
  const { dialog, get, open, requests, Event } = fixture(t, true);
  const picked = open();
  requests[0].reply(page([]));
  await tick();
  get('[data-direct]').open = true;
  get('[data-direct-image]').value = head.reference;
  get('[data-direct-form]').dispatchEvent(new Event('submit'));
  requests[1].reply(head);
  await tick();
  assert.equal(get('[data-use]').disabled, true);
  assert.match(get('[data-detail-error-message]').textContent, /Linode Docker/);
  const reference = 'docker.io/rancher/rancher:v2.17-head';
  get('[data-direct-image]').value = reference;
  get('[data-direct-form]').dispatchEvent(new Event('submit'));
  assert.equal(requests[2].q.get('reference'), reference);
  requests[2].reply({ reference, moving: true });
  await tick();
  assert.equal(get('[data-use]').textContent, 'Use moving tag');
  get('[data-use]').click();
  assert.equal(await picked, reference);
  assert.equal(dialog.open, false);
});

test('closing cancels pending discovery and reopening ignores the late response', async t => {
  const { dialog, get, open, requests } = fixture(t);
  const first = open();
  dialog.close();
  assert.equal(await first, '');
  const second = open();
  assert.equal(requests[0].signal.aborted, true);
  requests[0].reply(page([head]));
  await tick();
  assert.equal(get('[data-builds]').children.length, 0);
  requests[1].reply(page([otherLine]));
  await tick();
  assert.equal(get('[data-line]').textContent, otherLine.line);
  dialog.close();
  assert.equal(await second, '');
});

test('returning to a verified head reuses it and refresh checks the open line again', async t => {
  const { dialog, get, open, requests } = fixture(t);
  const picked = open();
  requests[0].reply(page([head]));
  await tick();
  get('.setup-build-toggle').click();
  requests[1].reply(page([older]));
  requests[2].reply(head);
  await tick();
  get('[data-history-list] button').click();
  get('[data-latest]').click();
  await tick();
  assert.equal(requests.length, 4);
  assert.equal(requests[3].signal.aborted, true);
  assert.equal(get('[data-use]').disabled, false);
  get('[data-refresh]').click();
  requests[4].reply(page([head]));
  await tick();
  assert.equal(requests[5].q.get('refresh'), '1');
  assert.equal(requests[6].q.get('report'), head.reportPath);
  assert.equal(get('[data-use]').disabled, true);
  requests[5].fail('History unavailable');
  requests[6].reply(head);
  await tick();
  assert.equal(get('[data-use]').disabled, false);
  assert.match(get('[data-history-error]').textContent, /History unavailable/);
  get('[data-history-error] button').click();
  requests[7].reply(page([]));
  await tick();
  assert.match(get('[data-history-status]').textContent, /No other builds/);
  dialog.close();
  assert.equal(await picked, '');
});
