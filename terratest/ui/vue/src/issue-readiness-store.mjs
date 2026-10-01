import {ref} from 'vue';
import {apiFetch} from './store.js';
import {refreshMyWork} from './my-work-store.mjs';
import {createIssueReadinessRequest,readReadinessStream} from './issue-readiness-request.mjs';
export const readinessDialogOpen=ref(false),readinessResult=ref({issueUrl:'',report:null,error:'',busy:false,progress:[]});
const requests=createIssueReadinessRequest(async(issueUrl,signal,onProgress,options)=>{
 const response=await apiFetch('/api/issue-readiness',{method:'POST',headers:{'Content-Type':'application/json',Accept:'application/x-ndjson'},body:JSON.stringify({issueUrl,expanded:!!options.expanded}),signal});
 if(!response.ok)throw new Error(await response.text());
 const report=response.headers.get('Content-Type')?.includes('application/x-ndjson')?await readReadinessStream(response,onProgress):await response.json();
 void refreshMyWork({force:true});return report;
},value=>{readinessResult.value=value});
export function openIssueReadiness(issueUrl,options={}){if(!issueUrl)return;readinessDialogOpen.value=true;requests.start(issueUrl,options);}
export function closeIssueReadiness(){requests.cancel();readinessDialogOpen.value=false;readinessResult.value={...readinessResult.value,busy:false};}
