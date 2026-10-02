<script setup>
import RancherImageEvidence from './RancherImageEvidence.vue';
import { discoverHeadSources, imageRegistry, imageCommitTag } from './upgrade-sources.mjs';
import { computed, nextTick, onMounted, ref, watch } from 'vue';
import { defaultHeadChart, headTargets, imageTag, releaseTargets, upgradeSearchHints } from './upgrade-targets.mjs';

const props = defineProps({ cluster: { type: Object, required: true }, request: { type: Function, required: true }, loadCatalog: { type: Function, required: true }, running: Boolean });
const emit = defineEmits(['started']);
const installed = ref(null), catalog = ref(null), heads = ref([]), plan = ref(null);
const mode = ref('stable'), distribution = ref('community'), channel = ref('ga');
const chartVersion = ref(''), image = ref(''), search = ref(''), advanced = ref(false);
const sources = ref([]), registry = ref('all'), sourceResults = ref([]);
const sourceLabel = image => sources.value.find(source => source.registry === imageRegistry(image))?.label || imageRegistry(image);
const busy = ref(''), error = ref(''), backup = ref(false), experimental = ref(false);
const errorBox = ref(null), reviewHeading = ref(null);
async function focusResult(target) {
  await nextTick();
  target.value?.focus({ preventScroll: true });
  target.value?.scrollIntoView({ behavior: 'smooth', block: 'center' });
}
const choices = [
  { id: 'stable', title: 'Stable release', description: 'Published releases & patches', icon: '✓' },
  { id: 'preview', title: 'RC / Alpha', description: 'Test an upcoming release', icon: '◈' },
  { id: 'head', title: 'Head', description: 'Browse development builds', icon: '↗' },
  { id: 'image', title: 'Exact image', description: 'Paste a repository & tag', icon: '▣' },
];
const isHead = computed(() => mode.value === 'head');
const isImage = computed(() => isHead.value || mode.value === 'image');
const releases = computed(() => releaseTargets(installed.value?.version, catalog.value?.versions || [], mode.value !== 'stable'));
const filteredReleases = computed(() => releases.value.filter(item => `${item.target} ${item.version}`.toLowerCase().includes(search.value.toLowerCase())));
const allHeads = computed(() => headTargets(installed.value?.version, heads.value.filter(head => registry.value === 'all' || imageRegistry(head) === registry.value), search.value));
const displayedHeads = computed(() => allHeads.value.slice(0, 40));
const selectedRelease = computed(() => releases.value.find(item => item.version === chartVersion.value));
const targetLabel = computed(() => isImage.value ? imageTag(image.value) : selectedRelease.value?.target);
const canReview = computed(() => installed.value && catalog.value && chartVersion.value && (isImage.value ? Boolean(image.value) : selectedRelease.value && !selectedRelease.value.blocked));
const selectionKey = computed(() => [mode.value, distribution.value, channel.value, chartVersion.value, image.value].join('|'));
watch(selectionKey, () => { plan.value = null; backup.value = false; experimental.value = false; }, { flush: 'sync' });

