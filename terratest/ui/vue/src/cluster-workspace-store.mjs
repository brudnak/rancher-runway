import { ref, watch, onMounted, onBeforeUnmount } from 'vue';
import { apiFetch, setActivePanelTab, activeTab } from './store.js';
import { clusterName } from './cluster-workspace.mjs';

export const clusterWorkspaces = ref([]);
export const clusterHistoryTrash = ref([]);
export const clusterWorkspaceError = ref('');
export const selectedClusterWorkspaceId = ref('');
export const testWorkspaceIntent = ref(null);
export const cacheLabIntent = ref(null);
// Retain the originating cluster while moving between its tests, packages, and cache.
export const clusterReturnContext = ref(null);
export function rememberClusterOrigin(cluster) {
  if (!cluster?.id) return;
  clusterReturnContext.value = { id: cluster.id, name: clusterName(cluster, cluster.id), runId: cluster.runId || '', tab: activeTab.value === 'history' ? 'history' : 'clusters' };
}
export function returnToCluster() {
  const origin = clusterReturnContext.value;
  selectedClusterWorkspaceId.value=origin?.id || '';
  setActivePanelTab(origin?.tab || 'clusters');
}
watch(activeTab, tab => {
  if (!['tests', 'packages', 'cache'].includes(tab)) clusterReturnContext.value = null;
});
let inFlight, pollTimer, consumers = 0;
const eventName = 'rancher-runway:lab-data-changed';
export async function refreshClusterWorkspaces({fresh=false}={}) {
  if (fresh && inFlight) await inFlight;
  if (inFlight) return inFlight;
  inFlight = (async()=>{
    const controller = new AbortController(), timer = setTimeout(()=>controller.abort(),20000);
    try {
      const response = await apiFetch('/api/cluster-workspaces',{signal:controller.signal});
      if (!response.ok) throw new Error(await response.text());
      const data = await response.json();
      clusterWorkspaces.value = data.clusters || [];
      clusterHistoryTrash.value = data.trash || [];
      clusterWorkspaceError.value = '';
    } catch (error) { clusterWorkspaceError.value = error.name==='AbortError' ? 'Cluster history took too long to load. Try refreshing.' : error.message; }
    finally { clearTimeout(timer); }
  })().finally(()=>{inFlight=null;});
  return inFlight;
}
export function notifyClusterDataChanged() { window.dispatchEvent(new CustomEvent(eventName)); }
export function listenClusterDataChanged(listener) {
  window.addEventListener(eventName,listener);
  return ()=>window.removeEventListener(eventName,listener);
}
const refreshAfterMutation = () => refreshClusterWorkspaces({fresh:true});
function poll() {
  clearTimeout(pollTimer);
  if (!document.hidden && ['clusters','history','tests','cache','destroy','packages'].includes(activeTab.value)) refreshClusterWorkspaces();
  if (consumers) pollTimer=setTimeout(poll,10000);
}
export function useClusterWorkspaces() {
  onMounted(()=>{if(consumers++===0){window.addEventListener(eventName,refreshAfterMutation);document.addEventListener('visibilitychange',poll);poll();}refreshClusterWorkspaces();});
  onBeforeUnmount(()=>{if(--consumers===0){clearTimeout(pollTimer);window.removeEventListener(eventName,refreshAfterMutation);document.removeEventListener('visibilitychange',poll);}});
  return {clusterWorkspaces, clusterWorkspaceError};
}
export function clusterDisplayName(id,fallback='Unlinked target') { return clusterName(clusterWorkspaces.value.find(cluster=>cluster.id===id),fallback); }
export async function renameCluster(id,nickname) {
  const response = await apiFetch('/api/cluster-workspaces',{method:'POST',body:JSON.stringify({action:'rename',id,nickname})});
  if(!response.ok)throw new Error(await response.text());
  await refreshClusterWorkspaces({fresh:true});notifyClusterDataChanged();
}
export function openClusterWorkspace(id) { selectedClusterWorkspaceId.value=id;setActivePanelTab(clusterWorkspaces.value.find(item=>item.id===id)?.archived ? 'history' : 'clusters'); }
export function openClusterTestRun(id) { testWorkspaceIntent.value={runId:id};setActivePanelTab('tests'); }
export function openClusterTestPlan(id) { testWorkspaceIntent.value={planId:id};setActivePanelTab('tests'); }
export function prepareClusterTest(id) { testWorkspaceIntent.value={clusterId:id};setActivePanelTab('tests'); }
export function prepareClusterCache(id) { cacheLabIntent.value={clusterId:id};setActivePanelTab('cache'); }
export function openClusterCache(workspaceId,snapshotId='') { cacheLabIntent.value={workspaceId,snapshotId};setActivePanelTab('cache'); }
