import test from 'node:test';
import assert from 'node:assert/strict';
import { releaseTargets, headTargets, defaultHeadChart, upgradeSearchHints } from './upgrade-targets.mjs';
const chart = version => ({ version, appVersion: `v${version}` });
const image = tag => `docker.io/rancher/rancher:${tag}`;

test('suggests the latest current patch before a minor upgrade and explains blocked targets', () => {
  const targets = releaseTargets('v2.15.2', ['2.16.2', '2.15.3', '2.15.10', '2.17.0', '2.14.9', '2.15.2'].map(chart));
  assert.deepEqual(targets.map(t => t.version), ['2.15.10', '2.15.3', '2.16.2']);
  assert.equal(targets[0].recommended, true);
  assert.equal(targets[2].blocked, 'Install v2.15.10 first');
});
test('allows latest next minor when no current patch remains', () => {
  const targets = releaseTargets('v2.15.10', ['2.16.1', '2.16.2', '2.15.10'].map(chart));
  assert.equal(targets[0].version, '2.16.2');
  assert.equal(targets[0].blocked, '');
  assert.equal(targets[1].blocked, 'Use v2.16.2 for this minor');
});
test('prerelease search supports rc10 ordering and keeps experimental choices available', () => {
  const targets = releaseTargets('v2.15.2', ['2.16.0-rc2', '2.16.0-rc10', '2.15.3-rc1'].map(chart), true);
  assert.equal(targets[0].version, '2.16.0-rc10');
  assert.ok(targets.every(t => !t.blocked && !t.recommended));
});
test('suggested heads contain global, patch and minor aliases, never old versions or build hashes', () => {
  const tags = ['v2.15.3-head-abc123', 'v2.14-head', 'v2.15.1-head', 'v2.17.0-head', 'v2.16-head', 'v2.15-head', 'v2.15.3-head', 'head'];
  const targets = headTargets('v2.15.2', tags.map(image));
  assert.deepEqual(targets.map(t => t.split(':').at(-1)), ['head', 'v2.16-head', 'v2.15.3-head', 'v2.15-head']);
  assert.deepEqual(headTargets('v2.15.2', tags.map(image), 'ABC123'), [image('v2.15.3-head-abc123')]);
  assert.deepEqual(headTargets('v2.15.2', tags.map(image), '2.17'), [image('v2.17.0-head')]);
});
test('head chart proposal prefers installed version and never silently jumps release lines', () => {
  assert.equal(defaultHeadChart('v2.15.2', ['2.16.0', '2.15.3', '2.15.2'].map(chart)), '2.15.2');
  assert.equal(defaultHeadChart('v2.15.2', ['2.16.0'].map(chart)), '');
  assert.equal(defaultHeadChart('v2.15.2', ['2.15.3', '2.15.10'].map(chart)), '2.15.10');
});
test('unresolved versions do not invent version paths or chart recommendations', () => {
  assert.deepEqual(releaseTargets('head', ['2.16.0'].map(chart)), []);
  assert.equal(defaultHeadChart('head', ['2.16.0'].map(chart)), '');
  assert.deepEqual(upgradeSearchHints('head'), []);
  assert.deepEqual(upgradeSearchHints('v2.15.2').map(h => h.query), ['2.15', '2.16']);
});

test('installed commit-based minor heads retain searches and chart proposals without inventing a patch', () => {
  const current = 'v2.15-19c92983f6f9d7f455de668e62fbfe55c045cde2-head';
  assert.deepEqual(upgradeSearchHints(current).map(h => h.query), ['2.15', '2.16']);
  assert.equal(defaultHeadChart(current, ['2.15.2', '2.16.0'].map(chart)), '2.15.2');
  assert.deepEqual(headTargets(current, ['v2.15.3-head', 'v2.16-head', 'v2.14-head'].map(image)), ['v2.16-head', 'v2.15.3-head'].map(image));
  const stable = releaseTargets(current, ['2.15.3', '2.16.0'].map(chart));
  assert.equal(stable.length, 2);
  assert.ok(stable.every(item => !item.recommended && !item.blocked));
  assert.equal(releaseTargets(current, ['2.15.3-rc1', '2.16.0-rc1', '2.17.0-rc1'].map(chart), true).length, 2);
});
