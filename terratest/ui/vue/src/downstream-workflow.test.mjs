import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {computed, nextTick, ref, watch, effectScope, reactive} from 'vue';
import * as choices from './downstream-options.mjs';
import {machineValues, operationEvidence} from './rancher-operations.mjs';
import {rancherConnectionURL} from './rancher-connection.mjs';

const source = readFileSync(new URL('./RancherOperations.vue', import.meta.url), 'utf8');
function workflow(t, {machineFields = {region:{type:'string',default:'us-east-2',required:true}}, catalog = {regions:[{id:'us-east-2'}],types:[],images:[]}, credentials = [{id:'cloud-1'}], environment = false, signInFails = false, driverActive = true, existingRecords = [], submitFails = false, beforeSignIn = async () => {}, onSubmit = () => {}, beforeHistory = async () => {}} = {}) {
 const calls = [], cleanup = [], timers = new Map();
 let timerID = 0;
 const advancePoll = async () => {
  await nextTick();
  await new Promise(resolve=>setImmediate(resolve));
  const pending = [...timers.values()]; timers.clear();
  for (const callback of pending) await callback();
  await nextTick();
  await new Promise(resolve=>setImmediate(resolve));
 }; 
 const props = reactive({cluster:{id:'local',rancherUrl:'https://rancher.example',deploymentType:'ha-rke2'}});
 const bindings = {setTimeout:fn=>{timers.set(++timerID,fn);return timerID;},clearTimeout:id=>timers.delete(id),computed,nextTick,ref,watch,onBeforeUnmount:fn=>cleanup.push(fn),defineProps:()=>props,defineEmits:()=>()=>{}, ...choices,machineValues,operationEvidence,rancherConnectionURL,
  apiFetch:async (url, init) => {
   const input = init?.body ? JSON.parse(init.body) : {};
   calls.push({endpoint:url,...input});
   if (url === '/api/rancher/token') {
    await beforeSignIn();
    if(signInFails) throw new Error('Configured password unavailable');
    return {json:async()=>({token:'test-token'})};
   }
   if(input.action === 'downstream' && submitFails) throw new Error('Submission failed');
   if(input.action === 'downstream') onSubmit(input);
   if(!init?.body) await beforeHistory();
   const results = {
    'driver-status':{active:driverActive},
    'provider-options':catalog,
    options:{versions:['v1.35.1+rke2r1'],defaultVersion:'v1.35.1+rke2r1',namePrefix:'qa',credentials,fields:machineFields},
    environment:{available:environment},downstream:{},
   };
   return {json:async()=>results[input.action] || {records:structuredClone(existingRecords)}};
  },readJSON:async fn=>(await fn()).json()};
 const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;\n/gm,'');
 const scope = effectScope();
 const model = scope.run(()=>new Function(...Object.keys(bindings),`${script}\nreturn {running,refreshHistory,openWorkflow,quickCreate,toggleDownstreamOptions,createCluster,quickCreating,open,tab,quantity,machine,fetchProviderChoices,additionalDetails,canCreate,name,provider,distro,token,options,error,initialized};`)(...Object.values(bindings)));
 t.after(()=>{cleanup.forEach(fn=>fn());scope.stop();});
 return {model,calls,props,timers,advancePoll};
}
test('management deployment defaults follow AWS RKE2, AWS K3s and Linode Docker',()=>{
 assert.deepEqual(choices.downstreamLocalDefaults({deploymentType:'ha-rke2'}),{provider:'amazonec2',distribution:'rke2'});
 assert.deepEqual(choices.downstreamLocalDefaults({deploymentType:'hosted-tenant-k3s'}),{provider:'amazonec2',distribution:'k3s'});
 assert.deepEqual(choices.downstreamLocalDefaults({deploymentType:'linode-docker-cattle'}),{provider:'linode',distribution:'rke2'});
});
test('opening preloads defaults without provisioning; one Create submits those settings',async t=>{
 const {model,calls}=workflow(t);
 await model.openWorkflow('downstream');
 assert.equal(model.canCreate.value,true);
 assert.equal(model.additionalDetails.value,false);
 assert.match(model.name.value,/^qa-[a-f0-9]{6}$/);
 assert.equal(calls.some(call=>call.action==='downstream'),false);
 await model.openWorkflow('downstream');
 assert.equal(calls.filter(call=>call.endpoint==='/api/rancher/token').length,1);
 await model.createCluster();
 const submit=calls.find(call=>call.action==='downstream');
 assert.equal(submit.confirmed,true);
 assert.equal(submit.credentialId,'cloud-1');
 assert.deepEqual(submit.machine,{region:'us-east-2'});
 assert.equal(model.token.value,'');
 assert.equal(model.initialized.value,false);
});
test('missing credentials expand settings and prevent creation',async t=>{
 const {model,calls}=workflow(t,{credentials:[]});
 await model.openWorkflow('downstream');
 assert.equal(model.additionalDetails.value,true);
 assert.equal(model.canCreate.value,false);
 await model.createCluster();
 assert.equal(calls.some(call=>call.action==='downstream'),false);
});
test('available environment credentials are selected without exposing secrets',async t=>{
 const {model,calls}=workflow(t,{credentials:[],environment:true});
 await model.openWorkflow('downstream');
 await model.createCluster();
 const submit=calls.find(call=>call.action==='downstream');
 assert.equal(submit.useEnvironment,true);
 assert.deepEqual(submit.credentials,{});
});
test('failed automatic sign-in exposes the manual recovery form',async t=>{
 const {model}=workflow(t,{signInFails:true});
 await model.openWorkflow('downstream');
 assert.equal(model.additionalDetails.value,true);
 assert.match(model.error.value,/Configured password unavailable/);
 assert.equal(model.canCreate.value,false);
});

