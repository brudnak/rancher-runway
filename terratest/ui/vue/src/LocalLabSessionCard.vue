<script setup>
import { computed, ref } from 'vue';
import LabIcon from './HelmLabIcon.vue';
import LocalLabConfirm from './LocalLabConfirm.vue';
import { sessionStatus, endpointURL, timeLabel, kubectlCommand } from './local-lab.mjs';
const props=defineProps({record:Object,kind:String,blocked:Boolean,pending:String,streaming:Boolean});
const emit=defineEmits(['action']);
const removing=ref(false),deleteFiles=ref(false),removeButton=ref(null);
const status=computed(()=>sessionStatus(props.record,props.kind));
const endpoint=computed(()=>endpointURL(props.record));
const ready=computed(()=>props.kind==='k3d'?props.record.status==='running':props.record.status==='serving');
const title=computed(()=>props.kind==='steve'?props.record.steveRef || 'Steve session':`Cluster ${props.record.runId.replace(/^k3d-/,'')}`);
const files=computed(()=>[
  {label:'Kubeconfig',path:props.record.kubeconfig},
  ...(props.kind==='steve'?[{label:'Runtime log',path:props.record.logPath},{label:'SQLite cache',path:props.record.sourceDir?`${props.record.sourceDir}/informer_object_cache.db`:''}]:[]),
  {label:'Run folder',path:props.record.runDir},
].filter(item=>item.path));
function action(type,extra={}) {emit('action',{type,record:props.record,...extra});}
function remove() {if(props.blocked)return;action('delete',{deleteFiles:deleteFiles.value});}
</script>
<template>
  <article class="lab-session" :data-tone="status.tone" :aria-label="`${title}, ${record.runId}`">
    <header class="lab-session-header"><div class="lab-session-title"><span class="lab-session-symbol"><LabIcon :name="kind==='k3d'?'boxes':'pulse'" /></span><div><h4>{{ title }}</h4><span class="lab-record-id">{{ record.runId }}<template v-if="record.steveCommit"> · {{ record.steveCommit.slice(0,8) }}</template></span></div></div><span class="lab-badge" :data-tone="status.tone"><span class="lab-status-dot" :class="{'lab-pulse':status.tone==='working'}"></span>{{ status.label }}</span></header>
    <div class="lab-session-meta"><span><LabIcon name="layers" />K3s {{ record.k3sVersion || 'Not recorded' }}</span><span :title="record.createdAt"><LabIcon name="clock" />{{ timeLabel(record.createdAt) }}</span><span v-if="record.enableMetrics"><LabIcon name="pulse" />Metrics · {{ record.metricsUpdateIntervalSeconds || 15 }}s</span></div>
    <div class="lab-endpoint"><div><span class="lab-eyebrow">{{ kind==='k3d'?'KUBERNETES API':'STEVE API' }}</span><code>{{ endpoint || (status.tone==='working'?'Preparing your endpoint…':'No endpoint recorded') }}</code></div><button type="button" class="lab-icon-button" :disabled="!endpoint" :aria-label="`Copy endpoint for ${record.runId}`" title="Copy endpoint" @click="action('copy-endpoint')"><LabIcon name="copy" /></button></div>
    <p v-if="record.status==='stopped'" class="lab-help lab-session-hint">{{ kind==='k3d'?'Stopped. Start this cluster to reconnect; its files are still here.':'Endpoint stopped. The local cluster and files remain available.' }}</p>
    <p v-if="record.error" class="lab-record-error"><LabIcon name="signal" />{{ record.error }}</p>
    <div class="lab-button-row lab-session-actions">
      <button v-if="kind==='steve'" type="button" class="lab-primary" :disabled="!ready || !endpoint" @click="action('open-endpoint')"><LabIcon name="external" />Open endpoint</button>
      <button v-else type="button" class="lab-primary" :disabled="!record.kubeconfig" @click="action('copy-command')"><LabIcon name="terminal" />Copy kubectl command</button>
      <button type="button" :disabled="!record.kubeconfig || !!pending" @click="action('save-kubeconfig')"><LabIcon name="download" />{{ pending===`save-${record.runId}`?'Saving…':'Save kubeconfig' }}</button>
      <button v-if="kind==='steve'" type="button" :disabled="!record.logPath" :aria-pressed="streaming" @click="action('logs')"><LabIcon name="terminal" />{{ streaming?'Viewing logs':'View logs' }}</button>
      <button v-else-if="record.status==='stopped'" type="button" :disabled="blocked" @click="action('restart')"><LabIcon name="play" />{{ pending===`restart-${record.runId}`?'Starting…':'Start cluster' }}</button>
    </div>
    <details class="lab-session-details"><summary>Connection & files <LabIcon name="chevron" /></summary><div class="lab-details-content">
      <div class="lab-command-line"><div><span class="lab-eyebrow">RUN AGAINST THIS CLUSTER</span><code>{{ kubectlCommand(record) || 'Kubeconfig not available yet.' }}</code></div><button type="button" :disabled="!record.kubeconfig" :aria-label="`Copy kubectl command for ${record.runId}`" @click="action('copy-command')"><LabIcon name="copy" /></button></div>
      <p class="lab-help">Uses this session’s kubeconfig without changing your terminal’s current context.</p>
      <div v-for="file in files" :key="file.label" class="lab-file-row"><div><strong>{{ file.label }}</strong><code>{{ file.path }}</code></div><button type="button" class="lab-icon-button" :aria-label="`Copy ${file.label.toLowerCase()} path for ${record.runId}`" title="Copy path" @click="action('copy-path',{path:file.path,label:`${file.label} path`})"><LabIcon name="copy" /></button><button type="button" class="lab-icon-button" :aria-label="`Open ${file.label.toLowerCase()} location for ${record.runId}`" title="Show in folder" @click="action('open-path',{path:file.path,label:file.label})"><LabIcon name="folder" /></button></div>
      <div v-if="kind==='steve'" class="lab-button-row"><button type="button" :disabled="!record.logPath" @click="action('full-logs')"><LabIcon name="external" />Open full log viewer</button><button type="button" :disabled="!record.sourceDir || !!pending" @click="action('cache-lab')"><LabIcon name="database" />Capture in Cache Lab</button><button type="button" :disabled="!record.sourceDir || !!pending" @click="action('sqlite')"><LabIcon name="download" />{{ pending===`sqlite-${record.runId}`?'Preparing copy…':'Save SQLite snapshot' }}</button></div>
      <dl class="lab-record-facts"><div><dt>Cluster</dt><dd>{{ record.clusterName || 'Not recorded' }}</dd></div><div v-if="kind==='steve'"><dt>Runtime overrides</dt><dd>{{ (record.extraEnv || []).length }} environment {{ record.extraEnv?.length===1?'variable':'variables' }} · {{ (record.extraArgs || []).length }} {{ record.extraArgs?.length===1?'argument':'arguments' }}</dd></div></dl>
    </div></details>
    <footer class="lab-session-footer"><button type="button" class="lab-text-button" :disabled="blocked" @click="action('reuse')"><LabIcon name="redo" />Use these settings</button><div class="lab-button-row"><button v-if="kind==='k3d' && record.status==='running' || kind==='steve' && record.stevePid" type="button" class="lab-text-button" :disabled="blocked" @click="action('stop')"><LabIcon name="stop" />{{ pending===`stop-${record.runId}`?'Stopping…':kind==='k3d'?'Stop cluster':'Stop endpoint' }}</button><button ref="removeButton" type="button" class="lab-text-button lab-remove-link" :disabled="blocked" :aria-expanded="removing" @click="removing=!removing"><LabIcon name="trash" />Remove…</button></div></footer>
    <LocalLabConfirm v-if="removing" :return-focus="removeButton" :title="kind==='k3d'?'Remove this cluster?':'Remove this Steve session?'" :phrase="record.runId" :busy="!!pending" :disabled="blocked" button-label="Remove session" :message="kind==='k3d'?'This deletes the cluster and its Kubernetes resources. This cannot be undone.':'This stops the endpoint and deletes its k3d cluster and Kubernetes resources. This cannot be undone.'" @confirm="remove" @cancel="removing=false"><label class="lab-check-label"><input v-model="deleteFiles" type="checkbox" :disabled="!!pending">Also delete the run folder and local files</label><p class="lab-help">{{ deleteFiles?'Logs, kubeconfig, and other files in the run folder will be permanently removed.':'Keep local files on disk for inspection. The session will leave this list.' }}</p></LocalLabConfirm>
  </article>
</template>
