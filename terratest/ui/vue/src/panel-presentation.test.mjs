import test from 'node:test';
import assert from 'node:assert/strict';
import { discoveryValue, initialDiscovery, inventoryExposure } from './panel-presentation.mjs';

test('background discovery retains completed empty results, card copy, and actions', () => {
  const completed = { items: [], updatedAt: '2026-09-29T18:00:00Z', refreshing: false };
  const refreshing = { ...completed, refreshing: true };
  for (const kind of ['aws', 'clusters']) {
    assert.equal(discoveryValue(kind, refreshing), discoveryValue(kind, completed));
  }
  for (const hasRuns of [true, false]) {
    assert.deepEqual(inventoryExposure(refreshing, hasRuns), inventoryExposure(completed, hasRuns));
  }
  assert.equal(initialDiscovery(refreshing), false);
});

test('first discovery remains visibly unverified and real results still update', () => {
  const first = { items: [], refreshing: true };
  assert.equal(initialDiscovery(first), true);
  assert.equal(discoveryValue('clusters', first), 'Checking…');
  assert.equal(discoveryValue('aws', first), 'Scanning…');
  assert.equal(inventoryExposure(first).title, 'Checking AWS resources');
  const inventory = { ...first, items: [{}, {}] };
  assert.equal(discoveryValue('aws', inventory), '2 resources');
  assert.equal(inventoryExposure(inventory).title, '2 resources visible');
  assert.equal(discoveryValue('clusters', { items: [{reachable: true}, {reachable: false}] }), '1/2 reachable');
  assert.equal(initialDiscovery(inventory), false);
});
