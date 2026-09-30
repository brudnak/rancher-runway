import test from 'node:test';
import assert from 'node:assert/strict';
import {filterPackages,safePackageURL,sessionProgress,changedPlan,packageCopy} from './test-packages.mjs';
test('package search matches every word across the issue and title while respecting status',()=>{
 const rows=[{title:'Project visibility',issue:{url:'https://github.com/rancher/rancher/issues/42'},status:'planning'},{title:'Provisioning',status:'closed'}];
 assert.equal(filterPackages(rows,'visibility 42').length,1);
 assert.equal(filterPackages(rows,'visibility missing').length,0);
 assert.equal(filterPackages(rows,'','closed')[0].title,'Provisioning');
});
test('completed steps do not imply a passing case',()=>{
 assert.deepEqual(sessionProgress([{outcome:'not-run',steps:[{done:true}]},{outcome:'failed'},{outcome:'blocked'}]),{total:3,complete:2,passed:0,failed:1,blocked:1,skipped:0});
});
test('external package references allow HTTPS without embedded credentials',()=>{
 assert.equal(safePackageURL('javascript:alert(1)'), '');
 assert.equal(safePackageURL('https://name:secret@example.test'), '');
 assert.equal(safePackageURL('https://github.com/rancher/rancher/issues/42'),'https://github.com/rancher/rancher/issues/42');
});
test('editable plans are independent from the preserved source',()=>{
 const source={cases:[{title:'Before',steps:[{action:'Original'}]}]};const draft=packageCopy(source);
 assert.equal(changedPlan(source,draft),false);draft.cases[0].steps[0].action='Changed';
 assert.equal(changedPlan(source,draft),true);assert.equal(source.cases[0].steps[0].action,'Original');
});
import {packagePlanProblem} from './test-packages.mjs';
test('plan validation points to the exact incomplete case or step',()=>{
 const plan={title:'Issue',cases:[{id:'a',title:'Visibility',steps:[{instruction:' '}]}]};
 assert.equal(packagePlanProblem(plan).caseId,'a');assert.match(packagePlanProblem(plan).message,/step 1/);
 plan.cases[0].steps[0].instruction='Open the project';assert.equal(packagePlanProblem(plan),null);
 plan.cases[0].title='';assert.match(packagePlanProblem(plan).message,/case 1/);
});

import {packageDate} from './test-packages.mjs';
test('absent, invalid, and Go zero timestamps are not historical dates',()=>{assert.equal(packageDate('0001-01-01T00:00:00Z'),'Not recorded');assert.equal(packageDate('bad date'),'Not recorded');assert.equal(packageDate(null),'Not recorded');});

import {newPackageRecordID} from './test-packages.mjs';
test('case and step IDs match the backend portable identifier format',()=>{const first=newPackageRecordID();assert.match(first,/^[0-9a-f]{24}$/);assert.notEqual(newPackageRecordID(),first);});

