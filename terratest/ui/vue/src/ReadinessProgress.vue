<script setup>
import {computed,ref,onMounted,onBeforeUnmount} from 'vue';
const props=defineProps({progress:{type:Array,default:()=>[]},startedAt:String});
const now=ref(Date.now());let timer;
onMounted(()=>{timer=setInterval(()=>{now.value=Date.now();},1000);});onBeforeUnmount(()=>clearInterval(timer));
const elapsed=computed(()=>Math.max(0,Math.floor((now.value-new Date(props.startedAt||now.value).getTime())/1000)));
const message=computed(()=>props.progress.at(-1)?.message||'Starting the scan…');
</script>
<template><div class="scan-progress"><div class="scan-progress-status" role="status"><i class="scan-spinner" aria-hidden="true"></i><div><strong>{{message}}</strong><span>{{elapsed}}s elapsed · You can cancel at any time.</span></div></div><details v-if="progress.length"><summary>Live activity · {{progress.length}} recent steps</summary><ol><li v-for="(event,index) in progress" :key="index"><time>{{new Date(event.at).toLocaleTimeString()}}</time><span>{{event.message}}</span></li></ol></details></div></template>
<style scoped>
.scan-progress{padding:22px 0;font-size:12px}.scan-progress-status{display:flex;align-items:center;gap:15px}.scan-progress-status strong{display:block;font-weight:600;overflow-wrap:anywhere}.scan-progress-status span{display:block;margin-top:7px;color:var(--runway-muted,#657577);font-size:11px}.scan-spinner{display:block;flex:0 0 26px;height:26px;border:2px solid #31947635;border-top-color:#319476;border-radius:50%;animation:scan-turn 1s linear infinite}.scan-progress details{margin-top:18px;color:var(--runway-muted,#657577)}.scan-progress summary{cursor:pointer}.scan-progress ol{max-height:190px;overflow:auto;margin-top:10px;list-style:none}.scan-progress li{display:flex;gap:12px;padding:5px 0;font-size:11px;overflow-wrap:anywhere}.scan-progress time{flex-shrink:0;font-variant-numeric:tabular-nums}@keyframes scan-turn{to{transform:rotate(360deg)}}@media(prefers-reduced-motion:reduce){.scan-spinner{animation:none}}
</style>
