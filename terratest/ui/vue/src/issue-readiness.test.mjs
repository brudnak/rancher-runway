import test from 'node:test';
import assert from 'node:assert/strict';
import {createIssueReadinessRequest} from './issue-readiness-request.mjs';
import {dailyBuildGroups,dailyEntries} from './daily-readiness.mjs';

test('switching issues and closing cannot publish an older scan',async()=>{
 const pending=new Map(),updates=[];
 const request=createIssueReadinessRequest((url,signal)=>new Promise(resolve=>pending.set(url,{resolve,signal})),state=>updates.push(state));
 const one=request.start('issue-one'),two=request.start('issue-two');
 assert.equal(pending.get('issue-one').signal.aborted,true);
 pending.get('issue-two').resolve({title:'second'});await two;
 pending.get('issue-one').resolve({title:'first'});await one;
 assert.equal(updates.at(-1).report.title,'second');
 const three=request.start('issue-three');request.cancel();pending.get('issue-three').resolve({title:'third'});await three;
 assert.equal(updates.filter(state=>state.report).length,1);
});
test('daily build counts require the same observed pair and exclude closed or unproven issues',()=>{
 const build={reference:'staging/head',serverDigest:'sha256:a',agentReference:'staging/agent:head',agentDigest:'sha256:b'};
 const rows=[{issueUrl:'one',verdict:'ready',builds:[build,build],needsPlan:true,qaComplete:true,qaState:'draft'},{issueUrl:'two',verdict:'ready',builds:[{...build,agentDigest:'sha256:c'}]},{issueUrl:'closed',closed:true,verdict:'ready',needsPlan:true,builds:[build]},{issueUrl:'unknown',verdict:'unknown',builds:[build]},{issueUrl:'pending',verdict:'pending'}];
 assert.deepEqual(dailyBuildGroups(rows).map(group=>group.count),[1,1]);
 assert.deepEqual(dailyEntries(rows,'plans').map(row=>row.issueUrl),['one']);
 assert.deepEqual(dailyEntries(rows,'qa').map(row=>row.issueUrl),['one']);
 assert.deepEqual(dailyEntries(rows,'unchecked').map(row=>row.issueUrl),['pending']);
});

test('stream decodes split UTF-8 progress and requires a final report',async()=>{
 const {readReadinessStream}=await import('./issue-readiness-request.mjs');
 const bytes=new TextEncoder().encode(JSON.stringify({type:'progress',progress:{message:'Reading Steve → Rancher…'}})+'\n'+JSON.stringify({type:'report',report:{verdict:'ready'}}));
 const response=new Response(new ReadableStream({start(controller){for(let i=0;i<bytes.length;i+=3)controller.enqueue(bytes.slice(i,i+3));controller.close();}}));
 const progress=[];assert.equal((await readReadinessStream(response,event=>progress.push(event))).verdict,'ready');assert.equal(progress[0].message,'Reading Steve → Rancher…');
 await assert.rejects(readReadinessStream(new Response('{"type":"progress","progress":{}}\n'),()=>{}),/before a report/);
 await assert.rejects(readReadinessStream(new Response('{"type":"error","error":"GitHub unavailable"}\n'),()=>{}),/GitHub unavailable/);
});
test('superseded scans cannot publish late progress',async()=>{
 const pending=[],updates=[];const request=createIssueReadinessRequest((url,signal,progress)=>new Promise(resolve=>pending.push({resolve,progress})),state=>updates.push(state));
 const one=request.start('one'),two=request.start('two');pending[0].progress({message:'old'});pending[1].progress({message:'current'});
 assert.equal(updates.at(-1).issueUrl,'two');assert.deepEqual(updates.at(-1).progress,[{message:'current'}]);pending[0].resolve({});pending[1].resolve({});await Promise.all([one,two]);
});

test('daily green lights require explicit combined evidence, including for older saved reports',()=>{
 const rows=[{issueUrl:'green',verdict:'ready',workflow:{greenLight:true}},{issueUrl:'review',verdict:'ready',workflow:{greenLight:false}},{issueUrl:'old',verdict:'ready'},{issueUrl:'closed',verdict:'ready',workflow:{greenLight:true},closed:true},{issueUrl:'exempt',verdict:'ready',workflow:{greenLight:true},qaNone:true},{issueUrl:'no-build',verdict:'unknown',workflow:{greenLight:true}}];
 assert.deepEqual(dailyEntries(rows,'green').map(row=>row.issueUrl),['green']);
 assert.deepEqual(dailyEntries(rows,'ready').map(row=>row.issueUrl),['green','review','old']);
});
