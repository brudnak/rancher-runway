<script setup>
import {computed,ref,watch} from 'vue';
import {trackerLibrary,trackerBusy,trackerError,loadTrackers,changeTracker,selectedTracker} from './release-trackers-store.mjs';
import {setActivePanelTab} from './store.js';
const props=defineProps({issue:{type:Object,required:true}});
const opened=ref(false),loading=ref(false),target=ref(''),notice=ref(''),error=ref('');
const trackers=computed(()=>trackerLibrary.value.trackers.filter(t=>!t.archived));
watch(trackers,items=>{if(!items.some(t=>t.id===target.value))target.value=items[0]?.id||'';});
const reference=computed(()=>{const match=props.issue.url.match(/^https:\/\/github\.com\/([^/]+\/[^/]+)\/issues\/(\d+)$/);return match?{repo:match[1].toLowerCase(),number:Number(match[2])}:null;});
const alreadyAdded=computed(()=>trackers.value.find(t=>t.id===target.value)?.issues?.some(i=>i.repo===reference.value?.repo&&i.number===reference.value?.number));
async function show(){opened.value=!opened.value;if(!opened.value)return;loading.value=true;error.value='';await loadTrackers();error.value=trackerError.value;loading.value=false;if(!target.value)target.value=trackers.value[0]?.id||'';}
async function add(){const tracker=trackers.value.find(t=>t.id===target.value);if(!tracker||!reference.value)return;error.value='';notice.value='';if(await changeTracker('add-issue',tracker,reference.value))notice.value=`Added to ${tracker.name}.`;else error.value=trackerError.value;}
function view(){selectedTracker.value=target.value;setActivePanelTab('my-work');}
</script>
<template><div class="add-tracker"><button type="button" :aria-expanded="opened" @click="show">Add to release tracker</button><div v-if="opened" class="tracker-picker"><strong>#{{issue.number}} · {{issue.title}}</strong><p>Keep this issue in your local release, regardless of its owner or GitHub milestone.</p><p v-if="loading" role="status">Loading trackers…</p><template v-else-if="trackers.length"><label>Release tracker<select v-model="target" :disabled="trackerBusy"><option v-for="t in trackers" :key="t.id" :value="t.id">{{t.name}}</option></select></label><button type="button" :disabled="trackerBusy||alreadyAdded||!reference" @click="add">{{trackerBusy?'Saving…':alreadyAdded?'Already added':'Add issue'}}</button><button v-if="alreadyAdded" type="button" @click="view">View in My Work →</button></template><template v-else><p>Create a release tracker in My Work, then add this issue.</p><button type="button" @click="setActivePanelTab('my-work')">Go to My Work</button></template><p v-if="notice" role="status">{{notice}}</p><p v-if="error" role="alert">{{error}}</p></div></div></template>
<style scoped>
.add-tracker{width:100%;font-size:12px}.add-tracker button,.add-tracker select{border:1px solid var(--runway-border);border-radius:7px;padding:7px 10px;color:var(--runway-accent);background:var(--runway-card);cursor:pointer}.add-tracker button:disabled{opacity:.55;cursor:default}.tracker-picker{display:grid;gap:10px;padding:12px;margin:8px 0 14px;border:1px solid var(--runway-border);border-radius:8px}.tracker-picker strong{line-height:1.5}.tracker-picker p{color:var(--runway-muted);line-height:1.5}.tracker-picker label{display:grid;gap:5px}.tracker-picker select{width:100%;min-width:0}.add-tracker :focus-visible{outline:2px solid var(--runway-accent);outline-offset:2px}
</style>
