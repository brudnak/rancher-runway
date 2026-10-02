import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {
  liveSteveRun, visibleRecords, sessionStatus, filterSessions, portError,
  imageTagError, normalizeImageTag, k3sMinor, refError, parseEnvironment, parseArguments,
  steveDraftErrors, steveStartPayload, reuseSteveDraft, shellQuote, kubectlCommand,
  timeLabel, durationLabel, logLines, highlightMatch, createSingleFlight, createLatestTask,
} from './local-lab.mjs';

const draft={steveRef:'v0.7.2',k3sVersion:'v1.33.5-k3s1',httpsPort:'',enableMetrics:false,metricsInterval:15,extraEnv:'',extraArgs:''};
const deferred=()=>{let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no;});return {promise,resolve,reject};};

test('session groups distinguish Steve startup from serving and preserve failures',()=>{
  assert.deepEqual(sessionStatus({status:'running'},'steve'),{label:'Starting',tone:'working',group:'running'});
  assert.equal(sessionStatus({status:'running'},'k3d').label,'Running');
  assert.equal(sessionStatus({status:'serving'},'steve').label,'Serving');
  assert.equal(sessionStatus({status:'stopped',error:'Failed to stop completely'},'k3d').group,'attention');
  assert.equal(liveSteveRun({status:'failed',stevePid:421}),true);
  assert.equal(liveSteveRun({status:'stopped'}),false);
  assert.deepEqual(visibleRecords([{status:'deleted'},{status:'cleaned'},{status:'stopped'}]),[{status:'stopped'}]);
  assert.deepEqual(visibleRecords(undefined),[]);
});

test('workspace search combines words across fields, filters status and sorts newest first',()=>{
  const records=[
    {runId:'one',status:'stopped',steveRef:'feature/cache',k3sVersion:'v1.32.2',createdAt:'2026-09-01'},
    {runId:'two',status:'serving',steveRef:'Feature/Cache',k3sVersion:'v1.33.5',createdAt:'2026-09-02'},
    {runId:'three',status:'cleaned',steveRef:'Feature/Cache',createdAt:'2026-09-03'},
    {runId:'legacy',status:'stopped',createdAt:'invalid'},
  ];
  assert.deepEqual(filterSessions(records,{kind:'steve'}).map(r=>r.runId),['two','one','legacy']);
  assert.deepEqual(filterSessions(records,{kind:'steve',search:'CACHE 1.33'}).map(r=>r.runId),['two']);
  assert.deepEqual(filterSessions(records,{kind:'steve',filter:'stopped',search:'cache'}).map(r=>r.runId),['one']);
  assert.equal(filterSessions(records,{search:'unmatched'}).length,0);
  assert.deepEqual(records.map(r=>r.runId),['one','two','three','legacy']);
});

test('ports accept automatic or safe integers and reject partial numeric strings',()=>{
  for(const port of ['',null,undefined,1024,'65535'])assert.equal(portError(port),'');
  for(const port of ['0',1023,65536,'1e4','1234.5','8443junk','-1000','Infinity'])assert.match(portError(port),/whole port/);
});

test('port hints reserve stopped K3D clusters and exempt only replaced Steve endpoints',()=>{
  const k3d=[{runId:'paused',status:'stopped',apiPort:16443},{runId:'deleted',status:'deleted',apiPort:16444}];
  assert.match(portError(16443,k3d),/paused/);
  assert.equal(portError(16444,k3d),'');
  const steve=[{runId:'active',status:'serving',httpsPort:8443,httpPort:8080}];
  assert.match(portError(8443,steve,{kind:'steve'}),/active/);
  assert.match(portError(8080,steve,{kind:'steve'}),/active/);
  assert.equal(portError(8443,steve,{kind:'steve',replacing:true}),'');
});