test('quick creation signs in, generates a name, loads defaults and submits with one action',async t=>{
 const {model,calls}=workflow(t);
 await model.quickCreate();
 const signIn=calls.find(call=>call.endpoint==='/api/rancher/token');
 assert.equal(signIn.useBootstrapPassword,true);
 assert.equal(signIn.username,'admin');
 const submissions=calls.filter(call=>call.action==='downstream');
 assert.equal(submissions.length,1);
 assert.match(submissions[0].name,/^qa-[a-f0-9]{6}$/);
 assert.equal(submissions[0].quantity,1);
 assert.equal(submissions[0].provider,'linode');
 assert.equal(submissions[0].confirmed,true);
 assert.equal(model.tab.value,'history');
 assert.equal(model.quickCreating.value,false);
});
test('double clicking quick create submits only once',async t=>{
 const {model,calls}=workflow(t);
 await Promise.all([model.quickCreate(),model.quickCreate()]);
 assert.equal(calls.filter(call=>call.action==='downstream').length,1);
});
test('quick creation enables an inactive driver before loading options',async t=>{
 const {model,calls}=workflow(t,{driverActive:false});
 await model.quickCreate();
 const actions=calls.map(call=>call.action);
 assert.ok(actions.indexOf('enable-driver') < actions.indexOf('options'));
 assert.equal(actions.filter(action=>action==='downstream').length,1);
});
test('customization never creates infrastructure or enables drivers',async t=>{
 const {model,calls}=workflow(t,{driverActive:false});
 await model.toggleDownstreamOptions();
 assert.equal(model.open.value,true);
 assert.equal(calls.some(call=>['downstream','enable-driver'].includes(call.action)),false);
});
test('quick creation resets customized name and node count to defaults',async t=>{
 const {model,calls}=workflow(t);
 await model.openWorkflow('downstream');
 model.name.value='custom-name'; model.quantity.value=5;
 await model.quickCreate();
 const submit=calls.find(call=>call.action==='downstream');
 assert.notEqual(submit.name,'custom-name');
 assert.equal(submit.quantity,1);
 assert.equal(calls.filter(call=>call.endpoint==='/api/rancher/token').length,1);
});
test('missing credentials reveal recovery without submitting or retrying',async t=>{
 const {model,calls}=workflow(t,{credentials:[]});
 await model.quickCreate();
 assert.equal(model.open.value,true);
 assert.equal(model.additionalDetails.value,true);
 assert.match(model.error.value,/cloud credential/);
 assert.equal(calls.some(call=>call.action==='downstream'),false);
});
test('an existing running operation prevents quick creation',async t=>{
 const {model,calls}=workflow(t,{existingRecords:[{id:'existing',status:'running'}]});
 await model.quickCreate();
 assert.equal(model.tab.value,'history');
 assert.equal(calls.some(call=>call.endpoint==='/api/rancher/token'||call.action==='downstream'),false);
});
test('changing clusters during sign-in prevents downstream submission',async t=>{
 let release;
 const waiting=new Promise(resolve=>{release=resolve;});
 const {model,calls,props}=workflow(t,{beforeSignIn:()=>waiting});
 const pending=model.quickCreate();
 while (!calls.some(call=>call.endpoint==='/api/rancher/token')) await new Promise(resolve=>setImmediate(resolve));
 props.cluster.id='other';
 await nextTick(); release(); await pending;
 assert.equal(calls.some(call=>call.action==='downstream'),false);
 assert.equal(model.token.value,'');
});
test('submission failures are shown without automatic resubmission',async t=>{
 const {model,calls}=workflow(t,{submitFails:true});
 await model.quickCreate();
 assert.equal(calls.filter(call=>call.action==='downstream').length,1);
 assert.equal(model.open.value,true);
 assert.match(model.error.value,/Submission failed/);
});

