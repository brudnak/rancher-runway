import { upgradeCandidate, upgradeVersionParts as versionParts } from './rancher-operations.mjs';

const compare = (a, b) => String(b).localeCompare(String(a), undefined, { numeric: true });
export const imageTag = image => String(image || '').split(':').at(-1);

// Release ordering mirrors the server's one-minor-at-a-time policy. Keep all
// candidates visible, but explain the prerequisite instead of offering a dead end.
export function releaseTargets(current, versions, experimental = false) {
  const before = versionParts(current);
  if (!before) return [];
  experimental ||= before[3] === undefined;
  const candidates = versions.filter(item => upgradeCandidate(current, item.appVersion || item.version, experimental))
    .sort((a, b) => compare(a.appVersion || a.version, b.appVersion || b.version));
  const patches = candidates.filter(item => versionParts(item.appVersion || item.version)?.[2] === before[2]);
  return candidates.map(item => {
    const target = item.appVersion || item.version;
    const nextMinor = versionParts(target)?.[2] !== before[2];
    const latestNext = candidates.find(candidate => versionParts(candidate.appVersion || candidate.version)?.[2] !== before[2]);
    const blocked = !experimental && nextMinor
      ? patches.length ? `Install ${patches[0].appVersion || patches[0].version} first` : item !== latestNext ? `Use ${latestNext.appVersion || latestNext.version} for this minor` : ''
      : '';
    return { ...item, target, blocked, recommended: !experimental && item === (patches[0] || latestNext) };
  }).sort((a, b) => Number(b.recommended) - Number(a.recommended) || Number(Boolean(a.blocked)) - Number(Boolean(b.blocked)));
}

// Show moving aliases on the current and next minor, not thousands of build
// hashes. An explicit search still exposes the complete published head catalog.
export function headTargets(current, images, query = '') {
  const before = versionParts(current);
  const needle = query.trim().toLowerCase();
  return [...new Set(images)].filter(image => {
    if (needle) return image.toLowerCase().includes(needle);
    const tag = imageTag(image);
    if (tag === 'head') return true;
    const match = tag.match(/^v?(\d+)\.(\d+)(?:\.(\d+))?-head$/);
    return before && match && +match[1] === +before[1] && +match[2] >= +before[2] && +match[2] <= +before[2] + 1 &&
      (+match[2] > +before[2] || match[3] === undefined || before[3] === undefined || +match[3] >= +before[3]);
  }).sort((a, b) => Number(imageTag(b) === 'head') - Number(imageTag(a) === 'head') || compare(imageTag(a), imageTag(b)));
}

// Use the installed release line as a starting chart for custom images. This is
// a proposal, not a compatibility assertion: the review and Helm preflight remain authoritative.
export function defaultHeadChart(current, versions) {
  const before = versionParts(current);
  if (!before) return '';
  const matching = versions.filter(item => {
    const target = versionParts(item.appVersion || item.version);
    return target && target[1] === before[1] && target[2] === before[2];
  }).sort((a, b) => compare(a.version, b.version));
  return matching.find(item => (item.appVersion || item.version).replace(/^v/, '') === current.replace(/^v/, ''))?.version || matching[0]?.version || '';
}

export function upgradeSearchHints(current) {
  const parts = versionParts(current);
  if (!parts) return [];
  return [
    { label: `${+parts[1]}.${+parts[2]}.x · current minor`, query: `${+parts[1]}.${+parts[2]}` },
    { label: `${+parts[1]}.${+parts[2] + 1}.x · next minor`, query: `${+parts[1]}.${+parts[2] + 1}` },
  ];
}
