<script setup>
import {ref,watch} from 'vue';
const props=defineProps({name:String,disabled:Boolean,label:{type:String,default:'Name'}});
const emit=defineEmits(['save']);
const editing=ref(false),draft=ref('');
watch(()=>props.name,()=>{editing.value=false;});
function save(){if(props.disabled||!draft.value.trim())return;emit('save',draft.value.trim());editing.value=false;}
</script>
<template><div class="lab-record-name"><template v-if="!editing"><strong>{{name}}</strong><button type="button" :disabled="disabled" :aria-label="`Rename ${name}`" @click="draft=name;editing=true">Rename</button></template><form v-else @submit.prevent="save"><label>{{label}}<input v-model="draft" maxlength="100" :disabled="disabled"/></label><button type="submit" :disabled="disabled||!draft.trim()">Save</button><button type="button" :disabled="disabled" @click="editing=false">Cancel</button></form></div></template>
<style scoped>
.lab-record-name{display:flex;flex-wrap:wrap;align-items:center;gap:.8rem}.lab-record-name>strong{font-size:1.05rem;color:var(--runway-ink);overflow-wrap:anywhere}.lab-record-name button{font:inherit;font-size:.74rem;color:var(--runway-accent);cursor:pointer}.lab-record-name form{display:flex;flex-wrap:wrap;align-items:end;gap:.65rem}.lab-record-name label{display:grid;gap:.3rem;color:var(--runway-muted);font-size:.75rem}.lab-record-name input{padding:.55rem .65rem;border:1px solid var(--runway-border);border-radius:7px;background:var(--runway-input);color:var(--runway-ink)}.lab-record-name button:disabled{opacity:.45;cursor:not-allowed}
</style>
