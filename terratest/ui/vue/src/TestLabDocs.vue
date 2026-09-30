<script setup>
import {computed,nextTick,ref,watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {markdownBlocks,markdownInline,safeDocURL,relatedReadmes} from './test-lab-workspace.mjs';
import {filterReadmes,resolveReadme,readmeTree,localReadmeLink,displayDocPath} from './test-lab-docs.mjs';
import {writeTextToClipboard} from './clipboard.js';
const props=defineProps({documents:{type:Array,default:()=>[]},sha:String,packages:{type:Array,default:()=>[]},initialPath:String,requestNonce:Number,busy:Boolean,error:String,embedded:Boolean,hideIndex:Boolean});
const emit=defineEmits(['reload','close','navigate']);
const query=ref(''),selected=ref(''),raw=ref(false),related=ref(false),copied=ref(''),closedFolders=ref(new Set()),reader=ref(null),linkNotice=ref(''),browseOpen=ref(false);
watch([()=>props.initialPath,()=>props.requestNonce],async([value])=>{selected.value=value||'';query.value='';related.value=false;copied.value='';linkNotice.value='';closedFolders.value=new Set();await nextTick();reader.value?.scrollTo({top:0});},{immediate:true});
const recommended=computed(()=>relatedReadmes(props.documents,props.packages));
const filtered=computed(()=>filterReadmes(related.value?recommended.value:props.documents,query.value));
const resolution=computed(()=>resolveReadme(props.documents,selected.value,props.packages));
const doc=computed(()=>resolution.value.doc);
watch(()=>doc.value?.path,path=>{if(path)browseOpen.value=false;});
const rows=computed(()=>readmeTree(filtered.value).filter(row=>query.value.trim()||!row.parents.some(path=>closedFolders.value.has(path))));
const blocks=computed(()=>{const ids=new Map();return markdownBlocks(doc.value?.content||'').map(block=>{if(block.type!=='heading')return block;const count=ids.get(block.id)||0;ids.set(block.id,count+1);return {...block,id:count?`${block.id}-${count}`:block.id};});});
const inline=text=>markdownInline(text,props.sha,doc.value?.path||'');
const selectedHidden=computed(()=>doc.value&&!filtered.value.some(item=>item.path===doc.value.path));
function toggleFolder(path){const next=new Set(closedFolders.value);next.has(path)?next.delete(path):next.add(path);closedFolders.value=next;}
async function choose(path){selected.value=path;browseOpen.value=false;copied.value='';linkNotice.value='';emit('navigate',path);await nextTick();reader.value?.scrollTo({top:0});}
async function copy(text,id){try{await writeTextToClipboard(text);copied.value=id;}catch{copied.value='failed';}}
async function link(event){
 const anchor=event.target.closest('a');if(!anchor||event.metaKey||event.ctrlKey||event.shiftKey||event.altKey)return;
 const target=localReadmeLink(anchor.getAttribute('href'),props.sha,doc.value?.path||'',props.documents);if(!target)return;
 event.preventDefault();await choose(target.path);
 if(target.anchor){raw.value=false;await nextTick();const heading=[...(reader.value?.querySelectorAll('[data-readme-anchor]')||[])].find(node=>node.dataset.readmeAnchor===target.anchor.toLowerCase());if(heading){reader.value.scrollTo({top:reader.value.scrollTop+heading.getBoundingClientRect().top-reader.value.getBoundingClientRect().top-16});heading.focus({preventScroll:true});}else if(doc.value)linkNotice.value=`This guide has no section named “${target.anchor}”. Showing the document from the top.`;}
}
</script>
<template>
 <section class="test-docs" :class="{'test-docs-embedded':embedded}" aria-label="Validation documentation">
  <div v-if="!embedded" class="test-section-heading"><div><span class="test-eyebrow">VALIDATION DOCUMENTATION</span><h3>Read first. Run with context.</h3><p>READMEs from the same revision as your tests.</p></div><button class="test-button" @click="$emit('close')"><Icon name="arrow-left"/>Back to run</button></div>
  <div v-if="error" role="alert" class="test-message test-error">{{ error }} <button class="test-link" @click="$emit('reload')">Retry</button></div>
  <div class="test-doc-layout">
   <component v-if="!hideIndex" :is="embedded?'details':'aside'" class="test-doc-index" aria-label="README explorer" :open="embedded?(browseOpen||!doc):undefined" @toggle="browseOpen=$event.target.open">
    <summary v-if="embedded" class="test-doc-browse"><Icon name="folder"/><span>Browse READMEs</span><small>{{ documents.length }} guides</small><Icon name="chevron"/></summary>
    <div class="test-doc-index-body">
    <div class="test-search"><Icon name="search"/><input v-model="query" type="search" aria-label="Search documentation" placeholder="Find a folder or README…"/><button v-if="query" class="test-doc-clear" type="button" aria-label="Clear documentation search" @click="query=''"><Icon name="close"/></button></div>
    <label class="test-check"><input v-model="related" type="checkbox" :disabled="!packages.length"/>Related to selected tests <span>{{ recommended.length }}</span></label>
    <p class="test-caption" aria-live="polite">{{ filtered.length }} {{ filtered.length===1?'guide':'guides' }}<span v-if="query"> matching “{{ query }}”</span> · {{ sha?.slice(0,9) }}</p>
    <nav class="test-doc-tree" aria-label="README folders and files">
     <template v-for="row in rows" :key="row.key">
      <button v-if="row.kind==='folder'" type="button" class="test-doc-folder" :style="{'--doc-depth':row.depth}" :aria-expanded="!!query.trim()||!closedFolders.has(row.path)" :title="row.path" @click="toggleFolder(row.path)"><Icon name="chevron" :class="{expanded:!!query.trim()||!closedFolders.has(row.path)}"/><Icon name="folder"/><span>{{ row.name }}</span></button>
      <button v-else type="button" class="test-doc-file" :class="{selected:doc?.path===row.path}" :style="{'--doc-depth':row.depth}" :aria-current="doc?.path===row.path?'page':undefined" :title="(row.doc.title||row.name)+' · '+row.path" @click="choose(row.path)"><Icon name="file"/><span><strong>{{ row.name }}</strong><small v-if="row.doc.title&&row.doc.title!==row.name">{{ row.doc.title }}</small></span></button>
     </template>
     <div v-if="!filtered.length" class="test-empty"><Icon :name="query?'search':'file'"/><p>{{ busy?'Reading documentation…':query?'No folders or READMEs match.':'No matching READMEs.' }}</p><button v-if="query||related" type="button" class="test-link" @click="query='';related=false">Show all guides</button></div>
    </nav>
    </div>
   </component>
   <div v-if="doc" class="test-doc-reader">
    <div class="test-doc-toolbar"><code :title="doc.path">{{ displayDocPath(doc.path) }}</code><div class="test-inline-actions"><div class="test-segment" aria-label="Document display"><button type="button" :aria-pressed="!raw" :class="{selected:!raw}" @click="raw=false">Read</button><button type="button" :aria-pressed="raw" :class="{selected:raw}" @click="raw=true">Raw MD</button></div><button type="button" class="test-link" @click="copy(doc.content,'document')">{{ copied==='document'?'Copied':'Copy Markdown' }}</button><a v-if="!embedded" :href="safeDocURL(doc.path.split('/').at(-1),sha,doc.path)" target="_blank" rel="noreferrer noopener" aria-label="Open README source on GitHub">GitHub ↗</a></div></div>
    <p v-if="resolution.note||linkNotice" class="test-doc-context" role="status">{{ linkNotice||resolution.note }}</p>
    <p v-if="!hideIndex&&selectedHidden" class="test-doc-context">Your open guide remains visible while you filter the list.</p>
    <pre v-if="raw" ref="reader" class="test-doc-raw" tabindex="0" aria-label="README Markdown source">{{ doc.content }}</pre>
    <article v-else ref="reader" class="test-markdown" aria-label="README content" tabindex="0" @click="link"><template v-for="(b,index) in blocks" :key="index"><component :is="'h'+Math.min(b.level+1,6)" v-if="b.type==='heading'" :data-readme-anchor="b.id" tabindex="-1" v-html="inline(b.text)"/><div v-else-if="b.type==='code'" class="test-doc-code"><div><span>{{ b.language||'Example' }}</span><button type="button" class="test-link" @click="copy(b.text,String(index))">{{ copied===String(index)?'Copied':'Copy example' }}</button></div><pre tabindex="0"><code>{{ b.text }}</code></pre></div><component :is="b.ordered?'ol':'ul'" v-else-if="b.type==='list'"><li v-for="(item,j) in b.items" :key="j" v-html="inline(item)"/></component><div v-else-if="b.type==='table'" class="test-doc-table"><table><thead><tr><th v-for="(cell,j) in b.header" :key="j" v-html="inline(cell)"/></tr></thead><tbody><tr v-for="(row,j) in b.rows" :key="j"><td v-for="(cell,k) in row" :key="k" v-html="inline(cell)"/></tr></tbody></table></div><blockquote v-else-if="b.type==='quote'" v-html="inline(b.text)"/><hr v-else-if="b.type==='rule'"/><p v-else v-html="inline(b.text)"/></template></article>
    <p class="test-caption test-doc-footnote">Documentation at revision {{ sha?.slice(0,9) }}. Examples are displayed as written; embedded HTML and remote images stay inactive.</p><p v-if="copied==='failed'" role="alert">Could not copy to the clipboard.</p>
   </div>
   <div v-else class="test-empty test-doc-missing"><Icon name="file"/><h4>{{ busy?'Opening documentation…':'Choose a README.' }}</h4><p>{{ busy?'Loading guides from the selected test revision.':resolution.note||(hideIndex?'Choose a README from the file explorer on the left.':'Browse the folders or search for a guide.') }}</p></div>
  </div>
 </section>
</template>
