<template>
  <div class="mb-4 flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
    <div>
      <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">AWS Inventory</h2>
      <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">{{ summary }}</p>
    </div>
    <span class="text-sm text-zinc-500">{{ updatedLabel }}</span>
  </div>

  <div class="aws-inventory grid min-w-0 gap-3">
    <div v-if="inventory.error" class="aws-warning">
      <strong>Inventory is incomplete.</strong> Review all covers only the candidates currently listed.
      <details class="mt-2"><summary>Scan details</summary><p class="mt-2 break-words">{{ inventory.error }}</p></details>
      <AppBuildStamp />
    </div>
    <div v-if="error && !plan" class="aws-warning" role="alert">{{ error }}<AppBuildStamp /></div>

    <div class="aws-cleanup-toolbar">
      <div>
        <h3 class="font-semibold text-zinc-900 dark:text-zinc-100">Clean up leftover resources</h3>
        <p class="mt-1 text-sm leading-6 text-zinc-600 dark:text-zinc-400">
          {{ candidates.length }} cleanup candidate{{ candidates.length === 1 ? '' : 's' }} · {{ items.length - candidates.length }} protected.
          Candidates have your Owner tag and Runway tags but no recorded run here. Verify they are unused before deleting.
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button class="aws-button" :disabled="locked || !selected.length" @click="review(selectedItems)">Review selected ({{ selected.length }})</button>
        <button class="aws-button aws-danger" :disabled="locked || !candidates.length" @click="review(candidates)">Review all candidates</button>
        <span v-if="busy" class="text-sm text-zinc-500" role="status">{{ busy === 'review' ? 'Checking resources in AWS…' : 'Starting cleanup…' }}</span>
      </div>
      <p v-if="lifecycleRunning" class="text-xs text-amber-700 dark:text-amber-300">Cleanup actions are locked while an operation is running.</p>
    </div>

    <section v-if="cleanup.startedAt" class="aws-results" aria-live="polite">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 class="font-semibold">{{ cleanup.running ? 'Cleanup in progress' : 'Cleanup finished' }}</h3>
        <span class="text-sm">{{ completedCount }} / {{ results.length }} processed</span>
      </div>
      <p v-if="cleanup.running" class="mt-2 text-sm text-zinc-500">Keep Runway open. AWS may take several minutes to finish deleting a resource.</p>
      <p v-if="cleanup.error" class="mt-2 text-sm text-amber-700 dark:text-amber-300">{{ cleanup.error }}</p>
      <AppBuildStamp />
      <details class="mt-3" :open="cleanup.running || !!cleanup.error">
        <summary class="cursor-pointer text-sm font-semibold">Per-resource results</summary>
        <ul class="mt-2 grid gap-2 text-sm">
          <li v-for="result in results" :key="itemKey(result.resource)" class="aws-result-row">
            <span class="aws-status" :data-status="result.status">{{ result.status }}</span>
            <div class="min-w-0 break-words"><strong>{{ result.resource.name || result.resource.id }}</strong><p class="text-xs text-zinc-500">{{ result.resource.type }} · {{ result.resource.id }}</p><p class="mt-1">{{ result.message }}</p></div>
          </li>
          <li v-if="!results.length" class="text-zinc-500">{{ cleanup.error || 'Refresh inventory to check remaining resources.' }}</li>
        </ul>
      </details>
    </section>

    <p v-if="!items.length" class="rounded-xl border border-zinc-200 p-4 text-sm text-zinc-500 dark:border-white/10">No matching AWS resources found for the recorded run prefixes or Owner tag.</p>
    <template v-else>
      <div class="flex flex-wrap items-center gap-2">
        <span v-for="badge in countBadges" :key="badge.type" class="rounded-md bg-zinc-100 px-2 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">{{ badge.type }}: {{ badge.count }}</span>
        <label class="ml-auto flex items-center gap-2 text-sm text-zinc-500"><input v-model="candidatesOnly" type="checkbox" /> Candidates only</label>
      </div>
      <div class="overflow-x-auto rounded-xl border border-zinc-200 dark:border-white/10">
        <table class="aws-table w-full border-collapse text-left">
          <thead class="bg-zinc-50 dark:bg-white/[0.04]">
            <tr>
              <th class="w-10"><input type="checkbox" aria-label="Select all cleanup candidates" :checked="allSelected" :indeterminate.prop="selected.length > 0 && !allSelected" :disabled="locked || !candidates.length" @change="selectAll($event.target.checked)" /></th>
              <th>Resource</th><th>Status / Run</th><th>Details</th><th>Cleanup</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-zinc-200 dark:divide-white/10">
            <tr v-for="item in visibleItems" :key="itemKey(item)">
              <td><input v-model="selected" type="checkbox" :value="itemKey(item)" :aria-label="`Select ${item.name || item.id}`" :disabled="locked || !item.cleanupEligible" :title="item.cleanupReason || 'Select cleanup candidate'" /></td>
              <td class="aws-resource"><div class="text-xs font-semibold text-zinc-500">{{ item.type }}</div><div class="mt-1 font-semibold">{{ item.name || item.id }}</div><div class="mt-1 text-xs text-zinc-500">{{ item.region }}</div></td>
              <td><div>{{ item.status || '—' }}</div><div class="mt-1 text-xs text-zinc-500">Run {{ item.runId || 'unknown' }}</div></td>
              <td class="aws-detail"><div>{{ item.details || item.id }}</div><div v-if="item.owner" class="mt-1 text-xs text-zinc-500">Owner {{ item.owner }}</div><details class="mt-2 text-xs text-zinc-500"><summary class="cursor-pointer">ID &amp; tags</summary><p class="mt-1">{{ item.id }}</p><p class="mt-1">{{ tagsFor(item) }}</p></details></td>
              <td class="aws-action"><template v-if="item.cleanupEligible"><span class="mb-2 block text-xs text-amber-700 dark:text-amber-300">Candidate</span><button class="aws-button aws-danger" :disabled="locked" @click="review([item])">Delete…</button></template><template v-else><span class="text-xs font-semibold text-zinc-500">Protected</span><p class="mt-1 text-xs text-zinc-500">{{ item.cleanupReason }}</p></template></td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>

  <Teleport to="body">
    <dialog ref="reviewDialog" class="aws-review" aria-labelledby="aws-review-title" @cancel.prevent="closeReview">
      <form v-if="plan" @submit.prevent="confirmCleanup">
        <header class="aws-review-header">
          <span class="text-xs font-semibold uppercase tracking-wide text-rose-600 dark:text-rose-300">Permanent AWS deletion</span>
          <h2 id="aws-review-title" class="mt-2 text-xl font-semibold">Review {{ plan.items.length }} resource{{ plan.items.length === 1 ? '' : 's' }}</h2>
          <AppBuildStamp />
          <p class="mt-2 text-sm">{{ plan.region }} · Owner {{ plan.owner }}. These resources are not recorded in this workspace. They may still be in use elsewhere.</p>
        </header>
        <div class="aws-review-content">
          <p class="aws-warning">Deletion cannot be undone. EC2 termination can permanently delete attached volumes. No backups or snapshots are created. Review every effect below.<AppBuildStamp /></p>
          <p v-if="plan.inventoryWarning" class="mt-3 text-sm text-amber-700 dark:text-amber-300">The inventory scan was incomplete. This review covers only the exact resources below; other resources may remain.</p>
          <ol class="mt-4 grid gap-3">
            <li v-for="item in plan.items" :key="itemKey(item.resource)" class="aws-review-item">
              <strong>{{ item.resource.type }} · {{ item.resource.name || item.resource.id }}</strong>
              <p class="mt-1 break-all text-xs text-zinc-500">{{ item.resource.id }}</p>
              <ul class="mt-2 list-disc space-y-1 pl-5 text-sm"><li v-for="effect in item.effects" :key="effect" class="break-words">{{ effect }}</li></ul>
            </li>
          </ol>
          <details v-if="plan.blocked.length" open class="mt-4 text-sm"><summary class="font-semibold">{{ plan.blocked.length }} protected or unavailable — will not be deleted</summary><ul class="mt-2 grid gap-2"><li v-for="item in plan.blocked" :key="itemKey(item.resource)"><strong>{{ item.resource.name || item.resource.id }}</strong>: {{ item.message }}</li></ul></details>
          <label v-if="plan.items.length" class="mt-5 block text-sm">Type <strong>{{ plan.confirmation }}</strong> to confirm this exact selection.<input v-model="confirmation" :disabled="!!busy || expired" autocomplete="off" spellcheck="false" aria-label="Deletion confirmation" class="aws-confirmation mt-2 block w-full" /></label>
          <p v-if="expired" class="mt-2 text-sm text-amber-700 dark:text-amber-300">This review expired. Cancel and review again.</p>
          <p v-if="error" class="aws-warning mt-3" role="alert">{{ error }}<AppBuildStamp /></p>
        </div>
        <footer class="aws-review-footer">
          <button type="button" class="aws-button" :disabled="!!busy" autofocus @click="closeReview">Cancel</button>
          <button type="submit" class="aws-button aws-delete-confirm" :disabled="!!busy || lifecycleRunning || expired || !plan.items.length || confirmation !== plan.confirmation">{{ busy === 'delete' ? 'Starting cleanup…' : `Delete ${plan.items.length} ${plan.items.length === 1 ? 'resource' : 'resources'}` }}</button>
        </footer>
      </form>
    </dialog>
  </Teleport>
