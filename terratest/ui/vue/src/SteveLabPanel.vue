<script setup>
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue';
import LabIcon from './HelmLabIcon.vue';
import LocalLabFrame from './LocalLabFrame.vue';
import LocalLabSessions from './LocalLabSessions.vue';
import LocalLabSessionCard from './LocalLabSessionCard.vue';
import LocalLabConsole from './LocalLabConsole.vue';
import LocalLabConfirm from './LocalLabConfirm.vue';
import { streamSteveLogs, setActivePanelTab, cacheWorkspaceIntent } from './store.js';
import { useLocalLab } from './use-local-lab.mjs';
import { createLatestTask, endpointURL, kubectlCommand, k3sMinor, liveSteveRun, refError, reuseSteveDraft, steveDraftErrors, steveStartPayload } from './local-lab.mjs';
const props=defineProps({active:{type:Boolean,default:true}});
const lab=useLocalLab('steve',()=>props.active);
const draft=reactive({steveRef:'',k3sVersion:'',httpsPort:'',enableMetrics:false,metricsInterval:15,extraEnv:'',extraArgs:''});
const tags=ref([]),tagsLoading=ref(false),tagsLoaded=ref(false),tagsError=ref(''),sourceMode=ref('release'),versionMode=ref('suggested'),portMode=ref('auto');
const compatibility=ref(null),refLoading=ref(false),refFailure=ref(''),refTask=createLatestTask();
const launchButton=ref(null);
const activity=ref(null),advancedOpen=ref(false),previousDraft=ref(null),draftNote=ref(''),replacement=ref(null);
const stream=ref(null),logText=ref(''),logError=ref(''),logsLoading=ref(false),logTask=createLatestTask();
let refTimer,logTimer,disposed=false;
const draftWithPort=computed(()=>({...draft,httpsPort:portMode.value==='auto'?'':draft.httpsPort}));
const errors=computed(()=>({...steveDraftErrors(draftWithPort.value,lab.records),...(portMode.value==='fixed' && !draft.httpsPort?{httpsPort:'Enter the port you want to reserve.'}:{})}));
const activeRuns=computed(()=>lab.records.filter(liveSteveRun));
const activeIDs=computed(()=>activeRuns.value.map(record=>record.runId).sort().join(','));
const inputBusy=computed(()=>!!lab.pending || lab.operation.running);
const recommendations=computed(()=>compatibility.value?.recommendedK3sVersions || []);
const versions=computed(()=>[...new Set([...recommendations.value,...(lab.state.k3sVersions || [])].filter(Boolean))]);
const startDisabled=computed(()=>lab.blocked || !lab.preflight.ready || Object.keys(errors.value).length>0 || (refLoading.value && versionMode.value==='suggested'));
const startHint=computed(()=>!lab.loaded?'Checking your local tools…':lab.error?'Refresh local status to launch.':lab.pending || lab.operation.running?'The current action must finish first.':!lab.preflight.ready?'Resolve the required tool checks above.':refLoading.value && versionMode.value==='suggested'?'Matching K3s to this Steve ref…':Object.values(errors.value)[0] || (activeRuns.value.length?'Review the active endpoint before replacing it.':'Builds Steve locally and connects it to a dedicated k3d cluster.'));
const minorMismatch=computed(()=>versionMode.value==='manual' && compatibility.value?.recommendedMinor && k3sMinor(draft.k3sVersion) && k3sMinor(draft.k3sVersion)!==compatibility.value.recommendedMinor);
const compatibilityTitle=computed(()=>refLoading.value?'Matching your Kubernetes version…':refFailure.value?'Version suggestion unavailable':compatibility.value?.recommendedMinor?`Source targets Kubernetes ${compatibility.value.recommendedMinor}`:!draft.steveRef?'Choose a Steve ref to match K3s':'Choose the K3s version for this ref');
const runtimeCount=computed(()=>[draft.extraEnv,draft.extraArgs].reduce((count,text)=>count+text.split(/\r?\n/).filter(line=>line.trim()).length,0));
watch(()=>lab.state.k3sVersions,values=>{if(!draft.k3sVersion && values?.length)draft.k3sVersion=values[0];});
async function loadTags() {
  if(tagsLoading.value)return;tagsLoading.value=true;tagsError.value='';
  try {const result=await lab.request('/api/steve/versions');if(disposed)return;tags.value=(result.tags || []).filter(item=>item.name);tagsError.value=result.error || '';tagsLoaded.value=true;if(!draft.steveRef && tags.value.length)draft.steveRef=tags.value[0].name;if(!tags.value.length && !draft.steveRef)sourceMode.value='custom';}
  catch(error){if(!disposed){tagsError.value=error.message;if(!draft.steveRef)sourceMode.value='custom';}}
  finally{if(!disposed)tagsLoading.value=false;}
}
async function inspectRef() {
  const selected=draft.steveRef.trim();
  if(!selected || refError(selected)){refLoading.value=false;return;}
  refLoading.value=true;
  const current=await refTask.run(signal=>lab.request(`/api/steve/ref?ref=${encodeURIComponent(selected)}`,{signal}),result=>{
    compatibility.value=result;refFailure.value=result.error || '';
    if(versionMode.value==='suggested' && result.recommendedK3sVersions?.length)draft.k3sVersion=result.recommendedK3sVersions[0];
  },error=>{refFailure.value=error.message;});
  if(current)refLoading.value=false;
}
watch([()=>draft.steveRef,()=>props.active],()=>{
  clearTimeout(refTimer);refTask.cancel();compatibility.value=null;refFailure.value='';refLoading.value=false;
  if(props.active && draft.steveRef && !refError(draft.steveRef)){refLoading.value=true;refTimer=setTimeout(inspectRef,350);}
},{immediate:true});
watch(()=>props.active,active=>{if(active && !tagsLoaded.value)loadTags();},{immediate:true});
function chooseSource(mode) {sourceMode.value=mode;if(mode==='release' && tags.value.length && !tags.value.some(tag=>tag.name===draft.steveRef))draft.steveRef=tags.value[0].name;}
function useSuggested() {if(recommendations.value.length){draft.k3sVersion=recommendations.value[0];versionMode.value='suggested';}}
function changeDraft(next,message) {previousDraft.value={...draft,sourceMode:sourceMode.value,versionMode:versionMode.value,portMode:portMode.value};versionMode.value='manual';sourceMode.value=tags.value.some(tag=>tag.name===next.steveRef)?'release':'custom';Object.assign(draft,next);portMode.value='auto';draftNote.value=message;replacement.value=null;nextTick(()=>document.getElementById(sourceMode.value==='custom'?'steve-ref':'steve-tag')?.focus());}
function undoDraft() {if(!previousDraft.value)return;const {sourceMode:source,versionMode:version,portMode:port,...previous}=previousDraft.value;Object.assign(draft,previous);sourceMode.value=source;versionMode.value=version;portMode.value=port;previousDraft.value=null;draftNote.value='';}
async function startRun(confirmed=false) {
  if(startDisabled.value)return;
  if(activeRuns.value.length && !confirmed){replacement.value=activeIDs.value;return;}
  if(confirmed && replacement.value!==activeIDs.value){replacement.value=null;lab.notify('The active sessions changed. Review the replacement again.','warning');return;}
  stopStream();activity.value?.expand();
  const success=await lab.action('launch','/api/steve/start',steveStartPayload(draftWithPort.value,activeRuns.value.length>0),'Steve launch started. Follow its progress in Activity.');
  if(success)replacement.value=null;
}
async function readLogs() {
  clearTimeout(logTimer);const selected=stream.value?.runId;if(!selected || !props.active || document.hidden)return;
  logsLoading.value=true;
  const current=await logTask.run(signal=>lab.request(`/api/steve/logs?runId=${encodeURIComponent(selected)}`,{signal}),result=>{logText.value=result.text || '';logError.value='';},error=>{logError.value=error.message;});
  if(current){logsLoading.value=false;if(!disposed && props.active && stream.value?.runId===selected && !document.hidden)logTimer=setTimeout(readLogs,3000);}
}
function stopStream() {logTask.cancel();clearTimeout(logTimer);stream.value=null;logText.value='';logError.value='';logsLoading.value=false;}
function viewLogs(record) {if(stream.value?.runId!==record.runId){logText.value='';logError.value='';stream.value=record;}readLogs();activity.value?.expand();}
function syncLogs(){clearTimeout(logTimer);logTask.cancel();logsLoading.value=false;if(props.active && !document.hidden && stream.value)readLogs();}
watch(()=>props.active,syncLogs);
watch(()=>lab.records,records=>{if(stream.value && lab.loaded && !records.some(record=>record.runId===stream.value.runId))stopStream();});
if(typeof document!=='undefined')document.addEventListener('visibilitychange',syncLogs);
async function sessionAction({type,record,...extra}) {
  if(type==='copy-endpoint')return lab.copy(endpointURL(record),'Endpoint');
  if(type==='copy-command')return lab.copy(kubectlCommand(record),'kubectl command');
  if(type==='copy-path')return lab.copy(extra.path,extra.label);
  if(type==='open-path')return lab.openPath(extra.path,extra.label);
  if(type==='logs')return viewLogs(record);
  if(type==='full-logs')return streamSteveLogs(record);
  if(type==='open-endpoint'){
    try{await lab.request('/api/open-url',{method:'POST',body:JSON.stringify({url:endpointURL(record)})});lab.notify('Endpoint opened.');}catch(error){lab.notify(error.message,'error');}return;
  }
  if(type==='save-kubeconfig')return lab.action(`save-${record.runId}`,'/api/steve/kubeconfig/save',{runId:record.runId},result=>`${result.filename || 'Kubeconfig'} saved to Downloads.`);
  if(type==='cache-lab'){
    const ok=await lab.action(`cache-${record.runId}`,'/api/cache-lab',{action:'steve',runId:record.runId},result=>{cacheWorkspaceIntent.value=result.workspace || '';return 'Snapshot started in Cache Lab.';});
    if(ok)setActivePanelTab('cache');return;
  }
  if(type==='sqlite')return lab.action(`sqlite-${record.runId}`,'/api/steve/sqlite/vacuum',{runId:record.runId},result=>`${result.filename || 'SQLite snapshot'} saved to Downloads.`);
  if(lab.blocked)return;
  if(type==='reuse')return changeDraft(reuseSteveDraft(record),`Settings from ${record.runId}. The version stays pinned; a free HTTPS port will be assigned.`);
  if(type==='stop')return lab.action(`stop-${record.runId}`,'/api/steve/stop',{runId:record.runId},'Endpoint stopped. Its cluster and local files are preserved.');
  if(type==='delete')return lab.action(`delete-${record.runId}`,'/api/steve/cleanup',{runId:record.runId,deleteDir:!!extra.deleteFiles,deleteK3d:true},'Steve session removed.');
}
function refreshActivity(){return stream.value?readLogs():lab.refresh(true);}
onBeforeUnmount(()=>{disposed=true;clearTimeout(refTimer);clearTimeout(logTimer);refTask.cancel();logTask.cancel();document.removeEventListener('visibilitychange',syncLogs);});
</script>

