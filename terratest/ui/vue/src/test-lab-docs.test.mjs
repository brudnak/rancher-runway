import test from 'node:test';
import assert from 'node:assert/strict';
import {filterReadmes,resolveReadme,readmeTree,localReadmeLink} from './test-lab-docs.mjs';
const sha='a'.repeat(40);
const docs=[
 {path:'validation/README.md',title:'Getting started',content:'Configuration for vai and provisioning'},
 {path:'validation/provisioning/README.md',title:'Provisioning clusters',content:'vai'},
 {path:'validation/vai/README.md',title:'Virtual API',content:'# Setup'},
 {path:'validation/vai/performance/readme.MD',title:'Performance',content:'# Setup'},
 {path:'validation/rbac/README.md',title:'Access control',content:'very important'}
];
test('documentation filtering matches paths and titles without matching the shared validation prefix or body',()=>{
 assert.deepEqual(filterReadmes(docs,'v').map(doc=>doc.path),['validation/provisioning/README.md','validation/vai/README.md','validation/vai/performance/readme.MD']);
 assert.deepEqual(filterReadmes(docs,'vai').map(doc=>doc.path),['validation/vai/README.md','validation/vai/performance/readme.MD']);
 assert.deepEqual(filterReadmes(docs,'VAI performance').map(doc=>doc.path),['validation/vai/performance/readme.MD']);
 assert.equal(filterReadmes(docs,'very important').length,0);
 assert.equal(filterReadmes(docs,'').length,docs.length);
});
test('README navigation finds case-insensitive paths and nearest parent guides instead of an unrelated fallback',()=>{
 assert.equal(resolveReadme(docs,'validation/vai/performance/README.md').doc.path,'validation/vai/performance/readme.MD');
 assert.equal(resolveReadme(docs,'validation/vai/performance').doc.path,'validation/vai/performance/readme.MD');
 const ancestor=resolveReadme(docs,'validation/vai/missing/README.md');
 assert.equal(ancestor.doc.path,'validation/vai/README.md');assert.match(ancestor.note,/closest parent guide/);
 const unavailable=resolveReadme(docs.filter(doc=>doc.path!=='validation/README.md'),'validation/missing/README.md');
 assert.equal(unavailable.doc,null);assert.match(unavailable.note,/No README is available/);
 assert.equal(resolveReadme(docs,'',['validation/vai/performance']).doc.path,'validation/vai/performance/readme.MD');
});
test('documentation tree nests matching files beneath their own folders with stable ancestry',()=>{
 const tree=readmeTree(filterReadmes(docs,'vai'));
 assert.deepEqual(tree.map(row=>[row.kind,row.path,row.depth]),[
  ['folder','vai',0],['folder','vai/performance',1],['document','validation/vai/performance/readme.MD',2],['document','validation/vai/README.md',1]
 ]);
 assert.deepEqual(tree[2].parents,['vai','vai/performance']);
 assert.deepEqual(tree[3].parents,['vai']);
});
test('local README links and anchors stay on the pinned revision while source and external links remain external',()=>{
 const same=localReadmeLink('#setup',sha,'validation/vai/README.md',docs);
 assert.equal(same.path,'validation/vai/README.md');assert.equal(same.anchor,'setup');
 const child=localReadmeLink(`https://github.com/rancher/tests/blob/${sha}/validation/vai/performance/README.md#user-content-setup`,sha,docs[0].path,docs);
 assert.equal(child.path,'validation/vai/performance/readme.MD');assert.equal(child.anchor,'setup');
 const parent=localReadmeLink('../README.md',sha,'validation/vai/performance/readme.MD',docs);
 assert.equal(parent.resolution.doc.path,'validation/vai/README.md');
 const missing=localReadmeLink('../unknown/README.md',sha,'validation/vai/README.md',[]);
 assert.equal(missing.resolution.doc,null);
 for(const href of [`https://github.com/rancher/tests/blob/main/validation/vai/README.md`,`https://example.test/README.md`,`https://github.com/other/tests/blob/${sha}/validation/vai/README.md`,`https://github.com/rancher/tests/blob/${sha}/validation/vai/main_test.go`,'javascript:alert(1)','%E0%A4%A'])assert.equal(localReadmeLink(href,sha,'validation/vai/README.md',docs),null);
});
