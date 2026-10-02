<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { apiFetch } from './store.js';
import { readJSON } from './read-json.mjs';
import { downstreamVersionChoices, compareDownstreamVersions, generateDownstreamName, linodeMachineChoices, linodeMachineDefaults, machineFieldLabel } from './downstream-options.mjs';
import { machineValues, operationEvidence } from './rancher-operations.mjs';
import { rancherConnectionURL } from './rancher-connection.mjs';
import RancherTokenGenerator from './RancherTokenGenerator.vue';
import RancherUpgrade from './RancherUpgrade.vue';
const props = defineProps({ cluster: { type: Object, required: true }, unifiedHistory: Boolean });
const emit = defineEmits(['history', 'completed']);
const open = ref(false), tab = ref('upgrade'), busy = ref(false), error = ref(''), notice = ref('');
const history = ref([]);
const authMode = ref('password'), connectionBusy = ref(false);
const token = ref(''), insecure = ref(false), provider = ref('linode'), distro = ref('rke2');
const driverState = ref(null), enablingDriver = ref(false);
const options = ref(null), kubernetes = ref(''), name = ref(''), quantity = ref(1), credentialID = ref('');
const useEnvironment = ref(false), environmentStatus = ref(null);
const credentials = ref({}), machine = ref({}), creationReviewed = ref(false), showAdvanced = ref(false);
const issueContext = ref('');
const namePrefix = ref(''), showOlderVersions = ref(false), providerCatalog = ref(null), providerError = ref('');
const versionChoices = computed(() => downstreamVersionChoices(options.value?.versions, showOlderVersions.value, kubernetes.value));
const missingMachineFields = computed(() => fields.value.some(([key, field]) => field.required && (machine.value[key] === undefined || machine.value[key] === null || machine.value[key] === '') && field.default == null));
const providerSelectionInvalid = computed(() => Object.entries(providerChoices.value).some(([key, choices]) => machine.value[key] && !choices.some(item => item.id === machine.value[key])));
const providerChoices = computed(() => linodeMachineChoices(providerCatalog.value));
function generateName() { try { name.value = generateDownstreamName(namePrefix.value); error.value = ''; } catch (err) { error.value = err.message; } }
const providerInput = () => ({...connection(), credentialId:credentialID.value, useEnvironment:!credentialID.value && useEnvironment.value, credentials:credentialID.value || useEnvironment.value ? {} : credentials.value});
async function fetchProviderChoices() {
  if (provider.value !== 'linode') return;
  providerError.value = '';
  const input = providerInput(), signature = JSON.stringify(input);
  try {
    const catalog = await request('provider-options', input);
    if (signature !== JSON.stringify(providerInput())) return;
    providerCatalog.value = catalog;
    const defaults = linodeMachineDefaults(catalog);
    const credentialRegion = options.value?.credentials.find(credential => credential.id === credentialID.value)?.defaultRegion;
    if (catalog.regions?.some(region => region.id === credentialRegion)) defaults.region = credentialRegion;
    for (const [key,value] of Object.entries(defaults)) {
      if (options.value?.fields[key] && !machine.value[key]) machine.value[key] = value;
    }
  } catch (err) { if (signature === JSON.stringify(providerInput())) providerError.value = err.message; }
}
const loadProviderChoices = () => work(fetchProviderChoices);
watch([credentialID, useEnvironment, credentials, provider, token], () => { providerCatalog.value = null; providerError.value = ''; creationReviewed.value = false; }, {deep:true});
watch(credentialID, value => { if (value && provider.value === 'linode') void loadProviderChoices(); });

