<script setup>
import {computed} from 'vue';
const props = defineProps({step: {type:Object, required:true}, index:Number});
const plan = computed(() => props.step.operation?.plan);
const facts = computed(() => Object.entries({
  'Chart': plan.value?.chart?.version, 'Chart repository': plan.value?.repository,
  'Server image': plan.value?.image && `${plan.value.image}:${plan.value.imageTag}`,
  'Server digest': plan.value?.digest, 'Source commit': plan.value?.imageRevision,
  'OSS commit': plan.value?.imageOSSRevision, 'Canonical image': plan.value?.imageCanonicalReference,
  'Agent image': plan.value?.agentImage, 'Agent digest': plan.value?.agentDigest,
  'Version evidence': plan.value?.targetVersionSource, 'OCI version': plan.value?.targetVersionLabel,
  'Previous Helm revision': plan.value?.before?.revision, 'Previous image': plan.value?.before?.image,
  'Finished': props.step.operation?.finished,
}).filter(([,value]) => value !== undefined && value !== null && value !== ''));
const date = value => value ? new Date(value).toLocaleString() : 'Date not recorded';
const pretty = value => JSON.stringify(value, null, 2);
</script>
<template>
 <li class="history-step" :data-status="step.status">
  <span class="history-dot" aria-hidden="true">{{ index === 0 ? '○' : step.status === 'succeeded' ? '✓' : '↗' }}</span>
  <article>
   <header><div><p class="step-label">{{ step.title }} <span>· {{ date(step.at) }}</span></p><h4><span v-if="step.from" class="step-from">{{ step.from }} <span aria-hidden="true">→</span> </span>{{ step.version || 'Version not recorded' }}</h4></div><span class="step-status">{{ step.status }}</span></header>
   <p class="step-note">{{ step.note }}</p>
   <p v-if="plan" class="step-summary">Chart {{ plan.chart?.version || 'unknown' }} · {{ plan.image }}:{{ plan.imageTag }}</p>
   <details class="step-details"><summary>Commands & metadata <span>{{ step.commands.length }} saved command{{ step.commands.length === 1 ? '' : 's' }}</span></summary>
    <p v-if="!step.commands.length" class="step-note">No Helm command was retained for this step. It cannot be reconstructed exactly.</p>
    <div v-for="command in step.commands" :key="command.id"><p class="step-label">{{ index === 0 ? 'Original Helm install command' : 'Prepared Helm upgrade command' }}</p><pre>{{ command.data.command }}</pre><p class="step-note">{{ command.data.source }}<template v-if="index > 0"> · Prepared before preflight; check the operation log to confirm execution.</template></p></div>
    <dl v-if="facts.length"><div v-for="[label,value] in facts" :key="label"><dt>{{ label }}</dt><dd>{{ value }}</dd></div></dl>
    <details v-if="step.observations.length"><summary>Runtime observations in this period ({{ step.observations.length }})</summary><p class="step-note">Captured before the next recorded operation. Later observations can include changes made outside Runway.</p><div v-for="observation in step.observations" :key="observation.id"><p class="step-label">{{ date(observation.at) }} · Rancher {{ observation.data.rancherVersion || 'unknown' }} · Webhook {{ observation.data.webhookChartVersion || 'unknown' }}</p><pre>{{ pretty(observation.data) }}</pre></div></details>
    <details v-if="step.operation?.events?.length"><summary>Checks, pod changes & operation log ({{ step.operation.events.length }})</summary><pre>{{ step.operation.events.map(event => `${date(event.at)}  ${event.message}`).join('\n\n') }}</pre></details>
    <details><summary>Full saved {{ index === 0 ? 'configuration' : 'operation' }}</summary><pre>{{ pretty(step.operation || step.metadata) }}</pre></details>
   </details>
  </article>
 </li>
</template>
<style scoped>
.history-step{position:relative;padding:0 0 24px 38px;border-left:1px solid var(--history-border);margin-left:13px}.history-step:last-child{border-left-color:transparent;padding-bottom:0}.history-dot{position:absolute;left:-13px;top:0;width:26px;height:26px;border:1px solid var(--history-border);border-radius:50%;display:grid;place-items:center;background:var(--history-card);color:var(--history-accent)}article{border:1px solid var(--history-border);border-radius:12px;padding:18px;background:var(--history-card)}header{display:flex;align-items:start;justify-content:space-between;gap:12px}.step-label{font-size:12px;font-weight:600;margin:0 0 6px}.step-label span,.step-from{color:var(--history-muted);font-weight:400}h4{font-size:19px;font-weight:650;overflow-wrap:anywhere}.step-status{font-size:11px;border:1px solid var(--history-border);border-radius:20px;padding:4px 10px;color:var(--history-muted)}[data-status=succeeded] .step-status{color:var(--history-accent)}[data-status=failed] .step-status,[data-status=interrupted] .step-status{color:#dc8066}.step-note{font-size:12px;line-height:1.6;color:var(--history-muted);margin:8px 0}.step-summary{font-family:ui-monospace,monospace;font-size:11px;overflow-wrap:anywhere;color:var(--history-muted);margin:8px 0}.step-details{border-top:1px solid var(--history-border);padding-top:12px;margin-top:14px}summary{cursor:pointer;font-size:12px;font-weight:600}summary span{float:right;font-size:11px;font-weight:400;color:var(--history-muted)}details details{margin:14px 0}pre{white-space:pre-wrap;overflow-wrap:anywhere;max-height:420px;overflow:auto;background:var(--history-soft);border-radius:8px;padding:14px;font:11px/1.7 ui-monospace,monospace;margin:12px 0}dl{display:grid;gap:12px;grid-template-columns:repeat(2,minmax(0,1fr));margin:18px 0}dt{font-size:11px;color:var(--history-muted)}dd{font:12px/1.6 ui-monospace,monospace;overflow-wrap:anywhere}@media(max-width:640px){.history-step{padding-left:22px}header{flex-direction:column}dl{grid-template-columns:1fr}summary span{float:none;display:block}}
</style>
