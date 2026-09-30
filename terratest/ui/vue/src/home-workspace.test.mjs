import test from 'node:test';
import assert from 'node:assert/strict';
import { workspaceTools, guidedPaths, resumableTab, filterWorkspaceTools, homeSnapshot } from './home-workspace.mjs';

test('home distinguishes unverified startup, fresh activity, and stale activity without claiming readiness', () => {
  const boot = homeSnapshot({}, { bootPending: true });
  assert.equal(boot.known, false);
  assert.equal(boot.checking, true);
  assert.equal(boot.action, 'runs');
  const pending = homeSnapshot({ setup: { running: true, runId: 'test-run' }, workspace: { canStartIsolatedRun: true } });
  assert.equal(pending.action, 'runs');
  assert.equal(pending.provisioning, true);
  assert.equal(pending.cloud[0].runId, 'test-run');
  const stale = homeSnapshot({ setup: { running: true } }, { error: 'Timed out' });
  assert.equal(stale.label, 'Last known status');
  assert.equal(stale.stale, true);
  assert.equal(stale.action, 'runs');
  assert.match(stale.detail, /may have changed/);
  assert.equal(homeSnapshot({}, { bootPending: true, error: 'Timed out' }).label, 'Status unavailable');
  assert.equal(homeSnapshot({ workspace: { canStartIsolatedRun: false } }).label, 'Workspace loaded');
});

test('cleanup routes and local activity remain independent of cloud provisioning', () => {
  const snapshot = homeSnapshot({ awsCleanup: { running: true }, k3d: { clusters: [{status:'running'}, {status:'stopped'}, {status:'creating'}] }, steve: { operation:{running:true}, runs:[{status:'serving'}, {status:'failed'}] }, aws: { items:[{}], refreshing:true } });
  assert.equal(snapshot.action, 'aws');
  assert.equal(snapshot.provisioning, false);
  assert.equal(snapshot.k3d, 2);
  assert.equal(snapshot.steve, 1);
  assert.equal(snapshot.localBusy, true);
  assert.equal(snapshot.aws, 1);
  assert.equal(snapshot.awsRefreshing, true);
  assert.equal(homeSnapshot({ cleanupBatch:{running:true} }).action, 'destroy');
  assert.equal(homeSnapshot({ workspace: { runs:[{}] } }).action, 'runs');
  assert.equal(homeSnapshot(null).action, 'setup');
});

test('tool discovery and resume navigation use real destinations and useful task terms', () => {
  assert.equal(workspaceTools.length, new Set(workspaceTools.map(tool => tool.id)).size);
  assert.equal(resumableTab('lifecycle'), 'runs');
  for (const id of ['home', 'unknown', null, 'javascript:alert(1)']) assert.equal(resumableTab(id), '');
  assert.equal(resumableTab('steve'), 'steve');
  assert.deepEqual(filterWorkspaceTools('SQL cache').map(tool => tool.id), ['clusters', 'cache', 'steve']);
  assert.deepEqual(filterWorkspaceTools('  K3s  LOCAL ').map(tool => tool.id), ['k3d']);
  assert.ok(filterWorkspaceTools('costs').some(tool => tool.id === 'destroy'));
  assert.deepEqual(filterWorkspaceTools('not-a-real-tool'), []);
  assert.equal(filterWorkspaceTools('').length, workspaceTools.length);
  for (const path of guidedPaths) for (const step of path.steps) assert.ok(resumableTab(step.tab));
});