let timer, disposed = false;
const nodeDriversURL = computed(() => { const base = rancherConnectionURL(props.cluster.rancherUrl); return base ? `${base}/dashboard/c/_/manager/nodeDriver` : ''; });
const running = computed(() => history.value.some(record => record.status === 'running'));
const fieldOrder = {region:0, instanceType:1, image:2, createPrivateIp:3};
const fields = computed(() => Object.entries(options.value?.fields || {}).sort(([a,av],[b,bv]) => (fieldOrder[a] ?? 10) - (fieldOrder[b] ?? 10) || Number(Boolean(bv.required)) - Number(Boolean(av.required)) || a.localeCompare(b)));
const commonFields = new Set(['region', 'instanceType', 'image', 'size', 'ami', 'vpcId', 'subnetId', 'securityGroup', 'zone', 'createPrivateIp']);
const displayedFields = computed(() => fields.value.filter(([key, field]) => showAdvanced.value || field.required || commonFields.has(key)));
const request = async (action, values = {}) => {
  const clusterId = props.cluster.id, rancherUrl = props.cluster.rancherUrl;
  const result = await readJSON(signal => apiFetch('/api/rancher/operations', {
    method: 'POST', signal, body: JSON.stringify({ action, clusterId, ...values }),
  }), { label: 'Rancher workflow', timeoutMs: 100000 });
  if (clusterId !== props.cluster.id || rancherUrl !== props.cluster.rancherUrl) throw new Error('Rancher changed during the request. Load options for the selected Rancher again.');
  return result;
};
async function work(fn) {
  if (busy.value) return;
  busy.value = true; error.value = ''; notice.value = '';
  try { await fn(); } catch (err) { error.value = err.message; }
  finally { busy.value = false; }
}
async function refreshHistory() {
  const clusterId = props.cluster.id;
  const data = await readJSON(signal => apiFetch(`/api/rancher/operations?clusterId=${encodeURIComponent(clusterId)}`, { signal }));
  if (!disposed && clusterId === props.cluster.id) {
    const records = data.records || [];
    const completed = records.some(record => record.finished && !history.value.some(previous => previous.id === record.id && previous.finished === record.finished));
    history.value = records;
    if (completed) emit('completed');
  }
}
async function poll() {
  try { await refreshHistory(); } catch (err) { if (!disposed) error.value = `History refresh: ${err.message}`; }
  if (!disposed && open.value) timer = setTimeout(poll, 5000);
}
watch(open, value => { clearTimeout(timer); if (value) void poll(); });
onBeforeUnmount(() => { disposed = true; clearTimeout(timer); token.value = ''; credentials.value = {}; });
watch([() => props.cluster.id, () => props.cluster.rancherUrl], () => { token.value = ''; options.value = null; history.value = []; credentials.value = {}; creationReviewed.value = false; });
watch([token, insecure, provider, distro], () => { driverState.value = null; options.value = null; credentialID.value = ''; credentials.value = {}; machine.value = {}; creationReviewed.value = false; useEnvironment.value = false; environmentStatus.value = null; });
watch([name, quantity, kubernetes, credentialID, credentials, machine, useEnvironment], () => { creationReviewed.value = false; }, { deep: true });
const connection = () => ({ token: token.value, insecure: insecure.value, provider: provider.value, distribution: distro.value });
const loadCatalog = (distribution, channel) => readJSON(signal => apiFetch(`/api/helm-lab/catalog?distribution=${distribution}&channel=${channel}`, { signal }), { timeoutMs: 60000 });
function openWorkflow(value) { tab.value = value; open.value = true; error.value = ''; notice.value = ''; }
async function upgradeStarted() { tab.value = 'history'; await work(refreshHistory); }
async function connected(created) { token.value = created.token; await loadOptions(); }
async function fetchOptions() {
  options.value = null; driverState.value = null;
  // Schema access can be allowed even when node-driver management is not.
  try { driverState.value = await request('driver-status', connection()); } catch { /* Let schema discovery report its own result. */ }
  if (driverState.value?.active === false) return;
  options.value = await request('options', connection());
  if (!options.value.versions.includes(kubernetes.value)) kubernetes.value = options.value.defaultVersion || '';
  if (!namePrefix.value) namePrefix.value = options.value.namePrefix || '';
  showOlderVersions.value = false;
  if (credentialID.value && !options.value.credentials.some(item => item.id === credentialID.value)) credentialID.value = '';
  const previousMachine = machine.value;
  machine.value = {};
  for (const [key, field] of Object.entries(options.value.fields)) {
    if (previousMachine[key] !== undefined) { machine.value[key] = previousMachine[key]; continue; }
    if (field.default !== undefined && field.default !== null) machine.value[key] = typeof field.default === 'object' ? JSON.stringify(field.default) : field.default;
  }
}
async function loadOptions() { await work(fetchOptions); }
async function enableDriver() {
  await work(async () => {
    enablingDriver.value = true;
    try {
      await request('enable-driver', connection());
      await fetchOptions();
      notice.value = 'Node driver enabled. Available machine options have been refreshed.';
    } finally { enablingDriver.value = false; }
  });
}
async function pullEnvironment() {
  await work(async () => {
    environmentStatus.value = await request('environment', { provider: provider.value });
    if (environmentStatus.value.available) { useEnvironment.value = true; credentials.value = {}; await fetchProviderChoices(); }
  });
}
async function createCluster() {
  await work(async () => {
    await request('downstream', { ...connection(), name: name.value, quantity: Number(quantity.value), kubernetesVersion: kubernetes.value,
      credentialId: credentialID.value, useEnvironment: !credentialID.value && useEnvironment.value, credentials: credentialID.value || useEnvironment.value ? {} : credentials.value,
      machine: machineValues(options.value.fields, machine.value), confirmed: creationReviewed.value });
    credentials.value = {}; token.value = ''; tab.value = 'history'; await refreshHistory();
  });
}
function exportEvidence(record, format) {
  const content = format === 'json' ? JSON.stringify(record, null, 2) : `${issueContext.value ? `Issue: ${issueContext.value}\n\n` : ''}${operationEvidence(record)}`;
  const url = URL.createObjectURL(new Blob([content], { type: format === 'json' ? 'application/json' : 'text/markdown' }));
  const link = document.createElement('a'); link.href = url; link.download = `rancher-${record.kind}-${record.id}.${format === 'json' ? 'json' : 'md'}`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
}
async function copyEvidence(record) { await work(async () => { await navigator.clipboard.writeText(operationEvidence(record)); notice.value = 'Issue evidence copied.'; }); }
function podsFor(record) {
  const event = [...record.events].reverse().find(event => event.message.startsWith('Pods: '));
  if (!event) return record.plan?.before?.pods || [];
  try { return JSON.parse(event.message.slice(6)); } catch { return []; }
}
</script>

