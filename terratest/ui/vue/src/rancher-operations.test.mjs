import test from 'node:test';
import assert from 'node:assert/strict';
import { upgradeCandidate, machineValues, operationEvidence } from './rancher-operations.mjs';
test('upgrade candidates include patch and next-minor heads but reject skipped minors', () => {
  assert.equal(upgradeCandidate('v2.15.2', 'v2.15.3-head', true), true);
  assert.equal(upgradeCandidate('v2.15.2', 'v2.16.0-head', true), true);
  assert.equal(upgradeCandidate('v2.15.2', 'v2.17.0-head', true), false);
  assert.equal(upgradeCandidate('v2.15.2', 'v2.15.1', true), false);
  assert.equal(upgradeCandidate('v2.15.2', 'v2.15.2'), false);
});
test('machine fields preserve typed values and reject malformed complex input', () => {
  const fields = { count: {type:'int'}, flag:{type:'boolean'}, labels:{type:'map[string]'}, empty:{type:'string'} };
  assert.deepEqual(machineValues(fields, {count:'3', flag:false, labels:'{"owner":"qa"}', empty:''}), {count:3, flag:false, labels:{owner:'qa'}});
  assert.throws(() => machineValues(fields, {labels:'bad json'}), /valid JSON/);
  assert.throws(() => machineValues(fields, {count:'not a number'}), /number/);
});
test('issue evidence preserves exact versions, digests, failures and resource history', () => {
  const report = operationEvidence({id:'op-1',clusterId:'server',kind:'upgrade',status:'failed',started:'2026-10-01',from:'v2.15.2',to:'v2.16.0-head',resources:['machine x'],events:[{at:'now',message:'Readiness failed'}],plan:{chart:{version:'2.16.0-alpha1'},repository:'https://repo',image:'rancher/rancher',imageTag:'head',digest:'sha256:abc',agentImage:'rancher/rancher-agent:head',agentDigest:'sha256:def',experimental:true,checks:['Deployment ready'],warnings:['Experimental']}});
  for (const expected of ['v2.15.2','v2.16.0-head','sha256:abc','sha256:def','Readiness failed','machine x']) assert.ok(report.includes(expected));
});

test('exported evidence retains branch label separately from runtime version and immutable provenance', () => {
  const report = operationEvidence({ id: 'head-upgrade', kind: 'upgrade', events: [], plan: {
    chart: { version: '2.15.2' }, checks: [], warnings: [],
    targetVersionSource: 'CATTLE_SERVER_VERSION in image configuration', targetVersionLabel: 'release-v2.15',
    image: 'docker.io/rancher/rancher', imageTag: 'v2.15-head', digest: 'sha256:server',
    imageRevision: '19c92983f6f9d7f455de668e62fbfe55c045cde2',
    imageCanonicalReference: 'rancher/rancher:v2.15-19c92983f6f9d7f455de668e62fbfe55c045cde2-head',
  } });
  for (const value of ['release-v2.15', 'CATTLE_SERVER_VERSION', 'sha256:server', '19c92983f6f9d7f455de668e62fbfe55c045cde2', 'docker.io/rancher/rancher:v2.15-head']) assert.ok(report.includes(value));
});
