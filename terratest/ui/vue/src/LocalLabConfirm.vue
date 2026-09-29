<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import LabIcon from './HelmLabIcon.vue';
const props=defineProps({title:String,message:String,phrase:String,busy:Boolean,disabled:Boolean,returnFocus:Object,buttonLabel:{type:String,default:'Confirm removal'}});
defineEmits(['confirm','cancel']);
const input=ref(''),field=ref(null);
const previousFocus=document.activeElement;
const valid=computed(()=>input.value===props.phrase);
onMounted(()=>nextTick(()=>{field.value?.closest('.lab-confirm')?.scrollIntoView({block:'nearest',behavior:'auto'});field.value?.focus({preventScroll:true});}));
onBeforeUnmount(()=>nextTick(()=>{const target=props.returnFocus || previousFocus;if(target?.isConnected)target.focus({preventScroll:true});}));
</script>
<template>
  <div class="lab-confirm" role="group" :aria-label="title" @keydown.esc.prevent="!busy && $emit('cancel')"><div class="lab-confirm-title"><LabIcon name="signal" /><strong>{{ title }}</strong></div><p>{{ message }}</p><slot /><label>Type <code>{{ phrase }}</code> to confirm<input ref="field" v-model="input" :aria-label="`Type ${phrase} to confirm`" autocomplete="off" spellcheck="false" :disabled="busy" @keydown.enter.prevent="valid && !busy && !disabled && $emit('confirm')"></label><div class="lab-button-row"><button type="button" class="lab-danger" :disabled="!valid || busy || disabled" @click="$emit('confirm')">{{ busy ? 'Working…' : buttonLabel }}</button><button type="button" :disabled="busy" @click="$emit('cancel')">Cancel</button></div></div>
</template>