<template>
  <section class="rw-operations" aria-label="Manage deployed Rancher">
    <div class="rw-launchers" aria-label="Rancher actions">
      <button type="button" class="rw-launch" :aria-expanded="open && tab === 'upgrade'" @click="openWorkflow('upgrade')"><span aria-hidden="true">↗</span> Upgrade Rancher</button>
      <button type="button" class="rw-launch" :aria-expanded="open && tab === 'downstream'" @click="openWorkflow('downstream')"><span aria-hidden="true">＋</span> Create downstream</button>
      <button type="button" class="rw-history-link" :aria-expanded="open && tab === 'history'" @click="props.unifiedHistory ? (open = false, emit('history')) : openWorkflow('history')">Cluster history <span aria-hidden="true">→</span></button>
    </div>
    <div v-if="open" class="rw-body" :class="{ 'rw-upgrade-body': tab === 'upgrade' }">
      <header class="rw-heading"><div><span class="rw-eyebrow">{{ tab === 'upgrade' ? 'RANCHER UPGRADE' : tab === 'downstream' ? 'DOWNSTREAM CLUSTER' : 'OPERATION HISTORY' }}</span><h3>{{ tab === 'upgrade' ? 'Choose your next Rancher' : tab === 'downstream' ? 'Create a downstream cluster' : 'History & evidence' }}</h3><p>{{ cluster.rancherUrl }}</p></div><button type="button" aria-label="Close Rancher workflow" class="rw-close" @click="open = false">✕</button></header>
      <div v-if="error" role="alert" class="rw-error"><p>{{ error }}</p><p v-if="tab === 'downstream' && nodeDriversURL"><a :href="nodeDriversURL" target="_blank" rel="noopener noreferrer">Open Node Drivers in Rancher ↗</a> · <a :href="nodeDriversURL.replace('/nodeDriver', '/provisioning.cattle.io.cluster/create')" target="_blank" rel="noopener noreferrer">Continue in Rancher ↗</a><span v-if="token"> · Your token is still available. After fixing the issue, click Load options from Rancher.</span></p></div>
      <p v-if="notice" role="status">{{ notice }}</p>
      <p v-if="running" role="status" class="rw-note">An operation is running. Keep Runway open; follow its progress in History & evidence.</p>
      <RancherUpgrade v-if="tab === 'upgrade'" :key="cluster.id" :cluster="cluster" :request="request" :load-catalog="loadCatalog" :running="running" @started="upgradeStarted" />
      <fieldset :disabled="busy || running" v-if="tab === 'downstream'">
        <p>Create an RKE2 or K3s cluster through this deployed Rancher. Available releases and machine fields come from its live API. Runway checks the selected node driver and can enable it before loading machine options.</p>
        <div class="rancher-methods" role="group" aria-label="Connection method"><button type="button" :aria-pressed="authMode === 'password'" :disabled="connectionBusy" @click="authMode = 'password'">Password sign-in</button><button type="button" :aria-pressed="authMode === 'token'" :disabled="connectionBusy" @click="authMode = 'token'">API token</button></div>
        <label v-if="authMode === 'token'">Rancher API token<input v-model.trim="token" type="password" autocomplete="off" placeholder="token-…:…"></label>
        <RancherTokenGenerator v-if="authMode === 'password'" purpose="downstream" :url="cluster.rancherUrl" :credential-available="!!token" :insecure="insecure" :disabled="busy || running" @busy="connectionBusy = $event" @generated="connected" />
        <label class="rw-check"><input type="checkbox" v-model="insecure" :disabled="connectionBusy">Allow this Rancher's self-signed certificate for this connection.</label>
        <div class="rw-grid">
          <label>Cloud provider<select v-model="provider" :disabled="connectionBusy"><option value="linode">Linode</option><option value="amazonec2">AWS EC2</option></select></label>
          <label>Distribution<select v-model="distro" :disabled="connectionBusy"><option value="rke2">RKE2</option><option value="k3s">K3s</option></select></label>
        </div>
        <button type="button" :disabled="!token || connectionBusy" @click="loadOptions">Load options from Rancher</button>
        <div v-if="driverState?.active === false" class="rw-driver-needed" role="status">
          <h4>{{ provider === 'linode' ? 'Linode' : 'Amazon EC2' }} node driver is inactive</h4>
          <p>Rancher needs this driver enabled before it can offer machine options. Enable the existing driver here, then continue configuring your cluster.</p>
          <button type="button" class="rw-primary" @click="enableDriver">{{ enablingDriver ? 'Enabling driver and waiting for options…' : `Enable ${provider === 'linode' ? 'Linode' : 'Amazon EC2'} driver` }}</button>
          <p><a :href="nodeDriversURL" target="_blank" rel="noopener noreferrer">View Node Drivers in Rancher ↗</a></p>
        </div>
        <template v-if="options">
          <div class="rw-grid">
            <div class="rw-name"><label>Cluster name<input v-model.trim="name" placeholder="my-downstream" maxlength="40"></label><div class="rw-name-generator"><input v-model.trim="namePrefix" aria-label="Cluster name prefix" placeholder="Your initials" maxlength="33"><button type="button" @click="generateName">Generate name</button></div></div>
            <div><label>Kubernetes version<select v-model="kubernetes"><option value="" disabled>Choose a version</option><option v-for="v in versionChoices" :key="v" :value="v">{{ v }}{{ v === options.defaultVersion ? ' · Rancher default' : options.defaultVersion && compareDownstreamVersions(v, options.defaultVersion) > 0 ? ' · Experimental' : '' }}</option></select></label><label class="rw-check"><input type="checkbox" v-model="showOlderVersions">Show older patches</label></div>
            <label>All-role nodes<select v-model.number="quantity"><option :value="1">1 · development</option><option :value="3">3</option><option :value="5">5</option></select></label>
            <label>Cloud credential<select v-model="credentialID"><option value="">Add a new credential to Rancher</option><option v-for="cred in options.credentials" :key="cred.id" :value="cred.id">{{ cred.name || cred.id }}</option></select></label>
          </div>
          <p v-for="warning in options.warnings || []" :key="warning" class="rw-note">{{ warning }}</p>
          <template v-if="!credentialID">
            <button type="button" @click="pullEnvironment">Use Runway environment credentials</button>
            <p v-if="environmentStatus" role="status">{{ environmentStatus.message }}</p>
            <label v-if="environmentStatus?.available" class="rw-check"><input type="checkbox" v-model="useEnvironment">Use configured {{ provider === 'linode' ? 'Linode' : 'AWS' }} credentials when creating the Rancher cloud credential</label>
            <template v-if="!useEnvironment">
            <label v-if="provider === 'linode'">Linode token<input type="password" v-model="credentials.token" autocomplete="off"></label>
            <template v-else><label>AWS access key<input type="password" v-model="credentials.accessKey" autocomplete="off"></label><label>AWS secret key<input type="password" v-model="credentials.secretKey" autocomplete="off"></label></template>
            </template>
            <p>New cloud credentials are stored in Rancher. Runway keeps the supplied secrets in memory only and excludes them from history and exports.</p>
          </template>
          <h4>Machine configuration</h4>
          <template v-if="provider === 'linode'"><button type="button" @click="loadProviderChoices">{{ providerCatalog ? 'Refresh regions, sizes & images' : 'Load regions, sizes & images' }}</button><p v-if="providerError" role="alert" class="rw-error">{{ providerError }}</p><p v-if="providerCatalog">Choices from Linode · defaults follow Rancher's Linode form when available. Review the size and OS image before creating.</p><p v-else>Choose a cloud credential or enter a Linode token to load provider choices. You can also enter exact values below.</p></template>
          <p v-else>Defaults supplied by Rancher are prefilled. Account-specific AWS choices require exact values; use Rancher for its full network picker.</p>
          <div class="rw-grid">
            <label v-for="[key, field] in displayedFields" :key="key">{{ machineFieldLabel(key) }}{{ field.required ? ' *' : '' }}
              <select v-if="providerChoices[key]?.length" v-model="machine[key]"><option value="">Choose {{ machineFieldLabel(key).toLowerCase() }}</option><option v-if="machine[key] && !providerChoices[key].some(item => item.id === machine[key])" :value="machine[key]">{{ machine[key] }} · not in refreshed catalog</option><option v-for="item in providerChoices[key]" :key="item.id" :value="item.id">{{ item.label }} · {{ item.id }}{{ item.deprecated ? ' · Deprecated' : '' }}</option></select>
              <select v-else-if="field.options?.length" v-model="machine[key]"><option value="">Use default</option><option v-for="option in field.options" :key="option">{{ option }}</option></select>
              <select v-else-if="field.type === 'boolean'" v-model="machine[key]"><option value="">Use default</option><option :value="true">True</option><option :value="false">False</option></select>
              <input v-else-if="['string','password','int','integer','float','number'].includes(field.type)" v-model="machine[key]" :type="field.type === 'password' || /token|secret|password/i.test(key) ? 'password' : 'text'" :placeholder="field.default == null ? (field.required ? `Enter ${machineFieldLabel(key).toLowerCase()}` : 'Leave unset for driver default') : String(field.default)" autocomplete="off">
              <textarea v-else v-model="machine[key]" placeholder="JSON value"></textarea>
              <small v-if="field.description && showAdvanced">{{ field.description }}</small>
            </label>
          </div>
          <label class="rw-check"><input type="checkbox" v-model="showAdvanced">Advanced · all {{ fields.length }} machine fields</label>
          <div class="rw-review">
            <h4>Review cluster creation</h4><p v-if="missingMachineFields || providerSelectionInvalid" class="rw-note">Choose valid values for the required machine fields before creating.</p><p>{{ name || 'Choose a name' }} · {{ provider }} · {{ kubernetes }} · {{ quantity }} all-role node(s) · fleet-default</p>
            <p v-if="machine.region || machine.instanceType || machine.image">{{ machine.region }} · {{ machine.instanceType }} · {{ machine.image || machine.ami }}</p><p>Rancher will provision billable cloud resources. A failed operation retains partial resources and records their IDs for inspection and cleanup.</p>
            <label class="rw-check"><input type="checkbox" v-model="creationReviewed">I reviewed these settings and want to create these resources.</label>
            <button type="button" class="rw-primary" :disabled="!creationReviewed || !name || !kubernetes || missingMachineFields || providerSelectionInvalid" @click="createCluster">Create downstream cluster</button>
          </div>
        </template>
      </fieldset>
      <div v-if="tab === 'history'">
        <p>History is saved locally and survives app restarts. An interrupted operation has an unknown outcome; inspect Rancher before retrying.</p>
        <label>Issue URL or reference to include in Markdown<input v-model="issueContext" placeholder="https://…/issues/123"></label>
        <p v-if="!history.length">No operations recorded for this Rancher yet.</p>
        <article v-for="record in history" :key="record.id" class="rw-review">
          <h4>{{ record.kind === 'upgrade' ? 'Rancher upgrade' : 'Downstream creation' }} · {{ record.status }}</h4>
          <p>{{ record.from ? `${record.from} → ` : '' }}{{ record.to }}</p><p>{{ new Date(record.started).toLocaleString() }} · {{ record.id }}</p>
          <div class="rw-actions"><button type="button" @click="exportEvidence(record, 'md')">Save issue evidence</button><button type="button" @click="copyEvidence(record)">Copy for issue</button><button type="button" @click="exportEvidence(record, 'json')">Export JSON</button></div>
          <table v-if="record.kind === 'upgrade' && podsFor(record).length"><thead><tr><th>Pod</th><th>Status</th><th>Ready</th><th>Restarts</th></tr></thead><tbody><tr v-for="pod in podsFor(record)" :key="`${pod.namespace}/${pod.name}`"><td>{{ pod.name }}</td><td>{{ pod.status }}</td><td>{{ pod.ready }}</td><td>{{ pod.restarts }}</td></tr></tbody></table>
          <details :open="record.status === 'running'"><summary>Checks, pod changes & timeline ({{ record.events.length }})</summary><pre class="rw-timeline">{{ record.events.map(event => `${new Date(event.at).toLocaleTimeString()}  ${event.message}`).join('\n\n') }}</pre></details>
          <p v-if="record.resources?.length">Created resources: {{ record.resources.join(' · ') }}</p>
        </article>
      </div>
      <p v-if="busy" role="status">Working…</p>
    </div>
  </section>
