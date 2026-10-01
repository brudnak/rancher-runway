<script setup>
import { computed, nextTick, reactive, ref, watch } from 'vue';
import LabIcon from './HelmLabIcon.vue';
import LocalLabFrame from './LocalLabFrame.vue';
import LocalLabSessions from './LocalLabSessions.vue';
import LocalLabSessionCard from './LocalLabSessionCard.vue';
import LocalLabConsole from './LocalLabConsole.vue';
import { useLocalLab } from './use-local-lab.mjs';
import { imageTagError, normalizeImageTag, portError, endpointURL, kubectlCommand } from './local-lab.mjs';
const props=defineProps({active:{type:Boolean,default:true}});
const lab=useLocalLab('k3d',()=>props.active);
const draft=reactive({k3sVersion:'',apiPort:''}),portMode=ref('auto'),activity=ref(null),previousDraft=ref(null),draftNote=ref('');
const versions=computed(()=>[...new Set((lab.state.k3sVersions || []).filter(Boolean))]);
watch(versions,items=>{if(!draft.k3sVersion && items.length)draft.k3sVersion=items[0];});
const port=computed(()=>portMode.value==='auto'?'':draft.apiPort);
const errors=computed(()=>({version:imageTagError(draft.k3sVersion),port:portMode.value==='fixed' && !draft.apiPort?'Enter the port you want to reserve.':portError(port.value,lab.records)}));
const missingK3D=computed(()=>(lab.preflight.items || []).some(item=>item.name==='k3d' && item.status==='error'));
const startDisabled=computed(()=>lab.blocked || !lab.preflight.ready || !!errors.value.version || !!errors.value.port);
const startHint=computed(()=>!lab.loaded?'Checking your local tools…':lab.error?'Refresh local status to launch.':lab.pending || lab.operation.running?'The current action must finish first.':!lab.preflight.ready?'Resolve the required tool checks above.':errors.value.version || errors.value.port || 'Creates a separate cluster. Existing clusters keep running.');
async function startCluster() {
  if(startDisabled.value)return;
  activity.value?.expand();
  await lab.action('launch','/api/k3d/start',{k3sVersion:normalizeImageTag(draft.k3sVersion),apiPort:Number(port.value || 0)},'Cluster launch started. Follow its progress in Activity.');
}
function setDraft(next,message) {previousDraft.value={...draft,portMode:portMode.value};Object.assign(draft,next);portMode.value='auto';draftNote.value=message;nextTick(()=>document.getElementById('k3d-version')?.focus());}
function undoDraft() {if(!previousDraft.value)return;Object.assign(draft,{k3sVersion:previousDraft.value.k3sVersion,apiPort:previousDraft.value.apiPort});portMode.value=previousDraft.value.portMode;previousDraft.value=null;draftNote.value='';}
async function sessionAction({type,record,...extra}) {
  if(type==='copy-endpoint')return lab.copy(endpointURL(record),'Endpoint');
  if(type==='copy-command')return lab.copy(kubectlCommand(record),'kubectl command');
  if(type==='copy-path')return lab.copy(extra.path,extra.label);
  if(type==='open-path')return lab.openPath(extra.path,extra.label);
  if(type==='save-kubeconfig')return lab.action(`save-${record.runId}`,'/api/k3d/kubeconfig/save',{runId:record.runId},result=>`${result.filename || 'Kubeconfig'} saved to Downloads.`);
  if(lab.blocked)return;
  if(type==='reuse')return setDraft({k3sVersion:record.k3sVersion,apiPort:''},`Settings from ${record.runId}. Port set to Automatic to avoid a collision.`);
  if(['restart','stop','delete'].includes(type)) await lab.action(`${type}-${record.runId}`,`/api/k3d/${type}`,{runId:record.runId,deleteDir:!!extra.deleteFiles},type==='restart'?'Cluster started.':type==='stop'?'Cluster stopped. Your local files are preserved.':'Cluster removed.');
}
</script>

