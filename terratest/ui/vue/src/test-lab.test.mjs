import test from 'node:test';
import assert from 'node:assert/strict';
import { filterTests, testGroups, toggleTestSelection, selectTestSuites, testRepoURL, testSourceURL } from './test-lab.mjs';
const suite={id:'validation/configmaps::TestSuite',package:'validation/configmaps',suite:'TestSuite',test:'',file:'validation/configmaps/test.go',line:7};
const first={...suite,id:suite.id+'/TestFirst',test:'TestFirst'};
const second={...suite,id:suite.id+'/TestSecond',test:'TestSecond'};
test('whole suites replace individual selections and choosing a method replaces whole suite',()=>{
 assert.deepEqual(toggleTestSelection([first.id,second.id],suite,true),[suite.id]);
 assert.deepEqual(toggleTestSelection([suite.id],first,true),[first.id]);
 assert.deepEqual(toggleTestSelection([first.id,second.id],first,false),[second.id]);
});
test('search groups method matches under the correct suite and supports multiple terms',()=>{
 const filtered=filterTests([suite,first,second],'configmap first');assert.deepEqual(filtered,[first]);
 assert.equal(testGroups(filtered)[0].id,suite.id);assert.equal(testGroups([suite,first,second])[0].tests.length,2);
 assert.equal(filterTests([suite,first],'','rbac').length,0);
});
test('selecting visible suites preserves individual tests chosen outside the current search',()=>{
 const elsewhere='validation/projects::TestOther/TestKeep';
 assert.deepEqual(selectTestSuites([first.id,elsewhere],testGroups([suite,first])),[elsewhere,suite.id]);
});
test('file selection shows only runnable entries in that file while retaining suite identity',()=>{
 const otherFile={...second,file:'validation/configmaps/extra_test.go'};
 const entries=[suite,first,otherFile];
 assert.deepEqual(filterTests(entries,'','',otherFile.file),[otherFile]);
 assert.equal(testGroups(filterTests(entries,'','',otherFile.file))[0].id,suite.id);
 assert.deepEqual(filterTests(entries,'extra_test.go'),[otherFile]);
 assert.deepEqual(filterTests(entries,'first','',otherFile.file),[]);
 assert.deepEqual(filterTests(entries,'','','validation/configmaps/missing.go'),[]);
});
test('external links require validated repository names and pinned source revisions',()=>{
 assert.equal(testRepoURL('user/private-tests',true),'https://github.com/user/private-tests/settings');
 assert.equal(testRepoURL('evil/../../outside'), '');assert.equal(testRepoURL('https://evil.test'), '');
 assert.equal(testSourceURL('main',suite),'');assert.match(testSourceURL('a'.repeat(40),suite),/\/blob\/a{40}\/validation\/configmaps\/test.go#L7$/);
});
