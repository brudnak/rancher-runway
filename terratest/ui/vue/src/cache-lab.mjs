export const cacheBytes = value => {
  const n = Number(value) || 0;
  if (n < 1024) return `${n} B`;
  const units = ['KiB', 'MiB', 'GiB']; const i = Math.min(2, Math.floor(Math.log(n) / Math.log(1024)) - 1);
  return `${(n / 1024 ** (i + 1)).toFixed(i ? 1 : 0)} ${units[i]}`;
};
export const cacheDate = value => value ? new Date(value).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }) : '';
export const quoteTable = value => `"${String(value).replaceAll('"', '""')}"`;
export function cacheCell(value) {
  if (value === null) return 'NULL';
  if (value && typeof value === 'object' && value.type === 'blob') return `BLOB · ${cacheBytes(value.bytes)}`;
  return typeof value === 'object' ? JSON.stringify(value) : String(value ?? '');
}
export function cacheObject(value) {
  if (typeof value === 'string') { try { const parsed = JSON.parse(value); if (parsed && typeof parsed === 'object') return parsed; } catch {} }
  return value;
}
export function cacheCSV(result) {
  const escape = value => `"${cacheCell(value).replaceAll('"', '""')}"`;
  // Prefix spreadsheet formulas in exported text cells.
  const safe = value => typeof value === 'string' && /^[=+\-@\t\r]/.test(value) ? `'${value}` : value;
  return [result.columns, ...result.rows].map(row => row.map(value => escape(safe(value))).join(',')).join('\r\n');
}
export function cacheView(view = {}) { return { snapshot: '', baseline: '', comparison: '', table: '', compareTable: '', mode: 'explore', sql: '', folder: '', ignoreVolatile: false, ...view }; }
// A poll updates data, not the user's navigation or an unfinished connection.
// null means keep the current view; '' means a selected workspace was removed.
export function cacheWorkspaceAfterRefresh(library, selected, { formOpen = false, intent = '' } = {}) {
  const workspaces = library.workspaces || [];
  if (intent && workspaces.some(workspace => workspace.id === intent)) return intent;
  if (formOpen || workspaces.some(workspace => workspace.id === selected)) return null;
  const fallback = workspaces.find(workspace => workspace.id === library.active)?.id || workspaces[0]?.id || '';
  return fallback === selected ? null : fallback;
}
export function cachePair(snapshots, selected) {
  const sorted = [...snapshots].sort((a, b) => new Date(a.createdAt) - new Date(b.createdAt));
  const at = sorted.findIndex(item => item.id === selected);
  return { baseline: sorted[Math.max(0, at - 1)]?.id || '', comparison: sorted[at]?.id || sorted.at(-1)?.id || '' };
}

export function cacheRecord(columns,row) {
 const result=Object.create(null);
 columns.forEach((column,i)=>{let name=column;let suffix=2;while(Object.hasOwn(result,name))name=`${column} (${suffix++})`;result[name]=row[i];});return result;
}
