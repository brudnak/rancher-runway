<script setup>
import {computed,onBeforeUnmount,ref,watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {clusterWorkspaces,clusterWorkspaceError,clusterDisplayName} from './cluster-workspace-store.mjs';
const props=defineProps({modelValue:{type:String,default:''},sessionId:{type:String,default:''},sessions:{type:Array,default:()=>[]},request:{type:Function,required:true},disabled:Boolean});
const emit=defineEmits(['update:modelValue','update:sessionId']);
const environment=ref(null),loading=ref(false),error=ref('');
let sequence=0,disposed=false;
const selectedSession=computed(()=>props.sessions.find(session=>session.id===props.sessionId));
const displayedEnvironment=computed(()=>selectedSession.value?.environment||environment.value);
const installation=computed(()=>displayedEnvironment.value?.details?.find(item=>item.label==='Installation type')?.value||'');
const summary=computed(()=>selectedSession.value?`${selectedSession.value.name} · recorded environment`:props.modelValue?clusterDisplayName(props.modelValue,'Selected environment'):'Choose a Rancher environment');
async function load(){
  const current=++sequence;environment.value=null;error.value='';loading.value=false;
  if(!props.modelValue||selectedSession.value)return;
  loading.value=true;
  try{const result=await props.request('preview-environment',{environment:{clusterId:props.modelValue}});if(!disposed&&current===sequence)environment.value=result.environment;}
  catch(err){if(!disposed&&current===sequence)error.value=err.message;}
  finally{if(!disposed&&current===sequence)loading.value=false;}
}
watch([()=>props.modelValue,()=>props.sessionId],load,{immediate:true});
onBeforeUnmount(()=>{disposed=true;sequence++;});
</script>
<template>
  <details class="plan-environment">
    <summary><Icon name="server"/><span>{{summary}}</span><small>{{loading?'Reading saved details…':installation||'Optional · included in Markdown'}}</small><Icon name="chevron"/></summary>
    <div class="pen-content">
      <label v-if="sessions.length">Include results from<select aria-label="Include results from" :value="sessionId" :disabled="disabled" @change="emit('update:sessionId',$event.target.value)"><option value="">Test plan · not tested yet</option><option v-for="session in sessions" :key="session.id" :value="session.id">{{session.name}} · {{session.status==='active'?'in progress':'completed'}}</option></select></label>
      <label v-if="!sessionId">Rancher environment<select aria-label="Rancher environment" :value="modelValue" :disabled="disabled" @change="emit('update:modelValue',$event.target.value)"><option value="">Fill in the environment later</option><option v-if="modelValue&&!clusterWorkspaces.some(cluster=>cluster.id===modelValue)" :value="modelValue">Previously selected environment · unavailable</option><option v-for="cluster in clusterWorkspaces" :key="cluster.id" :value="cluster.id">{{clusterDisplayName(cluster.id)}}{{cluster.version?` · ${cluster.version}`:''}}{{cluster.archived?' · retained history':''}}</option></select></label>
      <p v-if="!sessionId">Choosing a deployed Rancher fills the environment table from its saved deployment details, including the installation type. Missing details stay marked “Not recorded.”</p>
      <p v-else>Uses this session’s recorded environment. Results are included only for cases that still match what was tested.</p>
      <p v-if="clusterWorkspaceError&&!sessionId" class="pen-error" role="alert">{{clusterWorkspaceError}}</p>
      <p v-if="error&&!sessionId" class="pen-error" role="alert">{{error}} <button type="button" :disabled="disabled||loading" @click="load">Try again</button></p>
      <dl v-if="displayedEnvironment"><div><dt>Rancher</dt><dd>{{displayedEnvironment.rancherVersion||'Not recorded'}}</dd></div><div><dt>Kubernetes</dt><dd>{{displayedEnvironment.kubernetesVersion||'Not recorded'}}</dd></div><div v-if="installation"><dt>Installation</dt><dd>{{installation}}</dd></div></dl>
    </div>
  </details>
</template>
<style scoped>
.plan-environment{margin:0 28px;border-bottom:1px solid var(--runway-border);color:var(--runway-ink)}.plan-environment summary{list-style:none;display:flex;align-items:center;gap:10px;cursor:pointer;padding:14px 0;font-size:11px;color:var(--runway-muted)}.plan-environment summary::-webkit-details-marker{display:none}.plan-environment summary>span{color:var(--runway-ink)}.plan-environment svg{width:15px;height:15px;flex-shrink:0}.plan-environment summary>svg:first-child{color:var(--runway-accent)}.plan-environment summary>svg:last-child{margin-left:auto;width:12px}.plan-environment[open] summary>svg:last-child{transform:rotate(180deg)}.plan-environment summary small{font-size:10px}.pen-content{max-width:740px;padding:6px 0 24px}.pen-content label{display:grid;gap:8px;font-size:11px;margin:0 0 14px}.pen-content select{width:100%;padding:10px 12px;border:1px solid var(--runway-control);border-radius:8px;background:var(--runway-input);color:var(--runway-ink);font:inherit}.pen-content p{font-size:11px;line-height:1.8;color:var(--runway-muted);margin:8px 0}.pen-content dl{display:flex;gap:25px;flex-wrap:wrap;margin-top:17px}.pen-content dt{font-size:10px;color:var(--runway-muted)}.pen-content dd{font-size:12px;margin:5px 0 0;overflow-wrap:anywhere}.pen-content .pen-error{color:var(--runway-error)}.pen-content button{font:inherit;color:var(--runway-accent);border:0;background:none;cursor:pointer;text-decoration:underline}.plan-environment :focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}@media(max-width:650px){.plan-environment{margin:0 20px}.plan-environment summary{flex-wrap:wrap}.plan-environment summary small{width:100%;padding-left:25px}.plan-environment summary>svg:last-child{display:none}}
</style>