import {packageJourney} from './test-packages.mjs';
test('the journey follows plan, reproduction, fix, and validation without deciding outcomes',()=>{
 const pkg={status:'planning',cases:[],sessions:[]};
 let journey=packageJourney(pkg);
 assert.deepEqual(journey.stages.map(item=>item.state),['pending','pending','pending','pending']);assert.equal(journey.action,'plan');
 pkg.cases=[{id:'a',title:'Visibility',steps:[]}];
 assert.equal(packageJourney(pkg).action,'start-reproduction');assert.equal(packageJourney(pkg).stages[0].detail,'1 saved case');
 pkg.sessions=[{id:'s1',name:'Before the fix',purpose:'reproduction',status:'active',finding:'inconclusive',startedAt:'2026-09-01T00:00:00Z'}];
 journey=packageJourney(pkg);assert.equal(journey.stages[1].state,'active');assert.equal(journey.action,'finish-reproduction');assert.equal(journey.stages[1].sessionId,'s1');
 pkg.sessions[0].status='completed';pkg.sessions[0].finding='not-reproduced';
 journey=packageJourney(pkg);assert.equal(journey.stages[1].state,'attention');assert.equal(journey.action,'start-reproduction');
 pkg.sessions.push({id:'s2',name:'Second attempt',purpose:'reproduction',status:'completed',finding:'reproduced',startedAt:'2026-09-02T00:00:00Z'});
 pkg.sessions.push({id:'x',name:'Poking around',purpose:'exploration',status:'completed',finding:'inconclusive',startedAt:'2026-09-03T00:00:00Z'});
 journey=packageJourney(pkg);assert.equal(journey.stages[1].detail,'Issue reproduced · Second attempt');assert.equal(journey.action,'link-fix');
 pkg.fixUrl='https://github.com/rancher/rancher/pull/500';
 journey=packageJourney(pkg);assert.equal(journey.stages[2].detail,pkg.fixUrl);assert.equal(journey.action,'start-validation');
 pkg.fixTitle='Invalidate cached project lists';
 pkg.sessions.push({id:'v1',name:'Candidate 1',purpose:'validation',status:'completed',finding:'not-validated',baseline:{name:'Second attempt'},startedAt:'2026-09-04T00:00:00Z'});
 journey=packageJourney(pkg);assert.equal(journey.stages[2].detail,'Invalidate cached project lists');assert.equal(journey.stages[3].state,'attention');assert.equal(journey.action,'start-validation');
 pkg.sessions.push({id:'v2',name:'Candidate 2',fixUrl:pkg.fixUrl,purpose:'validation',status:'completed',finding:'validated',baseline:{name:'Second attempt'},startedAt:'2026-09-05T00:00:00Z'});
 journey=packageJourney(pkg);assert.equal(journey.stages[3].detail,'Fix validated · Candidate 2 · against baseline Second attempt');assert.equal(journey.action,'share');
 assert.equal(pkg.status,'planning');
});
test('package search also matches the linked fix',()=>{
 const rows=[{title:'Project visibility',fixUrl:'https://github.com/rancher/rancher/pull/500',fixTitle:'Invalidate cached lists',status:'validating'}];
 assert.equal(filterPackages(rows,'pull/500').length,1);assert.equal(filterPackages(rows,'cached lists').length,1);assert.equal(filterPackages(rows,'pull/501').length,0);
});

import {pullRequestReference,fixLookupState} from './test-packages.mjs';
test('pull request references match the backend pattern before any lookup is offered',()=>{
 assert.deepEqual(pullRequestReference(' https://github.com/rancher/rancher/pull/500/files '),{owner:'rancher',repo:'rancher',number:500,repository:'rancher/rancher',short:'rancher/rancher#500',url:'https://github.com/rancher/rancher/pull/500'});
 for(const bad of ['https://github.com/rancher/rancher/issues/500','http://github.com/rancher/rancher/pull/500','https://github.com/rancher/rancher/pull/500?diff=1','https://user:x@github.com/rancher/rancher/pull/5','https://github.evil.test/rancher/rancher/pull/500',''])assert.equal(pullRequestReference(bad),null,bad);
});
test('lookup state wording never claims more than GitHub reported',()=>{
 assert.equal(fixLookupState(null),'');
 assert.equal(fixLookupState({state:'open',draft:true}),'Open draft pull request');
 assert.equal(fixLookupState({state:'closed'}),'Closed without merging');
 assert.match(fixLookupState({state:'merged',mergedAt:'2026-09-28T10:00:00Z'}),/^Merged /);
});

test('the journey names the commit a validation session recorded',()=>{
 const pkg={cases:[{id:'a',title:'Visibility',steps:[]}],fixUrl:'https://github.com/rancher/rancher/pull/500',sessions:[{id:'v1',name:'Candidate 1',purpose:'validation',status:'active',finding:'inconclusive',fixCommit:'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',startedAt:'2026-09-04T00:00:00Z'}]};
 assert.equal(packageJourney(pkg).stages[3].detail,'In progress · Candidate 1 · commit aaaaaaaaaa');
});


test('changing the package fix does not reuse a different fix validation',()=>{
 const pkg={cases:[{id:'a'}],fixUrl:'https://github.com/rancher/rancher/pull/501',sessions:[
  {id:'r',purpose:'reproduction',status:'completed',finding:'reproduced'},
  {id:'v',name:'Earlier candidate',purpose:'validation',status:'completed',finding:'validated',fixUrl:'https://github.com/rancher/rancher/pull/500'}
 ]};
 let journey=packageJourney(pkg);
 assert.equal(journey.action,'start-validation');assert.equal(journey.stages[3].state,'attention');
 assert.match(journey.stages[3].detail,/Current fix has not been validated/);
 pkg.fixUrl='https://github.com/Rancher/rancher/pull/500/files';
 assert.equal(packageJourney(pkg).action,'share');
 pkg.sessions[1].fixUrl='';assert.equal(packageJourney(pkg).stages[3].state,'attention');
 assert.equal(pkg.sessions[1].finding,'validated');
});
