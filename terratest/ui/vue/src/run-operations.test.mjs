import test from 'node:test';
import assert from 'node:assert/strict';
import { activeOperations, operationSnapshot, operationForRun, runTimelineHTML, runAdmissionBlocked } from '../../static/control_panel_runs.js';
import { homeSnapshot } from './home-workspace.mjs';

test('concurrent runs keep independent status and log selection through completion', () => {
  const prime = { operation: 'setup', runId: 'prime-run', running: true, output: ['prime output'] };
  const community = { operation: 'setup', runId: 'community-run', running: true, output: ['community output'] };
  const state = { setup: community, runOperations: [prime, community] };
  assert.equal(activeOperations(state).length, 2);
  assert.equal(operationForRun({ runId: 'prime-run' }, state)[2], prime);
  assert.deepEqual(operationSnapshot(state, 'setup', 'prime-run').output, ['prime output']);
  assert.deepEqual(operationSnapshot(state, 'setup', 'community-run').output, ['community output']);
  assert.deepEqual(operationSnapshot(state, 'setup', 'unknown-run'), {});
  assert.equal(homeSnapshot(state).cloud.length, 2);
  assert.equal(new Set(homeSnapshot(state).cloud.map(item => item.id)).size, 2);
  prime.running = false;
  prime.finishedAt = '2026-11-12T00:00:00Z';
  assert.deepEqual(activeOperations(state).map(([, , item]) => item.runId), ['community-run']);
  assert.equal(operationSnapshot(state, 'setup', 'prime-run').finishedAt, prime.finishedAt);
  assert.equal(operationForRun({ runId: 'prime-run' }, state), null);
});

test('per-run phases coexist without falling back to a different run', () => {
  const setup = { operation: 'setup', runId: 'community', running: true };
  const readiness = { operation: 'readiness', runId: 'prime', running: true, output: ['ready soon'] };
  const state = { setup, readiness, runOperations: [setup, readiness] };
  assert.equal(operationForRun({ runId: 'prime' }, state)[0], 'readiness');
  assert.deepEqual(operationSnapshot(state, 'readiness', 'community'), {});
  assert.match(runTimelineHTML({ runId: 'prime', status: 'setup_complete' }, state), /Readiness/);
  assert.equal(activeOperations({ ...state, cleanupBatch: { running: true } }).length, 3);
});

test('older state snapshots retain operation lookup while rejecting unrelated run IDs', () => {
  const operation = { runId: 'old-run', running: true, output: ['legacy'] };
  const state = { setup: operation };
  assert.equal(activeOperations(state).length, 1);
  assert.equal(operationSnapshot(state, 'setup', 'old-run'), operation);
  assert.equal(operationSnapshot(state, 'setup'), operation);
  assert.deepEqual(operationSnapshot(state, 'setup', 'another-run'), {});
  assert.deepEqual(activeOperations(null), []);
});

test('setup editing and per-run retries use operation admission independently of saved configuration', () => {
  const prime = { operation: 'setup', runId: 'prime', running: true };
  const state = {
    runOperations: [prime],
    workspace: { canConfigureSetup: true, canStartIsolatedRun: false, isolatedRunBlockedReason: 'Choose another hostname' },
  };
  assert.equal(runAdmissionBlocked(state), false);
  assert.equal(runAdmissionBlocked(state, 'community'), false);
  assert.equal(runAdmissionBlocked(state, 'prime'), true);
  state.workspace.canConfigureSetup = false;
  assert.equal(runAdmissionBlocked(state), true);
  assert.equal(runAdmissionBlocked(state, 'community'), true);
});

test('fallback admission counts distinct active runs and retains destructive locks', () => {
  const prime = { operation: 'setup', runId: 'prime', running: true };
  const community = { operation: 'readiness', runId: 'community', running: true };
  assert.equal(runAdmissionBlocked({ runOperations: [prime] }), false);
  assert.equal(runAdmissionBlocked({ runOperations: [prime, community] }), true);
  assert.equal(runAdmissionBlocked({ runOperations: [], awsCleanup: { running: true } }), true);
  assert.equal(runAdmissionBlocked({ setup: prime }), true);
});
