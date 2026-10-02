import { ref } from 'vue';
import { setActivePanelTab, apiFetch } from './store.js';

export const issuePackageIntent = ref(null);
export const issuePackageReturn = ref(null);
export const issuePackageSummaries = ref([]);
export const issuePackageMilestoneBuckets = ref([]);
export function openIssuePackage(packageId,sessionId='',release=null) {
  issuePackageReturn.value=release;
  issuePackageIntent.value={packageId,sessionId};
  setActivePanelTab('packages');
}
export function createIssuePackageFromIssue(issue={},release=null) {
  issuePackageReturn.value=release;
  issuePackageIntent.value={create:true,issue:{url:issue.url||issue.html_url||'',title:issue.title||'',body:issue.body||'',number:issue.number||0,repo:issue.repo||''}};
  setActivePanelTab('packages');
}
export function createClusterIssuePackage(clusterId) {
  issuePackageReturn.value=null;
  issuePackageIntent.value={create:true,clusterId};
  setActivePanelTab('packages');
}

let refreshInFlight;
export async function refreshIssuePackageSummaries() {
  if(refreshInFlight)return refreshInFlight;
  refreshInFlight=(async()=>{const response=await apiFetch('/api/issue-packages');if(!response.ok)throw new Error(await response.text());const result=await response.json();issuePackageMilestoneBuckets.value=(result.library?.buckets||[]).filter(bucket=>bucket.sourceMilestone);issuePackageSummaries.value=(result.packages||[]).map(pkg=>({id:pkg.id,title:pkg.title,status:pkg.status,issueUrl:pkg.issueUrl,fixUrl:pkg.fixUrl,fixTitle:pkg.fixTitle,updatedAt:pkg.updatedAt,sessions:pkg.sessions||[],cases:pkg.cases||[]}));return issuePackageSummaries.value;})().finally(()=>{refreshInFlight=null;});
  return refreshInFlight;
}
