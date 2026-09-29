<script setup>
import { computed, nextTick, onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { apiFetch } from './store.js';
import { readJSON } from './read-json.mjs';
import { writeTextToClipboard } from './clipboard.js';
import HelmLabValue from './HelmLabValue.vue';
import HelmLabIcon from './HelmLabIcon.vue';
import HelmLabVersionPicker from './HelmLabVersionPicker.vue';
import HelmLabChanges from './HelmLabChanges.vue';
import { buildOutput, buildSetupScript, changedField, codeTokens, fieldErrors, fieldGroups, fieldsFor, importValues, initialConfig, initialOverrides, sensitivePath, releaseInputErrors } from './helmlab.mjs';
import { createEditHistory, editGroup, effectiveValue, planFindings, releaseChannels, suggestedFields } from './helm-workbench.mjs';

import { matchingFields, releaseIssues } from './helm-presentation.mjs';

const root = ref(null), searchInput = ref(null), fileInput = ref(null);
const config = reactive(initialConfig()), overrides = ref(initialOverrides()), env = ref([]);
const stage = ref('target'), section = ref('Access'), search = ref(''), editedOnly = ref(false);
const showAllSettings = ref(false), attemptedReview = ref(false), touched = reactive({}), noticeUndo = ref(false);
const modKey = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl';
const catalog = ref(null), loading = ref(false), loadError = ref(''), notice = ref(''), noticeError = ref(false);
const importOpen = ref(false), importText = ref(''), importError = ref(''), saving = ref(false), copied = ref('');
const outputTab = ref('script'), revealOutput = ref(false);
const stages = [{ id: 'target', label: 'Choose a release', hint: 'Chart & destination' }, { id: 'configure', label: 'Make it yours', hint: 'Values & environment' }, { id: 'review', label: 'Review & export', hint: 'An exact, portable plan' }];
const outputTabs = [{ id: 'script', label: 'Runbook' }, { id: 'command', label: 'Helm command' }, { id: 'yaml', label: 'Values YAML' }, { id: 'changes', label: 'Override review' }];
const channels = computed(() => releaseChannels[config.distribution] || []);
const selectedVersion = computed(() => catalog.value?.selected?.version || '');
const versions = computed(() => catalog.value?.versions || []);
const fields = computed(() => catalog.value ? fieldsFor(catalog.value.chart) : []);
const errors = computed(() => fieldErrors(fields.value, overrides.value));
const explicit = computed(() => fields.value.filter(field => Object.hasOwn(overrides.value, field.path)));
const unavailable = computed(() => Object.keys(overrides.value).filter(path => !fields.value.some(field => field.path === path)));
const findings = computed(() => catalog.value ? planFindings(config, fields.value, overrides.value, env.value) : []);
const blocking = computed(() => findings.value.filter(item => item.level === 'error'));
const value = path => effectiveValue(fields.value, overrides.value, path);
const activeEnv = computed(() => env.value.filter(item => item.name || item.value));
const filtered = computed(() => matchingFields(fields.value, overrides.value, { search: search.value, editedOnly: editedOnly.value, section: section.value }));
const shownFields = computed(() => search.value || editedOnly.value || showAllSettings.value ? filtered.value : suggestedFields(filtered.value, value));
const output = computed(() => {
  if (loading.value || loadError.value || !catalog.value) return {};
  if (blocking.value.length) return { error: blocking.value[0].detail };
  try { return buildOutput(config, { repo: catalog.value.repository, devel: config.type !== 'ga' }, selectedVersion.value, fields.value, overrides.value, env.value); }
  catch (error) { return { error: error.message }; }
});
const metadataErrors = computed(() => releaseInputErrors(config));
const issues = computed(() => releaseIssues(metadataErrors.value, errors.value, findings.value, unavailable.value));
const prerequisites = computed(() => findings.value.filter(item => item.level !== 'error'));
const issueCount = computed(() => issues.value.length || (output.value.error ? 1 : 0));
const targetReady = computed(() => !!catalog.value && !loading.value && !loadError.value && !issues.value.some(item => item.section === 'target'));
const visibleError = key => attemptedReview.value || touched[key] ? metadataErrors.value[key] : '';
const valueError = field => errors.value[field.path] || ((attemptedReview.value || touched[field.path]) && blocking.value.find(item => item.field === field.path)?.detail) || '';
const exportHeading = computed(() => loading.value ? 'Resolving your chart.' : loadError.value ? 'Reconnect to continue.' : !ready.value ? `${issueCount.value === 1 ? 'One detail' : `${issueCount.value || 'A few'} details`} to finish.` : config.dryRun ? 'Your rehearsal is ready.' : 'Ready to export.');
const script = computed(() => buildSetupScript(output.value));
const outputText = computed(() => ({ command: output.value.command, yaml: output.value.allYaml, script: script.value })[outputTab.value] || '');
const outputTokens = computed(() => codeTokens(outputText.value, outputTab.value === 'yaml'));
const hasSensitive = computed(() => explicit.value.some(field => (sensitivePath(field.path) || field.type === 'yaml') && overrides.value[field.path]) || activeEnv.value.length > 0);
const ready = computed(() => Boolean(output.value.command));
const planStatus = computed(() => loading.value ? 'Resolving chart' : loadError.value ? 'Catalog unavailable' : ready.value ? (config.dryRun ? 'Rehearsal ready' : 'Plan ready') : `${issueCount.value || 'A few'} ${issueCount.value === 1 ? 'detail' : 'details'} to finish`);
const actionLabel = computed(() => ({ install: 'Install or update', upgrade: 'Merge with installed values', 'upgrade-reuse': 'Keep installed defaults' })[config.action]);
const snapshot = () => ({ config: { ...config }, overrides: { ...overrides.value }, env: env.value.map(item => ({ ...item })) });
const history = createEditHistory(snapshot()), historyRevision = ref(0);
let restoring = false, previous = snapshot(), controller, requestID = 0, loadTimer, copyTimer;
const canUndo = computed(() => { historyRevision.value; return history.canUndo; });
const canRedo = computed(() => { historyRevision.value; return history.canRedo; });
watch(() => JSON.stringify(snapshot()), () => {
  const next = snapshot();
  if (!restoring) { history.record(next, editGroup(previous, next)); historyRevision.value++; }
  previous = next;
});
async function travel(direction) {
  restoring = true;
  const draft = history[direction]();
  Object.assign(config, draft.config); overrides.value = draft.overrides; env.value = draft.env;
  await nextTick(); restoring = false; previous = snapshot(); historyRevision.value++;
  announce(direction === 'undo' ? 'Edit undone.' : 'Edit restored.');
}
function announce(message, failed = false, undoable = false) { notice.value = message; noticeError.value = failed; noticeUndo.value = undoable; }
function setOverride(path, value) { overrides.value = { ...overrides.value, [path]: value }; }
function resetOverride(path) { const next = { ...overrides.value }; delete next[path]; overrides.value = next; }
async function load(refresh = false) {
  clearTimeout(loadTimer);
  const id = ++requestID;
  controller?.abort(); controller = new AbortController();
  const signal = controller.signal;
  loading.value = true; loadError.value = ''; revealOutput.value = false;
  const params = new URLSearchParams({ distribution: config.distribution, channel: config.type, version: config.version.trim() });
  if (refresh) params.set('refresh', '1');
  let detach = () => {};
  try {
    const response = await readJSON(deadlineSignal => {
      const combined = new AbortController();
      const abort = () => combined.abort();
      signal.addEventListener('abort', abort, { once: true });
      deadlineSignal.addEventListener('abort', abort, { once: true });
      if (signal.aborted || deadlineSignal.aborted) abort();
      detach = () => {
        signal.removeEventListener('abort', abort); deadlineSignal.removeEventListener('abort', abort);
      };
      return apiFetch(`/api/helm-lab/catalog?${params}`, { signal: combined.signal, cache: 'no-store' });
    }, { label: 'Rancher chart lookup', timeoutMs: 23000 });
    if (id === requestID) catalog.value = response;
  } catch (error) {
    if (id === requestID && !signal.aborted) loadError.value = error.message || 'Chart lookup failed. Retry the repository.';
  } finally { detach(); if (id === requestID) loading.value = false; }
}
watch(() => [config.distribution, config.type, config.version], () => {
  requestID++; controller?.abort(); loading.value = true; loadError.value = '';
  clearTimeout(loadTimer); loadTimer = setTimeout(() => load(), 200);
});
function chooseDistribution(distribution) {
  if (distribution === config.distribution) return;
  config.distribution = distribution;
  if (!channels.value.some(item => item.id === config.type)) config.type = 'ga';
  config.version = ''; config.repo = `runway-${config.distribution}-${config.type}`;
  catalog.value = null;
}
function chooseChannel() { config.version = ''; config.repo = `runway-${config.distribution}-${config.type}`; catalog.value = null; }
function goStage(id) {
  stage.value = id; revealOutput.value = false; if (id === 'review') attemptedReview.value = true;
  nextTick(() => { const tab = document.getElementById(`hl-stage-${id}`); tab?.focus({ preventScroll: true }); tab?.scrollIntoView({ block: 'nearest' }); });
}
function navigateStages(event) {
  const ids = stages.map(item => item.id), current = ids.indexOf(stage.value);
  const next = event.key === 'ArrowRight' ? (current + 1) % ids.length : event.key === 'ArrowLeft' ? (current + ids.length - 1) % ids.length : event.key === 'Home' ? 0 : event.key === 'End' ? ids.length - 1 : -1;
  if (next < 0) return;
  event.preventDefault(); goStage(ids[next]); document.getElementById(`hl-stage-${ids[next]}`)?.focus();
}
function navigateOutput(event) {
  const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End'];
  if (!keys.includes(event.key)) return;
  event.preventDefault();
  const i = outputTabs.findIndex(tab => tab.id === outputTab.value);
  outputTab.value = outputTabs[event.key === 'Home' ? 0 : event.key === 'End' ? 3 : (i + (event.key === 'ArrowRight' ? 1 : 3)) % 4].id;
  revealOutput.value = false;
  nextTick(() => document.getElementById(`hl-tab-${outputTab.value}`)?.focus());
}
async function focusField(field) {
  if (!field) return;
  stage.value = 'configure'; section.value = field.group; search.value = ''; editedOnly.value = false;
  showAllSettings.value = !suggestedFields(fields.value.filter(item => item.group === field.group), value).some(item => item.path === field.path);
  await nextTick(); const element = document.getElementById(`hl-${field.path}`);
  element?.scrollIntoView({ block: 'center', behavior: 'auto' }); element?.focus({ preventScroll: true });
}
async function fixFinding(item) {
  if (!item) { goStage(loadError.value || !catalog.value ? 'target' : 'configure'); return; }
  attemptedReview.value = true;
  if (item.field) { await focusField(fields.value.find(field => field.path === item.field)); return; }
  if (item.section === 'environment') {
    stage.value = 'configure'; section.value = 'Environment'; search.value = ''; editedOnly.value = false;
    await nextTick(); root.value?.querySelector('[id^="hl-env-name-"]')?.focus(); return;
  }
  if (item.section === 'unavailable') { stage.value = 'configure'; await nextTick(); document.getElementById('hl-unavailable')?.focus(); return; }
  stage.value = item.section === 'review' ? 'review' : 'target';
  await nextTick();
  if (item.configKey === 'repo') root.value.querySelector('.hl-repository-option').open = true;
  const element = document.getElementById(`hl-${item.configKey === 'action' ? 'strategy' : item.configKey || 'context'}`);
  element?.focus(); element?.scrollIntoView({ block: 'center' });
}
function firstIssue() { fixFinding(issues.value[0]); }
function chooseSection(id) { section.value = id; search.value = ''; editedOnly.value = false; showAllSettings.value = false; }
async function findSetting() { stage.value = 'configure'; await nextTick(); searchInput.value?.focus(); }
function keydown(event) {
  if (!root.value?.getClientRects().length || event.isComposing) return;
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); findSetting(); }
  if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') { event.preventDefault(); goStage('review'); }
  const editing = event.target instanceof HTMLElement && (event.target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target.tagName));
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'z' && !editing) { event.preventDefault(); const direction = event.shiftKey ? 'redo' : 'undo'; if (direction === 'undo' ? canUndo.value : canRedo.value) travel(direction); }
  if (event.key === 'Escape') { if (importOpen.value) importOpen.value = false; else search.value = ''; }
}
function preset(kind) {
  const candidates = kind === 'sandbox' ? { hostname: 'rancher.local', replicas: '1', [fields.value.some(item => item.path === 'image.pullPolicy') ? 'image.pullPolicy' : 'rancherImagePullPolicy']: 'Always' }
    : kind === 'resilient' ? { replicas: '3', antiAffinity: 'required' }
      : { 'ingress.tls.source': 'secret' };
  overrides.value = { ...overrides.value, ...Object.fromEntries(Object.entries(candidates).filter(([path]) => fields.value.some(field => field.path === path))) };
  if (kind === 'resilient') { config.wait = true; config.timeout = '10m'; }
  goStage('configure'); search.value = ''; editedOnly.value = false; showAllSettings.value = false; section.value = kind === 'resilient' ? 'Workload' : 'Access';
  announce('Starting point applied. Fine-tune it below.', false, true);
}
function reset() { overrides.value = {}; env.value = []; config.envExplicit = false; announce('Overrides cleared. You can undo this.', false, true); }
function removeUnavailable() { overrides.value = Object.fromEntries(Object.entries(overrides.value).filter(([path]) => !unavailable.value.includes(path))); }
function applyImport() {
  try {
    const result = importValues(importText.value, fields.value);
    overrides.value = result.overrides; env.value = result.env; config.envExplicit = result.envExplicit;
    importOpen.value = false; importError.value = ''; editedOnly.value = Object.keys(result.overrides).length > 0;
    search.value = ''; section.value = Object.keys(result.overrides).length ? 'Access' : result.envExplicit ? 'Environment' : 'Access';
    const count = Object.keys(result.overrides).length;
    announce(`${count} explicit ${count === 1 ? 'value' : 'values'} imported${result.envExplicit ? ' with environment settings' : ''}. Review below.`, false, true);
  } catch (error) { importError.value = error.message; }
}
async function readFile(event) {
  const file = event.target.files?.[0]; if (!file) return;
  if (file.size > 1024 * 1024) importError.value = 'Choose a YAML file smaller than 1 MB.';
  else { try { importText.value = await file.text(); importError.value = ''; } catch { importError.value = 'The file could not be read.'; } }
  event.target.value = '';
}
async function copy(text, label) {
  if (!text) return;
  try { await writeTextToClipboard(text); copied.value = label; clearTimeout(copyTimer); copyTimer = setTimeout(() => { copied.value = ''; }, 2000); }
  catch (error) { announce(error.message, true); }
}
async function save(text, filename) {
  if (saving.value || !text) return;
  saving.value = true;
  try {
    const saved = await readJSON(signal => apiFetch('/api/helm-lab/save', { method: 'POST', signal, body: JSON.stringify({ filename, content: text }) }), { label: 'Export', timeoutMs: 15000 });
    announce(`Saved ${saved.filename} to Downloads.`);
  } catch (error) { announce(error.message, true); }
  finally { saving.value = false; }
}
onMounted(() => { load(); document.addEventListener('keydown', keydown); });
onBeforeUnmount(() => { requestID++; controller?.abort(); clearTimeout(loadTimer); clearTimeout(copyTimer); document.removeEventListener('keydown', keydown); });
</script>

