<script setup>
import {computed, onBeforeUnmount, ref, watch} from 'vue';
import {apiFetch} from './store.js';
import {readJSON} from './read-json.mjs';
import {clusterTimeline, clusterHistoryMarkdown} from './cluster-timeline.mjs';
import ClusterHistoryStep from './ClusterHistoryStep.vue';
const props = defineProps({clusterId:{type:String,required:true}, active:{type:Boolean,default:true}});
const archive = ref(null), error = ref(''), loading = ref(false);
const timeline = computed(() => clusterTimeline(archive.value));
const upgradeCount = computed(() => (archive.value?.operations || []).filter(op => op.kind === 'upgrade' && op.status === 'succeeded').length);
const observations = computed(() => [...(archive.value?.events || [])].reverse());
let generation = 0, timer;
async function load() {
 const request = ++generation; loading.value = true; error.value = ''; clearTimeout(timer);
 try {
  const data = await readJSON(signal => apiFetch(`/api/cluster-history?cluster=${encodeURIComponent(props.clusterId)}`, {signal}), {label:'Cluster history'});
  if (request === generation) archive.value = data;
 } catch (e) { if (request === generation) error.value = e.message; }
 finally { if (request === generation) { loading.value = false; if (props.active) timer = setTimeout(load, 15000); } }
}
watch([() => props.clusterId, () => props.active], ([id,active], previous) => {
 generation++; clearTimeout(timer); loading.value = false;
 if (id !== previous?.[0]) archive.value = null;
 if (active) void load();
}, {immediate:true});
onBeforeUnmount(() => {generation++; clearTimeout(timer);});
const pretty = value => JSON.stringify(value,null,2);
function exportArchive(format) {
 const content = format === 'json' ? pretty(archive.value) : clusterHistoryMarkdown(archive.value);
 const url = URL.createObjectURL(new Blob([content], {type:format === 'json' ? 'application/json' : 'text/markdown'}));
 const a = document.createElement('a'); a.href = url; a.download = `rancher-history-${props.clusterId}.${format}`; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function summary(event) {
 const d = event.data || {};
 if (event.kind === 'deployment') return [d.rancherVersion && `Rancher ${d.rancherVersion}`, d.kubernetesVersion && `Kubernetes ${d.kubernetesVersion}`, d.webhookChartVersion && `Webhook ${d.webhookChartVersion}`].filter(Boolean).join(' · ') || 'Partial deployment observation';
 return event.kind.replaceAll('-', ' ');
}
</script>
<template>
 <section class="evidence-history" aria-label="This cluster’s history">
  <header><div><h3>This cluster’s history</h3><p>Starting configuration → every upgrade attempt → retained observations.</p></div><div class="history-actions"><button type="button" :disabled="loading" @click="load">{{ loading ? 'Loading…' : 'Refresh history' }}</button><button type="button" :disabled="!archive" @click="exportArchive('md')">Save issue evidence</button><button type="button" :disabled="!archive" @click="exportArchive('json')">Export JSON</button></div></header>
  <p v-if="error" role="alert" class="history-error">{{ error }}</p>
  <template v-if="archive">
   <div class="history-caption"><span>{{ upgradeCount }} completed upgrade{{ upgradeCount === 1 ? '' : 's' }}</span><span>{{ archive.operations?.length || 0 }} recorded operations</span><span>Saved locally · survives infrastructure cleanup</span></div>
   <p v-if="!archive.events?.length && !archive.operations?.length">No detailed history was captured for this cluster. Older infrastructure cannot be queried after it has been removed.</p>
   <ol class="history-timeline"><ClusterHistoryStep v-for="(step,index) in timeline" :key="step.id" :step="step" :index="index" /></ol>
   <details class="history-observations"><summary>Deployment observations & Helm revisions <span>{{ observations.length }} saved records</span></summary><p>Timestamped snapshots retain webhook versions, image digests, pod state and Helm revision history when available. They are observations, not proof of an upgrade performed by Runway.</p><details v-for="event in observations" :key="event.id"><summary>{{ new Date(event.at).toLocaleString() }} · {{ summary(event) }}</summary><pre>{{ pretty(event.data) }}</pre></details></details>
   <p class="history-footnote">Commands have secrets and local paths redacted. Missing metadata stays unknown; each saved operation keeps its own plan, image digests and log.</p>
  </template>
 </section>
</template>
<style scoped>
.evidence-history{--history-border:#d4dce0;--history-card:#fff;--history-soft:#f5f7f8;--history-muted:#66717f;--history-accent:#047857;padding:22px 0;font-size:13px;color:#263341}:global(.dark .evidence-history){--history-border:#35404a;--history-card:#191f27;--history-soft:#202933;--history-muted:#a0aab7;--history-accent:#7ddbc0;color:#e5ebf0}.evidence-history>header{display:flex;justify-content:space-between;align-items:start;gap:20px;flex-wrap:wrap}h3{font-size:19px;font-weight:650}.evidence-history p{color:var(--history-muted);line-height:1.6;margin:6px 0}.history-actions{display:flex;gap:8px;flex-wrap:wrap}.history-actions button{border:1px solid var(--history-border);padding:8px 12px;border-radius:8px;font-size:12px;font-weight:600;cursor:pointer}.history-actions button:disabled{opacity:.5}.history-caption{display:flex;gap:18px;flex-wrap:wrap;color:var(--history-muted);font-size:11px;margin:18px 0 24px}.history-caption span:first-child{color:var(--history-accent)}.history-timeline{list-style:none;padding:0;margin:0}.history-observations{border:1px solid var(--history-border);border-radius:12px;padding:16px;margin-top:24px}.history-observations summary{cursor:pointer;font-weight:600}.history-observations summary span{font-size:11px;color:var(--history-muted);margin-left:8px}.history-observations details{border-top:1px solid var(--history-border);padding-top:12px;margin-top:12px;font-size:12px}.history-observations pre{white-space:pre-wrap;overflow-wrap:anywhere;max-height:420px;overflow:auto;font:11px/1.6 ui-monospace,monospace;background:var(--history-soft);padding:14px;margin-top:12px}.history-footnote{font-size:11px;margin-top:16px!important}.history-error{color:#dc8066!important}button:focus-visible,summary:focus-visible{outline:2px solid var(--history-accent);outline-offset:3px}
</style>
