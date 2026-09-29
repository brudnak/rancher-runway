import test from 'node:test';
import assert from 'node:assert/strict';
import { fieldPresentation, matchingFields, releaseIssues } from './helm-presentation.mjs';
import { buildOutput, initialConfig, releaseInputErrors } from './helmlab.mjs';

const fields = [
  {path:'hostname',group:'Access',description:'Public ingress host'},
  {path:'antiAffinity',group:'Workload',description:'Schedule across nodes'},
  {path:'auditLog.level',group:'Observability',description:'Audit level'},
];
test('setting search finds human labels, exact keys and chart documentation across categories', () => {
  const find = search => matchingFields(fields, {}, {search,section:'Access'}).map(field=>field.path);
  assert.deepEqual(find('pod placement'), ['antiAffinity']);
  assert.deepEqual(find('auditLog.level'), ['auditLog.level']);
  assert.deepEqual(find('across nodes'), ['antiAffinity']);
  assert.deepEqual(find('hostname nonexistent'), []);
});
test('override filter includes every category and keeps explicitly pinned defaults', () => {
  const overrides = {hostname:'',antiAffinity:'preferred'};
  assert.deepEqual(matchingFields(fields, overrides, {editedOnly:true,section:'Observability'}).map(field=>field.path), ['hostname','antiAffinity']);
  assert.deepEqual(matchingFields(fields, overrides, {editedOnly:true,search:'pod'}).map(field=>field.path), ['antiAffinity']);
  assert.deepEqual(matchingFields(fields, overrides, {section:'Observability'}).map(field=>field.path), ['auditLog.level']);
});
test('concise labels preserve original chart documentation for progressive disclosure', () => {
  const known = fieldPresentation(fields[0]);
  assert.equal(known.label, 'Rancher hostname');
  assert.equal(known.documentation, fields[0].description);
  const long = fieldPresentation({path:'custom.someSetting',description:'A'.repeat(200)});
  assert.equal(long.label, 'Custom · some Setting');
  assert.equal(long.hint, '');
  assert.equal(long.documentation.length,200);
});
test('review issues route to the correct controls and do not double count a bad field', () => {
  const issues = releaseIssues({release:'Bad name',timeout:'Bad timeout',repo:'Bad alias'}, {replicas:'Not an integer'}, [
    {id:'replicas',level:'error',field:'replicas',title:'Replicas',detail:'Invalid'},
    {id:'hostname',level:'error',field:'hostname',title:'Choose hostname',detail:'Required'},
    {id:'env',level:'error',section:'environment',title:'Duplicate variable',detail:'Unique names required'},
    {id:'cert',level:'note',title:'Install cert-manager'},
  ], ['removed.option']);
  assert.equal(issues.length,7);
  assert.equal(issues[0].section,'target');
  assert.equal(issues[1].section,'review');
  assert.equal(issues[2].configKey,'repo');
  assert.equal(issues.filter(item=>item.field==='replicas').length,1);
  assert.ok(issues.some(item=>item.section==='environment'));
  assert.equal(issues.at(-1).section,'unavailable');
  assert.ok(!issues.some(item=>item.id==='cert'));
});
test('inline release validation and export validation share the same rules', () => {
  const invalid = {release:'Bad_Name',namespace:'a'.repeat(64),repo:'bad alias',context:'cluster\nnext',action:'unknown',delivery:'unknown',wait:true,timeout:'forever'};
  const valid = initialConfig();
  assert.deepEqual(releaseInputErrors(valid),{});
  for (const [key,value] of Object.entries(invalid)) {
    if(key==='wait') continue;
    const config = {...valid,[key]:value,wait:key==='timeout'};
    assert.deepEqual(Object.keys(releaseInputErrors(config)),[key]);
    assert.throws(()=>buildOutput(config,{repo:'https://charts.example.com'},'2.15.2',[],{},[]), {message:releaseInputErrors(config)[key]});
  }
});
