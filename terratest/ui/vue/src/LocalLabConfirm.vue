<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import LabIcon from './HelmLabIcon.vue';
import { CONFIRMATION_TEXT, isConfirmed } from './confirmation.mjs';
const props=defineProps({title:String,message:String,busy:Boolean,disabled:Boolean,returnFocus:Object,buttonLabel:{type:String,default:'Confirm removal'}});
defineEmits(['confirm','cancel']);
const input=ref(''),field=ref(null);
const previousFocus=document.activeElement;
const valid=computed(()=>isConfirmed(input.value));
watch([()=>props.title,()=>props.message],()=>{input.value='';});
onMounted(()=>nextTick(()=>{field.value?.closest('.lab-confirm')?.scrollIntoView({block:'nearest',behavior:'auto'});field.value?.focus({preventScroll:true});}));
onBeforeUnmount(()=>nextTick(()=>{const target=props.returnFocus || previousFocus;if(target?.isConnected)target.focus({preventScroll:true});}));
</script>
<template>
  <div class="lab-confirm" role="group" :aria-label="title" @keydown.esc.prevent="!busy && $emit('cancel')"><div class="lab-confirm-title"><LabIcon name="signal" /><strong>{{ title }}</strong></div><p>{{ message }}</p><slot /><label class="typed-confirm-label"><span class="typed-confirm-prompt">Type <strong>{{ CONFIRMATION_TEXT }}</strong> to continue.</span><input ref="field" v-model="input" :placeholder="CONFIRMATION_TEXT" aria-label="Type confirm to continue" autocomplete="off" autocapitalize="none" autocorrect="off" spellcheck="false" :disabled="busy" @keydown.enter.prevent="valid && !busy && !disabled && $emit('confirm')"></label><div class="lab-button-row"><button type="button" class="lab-danger" :disabled="!valid || busy || disabled" @click="$emit('confirm')">{{ busy ? 'Working…' : buttonLabel }}</button><button type="button" :disabled="busy" @click="$emit('cancel')">Cancel</button></div></div>
</template>

<style scoped>
.lab-confirm { color:var(--runway-ink); border:1px solid var(--runway-error); background:var(--runway-soft); }
.lab-confirm-title { color:var(--runway-error); }
.lab-confirm > p { color:var(--runway-muted); margin:.55rem 0; }
.lab-confirm input { display:block; width:100%; max-width:26rem; min-height:2.75rem; padding:.65rem .8rem; border:1px solid var(--runway-control); border-radius:.55rem; background:var(--runway-input); color:var(--runway-ink); font:inherit; line-height:1.5; }
.lab-confirm input::placeholder { color:var(--runway-muted); opacity:1; }
.lab-confirm button { display:inline-flex; align-items:center; justify-content:center; min-height:2.5rem; padding:.55rem .8rem; border:1px solid var(--runway-border); border-radius:.55rem; background:var(--runway-raised); color:var(--runway-ink); font:inherit; font-size:.8rem; font-weight:550; cursor:pointer; }
.lab-confirm .lab-danger { color:var(--runway-error); }
.lab-confirm button:hover:not(:disabled) { border-color:var(--runway-accent); }
.lab-confirm button:disabled { opacity:.45; cursor:not-allowed; }
.lab-confirm :focus-visible { outline:2px solid var(--runway-accent); outline-offset:3px; }
</style>
