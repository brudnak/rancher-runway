import test from 'node:test';
import assert from 'node:assert/strict';
import {workingDays,extractPlanDates,effectiveCheckpoints,trackerIssues,validDay,checkpointOptions} from './release-trackers.mjs';
test('working days cross weekends, holidays and DST without counting today',()=>{
 assert.equal(workingDays('2026-10-16',[],'2026-10-01'),11);
 assert.equal(workingDays('2026-10-16',['2026-10-12'],'2026-10-01'),10);
 assert.equal(workingDays('2026-10-05',[],'2026-10-02'),1);
 assert.equal(workingDays('2026-10-01',[],'2026-10-01'),0);
 assert.equal(workingDays('2026-10-01',[],'2026-10-02'),-1);
 assert.equal(validDay('2026-02-30'),false);
});
test('planner extraction retains evidence and tentative wording; does not guess ambiguous dates',()=>{
 const found=extractPlanDates('Code Freeze\n16 Oct 2026\nCharts QA Sign Off: 22 Oct 2026\nTentative Release | October 28, 2026\nUnknown 10/11/26\nBad 30 Feb 2026');
 assert.deepEqual(found.map(x=>[x.name,x.date]),[['Code Freeze','2026-10-16'],['Charts QA Sign Off','2026-10-22'],['Tentative Release','2026-10-28']]);
 assert.equal(found[2].tentative,true);assert.match(found[0].source,/Code Freeze/);
});
test('milestone overrides replace only the matching shared checkpoint',()=>{
 const t={checkpoints:[{name:'Freeze',date:'2026-10-16',milestone:''},{name:'Freeze',date:'2026-10-20',milestone:'r/r#1'},{name:'Release',date:'2026-10-28',milestone:''}]};
 assert.deepEqual(effectiveCheckpoints(t,'r/r#1').map(c=>c.date),['2026-10-20','2026-10-28']);
 assert.deepEqual(effectiveCheckpoints(t,'r/r#2').map(c=>c.date),['2026-10-16','2026-10-28']);
});
test('aggregate queue deduplicates linked issues and excludes archives',()=>{
 const t={id:'a',name:'October',milestones:[{config:{repo:'r/r',milestone:1},snapshot:{milestone:{title:'v1'},issues:[{html_url:'https://github.com/r/r/issues/1'}]}}]};
 assert.equal(trackerIssues([t,{...t,id:'b'}]).length,1);
 assert.equal(trackerIssues([{...t,archived:true}]).length,0);
 assert.equal(trackerIssues([t],'a','r/r#2').length,0);
});

test('unchecked dates retain details but do not appear in timelines or inherited schedules',()=>{
 const t={checkpoints:[{name:'Code freeze',date:'2026-10-16',milestone:''},{name:'Feature complete',date:'2026-10-09',milestone:'',disabled:true},{name:'Code freeze',date:'2026-10-20',milestone:'r/r#1',disabled:true}]};
 assert.deepEqual(effectiveCheckpoints(t).map(c=>c.date),['2026-10-16']);
 assert.deepEqual(effectiveCheckpoints(t,'r/r#1'),[]);
 t.checkpoints[1].disabled=false;
 assert.equal(effectiveCheckpoints(t)[0].date,'2026-10-09');
});

test('new trackers default to bug complete; existing and renamed options survive editing',()=>{
 const fresh=checkpointOptions([],true);
 assert.deepEqual(fresh.filter(c=>!c.disabled).map(c=>c.name),['Bug complete']);
 const saved=fresh.map(c=>c.kind==='bug-complete'?{...c,name:'Bug complete & release notes',date:'2026-10-09',disabled:true}:c);
 const reopened=checkpointOptions(saved);
 assert.equal(reopened.length,saved.length);
 assert.equal(reopened[0].name,'Bug complete & release notes');
 assert.equal(reopened[0].date,'2026-10-09');
 assert.equal(reopened[0].disabled,true);
 const legacy=checkpointOptions([{id:'old',name:'Code freeze',date:'2026-10-16',milestone:''}]);
 assert.equal(legacy.filter(c=>c.kind==='code-freeze').length,1);
 assert.equal(legacy[0].disabled,false);
});

test('saved work matches by milestone but belongs only to its full owner scope',async()=>{
 const {ownsSavedWork,matchingWorkMilestone}=await import('./release-trackers.mjs');
 const config={repo:'rancher/rancher',milestone:19,scope:'mine',user:'brudnak',label:'team/frameworks'};
 const snapshot={config};
 const tracker={milestones:[{config:{...config,scope:'all'}}]};
 assert.equal(matchingWorkMilestone(tracker,snapshot),true);
 assert.equal(ownsSavedWork(tracker,snapshot),false);
 tracker.milestones[0].config={...config};
 assert.equal(ownsSavedWork(tracker,snapshot),true);
 assert.equal(matchingWorkMilestone({milestones:[]},snapshot),false);
});
