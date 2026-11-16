<template>
  <button
    v-if="eligible"
    type="button"
    :disabled="disabled"
    :title="reason || `Review destruction of ${name}`"
    :aria-label="`Destroy cluster ${name}`"
    class="inline-flex min-h-10 items-center justify-center gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm font-semibold text-rose-700 hover:bg-rose-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200 dark:hover:bg-rose-500/20"
    @click="destroy"
  >
    <Icon name="trash" />
    Destroy cluster
  </button>
</template>

<script setup>
import { computed, ref } from 'vue';
import Icon from './HelmLabIcon.vue';
import { clusterDisplayName } from './cluster-workspace-store.mjs';
import { CONFIRMATION_TEXT } from './confirmation.mjs';
import { state, bootPending, cleanupStarting, runDestroyBlocked, requestTypedConfirmation, apiFetch, openCleanupLogs, refresh, showPanelNotice } from './store.js';

const props = defineProps({ cluster: { type: Object, required: true } });
const reviewing = ref(false);
const run = computed(() => state.value?.workspace?.runs?.find(item => item.runId === props.cluster.runId));
const eligible = computed(() => {
  const { id, runId, type, haIndex } = props.cluster;
  return run.value && type === 'local' && runId && (!run.value.deploymentType || run.value.deploymentType === 'ha-rke2') &&
    Number.isInteger(haIndex) && haIndex > 0 && id === `run-${runId}-ha-${haIndex}-local`;
});
const name = computed(() => clusterDisplayName(props.cluster.id, props.cluster.name));
const reason = computed(() => {
  if (bootPending.value || cleanupStarting.value || runDestroyBlocked(run.value)) return 'Wait for the current lifecycle operation to finish.';
  const items = state.value?.clusters?.items || [];
  if (!items.some(item => item.id === props.cluster.id)) return 'Refresh the cluster list to review this target.';
  const children = items.filter(item => item.type === 'downstream' && item.runId === props.cluster.runId && item.haIndex === props.cluster.haIndex);
  if (children.length) return `Remove or detach the downstream clusters first: ${children.map(item => item.name).join(', ')}.`;
  return '';
});
const disabled = computed(() => !eligible.value || reviewing.value || Boolean(reason.value));

async function destroy() {
  if (disabled.value) return;
  const { id, name: recordedName, rancherUrl } = props.cluster;
  const displayName = name.value;
  reviewing.value = true;
  try {
    const confirmed = await requestTypedConfirmation({
      title: `Destroy ${displayName}?`,
      body: `This permanently removes the RKE2 management cluster ${recordedName} and its AWS resources.${rancherUrl ? ' Its Rancher URL is ' + rancherUrl + '.' : ''} Cleanup stops if this Rancher has downstream clusters.`,
      confirmText: 'Destroy cluster',
      accentText: 'Single cluster',
    });
    if (!confirmed) return;
    if (!eligible.value || props.cluster.id !== id || props.cluster.rancherUrl !== rancherUrl || name.value !== displayName || reason.value) {
      showPanelNotice('Cluster destroy canceled', 'The selection or cluster state changed. Review the cluster again.');
      return;
    }
    cleanupStarting.value = true;
    try {
      const response = await apiFetch('/api/clusters/destroy', {
        method: 'POST',
        body: JSON.stringify({ clusterId: id, confirm: CONFIRMATION_TEXT }),
      });
      state.value = { ...state.value, cleanup: await response.json() };
      openCleanupLogs();
      refresh();
    } finally {
      cleanupStarting.value = false;
    }
  } catch (error) {
    showPanelNotice('Cluster destroy failed', error.message);
  } finally {
    reviewing.value = false;
  }
}
</script>
