// A deadline includes body parsing; cancellation also releases a stalled body read.
export async function readImageJSON(request, controller, label, timeoutMs) {
  if (controller.signal.aborted) throw new DOMException('Cancelled', 'AbortError');
  let timer, onAbort;
  const aborted = new Promise((_, reject) => {
    onAbort = () => reject(new DOMException('Cancelled', 'AbortError'));
    controller.signal.addEventListener('abort', onAbort, { once: true });
  });
  const deadline = new Promise((_, reject) => {
    timer = setTimeout(() => {
      reject(new Error(`${label} did not respond within ${Math.ceil(timeoutMs / 1000)} seconds. Try again.`));
      controller.abort();
    }, timeoutMs);
  });
  try { return await Promise.race([(async () => (await request(controller.signal)).json())(), aborted, deadline]); }
  finally { clearTimeout(timer); controller.signal.removeEventListener('abort', onAbort); }
}

export function digestReference(reference, digest) {
  if (!/^sha256:[a-f0-9]{64}$/i.test(digest || '')) return '';
  const input = String(reference || '').trim();
  if (!input || /[\s\x00-\x1f]/.test(input) || input.includes('://')) return '';
  const base = input.split('@')[0];
  const colon = base.lastIndexOf(':');
  const repository = colon > base.lastIndexOf('/') ? base.slice(0, colon) : base;
  return repository.includes('/') ? `${repository}@${digest}` : '';
}

export const imageEvidenceState = image => {
  if (!image || image.found === false) return image?.error ? 'error' : 'unavailable';
  if (image.error) return 'error';
  const normalize = value => String(value || '').trim().toLowerCase().replace(/[\s_-]+/g, '-');
  const verdict = normalize(image.match?.verdict), relation = normalize(image.match?.relation);
  if (['exact', 'equal', 'same'].includes(relation) || ['exact', 'exact-revision', 'exact-match', 'exact-commit'].includes(verdict)) return 'exact';
  if (['descendant', 'included', 'contains', 'present', 'verified', 'candidate-is-descendant', 'required-is-ancestor'].includes(relation)
    || ['descendant', 'included', 'included-descendant', 'contains', 'contains-commit', 'commit-included', 'present', 'verified', 'match'].includes(verdict)) return 'descendant';
  if (['not-included', 'not-present', 'absent', 'diverged', 'unrelated', 'does-not-contain'].includes(verdict)
    || ['not-included', 'not-present', 'ancestor', 'candidate-is-ancestor', 'required-is-descendant', 'diverged', 'unrelated'].includes(relation)) return 'not-included';
  return 'unknown';
};

export const evidenceStateLabel = state => ({ exact: 'Exact revision', descendant: 'Included (descendant)', included: 'Included', 'not-included': 'Not included', unknown: 'Unknown', unavailable: 'Image not found', error: 'Check failed' }[state] || 'Unknown');

const md = value => String(value ?? '').replace(/\s+/g, ' ').trim().replace(/[\\`*_{}\[\]<>()#!|]/g, '\\$&');
export function buildPRImageBrief(result) {
  const pr = result.pullRequest || {};
  const lines = ['# PR Image Check · Evidence summary', '', `Pull request: ${md(pr.url || pr.htmlUrl || '')}`, `Title: ${md(pr.title)}`, `Target tag: ${md(result.tag)}`, `Observed: ${md(result.checkedAt)}`, `Platform: ${md(result.platform || 'linux/amd64')}`, `Verification commit: ${md(pr.inclusionCommitSha || pr.requiredRevision || pr.commitSha || '')}`, `Verification basis: ${md(pr.inclusionBasis || '')}`, `Scan complete: ${result.summary?.scanComplete === true ? 'Yes' : 'Not confirmed'}`, '', '## Registry observations', '', '| Registry | Image | Ancestry result | Digest-pinned reference | Compared revision |', '| --- | --- | --- | --- | --- |'];
  for (const registry of result.registries || []) for (const role of ['server', 'agent']) {
    const image = registry[role];
    lines.push(`| ${[registry.registry || registry.label, role, evidenceStateLabel(imageEvidenceState(image)), digestReference(image?.reference, image?.digest) || 'Unavailable', image?.match?.candidateRevision || image?.ossRevision || image?.revision || 'Unknown'].map(md).join(' | ')} |`);
  }
  lines.push('', '## Interpretation', '', 'Server evidence determines the registry verdict. An available server/agent pair does not establish matching ancestry for both images. Labels are producer-declared; ancestry is not a binary attestation. Reverts and equivalent cherry-picks require separate verification. Mutable tags may have moved since this observation.');
  for (const warning of result.warnings || []) lines.push(`- ${md(warning)}`);
  for (const registry of result.registries || []) for (const role of ['server', 'agent']) {
    const reason = registry[role]?.error || registry[role]?.match?.reason;
    if (reason) lines.push(`- ${md(registry.registry || registry.label)} / ${role}: ${md(reason)}`);
  }
  return lines.join('\n') + '\n';
}
