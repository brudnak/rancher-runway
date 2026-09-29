<script setup>
import {computed,nextTick,onBeforeUnmount,onMounted,ref,useId} from 'vue';
import Icon from './HelmLabIcon.vue';
import {apiFetch} from './store.js';
import {filterRancherTargets,rancherConnectionURL,rancherTestHost} from './rancher-connection.mjs';
const props=defineProps({purpose:{type:String,required:true},url:{type:String,default:''},disabled:Boolean});
const emit=defineEmits(['select']);
const targets=ref([]),query=ref(''),loading=ref(false),loaded=ref(false),error=ref(''),open=ref(false),toggleRef=ref(null);
const listId=useId();
const matches=computed(()=>filterRancherTargets(targets.value,query.value));
let controller,disposed=false;
async function refresh(){
 if(loading.value)return;controller=new AbortController();loading.value=true;error.value='';const timer=setTimeout(()=>controller.abort(),15000);
 try{const response=await apiFetch('/api/rancher/targets',{signal:controller.signal});const data=await response.json();if(!disposed){targets.value=data.targets||[];loaded.value=true;}}
 catch{if(!disposed)error.value='The detected Rancher list could not be loaded. You can still enter a connection below.';}
 finally{clearTimeout(timer);if(!disposed)loading.value=false;}
}
function supported(target){return props.purpose==='cache'?target.cacheSupported:!!rancherTestHost(target.url);}
function selected(target){return rancherConnectionURL(props.url)===target.url;}
function choose(target,kind){
 if(props.disabled||!supported(target))return;
 open.value=false;
 nextTick(()=>toggleRef.value?.focus({preventScroll:true}));
 emit('select',{target,kind});
}
onMounted(refresh);onBeforeUnmount(()=>{disposed=true;controller?.abort();});
</script>
<template>
 <section class="rancher-connect-picker" aria-label="Detected Ranchers">
  <button ref="toggleRef" type="button" class="rancher-picker-toggle" :aria-expanded="open" :aria-controls="listId" :disabled="disabled" @click="open=!open"><Icon name="server"/><strong>Use a detected Rancher</strong><span class="rancher-picker-count">{{ loading?'Loading…':loaded?targets.length:'Unavailable' }}</span><small>From Runs & Clusters</small><Icon name="chevron" :class="{'is-open':open}"/></button>
  <div v-if="open" :id="listId" class="rancher-picker-body">
  <div class="rancher-connect-heading"><p>Choose a recorded environment to fill the connection below.</p><button type="button" class="rancher-connect-button" :disabled="loading||disabled" @click="refresh"><Icon name="refresh" :class="{'lab-spin':loading}"/>{{ loading?'Loading…':'Refresh list' }}</button></div>
  <label v-if="targets.length>3" class="rancher-connect-search"><Icon name="search"/><input v-model="query" type="search" placeholder="Find a Rancher, version, or run…" aria-label="Find a detected Rancher" :disabled="disabled"/></label>
  <p v-if="error" class="rancher-connect-error" role="alert">{{ error }}</p>
  <p v-else-if="loaded&&!targets.length" class="rancher-connect-empty">No Rancher has been detected yet. A recorded environment appears here once its URL is available. You can connect any Rancher manually.</p>
  <p v-else-if="!loaded" class="rancher-connect-empty" role="status">Looking for recorded environments…</p>
  <div v-if="matches.length" class="rancher-connect-targets">
   <article v-for="target in matches" :key="target.id" class="rancher-connect-target" :class="{'is-selected':selected(target)}">
    <div class="rancher-connect-target-info"><div class="rancher-connect-target-name"><strong>{{ target.name }}</strong><span v-if="selected(target)" class="rancher-connect-badge"><Icon name="check"/>Selected</span><span v-else class="rancher-connect-badge" :class="{'is-waiting':target.provisioning}">{{ target.provisioning?'Provisioning':target.reachable?'Cluster reachable':'Discovered' }}</span></div><span class="rancher-connect-address">{{ target.url }}</span><small><template v-if="target.version">{{ target.version }} · </template><template v-if="target.role">{{ target.role }} · </template>{{ target.runId?'Run '+target.runId:'Recorded environment' }}</small><small v-if="!supported(target)">{{ purpose==='cache'?'Docker-based Rancher has no Kubernetes pod to capture.':'This URL needs a scheme or path that rancher/tests does not support.' }}</small><small v-else-if="target.provisioning">Connection may become available before provisioning finishes.</small></div>
    <div class="rancher-connect-target-actions"><button type="button" class="rancher-connect-button" :disabled="disabled||!supported(target)" @click="choose(target,'rancher')">Use this Rancher<Icon name="arrow"/></button><button v-if="purpose==='cache'&&target.kubeconfig&&supported(target)" type="button" class="rancher-connect-link" :disabled="disabled" @click="choose(target,'kubeconfig')"><Icon name="key"/>Use kubeconfig</button></div>
   </article>
  </div>
  <p v-else-if="query&&targets.length" class="rancher-connect-empty">No Ranchers match “{{ query }}”.</p>
  </div>
 </section>
</template>
