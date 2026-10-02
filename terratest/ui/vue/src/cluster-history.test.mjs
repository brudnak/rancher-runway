import test from 'node:test';
import assert from 'node:assert/strict';
import {historyGroups} from './cluster-history.mjs';
test('history prefers explicit or linked milestones and otherwise groups exact versions',()=>{
 const rows=[{id:'a',version:'head',milestone:'v2.16.0'},{id:'b',version:'v2.15.3-head'},{id:'c',version:'v2.15.2'},{id:'d'}];
 const packages=[{id:'pkg',sessions:[{environment:{clusterId:'b'}}]}];
 const buckets=[{name:'v2.15.3',packageIds:['pkg']}];
 const groups=historyGroups(rows,packages,buckets);
 assert.deepEqual(groups.map(g=>g.label),['Milestone · v2.16.0','Milestone · v2.15.3','Rancher · v2.15.2','Rancher · Unknown version']);
 assert.equal(historyGroups(rows,packages,buckets,'v2.15.3 head')[0].clusters[0].id,'b');
 assert.equal(historyGroups(rows,packages,buckets,'absent').length,0);
});
