<script setup>
import {computed,ref,watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {markdownBlocks,markdownInline,safeDocURL,relatedReadmes} from './test-lab-workspace.mjs';
import {writeTextToClipboard} from './clipboard.js';
const props=defineProps({documents:{type:Array,default:()=>[]},sha:String,packages:{type:Array,default:()=>[]},initialPath:String,busy:Boolean,error:String});
defineEmits(['reload','close']);
const query=ref(''),selected=ref(''),raw=ref(false),related=ref(false),copied=ref('');
watch(()=>props.initialPath,value=>{if(value){selected.value=value;query.value='';related.value=false;}},{immediate:true});
const recommended=computed(()=>relatedReadmes(props.documents,props.packages));
const filtered=computed(()=>{const docs=related.value?recommended.value:props.documents;const words=query.value.toLowerCase().split(/\s+/).filter(Boolean);return docs.filter(d=>words.every(w=>`${d.path} ${d.title} ${d.content}`.toLowerCase().includes(w)));});
const doc=computed(()=>props.documents.find(d=>d.path===selected.value)||(!query.value&&recommended.value.at(-1))||filtered.value[0]);
const blocks=computed(()=>markdownBlocks(doc.value?.content||''));
const inline=text=>markdownInline(text,props.sha,doc.value?.path||'');
async function copy(text,id){try{await writeTextToClipboard(text);copied.value=id;}catch{copied.value='failed';}}
function link(event){const anchor=event.target.closest('a');if(!anchor)return;const url=new URL(anchor.href);const prefix=`/rancher/tests/blob/${props.sha}/`;if(url.origin==='https://github.com'&&url.pathname.startsWith(prefix)){const path=decodeURIComponent(url.pathname.slice(prefix.length));if(props.documents.some(d=>d.path===path)){event.preventDefault();selected.value=path;if(url.hash){setTimeout(()=>document.getElementById('test-readme-'+url.hash.slice(1).toLowerCase())?.scrollIntoView({block:'nearest'}),0);}}}}
</script>
<template>
 <section class="test-docs" aria-label="Validation documentation">
  <div class="test-section-heading"><div><span class="test-eyebrow">THE FIELD GUIDE</span><h3>Read first. Run with context.</h3><p>Validation READMEs from the same revision as your tests.</p></div><button class="test-button" @click="$emit('close')"><Icon name="arrow-left"/>Back to run</button></div>
  <div v-if="error" role="alert" class="test-message test-error">{{ error }} <button class="test-link" @click="$emit('reload')">Retry</button></div>
  <div class="test-doc-layout"><aside class="test-doc-index"><div class="test-search"><Icon name="search"/><input v-model="query" type="search" aria-label="Search documentation" placeholder="Search guides & their contents…"/></div><label class="test-check"><input v-model="related" type="checkbox" :disabled="!packages.length"/>Related to selected tests <span>{{ recommended.length }}</span></label><p class="test-caption">{{ filtered.length }} guides · {{ sha?.slice(0,9) }}</p><div class="test-doc-list"><button v-for="d in filtered" :key="d.path" :class="{selected:doc?.path===d.path}" @click="selected=d.path;copied=''" ><Icon name="file"/><span><strong>{{ d.title }}</strong><small>{{ d.path.replace(/^validation\//,'') }}</small></span></button><p v-if="!filtered.length" class="test-empty">{{ busy?'Reading documentation…':'No matching READMEs.' }}</p></div></aside>
   <div v-if="doc" class="test-doc-reader"><div class="test-doc-toolbar"><code>{{ doc.path }}</code><div class="test-inline-actions"><div class="test-segment" aria-label="Document display"><button :aria-pressed="!raw" :class="{selected:!raw}" @click="raw=false">Read</button><button :aria-pressed="raw" :class="{selected:raw}" @click="raw=true">Raw MD</button></div><button class="test-link" @click="copy(doc.content,'document')">{{ copied==='document'?'Copied':'Copy Markdown' }}</button><a :href="safeDocURL(doc.path.split('/').at(-1),sha,doc.path)" target="_blank" rel="noreferrer">Source ↗</a></div></div>
    <pre v-if="raw" class="test-doc-raw" tabindex="0">{{ doc.content }}</pre>
    <article v-else class="test-markdown" @click="link"><template v-for="(b,index) in blocks" :key="index"><component :is="'h'+Math.min(b.level+1,6)" v-if="b.type==='heading'" :id="'test-readme-'+b.id" v-html="inline(b.text)"/><div v-else-if="b.type==='code'" class="test-doc-code"><div><span>{{ b.language||'Example' }}</span><button class="test-link" @click="copy(b.text,String(index))">{{ copied===String(index)?'Copied':'Copy example' }}</button></div><pre tabindex="0"><code>{{ b.text }}</code></pre></div><component :is="b.ordered?'ol':'ul'" v-else-if="b.type==='list'"><li v-for="(item,j) in b.items" :key="j" v-html="inline(item)"/></component><div v-else-if="b.type==='table'" class="test-doc-table"><table><thead><tr><th v-for="(cell,j) in b.header" :key="j" v-html="inline(cell)"/></tr></thead><tbody><tr v-for="(row,j) in b.rows" :key="j"><td v-for="(cell,k) in row" :key="k" v-html="inline(cell)"/></tr></tbody></table></div><blockquote v-else-if="b.type==='quote'" v-html="inline(b.text)"/><hr v-else-if="b.type==='rule'"/><p v-else v-html="inline(b.text)"/></template></article>
    <p class="test-caption test-doc-footnote">Upstream documentation. Examples may contain placeholders or outdated instructions; review them before use. Embedded HTML and remote images are displayed as text or links.</p><p v-if="copied==='failed'" role="alert">Could not copy to the clipboard.</p>
   </div><div v-else class="test-empty"><Icon name="file"/><h4>{{ busy?'Opening the field guide…':'Documentation lives with the tests.' }}</h4><p>Load a test catalog to browse every README under validation.</p></div>
  </div>
 </section>
</template>
