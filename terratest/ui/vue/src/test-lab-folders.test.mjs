import test from 'node:test';
import assert from 'node:assert/strict';
import {testFolders,filterTestFolders,folderContainsPackage,testExplorer,filterTestExplorer} from './test-lab-folders.mjs';
const entries=[{package:'validation/steve/vai',suite:'TestVai'},{package:'validation/steve/vai',suite:'TestVai',test:'TestFirst'},{package:'validation/steve/counts',suite:'TestCounts'},{package:'validation/v2prov',suite:'TestV2'},{package:'validation/auth',suite:'TestAuth'}];
test('folder tree includes nested paths and counts suites once',()=>{
 const folders=testFolders(entries);assert.equal(folders.find(f=>f.path==='steve').count,2);assert.equal(folders.find(f=>f.path==='steve/vai').count,1);
 assert.deepEqual(filterTestFolders(folders).map(f=>f.path),['auth','steve','v2prov']);
 assert.deepEqual(filterTestFolders(folders,'',new Set(['steve'])).map(f=>f.path),['auth','steve','steve/counts','steve/vai','v2prov']);
});
test('live prefix search finds nested vai without matching the validation root',()=>{
 const folders=testFolders(entries);
 assert.deepEqual(filterTestFolders(folders,'v').map(f=>f.path),['steve/vai','v2prov']);
 assert.deepEqual(filterTestFolders(folders,'VAi').map(f=>f.path),['steve/vai']);
 assert.deepEqual(filterTestFolders(folders,'ste vai').map(f=>f.path),['steve/vai']);
 assert.deepEqual(filterTestFolders(folders,'missing'),[]);
});
test('folder selection includes descendants with a path boundary',()=>{
 assert.equal(folderContainsPackage('steve','validation/steve/vai'),true);
 assert.equal(folderContainsPackage('steve/vai','validation/steve/vai'),true);
 assert.equal(folderContainsPackage('steve/vai','validation/steve/vai2'),false);
 assert.equal(folderContainsPackage('','validation/auth'),true);
});

const catalog=[
 {package:'validation/steve/vai',suite:'TestVai',file:'validation/steve/vai/vai_test.go'},
 {package:'validation/steve/vai',suite:'TestVai',test:'TestEnabled',file:'validation/steve/vai/vai_test.go'},
 {package:'validation/steve/vai',suite:'TestVai',test:'TestDisabled',file:'validation/steve/vai/disabled_test.go'},
 {package:'validation/steve/vai',suite:'TestProjects',file:'validation/steve/vai/projects_test.go'},
 {package:'validation/steve/counts',suite:'TestCounts',file:'validation/steve/counts/counts_test.go'},
 {package:'validation/v2prov',suite:'TestV2',file:'validation/v2prov/provision_test.go'},
 {package:'validation',suite:'TestRoot',file:'validation/root_test.go'},
];
const documents=[
 {path:'validation/README.md',title:'Validation'},
 {path:'validation/steve/vai/README.md',title:'Vai'},
 {path:'validation/steve/README.md',title:'Steve'},
 {path:'validation/guides/usage/README.md',title:'Usage'},
];

test('explorer nests README and discovered test files with folders before files',()=>{
 const nodes=testExplorer(catalog,documents),byPath=path=>nodes.find(node=>node.path===path);
 assert.deepEqual(nodes.map(node=>node.path),[
  'validation/guides','validation/guides/usage','validation/guides/usage/README.md',
  'validation/steve','validation/steve/counts','validation/steve/counts/counts_test.go',
  'validation/steve/vai','validation/steve/vai/disabled_test.go','validation/steve/vai/projects_test.go','validation/steve/vai/README.md','validation/steve/vai/vai_test.go',
  'validation/steve/README.md','validation/v2prov','validation/v2prov/provision_test.go','validation/README.md','validation/root_test.go',
 ]);
 assert.equal(byPath('validation/steve').count,3);
 assert.equal(byPath('validation/steve/vai').count,2,'method files do not increase the folder suite count');
 assert.equal(byPath('validation/steve/vai/vai_test.go').count,1);
 assert.equal(byPath('validation/steve/vai/vai_test.go').entries.length,2);
 assert.deepEqual(byPath('validation/steve/vai/disabled_test.go').suiteIDs,['validation/steve/vai::TestVai']);
 assert.equal(byPath('validation/guides').count,0,'documentation-only folders are present');
 assert.equal(byPath('validation/guides').hasChildren,true);
 assert.deepEqual(byPath('validation/steve/vai/README.md').parents,['validation/steve','validation/steve/vai']);
 assert.equal(byPath('validation/steve/vai/README.md').depth,2);
 assert.equal(byPath('validation/README.md').parent,'');
 assert.equal(byPath('validation/README.md').depth,0);
 assert.equal(byPath('validation/README.md').document.title,'Validation');
 assert.equal(byPath('validation/root_test.go').kind,'test-file');
 assert.equal(byPath('validation/steve/vai/README.md').key,'readme:validation/steve/vai/README.md');
});

