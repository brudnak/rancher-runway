export const CATTLE_BUNDLE_FORMAT = 'rancher-runway/cattle-configs';
export const CATTLE_BUNDLE_LIMIT = 80 * 1024 * 1024;
export const configFilename = name => /\.ya?ml$/i.test(name) ? name : `${name}.yml`;

// The same flattened tree drives rendering and keyboard navigation, so search
// and collapsed folders cannot leave keyboard focus on a hidden file.
export function configTree(library, expanded, search = '') {
  const query = search.trim().toLocaleLowerCase();
  const sorted = items => [...items].sort((a, b) => a.name.localeCompare(b.name, undefined, {numeric: true, sensitivity: 'base'}));
  const matches = file => file.name.toLocaleLowerCase().includes(query);
  const fileRow = (file, level) => ({...file, kind: 'file', key: `file:${file.id}`, level});
  const rows = [];
  for (const folder of sorted(library.folders)) {
    const children = sorted(library.files.filter(file => file.folder === folder.id));
    const visible = query && !folder.name.toLocaleLowerCase().includes(query) ? children.filter(matches) : children;
    if (query && !visible.length && !folder.name.toLocaleLowerCase().includes(query)) continue;
    const open = Boolean(query || expanded[folder.id]);
    rows.push({...folder, kind: 'folder', key: `folder:${folder.id}`, level: 1, count: children.length, visibleCount: visible.length, open});
    if (open) rows.push(...visible.map(file => fileRow(file, 2)));
  }
  rows.push(...sorted(library.files.filter(file => !file.folder && matches(file))).map(file => fileRow(file, 1)));
  return rows;
}

export function configTreeKey(rows, key, pressed) {
  const index = Math.max(0, rows.findIndex(row => row.key === key));
  const row = rows[index];
  if (!row) return {};
  if (pressed === 'ArrowDown') return {focus: rows[Math.min(index + 1, rows.length - 1)].key};
  if (pressed === 'ArrowUp') return {focus: rows[Math.max(index - 1, 0)].key};
  if (pressed === 'Home') return {focus: rows[0].key};
  if (pressed === 'End') return {focus: rows.at(-1).key};
  if (pressed === 'ArrowRight' && row.kind === 'folder') return row.open ? {focus: rows[index + 1]?.level === 2 ? rows[index + 1].key : row.key} : {expand: row.id};
  if (pressed === 'ArrowLeft') {
    if (row.kind === 'folder' && row.open) return {collapse: row.id};
    if (row.level === 2) return {focus: `folder:${row.folder}`};
  }
  return {};
}
