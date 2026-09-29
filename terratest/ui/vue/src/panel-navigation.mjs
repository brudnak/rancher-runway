import { workspaceTools, toolGroups, filterWorkspaceTools } from './home-workspace.mjs';

const groupLabels = { operate: 'Deploy & operate', investigate: 'Investigate', local: 'Local tools' };
const groupIcons = { operate: 'server', investigate: 'search', local: 'flask' };
// The Home guide is the catalog: new tools appear here without another tab list.
export const navigationGroups = toolGroups.map(group => ({ ...group,
  label: groupLabels[group.id] || group.title, icon: groupIcons[group.id] || 'layers',
  tabs: workspaceTools.filter(tool => tool.group === group.id),
}));

export const navigationGroup = tab => navigationGroups.find(group => group.tabs.some(item => item.id === tab));

export function matchingNavigationGroups(query, group = 'all') {
  const searching = Boolean(String(query || '').trim());
  const words = String(query || '').toLowerCase().trim().split(/\s+/).filter(Boolean);
  const relevance = tool => words.reduce((score, word) => score + (tool.label.toLowerCase().split(/\W+/).includes(word) ? 4 : tool.label.toLowerCase().includes(word) ? 2 : 0), 0);
  const matches = new Set(filterWorkspaceTools(query).map(tool => tool.id));
  return navigationGroups.filter(item => searching || group === 'all' || item.id === group)
    .map(item => ({ ...item, tabs: item.tabs.filter(tool => matches.has(tool.id)).sort((a,b) => relevance(b) - relevance(a)) }))
    .filter(item => item.tabs.length)
    .sort((a,b) => relevance(b.tabs[0]) - relevance(a.tabs[0]));
}

// Shared by both navigation rows. Arrow keys move focus without activating a page.
export function navigationFocusIndex(key, index, count) {
  if (!count) return null;
  if (key === 'Home') return 0;
  if (key === 'End') return count - 1;
  if (key === 'ArrowDown') return (index + 1) % count;
  if (key === 'ArrowUp') return (index + count - 1) % count;
  return null;
}
