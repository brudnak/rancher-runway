import test from 'node:test';
import assert from 'node:assert/strict';
import { fieldsFor, initialConfig } from './helmlab.mjs';
import { createEditHistory, editGroup, planFindings, randomBootstrapPassword } from './helm-workbench.mjs';
const fields = fieldsFor({ values: {replicas:3, tls:'ingress', ingress:{enabled:true,tls:{source:'rancher',secretName:'tls-rancher-ingress'}},letsEncrypt:{email:''}} });
test('plan checks guide hostname, TLS prerequisites and environment fixes', () => {
 const config=initialConfig();
 assert.ok(planFindings(config,fields,{},[]).some(f=>f.id==='hostname'&&f.level==='error'));
 const ready=planFindings(config,fields,{hostname:'rancher.example.com'},[]);
 assert.equal(ready.filter(f=>f.level==='error').length,0);
 assert.ok(ready.some(f=>f.id==='certs'));
 assert.ok(planFindings(config,fields,{hostname:'https://rancher.local/path'},[]).some(f=>f.id==='hostname'));
 assert.ok(planFindings(config,fields,{'ingress.tls.source':'letsEncrypt'},[]).some(f=>f.id==='email'));
 assert.ok(!planFindings(config,fields,{'ingress.enabled':'false','ingress.tls.source':'letsEncrypt'},[]).some(f=>f.id==='email'||f.id==='certs'));
 assert.ok(planFindings(config,fields,{},[{name:'X',value:'1'},{name:'X',value:'2'}]).some(f=>f.id==='env-duplicate'));
 assert.ok(planFindings(config,fields,{},[{name:'',value:'x'}]).some(f=>f.id==='env'));
 assert.ok(!planFindings({...config,action:'upgrade'},fields,{},[]).some(f=>f.id==='hostname'));
});
test('history coalesces typing, keeps actions atomic, and branches after undo', () => {
 const history=createEditHistory({hostname:''});
 history.record({hostname:'a'},'hostname',1000);
 history.record({hostname:'ab'},'hostname',1100);
 history.record({hostname:'abc'},'hostname',1200);
 assert.deepEqual(history.undo(),{hostname:''});
 assert.deepEqual(history.redo(),{hostname:'abc'});
 history.record({hostname:'demo',replicas:1},'',2000);
 assert.deepEqual(history.undo(),{hostname:'abc'});
 history.record({hostname:'other'},'hostname',2100);
 assert.equal(history.canRedo,false);
 assert.deepEqual(history.undo(),{hostname:'abc'});
});
test('history has bounded independent snapshots and groups individual fields', () => {
 const draft={config:{release:'rancher'},overrides:{},env:[]};
 const history=createEditHistory(draft,3);
 for(let i=1;i<=4;i++)history.record({...draft,overrides:{replicas:String(i)}},'',i*1000);
 assert.equal(history.undo().overrides.replicas,'3');
 const copy=history.undo();copy.config.release='mutated';
 assert.equal(history.canUndo,false);
 assert.equal(history.redo().config.release,'rancher');
 assert.equal(editGroup(draft,{...draft,overrides:{hostname:'x'}}),'overrides.hostname');
 assert.equal(editGroup(draft,{...draft,overrides:{hostname:'x',replicas:'1'}}),'');
});
test('generated credentials use 24 bytes from the cryptographic provider', () => {
 let requested=0;
 const password=randomBootstrapPassword({getRandomValues(bytes){requested=bytes.length;for(let i=0;i<bytes.length;i++)bytes[i]=i;return bytes;}});
 assert.equal(requested,24);
 assert.equal(password,'000102030405060708090a0b0c0d0e0f1011121314151617');
});
