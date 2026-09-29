<script setup>
import {computed,nextTick,ref} from 'vue';
import {codeTokens} from './helmlab.mjs';
import {inspectConfig,indentSelection,CONFIG_LIMIT} from './test-lab-workspace.mjs';
const props=defineProps({modelValue:{type:String,default:''},label:{type:String,default:'cattle-config.yml'}});
const emit=defineEmits(['update:modelValue']);
const input=ref(null),numbers=ref(null),highlight=ref(null),position=ref({line:1,column:1}),find=ref(''),showFind=ref(false),allowTab=ref(false);
const health=computed(()=>inspectConfig(props.modelValue));
const tokens=computed(()=>codeTokens(props.modelValue,true));
const lines=computed(()=>props.modelValue.split('\n').length);
const size=computed(()=>new TextEncoder().encode(props.modelValue).length);
function cursor(){const el=input.value;if(!el)return;const before=el.value.slice(0,el.selectionStart).split('\n');position.value={line:before.length,column:before.at(-1).length+1};}
function scroll(){if(numbers.value)numbers.value.scrollTop=input.value.scrollTop;if(highlight.value){highlight.value.scrollTop=input.value.scrollTop;highlight.value.scrollLeft=input.value.scrollLeft;}}
async function replace(value){emit('update:modelValue',value.text);await nextTick();input.value.setSelectionRange(value.start,value.end);cursor();}
function key(event){const el=input.value;if(event.key==='Escape'){allowTab.value=true;showFind.value=false;return;}if(event.key==='Tab'&&!allowTab.value){event.preventDefault();replace(indentSelection(el.value,el.selectionStart,el.selectionEnd,event.shiftKey));}else if(event.key==='Enter'){event.preventDefault();const start=el.selectionStart;const indent=el.value.slice(0,start).split('\n').at(-1).match(/^ */)[0];const insertion='\n'+indent;replace({text:el.value.slice(0,start)+insertion+el.value.slice(el.selectionEnd),start:start+insertion.length,end:start+insertion.length});}else if((event.metaKey||event.ctrlKey)&&event.key==='f'){event.preventDefault();showFind.value=true;nextTick(()=>el.closest('.test-yaml-editor').querySelector('input[type=search]')?.focus());}}
function findNext(){const el=input.value;if(!find.value)return;const text=el.value.toLowerCase(),word=find.value.toLowerCase();let index=text.indexOf(word,el.selectionEnd);if(index<0)index=text.indexOf(word);if(index>=0){el.focus();el.setSelectionRange(index,index+word.length);el.scrollTop=Math.max(0,(text.slice(0,index).split('\n').length-4)*22);scroll();cursor();}}
</script>
<template>
 <div class="test-yaml-editor" :class="{'has-error':!health.valid}">
  <div class="test-editor-title"><strong>{{ label }}</strong><button class="test-link" :aria-expanded="showFind" @click="showFind=!showFind">Find <kbd>⌘ / Ctrl F</kbd></button></div>
  <div v-if="showFind" class="test-editor-find"><input v-model="find" type="search" aria-label="Find in YAML" placeholder="Find a field or value…" @keydown.enter="findNext"/><button class="test-button" @click="findNext">Find next</button></div>
  <div class="test-editor-body"><pre ref="numbers" class="test-editor-lines" aria-hidden="true">{{ Array.from({length:lines},(_,i)=>i+1).join('\n') }}</pre><div class="test-editor-surface"><pre ref="highlight" class="test-editor-highlight" aria-hidden="true"><span v-for="(token,i) in tokens" :key="i" :class="token.kind?'test-yaml-'+token.kind:undefined">{{ token.text }}</span>{{ '\n' }}</pre><textarea ref="input" :value="modelValue" :aria-label="label" :aria-invalid="!health.valid" spellcheck="false" autocomplete="off" autocapitalize="off" autocorrect="off" wrap="off" @input="emit('update:modelValue',$event.target.value);cursor()" @keydown="key" @click="cursor" @keyup="cursor" @scroll="scroll" @focus="allowTab=false" /></div></div>
  <div class="test-editor-status"><span :class="{'test-error-text':!health.valid}">{{ health.message }}</span><span>Ln {{ position.line }}, Col {{ position.column }} · {{ lines }} lines · {{ (size/1024).toFixed(1) }} / {{ CONFIG_LIMIT/1024 }} KiB</span></div>
 </div>
 <p class="test-caption">Two-space indentation · Tab to indent, Shift Tab to outdent · Esc then Tab to leave the editor. Credentials are visible in YAML.</p>
</template>
