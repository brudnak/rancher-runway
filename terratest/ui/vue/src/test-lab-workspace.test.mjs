import test from 'node:test';
import assert from 'node:assert/strict';
import {inspectConfig,setRancherField,indentSelection,safeDocURL,markdownInline,markdownBlocks,relatedReadmes} from './test-lab-workspace.mjs';
const sha='a'.repeat(40),path='validation/provisioning/README.md';
test('guided editing preserves comments and unrelated provider configuration',()=>{
 const raw='# shared template\nrancher:\n  host: old.test # connection\n  adminToken: token-secret\nprovider: # retain this\n  machine: large\n';
 const changed=setRancherField(raw,'host','new.test');
 assert.match(changed,/# shared template/);assert.match(changed,/# connection/);assert.match(changed,/# retain this/);assert.equal(inspectConfig(changed).value.provider.machine,'large');assert.match(raw,/old.test/);
});
test('invalid, duplicate, multi-document, alias-expanding, and oversized YAML fail without echoing values',()=>{
 for(const raw of ['[one,two]','secret: 1\nsecret: 2','secret: PRIVATE[\n  no: [\n','a: 1\n---\nb: 2','x: '+ 'a'.repeat(128*1024)]){const result=inspectConfig(raw);assert.equal(result.valid,false);assert.ok(!result.message.includes('PRIVATE'));}
 assert.equal(inspectConfig('rancher: {}\ntenantRanchers:\n  clients: []\n').valid,true);
});
test('editor indentation handles a selection ending on the next line without changing that line',()=>{
 const value=indentSelection('one\ntwo\nthree',0,8);assert.equal(value.text,'  one\n  two\nthree');assert.deepEqual(indentSelection(value.text,value.start,value.end,true),{text:'one\ntwo\nthree',start:0,end:8});
 assert.equal(indentSelection('x',1,1).text,'x  ');
});
test('README renderer escapes HTML and rejects script URLs without loading images',()=>{
 const rendered=markdownInline('<script>alert(1)</script> [bad](javascript:alert) ![track](https://example.test/pixel) `x < y` **safe**',sha,path);
 assert.ok(!rendered.includes('<script>'));assert.ok(!rendered.includes('href="javascript:'));assert.ok(!rendered.includes('<img'));assert.match(rendered,/<code>x &lt; y<\/code>/);assert.match(rendered,/<strong>safe<\/strong>/);
 for(const href of ['data:text/html,hello','javascript:alert(1)','file:///etc/passwd','https://user:pass@host.test','java\nscript:alert(1)'])assert.equal(safeDocURL(href,sha,path),'');
 assert.equal(safeDocURL('rke2/README.md',sha,path),`https://github.com/rancher/tests/blob/${sha}/validation/provisioning/rke2/README.md`);
});
test('README blocks retain exact config examples and support tables, headings, lists and quotes',()=>{
 const blocks=markdownBlocks('# Guide\n\n```yaml\na:\n  b: true # keep\n```\n\n| Field | Type |\n| --- | --- |\n| host | string |\n\n1. One\n2. Two\n\n> Note');
 assert.equal(blocks[1].text,'a:\n  b: true # keep');assert.equal(blocks[2].type,'table');assert.equal(blocks[3].ordered,true);assert.equal(blocks[4].type,'quote');
 const docs=[{path:'validation/README.md'},{path:'validation/provisioning/README.md'},{path:'validation/other/README.md'}];assert.equal(relatedReadmes(docs,['validation/provisioning/rke2']).length,2);
});
