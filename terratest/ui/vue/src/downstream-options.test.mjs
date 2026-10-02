import test from 'node:test';
import assert from 'node:assert/strict';
import {downstreamVersionChoices, compareDownstreamVersions, generateDownstreamName, linodeMachineDefaults} from './downstream-options.mjs';
test('normal view keeps newest patch per minor with numeric distro builds and selectable older versions', () => {
 const versions=['v1.35.0+k3s1','v1.35.0+k3s3','v1.36.4+k3s1','v1.36.5+k3s1','v1.37.0-rc1+k3s1'];
 assert.deepEqual(downstreamVersionChoices(versions),['v1.37.0-rc1+k3s1','v1.36.5+k3s1','v1.35.0+k3s3']);
 assert.equal(downstreamVersionChoices(versions,true).length,5);
 assert.ok(downstreamVersionChoices(versions,false,'v1.36.4+k3s1').includes('v1.36.4+k3s1'));
 assert.ok(compareDownstreamVersions('v1.35.0+k3s10','v1.35.0+k3s3')>0);
 assert.ok(compareDownstreamVersions('v1.36.0+rke2r1','v1.36.0-rc2+rke2r1')>0);
});
test('generated cluster names use sanitized initials and exactly six random hex characters', () => {
 const random={getRandomValues: bytes => {bytes.set([0,15,255]);return bytes;}};
 assert.equal(generateDownstreamName('ATB',random),'atb-000fff');
 assert.equal(generateDownstreamName(' 3A_TB-- ',random),'atb-000fff');
 assert.equal(generateDownstreamName('a'.repeat(50),random).length,40);
 assert.throws(()=>generateDownstreamName('123---',random),/prefix/);
});
test('Linode defaults follow dashboard preferences only when catalog choices exist', () => {
 assert.deepEqual(linodeMachineDefaults({regions:[{id:'us-east'},{id:'us-west'}],types:[{id:'g6-standard-2'}],images:[{id:'linode/ubuntu20.04'}]}),{region:'us-west',instanceType:'g6-standard-2',image:'linode/ubuntu20.04'});
 assert.deepEqual(linodeMachineDefaults({regions:[{id:'eu-west'}],types:[{id:'small',memoryMB:1024},{id:'medium',memoryMB:4096}],images:[{id:'old',deprecated:true},{id:'available'}]}),{region:'eu-west',instanceType:'medium',image:'available'});
});
