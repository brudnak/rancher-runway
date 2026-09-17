<script setup>
import { computed, nextTick, onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { writeTextToClipboard } from './clipboard.js';
import HelmLabValue from './HelmLabValue.vue';
import HelmLabIcon from './HelmLabIcon.vue';
import HelmLabVersionPicker from './HelmLabVersionPicker.vue';
import HelmLabChanges from './HelmLabChanges.vue';
import {
  buildOutput, buildSetupScript, changedField, codeTokens, dataRoot, fieldErrors, fieldsFor, importValues,
  initialConfig, initialOverrides, restoreState, shareState,
} from './helmlab.mjs';

const config = reactive(initialConfig());
const overrides = ref(initialOverrides());
const env = ref([]);
const search = ref('');
const changedOnly = ref(false);
const catalog = ref(null);
const versions = ref([]);
const chart = ref(null);
const entry = ref(null);
const loading = ref(false);
const error = ref('');
const notice = ref('');
const noticeIsError = ref(false);
const outputTab = ref('command');
const outputTabs = [{ id: 'command', label: 'Command' }, { id: 'yaml', label: 'Values' }, { id: 'changes', label: 'Changes' }, { id: 'script', label: 'Setup script' }];
const copiedText = ref('');
let copyTimer;
const filteredCollapsed = ref(new Set());
const expanded = ref(new Set(['Essentials']));
const importOpen = ref(false);
const importText = ref('');
const importError = ref('');
const fileInput = ref(null);
const undo = ref(null);
const includeSensitive = ref(false);
const shareOpen = ref(false);
const omitted = ref([]);
const channels = computed(() => catalog.value?.distributions.find(item => item.id === config.distribution)?.channels || []);
const channel = computed(() => channels.value.find(item => item.id === config.type));
const fields = computed(() => chart.value ? fieldsFor(chart.value) : []);
const errors = computed(() => fieldErrors(fields.value, overrides.value));
const changed = computed(() => fields.value.filter(field => changedField(field, overrides.value)));
const unavailable = computed(() => chart.value ? Object.keys(overrides.value).filter(path => !fields.value.some(field => field.path === path)) : []);
const groups = computed(() => {
  const result = new Map();
  const query = search.value.trim().toLowerCase();
  for (const field of fields.value) {
    if (changedOnly.value && !changedField(field, overrides.value)) continue;
    if (query && !`${field.path} ${field.description || ''}`.toLowerCase().includes(query)) continue;
    if (!result.has(field.group)) result.set(field.group, []);
    result.get(field.group).push(field);
  }
  return [...result.entries()].map(([name, items]) => ({ name, fields: items, changed: items.filter(field => changedField(field, overrides.value)).length }));
});
const visibleCount = computed(() => groups.value.reduce((sum, group) => sum + group.fields.length, 0));
const unknownVersion = computed(() => config.version.trim() && entry.value?.version !== config.version.trim());
const selectedVersion = computed(() => config.version.trim() || entry.value?.version || 'Resolving chart…');
const output = computed(() => {
  if (loading.value || error.value || !chart.value || !entry.value || !channel.value) return {};
  try { return buildOutput(config, channel.value, selectedVersion.value, fields.value, overrides.value, env.value, catalog.value.chart); }
  catch (err) { return { error: err.message }; }
});
const setupScript = computed(() => buildSetupScript(output.value));
const outputText = computed(() => outputTab.value === 'changes' ? '' : outputTab.value === 'script' ? setupScript.value : outputTab.value === 'yaml' ? output.value.allYaml : output.value.command);
const highlightedOutput = computed(() => codeTokens(outputText.value || '', outputTab.value === 'yaml'));
const activeEnv = computed(() => env.value.filter(item => item.name || item.value));
const allGroupsOpen = computed(() => groups.value.length && groups.value.every(group => groupOpen(group.name)));
const firstInvalidField = computed(() => fields.value.find(field => errors.value[field.path]));
const actionLabel = computed(() => ({ install: 'Install or upgrade', upgrade: 'Merge new defaults', 'upgrade-reuse': 'Reuse installed values' })[config.action]);
const actionHint = computed(() => ({
  install: 'Creates the release and namespace when needed.',
  upgrade: 'New chart defaults → installed values → your overrides. Requires Helm 3.14+.',
  'upgrade-reuse': 'Keeps installed values, then applies your overrides. New chart defaults are skipped.',
})[config.action]);
let generation = 0;
let abortController;
const cache = new Map();
async function read(path, signal) {
  if (cache.has(path)) return cache.get(path);
  const response = await fetch(dataRoot + path, { signal });
  if (!response.ok) throw new Error(`Chart catalog request failed (${response.status}).`);
  const data = await response.json();
  if (data.schemaVersion !== 1) throw new Error('Unsupported chart catalog format.');
  cache.set(path, data);
  return data;
}
async function load(refresh = false) {
  const request = ++generation;
  abortController?.abort();
  abortController = new AbortController();
  const controller = abortController;
  const timeout = setTimeout(() => controller.abort(), 20000);
  loading.value = true;
  error.value = '';
  chart.value = null;
  try {
    if (refresh) { cache.clear(); catalog.value = null; }
    const index = catalog.value || await read('index.json', controller.signal);
    if (request !== generation) return;
    catalog.value = index;
    const selected = channel.value;
    if (!selected) throw new Error('This channel is unavailable. Select another release channel.');
    const wanted = config.version.trim();
    const list = (await read(selected.versions, controller.signal)).versions;
    const chosen = list.find(item => item.version === wanted) || list.find(item => item.version === selected.latest) || list[0];
    if (!chosen) throw new Error('No versions are available in this channel.');
    const payload = await read(`charts/${chosen.chart}.json`, controller.signal);
    if (request !== generation) return;
    versions.value = list;
    entry.value = chosen;
    chart.value = payload;
  } catch (err) {
    if (request === generation) error.value = err.name === 'AbortError' ? 'The catalog request timed out. Check your connection and retry.' : err.message;
  } finally {
    clearTimeout(timeout);
    if (request === generation) loading.value = false;
  }
}
function changeDistribution() {
  if (!channels.value.some(item => item.id === config.type)) config.type = channels.value[0]?.id || 'ga';
  changeChannel();
}
function changeChannel() {
  config.version = '';
  config.repo = `rancher-${config.distribution}-${config.type}`;
  versions.value = [];
  load();
}
function jumpTo(id) { document.getElementById(id)?.scrollIntoView({ block: 'start' }); }
function announce(text, failed = false) { notice.value = text; noticeIsError.value = failed; }
async function copy(text, message = 'Copied to clipboard.') {
  try {
    await writeTextToClipboard(text);
    copiedText.value = text;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => { copiedText.value = ''; }, 2200);
    announce(message);
  }
  catch (err) { announce(err.message, true); }
}
function share() {
  const url = new URL(window.location.href);
  url.search = '';
  const saved = shareState(config, overrides.value, env.value, includeSensitive.value);
  url.hash = 'helm=' + encodeURIComponent(JSON.stringify(saved));
  copy(url.href, saved.omitted.length ? 'Link copied. Passwords and environment values were omitted.' : 'Configuration link copied.');
  shareOpen.value = false;
}
function download(text, filename) {
  const url = URL.createObjectURL(new Blob([text], { type: filename.endsWith('.sh') ? 'text/x-shellscript' : 'application/yaml' }));
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
  announce(`${filename} downloaded.`);
}
function snapshot() { undo.value = JSON.stringify({ overrides: overrides.value, env: env.value }); }
function undoEdit() {
  if (!undo.value) return;
  const previous = JSON.parse(undo.value);
  overrides.value = previous.overrides;
  env.value = previous.env;
  undo.value = null;
  announce('Previous values restored.');
}
function reset() { snapshot(); overrides.value = {}; env.value = []; announce('Chart defaults restored. Undo is available.'); }
function preset(kind) {
  snapshot();
  const values = kind === 'local' ? initialOverrides() : { replicas: '3', 'image.pullPolicy': 'IfNotPresent' };
  overrides.value = { ...overrides.value, ...Object.fromEntries(Object.entries(values).filter(([path]) => fields.value.some(field => field.path === path))) };
  announce(kind === 'local' ? 'Local test values applied. Undo is available.' : 'Three replicas and IfNotPresent applied. Undo is available.');
}
function removeUnavailable() {
  snapshot();
  overrides.value = Object.fromEntries(Object.entries(overrides.value).filter(([path]) => !unavailable.value.includes(path)));
}
function applyImport() {
  try {
    const imported = importValues(importText.value, fields.value);
    snapshot();
    overrides.value = imported.overrides;
    env.value = imported.env;
    importOpen.value = false;
    importError.value = '';
    announce('YAML imported. Review the changes below; undo is available.');
    changedOnly.value = true;
  } catch (err) { importError.value = err.message; }
}
async function readFile(event) {
  const file = event.target.files?.[0];
  if (!file) return;
  if (file.size > 1024 * 1024) importError.value = 'Choose a values file smaller than 1 MB.';
  else {
    try { importText.value = await file.text(); importError.value = ''; }
    catch { importError.value = 'The file could not be read.'; }
  }
  event.target.value = '';
}
function groupOpen(name) {
  return search.value.trim() || changedOnly.value ? !filteredCollapsed.value.has(name) : expanded.value.has(name);
}
function toggleAllGroups() {
  const close = allGroupsOpen.value;
  expanded.value = new Set(close ? [] : groups.value.map(group => group.name));
  filteredCollapsed.value = new Set(close ? groups.value.map(group => group.name) : []);
}
async function focusField(field) {
  search.value = '';
  changedOnly.value = false;
  expanded.value = new Set([...expanded.value, field.group]);
  await nextTick();
  const input = document.getElementById(`hl-${field.path}`);
  input?.scrollIntoView({ block: 'center' });
  input?.focus({ preventScroll: true });
}
function reviewChanges() { outputTab.value = 'changes'; jumpTo('hl-preview'); }
watch([search, changedOnly], () => { filteredCollapsed.value = new Set(); });
function selectOutput(event) {
  const tabs = outputTabs.map(item => item.id);
  const current = tabs.indexOf(outputTab.value);
  const index = event.key === 'ArrowRight' ? (current + 1) % tabs.length
    : event.key === 'ArrowLeft' ? (current + tabs.length - 1) % tabs.length
      : event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : -1;
  if (index < 0) return;
  event.preventDefault();
  outputTab.value = tabs[index];
  document.getElementById(`hl-tab-${tabs[index]}`)?.focus();
}
function toggleGroup(name) {
  if (search.value.trim() || changedOnly.value) {
    const next = new Set(filteredCollapsed.value);
    if (next.has(name)) next.delete(name); else next.add(name);
    filteredCollapsed.value = next;
  } else {
    const next = new Set(expanded.value);
    if (next.has(name)) next.delete(name); else next.add(name);
    expanded.value = next;
  }
}
onMounted(() => {
  if (window.location.hash.startsWith('#helm=')) {
    try {
      const saved = restoreState(JSON.parse(decodeURIComponent(window.location.hash.slice(6))));
      Object.assign(config, saved.config);
      overrides.value = saved.overrides;
      env.value = saved.env;
      omitted.value = saved.omitted;
    } catch { announce('This configuration link could not be read. Starting with local test defaults.', true); }
  }
  load();
});
onBeforeUnmount(() => { generation++; abortController?.abort(); });
// Changing a version immediately hides output built from a different reference chart.
let versionTimer;
watch(() => config.version, () => {
  generation++;
  abortController?.abort();
  loading.value = true;
  clearTimeout(versionTimer);
  versionTimer = setTimeout(() => load(), 300);
});
onBeforeUnmount(() => { clearTimeout(versionTimer); clearTimeout(copyTimer); });
</script>

