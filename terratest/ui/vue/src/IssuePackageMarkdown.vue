<script setup>
import { computed, ref } from 'vue';
import { packageMarkdownBlocks, packageMarkdownInline } from './issue-package-markdown.mjs';
import { apiFetch } from './store.js';
const props=defineProps({content:{type:String,default:''}});
const blocks=computed(()=>packageMarkdownBlocks(props.content));
const error=ref('');
const inline=text=>packageMarkdownInline(text);
async function openLink(event){const anchor=event.target.closest('a');if(!anchor)return;event.preventDefault();try{const response=await apiFetch('/api/open-url',{method:'POST',body:JSON.stringify({url:anchor.href})});if(!response.ok)throw new Error(await response.text());}catch(err){error.value=err.message;}}
</script>
<template>
  <article class="package-markdown" aria-label="Report preview" @click="openLink">
    <p v-if="error" role="alert">{{ error }}</p>
    <template v-for="(block,index) in blocks" :key="index">
      <component :is="'h'+Math.min(block.level+1,6)" v-if="block.type==='heading'" v-html="inline(block.text)"/>
      <pre v-else-if="block.type==='code'" tabindex="0"><code>{{ block.text }}</code></pre>
      <component :is="block.ordered?'ol':'ul'" v-else-if="block.type==='list'"><li v-for="(item,j) in block.items" :key="j" v-html="inline(item)"/></component>
      <div v-else-if="block.type==='table'" class="package-markdown-table"><table><thead><tr><th v-for="(cell,j) in block.header" :key="j" v-html="inline(cell)"/></tr></thead><tbody><tr v-for="(row,j) in block.rows" :key="j"><td v-for="(cell,k) in row" :key="k" v-html="inline(cell)"/></tr></tbody></table></div>
      <blockquote v-else-if="block.type==='quote'" v-html="inline(block.text)"/>
      <hr v-else-if="block.type==='rule'"/>
      <p v-else v-html="inline(block.text)"/>
    </template>
  </article>
</template>
<style scoped>
.package-markdown { color:var(--runway-ink); font-size:.88rem; line-height:1.8; overflow-wrap:anywhere; }
.package-markdown :deep(h2),.package-markdown :deep(h3),.package-markdown :deep(h4) { margin:1.6rem 0 .75rem; font-weight:650; line-height:1.3; letter-spacing:-.025em; }
.package-markdown :deep(h2) {font-size:1.55rem;} .package-markdown :deep(h3){font-size:1.2rem}.package-markdown :deep(h4){font-size:1rem}
.package-markdown :deep(p) {margin:.8rem 0;color:var(--runway-muted)}
.package-markdown :deep(a) {color:var(--runway-accent);text-decoration:underline;text-underline-offset:3px}
.package-markdown :deep(ul),.package-markdown :deep(ol){padding-left:1.4rem;margin:.75rem 0}.package-markdown :deep(ul){list-style:disc}.package-markdown :deep(ol){list-style:decimal}
.package-markdown :deep(li){padding:.2rem 0}.package-markdown :deep(code){font-size:.78rem;background:var(--runway-soft);padding:.12rem .3rem;border-radius:4px}.package-markdown pre {padding:1rem;background:var(--runway-soft);border:1px solid var(--runway-border);border-radius:10px;overflow:auto}.package-markdown pre code{background:none;padding:0}
.package-markdown-table{overflow:auto;margin:1rem 0;border:1px solid var(--runway-border);border-radius:10px}.package-markdown table{width:100%;border-collapse:collapse;font-size:.8rem}.package-markdown th{text-align:left;font-weight:600;background:var(--runway-soft)}.package-markdown th,.package-markdown td{padding:.6rem .8rem;border-bottom:1px solid var(--runway-border)}.package-markdown tr:last-child td{border-bottom:0}.package-markdown blockquote{border-left:3px solid var(--runway-accent);padding:.5rem 1rem;margin:1rem 0;color:var(--runway-muted)}.package-markdown hr{border:0;border-top:1px solid var(--runway-border);margin:1.5rem 0}
</style>
