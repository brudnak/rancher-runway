<script setup>
import {computed,ref,watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {clusterWorkspaces,clusterDisplayName,openClusterWorkspace,renameCluster} from './cluster-workspace-store.mjs';
const props=defineProps({clusterId:String,fallback:String,disabled:Boolean});
const cluster=computed(()=>clusterWorkspaces.value.find(item=>item.id===props.clusterId));
const editing=ref(false),name=ref(''),busy=ref(false),error=ref('');
watch(()=>props.clusterId,()=>{editing.value=false;name.value='';error.value='';});
function edit(){name.value=cluster.value?.nickname||'';error.value='';editing.value=true;}
async function save(){if(busy.value)return;const id=props.clusterId,nickname=name.value.trim();busy.value=true;error.value='';try{await renameCluster(id,nickname);if(props.clusterId===id)editing.value=false;}catch(e){if(props.clusterId===id)error.value=e.message;}finally{busy.value=false;}}
</script>
<template>
 <div v-if="clusterId" class="cluster-identity">
  <div class="cluster-identity-row"><Icon name="server"/><button class="cluster-identity-link" type="button" @click="openClusterWorkspace(clusterId)">{{ clusterDisplayName(clusterId,fallback) }}</button><span v-if="cluster?.version">{{ cluster.version }}</span><button v-if="cluster" class="cluster-identity-edit" type="button" :disabled="disabled" @click="edit" :aria-label="`Set nickname for ${clusterDisplayName(clusterId,fallback)}`">Nickname</button></div>
  <form v-if="editing" class="cluster-identity-editor" @submit.prevent="save"><label>Cluster nickname<input v-model="name" maxlength="80" :placeholder="cluster?.name" :disabled="busy" autocomplete="off"/></label><p>Shown everywhere in Runway. Leave blank to use the original name.</p><div><button type="submit" :disabled="busy">{{busy?'Saving…':'Save nickname'}}</button><button type="button" :disabled="busy" @click="editing=false">Cancel</button></div><p v-if="error" role="alert">{{error}}</p></form>
 </div>
</template>
<style scoped>
.cluster-identity{margin:.75rem 0;color:var(--runway-muted);font-size:.78rem;min-width:0}.cluster-identity-row{display:flex;align-items:center;flex-wrap:wrap;gap:.5rem}.cluster-identity-row>.hl-icon{width:1rem;height:1rem;color:var(--runway-accent)}.cluster-identity button{font:inherit;background:transparent;cursor:pointer}.cluster-identity-link{color:var(--runway-accent);font-weight:650;overflow-wrap:anywhere}.cluster-identity-row>span{border:1px solid var(--runway-border);border-radius:5px;padding:.12rem .35rem;font-size:.7rem}.cluster-identity-edit{color:var(--runway-muted);text-decoration:underline;text-underline-offset:3px;margin-left:.3rem}.cluster-identity-editor{margin-top:.6rem;padding:.8rem;border:1px solid var(--runway-border);border-radius:10px;max-width:32rem;background:var(--runway-soft)}.cluster-identity-editor label{display:grid;gap:.3rem}.cluster-identity-editor input{width:100%;border:1px solid var(--runway-border);border-radius:7px;padding:.6rem;background:var(--runway-input);color:var(--runway-ink)}.cluster-identity-editor p{font-size:.73rem;margin:.5rem 0;line-height:1.6}.cluster-identity-editor button{border:1px solid var(--runway-border);border-radius:7px;padding:.4rem .7rem;color:var(--runway-ink);margin-right:.4rem}.cluster-identity button:disabled{opacity:.45;cursor:not-allowed}
</style>
