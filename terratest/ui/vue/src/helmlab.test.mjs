import test from 'node:test';
import assert from 'node:assert/strict';
import { parse } from 'yaml';
import { buildOutput, fieldsFor, initialOverrides, quote } from './helmlab.mjs';
const config = { repo: 'prime', release: 'rancher', namespace: 'cattle-system', action: 'upgrade', delivery: 'set' };
const channel = { repo: 'https://example.com/charts', devel: true };
const fields = fieldsFor({ values: { bootstrapPassword: '', image: { pullPolicy: 'IfNotPresent' }, replicas: 3, hostname: '', agentTLSMode: '', annotations: {}, tolerations: [] } });
const build = (overrides = initialOverrides(), env = [], changes = {}) => buildOutput({ ...config, ...changes }, channel, '2.16.0-head', fields, overrides, env);
test('linked defaults produce a pinned Prime head upgrade', () => {
 const result = build();
 assert.match(result.command, /--reset-then-reuse-values/);
 assert.match(result.command, /--version '2.16.0-head'/);
 assert.match(result.command, /--devel/);
 assert.match(result.command, /--set 'replicas=1'/);
 assert.match(result.command, /--set-string 'bootstrapPassword=admin'/);
 assert.doesNotMatch(result.command, /--values/);
});
test('file mode preserves string types, nested values, lists and environment', () => {
 const result = build({ ...initialOverrides(), annotations: 'example.com/key: "123"', tolerations: '- key: dedicated' }, [{ name: 'PORT', value: '8080' }], { delivery: 'file' });
 const values = parse(result.yaml);
 assert.equal(values.replicas, 1);
 assert.equal(values.image.pullPolicy, 'Always');
 assert.equal(values.annotations['example.com/key'], '123');
 assert.equal(values.extraEnv[0].value, '8080');
 assert.equal(values.tolerations[0].key, 'dedicated');
 assert.match(result.command, /--values values.yaml/);
 assert.doesNotMatch(result.command, /--set/);
});
test('flag mode keeps structured values in the companion file', () => {
 const result = build({ hostname: 'rancher.local', annotations: 'key: value' });
 assert.deepEqual(parse(result.yaml), { annotations: { key: 'value' } });
 assert.match(result.command, /--set-string 'hostname=rancher.local'/);
 assert.match(result.command, /--values values.yaml/);
});
test('shell and Helm escaping protect literal strings', () => {
 const result = build({ bootstrapPassword: "a'b,$(echo bad){x}\\z" });
 assert.ok(result.command.includes("a'\\''b\\,$(echo bad)\\{x\\}\\\\z"));
 assert.equal(quote("x'y"), "'x'\\''y'");
});
test('invalid numbers, YAML and duplicate env names block output', () => {
 assert.throws(() => build({ replicas: '' }), /must be a number/);
 assert.throws(() => build({ annotations: '[' }), /annotations/);
 assert.throws(() => build({ annotations: 'scalar' }), /YAML map/);
 assert.throws(() => build({}, [{ name: 'X', value: 'a' }, { name: 'X', value: 'b' }]), /unique/);
});
test('install and reuse actions select appropriate flags', () => {
 assert.match(build({}, [], { action: 'install' }).command, /upgrade --install/);
 assert.match(build({}, [], { action: 'install' }).command, /--create-namespace/);
 assert.match(build({}, [], { action: 'upgrade-reuse' }).command, /--reuse-values/);
});
