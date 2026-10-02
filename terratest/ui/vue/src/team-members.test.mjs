import test from 'node:test';
import assert from 'node:assert/strict';
import {normalizeTeamMembers,issueOwnership} from './team-members.mjs';
test('team usernames normalize and deduplicate individual entries',()=>{assert.deepEqual(normalizeTeamMembers(['@Alice','alice',' Bob ','']),['alice','bob']);});
test('ownership keeps mine, team, outside and unassigned distinct',()=>{
 const issue={assignees:[{login:'Alice'},{login:'outsider'}]};
 assert.equal(issueOwnership(issue,['alice'],'alice').mine,true);
 assert.equal(issueOwnership(issue,['alice']).group,'team');
 assert.equal(issueOwnership(issue,['bob']).group,'outside');
 assert.equal(issueOwnership({assignees:[]},['alice']).group,'unassigned');
 assert.equal(issueOwnership({assignees:[{login:'outsider'}]},['alice'],'alice').mine,false);
});
