import {folderContainsPackage} from './test-lab-folders.mjs';
export const emptyTestDraft = () => ({name:'', clusterId:'', ref:'main', sha:'', selection:[], tags:'validation,infra.any,cluster.any', timeout:30});
export function filterTests(entries=[], query='', category='', file='') {
 const words=String(query).toLowerCase().trim().split(/\s+/).filter(Boolean);
 return entries.filter(e=>(!file||e.file===file)&&folderContainsPackage(category,e.package)&&words.every(w=>`${e.package} ${e.file||''} ${e.suite} ${e.test} ${e.description||''} ${e.constraint||''}`.toLowerCase().includes(w)));
}
export function testGroups(entries=[]) {
 const groups=new Map();
 for(const entry of entries){const id=`${entry.package}::${entry.suite}`;if(!groups.has(id))groups.set(id,{id,package:entry.package,suite:entry.suite,entry:null,tests:[]});const group=groups.get(id);if(entry.test)group.tests.push(entry);else group.entry=entry;}
 return [...groups.values()];
}
export function toggleTestSelection(selection,entry,checked) {
 const suite=`${entry.package}::${entry.suite}`;
 const next=selection.filter(id=>id!==entry.id && !(checked && !entry.test && id.startsWith(`${suite}/`))); 
 if(checked){if(entry.test){const index=next.indexOf(suite);if(index>=0)next.splice(index,1);}next.push(entry.id);}
 return [...new Set(next)];
}
export function selectTestSuites(selection,groups) {
 return groups.reduce((next,group)=>toggleTestSelection(next,{id:group.id,package:group.package,suite:group.suite,test:''},true),selection);
}
export const testDuration = seconds => seconds<60?`${Math.round(seconds||0)}s`:`${Math.floor(seconds/60)}m ${Math.round(seconds%60)}s`;
export function testRunSummary(results=[]) {return {passed:results.filter(r=>r.status==='pass').length,failed:results.filter(r=>r.status==='fail').length,skipped:results.filter(r=>r.status==='skip').length,running:results.filter(r=>r.status==='run').length};}
export function testSourceURL(sha,entry) {return /^[a-f0-9]{40}$/.test(sha)?`https://github.com/rancher/tests/blob/${sha}/${entry.file}#L${entry.line}`:'';}
export function testRepoURL(repo,settings=false) {return /^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9_.-]{1,100}$/.test(repo)?`https://github.com/${repo}${settings?'/settings':''}`:'';}
