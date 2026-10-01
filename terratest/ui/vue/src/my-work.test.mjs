import test from 'node:test';
import assert from 'node:assert/strict';
import {myWorkRows,myWorkMetrics,filterMyWork,workTrend} from './my-work.mjs';
const issue=(number,state='open')=>({number,title:`Issue ${number}`,state,html_url:`https://github.com/rancher/rancher/issues/${number}`});
test('closure is independent of plan coverage and explicit validation',()=>{const rows=myWorkRows({issues:[issue(1),issue(2),issue(3,'closed')]},[{id:'p',issueUrl:issue(2).html_url.toUpperCase()+'/',cases:[{id:'c'}],sessions:[]}]);assert.deepEqual(myWorkMetrics(rows),{total:3,open:2,closed:1,needsPlan:1,planned:1,needsReproduction:1,needsValidation:0,validated:0});assert.equal(rows[2].validated,false);assert.equal(filterMyWork(rows,'needsPlan')[0].issue.number,1);assert.equal(filterMyWork(rows,'closed','#3')[0].issue.number,3);});
test('validating an older fix does not count as validating the current fix',()=>{const pkg={issueUrl:issue(1).html_url,cases:[{id:'c'}],fixUrl:'https://github.com/rancher/rancher/pull/20',sessions:[{id:'s',purpose:'validation',status:'completed',finding:'validated',fixUrl:'https://github.com/rancher/rancher/pull/10'}]};assert.equal(myWorkRows({issues:[issue(1)]},[pkg])[0].validated,false);pkg.sessions[0].fixUrl=pkg.fixUrl;assert.equal(myWorkRows({issues:[issue(1)]},[pkg])[0].validated,true);});
test('multiple packages stay discoverable and use the latest plan for coverage',()=>{const rows=myWorkRows({issues:[issue(1)]},[{id:'old',issueUrl:issue(1).html_url,updatedAt:'2026-01-01',cases:[{}]},{id:'new',issueUrl:issue(1).html_url,updatedAt:'2026-09-30',cases:[]}]);assert.equal(rows[0].matches.length,2);assert.equal(rows[0].pkg.id,'new');assert.equal(rows[0].planned,false);});
test('trend draws observed counts even with a zero remaining count',()=>{assert.equal(workTrend([{open:10},{open:0}]),'0,10 300,80');assert.equal(workTrend([{open:0}]),'0,80');});
test('QA/None remains visible but does not count as remaining work or missing preparation',()=>{
 const rows=myWorkRows({issues:[{...issue(1),labels:[{name:'QA/None'}]},issue(2)]},[]);
 assert.equal(myWorkMetrics(rows).open,1);assert.equal(myWorkMetrics(rows).needsPlan,1);
 assert.deepEqual(filterMyWork(rows,'open').map(row=>row.issue.number),[2]);
 assert.deepEqual(filterMyWork(rows,'qaNone').map(row=>row.issue.number),[1]);
});
