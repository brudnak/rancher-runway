<script setup>
import { computed, nextTick, ref, watch } from 'vue';
import LabIcon from './HelmLabIcon.vue';
import { logLines, highlightMatch } from './local-lab.mjs';
const props=defineProps({kind:String,text:{type:String,default:''},source:{type:String,default:'Startup output'},running:Boolean,live:Boolean,busy:Boolean,refreshing:Boolean,error:String,warning:String,command:String,canClear:Boolean});
const emit=defineEmits(['refresh','stop','clear','copy','startup']);
const query=ref(''),issuesOnly=ref(false),wrapped=ref(true),paused=ref(false),frozen=ref(''),follow=ref(true),viewport=ref(null),collapsed=ref(false);
const buffer=computed(()=>paused.value?frozen.value:props.text);
const lines=computed(()=>buffer.value?logLines(buffer.value,query.value,issuesOnly.value):[]);
const visible=computed(()=>lines.value.slice(-1000));
const hiddenCount=computed(()=>Math.max(0,lines.value.length-visible.value.length));
const freshCount=computed(()=>Math.max(0,props.text.split('\n').length-frozen.value.split('\n').length));
async function latest() {follow.value=true;await nextTick();if(viewport.value)viewport.value.scrollTop=viewport.value.scrollHeight;}
function onScroll() {if(viewport.value && viewport.value.scrollHeight>viewport.value.clientHeight+30)follow.value=viewport.value.scrollHeight-viewport.value.scrollTop-viewport.value.clientHeight<32;}
function togglePause() {if(!paused.value)frozen.value=props.text;paused.value=!paused.value;if(!paused.value)latest();}
function expand() {collapsed.value=false;nextTick(()=>{document.getElementById(`${props.kind}-activity`)?.scrollIntoView({block:'start',behavior:'auto'});});}
defineExpose({expand});
watch(()=>props.text,()=>{if(follow.value&&!paused.value)latest();});
watch(()=>props.source,()=>{paused.value=false;frozen.value='';query.value='';issuesOnly.value=false;follow.value=true;});
watch([query,issuesOnly],()=>nextTick(()=>{if(viewport.value)viewport.value.scrollTop=0;}));
watch(()=>props.running,running=>{if(running)collapsed.value=false;});
</script>
<template>
  <section :id="`${kind}-activity`" class="lab-console" :aria-labelledby="`${kind}-activity-title`">
    <header class="lab-console-header"><div class="lab-console-title"><span class="lab-console-symbol"><LabIcon name="terminal" /></span><div><h3 :id="`${kind}-activity-title`">Activity</h3><span>{{ source }}<span v-if="running || live" class="lab-live-label"><span class="lab-status-dot lab-pulse"></span>Live</span></span></div></div><div class="lab-button-row"><slot name="source" /><button v-if="running" type="button" :disabled="busy" @click="$emit('stop')"><LabIcon name="stop" />Stop action</button><button type="button" :disabled="refreshing" aria-label="Refresh activity" @click="$emit('refresh')"><LabIcon name="refresh" :class="{'lab-spin':refreshing}" /></button><button type="button" :aria-label="collapsed?'Expand activity':'Collapse activity'" :aria-expanded="!collapsed" :aria-controls="`${kind}-console-body`" @click="collapsed=!collapsed"><LabIcon name="chevron" :class="{'lab-rotated':!collapsed}" /></button></div></header>
    <div v-show="!collapsed" :id="`${kind}-console-body`">
      <div class="lab-console-toolbar"><div class="lab-search"><LabIcon name="search" /><input v-model="query" type="search" :aria-label="`Search ${kind} activity`" placeholder="Find in output…"><button v-if="query" type="button" aria-label="Clear activity search" @click="query=''"><LabIcon name="close" /></button></div><button type="button" :aria-pressed="issuesOnly" @click="issuesOnly=!issuesOnly"><LabIcon name="signal" />Issues only</button><button type="button" :aria-pressed="wrapped" @click="wrapped=!wrapped">Wrap lines</button><button type="button" :disabled="!text" :aria-pressed="paused" @click="togglePause"><LabIcon :name="paused?'play':'pause'" />{{ paused?'Resume view':'Pause view' }}</button></div>
      <p v-if="error" class="lab-console-error" role="status"><LabIcon name="signal" />{{ error }}</p>
      <p v-if="warning" class="lab-console-warning" role="status"><LabIcon name="signal" />{{ warning }}</p>
      <div v-if="paused" class="lab-console-paused" role="status">View paused. {{ freshCount ? `${freshCount} new lines available.` : 'Your place is saved.' }}<button type="button" class="lab-text-button" @click="togglePause">Resume <LabIcon name="arrow" /></button></div>
      <div v-if="command" class="lab-command-caption"><span>Current action</span><code>{{ command }}</code></div>
      <div ref="viewport" class="lab-console-output" :class="{'lab-console-wrap':wrapped}" tabindex="0" role="region" :aria-label="`${kind} activity output`" @scroll="onScroll">
        <p v-if="hiddenCount" class="lab-console-limit">Showing the latest 1,000 matching lines. Copy includes all {{ lines.length.toLocaleString() }} matches.</p>
        <div v-for="line in visible" :key="line.number" class="lab-log-line" :data-tone="line.tone"><span class="lab-line-number" aria-hidden="true">{{ line.number }}</span><code><template v-for="(part,index) in highlightMatch(line.text,query)" :key="index"><mark v-if="part.match">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template>{{ line.text ? '' : ' ' }}</code></div>
        <div v-if="!visible.length" class="lab-console-empty"><LabIcon :name="text?'search':'terminal'" /><strong>{{ text?'No lines match your filters.':running || live || refreshing?'Waiting for output…':'A quiet console. A clear starting point.' }}</strong><p>{{ text?'Clear the search or turn off Issues only.':live?'Reading this session’s runtime log.':running?'Following this action’s progress.':'Launch a session to follow its progress here.' }}</p><button v-if="text" type="button" @click="query='';issuesOnly=false">Clear filters</button></div>
      </div>
      <footer class="lab-console-footer"><span role="status">{{ lines.length.toLocaleString() }} {{ lines.length===1?'line':'lines' }}{{ query || issuesOnly ? ' matched' : '' }}</span><button v-if="!follow && text" type="button" class="lab-text-button" @click="latest">Jump to latest <LabIcon name="arrow" /></button><div class="lab-button-row"><button type="button" :disabled="!lines.length" @click="$emit('copy',lines.map(line=>line.text).join('\n'))"><LabIcon name="copy" />{{ query || issuesOnly?'Copy matches':'Copy output' }}</button><button v-if="canClear" type="button" :disabled="busy || running || !text" @click="$emit('clear')">Clear output</button></div></footer>
    </div>
  </section>
</template>