const retiredImageFields = {
 region:{type:'string',default:'us-east',required:true},
 instanceType:{type:'string',default:'g6-standard-4',required:true},
 image:{type:'string',default:'linode/ubuntu18.04',required:true},
};
const currentLinodeCatalog = {
 regions:[{id:'us-east'},{id:'us-west'}],
 types:[{id:'g6-standard-2'},{id:'g6-standard-4'}],
 images:[{id:'linode/almalinux9'},{id:'linode/ubuntu22.04'}],
};
test('quick creation replaces the retired Ubuntu 18.04 schema default with a current image',async t=>{
 const {model,calls}=workflow(t,{credentials:[],environment:true,machineFields:retiredImageFields,catalog:currentLinodeCatalog});
 await model.quickCreate();
 const submitted=calls.find(call=>call.action==='downstream');
 assert.ok(submitted,model.error.value);
 assert.deepEqual(submitted.machine,{region:'us-east',instanceType:'g6-standard-4',image:'linode/ubuntu22.04'});
 assert.equal(submitted.useEnvironment,true);
});
test('manual catalog refresh preserves an explicit image selection even if unavailable',async t=>{
 const {model}=workflow(t,{machineFields:retiredImageFields,catalog:currentLinodeCatalog});
 await model.quickCreate();
 model.provider.value='linode';
 await nextTick();
 model.options.value={fields:retiredImageFields,credentials:[]};
 model.machine.value={region:'us-east',image:'linode/custom-image'};
 await model.fetchProviderChoices();
 assert.equal(model.machine.value.image,'linode/custom-image');
});
test('an empty image catalog blocks creation and identifies OS image as unavailable',async t=>{
 const {model,calls}=workflow(t,{machineFields:retiredImageFields,catalog:{...currentLinodeCatalog,images:[]}});
 await model.quickCreate();
 assert.equal(calls.some(call=>call.action==='downstream'),false);
 assert.match(model.error.value,/OS image is unavailable/);
});

test('closing the panel during creation keeps polling, unlocks on completion and allows another unique cluster',async t=>{
 const records=[];
 const {model,calls,timers,advancePoll}=workflow(t,{existingRecords:records,onSubmit:()=>records.unshift({id:`op-${records.length}`,kind:'downstream',status:'running'})});
 await model.quickCreate();
 assert.equal(model.running.value,true);
 model.open.value=false;
 await advancePoll();
 assert.ok(timers.size > 0,'closed workflows must keep checking active operations');
 records[0]={...records[0],status:'succeeded',finished:'2026-10-05T18:00:00Z'};
 await advancePoll();
 assert.equal(model.running.value,false);
 assert.equal(timers.size,0,'stop background polling after closed operation finishes');
 await model.quickCreate();
 const submissions=calls.filter(call=>call.action==='downstream');
 assert.equal(submissions.length,2);
 assert.notEqual(submissions[0].name,submissions[1].name);
});
test('failed operations also unlock the closed workflow, with no automatic retry',async t=>{
 const records=[];
 const {model,calls,advancePoll}=workflow(t,{existingRecords:records,onSubmit:()=>records.push({id:'failure',status:'running'})});
 await model.quickCreate();
 model.open.value=false;
 await advancePoll();
 records[0]={id:'failure',status:'failed',finished:'2026-10-05T18:00:00Z'};
 await advancePoll();
 assert.equal(model.running.value,false);
 assert.equal(calls.filter(call=>call.action==='downstream').length,1);
});
test('a temporary history failure retains the lock and continues polling while closed',async t=>{
 let failHistory=false;
 const records=[];
 const {model,timers,advancePoll}=workflow(t,{existingRecords:records,onSubmit:()=>records.push({id:'op',status:'running'}),beforeHistory:async()=>{if(failHistory) throw new Error('Offline');}});
 await model.quickCreate();
 model.open.value=false;
 failHistory=true;
 await advancePoll();
 assert.equal(model.running.value,true);
 assert.ok(timers.size > 0);
 failHistory=false;
 records[0]={id:'op',status:'succeeded',finished:'2026-10-05T18:00:00Z'};
 await advancePoll();
 assert.equal(model.running.value,false);
});
test('custom creation clears the submitted name before preparing the next cluster',async t=>{
 const {model}=workflow(t);
 await model.openWorkflow('downstream');
 const first=model.name.value;
 await model.createCluster();
 assert.equal(model.name.value,'');
 await model.openWorkflow('downstream');
 assert.notEqual(model.name.value,first);
 assert.match(model.name.value,/^qa-[a-f0-9]{6}$/);
});
