<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { writeTextToClipboard } from './clipboard.js';
import { buildOutput, dataRoot, displayValue, fieldsFor, initialOverrides } from './helmlab.mjs';

const config = reactive({ distribution: 'prime', type: 'head', version: '', repo: 'rancher-prime-head', release: 'rancher', namespace: 'cattle-system', action: 'upgrade', delivery: 'set' });
const overrides = ref(initialOverrides()), env = ref([]), search = ref('');
const catalog = ref(null), versions = ref([]), chart = ref(null), entry = ref(null);
const loading = ref(false), error = ref(''), notice = ref('');
const channels = computed(() => catalog.value?.distributions.find(item => item.id === config.distribution)?.channels || []);
const channel = computed(() => channels.value.find(item => item.id === config.type));
const fields = computed(() => chart.value ? fieldsFor(chart.value) : []);
const visibleFields = computed(() => fields.value.filter(field => `${field.path} ${field.description || ''}`.toLowerCase().includes(search.value.toLowerCase())));
const unknownVersion = computed(() => config.version && entry.value?.version !== config.version);
const output = computed(() => {
  if (loading.value || error.value || !chart.value || !entry.value || !channel.value) return {};
  try { return buildOutput(config, channel.value, config.version || entry.value.version, fields.value, overrides.value, env.value, catalog.value.chart); }
  catch (err) { return { error: err.message }; }
});
let generation = 0;
const cache = new Map();
async function read(path) {
  if (cache.has(path)) return cache.get(path);
  const response = await fetch(dataRoot + path, { signal: AbortSignal.timeout(20000) });
  if (!response.ok) throw new Error(`Chart catalog request failed (${response.status}).`);
  const data = await response.json();
  if (data.schemaVersion !== 1) throw new Error('Unsupported chart catalog format.');
  cache.set(path, data);
  return data;
}
async function load() {
  const request = ++generation;
  loading.value = true; error.value = ''; chart.value = null;
  try {
    if (!catalog.value) catalog.value = await read('index.json');
    const selected = channel.value;
    if (!selected) throw new Error('Select an available distribution and version type.');
    const list = (await read(selected.versions)).versions;
    const chosen = list.find(item => item.version === config.version) || list.find(item => item.version === selected.latest) || list[0];
    if (!chosen) throw new Error('No chart versions are available for this selection.');
    const payload = await read(`charts/${chosen.chart}.json`);
    if (request !== generation) return;
    versions.value = list; entry.value = chosen; chart.value = payload;
  } catch (err) { if (request === generation) error.value = err.message; }
  finally { if (request === generation) loading.value = false; }
}
function changeDistribution() {
  if (!channels.value.some(item => item.id === config.type)) config.type = channels.value[0]?.id || 'ga';
  changeChannel();
}
function changeChannel() { config.version = ''; config.repo = `rancher-${config.distribution}-${config.type}`; load(); }
async function copy(text) {
  try { await writeTextToClipboard(text); notice.value = 'Copied to clipboard.'; }
  catch (err) { notice.value = err.message; }
}
function share() {
  const url = new URL(window.location.href);
  // The hash carries only playground state, never the panel's authentication query.
  url.search = '';
  url.hash = 'helm=' + encodeURIComponent(JSON.stringify({ config, overrides: overrides.value, env: env.value }));
  copy(url.href);
}
function download() {
  const url = URL.createObjectURL(new Blob([output.value.yaml], { type: 'application/yaml' }));
  const a = document.createElement('a'); a.href = url; a.download = 'values.yaml'; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
onMounted(() => {
  if (window.location.hash.startsWith('#helm=')) {
    try {
      const saved = JSON.parse(decodeURIComponent(window.location.hash.slice(6)));
      for (const key of Object.keys(config)) if (typeof saved.config?.[key] === 'string') config[key] = saved.config[key];
      if (saved.overrides && typeof saved.overrides === 'object' && !Array.isArray(saved.overrides)) overrides.value = Object.fromEntries(Object.entries(saved.overrides).filter(([, value]) => typeof value === 'string'));
      if (Array.isArray(saved.env)) env.value = saved.env.filter(item => typeof item?.name === 'string' && typeof item?.value === 'string').map(({ name, value }) => ({ name, value }));
    } catch { notice.value = 'The configuration link could not be read; using defaults.'; }
  }
  load();
});
watch([config, overrides, env], () => { notice.value = ''; }, { deep: true });
</script>

<template>
  <div class="helmlab">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div><h2 class="text-lg font-semibold">Helm Lab</h2><p class="mt-1 text-sm text-zinc-500">Configure Rancher charts and generate a Helm command or values.yaml.</p></div>
      <button type="button" @click="share">Copy configuration link</button>
    </header>
    <p class="my-3 text-xs text-zinc-500">Live chart catalog from <a href="https://tashima42.github.io/rancher-helm-playground/" target="_blank" rel="noopener noreferrer" class="underline">Rancher Helm Playground</a>. Internet access is required. Configuration links include entered values, including passwords.</p>
    <p v-if="notice" role="status" class="my-3 text-sm">{{ notice }}</p>
    <div class="grid min-w-0 gap-6 xl:grid-cols-2">
      <div class="min-w-0 space-y-5">
        <div class="grid gap-3 sm:grid-cols-2">
          <label>Distribution<select v-model="config.distribution" @change="changeDistribution"><option v-for="item in catalog?.distributions || [{ id: 'prime', label: 'Prime' }]" :key="item.id" :value="item.id">{{ item.label }}</option></select></label>
          <label>Version type<select v-model="config.type" @change="changeChannel"><option v-for="item in channels" :key="item.id" :value="item.id">{{ item.label }}</option></select></label>
          <label class="sm:col-span-2">Chart version<input v-model="config.version" list="helm-chart-versions" placeholder="Latest published version" @change="load"><datalist id="helm-chart-versions"><option v-for="item in versions" :key="item.version" :value="item.version" /></datalist><span class="help">Choose a published version or enter a custom version. {{ channel?.hint }}</span></label>
          <label>Helm repo name<input v-model="config.repo"></label>
          <label>Release name<input v-model="config.release"></label>
          <label>Namespace<input v-model="config.namespace"></label>
          <label>Values delivery<select v-model="config.delivery"><option value="set">Flags on the command</option><option value="file">values.yaml</option></select></label>
          <label class="sm:col-span-2">Command<select v-model="config.action"><option value="install">Install or upgrade</option><option value="upgrade">Upgrade, taking the new chart defaults</option><option value="upgrade-reuse">Upgrade, keeping the installed values</option></select></label>
        </div>
        <p class="help" v-if="config.action === 'upgrade'">Applies new chart defaults, then installed values, then your overrides. Requires Helm 3.14 or newer.</p>
        <fieldset><legend>Extra environment variables</legend><div v-for="(item, index) in env" :key="index" class="my-2 flex gap-2"><input v-model="item.name" placeholder="NAME" :aria-label="`Variable ${index + 1} name`"><input v-model="item.value" placeholder="Value" :aria-label="`Variable ${index + 1} value`"><button type="button" @click="env.splice(index, 1)" :aria-label="`Remove variable ${index + 1}`">Remove</button></div><button type="button" class="mt-2" @click="env.push({ name: '', value: '' })">+ Add variable</button></fieldset>
        <div class="flex items-center justify-between"><h3 class="font-semibold">Chart values</h3><button type="button" @click="overrides = {}; env = []">Reset to chart defaults</button></div>
        <input v-model="search" type="search" placeholder="Search values by name or description" aria-label="Search chart values">
        <p v-if="loading" role="status">Loading chart values…</p>
        <div v-if="error" role="alert"><p>{{ error }}</p><button type="button" class="mt-2" @click="load">Retry catalog</button></div>
        <p v-if="unknownVersion && chart" class="help">Custom version: fields use {{ entry.version }} as a reference. Verify these values against your custom chart.</p>
        <p v-if="chart" class="help">{{ visibleFields.length }} of {{ fields.length }} values · reference chart {{ entry.version }}. Only overrides appear in the output.</p>
        <div v-for="field in visibleFields" :key="field.path" class="value-field">
          <label><span class="font-mono text-xs">{{ field.path }}</span><span v-if="field.path in overrides" class="ml-2 text-xs text-emerald-600">Edited</span>
            <select v-if="field.type === 'boolean' || field.choices" :value="overrides[field.path] ?? displayValue(field)" @change="overrides[field.path] = $event.target.value"><option v-for="choice in field.choices || [true, false]" :key="String(choice)" :value="String(choice)">{{ choice }}</option></select>
            <textarea v-else-if="field.type === 'yaml'" rows="4" :value="overrides[field.path] ?? displayValue(field)" @input="overrides[field.path] = $event.target.value" spellcheck="false" />
            <input v-else :type="field.type === 'number' ? 'number' : 'text'" :value="overrides[field.path] ?? displayValue(field)" @input="overrides[field.path] = $event.target.value" spellcheck="false">
          </label>
          <p v-if="field.description" class="help">{{ field.description }}</p>
          <button v-if="field.path in overrides" type="button" class="mt-1" @click="delete overrides[field.path]">Use default</button>
        </div>
      </div>
      <div class="min-w-0"><div class="sticky top-4 space-y-4">
        <p v-if="output.error" role="alert" class="text-red-600">{{ output.error }}</p>
        <template v-if="output.command">
          <article v-for="block in [{ title: 'Chart repo', text: channel.repo }, { title: 'Add the repo', text: output.repo }, { title: 'Helm command', text: output.command }, { title: 'values.yaml', text: output.yaml }]" :key="block.title" class="output-card">
            <div class="mb-3 flex items-center justify-between gap-2"><h3 class="font-semibold">{{ block.title }}</h3><button type="button" @click="copy(block.text)" :aria-label="`Copy ${block.title}`">Copy</button></div><pre>{{ block.text }}</pre>
          </article>
          <button type="button" @click="download">Download values.yaml</button>
          <p class="help">In flag mode, lists and maps go in values.yaml; scalars and environment variables go in the command. Run the generated command in your target cluster context.</p>
        </template>
      </div></div>
    </div>
  </div>
</template>

