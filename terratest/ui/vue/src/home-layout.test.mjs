import test from 'node:test';
import assert from 'node:assert/strict';
import {homeBriefing,normalizeHomeLayout} from './home-layout.mjs';
const config={repo:'rancher/rancher',milestone:16,scope:'assigned',user:'brudnak',label:'team/frameworks'};
const issue=(number,overrides={})=>({html_url:`https://github.com/rancher/rancher/issues/${number}`,state:'open',updated_at:'2026-09-29T10:00:00Z',labels:[],...overrides});
const entry=(number,overrides={})=>({issueUrl:issue(number).html_url,verdict:'ready',workflow:{greenLight:true},...overrides});
const report=(entries)=>({config,startedAt:'2026-09-30T10:00:00Z',entries});
const snapshot=issues=>({config,issues});
test('Home only counts current QA issues with observed build and workflow evidence',()=>{
 const result=homeBriefing(report([entry(1),entry(2,{workflow:{greenLight:false}}),entry(3,{verdict:'pending'}),entry(4),entry(5),entry(6)]),snapshot([issue(1),issue(2),issue(3),issue(4,{state:'closed'}),issue(5,{labels:[{name:' QA/None '}]})]));
 assert.equal(result.builds,2);assert.equal(result.green,1);assert.equal(result.entries.length,3);
});
test('Home does not use a readiness report from another milestone or owner scope',()=>{
 for(const key of Object.keys(config))assert.equal(homeBriefing(report([entry(1)]),{...snapshot([issue(1)]),config:{...config,[key]:'changed'}}).available,false);
 assert.equal(homeBriefing(null,snapshot([])).available,false);
 assert.equal(homeBriefing(report([]),null).available,false);
});
test('changed issues need fresh evidence and older reports cannot imply a workflow green light',()=>{
 const result=homeBriefing(report([entry(1),entry(2,{workflow:undefined})]),snapshot([issue(1,{updated_at:'2026-09-30T11:00:00Z'}),issue(2)]));
 assert.equal(result.entries[0].changed,true);assert.equal(result.green,0);assert.equal(result.builds,1);
});
test('Home layout accepts the two supported preferences and recovers from invalid storage',()=>{
 assert.equal(normalizeHomeLayout('ready'),'ready');assert.equal(normalizeHomeLayout('continue'),'continue');
 for(const invalid of [undefined,null,'', 'missing'])assert.equal(normalizeHomeLayout(invalid),'continue');
});
