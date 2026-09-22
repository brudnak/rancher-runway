import test from 'node:test';
import assert from 'node:assert/strict';
import { parse } from 'yaml';
import { mkdtempSync, writeFileSync, readFileSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { buildOutput, fieldsFor, initialOverrides, quote, importValues, changedField, fieldErrors, buildSetupScript, filterVersions, codeTokens } from './helmlab.mjs';
const config = { repo: 'prime', release: 'rancher', namespace: 'cattle-system', action: 'upgrade', delivery: 'set' };
const channel = { repo: 'https://example.com/charts', devel: true };
const fields = fieldsFor({ values: { bootstrapPassword: '', image: { pullPolicy: 'IfNotPresent' }, replicas: 3, hostname: '', agentTLSMode: '', annotations: {}, tolerations: [] } });
const build = (overrides = initialOverrides(), env = [], changes = {}) => buildOutput({ ...config, ...changes }, channel, '2.16.0-head', fields, overrides, env);
test('local defaults produce a pinned Prime head upgrade', () => {
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

test('validation rejects invalid release metadata and noninteger counts', () => {
 assert.throws(() => build({}, [], { release: 'Bad_Name' }), /Release name/);
 assert.throws(() => build({}, [], { namespace: 'a'.repeat(64) }), /Namespace/);
 assert.throws(() => build({}, [], { action: 'unknown' }), /strategy/);
 assert.throws(() => build({ replicas: '1.5' }), /whole number/);
 assert.throws(() => build({ missingField: 'x' }), /not available/);
});
test('optional context, wait and dry-run flags preserve shell literals', () => {
 const result = build({}, [], { context: "team's cluster", wait: true, timeout: '1m30s', dryRun: true });
 assert.ok(result.command.includes("--kube-context 'team'\\''s cluster'"));
 assert.match(result.command, /--wait/);
 assert.match(result.command, /--timeout '1m30s'/);
 assert.match(result.command, /--dry-run/);
 assert.throws(() => build({}, [], { wait: true, timeout: 'forever' }), /timeout/);
});
test('complete YAML export includes flag values as well as file values', () => {
 const result = build({ hostname: 'demo.local', tolerations: '- key: dedicated' });
 assert.deepEqual(parse(result.yaml), { tolerations: [{ key: 'dedicated' }] });
 assert.deepEqual(parse(result.allYaml), { hostname: 'demo.local', tolerations: [{ key: 'dedicated' }] });
 assert.equal(result.needsFile, true);
 assert.equal(build().needsFile, false);
});
test('YAML import round-trips nested values, types, maps and environment pairs', () => {
 const first = build({ hostname: 'demo.local', replicas: '2', annotations: 'example.com/key: "true"' }, [{name:'PORT', value:'8080'}]);
 const imported = importValues(first.allYaml, fields);
 const second = build(imported.overrides, imported.env);
 assert.deepEqual(parse(second.allYaml), parse(first.allYaml));
 assert.throws(() => importValues('replicas: "3"', fields), /expected number/);
 assert.throws(() => importValues('unknown: yes', fields), /Unknown chart values/);
 assert.throws(() => importValues('extraEnv:\n- name: X\n  valueFrom: {}', fields), /name\/value/);
 assert.throws(() => importValues('__proto__:\n  polluted: true', fields), /Unsupported key/);
 assert.deepEqual(importValues('{}', fields), {overrides: {}, env: []});
});
test('documented maps stay intact and dotted map keys survive export', () => {
 const mapFields = fieldsFor({values:{annotations:{'example.com/key': 'default'}}, options:[{path:'annotations', default:{}}]});
 assert.deepEqual(mapFields.map(f => f.path), ['annotations']);
 const result = buildOutput(config, channel, '1.0.0', mapFields, {annotations:'example.com/key: custom'}, []);
 assert.equal(parse(result.yaml).annotations['example.com/key'], 'custom');
 const dottedFields = fieldsFor({values:{settings:{'example.com/key':'default'}}});
 const dottedResult = buildOutput(config, channel, '1.0.0', dottedFields, {'settings.example.com/key':'custom'}, []);
 assert.equal(parse(dottedResult.allYaml).settings['example.com/key'], 'custom');
 assert.ok(dottedResult.command.includes('settings.example\\.com/key=custom'));
});
test('unchanged typed values are omitted and field errors are available inline', () => {
 const field = fields.find(f => f.path === 'replicas');
 assert.equal(changedField(field, {replicas:'3.0'}), false);
 assert.equal(changedField(field, {replicas:'0'}), true);
 assert.match(fieldErrors(fields, {replicas:'1.5'}).replicas, /whole number/);
 const boolField = fieldsFor({values:{debug:false}})[0];
 assert.throws(() => buildOutput(config, channel, '1.0.0', [boolField], {debug:'maybe'}, []), /true or false/);
});

test('setup script runs with mocked Helm, preserves literal YAML and cleans up', () => {
 const dir = mkdtempSync(join(tmpdir(), 'helmlab-test-'));
 try {
  const capture = join(dir, 'capture');
  writeFileSync(join(dir, 'helm'), `#!/bin/sh
printf '%s\\n' "$@" >> "$HELM_LAB_CAPTURE"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--values" ]; then
    cp "$2" "$HELM_LAB_CAPTURE.values"
    printf '%s' "$2" > "$HELM_LAB_CAPTURE.path"
  fi
  shift
done
exit "\${HELM_LAB_MOCK_EXIT:-0}"
`, {mode:0o700});
  writeFileSync(join(dir, 'values.yaml'), 'do not overwrite');
  const output = build({hostname:'rancher.local', annotations:'note: "$(touch injected)"'}, [], { delivery:'file' });
  const script = buildSetupScript(output);
  writeFileSync(join(dir, 'setup.sh'), script);
  const result = spawnSync('/bin/sh', ['setup.sh'], {cwd:dir, env:{...process.env, PATH:dir+':'+process.env.PATH, HELM_LAB_CAPTURE:capture}, encoding:'utf8'});
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(parse(readFileSync(capture+'.values','utf8')), parse(output.yaml));
  assert.equal(readFileSync(join(dir,'values.yaml'),'utf8'), 'do not overwrite');
  assert.equal(existsSync(join(dir,'injected')), false);
  assert.equal(existsSync(readFileSync(capture+'.path','utf8')), false);
  assert.match(readFileSync(capture,'utf8'), /--reset-then-reuse-values/);
 } finally { rmSync(dir,{recursive:true,force:true}); }
});
test('setup script propagates Helm errors and avoids files for scalar-only plans', () => {
 const output = build();
 const script = buildSetupScript(output);
 assert.doesNotMatch(script, /mktemp|values_dir/);
 assert.ok(script.includes(output.command));
 const dir = mkdtempSync(join(tmpdir(),'helmlab-failure-'));
 try {
  writeFileSync(join(dir,'helm'),'#!/bin/sh\nexit 17\n',{mode:0o700});
  const result = spawnSync('/bin/sh',['-c',script],{env:{...process.env,PATH:dir+':'+process.env.PATH}});
  assert.equal(result.status,17);
 } finally { rmSync(dir,{recursive:true,force:true}); }
});
test('version search matches release and app versions; preview tokens preserve source', () => {
 const versions = [{version:'2.16.0-abcd-head',appVersion:'v2.16.0-head'}, {version:'2.15.1',appVersion:'v2.15.1'}];
 assert.deepEqual(filterVersions(versions,'2.16 HEAD'),[versions[0]]);
 assert.equal(filterVersions(versions,'no-match').length,0);
 for (const text of [build().command, buildSetupScript(build()), 'host: "<script>&test"\nreplicas: 3\n']) {
  assert.equal(codeTokens(text).map(token=>token.text).join(''),text);
  assert.equal(codeTokens(text,true).map(token=>token.text).join(''),text);
 }
});
