<script setup>
import {computed, ref, watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {packageDate} from './issue-packages.mjs';
import {writeTextToClipboard} from './clipboard.js';

const props=defineProps({environment:{type:Object,required:true},name:String,preview:Boolean,busy:Boolean});
defineEmits(['refresh','open-cluster']);
const copyStatus=ref('');
watch(()=>props.environment.helmCommand,()=>{copyStatus.value='';});
const details=computed(()=>props.environment.details||[]);
const webhook=computed(()=>details.value.find(item=>/webhook.*version/i.test(item.label))?.value||details.value.find(item=>/webhook.*image/i.test(item.label))?.value);
const sourceLabel=value=>({recorded:'Runway record',observed:'Cluster observation',manual:'Entered manually',imported:'Imported record'}[value]||value);
async function copyCommand(){try{await writeTextToClipboard(props.environment.helmCommand);copyStatus.value='Copied';}catch{copyStatus.value='Could not copy — select the command below.';}}
</script>
<template>
  <section class="package-environment" :aria-label="preview?'Environment to preserve':'Preserved environment'">
    <header>
      <span class="pe-emblem"><Icon name="server"/></span>
      <div class="pe-heading"><span class="pe-eyebrow">{{preview?'ENVIRONMENT PREVIEW':'FROZEN ENVIRONMENT'}}</span><strong>{{name||environment.clusterName||'Manually described environment'}}</strong><small v-if="environment.clusterName&&name&&name!==environment.clusterName">Recorded as {{environment.clusterName}}</small></div>
      <button v-if="preview&&environment.clusterId" type="button" :disabled="busy" @click="$emit('refresh')"><Icon name="refresh"/>{{busy?'Reading cluster…':'Refresh from cluster'}}</button>
      <button v-else-if="environment.clusterId" type="button" @click="$emit('open-cluster',environment.clusterId)">Cluster workspace <Icon name="arrow"/></button>
    </header>
    <dl class="pe-versions">
      <div><dt>Rancher</dt><dd>{{environment.rancherVersion||'Not recorded'}}</dd></div>
      <div><dt>Kubernetes</dt><dd>{{environment.kubernetesVersion||'Not recorded'}}</dd></div>
      <div v-if="webhook"><dt>Webhook</dt><dd>{{webhook}}</dd></div>
    </dl>
    <div class="pe-provenance"><Icon :name="preview?'clock':'lock'"/><span>{{sourceLabel(environment.source)}}<template v-if="environment.observedAt&&packageDate(environment.observedAt)!=='Not recorded'"> · Cluster observed {{packageDate(environment.observedAt,true)}}</template><template v-else> · {{preview?'Captured when the session starts':'Saved '+packageDate(environment.recordedAt,true)}}</template></span></div>
    <ul v-if="environment.warnings?.length" class="pe-warnings"><li v-for="warning in environment.warnings" :key="warning">{{warning}}</li></ul>
    <details v-if="details.length||environment.helmCommand||environment.images?.length||environment.configuration" class="pe-full">
      <summary><Icon name="layers"/>Full environment record <span>{{details.length?`${details.length} details`:''}}{{environment.helmCommand?' · Helm command':''}}</span><Icon name="chevron"/></summary>
      <dl v-if="details.length" class="pe-detail-list"><div v-for="(detail,index) in details" :key="index"><dt>{{detail.label}}<small>{{sourceLabel(detail.source)}}<template v-if="detail.observedAt"> · {{packageDate(detail.observedAt,true)}}</template></small></dt><dd>{{detail.value}}</dd></div></dl>
      <div v-if="environment.helmCommand" class="pe-command"><div><h5>Recorded Helm install command</h5><button type="button" @click="copyCommand"><Icon name="copy"/>{{copyStatus==='Copied'?'Copied':'Copy command'}}</button></div><p>Credentials, hostnames, and local file paths are redacted. Restore placeholders before reusing.</p><pre tabindex="0">{{environment.helmCommand}}</pre><p v-if="copyStatus&&copyStatus!=='Copied'" role="status">{{copyStatus}}</p></div>
      <div v-if="environment.images?.length" class="pe-images"><h5>Container images & immutable IDs</h5><code v-for="image in environment.images" :key="image">{{image}}</code></div>
      <div v-if="environment.configuration" class="pe-configuration"><h5>Configuration & prerequisites</h5><p>{{environment.configuration}}</p></div>
    </details>
    <p v-if="preview" class="pe-footnote">Starting the session preserves these known details. Refresh reads the cluster; missing details remain unknown.</p>
  </section>
</template>
<style scoped>
.package-environment{background:var(--runway-soft);border:1px solid var(--runway-border);border-radius:14px;margin:18px 0 24px;overflow:hidden;color:var(--runway-ink)}.package-environment header{display:flex;align-items:center;gap:13px;padding:20px 22px 16px;flex-wrap:wrap}.pe-emblem{display:grid;place-items:center;width:39px;height:39px;border-radius:11px;background:var(--runway-accent-soft);color:var(--runway-accent);flex-shrink:0}.pe-emblem svg{width:21px;height:21px}.pe-heading{display:grid;gap:5px;min-width:0;flex:1}.pe-eyebrow{font-size:9px;letter-spacing:.13em;font-weight:650;color:var(--runway-muted)}.pe-heading strong{font-size:14px;overflow-wrap:anywhere}.pe-heading small{font-size:10px;color:var(--runway-muted)}.package-environment button{display:inline-flex;align-items:center;gap:7px;border:1px solid var(--runway-border);border-radius:8px;background:var(--runway-input);color:var(--runway-accent);padding:9px 11px;font:inherit;font-size:11px;cursor:pointer}.package-environment button:disabled{opacity:.55;cursor:wait}.package-environment :focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}.package-environment button svg{width:14px;height:14px}.pe-versions{display:grid;grid-template-columns:repeat(auto-fit,minmax(145px,1fr));gap:18px;margin:0;padding:4px 22px 20px}.pe-versions dt{font-size:10px;color:var(--runway-muted);margin-bottom:5px}.pe-versions dd{font-size:15px;line-height:1.5;font-weight:550;margin:0;overflow-wrap:anywhere}.pe-provenance{display:flex;align-items:flex-start;gap:7px;font-size:10px;color:var(--runway-muted);padding:0 22px 17px;line-height:1.7}.pe-provenance svg{width:13px;height:13px;flex-shrink:0;margin-top:2px}.pe-warnings{font-size:11px;line-height:1.7;color:var(--runway-gold);padding:12px 22px 12px 36px;margin:0 20px 17px;border:1px solid var(--runway-border);border-radius:8px}.pe-full{border-top:1px solid var(--runway-border)}.pe-full summary{display:flex;align-items:center;gap:8px;cursor:pointer;padding:14px 22px;font-size:11px;font-weight:550;list-style:none}.pe-full summary::-webkit-details-marker{display:none}.pe-full summary>svg{width:15px;height:15px;color:var(--runway-accent)}.pe-full summary>svg:last-child{margin-left:auto}.pe-full[open] summary>svg:last-child{transform:rotate(180deg)}.pe-full summary span{font-size:10px;font-weight:400;color:var(--runway-muted)}.pe-detail-list{margin:0;padding:0 22px}.pe-detail-list>div{display:grid;grid-template-columns:minmax(120px,.8fr) minmax(0,1.6fr);gap:20px;padding:11px 0;border-top:1px solid var(--runway-border)}.pe-detail-list dt{font-size:11px;color:var(--runway-muted)}.pe-detail-list small{display:block;font-size:9px;margin-top:4px;opacity:.85}.pe-detail-list dd{font:11px/1.8 ui-monospace,monospace;margin:0;white-space:pre-wrap;overflow-wrap:anywhere}.pe-command,.pe-images,.pe-configuration{margin:17px 22px}.package-environment h5{font-size:11px;font-weight:600;margin:0 0 8px}.pe-command>div{display:flex;align-items:center;justify-content:space-between;gap:15px}.pe-command>div h5{margin:0}.pe-command p,.pe-footnote{font-size:10px;line-height:1.7;color:var(--runway-muted)}.pe-command p{margin:8px 0}.pe-command pre{background:var(--runway-input);border:1px solid var(--runway-border);border-radius:9px;padding:15px;font:11px/1.85 ui-monospace,monospace;white-space:pre-wrap;overflow-wrap:anywhere;max-height:320px;overflow:auto}.pe-images code{display:block;font:10px/1.8 ui-monospace,monospace;overflow-wrap:anywhere;padding:5px 0;color:var(--runway-muted)}.pe-configuration p{font-size:12px;line-height:1.8;white-space:pre-wrap;overflow-wrap:anywhere}.pe-footnote{padding:12px 22px;margin:0;border-top:1px solid var(--runway-border)}@media(max-width:650px){.package-environment header{padding:17px}.package-environment header>button{margin-left:52px}.pe-versions{padding:4px 17px 18px;gap:12px}.pe-detail-list>div{grid-template-columns:1fr;gap:5px}.pe-full summary span{display:none}.pe-full summary{padding:14px 17px}}
</style>
