<script setup>
import {computed} from 'vue';
const props=defineProps({modelValue:{type:String,default:''}});
const emit=defineEmits(['update:modelValue']);
const monthNames=['January','February','March','April','May','June','July','August','September','October','November','December'];
const year=computed(()=>props.modelValue?.slice(0,4)||String(new Date().getFullYear()));
const month=computed(()=>props.modelValue?.slice(5,7)||String(new Date().getMonth()+1).padStart(2,'0'));
const years=Array.from({length:201},(_,i)=>String(2000+i));
function change(part,value){emit('update:modelValue',part==='year'?`${value}-${month.value}`:`${year.value}-${value}`);}
</script>
<template><div class="release-month-picker" role="group" aria-label="Release month and year"><select :value="month" aria-label="Calendar month" @change="change('month',$event.target.value)"><option v-for="(name,index) in monthNames" :key="name" :value="String(index+1).padStart(2,'0')">{{name}}</option></select><select :value="year" aria-label="Calendar year" @change="change('year',$event.target.value)"><option v-for="value in years" :key="value" :value="value">{{value}}</option></select></div></template>
<style scoped>.release-month-picker{display:flex;gap:8px}.release-month-picker select{min-width:0;padding:9px;border:1px solid var(--runway-border);background:var(--runway-card);color:var(--runway-ink);border-radius:6px;font:inherit}.release-month-picker select:first-child{flex:1}.release-month-picker select:focus-visible{outline:2px solid var(--runway-accent);outline-offset:2px}</style>
