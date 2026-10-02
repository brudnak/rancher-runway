// Assemble saved facts without inferring an installed version from a requested tag.
const ordered = (items, field) => [...(items || [])].sort((a, b) => String(a[field] || '').localeCompare(String(b[field] || '')) || String(a.id || '').localeCompare(String(b.id || '')));
export function clusterTimeline(archive) {
  if (!archive) return [];
  const events = ordered(archive.events, 'at');
  const operations = ordered(archive.operations, 'started');
  const discovery = events.find(event => event.kind === 'discovery');
  const installs = events.filter(event => event.kind === 'helm-install');
  const initial = discovery?.data || {};
  const baseline = {
    id: 'initial', title: 'Starting configuration', status: 'recorded', at: discovery?.at || installs[0]?.at,
    version: initial.version || archive.cluster?.version || '',
    note: 'Original requested version. Capture time is not necessarily the installation time.',
    commands: installs.slice(0, 1), metadata: initial, observations: events.filter(event => event.kind === 'deployment' && (!operations[0] || event.at < operations[0].started)),
  };
  return [baseline, ...operations.map((operation, index) => {
    const upgrade = operation.kind === 'upgrade';
    return {
      id: operation.id, at: operation.started, status: operation.status,
      title: upgrade ? (operation.status === 'succeeded' ? 'Upgraded Rancher' : 'Upgrade attempt') : 'Downstream creation',
      from: operation.from, version: operation.to, operation,
      note: operation.status === 'succeeded' ? 'Readiness checks passed.' : operation.status === 'running' ? 'In progress. The target is not yet confirmed installed.' : 'Outcome does not establish the installed version. Inspect the saved observations before retrying.',
      commands: events.filter(event => event.kind === 'helm-upgrade-command' && event.data?.operationId === operation.id),
      metadata: operation.plan || {resources: operation.resources || []},
      observations: events.filter(event => event.kind === 'deployment' && event.at >= operation.started && (!operations[index + 1] || event.at < operations[index + 1].started)),
    };
  })];
}
export function clusterHistoryMarkdown(archive) {
  const lines = ['# Cluster history', '', `Cluster: ${archive.cluster?.nickname || archive.cluster?.name || archive.cluster?.id || 'Unknown'}`, '', 'Commands are retained with secrets and local paths redacted. Missing facts were not recorded.', ''];
  for (const step of clusterTimeline(archive)) {
    lines.push(`## ${step.title}: ${step.from ? `${step.from} → ` : ''}${step.version || 'Version not recorded'}`, '', `Status: ${step.status} · Captured: ${step.at || 'Not recorded'}`, '', step.note, '');
    for (const command of step.commands) lines.push('```sh', command.data.command, '```', command.data.source || '', '');
    if (!step.commands.length) lines.push('Helm command not recorded.', '');
    for (const observation of step.observations) lines.push(`Observed at ${observation.at}`, '```json', JSON.stringify(observation.data, null, 2), '```', '');
    lines.push('```json', JSON.stringify(step.operation || step.metadata, null, 2), '```', '');
  }
  lines.push('## Timestamped observations', '', '```json', JSON.stringify(archive.events || [], null, 2), '```', '');
  return lines.join('\n');
}
