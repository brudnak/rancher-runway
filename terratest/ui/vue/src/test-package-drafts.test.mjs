import test from 'node:test';
import assert from 'node:assert/strict';
import {createPackageDraftController, packageDraftHasWriting, packageDraftObservationKey} from './test-package-drafts.mjs';

const fingerprint = 'a'.repeat(64);
const draft = notes => ({basePlanFingerprint:fingerprint, plan:{notes}, observations:[]});
const deferred = () => { let resolve, reject; const promise = new Promise((a,b) => { resolve=a; reject=b; }); return {promise,resolve,reject}; };
const tick = () => new Promise(resolve => setImmediate(resolve));
function fixture(hook = async () => {}) {
  let revision = '', value = {basePlanFingerprint:fingerprint,plan:null,observations:[]}, counter=0;
  const calls=[], scheduled=new Map();
  let timerId=0;
  const timers={setTimeout(fn){scheduled.set(++timerId,fn);return timerId;},clearTimeout(id){scheduled.delete(id);}};
  const response = () => ({draft:{...structuredClone(value),revision,updatedAt:revision?'2026-09-30T12:00:00Z':undefined},currentPlanFingerprint:fingerprint,planChanged:false,observationConflicts:[],warnings:[]});
  const request = async (action, fields) => {
    calls.push({action,...structuredClone(fields)});
    await hook(action,fields);
    if (action === 'draft-read') return response();
    if (fields.draftRevision !== revision) throw Object.assign(new Error('writing draft changed in another view'),{status:409});
    revision=`revision-${++counter}`;
    value=action==='draft-clear'?{basePlanFingerprint:fingerprint,plan:null,observations:[]}:structuredClone(fields.draft);
    return response();
  };
  const states=[],saved=[];
  const controller=createPackageDraftController({request,timers,onState:(id,state)=>states.push({id,...state}),onSaved:(id,response)=>saved.push({id,response})});
  return {controller,calls,states,saved,scheduled,response,externalWrite(notes){value=draft(notes);revision=`revision-${++counter}`;}};
}

test('recovery loads existing text and debounce writes only the most recent draft',async()=>{
  const f=fixture();
  f.externalWrite('recover me');
  const recovered=await f.controller.load('package-a');
  assert.equal(recovered.draft.plan.notes,'recover me');
  f.controller.queue('package-a',draft('first edit'));
  f.controller.queue('package-a',draft('most recent edit'));
  assert.equal(f.controller.state('package-a').hasPending,true);
  assert.equal(f.scheduled.size,1);
  await f.controller.flush('package-a');
  assert.equal(f.calls.filter(call=>call.action==='draft-write').length,1);
  assert.equal(f.response().draft.plan.notes,'most recent edit');
  assert.equal(f.controller.state('package-a').status,'saved');
  assert.equal(f.controller.state('package-a').hasPending,false);
  assert.equal(f.scheduled.size,0);
  f.controller.dispose();
});

test('edits during an in-flight save are serialized with the returned CAS revision',async()=>{
  const first=deferred();
  let writes=0;
  const f=fixture(async action=>{if(action==='draft-write'&&++writes===1)await first.promise;});
  await f.controller.load('package-a');
  f.controller.queue('package-a',draft('first'));
  const flushing=f.controller.flush('package-a');
  await tick();
  f.controller.queue('package-a',draft('second'));
  first.resolve();
  await flushing;
  const calls=f.calls.filter(call=>call.action==='draft-write');
  assert.equal(calls.length,2);
  assert.equal(calls[0].draftRevision,'');
  assert.equal(calls[1].draftRevision,'revision-1');
  assert.equal(f.response().draft.plan.notes,'second');
  f.controller.dispose();
});

test('clear follows in-flight saves and cannot erase text typed after discard',async()=>{
  const first=deferred();
  let writes=0;
  const f=fixture(async action=>{if(action==='draft-write'&&++writes===1)await first.promise;});
  await f.controller.load('package-a');
  f.controller.queue('package-a',draft('discard this'));
  const flushing=f.controller.flush('package-a');
  await tick();
  const clearing=f.controller.clear('package-a');
  f.controller.queue('package-a',draft('new work after discard'));
  const finalFlush=f.controller.flush('package-a');
  first.resolve();
  await Promise.all([flushing,clearing,finalFlush]);
  assert.deepEqual(f.calls.filter(call=>call.action!=='draft-read').map(call=>call.action),['draft-write','draft-clear','draft-write']);
  assert.equal(f.response().draft.plan.notes,'new work after discard');
  assert.equal(f.calls.at(-1).draftRevision,'revision-2');
  f.controller.dispose();
});

