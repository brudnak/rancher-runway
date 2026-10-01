import {ref} from 'vue';
import {apiFetch} from './store.js';

export const dailyReadiness=ref({enabled:false,report:null});
export const dailyReadinessError=ref('');
export const dailyReadinessBusy=ref(false);
let timer,started=false,waking=false,sequence=0;
const running=()=>dailyReadiness.value.report?.status==='running';
function poll(){
 clearTimeout(timer);
 if(started&&running()&&!document.hidden)timer=setTimeout(refreshDailyReadiness,3000);
}
async function request(payload){
 const ticket=++sequence;
 try{
  const response=await apiFetch('/api/issue-readiness/daily',payload?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)}:{});
  if(!response.ok)throw new Error(await response.text());
  const state=await response.json();
  if(ticket===sequence){dailyReadiness.value=state;dailyReadinessError.value='';}
  return state;
 }catch(error){if(ticket===sequence)dailyReadinessError.value=error.message;return null;}
 finally{if(ticket===sequence)poll();}
}
export const refreshDailyReadiness=()=>dailyReadinessBusy.value?Promise.resolve(null):request();
export async function changeDailyReadiness(action,enabled){
 if(dailyReadinessBusy.value)return;
 dailyReadinessBusy.value=true;
 try{
  const result=await request({action,...(enabled===undefined?{}:{enabled}),timezone:Intl.DateTimeFormat().resolvedOptions().timeZone||'UTC'});
  if(result&&action==='settings'&&enabled)await request({action:'auto',timezone:Intl.DateTimeFormat().resolvedOptions().timeZone||'UTC'});
 }finally{dailyReadinessBusy.value=false;}
}
export async function wakeDailyReadiness(){
 if(waking||document.hidden||dailyReadinessBusy.value)return;
 waking=true;
 try{const state=await refreshDailyReadiness();if(state?.enabled&&!running())await changeDailyReadiness('auto');}
 finally{waking=false;}
}
export function startDailyReadiness(){
 if(started)return;
 started=true;
 window.addEventListener('focus',wakeDailyReadiness);
 document.addEventListener('visibilitychange',wakeDailyReadiness);
 void wakeDailyReadiness();
}
export function stopDailyReadiness(){
 started=false;clearTimeout(timer);
 window.removeEventListener('focus',wakeDailyReadiness);
 document.removeEventListener('visibilitychange',wakeDailyReadiness);
}
