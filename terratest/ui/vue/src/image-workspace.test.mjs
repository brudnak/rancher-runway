import test from 'node:test';
import assert from 'node:assert/strict';
import { digestReference, imageEvidenceState, buildPRImageBrief, readImageJSON } from './image-workspace.mjs';

const digest = `sha256:${'a'.repeat(64)}`;
test('digest pinning preserves repository and registry port while replacing mutable selectors', () => {
  for (const reference of ['registry.example:5000/team/image:head', `registry.example:5000/team/image@sha256:${'b'.repeat(64)}`, 'registry.example:5000/team/image']) {
    assert.equal(digestReference(reference, digest), `registry.example:5000/team/image@${digest}`);
  }
  assert.equal(digestReference('rancher/rancher:head', digest), `rancher/rancher@${digest}`);
  for (const reference of ['', 'bad input/repo', 'https://example/repo:head']) assert.equal(digestReference(reference, digest), '');
  assert.equal(digestReference('rancher/rancher:head', 'short-sha'), '');
});

test('ancestry distinguishes errors, absence, exact matches, descendants and unproven metadata', () => {
  assert.equal(imageEvidenceState({found:false}), 'unavailable');
  assert.equal(imageEvidenceState({found:false,error:'denied'}), 'error');
  assert.equal(imageEvidenceState({found:true,error:'comparison failed',match:{verdict:'included'}}), 'error');
  assert.equal(imageEvidenceState({found:true,match:{relation:'equal'}}), 'exact');
  assert.equal(imageEvidenceState({found:true,match:{relation:'candidate_is_descendant'}}), 'descendant');
  assert.equal(imageEvidenceState({found:true,match:{relation:'candidate_is_ancestor'}}), 'not-included');
  assert.equal(imageEvidenceState({found:true,match:{verdict:'unknown'}}), 'unknown');
});

test('evidence brief retains the entire observed matrix, commit, platform, failures and limits', () => {
  const observed = {reference:'docker.io/rancher/rancher:v2.14-head',digest,found:true,match:{relation:'equal',candidateRevision:'c'.repeat(40)}};
  const result = {tag:'v2.14-head',checkedAt:'2026-09-29T14:00:00Z',platform:'linux/amd64',pullRequest:{url:'https://github.com/rancher/rancher/pull/42',title:'[Unsafe](javascript:alert)\n# header',inclusionCommitSha:'c'.repeat(40),inclusionBasis:'merged_commit'},summary:{scanComplete:false},registries:[{registry:'docker.io',server:observed,agent:{found:false}},{registry:'registry.suse.com',server:{found:false,error:'Access denied'},agent:{found:false,error:'Access denied'}}],warnings:['Partial registry evidence']};
  const brief=buildPRImageBrief(result);
  assert.ok(brief.includes(`docker.io/rancher/rancher@${digest}`));
  assert.ok(brief.includes('Verification commit: '+ 'c'.repeat(40)));
  assert.equal((brief.match(/^\| (docker|registry)/gm)||[]).length,4);
  for(const text of ['Exact revision','Image not found','Check failed','Access denied','Partial registry evidence','Scan complete: Not confirmed','not a binary attestation','Mutable tags may have moved']) assert.ok(brief.includes(text),text);
  assert.doesNotMatch(brief,/\n# header|\[Unsafe\]\(/);
});

test('deadline covers a stalled response body and aborts the network request', async () => {
  const controller=new AbortController();
  await assert.rejects(readImageJSON(async ()=>({json:()=>new Promise(()=>{})}),controller,'Inspection',15),/Inspection did not respond/);
  assert.equal(controller.signal.aborted,true);
});

test('cancellation settles even when a transport ignores abort; late data cannot win the race', async () => {
  const controller=new AbortController(); let finish;
  const pending=readImageJSON(async ()=>({json:()=>new Promise(resolve=>{finish=resolve;})}),controller,'Inspection',5000);
  await Promise.resolve(); controller.abort();
  await assert.rejects(pending,{name:'AbortError'});
  finish({reference:'obsolete'});
  await assert.rejects(readImageJSON(async ()=>({json:async()=>({})}),controller,'Inspection',5000),{name:'AbortError'});
  const next=new AbortController();
  assert.deepEqual(await readImageJSON(async ()=>({json:async()=>({reference:'current'})}),next,'Inspection',15),{reference:'current'});
  await new Promise(resolve=>setTimeout(resolve,25));
  assert.equal(next.signal.aborted,false);
});