async function work(label, fn) {
  if (busy.value) return;
  busy.value = label; error.value = '';
  try { await fn(); } catch (err) { error.value = err.message || String(err); await focusResult(errorBox); }
  finally { busy.value = ''; }
}
async function fetchTargets() {
  catalog.value = null; heads.value = []; sourceResults.value = []; chartVersion.value = ''; if (mode.value !== 'image') image.value = ''; plan.value = null;
  const [charts, published] = await Promise.all([
    props.loadCatalog(distribution.value, channel.value),
    isHead.value ? discoverHeadSources(sources.value, props.request) : Promise.resolve([]),
  ]);
  catalog.value = charts;
  if (isImage.value) {
    sourceResults.value = published;
    heads.value = published.flatMap(source => source.images);
    chartVersion.value = defaultHeadChart(installed.value.version, charts.versions || []);
    // A published global head remains explicitly selected by the user.
  } else {
    chartVersion.value = releases.value.find(item => !item.blocked)?.version || '';
  }
}
async function discover() {
  await work('Checking installed Rancher and available targets…', async () => {
    installed.value = null; catalog.value = null; plan.value = null;
    const [current, available] = await Promise.all([props.request('inspect'), props.request('image-sources')]);
    installed.value = current; sources.value = available.sources || [];
    await fetchTargets();
  });
}
async function chooseMode(value) {
  if (busy.value || props.running || mode.value === value) return;
  mode.value = value; search.value = ''; image.value = ''; advanced.value = false;
  channel.value = value === 'preview' ? 'rc' : 'ga';
  if (installed.value) await work('Loading available targets…', fetchTargets);
  else await discover();
}
async function reloadTargets() {
  if (installed.value) await work('Loading available targets…', fetchTargets);
  else await discover();
}
async function review() {
  if (!canReview.value) return;
  await work('Checking readiness, version path, image digests and Helm dry-run…', async () => {
    plan.value = null; backup.value = false; experimental.value = false;
    plan.value = await props.request('plan', { distribution: distribution.value, channel: channel.value, version: chartVersion.value, image: isImage.value ? image.value : '' });
    await focusResult(reviewHeading);
  });
}
async function upgrade() {
  if (!plan.value || !backup.value || (plan.value.experimental && !experimental.value)) return;
  await work('Starting the reviewed upgrade…', async () => {
    await props.request('upgrade', { planId: plan.value.id, backupConfirmed: backup.value, experimentalConfirmed: experimental.value });
    plan.value = null; emit('started');
  });
}
onMounted(() => { if (props.cluster.kubeconfigPath && !props.running) void discover(); });
</script>