</template>

<style scoped>
.rw-operations{--runway-ink:#263341;--runway-muted:#66717f;--runway-accent:#047857;--runway-card:#fff;--runway-input:#fff;--runway-border:#d4dce0;--runway-control:#d4dce0;--runway-soft:#f5f7f8;--runway-gold:#a16207;--runway-error:#dc2626;margin-top:1rem;font-size:.875rem;color:inherit}.rw-open{color:#059669}.rw-body{margin-top:1rem;padding:1.25rem;border:1px solid #71717a50;border-radius:1rem}.rw-body header{margin-bottom:1rem}.rw-body h3{font-size:1.2rem;font-weight:700}.rw-body h4{font-weight:700;margin-bottom:.5rem}.rw-body p{margin:.65rem 0;line-height:1.6;overflow-wrap:anywhere}.rw-body nav,.rw-actions{display:flex;gap:.5rem;flex-wrap:wrap;margin:.75rem 0}.rw-body:not(.rw-upgrade-body) button,.rw-open{border:1px solid #71717a60;border-radius:.5rem;padding:.6rem .8rem;font-weight:600}.rw-body button[aria-pressed=true],.rw-primary{background:#047857;color:white}.rw-body button:disabled{opacity:.45;cursor:not-allowed}.rw-body:not(.rw-upgrade-body) fieldset{min-width:0;border:0;padding:0}.rw-body:not(.rw-upgrade-body) label{display:flex;flex-direction:column;gap:.35rem;margin:.65rem 0;font-weight:500}.rw-body:not(.rw-upgrade-body) input:not([type=checkbox]),.rw-body:not(.rw-upgrade-body) select,.rw-body:not(.rw-upgrade-body) textarea{width:100%;min-width:0;border:1px solid #71717a60;border-radius:.4rem;background:transparent;padding:.55rem;color:inherit}.rw-body select option{color:#18181b;background:white}.rw-body:not(.rw-upgrade-body) .rw-check{flex-direction:row;align-items:start;gap:.65rem}.rw-check input{margin-top:.3rem}.rw-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:0 1rem}.rw-driver-needed{padding:18px;border:1px solid #d9a15e;border-radius:10px;margin:16px 0;background:#d9a15e0a}.rw-driver-needed a{text-decoration:underline}.rw-review{padding:1rem;border:1px solid #71717a50;border-radius:.75rem;margin:1rem 0}.rw-note{border-left:3px solid #d97706;padding-left:.75rem}.rw-error{color:#dc2626}.rw-error a{color:inherit;text-decoration:underline;font-weight:600}.rw-body:not(.rw-upgrade-body) ul{padding-left:1.2rem;list-style:disc}.rw-body:not(.rw-upgrade-body) li{margin:.4rem 0}.rw-body details{margin:.8rem 0}.rw-body summary{cursor:pointer;font-weight:600}.rw-mono,.rw-timeline{font-family:ui-monospace,monospace;font-size:.75rem;overflow-wrap:anywhere}.rw-timeline{max-height:26rem;overflow:auto;white-space:pre-wrap;padding:1rem;background:#71717a10;border-radius:.5rem}.rw-body table{width:100%;font-size:.75rem;margin:1rem 0}.rw-body th,.rw-body td{text-align:left;padding:.35rem;overflow-wrap:anywhere}.rw-body small{font-weight:400;line-height:1.5}

.rw-launchers{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.rw-launch,.rw-history-link{display:inline-flex;align-items:center;gap:10px;font-size:13px;font-weight:600;border-radius:10px;padding:11px 16px}.rw-launch{border:1px solid #d4dce0;background:white;color:#34454f}.rw-launch span{font-size:18px;color:#047857}.rw-launch:hover,.rw-launch[aria-expanded=true]{border-color:#059669;background:#ecfdf5;color:#047857}.rw-history-link{color:#66717f}.rw-history-link:hover{color:#047857}.rw-launch:focus-visible,.rw-history-link:focus-visible,.rw-close:focus-visible{outline:2px solid #059669;outline-offset:3px}.rw-body{background:#fff;color:#263341;padding:28px;margin-top:16px}.rw-heading{display:flex;justify-content:space-between;gap:20px;align-items:flex-start;max-width:980px;margin:0 auto 22px!important}.rw-eyebrow{font-size:10px;font-weight:700;letter-spacing:1.5px;color:#66717f}.rw-heading h3{font-size:23px;letter-spacing:-.5px;margin-top:6px}.rw-heading p{font-size:12px;color:#66717f;margin-top:6px}.rw-close{border:1px solid #d4dce0;border-radius:8px;padding:7px 11px;color:#66717f}:global(.dark .rw-operations){--runway-ink:#e5ebf0;--runway-muted:#a0aab7;--runway-accent:#7ddbc0;--runway-card:#191f27;--runway-input:#191f27;--runway-border:#35404a;--runway-control:#35404a;--runway-soft:#202933;--runway-gold:#d9a15e;--runway-error:#fca5a5}.rw-body>fieldset{max-width:980px;margin:auto}:global(.dark .rw-body){background:#191f27;color:#e5ebf0;border-color:#35404a}:global(.dark .rw-launch){background:#202933;border-color:#35404a;color:#e5ebf0}:global(.dark .rw-launch span),:global(.dark .rw-launch[aria-expanded=true]){color:#7ddbc0}:global(.dark .rw-launch:hover),:global(.dark .rw-launch[aria-expanded=true]){border-color:#7ddbc0;background:#1d3331}:global(.dark .rw-heading p),:global(.dark .rw-eyebrow),:global(.dark .rw-history-link),:global(.dark .rw-close){color:#a0aab7}@media(max-width:640px){.rw-body{padding:16px}.rw-heading h3{font-size:20px}}
</style>

<style scoped>
.rw-name-generator{display:flex;gap:8px;align-items:center}.rw-body .rw-name-generator input{width:100px!important}.rw-name-generator button{white-space:nowrap}.rw-name-generator{margin-bottom:12px}
</style>
