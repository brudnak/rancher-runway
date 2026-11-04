import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { computed, reactive, ref } from 'vue';
import { sameRunKey } from '../../static/control_panel_utils.js';
import { CONFIRMATION_TEXT } from './confirmation.mjs';

const source = readFileSync(new URL('./store.js', import.meta.url), 'utf8');
const section = (start, end) => source.slice(source.indexOf(start), source.indexOf(end)).replaceAll('export const ', 'const ');
const cleanupSource = [
  section('const snapshotLabCleanup =', 'export const cleanupBatchStarting ='),
  section('export const awsLifecycleRunning =', 'const cleanupRunIdsInState ='),
  section('export const runCleanup =', 'export const runCleanupBatch ='),
].join('\n');

function cleanupWorkflow({ deploymentType = 'ha-rke2', submitError = '' } = {}) {
  const calls = [], confirmations = [], logRoutes = [];
  let resolveConfirmation;
  const bindings = {
    computed, sameRunKey, CONFIRMATION_TEXT,
    state: ref({workspace: {runs: [{runId: 'run-a', deploymentType}, {runId: 'run-b', deploymentType: 'ha-rke2'}]}}),
    selectedCleanupRunId: ref('run-b'),
    bootPending: ref(false), cleanupStarting: ref(false), cleanupBatchStarting: ref(false),
    refreshStatus: ref(''), dismissedCleanupResultKey: ref('previous-result'),
    cleanupLabOptions: reactive({testLab: false, cacheLab: false}),
    requestTypedConfirmation: options => {
      confirmations.push(options);
      return new Promise(resolve => { resolveConfirmation = resolve; });
    },
    apiFetch: async (path, options) => {
      calls.push({path, method: options.method, body: JSON.parse(options.body)});
      if (submitError) throw new Error(submitError);
    },
    dispatchSetupLifecycleState: () => {},
    openCleanupLogs: linode => logRoutes.push(linode),
    refresh: () => {},
  };
  const runCleanup = new Function(...Object.keys(bindings), `${cleanupSource}\nreturn runCleanup;`)(...Object.values(bindings));
  return {...bindings, runCleanup, calls, confirmations, logRoutes, confirm: result => resolveConfirmation(result)};
}

test('destroy opens a whole-run review and cancellation makes no cleanup request', async () => {
  const flow = cleanupWorkflow();
  const pending = flow.runCleanup('run-a');
  assert.equal(flow.calls.length, 0);
  assert.equal(flow.confirmations[0].title, 'Destroy run run-a?');
  assert.match(flow.confirmations[0].body, /entire run, including all management clusters and tracked downstream clusters/);
  flow.confirm(false);
  await pending;
  assert.equal(flow.calls.length, 0);
  assert.equal(flow.selectedCleanupRunId.value, 'run-b');
  assert.equal(flow.cleanupStarting.value, false);
});

test('confirmed cleanup keeps the reviewed run even if selection changes', async () => {
  const flow = cleanupWorkflow();
  flow.selectedCleanupRunId.value = 'run-a';
  const pending = flow.runCleanup();
  flow.selectedCleanupRunId.value = 'run-b';
  flow.confirm(true);
  await pending;
  assert.deepEqual(flow.calls, [{path: '/api/cleanup', method: 'POST', body: {
    confirm: CONFIRMATION_TEXT, runId: 'run-a', cleanupTestLab: false, cleanupCacheLab: false,
  }}]);
  assert.equal(flow.state.value.cleanup.runId, 'run-a');
  assert.equal(flow.state.value.cleanup.running, true);
  assert.equal(flow.selectedCleanupRunId.value, 'run-a');
  assert.deepEqual(flow.logRoutes, [false]);
});

test('contextual destroy retains lab data regardless of Destroy tab options', async () => {
  const flow = cleanupWorkflow();
  flow.cleanupLabOptions.testLab = true;
  flow.cleanupLabOptions.cacheLab = true;
  const pending = flow.runCleanup('run-a', {preserveLabData: true});
  assert.match(flow.confirmations[0].body, /Test Lab results will be kept/);
  assert.match(flow.confirmations[0].body, /Cache Lab snapshots will be kept/);
  flow.confirm(true);
  await pending;
  assert.equal(flow.calls[0].body.cleanupTestLab, false);
  assert.equal(flow.calls[0].body.cleanupCacheLab, false);
  assert.deepEqual({...flow.cleanupLabOptions}, {testLab: true, cacheLab: true});
});

