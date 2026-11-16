import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { computed, reactive, ref } from 'vue';
import { CONFIRMATION_TEXT } from './confirmation.mjs';

const component = name => readFileSync(new URL(name, import.meta.url), 'utf8');
const script = component('./DestroyClusterButton.vue').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;\n/gm, '');

function fixture() {
  const cluster = index => ({ id: `run-4ff008a3-ha-${index}-local`, name: `HA ${index} Local (4ff008a3)`, runId: '4ff008a3', type: 'local', haIndex: index, rancherUrl: `https://ha-${index}.example` });
  const props = reactive({ cluster: cluster(4) });
  const state = ref({ workspace: { runs: [{ runId: '4ff008a3', deploymentType: 'ha-rke2', totalHAs: 5 }] }, clusters: { items: [cluster(4), cluster(5)] } });
  const requests = [], reviews = [], notices = [];
  let confirm;
  const bindings = {
    computed, ref, state, defineProps: () => props, CONFIRMATION_TEXT,
    bootPending: ref(false), cleanupStarting: ref(false), runDestroyBlocked: () => false,
    clusterDisplayName: (id, name) => id === cluster(4).id ? 'Webhook QA' : name,
    requestTypedConfirmation: options => { reviews.push(options); return new Promise(resolve => { confirm = resolve; }); },
    apiFetch: async (url, options) => { requests.push([url, JSON.parse(options.body)]); return { json: async () => ({ running: true, clusterId: cluster(4).id }) }; },
    showPanelNotice: (...args) => notices.push(args),
    openCleanupLogs() {}, refresh() {},
  };
  const model = new Function(...Object.keys(bindings), `${script}\nreturn {destroy, disabled, eligible, reason};`)(...Object.values(bindings));
  return { ...model, props, state, requests, reviews, notices, cluster, confirm: value => confirm(value) };
}

test('management card and slot header use different destroy actions', () => {
  assert.doesNotMatch(component('./ClusterCard.vue'), /DestroyRunButton|runCleanup\(/);
  assert.match(component('./ClusterCard.vue'), /<DestroyClusterButton :cluster="cluster"/);
  assert.match(component('./ClustersPanel.vue'), /<DestroyRunButton :run-id="activeRunGroup\?\.run\?\.runId"/);
});

test('review names the displayed and recorded Rancher, confirms only that cluster', async () => {
  const ui = fixture();
  const pending = ui.destroy();
  await ui.destroy();
  assert.equal(ui.reviews.length, 1);
  assert.equal(ui.reviews[0].title, 'Destroy Webhook QA?');
  assert.match(ui.reviews[0].body, /HA 4 Local \(4ff008a3\)/);
  assert.match(ui.reviews[0].body, /ha-4.example/);
  assert.deepEqual(ui.requests, []);
  ui.confirm(true);
  await pending;
  assert.deepEqual(ui.requests, [['/api/clusters/destroy', { clusterId: ui.cluster(4).id, confirm: CONFIRMATION_TEXT }]]);
});

test('cancel, switch, downstream arrival and stale inventory send no requests', async () => {
  for (const change of ['cancel', 'switch', 'downstream', 'stale']) {
    const ui = fixture();
    const pending = ui.destroy();
    if (change === 'switch') ui.props.cluster = ui.cluster(5);
    if (change === 'downstream') ui.state.value.clusters.items.push({ type: 'downstream', runId: '4ff008a3', haIndex: 4, name: 'child' });
    if (change === 'stale') ui.state.value.clusters.items = [];
    ui.confirm(change !== 'cancel');
    await pending;
    assert.deepEqual(ui.requests, [], change);
  }
});

test('external, hosted and downstream cards have no cluster destroy action', () => {
  for (const patch of [{ id: 'external' }, { runId: '' }, { id: 'run-4ff008a3-hosted-host-1' }, { type: 'downstream' }]) {
    const ui = fixture();
    Object.assign(ui.props.cluster, patch);
    assert.ok(!ui.eligible.value);
  }
});
