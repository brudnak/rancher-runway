import test from 'node:test';
import assert from 'node:assert/strict';
import {rancherConnectionURL,rancherTestHost,rancherConnectionFingerprint,filterRancherTargets,rancherCredentialAvailable} from './rancher-connection.mjs';
import {setRancherField,inspectConfig} from './test-lab-workspace.mjs';
test('connections normalize hosts without confusing credentials or path-prefixed servers',()=>{
 assert.equal(rancherConnectionURL(' Rancher.example.com:8443 '),'https://rancher.example.com:8443');
 assert.equal(rancherConnectionURL('https://rancher.example.com/tenant/'),'https://rancher.example.com/tenant');
 for(const url of ['https://user:password@example.com','file:///etc/passwd','https://example.com?token=secret','https://example.com#secret',''])assert.equal(rancherConnectionURL(url),'');
 assert.equal(rancherTestHost('https://rancher.example.com:8443/'),'rancher.example.com:8443');
 assert.equal(rancherTestHost('https://rancher.example.com/tenant'),'');
 assert.equal(rancherTestHost('http://localhost:8080'),'');
});
test('in-flight target identity includes TLS trust, not only the URL',()=>{
 const base=rancherConnectionFingerprint('https://rancher.example.com',false,'');
 assert.equal(base,rancherConnectionFingerprint('https://RANCHER.example.com/',false));
 assert.notEqual(base,rancherConnectionFingerprint('https://rancher.example.com',true));
 assert.notEqual(base,rancherConnectionFingerprint('https://rancher.example.com',false,'CA'));
});
test('discovery searches environment names, URLs, versions and runs',()=>{
 const targets=[{name:'Staging',url:'https://staging.test',version:'2.15',runId:'abcdef'}, {name:'Production',url:'https://prod.test'}];
 assert.deepEqual(filterRancherTargets(targets,' ABCDEF '),[targets[0]]);
 assert.deepEqual(filterRancherTargets(targets,'prod.test'),[targets[1]]);
 assert.equal(filterRancherTargets(targets,'missing').length,0);
});
test('generated tokens update only the working configuration connection',()=>{
 const source='# Keep this environment\nrancher:\n  host: rancher.example.com\n  adminToken: old-token # temporary\n  cleanup: false\nprovisioningInput:\n  nodePools: [workers]\n';
 const updated=setRancherField(source,'adminToken','new-token:secret');
 const result=inspectConfig(updated);
 assert.equal(result.value.rancher.adminToken,'new-token:secret');
 assert.equal(result.value.rancher.cleanup,false);
 assert.deepEqual(result.value.provisioningInput,{nodePools:['workers']});
 assert.match(updated,/# Keep this environment/);assert.match(updated,/# temporary/);
 assert.match(source,/old-token/);
});

test('password-first workspaces need a new token or a connected credential for the same server',()=>{
 const saved={kind:'rancher',url:'https://rancher.example.com',connected:true};
 assert.equal(rancherCredentialAvailable('',saved,saved),true);
 assert.equal(rancherCredentialAvailable('',{...saved,url:'https://other.example.com'},saved),false);
 assert.equal(rancherCredentialAvailable('',saved,{...saved,connected:false}),false);
 assert.equal(rancherCredentialAvailable('   ',saved,null),false);
 assert.equal(rancherCredentialAvailable('token:new',saved,null),true);
 assert.equal(rancherCredentialAvailable('',{...saved,url:''},saved),false);
});
