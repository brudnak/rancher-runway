<script setup>
import {computed,ref} from 'vue';
import Icon from './HelmLabIcon.vue';
import {CONFIG_LIMIT} from './test-lab-workspace.mjs';
import {CATTLE_BUNDLE_FORMAT,CATTLE_BUNDLE_LIMIT} from './test-lab-explorer.mjs';
const props=defineProps({request:Function,selection:Object,library:Object,targetFolder:String});
const emit=defineEmits(['close','imported']);
const fileInput=ref(null),pending=ref(false),importingCopies=ref(false),error=ref(''),acknowledged=ref(false),result=ref(null),bundle=ref(null),preview=ref(null),filename=ref(''),destination=ref(props.targetFolder||'');
const importing=computed(()=>props.selection.scope==='import');
const count=computed(()=>props.selection.scope==='config'?1:props.library.files.filter(f=>props.selection.scope==='library'||f.folder===props.selection.id).length);
const folderCount=computed(()=>props.selection.scope==='folder'?1:props.library.folders.length);
const label=computed(()=>props.selection.scope==='library'?'Entire cattle-config library':props.selection.name);
async function perform(fn){if(pending.value)return;pending.value=true;error.value='';try{await fn();}catch(e){error.value=e.message;}finally{pending.value=false;}}
async function readFile(event){
 const file=event.target.files?.[0];event.target.value='';if(!file)return;
 bundle.value=null;preview.value=null;result.value=null;filename.value=file.name;
 await perform(async()=>{
  const yaml=/\.ya?ml$/i.test(file.name);
  if(!yaml&&!/\.json$/i.test(file.name))throw new Error('Choose a cattle-config YAML file or a runway-cattle-configs.json bundle.');
  if(file.size>(yaml?CONFIG_LIMIT:CATTLE_BUNDLE_LIMIT))throw new Error(yaml?'YAML files must be 128 KiB or smaller.':'Bundles must be 80 MiB or smaller.');
  const text=await file.text();
  if(yaml)bundle.value={format:CATTLE_BUNDLE_FORMAT,version:1,folders:[],configs:[{name:file.name.replace(/\.ya?ml$/i,''),folder:'',yaml:text}]};
  else {try{bundle.value=JSON.parse(text);}catch{throw new Error('This file is not valid JSON. Choose a Runway cattle-config bundle.');}}
  preview.value=await props.request('config-import-preview',{bundle:bundle.value,folder:destination.value});
 });
}
async function review(){preview.value=null;await perform(async()=>{preview.value=await props.request('config-import-preview',{bundle:bundle.value,folder:destination.value});});}
async function importCopies(){importingCopies.value=true;await perform(async()=>{
 const reviewed=preview.value;preview.value=null;
 const saved=await props.request('config-import',{bundle:bundle.value,folder:destination.value,revision:reviewed.revision,confirm:'IMPORT COPIES'});
 result.value={imported:saved};bundle.value=null;emit('imported');
});importingCopies.value=false;}
async function exportFiles(){await perform(async()=>{result.value=await props.request('config-export',{scope:props.selection.scope,id:props.selection.id||'',confirm:'EXPORT WITH CREDENTIALS'});});}
</script>
<template>
 <section class="test-transfer" :aria-label="importing?'Import cattle-configs':'Export cattle-configs'">
  <div class="test-transfer-heading"><div class="test-transfer-mark"><Icon :name="importing?'upload':'download'"/></div><div><span class="test-eyebrow">{{ importing?'BRING YOUR ENVIRONMENTS':'TAKE YOUR ENVIRONMENTS WITH YOU' }}</span><h4>{{ importing?'Import cattle-configs':`Export ${label}` }}</h4></div><button class="test-icon-button" :disabled="pending" aria-label="Close transfer" @click="emit('close')"><Icon name="close"/></button></div>
  <div v-if="error" class="test-message test-error" role="alert">{{ error }}</div>
  <template v-if="result"><div class="test-transfer-complete" role="status"><Icon name="check"/><div><strong>{{ importing?`${result.imported.configs} cattle-config${result.imported.configs===1?'':'s'} and ${result.imported.folders} folder${result.imported.folders===1?'':'s'} imported`:'Export saved to Downloads' }}</strong><p>{{ importing?'Your existing files and open editor are unchanged.':result.filename }}</p><code v-if="result.path">{{ result.path }}</code></div></div><button class="test-button" @click="emit('close')">Done</button></template>
  <template v-else-if="importing">
   <p class="test-caption">Open a YAML file or a <code>.runway-cattle-configs.json</code> bundle. Review the destination names before adding copies to your library.</p>
   <div class="test-transfer-controls"><button class="test-button" :disabled="pending" @click="fileInput.click()"><Icon name="upload"/>{{ filename?'Choose another file':'Choose a file' }}</button><span class="test-transfer-filename">{{ filename||'YAML · 128 KiB / Bundle · 80 MiB' }}</span><label>Destination for configs without a folder<select v-model="destination" :disabled="pending" @change="bundle&&review()"><option value="">Library root</option><option v-for="folder in library.folders" :key="folder.id" :value="folder.id">{{ folder.name }}</option></select></label></div>
   <input ref="fileInput" hidden type="file" accept=".yml,.yaml,.json" @change="readFile"/>
   <div v-if="preview" class="test-import-review"><div class="test-transfer-summary"><strong>{{ preview.configs }} config{{ preview.configs===1?'':'s' }} · {{ preview.folders }} folder{{ preview.folders===1?'':'s' }}</strong><span>{{ preview.renamed?`${preview.renamed} renamed to avoid collisions`:'All names available' }}</span></div><div class="test-import-entries" aria-label="Import destinations"><div v-for="(entry,i) in preview.entries" :key="i"><Icon :name="entry.kind==='folder'?'folder':'file'"/><div><strong>{{ entry.name }}</strong><small>{{ entry.kind==='folder'?'New folder':entry.folder }}<template v-if="entry.renamed"> · Was “{{ entry.original }}”</template></small></div><span v-if="entry.renamed" class="test-count">Renamed copy</span></div></div><p class="test-caption">Existing files stay untouched. Bundled folders become new folders, including empty ones. YAML values and comments are preserved exactly.</p></div>
   <div class="test-inline-actions test-transfer-footer"><button v-if="preview" class="test-button test-primary" :disabled="pending" @click="importCopies"><Icon name="plus"/>{{ pending?'Importing…':'Import copies' }}</button><button v-else-if="bundle" class="test-button" :disabled="pending" @click="review">{{ pending?(importingCopies?'Importing copies…':'Checking bundle…'):'Preview again' }}</button><span v-else-if="pending" role="status">Reading file…</span><button class="test-link" :disabled="pending" @click="emit('close')">Cancel</button></div>
  </template>
  <template v-else>
   <div class="test-transfer-summary"><strong>{{ count }} {{ count===1?'cattle-config':'cattle-configs' }}<template v-if="selection.scope!=='config'"> · {{ folderCount }} folder{{ folderCount===1?'':'s' }}</template></strong><code>{{ selection.scope==='config'?'YAML file':'.runway-cattle-configs.json' }}</code></div>
   <p class="test-caption">Exports use saved versions. {{ selection.scope==='config'?'The YAML can be used directly as a cattle-config.':'The portable bundle preserves folder names, config names, comments, and YAML. Import it into another Runway library whenever you need it.' }}</p>
   <label class="test-check test-export-ack"><input v-model="acknowledged" type="checkbox" :disabled="pending"/><span>Include all saved values, including any tokens and provider credentials. This export is unencrypted.</span></label>
   <div class="test-inline-actions"><button class="test-button test-primary" :disabled="pending||!acknowledged" @click="exportFiles"><Icon name="download"/>{{ pending?'Exporting…':'Save to Downloads' }}</button><button class="test-link" :disabled="pending" @click="emit('close')">Cancel</button></div>
  </template>
 </section>
</template>
