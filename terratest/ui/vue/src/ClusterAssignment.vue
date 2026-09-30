<script setup>
import {computed,ref,watch} from 'vue';
import ClusterIdentity from './ClusterIdentity.vue';
import {clusterWorkspaces,clusterDisplayName} from './cluster-workspace-store.mjs';
import {rancherConnectionURL} from './rancher-connection.mjs';
const props=defineProps({clusterId:String,host:String,disabled:Boolean,allowChange:{type:Boolean,default:true}});
const emit=defineEmits(['assign']);
const choice=ref(''),editing=ref(false);
const choices=computed(()=>clusterWorkspaces.value.filter(item=>!props.host||rancherConnectionURL(item.url)===rancherConnectionURL(props.host)));
const canChange=computed(()=>props.allowChange&&choices.value.some(c=>c.id!==props.clusterId));
watch(()=>props.clusterId,()=>{editing.value=false;choice.value='';});
</script>
<template><div><ClusterIdentity v-if="clusterId" :cluster-id="clusterId" :fallback="host" :disabled="disabled"/><button v-if="clusterId&&canChange&&!editing" type="button" class="cluster-relink" :disabled="disabled" @click="editing=true">Change history link</button><div v-if="!clusterId||editing" class="cluster-assignment"><span>Cluster history</span><template v-if="choices.length"><select v-model="choice" aria-label="Link to a cluster" :disabled="disabled"><option value="">Choose a cluster…</option><option v-for="item in choices" :key="item.id" :value="item.id">{{clusterDisplayName(item.id)}} · {{item.id}}</option></select><button type="button" :disabled="disabled||!choice||choice===clusterId" @click="emit('assign',choice)">Link</button><button v-if="editing" type="button" @click="editing=false">Cancel</button><small v-if="editing">Changes the local history association. The recorded target stays unchanged.</small></template><small v-else>Saved locally. A matching cluster will be linked when it is discovered.</small></div></div></template>
<style scoped>
.cluster-assignment{display:flex;align-items:center;flex-wrap:wrap;gap:.6rem;margin:.85rem 0;font-size:.75rem;color:var(--runway-muted)}.cluster-assignment>span{font-weight:600}.cluster-assignment select{max-width:100%;padding:.45rem .6rem;border:1px solid var(--runway-border);border-radius:7px;background:var(--runway-input);color:var(--runway-ink)}.cluster-assignment button,.cluster-relink{color:var(--runway-accent);font-weight:650;padding:.4rem;cursor:pointer}.cluster-relink{font-size:.72rem}.cluster-assignment small{flex-basis:100%}.cluster-assignment button:disabled,.cluster-relink:disabled{opacity:.4;cursor:not-allowed}
</style>
