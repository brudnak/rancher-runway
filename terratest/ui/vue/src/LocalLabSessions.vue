<script setup>
import { computed, ref, watch } from 'vue';
import LabIcon from './HelmLabIcon.vue';
import { filterSessions, sessionStatus } from './local-lab.mjs';
const props=defineProps({kind:String,records:Array,loaded:Boolean,unavailable:Boolean});
const search=ref(''),filter=ref('all'),limit=ref(6);
const filters=[{id:'all',label:'All sessions'},{id:'running',label:'Active'},{id:'stopped',label:'Stopped'},{id:'attention',label:'Needs attention'}];
const filtered=computed(()=>filterSessions(props.records,{search:search.value,filter:filter.value,kind:props.kind}));
const count=id=>id==='all'?props.records.length:props.records.filter(record=>sessionStatus(record,props.kind).group===id).length;
watch([search,filter],()=>{limit.value=6;});
</script>
<template>
  <section :id="`${kind}-sessions`" class="lab-sessions" :aria-labelledby="`${kind}-sessions-title`">
    <div class="lab-section-heading"><div><span class="lab-eyebrow">YOUR WORKSPACE</span><h3 :id="`${kind}-sessions-title`">{{ kind === 'k3d' ? 'Clusters, at your fingertips.' : 'A home for every experiment.' }}</h3></div><span class="lab-total">{{ records.length }}</span></div>
    <template v-if="records.length">
      <div class="lab-search"><LabIcon name="search" /><input v-model="search" type="search" :aria-label="`Search ${kind === 'k3d' ? 'clusters' : 'Steve sessions'}`" :placeholder="kind === 'k3d' ? 'Find a cluster, version, or endpoint…' : 'Find a ref, commit, or endpoint…'"><button v-if="search" type="button" aria-label="Clear session search" @click="search=''"><LabIcon name="close" /></button></div>
      <div class="lab-filters" aria-label="Filter sessions"><button v-for="item in filters" :key="item.id" type="button" :aria-pressed="filter===item.id" @click="filter=item.id">{{ item.label }}<span>{{ count(item.id) }}</span></button></div>
      <span class="lab-sr-only" role="status">{{ filtered.length }} matching {{ filtered.length===1?'session':'sessions' }}</span>
      <div class="lab-session-list"><slot v-for="record in filtered.slice(0,limit)" :key="record.runId" :record="record" /></div>
      <div v-if="!filtered.length" class="lab-empty lab-empty-filter"><LabIcon name="search" /><h4>No matching sessions.</h4><p>Try another version, ref, or name.</p><button type="button" @click="search='';filter='all'">Clear filters</button></div>
      <button v-if="filtered.length>limit" type="button" class="lab-show-more" @click="limit+=6">Show {{ Math.min(6,filtered.length-limit) }} more sessions <LabIcon name="chevron" /></button>
    </template>
    <div v-else class="lab-empty lab-empty-workspace">
      <div class="lab-empty-topology" aria-hidden="true"><span><LabIcon name="server" /></span><i></i><span class="lab-empty-center"><LabIcon :name="kind==='k3d'?'boxes':'pulse'" /></span><i></i><span><LabIcon name="terminal" /></span></div>
      <span class="lab-eyebrow">{{ unavailable ? 'STATUS UNAVAILABLE' : loaded ? 'READY FOR YOUR NEXT IDEA' : 'READING LOCAL STATE' }}</span><h4>{{ unavailable ? 'Your workspace will return here.' : !loaded ? 'Finding your sessions…' : kind === 'k3d' ? 'Your first cluster starts here.' : 'Bring a Steve build to life.' }}</h4>
      <p>{{ unavailable ? 'Refresh local status to reconnect to your sessions. Your configuration stays here while you retry.' : kind === 'k3d' ? 'Choose a K3s version and launch. Your cluster, API endpoint, and kubeconfig will be collected here.' : 'Choose a Steve ref and launch. Your API, runtime logs, and local files will be one click away.' }}</p>
      <div class="lab-empty-features"><span><LabIcon name="check" />{{ kind==='k3d'?'Side-by-side clusters':'Source-aware version matching' }}</span><span><LabIcon name="check" />A dedicated kubeconfig</span><span><LabIcon name="check" />Live activity</span></div>
    </div>
  </section>
</template>
