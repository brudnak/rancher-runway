export function historyGroups(clusters, packages = [], buckets = [], query = '') {
  const groups = new Map();
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  for (const cluster of clusters) {
    const linked = packages.filter(pkg => pkg.sessions?.some(session => session.environment?.clusterId === cluster.id));
    const inferred = buckets.filter(bucket => linked.some(pkg => bucket.packageIds?.includes(pkg.id))).map(bucket => bucket.name).sort();
    const milestone = cluster.milestone || [...new Set(inferred)].join(' / ');
    const version = cluster.version || 'Unknown version';
    const label = milestone ? `Milestone · ${milestone}` : `Rancher · ${version}`;
    const text = [cluster.name, cluster.nickname, cluster.id, cluster.runId, version, milestone, cluster.notes, cluster.url].join(' ').toLowerCase();
    if (!words.every(word => text.includes(word))) continue;
    if (!groups.has(label)) groups.set(label, { label, milestone: Boolean(milestone), clusters: [] });
    groups.get(label).clusters.push(cluster);
  }
  return [...groups.values()].sort((a,b) => Number(b.milestone)-Number(a.milestone) || b.label.localeCompare(a.label, undefined, {numeric:true})).map(group => ({...group, clusters:group.clusters.sort((a,b)=>(a.nickname||a.name||a.id).localeCompare(b.nickname||b.name||b.id,undefined,{numeric:true}))}));
}
