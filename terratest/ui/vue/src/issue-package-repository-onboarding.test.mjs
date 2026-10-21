import test from 'node:test';
import assert from 'node:assert/strict';
import {createRepositoryOnboardingController} from './issue-package-repository-onboarding.mjs';

const deferred = () => { let resolve, reject; const promise = new Promise((yes,no) => { resolve=yes; reject=no; }); return {promise,resolve,reject}; };
const tick = () => new Promise(resolve => setImmediate(resolve));
function fixture() {
  const calls=[],states=[];
  const controller=createRepositoryOnboardingController({
    request(action,fields) { const response=deferred();calls.push({action,fields,response});return response.promise; },
    onState(state) { states.push(state); },
  });
  return {controller,calls,states,state:()=>states.at(-1)};
}
const attempt = {key:'new:1',contextTitle:'New issue package'};

test('onboarding checks before authoring and keeps the continuation pending until complete',async()=>{
  const f=fixture();
  let resumed=0;
  const ready=f.controller.begin(attempt).then(value=>{if(value)resumed++;return value;});
  assert.deepEqual(f.calls[0].fields,{id:''});
  assert.equal(f.calls[0].action,'plan-git-onboarding');
  assert.deepEqual(f.state(),{phase:'checking',context:{...attempt,packageId:''},error:''});
  f.calls[0].response.resolve({required:true});await tick();
  assert.equal(f.state().phase,'onboarding');assert.equal(resumed,0);
  assert.equal(f.controller.complete(),true);
  assert.equal(await ready,true);assert.equal(resumed,1);
  assert.deepEqual(f.state(),{phase:'idle',context:null,error:''});
  assert.equal(f.controller.complete(),false);assert.equal(resumed,1);
});

test('duplicate calls for the same authoring attempt share a promise and one status read',async()=>{
  const f=fixture();
  const first=f.controller.begin({key:'plan:a',packageId:'a',contextTitle:'Original context'});
  const duplicate=f.controller.begin({key:'plan:a',packageId:'a',contextTitle:'Repeated click'});
  assert.strictEqual(first,duplicate);assert.equal(f.calls.length,1);
  assert.equal(f.state().context.contextTitle,'Original context');
  assert.strictEqual(f.controller.retry(),first);assert.equal(f.calls.length,1);
  f.calls[0].response.resolve({required:true});await tick();
  assert.strictEqual(f.controller.begin({key:'plan:a'}),first);assert.equal(f.calls.length,1);
  f.controller.complete();assert.equal(await first,true);
});

test('linked or dismissed setup proceeds and remembers only the handled attempt',async()=>{
  const f=fixture();
  const ready=f.controller.begin({key:'plan:a',packageId:'a'});
  f.calls[0].response.resolve({required:false});
  assert.equal(await ready,true);assert.equal(f.state().phase,'idle');
  const cached=f.controller.begin({key:'plan:a',packageId:'a'});
  assert.ok(cached instanceof Promise);assert.equal(await cached,true);assert.equal(f.calls.length,1);
  const next=f.controller.begin({key:'plan:b',packageId:'b'});
  assert.equal(f.calls.length,2);assert.deepEqual(f.calls[1].fields,{id:'b'});
  f.controller.cancel();assert.equal(await next,false);
});

test('skip carries from new package creation into its first plan without another prompt',async()=>{
  const f=fixture();
  const ready=f.controller.begin(attempt);
  f.calls[0].response.resolve({required:true});await tick();
  f.controller.complete();assert.equal(await ready,true);
  f.controller.allow('plan:created-package');
  assert.equal(await f.controller.begin({key:'plan:created-package',packageId:'created-package'}),true);
  assert.equal(f.calls.length,1);
  const next=f.controller.begin({key:'new:2'});
  assert.equal(f.calls.length,2);f.controller.cancel();assert.equal(await next,false);
});

test('a new attempt replaces the old continuation and late responses cannot reopen it',async()=>{
  const f=fixture();
  let oldNavigations=0,newNavigations=0;
  const old=f.controller.begin(attempt).then(ready=>{if(ready)oldNavigations++;return ready;});
  const next=f.controller.begin({key:'plan:next',packageId:'next'}).then(ready=>{if(ready)newNavigations++;return ready;});
  assert.equal(await old,false);
  f.calls[1].response.resolve({required:false});assert.equal(await next,true);
  const statesAfterNew=f.states.length;
  f.calls[0].response.resolve({required:true});await tick();
  assert.equal(f.states.length,statesAfterNew);assert.equal(oldNavigations,0);assert.equal(newNavigations,1);
  assert.equal(f.state().phase,'idle');
});

