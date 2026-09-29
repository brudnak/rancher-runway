// Shared presentation and validation for the local labs. No cluster side effects.
export const liveSteveRun = run => Boolean(run.stevePid) || ['running', 'starting', 'serving'].includes(run.status);
export const visibleRecords = records => (Array.isArray(records) ? records : []).filter(item => !['deleted', 'cleaned'].includes(item.status));
export function sessionStatus(record, kind) {
  const status = record.status;
  if (status === 'failed' || record.error) return { label: 'Needs attention', tone: 'error', group: 'attention' };
  if (status === 'creating' || status === 'starting' || (kind === 'steve' && status === 'running')) return { label: 'Starting', tone: 'working', group: 'running' };
  if (status === 'serving' || status === 'running') return { label: kind === 'steve' ? 'Serving' : 'Running', tone: 'success', group: 'running' };
  if (status === 'stopped') return { label: 'Stopped', tone: 'muted', group: 'stopped' };
  return { label: status ? status.charAt(0).toUpperCase() + status.slice(1) : 'Unknown', tone: 'muted', group: 'attention' };
}
export function filterSessions(records, { search = '', filter = 'all', kind = 'k3d' } = {}) {
  const words = search.toLowerCase().trim().split(/\s+/).filter(Boolean);
  return visibleRecords(records).filter(record => {
    if (filter !== 'all' && sessionStatus(record, kind).group !== filter) return false;
    const haystack = [record.runId, record.clusterName, record.steveRef, record.steveCommit, record.k3sVersion, record.apiUrl, record.httpsUrl, record.httpUrl, sessionStatus(record, kind).label].filter(Boolean).join(' ').toLowerCase();
    return words.every(word => haystack.includes(word));
  }).sort((a,b) => (Date.parse(b.createdAt) || 0) - (Date.parse(a.createdAt) || 0) || String(a.runId).localeCompare(String(b.runId)));
}
export function portError(value, records = [], { kind = 'k3d', replacing = false } = {}) {
  if (value === '' || value === null || value === undefined) return '';
  if (!/^\d+$/.test(String(value)) || !Number.isSafeInteger(Number(value)) || Number(value) < 1024 || Number(value) > 65535) return 'Use a whole port number from 1024 to 65535, or choose Automatic.';
  const conflict = visibleRecords(records).find(record => {
    if (replacing && kind === 'steve' && liveSteveRun(record)) return false;
    const reserved = kind === 'k3d' ? ['running','creating','stopped'].includes(record.status) : liveSteveRun(record);
    return reserved && [record.apiPort,record.httpPort,record.httpsPort].some(port => Number(port) === Number(value));
  });
  return conflict ? `Port ${value} belongs to ${conflict.runId}. Choose another port or Automatic.` : '';
}
export function imageTagError(value) {
  const tag = String(value || '').trim().replace(/^rancher\/k3s:/, '');
  return /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/.test(tag) ? '' : 'Enter a K3s image tag, such as v1.33.5-k3s1.';
}
export const k3sMinor = value => normalizeImageTag(value).match(/^v?(\d+\.\d+)\./)?.[1] || '';
export const normalizeImageTag = value => String(value || '').trim().replace(/^rancher\/k3s:/, '');
export const refError = value => /^[A-Za-z0-9._/@+-]+$/.test(String(value || '').trim()) ? '' : 'Choose a tag, branch, or commit SHA without spaces.';
export function parseEnvironment(text) {
  const seen = new Set();
  return String(text || '').split(/\r?\n/).map((line,index) => ({line:line.trim(),index})).filter(item => item.line).map(({line,index}) => {
    const separator = line.indexOf('='), key = line.slice(0, separator);
    if (separator < 1 || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) throw new Error(`Line ${index + 1}: use NAME=value with a valid variable name.`);
    if (line.includes('\0')) throw new Error(`Line ${index + 1}: remove the null character.`);
    if (seen.has(key)) throw new Error(`${key} appears twice. Keep one value for each variable.`);
    seen.add(key); return line;
  });
}
// Arguments are one per line. Spaces inside a value stay intact; no shell parsing.
export function parseArguments(text) {
  return String(text || '').split(/\r?\n/).map(line => line.trim()).filter(Boolean).map((line, index) => {
    if (/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/.test(line)) throw new Error(`Argument ${index + 1}: remove control characters.`);
    return line;
  });
}
export function steveDraftErrors(draft, records = []) {
  const errors = {};
  if (refError(draft.steveRef)) errors.steveRef = refError(draft.steveRef);
  if (imageTagError(draft.k3sVersion)) errors.k3sVersion = imageTagError(draft.k3sVersion);
  if (portError(draft.httpsPort, records, { kind: 'steve', replacing: true })) errors.httpsPort = portError(draft.httpsPort, records, { kind: 'steve', replacing: true });
  if (draft.enableMetrics && (!/^\d+$/.test(String(draft.metricsInterval)) || !Number.isSafeInteger(Number(draft.metricsInterval)) || Number(draft.metricsInterval) < 1)) errors.metricsInterval = 'Use a whole number of seconds, at least 1.';
  try { parseEnvironment(draft.extraEnv); } catch (error) { errors.extraEnv = error.message; }
  try { parseArguments(draft.extraArgs); } catch (error) { errors.extraArgs = error.message; }
  return errors;
}
export function steveStartPayload(draft, replace = false) {
  return {steveRef:draft.steveRef.trim(),k3sVersion:normalizeImageTag(draft.k3sVersion),keepCluster:true,httpsPort:Number(draft.httpsPort || 0),headerAuth:true,enableMetrics:!!draft.enableMetrics,metricsUpdateIntervalSeconds:draft.enableMetrics ? Number(draft.metricsInterval) : 15,extraEnv:parseEnvironment(draft.extraEnv),extraArgs:parseArguments(draft.extraArgs),replace};
}
export function reuseSteveDraft(record) {
  return {steveRef:record.steveRef || '',k3sVersion:record.k3sVersion || '',httpsPort:'',enableMetrics:!!record.enableMetrics,metricsInterval:record.metricsUpdateIntervalSeconds || 15,extraEnv:(record.extraEnv || []).join('\n'),extraArgs:(record.extraArgs || []).join('\n')};
}
export const shellQuote = value => `'${String(value).replace(/'/g, `'\\''`)}'`;
export const kubectlCommand = record => record.kubeconfig ? `kubectl --kubeconfig ${shellQuote(record.kubeconfig)} get nodes` : '';
export const endpointURL = record => record.httpsUrl || record.httpUrl || record.apiUrl || '';
export function timeLabel(value) {
  const date = new Date(value);
  return value && Number.isFinite(date.getTime()) && date.getFullYear() > 1970 ? date.toLocaleString(undefined, {month:'short',day:'numeric',hour:'numeric',minute:'2-digit'}) : 'Not recorded';
}
export function durationLabel(start, end = Date.now()) {
  const first = Date.parse(start), last = typeof end === 'number' ? end : Date.parse(end);
  if (!Number.isFinite(first) || first < 0 || !Number.isFinite(last)) return '';
  const seconds = Math.max(0, Math.floor((last - first) / 1000));
  return seconds < 60 ? `${seconds}s` : seconds < 3600 ? `${Math.floor(seconds / 60)}m ${seconds % 60}s` : `${Math.floor(seconds / 3600)}h ${Math.floor(seconds % 3600 / 60)}m`;
}
export function logLines(text, search = '', issuesOnly = false) {
  const query = search.toLocaleLowerCase();
  return String(text || '').replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, '').split(/\r?\n/).map((text,index) => ({text,number:index + 1,tone:/\b(error|failed|fatal|panic)\b/i.test(text)?'error':/\b(warn|warning)\b/i.test(text)?'warning':'plain'})).filter(line => (!query || line.text.toLocaleLowerCase().includes(query)) && (!issuesOnly || line.tone !== 'plain'));
}
export function highlightMatch(text, search) {
  if (!search) return [{text,match:false}];
  const result = [], lower = text.toLocaleLowerCase(), needle = search.toLocaleLowerCase();
  let cursor = 0, index;
  while ((index = lower.indexOf(needle, cursor)) !== -1) { if (index > cursor) result.push({text:text.slice(cursor,index),match:false}); result.push({text:text.slice(index,index + search.length),match:true}); cursor = index + search.length; }
  if (cursor < text.length) result.push({text:text.slice(cursor),match:false});
  return result;
}
export function createSingleFlight(task) {
  let pending;
  return (...args) => {
    if (!pending) pending = Promise.resolve().then(() => task(...args)).finally(() => { pending = undefined; });
    return pending;
  };
}
// A slower response must never replace the result for a newer selection.
export function createLatestTask() {
  let generation = 0, controller;
  return {
    cancel() { generation++; controller?.abort(); },
    async run(task, commit, fail = () => {}) {
      const current = ++generation; controller?.abort(); controller = new AbortController();
      try { const result = await task(controller.signal); if (current === generation) commit(result); }
      catch (error) { if (current === generation && !controller.signal.aborted) fail(error); }
      return current === generation;
    },
  };
}