<template>
  <LocalLabFrame :lab="lab" title="K3D Lab" description="Local K3s clusters in Docker.">
    <template #tool-actions><button v-if="missingK3D" type="button" :disabled="lab.blocked" @click="lab.action('install','/api/k3d/install',{},'k3d installation started. Follow Activity for progress.')"><LabIcon name="download" />{{ lab.pending==='install'?'Installing…':'Install k3d' }}</button></template>
    <template #configure>
      <div class="lab-composer-heading"><div><span class="lab-eyebrow">LAUNCH PAD</span><h3>Configure a cluster</h3></div><LabIcon name="boxes" /></div>
      <p class="lab-composer-intro">Kubernetes in Docker, with a dedicated API port and kubeconfig for each experiment.</p>
      <div class="lab-topology" aria-label="Docker runs K3s and exposes a Kubernetes API"><span><LabIcon name="server" /><strong>Docker</strong></span><i></i><span><LabIcon name="boxes" /><strong>K3s</strong></span><i></i><span><LabIcon name="terminal" /><strong>Your API</strong></span></div>
      <div v-if="draftNote" class="lab-draft-note" role="status"><p>{{ draftNote }}</p><button type="button" class="lab-text-button" :disabled="lab.blocked" @click="undoDraft"><LabIcon name="undo" />Undo draft change</button></div>
      <form class="lab-form" @submit.prevent="startCluster">
        <div class="lab-field"><label for="k3d-version">K3s image version</label><input id="k3d-version" v-model.trim="draft.k3sVersion" list="k3d-version-options" autocomplete="off" spellcheck="false" placeholder="Choose a K3s image tag" :disabled="!!lab.pending || lab.operation.running" :aria-invalid="!!draft.k3sVersion && !!errors.version" aria-describedby="k3d-version-help"><datalist id="k3d-version-options"><option v-for="version in versions" :key="version" :value="version" /></datalist><p id="k3d-version-help" class="lab-help" :class="{'lab-field-error':draft.k3sVersion && errors.version}">{{ draft.k3sVersion && errors.version || 'Pick a suggested version, or enter an exact K3s image tag.' }}</p></div>
        <fieldset class="lab-port-options" :disabled="!!lab.pending || lab.operation.running"><legend>Kubernetes API port</legend><div class="lab-choice-row"><label :class="{'lab-choice-selected':portMode==='auto'}"><input v-model="portMode" type="radio" value="auto" name="k3d-port-mode"><span>Automatic<small>Find a free port</small></span><LabIcon v-if="portMode==='auto'" name="check" /></label><label :class="{'lab-choice-selected':portMode==='fixed'}"><input v-model="portMode" type="radio" value="fixed" name="k3d-port-mode"><span>Fixed<small>Choose your own</small></span><LabIcon v-if="portMode==='fixed'" name="check" /></label></div><div v-if="portMode==='fixed'" class="lab-field"><label for="k3d-port">API port</label><input id="k3d-port" v-model="draft.apiPort" type="number" min="1024" max="65535" step="1" placeholder="16443" :aria-invalid="!!errors.port" aria-describedby="k3d-port-help"><p id="k3d-port-help" class="lab-help" :class="{'lab-field-error':errors.port}">{{ errors.port || 'Runway checks availability before starting.' }}</p></div></fieldset>
        <div class="lab-launch-summary"><span class="lab-eyebrow">YOUR NEXT SESSION</span><div><span>Image</span><strong>{{ draft.k3sVersion || 'Choose a version' }}</strong></div><div><span>API binding</span><strong>{{ portMode==='auto'?'Assigned at launch':port?`127.0.0.1:${port}`:'Port needed' }}</strong></div><div><span>Isolation</span><strong>Separate cluster & kubeconfig</strong></div></div>
        <button type="submit" class="lab-primary lab-launch-button" :disabled="startDisabled"><LabIcon :name="lab.pending==='launch'?'refresh':'play'" :class="{'lab-spin':lab.pending==='launch'}" />{{ lab.pending==='launch'?'Launching…':lab.records.length?'Launch another cluster':'Launch cluster' }}<LabIcon name="arrow" /></button><p class="lab-launch-hint" :data-tone="!startDisabled?'muted':'warning'">{{ startHint }}</p>
      </form>
      <div class="lab-composer-foot"><LabIcon name="lock" /><span>Local resources only. No cloud credentials needed.</span></div>
    </template>
    <template #sessions><LocalLabSessions kind="k3d" :records="lab.records" :loaded="lab.loaded" :unavailable="!!lab.error"><template #default="{record}"><LocalLabSessionCard kind="k3d" :record="record" :blocked="lab.blocked" :pending="lab.pending" @action="sessionAction" /></template></LocalLabSessions></template>
    <template #activity><LocalLabConsole ref="activity" kind="k3d" :text="(lab.operation.output || []).join('\n')" :command="lab.operation.command" :running="lab.operation.running" :busy="lab.aborting" :refreshing="lab.refreshing" :error="lab.operation.error" :warning="lab.operation.warning" :can-clear="!lab.pending" @refresh="lab.refresh(true)" @stop="lab.abortAction" @copy="lab.copy($event,'Activity output')" @clear="lab.action('clear','/api/k3d/output/clear',{},'Activity output cleared.')" /></template>
  </LocalLabFrame>
</template>
