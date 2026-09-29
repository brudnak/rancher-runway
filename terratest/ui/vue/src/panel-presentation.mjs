// A background scan updates its indicator, not the last completed result.
// This also keeps the shared header's text and actions steady while polling.
export function initialDiscovery(discovery = {}) {
  return Boolean(discovery.refreshing && !discovery.updatedAt && !discovery.items?.length);
}

export function discoveryValue(kind, discovery = {}) {
  const items = Array.isArray(discovery.items) ? discovery.items : [];
  if (kind === 'clusters') {
    return items.length
      ? `${items.filter(item => item.reachable).length}/${items.length} reachable`
      : initialDiscovery(discovery) ? 'Checking…' : 'None yet';
  }
  return items.length
    ? `${items.length} resources`
    : initialDiscovery(discovery) ? 'Scanning…' : 'No resources shown';
}

export function inventoryExposure(inventory = {}, hasRuns = false) {
  const count = inventory.items?.length || 0;
  if (count) return {
    key: 'exposure', tone: 'amber', eyebrow: 'AWS exposure',
    title: `${count} resource${count === 1 ? '' : 's'} visible`,
    detail: 'Review leftover resources for individual or bulk cleanup in AWS Inventory. Recorded runs use the Destroy tab. Every deletion requires typed confirmation.',
    meta: 'Live', action: 'aws', actionLabel: 'Open inventory',
  };
  const firstScan = initialDiscovery(inventory);
  return {
    key: 'exposure', tone: hasRuns ? 'emerald' : 'zinc', eyebrow: 'AWS exposure',
    title: firstScan ? 'Checking AWS resources' : 'No resources shown',
    detail: firstScan
      ? 'The first AWS inventory scan is running. Resources have not been ruled out.'
      : hasRuns
        ? 'Recorded slots are available; the last completed AWS scan has no matching visible resources.'
        : 'No AWS resources are expected before an approved setup run.',
    meta: firstScan ? 'Checking' : 'Quiet',
    action: firstScan ? 'aws' : hasRuns ? 'destroy' : 'setup',
    actionLabel: firstScan ? 'Open inventory' : hasRuns ? 'Open destroy' : 'Open setup',
  };
}
