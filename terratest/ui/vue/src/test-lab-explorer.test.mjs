import test from 'node:test';
import assert from 'node:assert/strict';
import {configTree,configTreeKey,configFilename} from './test-lab-explorer.mjs';
const library={folders:[{id:'b',name:'Staging'},{id:'a',name:'Empty'}],files:[{id:'2',name:'Config 10',folder:'b'},{id:'1',name:'Config 2',folder:'b'},{id:'root',name:'Unfiled',folder:''}]};
test('explorer nests naturally sorted files immediately below their folders',()=>{
 const rows=configTree(library,{a:true,b:true});
 assert.deepEqual(rows.map(r=>[r.key,r.level]),[['folder:a',1],['folder:b',1],['file:1',2],['file:2',2],['file:root',1]]);
 assert.equal(rows[0].visibleCount,0);assert.equal(rows[1].count,2);
 assert.deepEqual(configTree(library,{}).map(r=>r.key),['folder:a','folder:b','file:root']);
});
test('search reveals matches inside collapsed folders without mutating expansion',()=>{
 const expanded={b:false};const rows=configTree(library,expanded,'CONFIG 2');
 assert.deepEqual(rows.map(r=>r.key),['folder:b','file:1']);assert.equal(rows[0].open,true);assert.equal(expanded.b,false);
 assert.equal(configTree(library,expanded,'staging').length,3);
 assert.deepEqual(configTree(library,expanded,'absent'),[]);
});
test('keyboard navigation follows visible rows and folder ancestry',()=>{
 const rows=configTree(library,{b:true});
 assert.deepEqual(configTreeKey(rows,'folder:b','ArrowRight'),{focus:'file:1'});
 assert.deepEqual(configTreeKey(rows,'file:2','ArrowLeft'),{focus:'folder:b'});
 assert.deepEqual(configTreeKey(rows,'folder:b','ArrowLeft'),{collapse:'b'});
 assert.deepEqual(configTreeKey(configTree(library,{}),'folder:b','ArrowRight'),{expand:'b'});
 assert.deepEqual(configTreeKey(rows,'file:root','ArrowDown'),{focus:'file:root'});
 assert.deepEqual(configTreeKey(rows,'file:2','Home'),{focus:'folder:a'});
 assert.deepEqual(configTreeKey(rows,'folder:a','End'),{focus:'file:root'});
 assert.deepEqual(configTreeKey([],'none','ArrowDown'),{});
});
test('displayed filenames preserve existing YAML extensions',()=>{
 assert.equal(configFilename('Staging'),'Staging.yml');assert.equal(configFilename('cattle-config.yaml'),'cattle-config.yaml');
});
