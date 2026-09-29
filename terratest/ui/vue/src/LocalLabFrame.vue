<script setup>
import { computed, ref, watch } from 'vue';
import LabIcon from './HelmLabIcon.vue';
import { sessionStatus, durationLabel } from './local-lab.mjs';
const props = defineProps({lab:Object,title:String,description:String});
const toolsOpen = ref(false);
const items = computed(()=>props.lab.preflight.items || []);
const blockers = computed(()=>items.value.filter(item=>item.status==='error').length);
const warnings = computed(()=>items.value.filter(item=>item.status==='warning').length);
const running = computed(()=>props.lab.records.filter(record=>sessionStatus(record,props.lab.kind).group==='running').length);
const toolLabel = computed(()=>props.lab.error ? 'Status unavailable' : !props.lab.loaded ? 'Checking tools' : !props.lab.preflight.ready ? 'Tools need attention' : warnings.value ? 'Ready with notes' : 'Ready to launch');
const activityLabel = computed(()=>props.lab.pending ? 'Action in progress' : props.lab.operation.running ? 'Working locally' : props.lab.operation.error ? 'Last action failed' : props.lab.operation.finishedAt ? 'Last action complete' : 'Standing by');
const elapsed = computed(()=>props.lab.pending && !props.lab.operation.running ? '' : durationLabel(props.lab.operation.startedAt,props.lab.operation.running ? props.lab.clock : props.lab.operation.finishedAt));
watch(()=>props.lab.preflight, value=>{if(props.lab.loaded && !value.ready) toolsOpen.value=true;});
</script>

<template>
  <div class="local-lab" :class="`lab-${lab.kind}`">
    <header class="lab-masthead">
      <div class="lab-brand"><span class="lab-emblem"><LabIcon :name="lab.kind === 'k3d' ? 'boxes' : 'pulse'" /></span><div><span class="lab-eyebrow">RANCHER RUNWAY / LOCAL LABS</span><h2>{{ title }}<span>.</span></h2><p>{{ description }}</p></div></div>
      <div class="lab-header-actions"><span class="lab-local-label"><LabIcon name="server" />On your machine</span><button type="button" :disabled="lab.refreshing" @click="lab.refresh(true)"><LabIcon name="refresh" :class="{'lab-spin':lab.refreshing}" />{{ lab.refreshing ? 'Checking…' : 'Refresh status' }}</button></div>
    </header>

    <div class="lab-overview">
      <button type="button" class="lab-overview-item" :aria-expanded="toolsOpen" :aria-controls="`${lab.kind}-tools`" @click="toolsOpen=!toolsOpen"><span class="lab-overview-icon" :data-tone="lab.error ? 'error' : !lab.loaded ? 'muted' : !lab.preflight.ready ? 'error' : warnings ? 'warning' : 'success'"><LabIcon :name="lab.error ? 'signal' : !lab.loaded ? 'clock' : !lab.preflight.ready ? 'signal' : 'check'" /></span><span><span class="lab-eyebrow">LOCAL READINESS</span><strong>{{ toolLabel }}</strong><small>{{ lab.error ? 'Refresh to reconnect' : !lab.loaded ? 'Reading local prerequisites' : blockers ? `${blockers} blocking ${blockers === 1 ? 'check' : 'checks'}` : `${items.length} checks · ${warnings ? `${warnings} to review` : 'view details'}` }}</small></span><LabIcon name="chevron" :class="{'lab-rotated':toolsOpen}" /></button>
      <a class="lab-overview-item" :href="`#${lab.kind}-sessions`"><span class="lab-overview-icon"><LabIcon name="layers" /></span><span><span class="lab-eyebrow">YOUR WORKSPACE</span><strong v-if="lab.loaded">{{ running }} {{ lab.kind === 'steve' ? (running===1?'active endpoint':'active endpoints') : (running===1?'running cluster':'running clusters') }}</strong><strong v-else>Waiting for local status</strong><small>{{ lab.records.length }} {{ lab.records.length === 1 ? 'saved session' : 'saved sessions' }} · view workspace</small></span><LabIcon name="arrow" /></a>
      <a class="lab-overview-item" :href="`#${lab.kind}-activity`"><span class="lab-overview-icon" :data-tone="lab.operation.error && !lab.operation.running ? 'error' : lab.operation.running ? 'working' : 'muted'"><LabIcon name="terminal" /></span><span><span class="lab-eyebrow">ACTIVITY</span><strong>{{ activityLabel }}</strong><small>{{ elapsed ? `${elapsed} · ` : '' }}{{ lab.operation.running ? 'Open live output' : 'Open the activity viewer' }}</small></span><LabIcon name="arrow" /></a>
    </div>
    <div v-if="lab.error" class="lab-alert" role="alert"><LabIcon name="signal" /><div><strong>Local status couldn’t be refreshed.</strong><p>{{ lab.error }}</p><small v-if="lab.loaded">Showing the last successful snapshot. Actions resume after a fresh check.</small></div><button type="button" :disabled="lab.refreshing" @click="lab.refresh(true)">Retry</button></div>
    <section v-if="toolsOpen" :id="`${lab.kind}-tools`" class="lab-readiness" aria-label="Local tool checks">
      <div class="lab-section-heading"><div><h3>Everything your lab needs</h3><p>{{ lab.preflight.summary || 'Checking your local tools…' }}</p></div><slot name="tool-actions" /></div>
      <div class="lab-checks"><div v-for="item in items" :key="item.name" class="lab-tool-check" :data-tone="item.status === 'ok' ? 'success' : item.status"><LabIcon :name="item.status === 'ok' ? 'check' : 'signal'" /><div><strong>{{ item.name }}<span>{{ item.status === 'ok' ? 'Ready' : item.status === 'error' ? 'Required' : 'Note' }}</span></strong><p>{{ item.detail || 'Available on this machine.' }}</p></div></div></div>
    </section>

    <div class="lab-workspace"><aside class="lab-composer" :id="`${lab.kind}-configure`"><slot name="configure" /></aside><div class="lab-session-column"><slot name="sessions" /></div></div>
    <slot name="activity" />
    <footer class="lab-footer"><span><span class="lab-status-dot"></span>Local by design. Built for iteration.</span><span>{{ lab.refreshing ? 'Refreshing local status…' : lab.updatedAt ? `Checked ${new Date(lab.updatedAt).toLocaleTimeString()}` : 'Waiting for local status' }}</span><span>Rancher Runway / {{ title }}</span></footer>
    <div v-if="lab.notice" class="lab-toast" :data-tone="lab.notice.tone" role="status"><LabIcon :name="lab.notice.tone === 'error' ? 'signal' : 'check'" /><span>{{ lab.notice.message }}</span><button type="button" aria-label="Dismiss notification" @click="lab.notice=null"><LabIcon name="close" /></button></div>
  </div>
</template>
