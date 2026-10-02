import test from 'node:test';
import assert from 'node:assert/strict';
import {clusterTimeline, clusterHistoryMarkdown} from './cluster-timeline.mjs';
const operation = (id, status, from, to) => ({id, kind:'upgrade', status, from, to, started:`2026-10-01T${id}:00:00Z`, events:[]});
test('repeated upgrades retain individual attempts, commands and chronological order', () => {
 const archive = {cluster:{version:'head'}, events:[
  {id:'baseline',kind:'discovery',at:'2026-10-01T09:00:00Z',data:{version:'2.15.2'}},
  {id:'install',kind:'helm-install',data:{command:'helm install rancher --version 2.15.2'}},
  {id:'cmd',kind:'helm-upgrade-command',data:{operationId:'12',command:'helm upgrade rancher --version 2.16.1'}},
  {id:'observed',kind:'deployment',at:'2026-10-01T12:30:00Z',data:{rancherVersion:'v2.16.1',webhookChartVersion:'110.0.3',images:[{digest:'sha256:exact'}]}},
 ], operations:[operation('13','succeeded','v2.16.1','v2.16.2'),operation('11','failed','v2.15.3','v2.16.0'),operation('10','succeeded','v2.15.2','v2.15.3'),operation('12','succeeded','v2.15.3','v2.16.1')]};
 const steps = clusterTimeline(archive);
 assert.deepEqual(steps.map(step => step.id), ['initial','10','11','12','13']);
 assert.equal(steps[0].version, '2.15.2');
 assert.equal(steps[2].title, 'Upgrade attempt');
 assert.equal(steps[3].from, 'v2.15.3');
 assert.equal(steps[3].commands[0].id, 'cmd');
 assert.equal(steps[4].commands.length, 0);
 assert.equal(steps[3].observations[0].id, 'observed');
 assert.equal(steps[2].observations.length, 0);
 const md = clusterHistoryMarkdown(archive);
 for (const expected of ['helm install','helm upgrade','sha256:exact','110.0.3','Status: failed','Helm command not recorded.']) assert.ok(md.includes(expected), expected);
 assert.equal(archive.operations[0].id, '13', 'does not reorder stored records');
});
test('unknown and interrupted history never invents a completed upgrade or initial runtime', () => {
 assert.deepEqual(clusterTimeline(null), []);
 const steps = clusterTimeline({operations:[operation('10','interrupted','v2.15.2','head')]});
 assert.equal(steps[0].version, '');
 assert.equal(steps[0].at, undefined);
 assert.equal(steps[1].title, 'Upgrade attempt');
 assert.match(steps[1].note, /does not establish/);
 assert.equal(steps[1].commands.length, 0);
});
