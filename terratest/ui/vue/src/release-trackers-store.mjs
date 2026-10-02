import {ref} from 'vue';
import {milestoneKey} from './release-trackers.mjs';
import {apiFetch} from './store.js';
import {refreshTestPackageSummaries} from './test-packages-store.mjs';
export const trackerLibrary=ref({revision:'initial',trackers:[]}),trackerError=ref(''),trackerBusy=ref(false),selectedTracker=ref(''),selectedMilestone=ref('');
let pending;
function accept(data){trackerLibrary.value=data;if(selectedTracker.value&&!data.trackers.some(t=>t.id===selectedTracker.value&&!t.archived)){selectedTracker.value='';selectedMilestone.value='';}if(selectedMilestone.value&&!data.trackers.find(t=>t.id===selectedTracker.value)?.milestones.some(m=>milestoneKey(m.config)===selectedMilestone.value))selectedMilestone.value='';}
async function read(response){if(!response.ok)throw new Error(await response.text());return response.json();}
export async function loadTrackers(){if(trackerBusy.value)return;if(pending)return pending;pending=(async()=>{try{accept(await read(await apiFetch('/api/release-trackers')));trackerError.value='';}catch(e){trackerError.value=e.message;}finally{pending=null;}})();return pending;}
export async function changeTracker(action,tracker){if(trackerBusy.value)return false;await pending;trackerBusy.value=true;trackerError.value='';try{accept(await read(await apiFetch('/api/release-trackers',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action,tracker:{...tracker,milestones:tracker.milestones.map(m=>({config:m.config}))},revision:trackerLibrary.value.revision})})));try{await refreshTestPackageSummaries();}catch(e){trackerError.value=`Tracker saved. Package list refresh failed: ${e.message}`;}return true;}catch(e){trackerError.value=e.message;return false;}finally{trackerBusy.value=false;}}
