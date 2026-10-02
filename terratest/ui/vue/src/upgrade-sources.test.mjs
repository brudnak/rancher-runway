import test from 'node:test';
import assert from 'node:assert/strict';
import { discoverHeadSources, imageRegistry, imageCommitTag } from './upgrade-sources.mjs';

test('head discovery preserves distinct registries and survives partial failures', async () => {
  const sources = [
    { registry: 'docker.io', distribution: 'community', label: 'Community' },
    { registry: 'stgregistry.suse.com', distribution: 'prime', label: 'Staging' },
    { registry: 'registry.suse.com', distribution: 'prime', label: 'SUSE' },
  ];
  const results = await discoverHeadSources(sources, async (action, options) => {
    assert.equal(action, 'heads');
    assert.equal(options.distribution, options.registry === 'docker.io' ? 'community' : 'prime');
    if (options.registry === 'registry.suse.com') throw new Error('Registry unavailable');
    return { images: [`${options.registry}/rancher/rancher:head`] };
  });
  assert.deepEqual(results.flatMap(source => source.images), ['docker.io/rancher/rancher:head', 'stgregistry.suse.com/rancher/rancher:head']);
  assert.equal(results[2].error, 'Registry unavailable');
  assert.equal(results[2].label, 'SUSE');
  assert.equal(results[0].error, '');
});

test('head provenance identifies the source and only labels actual commit tags as commits', () => {
  const sha = '19c92983f6f9d7f455de668e62fbfe55c045cde2';
  assert.equal(imageRegistry(`stgregistry.suse.com/rancher/rancher:v2.15-${sha}-head`), 'stgregistry.suse.com');
  assert.equal(imageCommitTag(`docker.io/rancher/rancher:v2.15-${sha}-head`), sha);
  assert.equal(imageCommitTag('docker.io/rancher/rancher:v2.15-head'), '');
});