<template>
  <div class="upgrade-flow" :aria-busy="Boolean(busy)">
    <div v-if="!cluster.kubeconfigPath" class="upgrade-message">This upgrade flow needs the management kubeconfig. Docker installations cannot be upgraded here.</div>
    <template v-else>
      <ol class="upgrade-steps" aria-label="Upgrade progress">
        <li :class="{ active: !plan }"><span>1</span> Choose target</li><li :class="{ active: plan }"><span>2</span> Review & upgrade</li><li><span>3</span> Follow progress</li>
      </ol>
      <div class="upgrade-current">
        <div><span class="upgrade-eyebrow">CURRENTLY INSTALLED</span><strong>{{ installed?.version || 'Checking Rancher…' }}</strong><small v-if="installed">Helm revision {{ installed.revision }} · {{ cluster.name }}</small></div>
        <button type="button" :disabled="Boolean(busy) || running" @click="discover">Refresh</button>
      </div>
      <div v-if="error" ref="errorBox" tabindex="-1" class="upgrade-error" role="alert"><strong>Couldn’t complete this check</strong><p>{{ error }}</p><button v-if="!catalog" type="button" :disabled="Boolean(busy) || running" @click="discover">Try again</button></div>
      <p v-if="busy" class="upgrade-loading" role="status"><span class="spinner"></span>{{ busy }}</p>
      <fieldset :disabled="Boolean(busy) || running">
        <template v-if="!plan">
          <h4>What do you want to upgrade to?</h4>
          <div class="upgrade-modes">
            <button v-for="choice in choices" :key="choice.id" type="button" :aria-pressed="mode === choice.id" @click="chooseMode(choice.id)">
              <span class="upgrade-mode-icon" aria-hidden="true">{{ choice.icon }}</span><strong>{{ choice.title }}</strong><small>{{ choice.description }}</small>
            </button>
          </div>
          <div v-if="mode === 'preview'" class="upgrade-inline"><label>Prerelease channel<select v-model="channel" @change="reloadTargets"><option value="rc">Release candidate</option><option value="alpha">Alpha</option></select></label><p>Experimental builds. Readiness and exact versions are checked before upgrading.</p></div>
          <div v-if="isHead" class="upgrade-source">
            <label>Filter image sources<select v-model="registry"><option value="all">All available sources</option><option v-for="source in sources" :key="source.registry" :value="source.registry">{{ source.label }} · {{ source.registry }}</option></select></label>
            <p class="upgrade-help">Community and staging builds are listed together. Each option shows its registry; select one to resolve its exact digest and commit.</p>
            <details v-if="sourceResults.length" class="upgrade-source-status"><summary>Registry results · {{ sourceResults.filter(source => !source.error).length }} of {{ sourceResults.length }} reached</summary><p v-for="source in sourceResults" :key="source.registry"><strong>{{ source.label }}</strong> · {{ source.registry }}<br>{{ source.error || `${source.images.length} published head tags` }}</p></details>
          </div>
          <div v-if="mode === 'image'" class="upgrade-source">
            <label>Rancher server image<input v-model.trim="image" placeholder="stgregistry.suse.com/rancher/rancher:v2.15.3-head" spellcheck="false" autocomplete="off"></label>
            <p class="upgrade-help">Paste a full repository and tag, including a staging build or a private mirror. The repository must end in /rancher; preflight checks its version and the matching /rancher-agent image. Registry access uses Runway's existing credentials.</p>
            <p class="upgrade-help">This is a Helm installation, so the image still uses a chart. A chart from the installed release line is proposed below; you can change it under Advanced.</p>
          </div>
          <template v-if="catalog && mode !== 'image'">
            <div class="upgrade-target-heading"><h4>{{ isHead ? 'Choose a head image' : 'Available versions' }}</h4><label class="upgrade-search"><span class="sr-only">Search {{ isHead ? 'published head images' : 'versions' }}</span><input v-model="search" :placeholder="isHead ? 'Search version or commit…' : 'Find a version…'" type="search"></label></div>
            <div class="upgrade-search-hints" aria-label="Suggested searches"><button type="button" :aria-pressed="!search" @click="search = ''">{{ isHead ? 'Suggested heads' : 'All available' }}</button><button v-for="hint in upgradeSearchHints(installed?.version)" :key="hint.query" type="button" :aria-pressed="search === hint.query" @click="search = hint.query">{{ hint.label }}</button></div>
            <p v-if="isHead" class="upgrade-help">Head follows development. Select a build to see its exact version, digest and source commit. Search by version or commit for older builds.</p>
            <div class="upgrade-targets" role="group" :aria-label="isHead ? 'Published head images' : 'Published releases'">
              <template v-if="isHead">
                <button v-for="head in displayedHeads" :key="head" class="upgrade-target" type="button" :aria-pressed="image === head" @click="image = head">
                  <span class="upgrade-radio" aria-hidden="true">{{ image === head ? '●' : '○' }}</span><span class="upgrade-target-copy"><strong>{{ imageTag(head) === 'head' ? 'Latest head' : imageTag(head) }}</strong><small>{{ sourceLabel(head) }} · {{ imageRegistry(head) }}</small><small v-if="imageCommitTag(head)" class="upgrade-reference">Commit {{ imageCommitTag(head) }}</small></span><span class="upgrade-badge">Experimental</span>
                </button>
                <p v-if="!displayedHeads.length" class="upgrade-empty">No matching head aliases. Search a version or tag, or choose Exact image.</p>
              </template>
              <template v-else>
                <button v-for="item in filteredReleases" :key="item.version" class="upgrade-target" type="button" :disabled="Boolean(item.blocked)" :aria-pressed="chartVersion === item.version" @click="chartVersion = item.version">
                  <span class="upgrade-radio" aria-hidden="true">{{ chartVersion === item.version ? '●' : '○' }}</span><span class="upgrade-target-copy"><strong>{{ item.target }}</strong><small>{{ item.blocked || `Helm chart ${item.version}` }}</small></span><span v-if="item.recommended" class="upgrade-badge recommended">Suggested next step</span>
                </button>
                <p v-if="!filteredReleases.length" class="upgrade-empty">{{ search ? 'No matching versions.' : 'No newer versions in this channel for the current or next minor. Try RC / Alpha or Head.' }}</p>
              </template>
            </div>
            <p v-if="isHead && allHeads.length > displayedHeads.length" class="upgrade-help">Showing {{ displayedHeads.length }} of {{ allHeads.length }} matches. Narrow your search to find a specific build.</p>
          </template>
          <RancherImageEvidence v-if="isImage" :image="image" :request="request" />
          <details class="upgrade-advanced" :open="advanced" @toggle="advanced = $event.target.open">
            <summary>Advanced · {{ distribution === 'prime' ? 'Prime' : 'Community' }}{{ isImage && chartVersion ? ` · chart ${chartVersion}` : '' }}</summary>
            <label>Rancher distribution<select v-model="distribution" @change="reloadTargets"><option value="community">Community</option><option value="prime">Prime</option></select></label>
            <template v-if="isImage">
              <p class="upgrade-help">The installed release line supplies the initial chart choice. Review chart compatibility for your head image; Helm checks run before applying it.</p>
              <label>Chart channel<select v-model="channel" @change="reloadTargets"><option value="ga">Stable</option><option value="rc">Release candidate</option><option value="alpha">Alpha</option><option v-if="distribution === 'prime'" value="head">Head</option></select></label>
              <label>Helm chart<select v-model="chartVersion"><option value="" disabled>Choose a chart</option><option v-for="chart in catalog?.versions || []" :key="chart.version" :value="chart.version">{{ chart.version }} · Rancher {{ chart.appVersion }}</option></select></label>
              <label v-if="isHead">Exact server image<input v-model.trim="image" placeholder="docker.io/rancher/rancher:head" spellcheck="false"></label>
            </template>
          </details>
          <p v-if="isImage && catalog && !chartVersion" class="upgrade-message">No chart matched the installed release line. Choose a published chart under Advanced to continue.</p>
          <div class="upgrade-footer"><div><small>{{ targetLabel ? 'YOUR UPGRADE' : 'CHOOSE A TARGET' }}</small><strong>{{ installed?.version || 'Current Rancher' }} <span aria-hidden="true">→</span> {{ targetLabel || 'Select a version above' }}</strong><small v-if="isImage && image" class="upgrade-reference">{{ image }}</small></div><button type="button" class="upgrade-primary" :disabled="!canReview" @click="review">{{ busy ? 'Checking…' : 'Run preflight' }} <span aria-hidden="true">→</span></button></div>
          <p class="upgrade-help">Review checks readiness and records exact images. Nothing is changed until you confirm the upgrade.</p>
        </template>
        <div v-else class="upgrade-review">
          <button type="button" class="upgrade-back" @click="plan = null">← Change target</button>
          <span class="upgrade-eyebrow">PREFLIGHT PASSED</span><h4 ref="reviewHeading" tabindex="-1">{{ plan.before.version }} <span>→</span> {{ plan.targetVersion }}</h4>
          <p>Helm chart {{ plan.chart.version }} · {{ plan.experimental ? 'Experimental target' : 'Stable release' }}</p>
          <p class="upgrade-help">The checks below passed for this exact target. {{ plan.experimental ? 'This remains an experimental path; passing checks does not establish official upgrade support.' : 'Review the backup and compatibility requirements below before confirming.' }}</p>
          <ul class="upgrade-checks"><li v-for="check in plan.checks" :key="check"><span aria-hidden="true">✓</span> {{ check }}</li></ul>
          <details><summary>Exact images & digests</summary><dl><template v-if="plan.targetVersionSource"><dt>Version evidence</dt><dd>{{ plan.targetVersionSource }}</dd><dt>OCI version label</dt><dd>{{ plan.targetVersionLabel || 'Not declared' }}</dd></template><template v-if="plan.imageRevision"><dt>Source commit</dt><dd>{{ plan.imageRevision }}</dd></template><template v-if="plan.imageOSSRevision"><dt>OSS commit</dt><dd>{{ plan.imageOSSRevision }}</dd></template><template v-if="plan.imageCanonicalReference"><dt>Canonical image</dt><dd>{{ plan.imageCanonicalReference }}</dd></template><dt>Server</dt><dd>{{ plan.image }}:{{ plan.imageTag }}</dd><dd>{{ plan.digest }}</dd><dt>Agent</dt><dd>{{ plan.agentImage }}</dd><dd>{{ plan.agentDigest }}</dd></dl></details>
          <ul class="upgrade-warnings"><li v-for="warning in plan.warnings" :key="warning">{{ warning }}</li></ul>
          <label class="upgrade-check"><input type="checkbox" v-model="backup">I have verified a current backup and reviewed release notes and Kubernetes compatibility.</label>
          <label v-if="plan.experimental" class="upgrade-check"><input type="checkbox" v-model="experimental">I understand this is an experimental upgrade and may require restoring from backup.</label>
          <div class="upgrade-footer"><p>Follow pod changes, logs and saved evidence after starting.</p><button type="button" class="upgrade-primary" :disabled="!backup || (plan.experimental && !experimental)" @click="upgrade">Upgrade to {{ plan.targetVersion }}</button></div>
          <p class="upgrade-help">Plan expires after 15 minutes. Installed state and target digests are checked again before execution.</p>
        </div>
      </fieldset>
    </template>
  </div>
