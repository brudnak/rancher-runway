import test from 'node:test';
import assert from 'node:assert/strict';
import { workspaceTools, toolGroups } from './home-workspace.mjs';
import { navigationGroups, navigationGroup, matchingNavigationGroups, navigationFocusIndex } from './panel-navigation.mjs';

test('the navigation contains every Home workspace exactly once in the same hierarchy', () => {
 const ids = navigationGroups.flatMap(group => group.tabs.map(tab => tab.id));
 assert.deepEqual([...ids].sort(), workspaceTools.map(tool => tool.id).sort());
 assert.equal(new Set(ids).size, ids.length);
 assert.deepEqual(navigationGroups.map(group => group.id), toolGroups.map(group => group.id));
 assert.equal(navigationGroup('cache').id, 'investigate');
 assert.equal(navigationGroup('home'), undefined);
});
test('group menus narrow browsing, while search always finds tools across the whole app', () => {
 assert.deepEqual(matchingNavigationGroups('', 'operate').map(group => group.id), ['operate']);
 assert.deepEqual(matchingNavigationGroups('sqlite', 'operate').flatMap(group => group.tabs.map(tab => tab.id)), ['cache','steve']);
 assert.deepEqual(matchingNavigationGroups('  PR   image  ').flatMap(group => group.tabs.map(tab => tab.id)), ['pr-builds','images']);
 assert.equal(matchingNavigationGroups('no-such-workspace').length, 0);
 assert.equal(matchingNavigationGroups('').flatMap(group => group.tabs).length, workspaceTools.length);
});
test('keyboard navigation wraps focus, supports boundaries, and leaves activation to Enter or Space', () => {
 assert.equal(navigationFocusIndex('ArrowDown', 4, 5), 0);
 assert.equal(navigationFocusIndex('ArrowUp', 0, 5), 4);
 assert.equal(navigationFocusIndex('Home', 2, 5), 0);
 assert.equal(navigationFocusIndex('End', 2, 5), 4);
 assert.equal(navigationFocusIndex('Enter', 2, 5), null);
 assert.equal(navigationFocusIndex(' ', 2, 5), null);
 assert.equal(navigationFocusIndex('ArrowDown', 0, 0), null);
});
