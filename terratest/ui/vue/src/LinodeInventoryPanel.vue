<script setup>
import {computed,nextTick,onUnmounted,ref,watch} from 'vue';
import {apiFetch} from './store.js';
import {readJSON} from './read-json.mjs';
const props=defineProps({active:Boolean});
const source=ref('configured'), token=ref(''), inventory=ref(null), busy=ref(''), error=ref(''), message=ref('');
const query=ref(''), type=ref(''), plan=ref(null), confirmName=ref(''), confirmID=ref(''), dialog=ref(null), expired=ref(false);
let timer, trigger, generation=0;
const labels={instance:'Instance',volume:'Volume',nodebalancer:'NodeBalancer',firewall:'Firewall'};
const items=computed(()=>inventory.value?.items||[]);
const visible=computed(()=>items.value.filter(item=>(!type.value||item.type===type.value)&&`${item.label} ${item.id} ${item.region||''} ${(item.tags||[]).join(' ')} ${(item.ipv4||[]).join(' ')}`.toLowerCase().includes(query.value.trim().toLowerCase())));
const confirmed=computed(()=>plan.value&&!expired.value&&confirmName.value===plan.value.nameConfirmation&&confirmID.value===plan.value.idConfirmation);
async function call(action,extra={}) {
 return readJSON(signal=>apiFetch('/api/linode/inventory',{method:'POST',signal,headers:{'Content-Type':'application/json'},body:JSON.stringify({action,token:source.value==='manual'?token.value:'',...extra})}),{label:'Linode inventory',timeoutMs:95000});
}
async function load(){
 if(busy.value)return;
 if(source.value==='manual'&&!token.value.trim()){error.value='Enter a Linode token for this session.';return;}
 const current=++generation;busy.value='list';error.value='';
 try{const value=await call('list');if(current===generation)inventory.value=value;}catch(e){if(current===generation)error.value=e.message;}finally{if(current===generation)busy.value='';}
}
function close(){if(busy.value==='delete')return;dialog.value?.close();plan.value=null;clearTimeout(timer);confirmName.value='';confirmID.value='';trigger?.focus();}
async function review(item,event){
 if(busy.value)return;trigger=event.currentTarget;error.value='';message.value='';busy.value='preview';
 try{plan.value=await call('preview',{resource:{type:item.type,id:item.id}});expired.value=false;confirmName.value='';confirmID.value='';await nextTick();dialog.value.showModal();timer=setTimeout(()=>{expired.value=true;},Math.max(0,new Date(plan.value.expiresAt)-Date.now()));}catch(e){error.value=e.message;}finally{busy.value='';}
}
async function remove(){
 if(!confirmed.value||busy.value)return;busy.value='delete';error.value='';
 try{const result=await call('delete',{review:plan.value.token,nameConfirmation:confirmName.value,idConfirmation:confirmID.value});message.value=result.message;busy.value='';close();await load();}catch(e){error.value=e.message;expired.value=true;}finally{busy.value='';}
}
watch([source,token],()=>{generation++;inventory.value=null;error.value='';message.value='';close();busy.value='';});
watch(()=>props.active,active=>{if(active&&!inventory.value&&source.value==='configured')load();},{immediate:true});
onUnmounted(()=>{clearTimeout(timer);generation++;});
</script>
<template>
 <div class="linode-inventory">
  <header><div><p class="eyebrow">CLOUD VISIBILITY</p><h2>Linode resources</h2><p>Instances, volumes, NodeBalancers and firewalls visible to your token.</p></div><button :disabled="!!busy" @click="load">{{busy==='list'?'Loading…':'Refresh resources'}}</button></header>
  <div class="scope-warning" role="note"><strong>Your token determines what you can see.</strong><p>An admin Linode token may show everyone’s resources across the account—not just yours. A matching name, prefix or tag does not prove ownership. Verify the exact resource before deleting it.</p></div>
  <div class="connection"><label>Token source<select v-model="source" :disabled="!!busy"><option value="configured">Use Runway’s configured Linode token</option><option value="manual">Use a token for this session</option></select></label><label v-if="source==='manual'">Linode API token<input v-model="token" :disabled="!!busy" type="password" autocomplete="off" placeholder="Token stays in memory only"/></label><p v-else>Uses your existing config or environment credentials. The token is never displayed.</p></div>
  <p v-if="message" role="status" class="success">{{message}}</p><p v-if="error&&!plan" role="alert" class="error">{{error}}</p>
  <div v-if="inventory?.warnings?.length" class="scope-warning"><strong>Partial inventory</strong><p>Other resources may exist. Some resource types could not be read with this token.</p><ul><li v-for="warning in inventory.warnings" :key="warning">{{warning}}</li></ul></div>
  <section class="browser"><div class="toolbar"><div><h3>{{items.length}} resources visible</h3><p>{{visible.length}} shown · Filters change this view, not ownership or deletion scope.</p></div><label class="search">Search<input v-model="query" type="search" placeholder="Name, ID, prefix, region or tag…"/></label><label>Type<select v-model="type"><option value="">All supported types</option><option v-for="(label,key) in labels" :key="key" :value="key">{{label}}</option></select></label></div>
   <p v-if="!inventory">Load resources to inspect the account visible to this token.</p><p v-else-if="!visible.length">No resources match this view. This does not guarantee that the account has no other resources.</p>
   <div v-else class="table-scroll"><table><thead><tr><th>Resource</th><th>Type / ID</th><th>Region / state</th><th>Tags / addresses</th><th><span class="sr-only">Actions</span></th></tr></thead><tbody><tr v-for="item in visible" :key="`${item.type}-${item.id}`"><td><strong>{{item.label}}</strong><small v-if="item.attachedTo">Attached to instance {{item.attachedTo}}</small></td><td>{{labels[item.type]}}<code>{{item.id}}</code></td><td>{{item.region||'—'}}<small>{{item.status||'State not reported'}}</small></td><td>{{(item.tags||[]).join(', ')||'No tags'}}<small>{{(item.ipv4||[]).join(', ')}}</small></td><td><button :disabled="!!busy" @click="review(item,$event)">Review deletion</button></td></tr></tbody></table></div>
   <p class="footnote">One resource at a time. There is no bulk deletion. For a Rancher-managed node, remove the downstream cluster or machine pool through Rancher first so it does not recreate the node.</p>
  </section>
  <Teleport to="body"><dialog ref="dialog" aria-labelledby="linode-delete-title" class="linode-delete-dialog" @cancel.prevent="close"><form v-if="plan" @submit.prevent="remove"><h2 id="linode-delete-title">Delete this {{labels[plan.resource.type]}}?</h2><div class="target"><strong>{{plan.resource.label}}</strong><code>{{plan.resource.type}} · {{plan.resource.id}} · {{plan.resource.region||'No region'}}</code></div><p class="scope-warning">An admin token may control other people’s resources. This action cannot be undone.</p><ul><li v-for="effect in plan.effects" :key="effect">{{effect}}</li></ul><label>Type <strong>{{plan.nameConfirmation}}</strong> to confirm deletion:<input v-model="confirmName" :disabled="!!busy||expired" autocomplete="off" spellcheck="false" autocapitalize="none"/></label><label>Type <strong>{{plan.idConfirmation}}</strong> again:<input v-model="confirmID" :disabled="!!busy||expired" autocomplete="off" spellcheck="false" autocapitalize="none"/></label><p v-if="expired">This review expired or can no longer be used. Close it and review the resource again.</p><p v-if="error" class="error" role="alert">{{error}}</p><footer><button type="button" :disabled="busy==='delete'" autofocus @click="close">Cancel</button><button class="danger" :disabled="!confirmed||!!busy" type="submit">{{busy==='delete'?'Deleting…':'Permanently delete this resource'}}</button></footer></form></dialog></Teleport>
 </div>
