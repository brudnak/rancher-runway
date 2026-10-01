import {wakeDailyReadiness} from './daily-readiness-store.mjs';
import {ref} from 'vue';
import {apiFetch,activeTab} from './store.js';
import {refreshTestPackageSummaries} from './test-packages-store.mjs';
export const myWorkSnapshot=ref(null),myWorkLoading=ref(false),myWorkRefreshing=ref(false),myWorkError=ref('');
let inFlight,lastRefresh=0,generation=0;
async function read(response){if(!response.ok)throw new Error(await response.text());return response.json();}
function accept(data){myWorkSnapshot.value=data.snapshot?.generatedAt&&data.snapshot?.milestone?.number?data.snapshot:null;}
export async function refreshMyWork({force=false}={}){
 if(myWorkLoading.value)return;
 if(inFlight)return inFlight;
 const ticket=generation;
 inFlight=(async()=>{try{
  const saved=await read(await apiFetch('/api/my-work'));if(ticket!==generation)return;accept(saved);
  if(myWorkSnapshot.value&&(force||Date.now()-lastRefresh>60000)){
   myWorkRefreshing.value=true;
   const data=await read(await apiFetch('/api/my-work/refresh',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'}));
   if(ticket!==generation)return;accept(data);lastRefresh=Date.now();
  }
  await refreshTestPackageSummaries();if(ticket===generation)myWorkError.value='';
 }catch(err){if(ticket===generation)myWorkError.value=`Could not refresh My Work from GitHub. Showing the saved snapshot. ${err.message}`;}
 finally{myWorkRefreshing.value=false;inFlight=null;}})();return inFlight;
}
export async function pullMyWork(config){if(myWorkLoading.value)return;generation++;myWorkLoading.value=true;myWorkError.value='';try{const data=await read(await apiFetch('/api/my-work',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(config)}));accept(data);lastRefresh=Date.now();await refreshTestPackageSummaries();void wakeDailyReadiness();return data.snapshot;}catch(err){myWorkError.value=err.message;throw err;}finally{myWorkLoading.value=false;}}
function wake(){if(!document.hidden&&['home','my-work'].includes(activeTab.value))void refreshMyWork();}
export function startMyWorkRefresh(){window.addEventListener('focus',wake);document.addEventListener('visibilitychange',wake);}
export function stopMyWorkRefresh(){window.removeEventListener('focus',wake);document.removeEventListener('visibilitychange',wake);}
