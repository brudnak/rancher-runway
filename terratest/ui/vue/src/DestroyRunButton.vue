<template>
  <button
    v-if="run"
    type="button"
    :disabled="disabled"
    :title="title"
    :aria-label="`Destroy entire run ${run.runId}`"
    class="inline-flex min-h-10 items-center justify-center gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm font-semibold text-rose-700 hover:bg-rose-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200 dark:hover:bg-rose-500/20"
    @click="destroy"
  >
    <Icon name="trash" />
    Destroy run
  </button>
</template>

<script setup>
import { computed, ref } from 'vue';
import Icon from './HelmLabIcon.vue';
import { state, bootPending, cleanupStarting, runDestroyBlocked, runCleanup } from './store.js';

const props = defineProps({ runId: String });
const reviewing = ref(false);
// An unrecorded/external cluster must never fall back to another selected run.
const run = computed(() => {
  const runId = String(props.runId || '').trim();
  return runId ? (state.value?.workspace?.runs || []).find(item => String(item.runId || '').trim() === runId) : null;
});
const disabled = computed(() => !run.value || bootPending.value || reviewing.value || cleanupStarting.value || runDestroyBlocked(run.value));
const title = computed(() => {
  if (bootPending.value) return 'Wait for recorded runs to finish loading.';
  if (reviewing.value || cleanupStarting.value) return 'A destroy request is being reviewed or started.';
  if (runDestroyBlocked(run.value)) return 'Wait for the current lifecycle operation to finish before destroying this run.';
  return `Review destruction of entire run ${run.value?.runId}: all its management clusters and tracked downstreams. Test history and cache data are kept.`;
});

async function destroy() {
  if (disabled.value) return;
  const runId = run.value.runId;
  reviewing.value = true;
  try {
    await runCleanup(runId, { preserveLabData: true });
  } finally {
    reviewing.value = false;
  }
}
</script>
