import { activeOperations } from '../../static/control_panel_runs.js';
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { computed, reactive, ref } from 'vue';

const source = readFileSync(new URL('./DestroyRunButton.vue', import.meta.url), 'utf8');
const store = readFileSync(new URL('./store.js', import.meta.url), 'utf8');
const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;\n/gm, '');
const lifecycleScript = store.slice(store.indexOf('export const awsLifecycleRunning'), store.indexOf('const cleanupRunIdsInState')).replaceAll('export const ', 'const ');

function button({ runId = 'run-a', runs = [{ runId: 'run-a', deploymentType: 'ha-rke2' }, { runId: 'run-b', deploymentType: 'linode-docker-cattle' }], cleanup = async () => {} } = {}) {
  const props = reactive({ runId });
  const state = ref({ workspace: { runs } });
  const bootPending = ref(false);
  const cleanupStarting = ref(false);
  const cleanupBatchStarting = ref(false);
  const calls = [];
  const navigation = [];
  const { runDestroyBlocked } = new Function('computed', 'state', 'cleanupBatchStarting', 'activeOperations', `${lifecycleScript}\nreturn {runDestroyBlocked};`)(computed, state, cleanupBatchStarting, activeOperations);
  const bindings = {
    computed, ref, state, bootPending, cleanupStarting, runDestroyBlocked,
    defineProps: () => props,
    runCleanup: async (...args) => { calls.push(args); return cleanup(...args); },
    setActivePanelTab: tab => navigation.push(tab),
  };
  const model = new Function(...Object.keys(bindings), `${script}\nreturn {run,disabled,title,reviewing,destroy};`)(...Object.values(bindings));
  return { model, props, state, bootPending, cleanupStarting, cleanupBatchStarting, calls, navigation };
}

test('contextual destroy uses the recorded run and keeps lab data without navigating', async () => {
  const ui = button({ runId: ' run-b ' });
  assert.equal(ui.model.disabled.value, false);
  assert.match(ui.model.title.value, /entire run run-b/);
  await ui.model.destroy();
  assert.deepEqual(ui.calls, [['run-b', { preserveLabData: true }]]);
  assert.deepEqual(ui.navigation, []);
});

test('changing the displayed run targets the newly selected recorded run', async () => {
  const ui = button();
  ui.props.runId = 'run-b';
  await ui.model.destroy();
  assert.equal(ui.calls[0][0], 'run-b');
});

test('changing selection during review does not retarget the existing request', async () => {
  let finish;
  const ui = button({ cleanup: () => new Promise(resolve => { finish = resolve; }) });
  const pending = ui.model.destroy();
  ui.props.runId = 'run-b';
  await ui.model.destroy();
  assert.deepEqual(ui.calls, [['run-a', { preserveLabData: true }]]);
  finish();
  await pending;
  const next = ui.model.destroy();
  assert.equal(ui.calls[1][0], 'run-b');
  finish();
  await next;
});

test('missing, external and stale runs cannot fall back to another recorded run', async () => {
  for (const runId of [undefined, '', ' ', 'external', 'run']) {
    const ui = button();
    ui.props.runId = runId;
    assert.ok(!ui.model.run.value);
    assert.equal(ui.model.disabled.value, true);
    await ui.model.destroy();
    assert.deepEqual(ui.calls, []);
  }
  const ui = button();
  ui.state.value.workspace.runs = ui.state.value.workspace.runs.filter(run => run.runId !== 'run-a');
  await ui.model.destroy();
  assert.deepEqual(ui.calls, []);
});

test('startup, pending cleanup and batches disable contextual destroy', async () => {
  for (const flag of ['bootPending', 'cleanupStarting', 'cleanupBatchStarting']) {
    const ui = button();
    ui[flag].value = true;
    assert.equal(ui.model.disabled.value, true, flag);
    await ui.model.destroy();
    assert.deepEqual(ui.calls, [], flag);
    ui[flag].value = false;
    assert.equal(ui.model.disabled.value, false, flag);
  }
});

test('contextual destroy waits for active work across both providers', async () => {
  const cases = [
    ['run-a', 'setup'], ['run-a', 'readiness'], ['run-a', 'downstream'], ['run-a', 'cleanup'],
    ['run-b', 'linodeSetup'], ['run-b', 'linodeCleanup'],
    ['run-a', 'awsCleanup'], ['run-b', 'awsCleanup'],
    ['run-a', 'cleanupBatch'], ['run-b', 'cleanupBatch'],
  ];
  for (const [runId, operation] of cases) {
    const ui = button({ runId });
    ui.state.value[operation] = { running: true };
    assert.equal(ui.model.disabled.value, true, `${runId}: ${operation}`);
    assert.match(ui.model.title.value, /Wait for the current lifecycle operation/);
    await ui.model.destroy();
    assert.deepEqual(ui.calls, []);
  }
  for (const [runId, operation] of [['run-a', 'linodeSetup'], ['run-b', 'setup']]) {
    const ui = button({ runId });
    ui.state.value[operation] = { running: true };
    assert.equal(ui.model.disabled.value, true, `${runId}: active ${operation}`);
  }
});

test('double click starts one review and cancellation unlocks the button', async () => {
  let cancel;
  const ui = button({ cleanup: () => new Promise(resolve => { cancel = resolve; }) });
  const pending = ui.model.destroy();
  assert.equal(ui.model.reviewing.value, true);
  assert.equal(ui.model.disabled.value, true);
  await ui.model.destroy();
  assert.equal(ui.calls.length, 1);
  cancel();
  await pending;
  assert.equal(ui.model.reviewing.value, false);
  assert.equal(ui.model.disabled.value, false);
});

test('a failed cleanup request releases the local review lock', async () => {
  const ui = button({ cleanup: async () => { throw new Error('request failed'); } });
  await assert.rejects(ui.model.destroy(), /request failed/);
  assert.equal(ui.model.reviewing.value, false);
  assert.equal(ui.model.disabled.value, false);
});