</template>

<style scoped>
.upgrade-flow{--up-border:#e1e5e9;--up-surface:#fff;--up-muted:#66717f;--up-tint:#f5f7f8;--up-accent:#047857;max-width:980px;margin:auto;color:#263341;font-size:14px}
:global(.dark .upgrade-flow){--up-border:#35404a;--up-surface:#191f27;--up-muted:#a0aab7;--up-tint:#202933;--up-accent:#7ddbc0;color:#e5ebf0}
.upgrade-flow button,.upgrade-flow input,.upgrade-flow select{font:inherit}.upgrade-flow button{cursor:pointer;border:1px solid var(--up-border);border-radius:10px;padding:10px 16px;font-weight:600;background:var(--up-surface);color:inherit}.upgrade-flow button:hover:not(:disabled){border-color:var(--up-accent)}.upgrade-flow button:focus-visible,.upgrade-flow input:focus-visible,.upgrade-flow select:focus-visible,.upgrade-flow summary:focus-visible{outline:2px solid var(--up-accent);outline-offset:3px}.upgrade-flow button:disabled{opacity:.45;cursor:not-allowed}.upgrade-flow fieldset{padding:0;border:0;min-width:0}.upgrade-flow h4{font-size:17px;font-weight:650;margin:22px 0 12px}.upgrade-flow p{line-height:1.6}.upgrade-steps{display:flex;gap:24px;list-style:none;padding:0 0 22px;margin:0;color:var(--up-muted);font-size:12px;border-bottom:1px solid var(--up-border)}.upgrade-steps li{display:flex;align-items:center;gap:8px}.upgrade-steps span{display:grid;place-items:center;width:23px;height:23px;border:1px solid var(--up-border);border-radius:50%}.upgrade-steps .active{color:var(--up-accent);font-weight:600}.upgrade-steps .active span{border-color:var(--up-accent)}.upgrade-current{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:20px 0}.upgrade-current strong{display:block;font-size:25px;letter-spacing:-.5px;margin:3px 0}.upgrade-current small,.upgrade-flow small,.upgrade-help{color:var(--up-muted);font-size:12px}.upgrade-eyebrow{font-size:10px;letter-spacing:1.5px;color:var(--up-muted);font-weight:700;display:block}.upgrade-modes{display:grid;grid-template-columns:repeat(4,1fr);gap:12px}.upgrade-modes button{text-align:left;padding:18px;display:grid;gap:5px}.upgrade-modes button[aria-pressed=true]{border-color:var(--up-accent);background:color-mix(in srgb,var(--up-accent) 7%,var(--up-surface));box-shadow:inset 0 0 0 1px var(--up-accent)}.upgrade-mode-icon{color:var(--up-accent);font-size:21px;margin-bottom:6px}.upgrade-modes small{font-weight:400}.upgrade-target-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-top:24px;margin-bottom:10px}.upgrade-target-heading h4{margin:0}.upgrade-flow input:not([type=checkbox]),.upgrade-flow select{border:1px solid var(--up-border);background:var(--up-surface);color:inherit;border-radius:8px;padding:10px 12px;min-width:0;width:100%}.upgrade-search{max-width:240px}.upgrade-source{padding:16px 0 0}.upgrade-source label{display:grid;gap:8px;font-size:12px;font-weight:600}.upgrade-reference{display:block;font-family:ui-monospace,monospace;font-size:11px!important;letter-spacing:0!important;margin-top:7px;overflow-wrap:anywhere}.upgrade-search-hints{display:flex;flex-wrap:wrap;gap:7px;margin:12px 0}.upgrade-search-hints button{font-size:11px;border-radius:20px;padding:6px 10px;color:var(--up-muted)}.upgrade-search-hints button[aria-pressed=true]{color:var(--up-accent);border-color:var(--up-accent)}.upgrade-targets{border:1px solid var(--up-border);border-radius:12px;overflow:auto;max-height:290px}.upgrade-flow .upgrade-target{display:flex;align-items:center;gap:12px;width:100%;text-align:left;border:0;border-bottom:1px solid var(--up-border);border-radius:0;padding:15px 17px}.upgrade-target:last-child{border-bottom:0}.upgrade-target[aria-pressed=true]{background:color-mix(in srgb,var(--up-accent) 7%,var(--up-surface))}.upgrade-radio{color:var(--up-accent);font-size:21px}.upgrade-target-copy{flex:1;min-width:0;display:grid;gap:4px;overflow-wrap:anywhere}.upgrade-target-copy small{font-weight:400}.upgrade-badge{font-size:10px;color:var(--up-muted);padding:4px 8px;border-radius:20px;border:1px solid var(--up-border);white-space:nowrap}.upgrade-badge.recommended{color:var(--up-accent)}.upgrade-empty{padding:18px;color:var(--up-muted)}.upgrade-help{margin:10px 0}.upgrade-advanced{margin:22px 0;color:var(--up-muted)}.upgrade-flow summary{cursor:pointer;font-size:13px;font-weight:600}.upgrade-advanced label,.upgrade-inline label{display:grid;gap:7px;font-size:12px;margin:16px 0}.upgrade-inline{display:flex;align-items:center;gap:20px}.upgrade-inline p{color:var(--up-muted);font-size:12px;max-width:380px}.upgrade-footer{margin-top:22px;padding-top:20px;border-top:1px solid var(--up-border);display:flex;justify-content:space-between;align-items:center;gap:20px}.upgrade-footer strong{display:block;font-size:16px;margin-top:4px;overflow-wrap:anywhere}.upgrade-footer small{font-size:10px;letter-spacing:1px}.upgrade-footer strong span,.upgrade-review h4 span{color:var(--up-muted);padding:0 10px}.upgrade-flow .upgrade-primary{overflow-wrap:anywhere;background:#047857;color:#fff;border-color:#047857;white-space:normal;padding:12px 20px}.upgrade-footer p{font-size:12px;color:var(--up-muted);max-width:340px}.upgrade-message,.upgrade-error{padding:16px;border-radius:10px;background:var(--up-tint);margin:12px 0;line-height:1.6}.upgrade-error{border:1px solid #e07b65}.upgrade-loading{display:flex;align-items:center;gap:10px;color:var(--up-accent)}.upgrade-checks{list-style:none;margin:20px 0;padding:16px;background:var(--up-tint);border-radius:10px;display:grid;gap:10px}.upgrade-checks span{color:var(--up-accent);margin-right:7px}.upgrade-review h4{overflow-wrap:anywhere;font-size:26px;margin:8px 0}.upgrade-flow .upgrade-back{border:0;background:transparent;padding:0;margin:5px 0 24px;color:var(--up-accent)}.upgrade-review dl{font-family:ui-monospace,monospace;font-size:12px;overflow-wrap:anywhere;padding:12px}.upgrade-review dt{margin-top:12px;font-weight:700}.upgrade-warnings{padding-left:18px;margin:22px 0;color:var(--up-muted);font-size:12px;line-height:1.7;list-style:disc}.upgrade-check{display:flex;align-items:flex-start;gap:12px;margin:16px 0;line-height:1.6}.upgrade-check input{margin-top:5px;accent-color:#047857;flex-shrink:0}
@media(max-width:800px){.upgrade-modes{grid-template-columns:repeat(2,1fr)}}
@media(max-width:640px){.upgrade-steps{gap:12px;font-size:10px}.upgrade-steps li{gap:5px}.upgrade-modes{gap:6px}.upgrade-modes button{padding:12px 9px}.upgrade-modes small{font-size:10px}.upgrade-footer,.upgrade-target-heading{align-items:stretch;flex-direction:column}.upgrade-search{max-width:none}.upgrade-badge{white-space:normal;max-width:95px}.upgrade-footer .upgrade-primary{width:100%}.upgrade-inline{align-items:stretch;flex-direction:column;gap:0}}
</style>