</template>

<script setup>
import { computed, nextTick, onUnmounted, ref, watch } from 'vue';
import { apiFetch, bootPending, lifecycleRunning, refresh, state } from './store.js';
import { readJSON } from './read-json.mjs';
import AppBuildStamp from './AppBuildStamp.vue';

const selected = ref([]);
const candidatesOnly = ref(false);
const busy = ref('');
const error = ref('');
const plan = ref(null);
const confirmation = ref('');
const reviewDialog = ref(null);
const expired = ref(false);
let expiryTimer;
const inventory = computed(() => state.value?.aws || {});
const cleanup = computed(() => state.value?.awsCleanup || {});
const items = computed(() => inventory.value.items || []);
const candidates = computed(() => items.value.filter(item => item.cleanupEligible));
const visibleItems = computed(() => candidatesOnly.value ? candidates.value : items.value);
const selectedItems = computed(() => candidates.value.filter(item => selected.value.includes(itemKey(item))));
const locked = computed(() => !!busy.value || !!plan.value || bootPending.value || lifecycleRunning.value);
const allSelected = computed(() => candidates.value.length > 0 && selected.value.length === candidates.value.length);
const results = computed(() => cleanup.value.results || []);
const completedCount = computed(() => results.value.filter(item => ['deleted','failed','blocked'].includes(item.status)).length);
const updatedLabel = computed(() => inventory.value.updatedAt ? `Updated ${new Date(inventory.value.updatedAt).toLocaleTimeString()}` : '');
const summary = computed(() => state.value?.aws ? `${items.value.length} matching AWS resources in ${inventory.value.region || 'the configured region'}. ${inventory.value.owner ? `Owner ${inventory.value.owner}.` : 'Owner tag not configured.'}` : 'Loading AWS inventory…');
const countBadges = computed(() => {
  const counts = items.value.reduce((all,item) => { all[item.type] = (all[item.type] || 0) + 1; return all; }, {});
  return Object.entries(counts).sort(([a],[b]) => a.localeCompare(b)).map(([type,count]) => ({type,count}));
});
const itemKey = item => JSON.stringify([item.type,item.id]);
const tagsFor = item => Object.entries(item.tags || {}).map(([key,value]) => `${key}=${value}`).join(' · ');
const selectAll = checked => { selected.value = checked ? candidates.value.map(itemKey) : []; };
watch(candidates, items => { const keys = new Set(items.map(itemKey)); selected.value = selected.value.filter(key => keys.has(key)); });
const review = async resources => {
  if (locked.value || !resources.length) return;
  error.value = ''; busy.value = 'review';
  try {
    plan.value = await readJSON(signal => apiFetch('/api/aws/cleanup/preview', { method:'POST', signal, body:JSON.stringify({ resources:resources.map(({type,id}) => ({type,id})) }) }), {label:'AWS cleanup review',timeoutMs:125000});
    confirmation.value = ''; expired.value = false;
    clearTimeout(expiryTimer);
    expiryTimer = setTimeout(() => { expired.value = true; }, Math.max(0,new Date(plan.value.expiresAt).getTime() - Date.now()));
    await nextTick(); reviewDialog.value.showModal();
  } catch (err) { error.value = err.message; }
  finally { busy.value = ''; }
};
const closeReview = () => {
  if (busy.value) return;
  reviewDialog.value?.close(); plan.value = null; confirmation.value = ''; error.value = ''; clearTimeout(expiryTimer);
};
const confirmCleanup = async () => {
  if (busy.value || lifecycleRunning.value || expired.value || !plan.value?.items.length || confirmation.value !== plan.value.confirmation) return;
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

