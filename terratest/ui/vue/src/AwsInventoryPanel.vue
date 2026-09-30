<template>
 <div class="ops-workspace inventory-workspace">
  <header class="ops-hero"><div><span class="ops-eyebrow"><Icon name="globe"/>RANCHER RUNWAY / CLOUD VISIBILITY</span><h2>AWS Inventory<span>.</span></h2><p>See what is there. Understand what belongs. Review what can go.</p></div><div class="ops-hero-actions"><span class="ops-badge"><i :class="{'ops-live':inventory.refreshing}"/>{{ inventory.refreshing?'Scan in progress':inventory.error?'Partial inventory':inventory.updatedAt?'Inventory loaded':'Awaiting inventory' }}</span><button class="ops-button" :disabled="manualRefreshInFlight||inventory.refreshing" @click="refreshChecks"><Icon name="refresh"/>Refresh view</button></div><div class="ops-hero-footer"><span><Icon name="globe"/>{{ inventory.region||'Configured region' }} + global services</span><span><Icon name="fingerprint"/>{{ inventory.owner||'Owner not configured' }}</span><span>{{ updatedLabel||'Waiting for the first scan' }}</span></div></header>
  <div class="ops-stat-grid"><button class="ops-stat" :class="{selected:status==='all'}" @click="status='all'"><span>Resources discovered</span><strong>{{ initialDiscovery(inventory)?'—':items.length }}</strong><small>Matching run prefixes or Owner tags</small></button><button class="ops-stat" :class="{selected:status==='candidates'}" @click="status='candidates'"><span>Cleanup candidates</span><strong class="ops-accent">{{ initialDiscovery(inventory)?'—':candidates.length }}</strong><small>Runway-tagged · no local run record</small></button><button class="ops-stat" :class="{selected:status==='protected'}" @click="status='protected'"><span>Protected resources</span><strong>{{ initialDiscovery(inventory)?'—':protectedCount }}</strong><small>Review the reason on each resource</small></button></div>
  <RefreshStatus :refreshing="inventory.refreshing" label="AWS inventory"/>
  <div v-if="inventory.error" class="ops-alert ops-alert-warning"><Icon name="signal"/><div><strong>This scan is incomplete.</strong><p>Cleanup review covers only the resources listed here. Other resources may remain.</p><details><summary>Scan details</summary><p>{{ inventory.error }}</p></details></div></div>
  <div v-if="error&&!plan" class="ops-alert ops-alert-error" role="alert">{{ error }}</div>
  <section class="ops-cleanup-guide"><div class="ops-guide-icon"><Icon name="compass"/></div><div><h3>A clear path for leftover resources.</h3><p>Candidates match your Owner and Runway tags and have no recorded run here. Review rechecks their dependencies in AWS before presenting the exact deletion effects.</p><details><summary>What stays protected?</summary><p>A local run record, missing ownership evidence, or an unverified dependency can keep a resource protected. IAM review checks EC2 use across enabled regions and includes owned instance profiles and policy detachments. Shared managed policies are retained. Verify that candidates are unused elsewhere.</p></details></div><button class="ops-button" @click="openDestroy">Have a recorded run?<span>Open Destroy</span><Icon name="arrow"/></button></section>
  <section v-if="cleanup.startedAt" class="ops-result-card" aria-live="polite"><div class="ops-section-heading"><div><span class="ops-eyebrow">CLEANUP ACTIVITY</span><h3>{{ cleanup.running?'Cleanup in progress':cleanup.error?'Cleanup needs attention':'Cleanup finished' }}</h3><p>{{ completedCount }} of {{ results.length }} resources processed. {{ cleanup.running?'Keep Runway open while AWS finishes.':'' }}</p></div><span class="ops-badge">{{ cleanup.running?'Running':'Finished' }}</span></div><div class="ops-progress-track"><i :style="{width:(results.length?completedCount/results.length*100:0)+'%'}"/></div><p v-if="cleanup.error" class="ops-warning-text">{{ cleanup.error }}</p><details :open="cleanup.running||!!cleanup.error"><summary>Per-resource results</summary><ul class="ops-result-list"><li v-for="result in results" :key="itemKey(result.resource)"><span class="aws-status" :data-status="result.status">{{ result.status }}</span><div><strong>{{ result.resource.name||result.resource.id }}</strong><small>{{ result.resource.type }} · {{ result.resource.id }}</small><p>{{ result.message }}</p></div></li><li v-if="!results.length">Refresh inventory to check remaining resources.</li></ul></details></section>
  <section class="ops-resource-browser" aria-label="AWS resources"><div class="ops-section-heading"><div><h3>Your AWS footprint.</h3><p>{{ visibleItems.length }} of {{ items.length }} resources · {{ selected.length }} selected</p></div><div class="ops-actions"><label class="ops-search"><Icon name="search"/><input v-model="query" type="search" aria-label="Search AWS resources" placeholder="Name, ID, run, or tag…"/></label><select v-model="type" aria-label="Filter AWS resource type"><option value="">All resource types</option><option v-for="badge in countBadges" :key="badge.type" :value="badge.type">{{ badge.type }} ({{ badge.count }})</option></select></div></div>
   <div class="ops-resource-controls"><div class="ops-segment" role="group" aria-label="Filter cleanup eligibility"><button v-for="filter in [{id:'all',label:'All resources'},{id:'candidates',label:'Candidates'},{id:'protected',label:'Protected'}]" :key="filter.id" :aria-pressed="status===filter.id" :class="{selected:status===filter.id}" @click="status=filter.id">{{ filter.label }}</button></div><div class="ops-actions"><button v-if="selected.length" class="ops-link" :disabled="locked" @click="selected=[]">Clear selection</button><button class="ops-button" :disabled="locked||!selected.length" @click="review(selectedItems,$event)">Review selected ({{ selected.length }})</button><button class="ops-button ops-primary" :disabled="locked||!visibleCandidates.length" @click="review(visibleCandidates,$event)">Review {{ visibleCandidates.length }} visible candidate{{ visibleCandidates.length===1?'':'s' }} <Icon name="arrow"/></button></div></div>
   <p v-if="busy" class="ops-inline-status" role="status"><span class="spinner"/>{{ busy==='review'?'Rechecking ownership and dependencies in AWS…':'Starting the reviewed cleanup…' }}</p><p v-else-if="lifecycleRunning" class="ops-inline-status"><Icon name="lock"/>Cleanup is locked while a lifecycle operation runs. You can still inspect and filter.</p>
   <div v-if="visibleItems.length" class="ops-table-scroll"><table class="ops-inventory-table"><thead><tr><th><input type="checkbox" aria-label="Select visible cleanup candidates" :checked="allSelected" :indeterminate.prop="visibleSelectedCount>0&&!allSelected" :disabled="locked||!visibleCandidates.length" @change="selectAll($event.target.checked)"/></th><th>Resource</th><th>Run & status</th><th>Details</th><th>Cleanup</th></tr></thead><tbody><tr v-for="item in visibleItems" :key="itemKey(item)" :class="{'is-selected':selected.includes(itemKey(item))}"><td><input v-model="selected" type="checkbox" :value="itemKey(item)" :aria-label="`Select ${item.name||item.id}`" :disabled="locked||!item.cleanupEligible" :title="item.cleanupReason||'Select cleanup candidate'"/></td><td><div class="ops-resource-name"><span class="ops-resource-icon"><Icon :name="resourceIcon(item)"/></span><div><small>{{ item.type }}</small><strong>{{ item.name||item.id }}</strong><span>{{ item.region }}</span></div></div></td><td><span class="ops-badge">{{ item.status||'Status not reported' }}</span><small>Run tag {{ item.runId||'not recorded' }}</small><small v-if="item.cleanupEligible">No recorded run here</small></td><td class="ops-resource-details"><p>{{ item.details||item.id }}</p><small v-if="item.owner">{{ item.owner }}</small><details><summary>ID & tags</summary><code>{{ item.id }}</code><p>{{ tagsFor(item)||'No tags returned' }}</p></details></td><td class="ops-resource-cleanup"><template v-if="item.cleanupEligible"><span class="ops-candidate-label"><i/>Candidate</span><button class="ops-button" :disabled="locked" @click="review([item],$event)">Review cleanup <Icon name="arrow"/></button></template><template v-else><span class="ops-protected-label"><Icon name="lock"/>Protected</span><p>{{ (item.cleanupReason||'Ownership or dependencies require verification.').replace(/^Protected:\s*/i,'') }}</p></template></td></tr></tbody></table></div>
   <div v-else class="ops-empty"><Icon :name="query||type||status!=='all'?'search':'globe'"/><h3>{{ initialDiscovery(inventory)?'Getting the picture.':items.length?'No resources match these filters.':inventory.error?'The scan needs attention.':'No matching resources found.' }}</h3><p>{{ initialDiscovery(inventory)?'Waiting for AWS discovery to finish.':items.length?'Try a broader search or another resource type.':'This view covers matching run prefixes and Owner tags, not your entire AWS account.' }}</p><button v-if="items.length" class="ops-link" @click="clearFilters">Clear filters <Icon name="arrow"/></button></div>
  </section>
  <div class="ops-footnote"><Icon name="lock"/><span>Every deletion requires a fresh dependency review and typed confirmation. Browsing this inventory never deletes resources.</span></div>
 </div>
 <Teleport to="body"><dialog ref="reviewDialog" class="aws-review ops-workspace" aria-labelledby="aws-review-title" @cancel.prevent="closeReview"><form v-if="plan" @submit.prevent="confirmCleanup"><header class="aws-review-header"><span class="ops-eyebrow ops-warning-text">PERMANENT AWS DELETION</span><h2 id="aws-review-title">Review {{ plan.items.length }} resource{{ plan.items.length===1?'':'s' }}</h2><p>{{ plan.region }} · Owner {{ plan.owner }}. These resources are not recorded in this workspace. They may still be in use elsewhere.</p><AppBuildStamp/></header><div class="aws-review-content"><p class="ops-alert ops-alert-warning">{{ cleanupWarning }}</p><p v-if="plan.inventoryWarning" class="ops-caption">The inventory scan was incomplete. This review covers only the exact resources below; other resources may remain.</p><ol class="ops-review-items"><li v-for="item in plan.items" :key="itemKey(item.resource)"><strong>{{ item.resource.type }} · {{ item.resource.name||item.resource.id }}</strong><code>{{ item.resource.id }}</code><ul v-if="item.checks?.length" class="ops-review-checks"><li v-for="check in item.checks" :key="check"><Icon name="check"/>{{ check }}</li></ul><ul class="ops-review-effects"><li v-for="effect in item.effects" :key="effect">{{ effect }}</li></ul></li></ol><details v-if="plan.blocked.length" open class="ops-review-blocked"><summary>{{ plan.blocked.length }} protected or unavailable — will not be deleted</summary><p v-for="item in plan.blocked" :key="itemKey(item.resource)"><strong>{{ item.resource.name||item.resource.id }}</strong>: {{ item.message }}</p></details><label v-if="plan.items.length" class="ops-confirm-label"><span class="typed-confirm-prompt">Type <strong>{{ CONFIRMATION_TEXT }}</strong> to continue.</span><input v-model="confirmation" :disabled="!!busy||expired" autocomplete="off" autocapitalize="none" autocorrect="off" spellcheck="false" :placeholder="CONFIRMATION_TEXT" aria-label="Type confirm to continue" class="aws-confirmation typed-confirm-input"/></label><p v-if="expired" class="ops-warning-text">This review expired. Cancel and review again.</p><p v-if="error" class="ops-alert ops-alert-error" role="alert">{{ error }}</p></div><footer class="aws-review-footer"><button type="button" class="ops-button" :disabled="!!busy" autofocus @click="closeReview">Cancel</button><button type="submit" class="ops-button ops-danger-solid" :disabled="!!busy||lifecycleRunning||expired||!plan.items.length||!isConfirmed(confirmation)">{{ busy==='delete'?'Starting cleanup…':`Delete ${plan.items.length} ${plan.items.length===1?'resource':'resources'}` }}</button></footer></form></dialog></Teleport>