test('replacing a visible prompt with an allowed key closes it without checking again',async()=>{
  const f=fixture();
  const old=f.controller.begin(attempt);f.calls[0].response.resolve({required:true});await tick();
  f.controller.allow('plan:already-handled');
  assert.equal(await f.controller.begin({key:'plan:already-handled'}),true);
  assert.equal(await old,false);assert.equal(f.calls.length,1);assert.equal(f.state().phase,'idle');
});

test('leaving the workspace cancels an unfinished check without permanently skipping the attempt',async()=>{
  const f=fixture();
  const ready=f.controller.begin(attempt);
  f.controller.cancel();assert.equal(await ready,false);
  const count=f.states.length;
  f.calls[0].response.resolve({required:true});await tick();assert.equal(f.states.length,count);
  const again=f.controller.begin(attempt);assert.equal(f.calls.length,2);
  f.calls[1].response.resolve({required:true});await tick();assert.equal(f.state().phase,'onboarding');
  f.controller.cancel();assert.equal(await again,false);
});

test('failed status reads retain context, remain skippable, and never mutate caller input',async()=>{
  const f=fixture();
  const context={...attempt,packageId:'',titleLines:'1. Preserve my unsaved title'};
  const ready=f.controller.begin(context);
  f.calls[0].response.reject(new Error('Could not read local settings'));await tick();
  assert.equal(f.state().phase,'error');assert.equal(f.state().error,'Could not read local settings');
  assert.equal(f.state().context.key,attempt.key);
  assert.equal(context.titleLines,'1. Preserve my unsaved title');
  f.controller.complete();assert.equal(await ready,true);
  assert.equal(await f.controller.begin(attempt),true);assert.equal(f.calls.length,1);
});

test('retry rechecks the same context and shares the original continuation',async()=>{
  const f=fixture();
  const ready=f.controller.begin({key:'plan:a',packageId:'a',contextTitle:'Case plan'});
  f.calls[0].response.reject(new Error('Temporary read failure'));await tick();
  assert.strictEqual(f.controller.retry(),ready);
  assert.equal(f.state().phase,'checking');assert.equal(f.state().error,'');
  assert.deepEqual(f.calls[1].fields,{id:'a'});
  assert.strictEqual(f.controller.retry(),ready);assert.equal(f.calls.length,2);
  f.calls[1].response.resolve({required:false});assert.equal(await ready,true);
});

test('malformed status cannot silently bypass setup, and synchronous failure remains skippable',async()=>{
  for(const response of [null,{}, {required:'false'}, {required:0}]) {
    const f=fixture(),ready=f.controller.begin(attempt);
    f.calls[0].response.resolve(response);await tick();
    assert.equal(f.state().phase,'error');assert.match(f.state().error,/incomplete/);
    f.controller.complete();assert.equal(await ready,true);
  }
  const states=[];
  const c=createRepositoryOnboardingController({request(){throw new Error('Unavailable');},onState:state=>states.push(state)});
  const ready=c.begin(attempt);assert.equal(states.at(-1).phase,'error');
  c.complete();assert.equal(await ready,true);
});

test('completion during a check suppresses its late failure, and dispose prevents future authoring',async()=>{
  const f=fixture(),ready=f.controller.begin(attempt);
  f.controller.complete();assert.equal(await ready,true);
  const count=f.states.length;
  f.calls[0].response.reject(new Error('Too late'));await tick();assert.equal(f.states.length,count);
  const next=f.controller.begin({key:'plan:b',packageId:'b'});
  f.controller.dispose();assert.equal(await next,false);
  const disposedCount=f.states.length;
  f.calls[1].response.resolve({required:true});await tick();
  assert.equal(f.states.length,disposedCount);
  assert.equal(await f.controller.begin(attempt),false);
  assert.equal(await f.controller.retry(),false);
  assert.equal(f.controller.complete(),false);assert.equal(f.calls.length,2);
  f.controller.dispose();assert.equal(f.states.length,disposedCount);
});

test('mutable UI state snapshots cannot rewrite the original authoring context',async()=>{
  const f=fixture();
  const ready=f.controller.begin({key:'plan:a',packageId:'a',contextTitle:'Original'});
  f.state().context.packageId='different';
  f.calls[0].response.reject(new Error('Retry needed'));await tick();
  f.state().context.key='different';
  f.controller.retry();assert.deepEqual(f.calls[1].fields,{id:'a'});
  f.calls[1].response.resolve({required:false});assert.equal(await ready,true);
  assert.equal(await f.controller.begin({key:'plan:a'}),true);assert.equal(f.calls.length,2);
});
