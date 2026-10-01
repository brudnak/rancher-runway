<script setup>
import {computed,ref} from 'vue';
import {filterMilestones} from './milestones.mjs';
const props=defineProps({items:{type:Array,required:true},disabled:Boolean,selected:{type:Number,default:0}});
const emit=defineEmits(['select']);
const search=ref('');
const matches=computed(()=>filterMilestones(props.items,search.value));
function select(event){const item=props.items.find(item=>String(item.number)===event.target.value);if(item)emit('select',item);}
</script>
<template>
 <div class="milestone-options">
  <input v-model="search" type="search" aria-label="Search GitHub milestones" placeholder="Search milestones…" autocomplete="off" autocapitalize="none" autocorrect="off" spellcheck="false" :disabled="disabled"/>
  <select v-if="matches.length" aria-label="Choose a repository milestone" :size="Math.min(6,matches.length+1)" :value="selected" :disabled="disabled" @change="select"><option value="0" disabled>Choose a milestone…</option><option v-for="item in matches" :key="item.number" :value="item.number">{{item.title}}{{item.state==='closed'?' · closed':''}}</option></select>
  <p role="status">{{matches.length?`${matches.length} of ${items.length} milestones`:'No matching milestones. Try another search.'}}</p>
 </div>
</template>
<style scoped>
.milestone-options{display:grid;gap:8px;padding:10px;border:1px solid var(--runway-border,#dfe7e4);border-radius:10px;background:var(--runway-card,#fff);min-width:0}.milestone-options input,.milestone-options select{width:100%;min-width:0;background:var(--runway-card,#fff);color:var(--runway-ink,#203230);border:1px solid var(--runway-border,#dfe7e4);border-radius:7px;padding:8px;font-size:12px}.milestone-options option{padding:7px 8px}.milestone-options p{font-size:11px;color:var(--runway-muted,#657577);font-weight:400}.milestone-options :focus-visible{outline:2px solid #319476;outline-offset:2px}
</style>