test('Destroy tab cleanup preserves its reviewed lab options', async () => {
  const flow = cleanupWorkflow();
  flow.cleanupLabOptions.testLab = true;
  const pending = flow.runCleanup('run-a');
  flow.cleanupLabOptions.testLab = false;
  flow.cleanupLabOptions.cacheLab = true;
  flow.confirm(true);
  await pending;
  assert.equal(flow.calls[0].body.cleanupTestLab, true);
  assert.equal(flow.calls[0].body.cleanupCacheLab, false);
});

test('Linode cleanup reviews all run instances and routes state and logs to Linode', async () => {
  const flow = cleanupWorkflow({deploymentType: 'linode-docker-cattle'});
  const pending = flow.runCleanup('run-a');
  assert.match(flow.confirmations[0].body, /entire Linode run/);
  assert.match(flow.confirmations[0].body, /all Linode instances and AWS Route53 records/);
  flow.confirm(true);
  await pending;
  assert.equal(flow.calls[0].body.runId, 'run-a');
  assert.equal(flow.state.value.linodeCleanup.runId, 'run-a');
  assert.equal(flow.state.value.linodeCleanup.running, true);
  assert.equal(flow.state.value.cleanup, undefined);
  assert.deepEqual(flow.logRoutes, [true]);
});

test('missing or blank run IDs cannot open review or fall back to another run', async () => {
  for (const id of ['', 'missing']) {
    const flow = cleanupWorkflow();
    await flow.runCleanup(id);
    assert.equal(flow.confirmations.length, 0);
    assert.equal(flow.calls.length, 0);
  }
});

const awsBlockers = ['setup', 'readiness', 'downstream', 'cleanup', 'awsCleanup', 'cleanupBatch'];
const linodeBlockers = ['linodeSetup', 'linodeCleanup', 'awsCleanup', 'cleanupBatch'];
test('active lifecycle operations prevent review and submission for the target provider', async () => {
  for (const [deploymentType, operations] of [['ha-rke2', awsBlockers], ['linode-docker-cattle', linodeBlockers]]) {
    for (const operation of operations) {
      const flow = cleanupWorkflow({deploymentType});
      flow.state.value[operation] = {running: true};
      await flow.runCleanup('run-a');
      assert.equal(flow.confirmations.length, 0, `${deploymentType}: ${operation}`);
      assert.equal(flow.calls.length, 0, `${deploymentType}: ${operation}`);
    }
  }
  for (const lock of ['bootPending', 'cleanupStarting', 'cleanupBatchStarting']) {
    const flow = cleanupWorkflow();
    flow[lock].value = true;
    await flow.runCleanup('run-a');
    assert.equal(flow.confirmations.length, 0, lock);
    assert.equal(flow.calls.length, 0, lock);
  }
});

test('operations starting during confirmation prevent submission after confirmation', async () => {
  for (const [deploymentType, operations] of [['ha-rke2', awsBlockers], ['linode-docker-cattle', linodeBlockers]]) {
    for (const operation of operations) {
      const flow = cleanupWorkflow({deploymentType});
      const pending = flow.runCleanup('run-a');
      flow.state.value[operation] = {running: true};
      flow.confirm(true);
      await pending;
      assert.equal(flow.calls.length, 0, `${deploymentType}: ${operation}`);
      assert.match(flow.refreshStatus.value, /not started/);
    }
  }
  for (const lock of ['bootPending', 'cleanupStarting', 'cleanupBatchStarting']) {
    const flow = cleanupWorkflow();
    const pending = flow.runCleanup('run-a');
    flow[lock].value = true;
    flow.confirm(true);
    await pending;
    assert.equal(flow.calls.length, 0, lock);
  }
});

test('a removed run or changed provider during confirmation prevents submission', async () => {
  for (const change of ['removed', 'provider']) {
    const flow = cleanupWorkflow();
    const pending = flow.runCleanup('run-a');
    if (change === 'removed') flow.state.value.workspace.runs.shift();
    else flow.state.value.workspace.runs[0].deploymentType = 'linode-docker-cattle';
    flow.confirm(true);
    await pending;
    assert.equal(flow.calls.length, 0, change);
    assert.match(flow.refreshStatus.value, /changed or is no longer recorded/);
  }
});

test('failed cleanup submission unlocks the action and reports the error', async () => {
  const flow = cleanupWorkflow({submitError: 'Run is busy'});
  const pending = flow.runCleanup('run-a');
  flow.confirm(true);
  await pending;
  assert.equal(flow.calls.length, 1);
  assert.equal(flow.cleanupStarting.value, false);
  assert.equal(flow.refreshStatus.value, 'Run is busy');
  assert.equal(flow.logRoutes.length, 0);
});
