<script setup>
import {computed, ref, watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {packageDate} from './issue-packages.mjs';
import {outcomeLabel, resultForCase} from './issue-package-comparison.mjs';

const props = defineProps({baseline:{type:Object,required:true},caseId:{type:String,default:''},ownerName:{type:String,default:''}});
const heading = ref(null), opened = ref(new Set());
const cases = computed(() => props.baseline.cases || []);
const visibleCases = computed(() => props.caseId ? cases.value.filter(item => item.id === props.caseId) : cases.value);
const finding = computed(() => ({reproduced:'Issue reproduced','not-reproduced':'Not reproduced',validated:'Fix validated','not-validated':'Not validated',inconclusive:'Inconclusive'}[props.baseline.finding] || 'Not recorded'));
const purpose = computed(() => ({reproduction:'Reproduce the original issue',validation:'Validate the fix',exploration:'Explore behavior'}[props.baseline.purpose] || props.baseline.purpose || 'Not recorded'));
const result = caseId => resultForCase(props.baseline,caseId);
const marker = (caseId,stepId) => result(caseId)?.steps?.find(item => item.stepId === stepId);
const evidence = caseId => (props.baseline.evidence || []).filter(item => item.caseId === caseId);
function toggleCase(id,event) {
  const next = new Set(opened.value);
  if (event.target.open) next.add(id);else next.delete(id);
  opened.value = next;
}
watch(() => [props.baseline,props.caseId], () => {opened.value = new Set(props.caseId ? [props.caseId] : []);}, {immediate:true});
defineExpose({focus:() => heading.value?.focus({preventScroll:true})});
</script>

<template>
  <section class="package-baseline-view" aria-label="Preserved baseline snapshot">
    <header class="pbv-heading"><Icon name="bookmark"/><div><span class="pbv-eyebrow">PRESERVED BASELINE</span><h4 ref="heading" tabindex="-1">{{baseline.name || 'Earlier test session'}}</h4><p v-if="ownerName">Snapshot saved with {{ownerName}}.</p></div></header>
    <p class="pbv-preserved"><Icon name="lock"/>This saved snapshot stays available after changes to the plan or deletion of the original session.</p>
    <dl class="pbv-summary"><div><dt>Purpose</dt><dd>{{purpose}}</dd></div><div><dt>Finding</dt><dd>{{finding}}</dd></div><div><dt>Session finished</dt><dd>{{packageDate(baseline.finishedAt,true)}}</dd></div><div><dt>Snapshot saved</dt><dd>{{packageDate(baseline.capturedAt,true)}}</dd></div><div v-if="baseline.environment?.rancherVersion"><dt>Rancher version</dt><dd>{{baseline.environment.rancherVersion}}</dd></div><div v-if="baseline.environment?.kubernetesVersion"><dt>Kubernetes version</dt><dd>{{baseline.environment.kubernetesVersion}}</dd></div></dl>
    <section class="pbv-conclusion"><h5>Conclusion</h5><p>{{baseline.conclusion || 'No conclusion recorded.'}}</p></section>
    <p v-if="caseId&&!visibleCases.length" class="pbv-empty" role="status">The matching case is unavailable in this preserved snapshot.</p>
    <p v-else-if="!cases.length" class="pbv-empty">This baseline contains no test cases.</p>
    <details v-for="item in visibleCases" :key="`${baseline.sessionId}-${item.id}`" class="pbv-case" :open="opened.has(item.id)" @toggle="toggleCase(item.id,$event)">
      <summary><strong>{{item.title || 'Untitled case'}}</strong><span class="pbv-outcome" :data-outcome="result(item.id)?.outcome">{{outcomeLabel(result(item.id)?.outcome)}}</span><Icon name="chevron"/></summary>
      <div v-if="opened.has(item.id)" class="pbv-case-content">
        <section v-if="item.preconditions"><h5>Preconditions</h5><p>{{item.preconditions}}</p></section>
        <section><h5>Steps</h5><ol v-if="item.steps?.length" class="pbv-steps"><li v-for="step in item.steps" :key="step.id"><p>{{step.instruction}}</p><div v-if="step.expected" class="pbv-expected"><h6>Expected</h6><p>{{step.expected}}</p></div><small>{{marker(item.id,step.id)?.done?'Marked performed':'Not marked performed'}}<template v-if="marker(item.id,step.id)?.done&&packageDate(marker(item.id,step.id).at)!=='Not recorded'"> · {{packageDate(marker(item.id,step.id).at,true)}}</template></small></li></ol><p v-else class="pbv-muted">No steps recorded.</p></section>
        <section v-if="item.expected"><h5>Expected outcome</h5><p>{{item.expected}}</p></section>
        <section class="pbv-observation"><h5>Recorded observation</h5><p>{{result(item.id)?.notes || 'No observation recorded.'}}</p></section>
        <section v-if="evidence(item.id).length" class="pbv-evidence"><h5>Evidence references</h5><ul><li v-for="entry in evidence(item.id)" :key="entry.id">{{entry.name || 'Evidence record'}}</li></ul><small>These are the names recorded in the snapshot. Attachment files belong to the original session.</small></section>
      </div>
    </details>
  </section>
</template>

<style scoped>
.package-baseline-view{margin:20px 0;border:1px solid var(--runway-border);border-radius:12px;padding:24px;color:var(--runway-ink);background:var(--runway-card);min-width:0}
.package-baseline-view svg{width:16px;height:16px;flex-shrink:0}
.pbv-heading{display:flex;align-items:flex-start;gap:12px}.pbv-heading>svg{color:var(--runway-accent);margin-top:3px}
.pbv-heading>div{min-width:0}.pbv-eyebrow{font-size:9px;letter-spacing:.12em;color:var(--runway-muted);font-weight:650}
.pbv-heading h4{font-size:19px;font-weight:600;line-height:1.5;margin:7px 0 0;overflow-wrap:anywhere}.pbv-heading h4:focus{outline:none}
.pbv-heading p,.pbv-preserved{font-size:11px;line-height:1.7;color:var(--runway-muted);margin:7px 0 0}.pbv-preserved{display:flex;align-items:flex-start;gap:8px;margin:17px 0}.pbv-preserved>svg{width:12px;height:12px;margin-top:3px}
.pbv-summary{display:grid;grid-template-columns:repeat(auto-fit,minmax(190px,1fr));gap:15px 24px;margin:22px 0}.pbv-summary dt{font-size:10px;line-height:1.6;color:var(--runway-muted);margin-bottom:4px}.pbv-summary dd{font-size:11px;line-height:1.7;margin:0;overflow-wrap:anywhere}
.package-baseline-view h5{font-size:11px;font-weight:600;margin:0 0 8px}.package-baseline-view p{overflow-wrap:anywhere}
.pbv-conclusion{margin:23px 0}.pbv-conclusion p,.pbv-case-content p{font-size:12px;line-height:1.8;white-space:pre-wrap;margin:0}
.pbv-case{border-top:1px solid var(--runway-border)}.pbv-case>summary{display:flex;align-items:center;gap:12px;padding:17px 0;list-style:none;cursor:pointer}.pbv-case>summary::-webkit-details-marker{display:none}.pbv-case>summary strong{font-size:12px;line-height:1.65;overflow-wrap:anywhere;flex:1}.pbv-case>summary svg{width:12px;height:12px;color:var(--runway-muted)}.pbv-case[open]>summary svg{transform:rotate(180deg)}.pbv-case>summary:focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}
.pbv-outcome{font-size:10px;white-space:nowrap;color:var(--runway-muted)}.pbv-outcome[data-outcome=passed]{color:var(--runway-accent)}.pbv-outcome[data-outcome=failed]{color:var(--runway-error)}.pbv-outcome[data-outcome=blocked]{color:var(--runway-gold)}
.pbv-case-content{padding:0 0 23px}.pbv-case-content>section+section{margin-top:22px}.pbv-muted,.pbv-empty{color:var(--runway-muted)}.pbv-empty{font-size:11px;line-height:1.7}
.pbv-steps{padding-left:22px;margin:0}.pbv-steps>li{font-size:11px;line-height:1.8;padding-left:5px;margin:0 0 17px}.pbv-expected{padding:9px 12px;margin-top:9px;background:var(--runway-soft);border-left:2px solid var(--runway-border)}.pbv-expected h6{font-size:10px;margin:0 0 4px}.pbv-steps small,.pbv-evidence small{display:block;font-size:10px;line-height:1.7;color:var(--runway-muted);margin-top:7px}
.pbv-observation{padding:15px 17px;background:var(--runway-soft);border-radius:8px}.pbv-evidence ul{font-size:11px;line-height:1.8;padding-left:18px;margin:0;overflow-wrap:anywhere}
@media(max-width:650px){.package-baseline-view{padding:18px}.pbv-summary{grid-template-columns:1fr 1fr;gap:12px}.pbv-case>summary{gap:8px}.pbv-heading h4{font-size:17px}}
@media(max-width:420px){.pbv-summary{grid-template-columns:1fr}}
</style>