test('K3s tags and source refs are validated without excluding ordinary refs',()=>{
  assert.equal(normalizeImageTag(' rancher/k3s:v1.33.5-k3s1 '),'v1.33.5-k3s1');
  assert.equal(k3sMinor('rancher/k3s:v1.33.5-k3s1'),'1.33');
  assert.equal(k3sMinor('latest'),'');
  for(const tag of ['v1.33.5-k3s1','rancher/k3s:v1.33.5-k3s1','latest'])assert.equal(imageTagError(tag),'');
  for(const tag of ['','--help','v1.33.5+k3s1','tag;echo bad','other/repo:tag'])assert.ok(imageTagError(tag));
  for(const value of ['v0.7.2','feature/cache','a4106c3','pull/12/head'])assert.equal(refError(value),'');
  assert.ok(refError('ref with spaces'));
});

test('environment parsing preserves equals and spaces in values and rejects duplicate keys',()=>{
  assert.deepEqual(parseEnvironment(' A=one two\n\nB=a=b=c\r\nEMPTY='),['A=one two','B=a=b=c','EMPTY=']);
  assert.throws(()=>parseEnvironment('A=secret-one\nA=secret-two'),error=>/A appears twice/.test(error.message)&&!error.message.includes('secret'));
  assert.throws(()=>parseEnvironment('9BAD=secret'),/valid variable name/);
  assert.throws(()=>parseEnvironment('MISSING'),/NAME=value/);
  assert.throws(()=>parseEnvironment('A=valid\n\nMISSING'),/Line 3/);
  assert.throws(()=>parseEnvironment('A=\0'),/null character/);
});

test('each argument is kept intact, without accidental whitespace tokenization',()=>{
  assert.deepEqual(parseArguments('--label=hello world\n--debug\n\n--literal=$(something)'),['--label=hello world','--debug','--literal=$(something)']);
  assert.throws(()=>parseArguments('--bad=\0'),/control characters/);
});

test('Steve payload validates metrics only when enabled and preserves API contracts',()=>{
  assert.deepEqual(steveDraftErrors(draft),{});
  assert.equal(steveDraftErrors({...draft,metricsInterval:0}).metricsInterval,undefined);
  for(const value of [0,-1,1.5,'abc','1e3'])assert.ok(steveDraftErrors({...draft,enableMetrics:true,metricsInterval:value}).metricsInterval);
  const result=steveStartPayload({...draft,steveRef:' main ',k3sVersion:'rancher/k3s:v1.33.5-k3s1',enableMetrics:true,metricsInterval:'20',extraEnv:'A=one=two',extraArgs:'--label=hello world'},true);
  assert.deepEqual(result,{steveRef:'main',k3sVersion:'v1.33.5-k3s1',keepCluster:true,httpsPort:0,headerAuth:true,enableMetrics:true,metricsUpdateIntervalSeconds:20,extraEnv:['A=one=two'],extraArgs:['--label=hello world'],replace:true});
});

test('reusing a Steve session retains runtime settings while releasing its occupied port',()=>{
  const record={steveRef:'feature/cache',k3sVersion:'v1.32.9-k3s1',httpsPort:9443,enableMetrics:true,metricsUpdateIntervalSeconds:30,extraEnv:['A=x y','B=a=b'],extraArgs:['--label=hello world','--debug']};
  const reused=reuseSteveDraft(record);
  assert.equal(reused.httpsPort,'');
  const payload=steveStartPayload(reused);
  assert.equal(payload.httpsPort,0);
  assert.equal(payload.k3sVersion,record.k3sVersion);
  assert.deepEqual(payload.extraEnv,record.extraEnv);
  assert.deepEqual(payload.extraArgs,record.extraArgs);
  assert.equal(payload.metricsUpdateIntervalSeconds,30);
});

test('copied shell commands safely round-trip paths without changing kube context',()=>{
  const path="/tmp/it's a folder/$(do-not-run);`not-a-command`/kubeconfig";
  assert.equal(execFileSync('/bin/sh',['-c',`printf '%s' ${shellQuote(path)}`],{encoding:'utf8'}),path);
  assert.equal(kubectlCommand({kubeconfig:path}),`kubectl --kubeconfig ${shellQuote(path)} get nodes`);
  assert.equal(kubectlCommand({}),'');
});

test('unrecorded timestamps and running durations are readable',()=>{
  assert.equal(timeLabel('0001-01-01T00:00:00Z'),'Not recorded');
  assert.equal(timeLabel('nonsense'),'Not recorded');
  assert.equal(durationLabel('2026-01-01T00:00:00Z','2026-01-01T00:02:03Z'),'2m 3s');
  assert.equal(durationLabel('2026-01-01T00:00:00Z','2026-01-01T01:02:03Z'),'1h 2m');
  assert.equal(durationLabel('invalid'),'');
});

