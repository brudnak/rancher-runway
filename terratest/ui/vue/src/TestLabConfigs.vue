<script setup>
import {computed,nextTick,onBeforeUnmount,onMounted,reactive,ref,watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import Confirm from './LocalLabConfirm.vue';
import Editor from './TestLabEditor.vue';
import Transfer from './TestLabTransfer.vue';
import {inspectConfig} from './test-lab-workspace.mjs';
import {configFilename,configTree,configTreeKey} from './test-lab-explorer.mjs';
const props=defineProps({request:Function,active:Boolean,currentConfig:String});
const emit=defineEmits(['use','close']);
const blankConfig='# Add your environment configuration here\nrancher:\n  host: ""\n  adminToken: ""\n';
const library=ref({folders:[],files:[]}),storage=ref(''),loaded=ref(false),pending=ref(false),error=ref(''),notice=ref('');
const query=ref(''),draft=ref(null),baseline=ref(''),dialog=ref(null),pendingSwitch=ref(null),selectedFolder=ref('');
const documentKey=ref(0);
const expanded=reactive({}),focusKey=ref(''),tree=ref(null),folderField=ref(null),nameField=ref(null),transfer=ref(null),menu=ref(null),menuElement=ref(null);
const folderEdit=reactive({open:false,id:'',name:''});
const dirty=computed(()=>Boolean(draft.value&&JSON.stringify(draft.value)!==baseline.value));
const valid=computed(()=>draft.value&&inspectConfig(draft.value.config).valid&&Boolean(draft.value.name.trim()));
const rows=computed(()=>configTree(library.value,expanded,query.value));
const folderName=id=>library.value.folders.find(f=>f.id===id)?.name||'Library root';
async function perform(fn){if(pending.value)return;pending.value=true;error.value='';try{return await fn();}catch(e){error.value=e.message;return false;}finally{pending.value=false;}}
async function reload(){
 const data=await props.request('config-library');library.value=data.library;storage.value=data.path;loaded.value=true;
 for(const f of data.library.folders)if(expanded[f.id]===undefined)expanded[f.id]=true;
 if(selectedFolder.value&&!data.library.folders.some(f=>f.id===selectedFolder.value))selectedFolder.value='';
}
function applyDraft(next){documentKey.value++;draft.value=next;baseline.value=JSON.stringify(next);pendingSwitch.value=null;selectedFolder.value=next.folder;if(next.folder)expanded[next.folder]=true;focusKey.value=next.id?`file:${next.id}`:focusKey.value;notice.value='';}
function replace(next){if(dirty.value){pendingSwitch.value=next;}else applyDraft(next);}
async function open(file){if(draft.value?.id===file.id)return;await perform(async()=>{const value=await props.request('config-load',{id:file.id});replace({...value.file,config:value.config});});}
function newDraft(config=blankConfig,name='Untitled cattle-config',folder=selectedFolder.value){replace({id:'',revision:'',name,folder,config});nextTick(()=>nameField.value?.focus({preventScroll:true}));}
async function saveDraft(){
 const payload={...draft.value};
 const result=await props.request('config-save',{...payload,confirm:payload.id?'UPDATE TEMPLATE':''});
 const saved={...result,config:payload.config};
 // Preserve keystrokes made while the request was in flight.
 if(JSON.stringify(draft.value)===JSON.stringify(payload))draft.value=saved;
 else if(draft.value?.id===payload.id)draft.value={...draft.value,id:result.id,revision:result.revision,updatedAt:result.updatedAt};
 baseline.value=JSON.stringify(saved);selectedFolder.value=result.folder;if(result.folder)expanded[result.folder]=true;
 await reload();focusKey.value=`file:${result.id}`;notice.value='Saved. Run working copies stay independent.';
 return !dirty.value;
}
async function saveAndContinue(){const next=pendingSwitch.value;const clean=await perform(saveDraft);if(clean)applyDraft(next);}
async function duplicate(file){await perform(async()=>{const value=await props.request('config-load',{id:file.id});replace({id:'',revision:'',name:(file.name.slice(0,85)+' copy'),folder:file.folder,config:value.config});});}
function remove(file){if(!file)return;dialog.value={title:'Delete this cattle-config?',message:`Permanently remove “${file.name}” and its saved YAML from this computer. Run working copies and Rancher resources stay in place.`,phrase:'DELETE CONFIG',button:'Delete config',run:async()=>{await props.request('config-delete',{id:file.id,revision:file.revision,confirm:'DELETE CONFIG'});await reload();if(draft.value?.id===file.id){draft.value=null;baseline.value='';pendingSwitch.value=null;}notice.value='Cattle-config deleted.';}};}
async function editFolder(folder){Object.assign(folderEdit,{open:true,id:folder?.id||'',name:folder?.name||''});await nextTick();folderField.value?.focus();}
async function saveFolder(){await perform(async()=>{const result=await props.request('folder-save',{id:folderEdit.id,name:folderEdit.name});const created=result.folders.find(f=>!library.value.folders.some(old=>old.id===f.id));await reload();folderEdit.open=false;if(created){selectedFolder.value=created.id;expanded[created.id]=true;focusRow(`folder:${created.id}`);}notice.value=created?'Folder created. Add configs from its action menu.':'Folder renamed.';});}
function removeFolder(folder){dialog.value={title:'Delete this folder?',message:`Remove the empty folder “${folder.name}” from your local library.`,phrase:'DELETE FOLDER',button:'Delete folder',run:async()=>{await props.request('folder-delete',{id:folder.id,confirm:'DELETE FOLDER'});await reload();notice.value='Folder deleted.';}};}
async function confirm(){const d=dialog.value;await perform(async()=>{await d.run();dialog.value=null;});}
async function focusRow(key){focusKey.value=key;await nextTick();const el=Array.from(tree.value?.querySelectorAll('[role=treeitem]')||[]).find(el=>el.dataset.key===key);el?.focus({preventScroll:true});el?.scrollIntoView({block:'nearest',inline:'nearest'});}
function activate(row){focusKey.value=row.key;if(row.kind==='folder'){selectedFolder.value=row.id;expanded[row.id]=!row.open;}else open(row);}
function treeKey(event,row){
 if(event.key==='Enter'||event.key===' '){event.preventDefault();activate(row);return;}
 if(event.key==='ContextMenu'||(event.shiftKey&&event.key==='F10')){event.preventDefault();showMenu(event,row);return;}
 if(!['ArrowUp','ArrowDown','ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;
 event.preventDefault();const action=configTreeKey(rows.value,row.key,event.key);
 if(action.expand)expanded[action.expand]=true;if(action.collapse)expanded[action.collapse]=false;if(action.focus)focusRow(action.focus);
}
async function showMenu(event,row){
 if(pending.value)return;const rect=event.currentTarget.getBoundingClientRect();const x=event.type==='contextmenu'?event.clientX:rect.right;const y=event.type==='contextmenu'?event.clientY:rect.bottom;
 menu.value={row,x:Math.max(12,Math.min(x,window.innerWidth-252)),y:Math.max(12,Math.min(y,window.innerHeight-270)),returnFocus:event.currentTarget};
 await nextTick();menuElement.value?.querySelector('[role=menuitem]:not(:disabled)')?.focus({preventScroll:true});
}
function closeMenu(restore=false){const target=menu.value?.returnFocus;menu.value=null;if(restore)nextTick(()=>target?.isConnected&&target.focus({preventScroll:true}));}
function showTransfer(selection){transfer.value=selection;nextTick(()=>document.getElementById('test-library-transfer')?.scrollIntoView({block:'nearest',behavior:'auto'}));}
const menuActions=computed(()=>{
 const row=menu.value?.row;if(!row)return[];
 return row.kind==='folder'?[
  {label:'New cattle-config',icon:'plus',run:()=>newDraft(blankConfig,'Untitled cattle-config',row.id)},
  {label:'Rename folder',icon:'folder',run:()=>editFolder(row)},
  {label:'Export folder…',icon:'download',run:()=>showTransfer({scope:'folder',id:row.id,name:row.name})},
  {label:row.count?'Delete folder (must be empty)':'Delete folder…',icon:'trash',danger:true,disabled:row.count>0,run:()=>removeFolder(row)}
 ]:[
  {label:'Open cattle-config',icon:'file',run:()=>open(row)},
  {label:'Duplicate saved config',icon:'copy',run:()=>duplicate(row)},
  {label:'Export saved YAML…',icon:'download',run:()=>showTransfer({scope:'config',id:row.id,name:row.name})},
  {label:'Delete cattle-config…',icon:'trash',danger:true,run:()=>remove(row)}
 ];
});
function menuKey(event){
 if(event.key==='Escape'){event.preventDefault();closeMenu(true);return;}
 if(event.key==='Tab'){closeMenu(true);return;}
 if(!['ArrowUp','ArrowDown','Home','End'].includes(event.key))return;
 event.preventDefault();const items=Array.from(menuElement.value.querySelectorAll('[role=menuitem]:not(:disabled)'));const index=items.indexOf(document.activeElement);const next=event.key==='Home'?0:event.key==='End'?items.length-1:(index+(event.key==='ArrowDown'?1:-1)+items.length)%items.length;items[next]?.focus();
}
function outsideMenu(event){if(menu.value&&!event.target.closest('.test-explorer-menu'))closeMenu();}
function saveShortcut(event){if(props.active&&(event.metaKey||event.ctrlKey)&&event.key.toLowerCase()==='s'&&event.target.closest('[data-config-library]')){event.preventDefault();if(draft.value&&valid.value&&!pending.value&&(dirty.value||!draft.value.id))perform(saveDraft);}}
function leave(event){if(dirty.value){event.preventDefault();event.returnValue='';}}
watch(rows,list=>{if(!list.some(row=>row.key===focusKey.value))focusKey.value=list[0]?.key||'';});
watch(()=>props.active,active=>{if(active&&!loaded.value)perform(reload);if(!active)closeMenu();},{immediate:true});
onMounted(()=>{window.addEventListener('pointerdown',outsideMenu);window.addEventListener('keydown',saveShortcut);window.addEventListener('beforeunload',leave);});
onBeforeUnmount(()=>{window.removeEventListener('pointerdown',outsideMenu);window.removeEventListener('keydown',saveShortcut);window.removeEventListener('beforeunload',leave);});
</script>
<template>
 <section aria-label="Cattle-config library" data-config-library>
  <div class="test-section-heading"><div><span class="test-eyebrow">YOUR ENVIRONMENT WORKSPACE</span><h3>Cattle-config library<span class="test-heading-dot">.</span></h3><p>Keep your environments organized. Every test run gets its own working copy.</p></div><button class="test-button" @click="emit('close')"><Icon name="arrow-left"/>Back to run</button></div>
  <div v-if="error" class="test-message test-error" role="alert"><span>{{ error }}</span><button class="test-link" @click="error=''">Dismiss</button></div>
  <div class="test-library-actions test-library-toolbar"><button class="test-button test-primary" :disabled="pending" @click="newDraft()"><Icon name="plus"/>New cattle-config</button><button class="test-button" :disabled="pending" @click="newDraft(currentConfig,'Current run config')"><Icon name="copy"/>From run draft</button><div class="test-library-transfer-actions"><button class="test-button" :disabled="pending" @click="showTransfer({scope:'import'})"><Icon name="upload"/>Import…</button><button class="test-button" :disabled="pending||!library.files.length&&!library.folders.length" @click="showTransfer({scope:'library'})"><Icon name="download"/>Export library…</button></div></div>
  <Transfer v-if="transfer" id="test-library-transfer" :key="transfer.scope+transfer.id" :request="request" :selection="transfer" :library="library" :target-folder="selectedFolder" @close="transfer=null" @imported="reload().catch(e=>error=e.message)"/>
  <div class="test-library-layout test-explorer-layout">
   <aside class="test-library-index test-explorer-index" aria-label="Cattle-config explorer">
    <div class="test-explorer-heading"><span class="test-eyebrow">EXPLORER</span><span class="test-count">{{ library.files.length }}</span><div><button class="test-icon-button" :disabled="pending" title="New folder" aria-label="New folder" @click="editFolder()"><Icon name="plus"/></button><button class="test-icon-button" title="Collapse all folders" aria-label="Collapse all folders" @click="query='';Object.keys(expanded).forEach(id=>expanded[id]=false)"><Icon name="layers"/></button><button class="test-icon-button" :disabled="pending" title="Refresh library" aria-label="Refresh library" @click="perform(reload)"><Icon name="refresh"/></button></div></div>
    <div class="test-search test-explorer-search"><Icon name="search"/><input v-model="query" type="search" aria-label="Search saved configs" placeholder="Find files or folders…"/></div>
    <form v-if="folderEdit.open" class="test-explorer-create" @submit.prevent="saveFolder"><label>{{ folderEdit.id?'Rename folder':'New folder' }}<input ref="folderField" v-model="folderEdit.name" maxlength="100" placeholder="e.g. Staging" :disabled="pending" @keydown.esc="folderEdit.open=false"/></label><button class="test-icon-button" :disabled="pending||!folderEdit.name.trim()" aria-label="Save folder"><Icon name="check"/></button><button type="button" class="test-icon-button" :disabled="pending" aria-label="Cancel folder edit" @click="folderEdit.open=false"><Icon name="close"/></button></form>
    <div ref="tree" role="tree" aria-label="Saved cattle-configs" class="test-explorer-tree" :aria-busy="pending">
     <template v-for="row in rows" :key="row.key">
      <div role="treeitem" :data-key="row.key" :aria-label="row.kind==='folder'?`${row.name}, ${row.count} config${row.count===1?'':'s'}`:configFilename(row.name)" :aria-level="row.level" :aria-expanded="row.kind==='folder'?row.open:undefined" :aria-selected="row.kind==='file'?draft?.id===row.id:selectedFolder===row.id" :tabindex="focusKey===row.key?0:-1" class="test-explorer-row" :class="{'is-file':row.kind==='file','is-folder':row.kind==='folder','is-child':row.level===2,'is-selected':row.kind==='file'&&draft?.id===row.id,'is-folder-selected':row.kind==='folder'&&selectedFolder===row.id}" :title="row.name" @focus="focusKey=row.key" @click="activate(row)" @keydown="treeKey($event,row)" @contextmenu.prevent="showMenu($event,row)">
       <Icon v-if="row.kind==='folder'" name="chevron" class="test-explorer-chevron" :class="{'is-open':row.open}"/><span v-else class="test-explorer-spacer"/>
       <Icon :name="row.kind==='folder'?'folder':'file'"/><span class="test-explorer-name">{{ row.kind==='file'?configFilename(row.name):row.name }}</span><span v-if="row.kind==='folder'" class="test-explorer-count">{{ row.count }}</span><span v-if="row.kind==='file'&&draft?.id===row.id&&dirty" class="test-explorer-dirty" title="Unsaved changes" aria-label="Unsaved changes"/>
       <button class="test-explorer-more" tabindex="-1" :aria-label="`Actions for ${row.name}`" aria-haspopup="menu" :aria-expanded="menu?.row.key===row.key" @click.stop="showMenu($event,row)" @keydown.stop>···</button>
      </div><div v-if="row.kind==='folder'&&row.open&&!row.visibleCount" class="test-explorer-empty-folder" role="none">Empty folder</div>
     </template>
     <div v-if="!rows.length" class="test-explorer-empty" role="none"><Icon :name="query?'search':'folder'"/><strong>{{ !loaded?'Opening your library…':query?'No matching files':'A place for every environment' }}</strong><p>{{ query?'Try a config or folder name.':'Create a cattle-config, add a folder, or import your existing setup.' }}</p><button v-if="query" class="test-link" @click="query=''">Clear search</button></div>
    </div>
    <div class="test-explorer-footer"><Icon name="lock"/><span>Local library</span><small>{{ library.folders.length }} folders · {{ library.files.length }} configs</small></div>
   </aside>
   <div class="test-library-editor test-explorer-editor">
    <div v-if="pendingSwitch" class="test-unsaved-prompt" role="group" aria-label="Unsaved changes"><strong>Keep your edits before switching?</strong><p>“{{ draft.name }}” has unsaved changes.</p><div class="test-inline-actions"><button class="test-button test-primary" :disabled="pending||!valid" @click="saveAndContinue">Save & continue</button><button class="test-button" :disabled="pending" @click="applyDraft(pendingSwitch)">Discard edits</button><button class="test-link" :disabled="pending" @click="pendingSwitch=null">Keep editing</button></div></div>
    <template v-if="draft">
     <div class="test-library-document-bar"><div><span class="test-eyebrow">{{ folderName(draft.folder) }}</span><h4><Icon name="file"/>{{ configFilename(draft.name||'Untitled') }}<span v-if="dirty" class="test-explorer-dirty" title="Unsaved changes"/></h4></div><div class="test-inline-actions"><button class="test-button" :disabled="!valid||pending" @click="emit('use',{config:draft.config,name:draft.name})">Use for run <Icon name="arrow"/></button><button class="test-button test-primary" :disabled="pending||!valid||draft.id&&!dirty" title="Save cattle-config (⌘ / Ctrl S)" @click="perform(saveDraft)"><Icon name="bookmark"/>{{ pending?'Saving…':draft.id?'Save changes':'Save config' }}</button></div></div>
     <details :key="draft.id||'new'" class="test-template-details" :open="!draft.id"><summary>{{ draft.id?'Rename or move':'Name & location' }}<span>{{ draft.id?(dirty?'Unsaved changes':'Saved template'):'New cattle-config' }}</span></summary><div class="test-template-fields"><label>Config name<input ref="nameField" v-model="draft.name" maxlength="100" placeholder="RKE2 on AWS · staging"/></label><label>Folder<select v-model="draft.folder"><option value="">Library root</option><option v-for="folder in library.folders" :key="folder.id" :value="folder.id">{{ folder.name }}</option></select></label></div></details>
     <Editor :key="documentKey" v-model="draft.config"/>
     <div class="test-library-document-footer"><span><Icon name="copy"/>Use for run includes your editor changes; the saved template stays independent.</span><button v-if="draft.id" class="test-link" :disabled="pending" @click="showTransfer({scope:'config',id:draft.id,name:library.files.find(f=>f.id===draft.id)?.name||draft.name})"><Icon name="download"/>Export saved YAML…</button></div>
    </template>
    <div v-else class="test-library-welcome"><div class="test-library-orbit"><Icon name="folder"/></div><span class="test-eyebrow">READY FOR YOUR NEXT EXPERIMENT</span><h4>Your setup.<br/>Always within reach.</h4><p>Open a cattle-config from the explorer. Edit its YAML, make a working copy for a test, or take a whole folder to another Runway installation.</p><div class="test-inline-actions"><button class="test-button" @click="newDraft(currentConfig,'Current run config')">Start from your run draft <Icon name="arrow"/></button></div><small>Arrow keys to explore · Right-click for actions · ⌘ / Ctrl S to save</small></div>
   </div>
  </div>
  <div class="test-library-status" role="status"><Icon :name="notice?'check':'lock'"/><span>{{ notice||'Saved privately on this computer. Export only when you choose.' }}</span><span v-if="pending">Working…</span></div>
  <Confirm v-if="dialog" :title="dialog.title" :message="dialog.message" :phrase="dialog.phrase" :button-label="dialog.button" :busy="pending" @cancel="dialog=null" @confirm="confirm"/>
  <details class="test-storage-note"><summary><Icon name="lock"/>Storage & portability</summary><p>Saved YAML can contain tokens and provider credentials. Runway keeps it outside the repository in an owner-only directory (0700), with owner-only files (0600). OS backups may include these files. Exports are unencrypted and include every saved value.</p><code>{{ storage }}</code><p>Individual configs export as YAML. Folders and full libraries use versioned .runway-cattle-configs.json bundles. Import always adds copies and preserves comments; it never overwrites existing files.</p></details>
 </section>
 <Teleport to="body"><div v-if="menu" class="test-lab test-explorer-menu-layer" :style="{left:menu.x+'px',top:menu.y+'px'}"><div ref="menuElement" role="menu" :aria-label="`Actions for ${menu.row.name}`" class="test-explorer-menu" @keydown="menuKey"><div class="test-explorer-menu-title">{{ menu.row.name }}</div><button v-for="action in menuActions" :key="action.label" role="menuitem" tabindex="-1" :disabled="action.disabled" :class="{'test-danger':action.danger}" @click="closeMenu(true);action.run()"><Icon :name="action.icon"/>{{ action.label }}</button></div></div></Teleport>
</template>