test('clearing cancels a queued save before it starts',async()=>{
  const f=fixture();
  await f.controller.load('package-a');
  f.controller.queue('package-a',draft('do not resurrect'));
  const flushing=f.controller.flush('package-a');
  const clearing=f.controller.clear('package-a');
  await Promise.all([flushing,clearing]);
  assert.equal(f.calls.filter(call=>call.action==='draft-write').length,0);
  assert.equal(f.response().draft.plan,null);
  assert.equal(f.controller.state('package-a').hasPending,false);
  f.controller.dispose();
});

test('competing view conflicts pause autosave and preserve latest local text until reviewed',async()=>{
  const f=fixture();
  await f.controller.load('package-a');
  f.externalWrite('other window');
  f.controller.queue('package-a',draft('my unsaved text'));
  await assert.rejects(f.controller.flush('package-a'),/another view/);
  assert.equal(f.controller.state('package-a').status,'conflict');
  assert.equal(f.controller.state('package-a').hasPending,true);
  f.controller.queue('package-a',draft('my newest text'));
  await assert.rejects(f.controller.flush('package-a'),/another view/);
  assert.equal(f.calls.filter(call=>call.action==='draft-write').length,1);
  const recovered=await f.controller.load('package-a',{force:true});
  assert.equal(recovered.draft.plan.notes,'other window');
  assert.equal(f.scheduled.size,0);
  assert.equal(f.controller.state('package-a').needsReview,true);
  await assert.rejects(f.controller.flush('package-a'),/Review the recovered/);
  assert.equal(f.response().draft.plan.notes,'other window');
  // Explicitly choosing the reviewed local draft is required to resume saving.
  await f.controller.write('package-a',draft('reviewed merged text'));
  assert.equal(f.response().draft.plan.notes,'reviewed merged text');
  assert.equal(f.controller.state('package-a').status,'saved');
  f.controller.dispose();
});

test('request failure retains edits and no failure claims the draft is saved',async()=>{
  const failure=deferred();
  const f=fixture(async action=>{if(action==='draft-write')await failure.promise;});
  await f.controller.load('package-a');
  f.controller.queue('package-a',draft('first'));
  const flushing=f.controller.flush('package-a');
  await tick();
  f.controller.queue('package-a',draft('latest text'));
  failure.reject(new Error('disk unavailable'));
  await assert.rejects(flushing,/disk unavailable/);
  assert.equal(f.controller.state('package-a').status,'error');
  assert.equal(f.controller.state('package-a').hasPending,true);
  assert.equal(f.saved.length,0);
  assert.equal(f.response().draft.plan,null);
  f.controller.dispose();
});

test('package queues remain independent and recovered content helper ignores tombstones',async()=>{
  const f=fixture();
  await f.controller.load('package-a');
  assert.equal(packageDraftHasWriting(f.response().draft),false);
  assert.equal(packageDraftHasWriting(draft('text')),true);
  assert.equal(packageDraftHasWriting({observations:[{notes:''}]}),true);
  assert.equal(packageDraftObservationKey('p','s','c'),'p/s/c');
  f.controller.queue('package-a',draft('pending'));
  assert.equal(f.controller.state('package-b').hasPending,false);
  f.controller.dispose();
  assert.equal(f.scheduled.size,0);
});

test('forgetting a deleted package cancels queued work and ignores in-flight callbacks',async()=>{
  const first=deferred();
  const f=fixture(async action=>{if(action==='draft-write')await first.promise;});
  await f.controller.load('package-a');
  f.controller.queue('package-a',draft('about to delete'));
  const flushing=f.controller.flush('package-a');
  await tick();
  f.controller.forget('package-a');
  const stateCount=f.states.length;
  first.resolve();
  await flushing;
  assert.equal(f.saved.length,0);
  assert.equal(f.states.length,stateCount);
  assert.equal(f.scheduled.size,0);
  assert.equal(f.controller.state('package-a').hasPending,false);
  f.controller.dispose();
});
