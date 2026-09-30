import { ref } from 'vue';
import { setActivePanelTab, apiFetch } from './store.js';

export const testPackageIntent = ref(null);
export const testPackageSummaries = ref([]);
export function openTestPackage(packageId,sessionId='') {
  testPackageIntent.value={packageId,sessionId};
  setActivePanelTab('packages');
}
export function createTestPackageFromIssue(issue={}) {
  testPackageIntent.value={create:true,issue:{url:issue.url||issue.html_url||'',title:issue.title||'',body:issue.body||'',number:issue.number||0,repo:issue.repo||''}};
  setActivePanelTab('packages');
}
export function createClusterTestPackage(clusterId) {
  testPackageIntent.value={create:true,clusterId};
  setActivePanelTab('packages');
}

let refreshInFlight;
export async function refreshTestPackageSummaries() {
  if(refreshInFlight)return refreshInFlight;
  refreshInFlight=(async()=>{const response=await apiFetch('/api/test-packages');if(!response.ok)throw new Error(await response.text());const result=await response.json();testPackageSummaries.value=(result.packages||[]).map(pkg=>({id:pkg.id,title:pkg.title,status:pkg.status,issueUrl:pkg.issueUrl,fixUrl:pkg.fixUrl,fixTitle:pkg.fixTitle,updatedAt:pkg.updatedAt,sessions:pkg.sessions||[],cases:pkg.cases||[]}));return testPackageSummaries.value;})().finally(()=>{refreshInFlight=null;});
  return refreshInFlight;
}