<template>
  <div class="helmlab">
    <header class="hl-header">
      <div class="hl-title-row"><span class="hl-mark" aria-hidden="true"><HelmLabIcon name="terminal" /></span><div><h2>Helm Lab</h2><p class="hl-muted">Shape your next Rancher release.</p></div></div>
      <div class="hl-header-actions"><button type="button" class="hl-mobile-jump" @click="jumpTo('hl-preview')">View command ↓</button><button type="button" :disabled="loading" @click="load(true)"><HelmLabIcon name="refresh" /> Refresh catalog</button><button type="button" @click="shareOpen = !shareOpen" :aria-expanded="shareOpen"><HelmLabIcon name="share" /> Share configuration</button></div>
    </header>
    <div v-if="shareOpen" class="hl-share">
      <div><strong>Share this setup</strong><p class="hl-help">Passwords and environment values are omitted unless you include them.</p></div>
      <label class="hl-check"><input v-model="includeSensitive" type="checkbox"> Include passwords and environment values</label>
      <button type="button" class="hl-primary" @click="share">Copy link</button>
    </div>
    <div v-if="notice" class="hl-notice" :class="{ 'hl-alert': noticeIsError }" role="status"><span>{{ notice }}</span><button type="button" aria-label="Dismiss notification" @click="notice = ''">×</button></div>
    <div v-if="omitted.length" class="hl-notice">This shared setup omitted {{ omitted.join(', ') }}. Add any needed values before using it.<button type="button" aria-label="Dismiss omitted values notice" @click="omitted = []">×</button></div>

    <div class="hl-summary">
      <div><span class="hl-eyebrow">Target release</span><strong>{{ config.release || 'Untitled release' }} <span class="hl-summary-separator">/</span> {{ config.namespace || 'No namespace' }}</strong></div>
      <div><span class="hl-eyebrow">Chart</span><strong>{{ config.distribution === 'prime' ? 'Prime' : 'Community' }} · {{ config.type.toUpperCase() }}</strong><span class="hl-summary-version">{{ selectedVersion }}</span></div>
      <div><span class="hl-eyebrow">Plan</span><strong>{{ actionLabel }}</strong><span class="hl-summary-version">{{ changed.length }} modified values · {{ activeEnv.length }} environment variables</span><button type="button" class="hl-summary-link" :disabled="!chart" @click="reviewChanges">Review changes <HelmLabIcon name="arrow" /></button></div>
    </div>

    <div class="hl-workspace">
      <div id="hl-editor" class="hl-editor">
        <section class="hl-card">
          <div class="hl-section-title"><span class="hl-step">01</span><h3>Release settings</h3></div>
          <div class="hl-grid">
            <label for="hl-distribution">Distribution<select id="hl-distribution" v-model="config.distribution" :disabled="!catalog" @change="changeDistribution"><option value="prime">Prime</option><option value="community">Community</option></select></label>
            <label for="hl-channel">Release channel<select id="hl-channel" v-model="config.type" :disabled="!catalog" @change="changeChannel"><option v-for="item in channels" :key="item.id" :value="item.id">{{ item.label }}</option></select></label>
            <HelmLabVersionPicker class="hl-span" v-model="config.version" :versions="versions" :latest="channel?.latest" :disabled="!versions.length" />
            <label for="hl-release">Release name<input id="hl-release" v-model="config.release" spellcheck="false"></label>
            <label for="hl-namespace">Namespace<input id="hl-namespace" v-model="config.namespace" spellcheck="false"></label>
            <label class="hl-span" for="hl-strategy">Upgrade strategy<select id="hl-strategy" v-model="config.action"><option value="install">Install or upgrade</option><option value="upgrade">Merge new chart defaults</option><option value="upgrade-reuse">Reuse installed values</option></select><span class="hl-help">{{ actionHint }}</span></label>
          </div>
          <details class="hl-advanced"><summary>Command options <span class="hl-muted">Context, wait, dry run & repository</span></summary><div class="hl-grid">
            <label for="hl-repo">Repository alias<input id="hl-repo" v-model="config.repo" spellcheck="false"></label>
            <label for="hl-context">Kube context<input id="hl-context" v-model="config.context" placeholder="Current context" spellcheck="false"></label>
            <label class="hl-check"><input v-model="config.wait" type="checkbox"> Wait for resources</label>
            <label class="hl-check"><input v-model="config.dryRun" type="checkbox"> Dry run</label>
            <label v-if="config.wait" for="hl-timeout">Wait timeout<input id="hl-timeout" v-model="config.timeout" placeholder="5m"></label>
            <p class="hl-help hl-span">{{ channel?.repo }}</p>
          </div></details>
        </section>

        <section class="hl-card">
          <div class="hl-section-title"><span class="hl-step">02</span><h3>Chart values</h3><span class="hl-count">{{ fields.length }}</span></div>
          <div class="hl-toolbar"><button type="button" :disabled="!chart" title="One replica, rancher.local, admin bootstrap password, and Always pull policy" @click="preset('local')">Local test</button><button type="button" :disabled="!chart" @click="preset('replicas')">3 replicas</button><button type="button" :disabled="!chart" @click="importOpen = !importOpen" :aria-expanded="importOpen">Import YAML</button><span class="hl-toolbar-spacer"></span><button v-if="undo" type="button" @click="undoEdit">Undo</button><button type="button" :disabled="!chart" @click="reset">Reset all</button></div>
          <div v-if="importOpen" class="hl-import"><div class="hl-row"><label for="hl-import">Paste values.yaml</label><button type="button" @click="fileInput.click()">Choose file</button><input ref="fileInput" class="hl-file-input" type="file" accept=".yaml,.yml,.txt" aria-label="Import values file" @change="readFile"></div><textarea id="hl-import" v-model="importText" rows="7" spellcheck="false" placeholder="hostname: rancher.example.com&#10;replicas: 3" /><p class="hl-help">Replaces the current overrides and environment variables. Only keys supported by this chart are accepted.</p><p v-if="importError" class="hl-error-text" role="alert">{{ importError }}</p><div class="hl-toolbar"><button type="button" class="hl-primary" @click="applyImport">Apply values</button><button type="button" @click="importOpen = false">Cancel</button></div></div>
          <div class="hl-filter"><div class="hl-search-wrap"><HelmLabIcon name="search" /><input v-model="search" type="search" placeholder="Find a value or setting…" aria-label="Search chart values"></div><button type="button" :class="{ 'hl-selected': changedOnly }" :aria-pressed="changedOnly" @click="changedOnly = !changedOnly">Modified {{ changed.length }}</button></div>
          <div v-if="loading" class="hl-empty" role="status"><span class="hl-loading-dot"></span> Loading chart metadata…</div>
          <div v-else-if="error" class="hl-empty hl-alert" role="alert"><p>{{ error }}</p><button type="button" @click="load(true)">Retry catalog</button></div>
          <template v-else-if="chart">
            <div v-if="unknownVersion" class="hl-note">Custom version selected. The editor uses {{ entry.version }} as a reference; verify compatibility with your chart.</div>
            <div v-if="unavailable.length" class="hl-note"><p>This chart does not support: {{ unavailable.join(', ') }}.</p><button type="button" @click="removeUnavailable">Remove unavailable overrides</button></div>
            <div class="hl-row hl-field-meta"><span>{{ visibleCount }} settings{{ search ? ' found' : '' }}</span><button type="button" class="hl-text-button" @click="toggleAllGroups">{{ allGroupsOpen ? 'Collapse all' : 'Expand all' }}</button></div>
            <div v-if="!groups.length" class="hl-empty"><strong>{{ changedOnly ? 'No modified values match.' : 'No settings found.' }}</strong><p class="hl-help">{{ search ? 'Try a shorter name or clear the search.' : 'Edit a value to see it here.' }}</p><button v-if="search || changedOnly" type="button" @click="search = ''; changedOnly = false">Show all settings</button></div>
            <section v-for="group in groups" :key="group.name" class="hl-group">
              <button type="button" class="hl-group-toggle" :aria-expanded="groupOpen(group.name)" @click="toggleGroup(group.name)"><span>{{ groupOpen(group.name) ? '−' : '+' }} <strong>{{ group.name }}</strong></span><span>{{ group.changed ? `${group.changed} modified · ` : '' }}{{ group.fields.length }}</span></button>
              <div v-if="groupOpen(group.name)" class="hl-group-body" :class="{ 'hl-essentials': group.name === 'Essentials' }"><HelmLabValue v-for="field in group.fields" :key="field.path" :field="field" :value="overrides[field.path]" :changed="changedField(field, overrides)" :error="errors[field.path]" @update="overrides[field.path] = $event" @reset="delete overrides[field.path]" /></div>
            </section>
          </template>
        </section>

        <section class="hl-card">
          <div class="hl-section-title"><span class="hl-step">03</span><h3>Environment</h3><span class="hl-count">{{ env.length }}</span><button type="button" class="hl-add" @click="env.push({ name: '', value: '' })">+ Add variable</button></div>
          <p v-if="!env.length" class="hl-help">Pass extra environment variables to the Rancher deployment.</p>
          <div v-for="(item, index) in env" :key="index" class="hl-env-row"><input v-model="item.name" placeholder="VARIABLE_NAME" :aria-label="`Variable ${index + 1} name`" spellcheck="false"><input v-model="item.value" placeholder="Value" :aria-label="`Variable ${index + 1} value`" spellcheck="false"><button type="button" :aria-label="`Remove variable ${index + 1}`" @click="env.splice(index, 1)">×</button></div>
        </section>
      </div>

      <aside id="hl-preview" class="hl-preview">
        <div class="hl-preview-inner">
          <button type="button" class="hl-mobile-jump" @click="jumpTo('hl-editor')">↑ Back to settings</button>
          <div class="hl-row"><div><span class="hl-eyebrow">Release plan</span><h3>Your release, ready to run.</h3></div><span class="hl-badge" :class="{ 'hl-badge-warning': !output.command }">{{ loading ? 'Loading' : output.command ? (config.dryRun ? 'Dry run' : 'Ready') : 'Needs attention' }}</span></div>
          <p class="hl-help">Check your changes, then copy a command or a complete setup script.</p><button type="button" class="hl-dry-run" :class="{ 'hl-selected': config.dryRun }" :aria-pressed="config.dryRun" @click="config.dryRun = !config.dryRun"><span class="hl-toggle-dot"></span> Dry run {{ config.dryRun ? 'on' : 'off' }}</button>
          <div class="hl-delivery" role="group" aria-label="Values delivery"><button type="button" :class="{ 'hl-selected': config.delivery === 'set' }" :aria-pressed="config.delivery === 'set'" @click="config.delivery = 'set'">Inline flags</button><button type="button" :class="{ 'hl-selected': config.delivery === 'file' }" :aria-pressed="config.delivery === 'file'" @click="config.delivery = 'file'">Values file</button></div>
          <div class="hl-output-tabs" role="tablist" aria-label="Generated output"><button v-for="item in outputTabs" :id="`hl-tab-${item.id}`" :key="item.id" type="button" role="tab" :aria-selected="outputTab === item.id" :tabindex="outputTab === item.id ? 0 : -1" @keydown="selectOutput" aria-controls="hl-output" :class="{ 'hl-output-active': outputTab === item.id }" @click="outputTab = item.id">{{ item.label }}<span v-if="item.id === 'changes'" class="hl-tab-count">{{ changed.length + (activeEnv.length ? 1 : 0) }}</span></button></div>
          <div id="hl-output" role="tabpanel" :aria-labelledby="`hl-tab-${outputTab}`">
            <HelmLabChanges v-if="outputTab === 'changes' && chart && !loading" :fields="changed" :overrides="overrides" :env="activeEnv" @edit="focusField" @reset="path => { snapshot(); delete overrides[path]; }" />
            <div v-else class="hl-code-window">
              <div class="hl-code-top"><span><span class="hl-code-dot"></span>{{ outputTab === 'yaml' ? 'values.yaml · all overrides' : outputTab === 'script' ? 'setup.sh · repository + release' : 'Helm · shell' }}</span><button type="button" :disabled="!outputText" @click="copy(outputText)"><HelmLabIcon :name="copiedText === outputText && outputText ? 'check' : 'copy'" />{{ copiedText === outputText && outputText ? 'Copied' : 'Copy' }}</button></div>
              <pre v-if="outputText" tabindex="0"><code><span v-for="(token, index) in highlightedOutput" :key="index" :class="token.kind ? `hl-token-${token.kind}` : undefined">{{ token.text }}</span></code></pre>
              <div v-else class="hl-code-empty">{{ loading ? 'Resolving chart and values…' : 'Your command will appear here after the settings are valid.' }}</div>
            </div>
          </div>
          <div v-if="output.error" class="hl-note hl-alert" role="alert">{{ output.error }}<button v-if="firstInvalidField" type="button" @click="focusField(firstInvalidField)">Fix {{ firstInvalidField.path }} →</button></div>
          <template v-if="output.command">
            <div class="hl-output-actions">
              <button type="button" class="hl-primary" @click="copy(outputTab === 'script' ? setupScript : outputTab === 'yaml' ? output.allYaml : output.command)"><HelmLabIcon name="copy" />{{ outputTab === 'script' ? 'Copy full setup' : outputTab === 'yaml' ? 'Copy values YAML' : 'Copy Helm command' }}</button>
              <button type="button" @click="download(outputTab === 'script' ? setupScript : output.allYaml, outputTab === 'script' ? 'setup.sh' : 'values.yaml')"><HelmLabIcon name="download" />{{ outputTab === 'script' ? 'Download script' : 'Export values' }}</button>
            </div>
            <p v-if="outputTab === 'script'" class="hl-help">Adds the repository, prepares any needed values in a temporary file, and runs your selected command. Review it before running in your terminal.</p>
            <div v-if="output.needsFile && outputTab !== 'script'" class="hl-note"><strong>Save the companion file before running.</strong><p class="hl-help">{{ config.delivery === 'file' ? 'Your command reads all overrides from values.yaml.' : 'Lists and maps are stored in values.yaml; scalar values stay on the command line.' }}</p><button type="button" @click="download(output.yaml, 'values.yaml')">Download command’s values.yaml</button><button type="button" class="hl-text-button" @click="outputTab = 'script'">Or use the self-contained setup script →</button></div>
            <div class="hl-review"><div><span>Release</span><strong>{{ config.release }}</strong></div><div><span>Namespace</span><strong>{{ config.namespace }}</strong></div><div><span>Context</span><strong>{{ config.context || 'Current kube context' }}</strong></div><div><span>Version</span><strong>{{ selectedVersion }}</strong></div></div>
          </template>
          <p class="hl-footnote">{{ catalog?.generatedAt ? `Catalog updated ${new Date(catalog.generatedAt).toLocaleDateString()}. ` : '' }}Chart metadata loads online; edits stay in this session.</p>
        </div>
      </aside>
    </div>
  </div>
</template>
