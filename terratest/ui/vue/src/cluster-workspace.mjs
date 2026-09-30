import { rancherConnectionURL } from './rancher-connection.mjs';

export const clusterName = (cluster, fallback='Unlinked target') => cluster?.nickname || cluster?.name || fallback;
export function matchingCluster(clusters, host) {
  const url = rancherConnectionURL(host);
  if (!url) return null;
  const matches = clusters.filter(cluster => rancherConnectionURL(cluster.url) === url);
  return matches.length === 1 ? matches[0] : null;
}
export function groupClusterRecords(records, clusters) {
  const groups = new Map();
  for (const record of records) {
    const id = record.clusterId || '';
    if (!groups.has(id)) groups.set(id, {id, name:clusterName(clusters.find(cluster=>cluster.id===id), id ? 'Cluster history' : 'Unlinked targets'), records:[]});
    groups.get(id).records.push(record);
  }
  return [...groups.values()].sort((a,b)=>a.id ? b.id ? a.name.localeCompare(b.name) : -1 : 1);
}