<template>
  <div ref="root" class="helmlab hl-workbench" :class="{ 'hl-in-flow': stage !== 'target' }">
    <header class="hl-masthead">
      <div class="hl-brand"><span class="hl-emblem" aria-hidden="true"><HelmLabIcon name="compass" /></span><div><span class="hl-eyebrow">RANCHER RUNWAY / RELEASE WORKBENCH</span><h2>Helm Lab<span class="hl-title-period">.</span></h2><p>A clear path from chart to command.</p></div></div>
      <div class="hl-tools"><button type="button" @click="findSetting"><HelmLabIcon name="search" /> Find a setting <kbd>{{ modKey }} K</kbd></button><div class="hl-history"><button type="button" :disabled="!canUndo" aria-label="Undo edit" :title="`Undo edit (${modKey} Z outside text fields)`" @click="travel('undo')"><HelmLabIcon name="undo" /></button><button type="button" :disabled="!canRedo" aria-label="Redo edit" :title="`Redo edit (${modKey} Shift Z outside text fields)`" @click="travel('redo')"><HelmLabIcon name="redo" /></button></div></div>
    </header>
    <div v-if="notice" class="hl-notice" :class="{ 'hl-alert': noticeError }" role="status"><HelmLabIcon :name="noticeError ? 'signal' : 'check'" /><span>{{ notice }}</span><button v-if="noticeUndo && canUndo" type="button" @click="travel('undo')">Undo</button><button type="button" aria-label="Dismiss notification" @click="notice = ''">×</button></div>

    <div class="hl-release-strip" aria-label="Release at a glance">
      <div class="hl-release-identity"><span class="hl-orbit" :class="{ 'hl-orbit-ready': ready }" aria-hidden="true"><span></span></span><div><strong>{{ config.release || 'Name your release' }}</strong><span>{{ config.namespace || 'Choose a namespace' }}</span></div></div>
      <div><span class="hl-eyebrow">Rancher</span><strong class="hl-truncate" :title="selectedVersion">{{ loading ? 'Resolving…' : selectedVersion || 'Choose a chart' }}</strong><small>{{ config.distribution === 'prime' ? 'Prime' : 'Community' }} / {{ channels.find(item => item.id === config.type)?.label }}</small></div>
      <div><span class="hl-eyebrow">Destination</span><strong class="hl-truncate">{{ value('hostname') || (config.action === 'install' ? 'Hostname needed' : 'Inherited hostname') }}</strong><small>{{ config.context || 'Current kube context' }}</small></div>
      <div class="hl-strip-status" :data-ready="ready"><button v-if="!ready && !loading && !loadError && catalog" type="button" class="hl-status-action" @click="firstIssue"><HelmLabIcon name="signal" />{{ planStatus }}<HelmLabIcon name="arrow" /></button><span v-else class="hl-status-label"><HelmLabIcon v-if="ready" name="check" /><span v-else class="hl-status-dot" :class="{ 'hl-status-pulse': loading }"></span>{{ planStatus }}</span></div>
    </div>

    <nav class="hl-stages" role="tablist" aria-label="Release workflow">
      <button v-for="(item, index) in stages" :id="`hl-stage-${item.id}`" :key="item.id" type="button" role="tab" :aria-selected="stage === item.id" :tabindex="stage === item.id ? 0 : -1" aria-controls="hl-stage-content" :class="{ 'hl-stage-active': stage === item.id }" @keydown="navigateStages" @click="goStage(item.id)"><span class="hl-stage-number" :class="{ 'hl-stage-complete': (item.id === 'target' && targetReady) || (item.id === 'configure' && ready) }"><HelmLabIcon v-if="(item.id === 'target' && targetReady) || (item.id === 'configure' && ready)" name="check" /><template v-else>0{{ index + 1 }}</template></span><span><strong>{{ item.label }}</strong><small>{{ item.hint }}</small></span><HelmLabIcon name="arrow" /></button>
    </nav>

    <div v-if="loadError" class="hl-catalog-error" role="alert"><HelmLabIcon name="signal" /><div><strong>The chart couldn’t be resolved.</strong><p>{{ loadError }}</p><span>Your edits are still here.</span></div><button type="button" @click="load(true)">Retry</button></div>
    <div v-if="loading" class="hl-progress" role="status"><span></span><p>Reading the selected chart from Rancher…</p></div>

    <div id="hl-stage-content" role="tabpanel" class="hl-stage-panel" :aria-labelledby="`hl-stage-${stage}`" :aria-busy="loading">
      <div v-if="stage === 'target'" class="hl-target-layout">
        <section class="hl-target-main">
          <div class="hl-section-heading"><div><span class="hl-eyebrow">THE FOUNDATION</span><h3>Choose your chart.</h3></div><button type="button" class="hl-text-button" :disabled="loading" @click="load(true)"><HelmLabIcon name="refresh" /> Refresh from Rancher</button></div>
          <div class="hl-distributions" role="group" aria-label="Rancher distribution"><button v-for="item in ['prime', 'community']" :key="item" type="button" :aria-pressed="config.distribution === item" :class="{ 'hl-distribution-active': config.distribution === item }" @click="chooseDistribution(item)"><HelmLabIcon :name="item === 'prime' ? 'diamond' : 'layers'" /><span><strong>{{ item === 'prime' ? 'Rancher Prime' : 'Rancher Community' }}</strong><small>{{ item === 'prime' ? 'SUSE distribution & development builds' : 'Community releases & previews' }}</small></span><span class="hl-choice-dot"></span></button></div>
          <div class="hl-grid hl-chart-selection"><label for="hl-channel">Release channel<select id="hl-channel" v-model="config.type" @change="chooseChannel"><option v-for="item in channels" :key="item.id" :value="item.id">{{ item.label }}</option></select></label><HelmLabVersionPicker v-model="config.version" :versions="versions" :latest="versions[0]?.version" :disabled="!versions.length || loading" /></div>
          <div v-if="catalog && !loading && !loadError" class="hl-provenance"><HelmLabIcon name="check" /><div><strong>Exact chart. Published checksum verified.</strong><span>{{ catalog.repository }}</span><small v-if="catalog.selected.kubeVersion">Chart Kubernetes constraint: {{ catalog.selected.kubeVersion }}</small></div></div>
          <div class="hl-section-heading hl-destination-heading"><div><span class="hl-eyebrow">THE DESTINATION</span><h3>Make the target explicit.</h3></div></div>
          <div class="hl-grid"><label for="hl-release">Release name<input id="hl-release" aria-label="Release name" v-model="config.release" :aria-invalid="Boolean(visibleError('release'))" :aria-describedby="visibleError('release') ? 'hl-error-release' : undefined" @blur="touched.release = true" spellcheck="false" autocomplete="off"><span v-if="visibleError('release')" id="hl-error-release" class="hl-error-text">{{ visibleError('release') }}</span></label><label for="hl-namespace">Namespace<input id="hl-namespace" aria-label="Namespace" v-model="config.namespace" :aria-invalid="Boolean(visibleError('namespace'))" :aria-describedby="visibleError('namespace') ? 'hl-error-namespace' : undefined" @blur="touched.namespace = true" spellcheck="false" autocomplete="off"><span v-if="visibleError('namespace')" id="hl-error-namespace" class="hl-error-text">{{ visibleError('namespace') }}</span></label><label class="hl-span" for="hl-context">Kube context <span class="hl-label-optional">optional</span><input id="hl-context" aria-label="Kube context" v-model="config.context" :aria-invalid="Boolean(visibleError('context'))" :aria-describedby="visibleError('context') ? 'hl-error-context' : undefined" @blur="touched.context = true" placeholder="Use the current terminal context" spellcheck="false" autocomplete="off"><span v-if="visibleError('context')" id="hl-error-context" class="hl-error-text">{{ visibleError('context') }}</span></label><label class="hl-span" for="hl-strategy">Release behavior<select id="hl-strategy" aria-label="Release behavior" aria-describedby="hl-strategy-hint" v-model="config.action"><option value="install">Install a release, or update it if it exists</option><option value="upgrade">Upgrade · merge new defaults, installed values and overrides</option><option value="upgrade-reuse">Upgrade · keep installed defaults and apply overrides</option></select><span id="hl-strategy-hint" class="hl-help">{{ config.action === 'upgrade' ? 'Uses Helm 3.14+ reset-then-reuse semantics. Explicit overrides always win.' : config.action === 'upgrade-reuse' ? 'New defaults are skipped. Choose this only when you want the installed baseline.' : 'Creates the release and namespace when needed.' }}</span></label></div>
          <div class="hl-next-row"><span><HelmLabIcon name="lock" /> Builds a plan; nothing runs in your cluster.</span><button type="button" class="hl-primary" :disabled="!catalog || loading || !!loadError" @click="goStage('configure')">Configure values <HelmLabIcon name="arrow" /></button></div>
        </section>
        <aside class="hl-starting-points"><span class="hl-eyebrow">A RUNNING START</span><h3>Less setup.<br>More intention.</h3><p>Start with a few deliberate choices. Fine-tune every value afterward.</p><button type="button" :disabled="!catalog || loading || !!loadError" @click="preset('sandbox')"><HelmLabIcon name="flask" /><span><strong>Local sandbox</strong><small>One replica, local hostname, fresh images</small></span><HelmLabIcon name="arrow" /></button><button type="button" :disabled="!catalog || loading || !!loadError" @click="preset('resilient')"><HelmLabIcon name="layers" /><span><strong>Spread the workload</strong><small>Three replicas on separate nodes; wait for readiness</small></span><HelmLabIcon name="arrow" /></button><button type="button" :disabled="!catalog || loading || !!loadError" @click="preset('certificate')"><HelmLabIcon name="key" /><span><strong>Use your certificate</strong><small>Configure ingress to use an existing TLS secret</small></span><HelmLabIcon name="arrow" /></button><div class="hl-start-note"><span class="hl-tiny-rule"></span><p>Every preset is editable.<br>Every edit can be undone.</p></div></aside>
      </div>

      <div v-else-if="stage === 'configure'" class="hl-configure">
        <div class="hl-configure-top"><div><span class="hl-eyebrow">THE DETAILS</span><h3>A place for every setting.</h3></div><div class="hl-tools"><button type="button" :disabled="!catalog || loading || !!loadError" :aria-expanded="importOpen" @click="importOpen = !importOpen"><HelmLabIcon name="upload" /> Import YAML</button><button type="button" :disabled="!explicit.length && !env.length && !config.envExplicit" @click="reset">Clear overrides</button></div></div>
        <div v-if="importOpen" class="hl-import"><div class="hl-row"><label for="hl-import-values">Bring your values</label><button type="button" @click="fileInput?.click()">Choose a YAML file</button><input ref="fileInput" class="hl-file-input" type="file" accept=".yaml,.yml,.txt" @change="readFile"></div><textarea id="hl-import-values" v-model="importText" rows="6" spellcheck="false" placeholder="hostname: rancher.example.com&#10;replicas: 3"></textarea><p class="hl-help">Replaces your overrides and environment variables. Explicit values, including chart defaults, are preserved.</p><p v-if="importError" role="alert" class="hl-error-text">{{ importError }}</p><div class="hl-toolbar"><button type="button" class="hl-primary" :disabled="!catalog || loading || !!loadError" @click="applyImport">Import and review</button><button type="button" @click="importOpen = false">Cancel</button></div></div>
        <div v-if="catalog && unavailable.length && !loading" id="hl-unavailable" tabindex="-1" class="hl-note hl-alert"><strong>{{ unavailable.length }} overrides don’t belong to this chart.</strong><p>{{ unavailable.join(', ') }}</p><button type="button" @click="removeUnavailable">Remove unsupported overrides</button></div>
        <div class="hl-filter"><div class="hl-search-wrap"><HelmLabIcon name="search" /><input ref="searchInput" v-model="search" type="search" aria-label="Search all chart settings" placeholder="Find hostname, TLS, image, scheduling…" :disabled="loading || !catalog"><button v-if="search" type="button" class="hl-clear-search" aria-label="Clear setting search" @click="search = ''; searchInput?.focus()"><HelmLabIcon name="close" /></button></div><button type="button" :aria-pressed="editedOnly" :class="{ 'hl-selected': editedOnly }" @click="editedOnly = !editedOnly">Overrides <span class="hl-count">{{ explicit.length }}</span></button></div>
        <div class="hl-settings-layout">
          <nav class="hl-section-nav" aria-label="Settings categories"><button v-for="group in fieldGroups" :key="group.id" type="button" :class="{ 'hl-section-active': section === group.id && !search && !editedOnly }" :aria-current="section === group.id && !search && !editedOnly ? 'true' : undefined" @click="chooseSection(group.id)"><HelmLabIcon :name="group.icon" /><span><strong>{{ group.id }}</strong><small>{{ group.hint }}</small></span><span class="hl-count">{{ fields.filter(field => field.group === group.id && Object.hasOwn(overrides, field.path)).length || '' }}</span></button><button type="button" :class="{ 'hl-section-active': section === 'Environment' && !search && !editedOnly }" :aria-current="section === 'Environment' && !search && !editedOnly ? 'true' : undefined" @click="chooseSection('Environment')"><HelmLabIcon name="terminal" /><span><strong>Environment</strong><small>Variables for Rancher pods</small></span><span class="hl-count">{{ activeEnv.length || (config.envExplicit ? 'clear' : '') }}</span></button><div class="hl-nav-foot">{{ fields.length }} settings from the selected chart.<br>Edits stay in this session.</div></nav>
          <section v-if="section === 'Environment' && !search && !editedOnly" class="hl-settings-body"><div class="hl-row"><div><h3>Environment variables</h3><p class="hl-help">Passed to the Rancher deployment as string values.</p></div><button type="button" @click="config.envExplicit = true; env.push({ name: '', value: '' })">+ Add variable</button></div><div v-if="config.envExplicit && !env.length" class="hl-note"><strong>Explicitly clear environment variables.</strong><p>The export includes extraEnv: [] to clear installed values.</p><button type="button" @click="config.envExplicit = false">Use inherited environment</button></div><div v-else-if="!env.length" class="hl-empty"><HelmLabIcon name="terminal" /><strong>A clean environment.</strong><p>Add only the variables your Rancher deployment needs.</p></div><div v-for="(item, index) in env" :key="index" class="hl-env-row"><label :for="`hl-env-name-${index}`">Name<input :id="`hl-env-name-${index}`" v-model="item.name" placeholder="VARIABLE_NAME" spellcheck="false" autocomplete="off"></label><label :for="`hl-env-value-${index}`">Value<input :id="`hl-env-value-${index}`" v-model="item.value" placeholder="String value" spellcheck="false" autocomplete="off"></label><button type="button" :aria-label="`Remove variable ${index + 1}`" @click="config.envExplicit = true; env.splice(index, 1)">×</button></div></section>
          <section v-else class="hl-settings-body"><div class="hl-row hl-fields-heading"><h3>{{ search ? 'Search results' : editedOnly ? 'Your overrides' : section }}</h3><span>{{ shownFields.length }}{{ shownFields.length < filtered.length ? ` of ${filtered.length}` : '' }} {{ filtered.length === 1 ? 'setting' : 'settings' }}{{ editedOnly ? ' with overrides' : '' }}</span></div><div v-if="loading" class="hl-empty">Resolving exact chart values…</div><div v-else-if="!catalog" class="hl-empty">Choose a chart to start configuring.</div><div v-else-if="!filtered.length" class="hl-empty"><HelmLabIcon name="search" /><strong>No matching settings.</strong><p>Try a shorter query or show inherited values.</p><button type="button" @click="search = ''; editedOnly = false">Clear filters</button></div><div v-else class="hl-fields"><HelmLabValue v-for="field in shownFields" :key="`${selectedVersion}-${field.path}`" :field="field" :value="overrides[field.path]" :explicit="Object.hasOwn(overrides, field.path)" :changed="changedField(field, overrides)" :error="valueError(field)" @blur="touched[field.path] = true" @update="setOverride(field.path, $event)" @reset="resetOverride(field.path)" /></div><button v-if="!search && !editedOnly && filtered.length > shownFields.length" type="button" class="hl-more-settings" @click="showAllSettings = true">Show {{ filtered.length - shownFields.length }} more {{ section.toLowerCase() }} settings <HelmLabIcon name="chevron" /></button><button v-else-if="showAllSettings && !search && !editedOnly && section !== 'Advanced'" type="button" class="hl-more-settings" @click="showAllSettings = false">Show essential settings</button></section>
        </div>
        <div class="hl-next-row"><button type="button" class="hl-text-button" @click="goStage('target')"><HelmLabIcon name="arrow-left" /> Chart & destination</button><span>{{ explicit.length }} explicit {{ explicit.length === 1 ? 'value' : 'values' }} · {{ activeEnv.length }} environment {{ activeEnv.length === 1 ? 'variable' : 'variables' }}<span v-if="config.envExplicit && !activeEnv.length"> · environment cleared</span></span><button type="button" class="hl-primary" @click="goStage('review')">Review release <HelmLabIcon name="arrow" /></button></div>
      </div>

      <div v-else class="hl-review-layout">
        <aside class="hl-plan-review"><span class="hl-eyebrow">THE RELEASE BRIEF</span><h3>{{ config.release || 'Your release' }}</h3><p>{{ actionLabel }}</p><dl><div><dt>Chart version</dt><dd>{{ selectedVersion || 'Unresolved' }}</dd></div><div><dt>Namespace</dt><dd>{{ config.namespace }}</dd></div><div><dt>Kube context</dt><dd>{{ config.context || 'Current context' }}</dd></div><div><dt>Rancher address</dt><dd>{{ value('hostname') || (config.action === 'install' ? 'Not set' : 'Inherited') }}</dd></div><div><dt>Overrides</dt><dd>{{ explicit.length }} {{ explicit.length === 1 ? 'value' : 'values' }} / {{ activeEnv.length }} {{ activeEnv.length === 1 ? 'variable' : 'variables' }}</dd></div></dl><details v-if="prerequisites.length" class="hl-prerequisites" open>
            <summary>Before you run <span class="hl-count">{{ prerequisites.length }}</span></summary>
            <div class="hl-findings"><button v-for="item in prerequisites" :key="item.id" type="button" @click="fixFinding(item)"><HelmLabIcon name="arrow" /><span><strong>{{ item.title }}</strong><small>{{ item.detail }}</small></span></button></div>
          </details><p class="hl-session-note"><HelmLabIcon name="lock" /> Generated locally. Nothing is deployed from this workbench.</p></aside>
        <section class="hl-export-workspace"><div class="hl-row hl-export-heading"><div><span class="hl-eyebrow">THE HANDOFF</span><h3>{{ exportHeading }}</h3></div><button type="button" class="hl-rehearsal" :aria-pressed="config.dryRun" :class="{ 'hl-selected': config.dryRun }" @click="config.dryRun = !config.dryRun"><HelmLabIcon name="flask" /> {{ config.dryRun ? 'Dry run on' : 'Dry run off' }}</button></div>
          <div v-if="issues.length && !loading && !loadError" class="hl-issues" aria-label="Details to finish">
            <div class="hl-issues-heading"><HelmLabIcon name="signal" /><strong>{{ issues.length === 1 ? 'Finish this detail to prepare your export' : `Finish these ${issues.length} details to prepare your export` }}</strong></div>
            <button v-for="issue in issues" :key="issue.id" type="button" @click="fixFinding(issue)"><span><strong>{{ issue.title }}</strong><small>{{ issue.detail }}</small></span><HelmLabIcon name="arrow" /></button>
          </div>
          <div class="hl-export-options"><label for="hl-delivery">Values delivery<select id="hl-delivery" v-model="config.delivery"><option value="file">YAML file</option><option value="set">Inline flags + structured YAML</option></select></label><label class="hl-check"><input v-model="config.wait" type="checkbox"> Wait for readiness</label><label v-if="config.wait" for="hl-timeout">Timeout<input id="hl-timeout" aria-label="Timeout" v-model="config.timeout" :aria-invalid="Boolean(visibleError('timeout'))" :aria-describedby="visibleError('timeout') ? 'hl-error-timeout' : undefined" @blur="touched.timeout = true" placeholder="10m"><span v-if="visibleError('timeout')" id="hl-error-timeout" class="hl-error-text">{{ visibleError('timeout') }}</span></label></div>
          <details class="hl-repository-option"><summary>Repository alias</summary><label for="hl-repo">Local Helm repository name<input id="hl-repo" aria-label="Repository alias" v-model="config.repo" :aria-invalid="Boolean(visibleError('repo'))" :aria-describedby="visibleError('repo') ? 'hl-error-repo' : undefined" @blur="touched.repo = true" spellcheck="false"><span v-if="visibleError('repo')" id="hl-error-repo" class="hl-error-text">{{ visibleError('repo') }}</span></label><pre>{{ output.repo || catalog?.repository }}</pre></details>
          <div class="hl-output-tabs" role="tablist" aria-label="Export format"><button v-for="item in outputTabs" :key="item.id" :id="`hl-tab-${item.id}`" type="button" role="tab" :aria-selected="outputTab === item.id" :tabindex="outputTab === item.id ? 0 : -1" aria-controls="hl-output" :class="{ 'hl-output-active': outputTab === item.id }" @click="outputTab = item.id; revealOutput = false" @keydown="navigateOutput">{{ item.label }}</button></div>
          <div class="hl-output-actions"><span class="hl-format-hint">{{ outputTab === 'yaml' ? 'Every explicit value, ready for Helm.' : outputTab === 'command' ? 'A pinned command for your terminal.' : outputTab === 'changes' ? 'Review each value before export.' : 'Repository, values, and command in one script.' }}</span><button type="button" class="hl-primary" :disabled="!ready" @click="copy(outputTab === 'changes' ? script : outputText, outputTab === 'yaml' ? 'Values YAML' : outputTab === 'command' ? 'Helm command' : 'Runbook')"><HelmLabIcon :name="copied === (outputTab === 'yaml' ? 'Values YAML' : outputTab === 'command' ? 'Helm command' : 'Runbook') ? 'check' : 'copy'" />{{ copied === (outputTab === 'yaml' ? 'Values YAML' : outputTab === 'command' ? 'Helm command' : 'Runbook') ? 'Copied' : outputTab === 'yaml' ? 'Copy values' : outputTab === 'command' ? 'Copy command' : 'Copy runbook' }}</button><button type="button" :disabled="!ready || saving" @click="save(outputTab === 'yaml' ? output.allYaml : script, outputTab === 'yaml' ? 'values.yaml' : 'setup.sh')"><HelmLabIcon name="download" />{{ saving ? 'Saving…' : outputTab === 'yaml' ? 'Export values.yaml' : 'Export setup.sh' }}</button></div>
          <div id="hl-output" role="tabpanel" :aria-labelledby="`hl-tab-${outputTab}`"><HelmLabChanges v-if="outputTab === 'changes' && catalog && !loading" :fields="explicit" :overrides="overrides" :env="activeEnv" :env-explicit="config.envExplicit" @reset-env="env = []; config.envExplicit = false" @edit="focusField" @reset="resetOverride" /><div v-else class="hl-code-window"><div class="hl-code-top"><span><HelmLabIcon :name="outputTab === 'yaml' ? 'file' : 'terminal'" />{{ outputTab === 'yaml' ? 'values.yaml' : outputTab === 'script' ? 'setup.sh' : 'Helm command' }}</span><button v-if="hasSensitive && outputText" type="button" :aria-pressed="revealOutput" @click="revealOutput = !revealOutput">{{ revealOutput ? 'Hide preview' : 'Reveal preview' }}</button><span v-else>PINNED TO {{ selectedVersion || '—' }}</span></div><div v-if="outputText && hasSensitive && !revealOutput" class="hl-secret-cover"><HelmLabIcon name="lock" /><strong>Preview hidden for privacy.</strong><p>Copy and export include the exact values. Reveal the preview when you’re ready to inspect them.</p><button type="button" @click="revealOutput = true">Reveal this preview</button></div><pre v-else-if="outputText" tabindex="0"><code><span v-for="(token, index) in outputTokens" :key="index" :class="token.kind ? `hl-token-${token.kind}` : undefined">{{ token.text }}</span></code></pre><div v-else class="hl-code-empty"><HelmLabIcon name="compass" /><strong>{{ loading ? 'Resolving your chart…' : 'Finish the release details.' }}</strong><p>{{ output.error || loadError || 'Choose a chart and configure its values to prepare an export.' }}</p><button type="button" @click="firstIssue">Back to settings <HelmLabIcon name="arrow" /></button></div></div></div>
          <div v-if="output.error && outputTab === 'changes'" class="hl-note hl-alert" role="alert">{{ output.error }}</div>
          <p class="hl-export-caption">{{ outputTab === 'yaml' ? 'Includes every explicit override and environment variable.' : outputTab === 'command' ? 'Register the repository above before running this command.' : 'A portable shell script: registers the repository, prepares private temporary values, then runs your plan.' }} Files save to Downloads without replacing existing files.</p><div v-if="output.needsFile && outputTab === 'command'" class="hl-note"><strong>This command also needs values.yaml.</strong><p>Save the companion file in your terminal’s working directory, or use the self-contained runbook.</p><button type="button" :disabled="saving" @click="save(output.yaml, 'values.yaml')">Export companion values</button></div>
        </section>
      </div>
    </div>
    <span class="hl-sr-only" role="status">{{ copied ? `${copied} copied to clipboard.` : '' }}</span>
    <footer class="hl-footer"><span><span class="hl-footer-dot"></span> An independent Rancher Runway workspace</span><span>Official chart sources · session-only edits</span><span>{{ modKey }} K to find · {{ modKey }} ↵ to review</span></footer>
  </div>
</template>