</template>
<style scoped>
.linode-inventory{--border:#d4d4d8;--surface:#fff;--muted:#71717a;color:#18181b;display:grid;gap:22px}.dark .linode-inventory{--border:#34404a;--surface:#192027;--muted:#a2adb8;color:#e4e8ed}.linode-inventory header,.toolbar{display:flex;align-items:center;justify-content:space-between;gap:20px;flex-wrap:wrap}header h2{font-size:30px;font-weight:650}.eyebrow{font-size:11px;letter-spacing:.16em;color:var(--muted)}p{font-size:14px;line-height:1.65;color:var(--muted);margin-top:6px}button,input,select{border:1px solid var(--border);border-radius:8px;padding:10px 13px;background:var(--surface);color:inherit}button{font-weight:600;font-size:13px}button:disabled{opacity:.5;cursor:not-allowed}label{display:grid;gap:7px;font-size:13px}input,select{min-width:220px}.scope-warning{border:1px solid #bf9447;background:#bd8f3310;border-radius:10px;padding:16px}.scope-warning p{color:inherit}.connection{display:flex;gap:22px;align-items:end;flex-wrap:wrap}.browser{background:var(--surface);border:1px solid var(--border);border-radius:14px;padding:20px}.toolbar{margin-bottom:18px}.toolbar h3{font-size:19px;font-weight:600}.table-scroll{overflow:auto}table{width:100%;border-collapse:collapse;font-size:13px}th,td{text-align:left;padding:14px 10px;border-bottom:1px solid var(--border)}th{font-size:11px;color:var(--muted);text-transform:uppercase}small,td code{display:block;color:var(--muted);margin-top:5px}.footnote{font-size:12px;margin-top:18px}.error{color:#db7766}.success{color:#22a080}.linode-delete-dialog{max-width:620px;width:calc(100% - 32px);max-height:90vh;overflow:auto;border:1px solid #53616b;border-radius:14px;background:#192027;color:#e4e8ed;padding:26px}.linode-delete-dialog::backdrop{background:#0009}.linode-delete-dialog h2{font-size:22px;font-weight:650}.linode-delete-dialog label{margin-top:18px}.linode-delete-dialog input{background:#11181f;border-color:#53616b}.linode-delete-dialog p{color:inherit}.linode-delete-dialog li{font-size:13px;line-height:1.6;margin:12px 0}.target{display:grid;gap:6px;margin:18px 0}.target code{font-size:13px}.linode-delete-dialog footer{display:flex;justify-content:flex-end;gap:12px;margin-top:24px}.linode-delete-dialog button{background:#26313b;border-color:#53616b}.linode-delete-dialog .danger{background:#9e3f32;color:white}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0,0,0,0)}
</style>
