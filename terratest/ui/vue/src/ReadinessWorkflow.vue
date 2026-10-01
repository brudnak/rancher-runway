<script setup>
import {computed} from 'vue';
const props=defineProps({workflow:Object,statuses:{type:Array,default:()=>[]},buildAvailable:Boolean,compact:Boolean});
const evidence=computed(()=>props.workflow?.state?props.workflow:{state:'unknown',title:'Workflow status unverified',detail:'Check again to read the current GitHub workflow status.',greenLight:false});
const tone=computed(()=>evidence.value.greenLight?'green':['closed','not_required'].includes(evidence.value.state)?'neutral':'caution');
</script>
<template>
 <section class="workflow-check" :class="{compact}" :data-tone="tone" aria-label="Issue workflow and build readiness">
  <div class="workflow-heading"><span>Issue workflow</span><strong>{{evidence.greenLight?'✓ Workflow + build confirmed':evidence.title}}</strong></div>
  <div v-if="statuses.length" class="workflow-status-list"><div v-for="(status,index) in statuses" :key="index"><strong>{{status.status}}</strong><span>{{status.project}}</span></div></div>
  <p v-else class="workflow-unavailable">{{evidence.state==='not_required'?'QA/None':evidence.state==='closed'?'Closed on GitHub':'Status unavailable · no green light yet'}}</p>
  <p>{{evidence.detail}}</p>
  <div v-if="!['closed','not_required'].includes(evidence.state)" class="workflow-build"><span aria-hidden="true">{{buildAvailable?'✓':'○'}}</span>{{buildAvailable?'Matching Rancher build: required fixes detected':'Matching Rancher build: all required fixes not yet proven'}}</div>
  <div v-if="evidence.accessCommand" class="workflow-access"><strong>Enable GitHub project status access</strong><p>Run this in your terminal, finish GitHub authorization, then choose Check again.</p><code>{{evidence.accessCommand}}</code></div>
 </section>
</template>
<style scoped>
.workflow-check{margin:18px 0;padding:18px;border:1px solid #c08a2070;border-radius:12px;background:#c08a2010;line-height:1.6;font-size:12px;overflow-wrap:anywhere}.workflow-check[data-tone=green]{border-color:#319476;background:#31947614}.workflow-check[data-tone=neutral]{border-color:var(--runway-border,#dfe7e4);background:transparent}.workflow-heading{display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap}.workflow-heading>span{font-size:10px;text-transform:uppercase;letter-spacing:.1em;color:var(--runway-muted,#657577)}.workflow-heading>strong{font-size:13px}.workflow-status-list{display:flex;flex-wrap:wrap;gap:12px;margin:14px 0}.workflow-status-list>div{display:grid;gap:4px}.workflow-status-list strong{display:block;padding:5px 12px;border:1px solid #c08a2080;border-radius:8px;background:#c08a2018;font-size:18px;font-weight:700;line-height:1.4}.workflow-check[data-tone=green] .workflow-status-list strong{border-color:#319476;background:#31947620}.workflow-status-list span{font-size:10px;color:var(--runway-muted,#657577)}.workflow-check p{margin:8px 0}.workflow-unavailable{font-weight:650}.workflow-build{display:flex;gap:8px;border-top:1px solid var(--runway-border,#dfe7e4);padding-top:12px;margin-top:12px;font-weight:600}.workflow-access{margin-top:14px;padding-top:12px;border-top:1px solid var(--runway-border,#dfe7e4)}.workflow-access code{display:block;white-space:pre-wrap;font-size:12px;padding:10px;border-radius:7px;background:var(--runway-card,#fff);user-select:all}.compact{margin:10px 0;padding:12px;font-size:11px}.compact .workflow-status-list{margin:8px 0}.compact .workflow-status-list strong{font-size:14px}.compact .workflow-build{padding-top:8px;margin-top:8px}
</style>