test('explorer expansion reveals direct children and preserves collapsed descendants',()=>{
 const nodes=testExplorer(catalog,documents);
 assert.deepEqual(filterTestExplorer(nodes).map(node=>node.path),['validation/guides','validation/steve','validation/v2prov','validation/README.md','validation/root_test.go']);
 assert.deepEqual(filterTestExplorer(nodes,'',new Set(['validation/steve'])).filter(node=>node.path.startsWith('validation/steve')).map(node=>node.path),['validation/steve','validation/steve/counts','validation/steve/vai','validation/steve/README.md']);
 assert.equal(filterTestExplorer(nodes,'',new Set(['validation/steve/vai'])).some(node=>node.path==='validation/steve/vai/README.md'),false,'a collapsed ancestor still hides nested files');
});

test('explorer search reveals ancestors, matching folders and their files',()=>{
 const nodes=testExplorer(catalog,documents),paths=query=>filterTestExplorer(nodes,query).map(node=>node.path);
 assert.deepEqual(paths('vai'),['validation/steve','validation/steve/vai','validation/steve/vai/disabled_test.go','validation/steve/vai/projects_test.go','validation/steve/vai/README.md','validation/steve/vai/vai_test.go']);
 assert.deepEqual(paths('STE DISABLED'),['validation/steve','validation/steve/vai','validation/steve/vai/disabled_test.go']);
 assert.deepEqual(paths('jects_test'),['validation/steve','validation/steve/vai','validation/steve/vai/projects_test.go']);
 assert.deepEqual(paths('validation'),[],'common validation root does not make every node match');
 assert.deepEqual(paths('unknown'),[]);
 assert.equal(paths('v').includes('validation/guides'),false,'folder search matches prefixes rather than arbitrary folder substrings');
});

test('README and test file filters retain only relevant ancestors',()=>{
 const nodes=testExplorer(catalog,documents),expanded=new Set(nodes.filter(node=>node.kind==='folder').map(node=>node.path));
 const readmes=filterTestExplorer(nodes,'',expanded,'readme');
 assert.equal(readmes.some(node=>node.kind==='test-file'),false);
 assert.equal(readmes.some(node=>node.path==='validation/steve/counts'),false);
 assert.equal(readmes.some(node=>node.path==='validation/guides/usage/README.md'),true);
 const tests=filterTestExplorer(nodes,'',expanded,'tests');
 assert.equal(tests.some(node=>node.kind==='readme'),false);
 assert.equal(tests.some(node=>node.path==='validation/guides'),false);
 assert.equal(tests.filter(node=>node.kind==='test-file').length,6);
 assert.deepEqual(filterTestExplorer(nodes,'vai',new Set(),'readmes').map(node=>node.path),['validation/steve','validation/steve/vai','validation/steve/vai/README.md']);
});

test('explorer ignores files outside validation and deduplicates identical file paths',()=>{
 const nodes=testExplorer([...catalog,...catalog,{package:'other',suite:'TestOther',file:'other/test.go'},{package:'validation',suite:'TestTraversal',file:'validation/../escape.go'}],[...documents,...documents,{path:'README.md'},{path:'validation/../README.md'}]);
 assert.equal(new Set(nodes.map(node=>node.key)).size,nodes.length);
 assert.equal(nodes.some(node=>!node.path.startsWith('validation/')),false);
 assert.equal(nodes.some(node=>node.path.includes('..')),false);
 assert.equal(nodes.find(node=>node.path==='validation/steve/vai').count,2);
});
