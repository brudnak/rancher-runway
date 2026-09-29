import test from 'node:test';
import assert from 'node:assert/strict';
import { cacheBytes, cacheCell, cacheObject, quoteTable, cacheCSV, cachePair, cacheView, cacheWorkspaceAfterRefresh } from './cache-lab.mjs';

test('polling an empty library never reselects the empty view or dismisses a connection draft',()=>{
 for(let poll=0;poll<5;poll++) {
  assert.equal(cacheWorkspaceAfterRefresh({workspaces:[]},''),null);
  assert.equal(cacheWorkspaceAfterRefresh({workspaces:[]},'',{formOpen:true}),null);
 }
});
test('refresh preserves drafts and current navigation, including a delayed initial load',()=>{
 const library={workspaces:[{id:'a'},{id:'b'}],active:'b'};
 assert.equal(cacheWorkspaceAfterRefresh(library,'',{formOpen:true}),null);
 assert.equal(cacheWorkspaceAfterRefresh(library,'a',{formOpen:true}),null);
 assert.equal(cacheWorkspaceAfterRefresh(library,'a'),null);
 assert.equal(cacheWorkspaceAfterRefresh(library,''),'b');
 assert.equal(cacheWorkspaceAfterRefresh({...library,active:'missing'},''),'a');
});
test('removed workspaces reconcile only outside a form; explicit Steve handoff still navigates',()=>{
 const library={workspaces:[{id:'steve'}]};
 assert.equal(cacheWorkspaceAfterRefresh(library,'removed',{formOpen:true}),null);
 assert.equal(cacheWorkspaceAfterRefresh(library,'removed'),'steve');
 assert.equal(cacheWorkspaceAfterRefresh({workspaces:[]},'removed'),'');
 assert.equal(cacheWorkspaceAfterRefresh(library,'',{formOpen:true,intent:'steve'}),'steve');
 assert.equal(cacheWorkspaceAfterRefresh(library,'',{formOpen:true,intent:'pending'}),null);
});
test('SQLite values preserve nulls, blobs, identifiers and literal JSON',()=>{
 assert.equal(cacheCell(null),'NULL');assert.equal(cacheCell({type:'blob',bytes:1024}),'BLOB · 1 KiB');assert.deepEqual(cacheObject('{"metadata":{"name":"sample"}}'),{metadata:{name:'sample'}});assert.equal(cacheObject('{"x":1} trailing'),'{"x":1} trailing');assert.equal(quoteTable('a"b;'),'"a""b;"');assert.equal(cacheBytes(2**30),'1.0 GiB');
});
test('CSV exports quote multiline data and neutralize spreadsheet formulas',()=>{
 const value=cacheCSV({columns:['name','data'],rows:[['=SUM(1)','a,"b\nc'],[null,12]]});assert.ok(value.includes('"\'=SUM(1)"'));assert.ok(value.includes('"a,""b\nc"'));assert.ok(value.endsWith('"NULL","12"'));
});
test('comparison pairs follow capture times and keep view defaults independent',()=>{
 const items=[{id:'b',createdAt:'2026-09-29T02:00:00Z'},{id:'a',createdAt:'2026-09-29T01:00:00Z'},{id:'c',createdAt:'2026-09-29T03:00:00Z'}];assert.deepEqual(cachePair(items,'b'),{baseline:'a',comparison:'b'});assert.deepEqual(cachePair([],''),{baseline:'',comparison:''});assert.equal(cacheView({mode:'sql',snapshot:'a'}).ignoreVolatile,false);
});

test('record inspector retains duplicate column names without modifying object prototypes',async()=>{
 const {cacheRecord}=await import('./cache-lab.mjs');const value=cacheRecord(['id','id','__proto__'],[1,2,'literal']);assert.equal(value.id,1);assert.equal(value['id (2)'],2);assert.equal(value.__proto__,'literal');assert.equal(Object.getPrototypeOf(value),null);
});
