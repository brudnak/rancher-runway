import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';

const source = readFileSync(new URL('../../static/interactive_setup.js', import.meta.url), 'utf8');
const launcher = source.slice(source.indexOf('const openBuildPicker ='), source.indexOf('const renderRows ='))
  .replace('import(setupEndpoint(`/static/setup_build_picker.js?${params}`))', 'loadPicker()');

function setup(pickRancherBuild) {
  const elements = new Map();
  const element = id => {
    if (!elements.has(id)) elements.set(id, { textContent: '', open: false, scrollIntoView() {} });
    return elements.get(id);
  };
  const button = { disabled: false, isConnected: true, addEventListener() {} };
  const ui = {
    URLSearchParams, versions: ['v2.16.0', 'v2.17.0'], manualCommands: ['helm install rancher custom/chart'],
    setupMode: 'manual', deploymentType: 'ha-rke2', token: 'fixture',
    setupRootEl: { dataset: { phase: 'editor' } },
    chooseRancherVersionEl: button, setupBuildTargetEl: { value: '1' },
    rowsEl: { addEventListener() {}, querySelector() { return { focus() {} }; } },
    byId: element, isLinodeDockerDeployment: () => false, autoRowLabel: index => `Rancher ${index + 1}`,
    setupEndpoint: path => '/setup-editor' + path, escapeHtml: value => value,
    loadPicker: async () => ({ pickRancherBuild }),
    saveDeploymentVersions() {}, clearValidationError() {}, renderMode() {}, renderSetupChecklist() {},
  };
  runInNewContext(launcher + '\nglobalThis.openBuildPicker = openBuildPicker;', ui);
  return { ui, button };
}

test('selecting a build from manual mode updates only the chosen version row', async () => {
  const reference = 'stgregistry.suse.com/rancher/rancher:v2.17.0-abcdef-head';
  let request;
  const { ui, button } = setup(async (_, options) => { request = options; return reference; });
  await ui.openBuildPicker(1, button);
  assert.equal(request.endpoint, '/setup-editor/api/rancher-builds');
  assert.equal(request.label, 'Rancher 2');
  assert.equal(request.version, 'v2.17.0');
  assert.equal(ui.setupMode, 'auto');
  assert.deepEqual(ui.versions, ['v2.16.0', reference]);
  assert.deepEqual(ui.manualCommands, ['helm install rancher custom/chart']);
});

test('closing the wizard preserves manual mode and version rows', async () => {
  const { ui, button } = setup(async () => '');
  await ui.openBuildPicker(0, button);
  assert.equal(ui.setupMode, 'manual');
  assert.deepEqual(ui.versions, ['v2.16.0', 'v2.17.0']);
});

test('a pending choice cannot apply after setup becomes locked or deployment changes', async () => {
  for (const change of [
    (ui, button) => { button.disabled = true; },
    ui => { ui.deploymentType = 'linode-docker-cattle'; },
    ui => { ui.setupRootEl.dataset.phase = 'resolving'; },
  ]) {
    const { ui, button } = setup(async () => { change(ui, button); return 'example/image:tag'; });
    await ui.openBuildPicker(0, button);
    assert.equal(ui.setupMode, 'manual');
    assert.deepEqual(ui.versions, ['v2.16.0', 'v2.17.0']);
  }
});
