export function upgradeVersionParts(value) {
  const text = String(value || '');
  const release = text.match(/^v?(\d+)\.(\d+)\.(\d+)(.*)$/);
  if (release) return release;
  const head = text.match(/^v?(\d+)\.(\d+)(-[a-f0-9]{7,40}-head)$/i);
  return head ? [head[0], head[1], head[2], undefined, head[3]] : null;
}
export function upgradeCandidate(current, target, experimental = false) {
  const a = upgradeVersionParts(current), b = upgradeVersionParts(target);
  if (!a || !b) return false;
  if (+a[1] !== +b[1] || +b[2] < +a[2] || +b[2] > +a[2] + 1) return false;
  if (a[3] === undefined || b[3] === undefined) return experimental && current !== target;
  if (+b[2] === +a[2] && +b[3] < +a[3]) return false;
  return experimental || (+b[2] > +a[2] || +b[3] > +a[3] || Boolean(a[4] && !b[4]));
}

export function machineValues(fields, draft) {
  const result = {};
  for (const [key, field] of Object.entries(fields || {})) {
    const value = draft[key];
    if (value === '' || value === undefined || value === null) continue;
    if (field.type === 'boolean') result[key] = value === true || value === 'true';
    else if (['int', 'integer', 'float', 'number'].includes(field.type)) {
      result[key] = Number(value);
      if (!Number.isFinite(result[key])) throw new Error(`${key} must be a number.`);
    } else if (field.type === 'string' || field.type === 'password') result[key] = String(value);
    else {
      try { result[key] = JSON.parse(value); }
      catch { throw new Error(`${key} must contain valid JSON.`); }
    }
  }
  return result;
}

export function operationEvidence(record) {
  const plan = record.plan;
  const lines = [
    `# Rancher Runway ${record.kind} evidence`, '',
    `- Operation: ${record.id}`, `- Rancher: ${record.clusterId}`,
    `- Status: ${record.status}`, `- Started: ${record.started}`,
    `- Finished: ${record.finished || 'Not recorded'}`,
    `- From: ${record.from || 'N/A'}`, `- To: ${record.to || 'N/A'}`,
  ];
  if (plan) lines.push(`- Chart: ${plan.chart.version}`, `- Repository: ${plan.repository}`,
    `- Server image: ${plan.image}:${plan.imageTag}`, `- Server digest: ${plan.digest}`,
    `- Agent image: ${plan.agentImage}`, `- Agent digest: ${plan.agentDigest}`,
    `- Experimental: ${plan.experimental ? 'yes' : 'no'}`, '', '## Preflight checks', '',
    ...plan.checks.map(check => `- ${check}`), '', '## Upgrade notes', '', ...plan.warnings.map(warning => `- ${warning}`));
  if (plan?.targetVersionSource) lines.push('', '## Image provenance', '',
    `- Version evidence: ${plan.targetVersionSource}`, `- OCI version label: ${plan.targetVersionLabel || 'Not declared'}`,
    `- Source commit: ${plan.imageRevision || 'Not declared'}`, `- OSS commit: ${plan.imageOSSRevision || 'Not declared'}`,
    `- Canonical image: ${plan.imageCanonicalReference || 'Not declared'}`);
  if (record.resources?.length) lines.push('', '## Created resources', '', ...record.resources.map(resource => `- ${resource}`));
  lines.push('', '## Timeline', '', ...record.events.map(event => `${event.at}  ${event.message}`));
  return lines.join('\n') + '\n';
}