test('log filtering strips terminal color and keeps original line numbers',()=>{
  const source='\u001b[32mReady\u001b[0m\nlevel=warning certificate\nlevel=error temporary retry\nwatch running';
  assert.deepEqual(logLines(source,'',true).map(r=>[r.number,r.tone]),[[2,'warning'],[3,'error']]);
  assert.equal(logLines(source,'READY')[0].text,'Ready');
  assert.equal(logLines(source,'retry',true)[0].number,3);
  assert.equal(logLines(source,'missing').length,0);
});

test('highlighting treats HTML and regex punctuation as literal text',()=>{
  const text='<script>alert(1)</script> [error] [ERROR]';
  const parts=highlightMatch(text,'[error]');
  assert.equal(parts.map(p=>p.text).join(''),text);
  assert.equal(parts.filter(p=>p.match).length,2);
  assert.equal(highlightMatch(text,'not present')[0].match,false);
  assert.deepEqual(highlightMatch(text,''),[{text,match:false}]);
});

test('state requests are coalesced while in flight and release after failure',async()=>{
  let calls=0;let pending=deferred();
  const read=createSingleFlight(()=>{calls++;return pending.promise;});
  const one=read(),two=read();
  assert.equal(one,two);await Promise.resolve();assert.equal(calls,1);
  pending.resolve('ready');assert.equal(await one,'ready');
  pending=deferred();const failure=read();pending.reject(new Error('offline'));
  await assert.rejects(failure,/offline/);assert.equal(calls,2);
  pending=deferred();const retry=read();pending.resolve('recovered');assert.equal(await retry,'recovered');assert.equal(calls,3);
});

test('a late version suggestion cannot overwrite a newer ref result',async()=>{
  const task=createLatestTask(),old=deferred(),fresh=deferred(),results=[],signals=[];
  const first=task.run(signal=>{signals.push(signal);return old.promise;},value=>results.push(value));
  const second=task.run(signal=>{signals.push(signal);return fresh.promise;},value=>results.push(value));
  assert.equal(signals[0].aborted,true);
  fresh.resolve('new ref');assert.equal(await second,true);
  old.resolve('old ref');assert.equal(await first,false);
  assert.deepEqual(results,['new ref']);
});

test('leaving a tab cancels async work without applying stale errors or data',async()=>{
  const task=createLatestTask(),pending=deferred(),events=[];
  const run=task.run(()=>pending.promise,value=>events.push(value),error=>events.push(error.message));
  task.cancel();pending.reject(new Error('late error'));assert.equal(await run,false);assert.deepEqual(events,[]);
  assert.equal(await task.run(async()=>{throw new Error('current error')},()=>{},error=>events.push(error.message)),true);
  assert.deepEqual(events,['current error']);
});

test('a stale request failure cannot replace the success of the selected target',async()=>{
  const task=createLatestTask(),old=deferred(),current=deferred(),values=[],errors=[];
  const first=task.run(()=>old.promise,value=>values.push(value),error=>errors.push(error.message));
  const second=task.run(()=>current.promise,value=>values.push(value),error=>errors.push(error.message));
  current.resolve({clusterId:'new',version:'v2.16.0'});
  assert.equal(await second,true);
  old.reject(new Error('old cluster unavailable'));
  assert.equal(await first,false);
  assert.deepEqual(values,[{clusterId:'new',version:'v2.16.0'}]);
  assert.deepEqual(errors,[]);
});

test('cancelled work that ignores abort cannot commit after a later successful run',async()=>{
  const task=createLatestTask(),abandoned=deferred(),values=[];
  const first=task.run(()=>abandoned.promise,value=>values.push(value));
  task.cancel();
  await task.run(async()=>({clusterId:'replacement'}),value=>values.push(value));
  abandoned.resolve({clusterId:'removed'});
  assert.equal(await first,false);
  assert.deepEqual(values,[{clusterId:'replacement'}]);
});