</template>

<script setup>
import Icon from './HelmLabIcon.vue';
import {filterAWSResources,toggleVisibleCandidates,awsResourceKey} from './cloud-workspace.mjs';
import RefreshStatus from './RefreshStatus.vue';
import { initialDiscovery } from './panel-presentation.mjs';
import { computed, nextTick, onUnmounted, ref, watch } from 'vue';
import { apiFetch, bootPending, lifecycleRunning, refresh, refreshChecks, manualRefreshInFlight, setActivePanelTab, setActiveDestroyTab, state } from './store.js';
import { readJSON } from './read-json.mjs';
import AppBuildStamp from './AppBuildStamp.vue';
import { CONFIRMATION_TEXT, isConfirmed } from './confirmation.mjs';

const selected = ref([]);
const query=ref(''),type=ref(''),status=ref('all');
const busy = ref('');
const error = ref('');
const plan = ref(null);
const cleanupWarning = computed(() => {
  const types = (plan.value?.items || []).map(item => item.resource.type);
  if (types.some(type => type === 'EC2 instance' || type === 'EBS volume')) return 'Deletion cannot be undone. EC2 termination can permanently delete attached volumes. No backups or snapshots are created. Review every effect below.';
  if (types.some(type => type.startsWith('IAM '))) return 'Review every role, instance profile, and policy detachment below. Deleted IAM resources cannot be restored with the same identity. Shared managed policies are retained.';
  return 'Deletion cannot be undone. Review every resource and effect below before confirming.';
});
const confirmation = ref('');
const reviewDialog = ref(null);
const expired = ref(false);
let expiryTimer;
let reviewTrigger;
const inventory = computed(() => state.value?.aws || {});
const cleanup = computed(() => state.value?.awsCleanup || {});
const items = computed(() => inventory.value.items || []);
const candidates = computed(() => items.value.filter(item => item.cleanupEligible));
const visibleItems=computed(()=>filterAWSResources(items.value,{query:query.value,type:type.value,status:status.value}));
const visibleCandidates=computed(()=>visibleItems.value.filter(item=>item.cleanupEligible));
const visibleSelectedCount=computed(()=>visibleCandidates.value.filter(item=>selected.value.includes(itemKey(item))).length);
const protectedCount=computed(()=>items.value.length-candidates.value.length);
const resourceIcon=item=>item.type.startsWith('IAM')?'lock':item.type.includes('EC2')?'server':item.type.includes('RDS')?'database':item.type.includes('EBS')?'layers':'globe';
function openDestroy(){setActiveDestroyTab('slots');setActivePanelTab('destroy');}
function clearFilters(){query.value='';type.value='';status.value='all';}
const selectedItems = computed(() => candidates.value.filter(item => selected.value.includes(itemKey(item))));
const locked = computed(() => !!busy.value || !!plan.value || bootPending.value || lifecycleRunning.value);
const allSelected = computed(() => visibleCandidates.value.length > 0 && visibleSelectedCount.value === visibleCandidates.value.length);
const results = computed(() => cleanup.value.results || []);
const completedCount = computed(() => results.value.filter(item => ['deleted','failed','blocked'].includes(item.status)).length);
const updatedLabel = computed(() => inventory.value.updatedAt ? `Updated ${new Date(inventory.value.updatedAt).toLocaleTimeString()}` : '');
const summary = computed(() => inventory.value.updatedAt ? `${items.value.length} matching AWS resources · ${inventory.value.region || 'the configured region'} and global services. ${inventory.value.owner ? `Owner ${inventory.value.owner}.` : 'Owner tag not configured.'}` : 'Loading AWS inventory…');
const countBadges = computed(() => {
  const counts = items.value.reduce((all,item) => { all[item.type] = (all[item.type] || 0) + 1; return all; }, {});
  return Object.entries(counts).sort(([a],[b]) => a.localeCompare(b)).map(([type,count]) => ({type,count}));
});
const itemKey = awsResourceKey;
const tagsFor = item => Object.entries(item.tags || {}).map(([key,value]) => `${key}=${value}`).join(' · ');
const selectAll = checked => { selected.value = toggleVisibleCandidates(selected.value,visibleItems.value,checked); };
watch(candidates, items => { const keys = new Set(items.map(itemKey)); selected.value = selected.value.filter(key => keys.has(key)); });
const review = async (resources,event) => {
  if (locked.value || !resources.length) return;
  reviewTrigger = event?.currentTarget || document.activeElement;
  error.value = ''; busy.value = 'review';
  try {
    plan.value = await readJSON(signal => apiFetch('/api/aws/cleanup/preview', { method:'POST', signal, body:JSON.stringify({ resources:resources.map(({type,id}) => ({type,id})) }) }), {label:'AWS cleanup review',timeoutMs:125000});
    confirmation.value = ''; expired.value = false;
    clearTimeout(expiryTimer);
    expiryTimer = setTimeout(() => { expired.value = true; }, Math.max(0,new Date(plan.value.expiresAt).getTime() - Date.now()));
    busy.value = '';
    await nextTick(); reviewDialog.value.showModal();
  } catch (err) { plan.value = null; error.value = err.message; }
  finally { busy.value = ''; }
};
const closeReview = () => {
  if (busy.value) return;
  reviewDialog.value?.close(); plan.value = null; confirmation.value = ''; error.value = ''; clearTimeout(expiryTimer);
  const trigger = reviewTrigger; reviewTrigger = null;
  nextTick(() => { if (trigger?.isConnected) trigger.focus({preventScroll:true}); });
};
const confirmCleanup = async () => {
  if (busy.value || lifecycleRunning.value || expired.value || !plan.value?.items.length || !isConfirmed(confirmation.value)) return;
  busy.value = 'delete'; error.value = '';
  try {
    await readJSON(signal => apiFetch('/api/aws/cleanup', { method:'POST', signal, body:JSON.stringify({token:plan.value.token,confirm:confirmation.value}) }), {label:'AWS cleanup start'});
    state.value = {...state.value, awsCleanup:{running:true,startedAt:new Date().toISOString(),results:plan.value.items.map(item => ({resource:item.resource,status:'queued'}))}};
    selected.value = []; busy.value = ''; closeReview(); await refresh();
  } catch (err) { error.value = `${err.message} Refresh inventory and check cleanup results before retrying.`; }
  finally { busy.value = ''; }
};
onUnmounted(() => { clearTimeout(expiryTimer); reviewDialog.value?.close(); });
</script>
