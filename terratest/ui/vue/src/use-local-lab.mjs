import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { readJSON } from './read-json.mjs';
import { CONFIRMATION_TEXT } from './confirmation.mjs';
import { writeTextToClipboard } from './clipboard.js';
import { createSingleFlight, visibleRecords } from './local-lab.mjs';

export function useLocalLab(kind, isActive) {
  const state = ref({preflight:{ready:false,items:[]},operation:{output:[]},clusters:[],runs:[],k3sVersions:[]});
  const loaded = ref(false), verified = ref(false), refreshing = ref(false), error = ref(''), updatedAt = ref(''), pending = ref(''), aborting = ref(false), notice = ref(null), clock = ref(Date.now());
  const requests = new Set();
  let disposed = false, pollTimer, clockTimer, noticeTimer;
  const token = JSON.parse(document.getElementById('control-panel-data')?.textContent || '{}').token || '';
  async function request(path, {signal:externalSignal,timeoutMs=30000,...options} = {}) {
    const controller = new AbortController(); requests.add(controller);
    const abort = () => controller.abort();
    externalSignal?.addEventListener('abort',abort,{once:true});
    if (externalSignal?.aborted) abort();
    let deadlineSignal;
    try {
      return await readJSON(async signal => {
        deadlineSignal = signal; signal.addEventListener('abort',abort,{once:true});
        const response = await fetch(path,{cache:'no-store',...options,signal:controller.signal,headers:{'Content-Type':'application/json','X-Control-Panel-Token':token,...options.headers}});
        if (!response.ok) throw new Error((await response.text()) || `Request failed (${response.status}).`);
        return response;
      }, {label:'Local lab request',timeoutMs});
    } finally {requests.delete(controller); externalSignal?.removeEventListener('abort',abort); deadlineSignal?.removeEventListener('abort',abort);}
  }
  function notify(message, tone='success') {
    clearTimeout(noticeTimer); notice.value = {message,tone};
    if (tone !== 'error') noticeTimer = setTimeout(()=>{notice.value=null;},6000);
  }
  const readState = createSingleFlight(async () => {
    refreshing.value = true;
    try {
      const next = await request(`/api/${kind}/state`);
      if (!next || !next.preflight || !next.operation) throw new Error('The lab returned an incomplete status. Refresh to try again.');
      if (disposed) return;
      state.value = next; loaded.value = true; verified.value = true; error.value = ''; updatedAt.value = new Date().toISOString();
    } catch (failure) {
      if (!disposed) {error.value = failure.message || 'Could not read local status.'; verified.value = false;}
    } finally {if (!disposed) refreshing.value = false;}
  });
  async function refresh(fresh = false) {
    clearTimeout(pollTimer);
    if (fresh && refreshing.value) await readState();
    if (disposed) return;
    await readState();
    clearTimeout(pollTimer);
    if (!disposed && isActive() && !document.hidden) pollTimer = setTimeout(refresh,state.value.operation?.running ? 3000 : 8000);
  }
  function syncActivity() {
    clearTimeout(pollTimer);
    if (isActive() && !document.hidden) { verified.value = false; refresh(); }
  }
  watch(isActive,syncActivity);
  onMounted(()=>{syncActivity();document.addEventListener('visibilitychange',syncActivity);clockTimer=setInterval(()=>{clock.value=Date.now();},1000);});
  onBeforeUnmount(()=>{disposed=true;clearTimeout(pollTimer);clearTimeout(noticeTimer);clearInterval(clockTimer);requests.forEach(controller=>controller.abort());document.removeEventListener('visibilitychange',syncActivity);});
  const operation = computed(()=>state.value.operation || {});
  const preflight = computed(()=>state.value.preflight || {ready:false,items:[]});
  const records = computed(()=>visibleRecords(state.value[kind === 'k3d' ? 'clusters' : 'runs']));
  const blocked = computed(()=>!verified.value || !!pending.value || aborting.value || !!operation.value.running);
  async function action(key,path,body,message,{timeoutMs=120000}={}) {
    if (pending.value || disposed) return false;
    pending.value = key;
    try {
      const result = await request(path,{method:'POST',body:JSON.stringify(body),timeoutMs});
      if (!disposed && message) notify(typeof message === 'function' ? message(result) : message);
      return true;
    } catch (failure) {
      if (!disposed) notify(failure.message || 'The action could not finish. Check status before retrying.','error');
      return false;
    } finally {
      if (!disposed) {await refresh(true);pending.value = '';}
    }
  }
  async function copy(text, label) {
    if (!text) return;
    try {await writeTextToClipboard(text); if(!disposed) notify(`${label} copied.`);}
    catch {if(!disposed) notify('Clipboard access was unavailable. Select the text to copy it manually.','error');}
  }
  async function openPath(path, label='Folder') {
    if (!path) return;
    try {await request('/api/open-path',{method:'POST',body:JSON.stringify({path,reveal:true})});notify(`${label} opened.`);}
    catch(failure) {notify(failure.message,'error');}
  }
  async function abortAction() {
    if (aborting.value || !operation.value.running) return;
    aborting.value=true;
    try {await request('/api/operations/abort',{method:'POST',body:JSON.stringify({operation:kind==='k3d'?'k3dLab':'steveLab',runId:operation.value.runId || '',confirm:CONFIRMATION_TEXT})});notify('Stop requested. Following the action until it finishes.','warning');}
    catch(failure) {notify(failure.message,'error');}
    finally {await refresh(true);aborting.value=false;}
  }
  return reactive({kind,state,loaded,verified,refreshing,error,updatedAt,pending,aborting,notice,clock,operation,preflight,records,blocked,request,refresh,notify,action,copy,openPath,abortAction});
}
