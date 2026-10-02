import test from 'node:test';
import assert from 'node:assert/strict';
import {comparableCase,compareSessionCases,comparisonSummary,changedCaseFields,compareEnvironments,nextCaseToCheck,caseStepProgress,evidenceForCase} from './issue-package-comparison.mjs';

const testCase=(id='a',title='Project isolation')=>({id,title,preconditions:'A member account',expected:'Only its project is visible',automation:'manual',steps:[{id:'s',instruction:'Open Projects',expected:'One project'}]});
const session=(cases,results=[],evidence=[])=>({cases,results,evidence});

test('omitted optional fields and empty arrays compare equally without changing source records',()=>{
  const before=testCase(),after={...testCase(),automationUrl:'',selection:[]};
  const original=JSON.stringify(before);
  assert.deepEqual(comparableCase(before),comparableCase(after));
  assert.equal(compareSessionCases(session([before]),session([after]))[0].comparability,'same');
  assert.equal(JSON.stringify(before),original);
});
test('a matching title cannot make different case IDs comparable',()=>{
  const rows=compareSessionCases(session([testCase('old')]),session([testCase('new')]));
  assert.deepEqual(rows.map(row=>row.comparability),['added','missing']);
  assert.equal(rows[0].baselineOutcome,null);
  assert.equal(rows[1].candidateOutcome,null);
});
test('changes to case text, step identity, ordering, or automation selection are explicitly flagged',()=>{
  const before=testCase(),after=structuredClone(before);
  after.steps[0].id='replacement';after.expected='All projects visible';after.selection=['TestProject'];
  assert.deepEqual(changedCaseFields(before,after),['Expected behavior','Automated selection','Steps']);
  const row=compareSessionCases(session([before]),session([after]))[0];
  assert.equal(row.comparability,'changed');
  const reordered={...before,steps:[...before.steps,{id:'s2',instruction:'Reload'}]};
  assert.deepEqual(changedCaseFields(reordered,{...reordered,steps:reordered.steps.slice().reverse()}),['Steps']);
});
test('matrix retains candidate order and appends removed baseline cases',()=>{
  const rows=compareSessionCases(session([testCase('a'),testCase('b'),testCase('c')]),session([testCase('c'),testCase('a')]));
  assert.deepEqual(rows.map(row=>row.id),['c','a','b']);
});
test('only matched case evidence belongs to a comparison row',()=>{
  const record=session([testCase()],[],[{id:'all'},{id:'case',caseId:'a'},{id:'step',caseId:'a',stepId:'s'},{id:'other',caseId:'b'}]);
  assert.deepEqual(evidenceForCase(record,'a').map(item=>item.id),['case','step']);
});
test('summary separates unavailable comparisons, missing execution and actual outcome changes',()=>{
  const baseline=session([testCase('a'),testCase('b'),testCase('c'),testCase('d'),testCase('e')],[{caseId:'a',outcome:'failed'},{caseId:'b',outcome:'failed'}]);
  const candidate=session([testCase('a'),testCase('b','Different procedure'),testCase('c'),testCase('d'),testCase('new')],[{caseId:'a',outcome:'passed'},{caseId:'b',outcome:'passed'},{caseId:'c',outcome:'blocked'},{caseId:'d',outcome:'skipped'}]);
  assert.deepEqual(comparisonSummary(compareSessionCases(baseline,candidate)),{same:3,changed:1,added:1,missing:1,notRun:1,blocked:1,skipped:1,outcomeChanges:1});
});
test('unperformed cases never inherit a baseline outcome',()=>{
  const rows=compareSessionCases(session([testCase()],[{caseId:'a',outcome:'failed'}]),session([testCase()]));
  assert.equal(rows[0].candidateOutcome,'not-run');assert.equal(comparisonSummary(rows).outcomeChanges,0);
});
test('environment comparison does not treat two unknown versions as verified equal',()=>{
  const rows=compareEnvironments({},{});
  assert.equal(rows.find(row=>row.key==='rancher').status,'unknown');
  assert.equal(rows.find(row=>row.key==='kubernetes').status,'unknown');
});
test('environment comparison excludes timestamps, preserves missing fields and normalizes image order',()=>{
  const before={rancherVersion:'v2.14.1',kubernetesVersion:'v1.34.1',clusterId:'one',images:['b','a'],observedAt:'old',details:[{label:'Webhook',value:'v0.9.1',observedAt:'old'}]};
  const after={...before,rancherVersion:'v2.14.2',images:['a','b','a'],observedAt:'new',details:[{label:'Webhook',value:'v0.9.1',observedAt:'new'},{label:'Flags',value:'SQL cache'}]};
  const rows=compareEnvironments(before,after);
  assert.equal(rows.find(row=>row.key==='rancher').status,'changed');
  assert.equal(rows.find(row=>row.key==='images').status,'same');
  assert.equal(rows.find(row=>row.key==='detail:webhook').status,'same');
  assert.equal(rows.find(row=>row.key==='detail:flags').status,'unknown');
});
test('navigation goes to the next unrun case and wraps without reselecting the current case',()=>{
  const record=session([testCase('a'),testCase('b'),testCase('c')],[{caseId:'a',outcome:'not-run'},{caseId:'b',outcome:'passed'},{caseId:'c',outcome:'not-run'}]);
  assert.equal(nextCaseToCheck(record,'a').id,'c');assert.equal(nextCaseToCheck(record,'c').id,'a');
  assert.equal(nextCaseToCheck(record,'missing').id,'a');
});
test('navigation prioritizes unrun cases over blocked and does not reopen decided failures',()=>{
  const record=session([testCase('a'),testCase('b'),testCase('c')],[{caseId:'a',outcome:'failed'},{caseId:'b',outcome:'blocked'},{caseId:'c',outcome:'not-run'}]);
  assert.equal(nextCaseToCheck(record,'a').id,'c');
  record.results[2].outcome='passed';
  assert.deepEqual(nextCaseToCheck(record,'a'),{id:'b',title:'Project isolation',blocked:true});
  assert.equal(nextCaseToCheck(record,'b'),null);
});
test('step progress ignores orphaned markers and never derives a case outcome',()=>{
  const result={outcome:'not-run',steps:[{stepId:'s',done:true},{stepId:'removed',done:true}]};
  assert.deepEqual(caseStepProgress(testCase(),result),{total:1,done:1});assert.equal(result.outcome,'not-run');
  assert.equal(nextCaseToCheck(session([testCase()],[]),'a'),null);
});