<template>
  <LocalLabFrame :lab="lab" title="Steve Lab" description="Run and inspect Steve locally.">
    <template #configure>
      <div class="lab-composer-heading"><div><span class="lab-eyebrow">LAUNCH PAD</span><h3>Configure Steve</h3></div><LabIcon name="flask" /></div>
      <p class="lab-composer-intro">Choose a source ref and local Kubernetes configuration.</p>
      <div class="lab-topology" aria-label="Steve source is built locally and connects to K3s"><span><LabIcon name="branch" /><strong>Your ref</strong></span><i></i><span><LabIcon name="pulse" /><strong>Steve</strong></span><i></i><span><LabIcon name="boxes" /><strong>K3s</strong></span></div>
      <div v-if="draftNote" class="lab-draft-note" role="status"><p>{{ draftNote }}</p><button type="button" class="lab-text-button" :disabled="lab.blocked" @click="undoDraft"><LabIcon name="undo" />Undo draft change</button></div>
      <form class="lab-form" @submit.prevent="startRun(false)">
        <fieldset :disabled="inputBusy"><legend>Steve source</legend><div class="lab-segmented" role="group" aria-label="Source selection"><button type="button" :aria-pressed="sourceMode==='release'" @click="chooseSource('release')">Release tag</button><button type="button" :aria-pressed="sourceMode==='custom'" @click="chooseSource('custom')">Branch or commit</button></div>
          <div v-if="sourceMode==='release'" class="lab-field"><div class="lab-label-row"><label for="steve-tag">Release tag</label><button type="button" class="lab-text-button" :disabled="tagsLoading" aria-label="Refresh Steve tags" @click="loadTags"><LabIcon name="refresh" :class="{'lab-spin':tagsLoading}" />Refresh</button></div><select id="steve-tag" v-model="draft.steveRef" :disabled="!tags.length && tagsLoading"><option value="" disabled>{{ tagsLoading?'Loading release tags…':'Choose a release tag' }}</option><option v-if="draft.steveRef && !tags.some(tag=>tag.name===draft.steveRef)" :value="draft.steveRef">{{ draft.steveRef }}</option><option v-for="tag in tags" :key="tag.name" :value="tag.name">{{ tag.name }}</option></select></div>
          <div v-else class="lab-field"><label for="steve-ref">Tag, branch, or commit SHA</label><input id="steve-ref" v-model.trim="draft.steveRef" autocomplete="off" spellcheck="false" placeholder="main or a commit SHA" :aria-invalid="!!draft.steveRef && !!errors.steveRef" aria-describedby="steve-ref-help"><p id="steve-ref-help" class="lab-help" :class="{'lab-field-error':draft.steveRef && errors.steveRef}">{{ draft.steveRef && errors.steveRef || 'Use an exact commit to reproduce a specific build.' }}</p></div>
          <p v-if="tagsError" class="lab-field-error">Tags couldn’t be loaded. {{ tagsError }} You can still enter a ref.</p>
        </fieldset>
        <div class="lab-field"><div class="lab-label-row"><label for="steve-version">K3s image version</label><span class="lab-small-label">{{ versionMode==='manual'?'Manual selection':'Auto match' }}</span></div><input id="steve-version" v-model.trim="draft.k3sVersion" list="steve-version-options" :disabled="inputBusy" autocomplete="off" spellcheck="false" :aria-invalid="!!draft.k3sVersion && !!errors.k3sVersion" aria-describedby="steve-version-help" @input="versionMode='manual'"><datalist id="steve-version-options"><option v-for="version in versions" :key="version" :value="version" /></datalist><p v-if="draft.k3sVersion && errors.k3sVersion" id="steve-version-help" class="lab-field-error">{{ errors.k3sVersion }}</p>
          <div v-else id="steve-version-help" class="lab-compatibility" :data-tone="refFailure || minorMismatch?'warning':'success'"><LabIcon :name="refLoading?'refresh':refFailure || minorMismatch?'signal':'layers'" :class="{'lab-spin':refLoading}" /><div><strong>{{ compatibilityTitle }}</strong><p>{{ refFailure || (minorMismatch ? 'Your manual K3s version uses a different minor. Keep it to test compatibility, or use the suggestion below.' : '') || (compatibility?.kubernetesModuleVersion ? `${compatibility.kubernetesModule} ${compatibility.kubernetesModuleVersion}` : 'The source’s Kubernetes dependency guides this suggestion.') }}</p><button v-if="recommendations.length && versionMode==='manual'" type="button" class="lab-text-button" :disabled="inputBusy" @click="useSuggested">Use suggested {{ recommendations[0] }} <LabIcon name="arrow" /></button><button v-if="refFailure" type="button" class="lab-text-button" :disabled="refLoading" @click="inspectRef">Retry suggestion</button></div></div>
        </div>
        <fieldset :disabled="inputBusy"><legend>Experiment profile</legend><div class="lab-choice-row"><label :class="{'lab-choice-selected':!draft.enableMetrics}"><input :checked="!draft.enableMetrics" type="radio" name="steve-profile" @change="draft.enableMetrics=false"><span>Standard<small>Explore the API</small></span><LabIcon v-if="!draft.enableMetrics" name="check" /></label><label :class="{'lab-choice-selected':draft.enableMetrics}"><input :checked="draft.enableMetrics" type="radio" name="steve-profile" @change="draft.enableMetrics=true"><span>Observe<small>Enable metrics</small></span><LabIcon v-if="draft.enableMetrics" name="check" /></label></div></fieldset>
        <details class="lab-advanced" :open="advancedOpen" @toggle="advancedOpen=$event.target.open"><summary><span><LabIcon name="sliders" />Runtime & networking <small v-if="runtimeCount">{{ runtimeCount }} overrides</small></span><LabIcon name="chevron" /></summary><fieldset :disabled="inputBusy" class="lab-advanced-fields"><legend class="lab-sr-only">Runtime and networking</legend>
          <div class="lab-field"><label for="steve-port-mode">HTTPS port</label><select id="steve-port-mode" v-model="portMode"><option value="auto">Automatic · choose a free port</option><option value="fixed">Fixed · choose a port</option></select></div><div v-if="portMode==='fixed'" class="lab-field"><label for="steve-port">Fixed HTTPS port</label><input id="steve-port" v-model="draft.httpsPort" type="number" min="1024" max="65535" step="1" placeholder="8443" :aria-invalid="!!errors.httpsPort" aria-describedby="steve-port-help"><p id="steve-port-help" class="lab-help" :class="{'lab-field-error':errors.httpsPort}">{{ errors.httpsPort || 'Runway checks availability before starting.' }}</p></div>
          <div v-if="draft.enableMetrics" class="lab-field"><label for="steve-metrics-interval">Metrics update interval <span class="lab-label-unit">seconds</span></label><input id="steve-metrics-interval" v-model="draft.metricsInterval" type="number" min="1" step="1" :aria-invalid="!!errors.metricsInterval" aria-describedby="steve-metrics-help"><p id="steve-metrics-help" class="lab-help" :class="{'lab-field-error':errors.metricsInterval}">{{ errors.metricsInterval || 'How often Steve updates its Prometheus metrics.' }}</p></div>
          <div class="lab-field"><label for="steve-env">Environment variables</label><textarea id="steve-env" v-model="draft.extraEnv" rows="4" placeholder="NAME=value" spellcheck="false" autocomplete="off" :aria-invalid="!!errors.extraEnv" aria-describedby="steve-env-help"></textarea><p id="steve-env-help" class="lab-help" :class="{'lab-field-error':errors.extraEnv}">{{ errors.extraEnv || 'One NAME=value per line. Saved with the local session after launch.' }}</p></div>
          <div class="lab-field"><label for="steve-args">Extra arguments</label><textarea id="steve-args" v-model="draft.extraArgs" rows="3" placeholder="--flag=value" spellcheck="false" autocomplete="off" :aria-invalid="!!errors.extraArgs" aria-describedby="steve-args-help"></textarea><p id="steve-args-help" class="lab-help" :class="{'lab-field-error':errors.extraArgs}">{{ errors.extraArgs || 'One argument per line. Spaces inside a value stay intact; no shell quoting or expansion.' }}</p></div>
        </fieldset></details>
        <div class="lab-launch-summary"><span class="lab-eyebrow">YOUR NEXT SESSION</span><div><span>Steve ref</span><strong>{{ draft.steveRef || 'Choose a ref' }}</strong></div><div><span>HTTPS binding</span><strong>{{ portMode==='auto'?'Assigned at launch':draft.httpsPort?`127.0.0.1:${draft.httpsPort}`:'Port needed' }}</strong></div><div><span>Metrics</span><strong>{{ draft.enableMetrics?`Enabled · every ${draft.metricsInterval}s`:'Off' }}</strong></div></div>
        <button ref="launchButton" type="submit" class="lab-primary lab-launch-button" :disabled="startDisabled"><LabIcon :name="lab.pending==='launch'?'refresh':'play'" :class="{'lab-spin':lab.pending==='launch'}" />{{ lab.pending==='launch'?'Launching…':activeRuns.length?'Review replacement':'Launch endpoint' }}<LabIcon name="arrow" /></button><p class="lab-launch-hint" :data-tone="!startDisabled?'muted':'warning'">{{ startHint }}</p>
      </form>
      <LocalLabConfirm v-if="replacement!==null" :return-focus="launchButton" title="Replace the active Steve environment?" :busy="!!lab.pending" :disabled="startDisabled" button-label="Replace & launch" :message="`This stops ${activeRuns.length} active ${activeRuns.length===1?'endpoint':'endpoints'} and permanently removes their clusters and run folders before launching the new build.`" @confirm="startRun(true)" @cancel="replacement=null"><ul class="lab-replacement-list"><li v-for="record in activeRuns" :key="record.runId">{{ record.runId }} · {{ record.steveRef }}</li></ul></LocalLabConfirm>
      <div class="lab-composer-foot"><LabIcon name="lock" /><span>One active Steve endpoint. Dedicated local Kubernetes.</span></div>
    </template>
    <template #sessions><LocalLabSessions kind="steve" :records="lab.records" :loaded="lab.loaded" :unavailable="!!lab.error"><template #default="{record}"><LocalLabSessionCard kind="steve" :record="record" :blocked="lab.blocked" :pending="lab.pending" :streaming="stream?.runId===record.runId" @action="sessionAction" /></template></LocalLabSessions></template>
    <template #activity><LocalLabConsole ref="activity" kind="steve" :text="stream?logText:(lab.operation.output || []).join('\n')" :source="stream?`${stream.runId} / steve.log`:'Startup output'" :command="stream?'':lab.operation.command" :running="lab.operation.running" :live="!!stream && active && !logError" :warning="stream?'':lab.operation.warning" :busy="lab.aborting" :refreshing="stream?logsLoading:lab.refreshing" :error="stream?logError:lab.operation.error" :can-clear="!stream && !lab.pending" @refresh="refreshActivity" @stop="lab.abortAction" @copy="lab.copy($event,'Activity output')" @clear="lab.action('clear','/api/steve/output/clear',{},'Activity output cleared.')"><template #source><button v-if="stream" type="button" @click="stopStream"><LabIcon name="arrow-left" />Startup output</button></template></LocalLabConsole></template>
  </LocalLabFrame>
</template>
