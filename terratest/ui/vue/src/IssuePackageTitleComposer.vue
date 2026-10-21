<script setup>
import {computed, onBeforeUnmount, onMounted, ref, useId} from 'vue';
import {parseCaseTitles, caseTitlesError} from './issue-package-titles.mjs';

const props = defineProps({modelValue:{type:String,default:''}, disabled:Boolean, existingCount:{type:Number,default:0}, packageTitle:String, saveStatus:String, saveError:String});
const emit = defineEmits(['update:modelValue', 'create', 'cancel']);
const id = useId(), dialog = ref(null), writing = ref(null);
const titles = computed(() => parseCaseTitles(props.modelValue));
const error = computed(() => caseTitlesError(titles.value, props.existingCount));
const canCreate = computed(() => !props.disabled && titles.value.length > 0 && !error.value);
const example = '1. Test case one\n2. Test something else here\n3. Test another thing here\n4. And something else here';
let previousOverflow;

onMounted(() => {
  previousOverflow = document.body.style.overflow;
  document.body.style.overflow = 'hidden';
  dialog.value.showModal();
  writing.value?.focus({preventScroll:true});
});
onBeforeUnmount(() => {
  dialog.value?.close();
  document.body.style.overflow = previousOverflow;
});
function create() {
  if (canCreate.value) emit('create', titles.value);
}
function shortcut(event) {
  if (event.key !== 'Enter' || event.isComposing || (!event.metaKey && !event.ctrlKey) || event.altKey || event.shiftKey) return;
  event.preventDefault();
  create();
}
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="title-composer" :aria-labelledby="`${id}-heading`" @cancel.prevent="emit('cancel')" @keydown.stop>
      <div class="tc-topbar">
        <div class="tc-context"><span class="tc-eyebrow">TEST PLAN</span><span class="tc-package">{{ packageTitle }}</span></div>
        <div class="tc-top-actions"><span class="tc-save" role="status">{{ saveStatus }}</span><button type="button" class="tc-cancel" aria-keyshortcuts="Escape" @click="emit('cancel')">Back to plan <span aria-hidden="true">↗</span></button></div>
      </div>
      <header class="tc-heading">
        <h2 :id="`${id}-heading`">What do you want to test?</h2>
        <p :id="`${id}-hint`">One case per line. Details can come later.</p>
      </header>
      <label class="tc-sr-only" :for="`${id}-titles`">Test case titles</label>
      <textarea ref="writing" :id="`${id}-titles`" class="tc-writing" :value="modelValue" :disabled="disabled" :placeholder="example" :aria-describedby="`${id}-hint${error ? ` ${id}-error` : ''}`" :aria-invalid="!!error" rows="4" spellcheck="true" autofocus @input="emit('update:modelValue', $event.target.value)" @keydown="shortcut"/>
      <p v-if="error || saveError" :id="`${id}-error`" class="tc-error" role="alert">{{ error || saveError }}</p>
      <footer>
        <div class="tc-count" aria-live="polite" aria-atomic="true">{{ titles.length }} {{ titles.length === 1 ? 'test case' : 'test cases' }}<small v-if="existingCount"> · {{ existingCount }} already in your plan</small></div>
        <div class="tc-actions"><span class="tc-shortcut"><kbd>⌘ / Ctrl</kbd> + <kbd>Enter</kbd></span><button type="button" class="tc-create" :disabled="!canCreate" aria-keyshortcuts="Control+Enter Meta+Enter" @click="create">{{ existingCount ? 'Add cases' : 'Create test plan' }}</button></div>
      </footer>
    </dialog>
  </Teleport>
</template>

<style scoped>
.title-composer{box-sizing:border-box;position:fixed;inset:0;width:100%;height:100vh;height:100dvh;max-width:none;max-height:none;margin:0;padding:0 clamp(24px,5vw,88px);border:0;background:var(--runway-card);color:var(--runway-ink);font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,sans-serif;overflow:auto;overscroll-behavior:contain}
.title-composer[open]{display:flex;flex-direction:column}.title-composer::backdrop{background:var(--runway-card)}
.tc-topbar{display:flex;justify-content:space-between;align-items:center;gap:24px;min-height:80px;flex-shrink:0;border-bottom:1px solid var(--runway-border)}.tc-context{display:flex;align-items:center;gap:20px;min-width:0}.tc-eyebrow{font-size:10px;font-weight:650;letter-spacing:.16em;color:var(--runway-accent);white-space:nowrap}.tc-package{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px;color:var(--runway-muted)}.tc-top-actions{display:flex;align-items:center;gap:24px;flex-shrink:0}.tc-save{font-size:11px;color:var(--runway-muted)}
.tc-heading{flex-shrink:0;padding:clamp(24px,5vh,56px) 0 24px}.tc-heading h2{margin:0 0 12px;font-size:clamp(25px,3vw,36px);font-weight:550;line-height:1.2;letter-spacing:-.04em}.tc-heading p{margin:0;color:var(--runway-muted);font-size:13px;line-height:1.5}
.title-composer .tc-writing{box-sizing:border-box;display:block;flex:1 0 160px;width:100%;min-height:160px;margin:0;padding:10px 0 24px;color:var(--runway-ink);caret-color:var(--runway-ink);background:transparent;border:0;border-radius:0;box-shadow:none;font:400 16px/1.4 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,sans-serif;resize:none;outline:none;overscroll-behavior:contain}
.tc-writing::placeholder{color:var(--runway-muted);opacity:.65}.tc-writing[aria-invalid=true]{box-shadow:inset 2px 0 var(--runway-error);padding-left:12px}.tc-writing:disabled{opacity:.6;cursor:wait}
.title-composer footer{display:flex;align-items:center;justify-content:space-between;gap:20px;flex-shrink:0;min-height:86px;border-top:1px solid var(--runway-border)}.tc-count{font-size:12px;color:var(--runway-muted)}.tc-count small{font:inherit}.tc-actions{display:flex;align-items:center;gap:20px}.title-composer button{font:inherit;font-size:12px;font-weight:550;cursor:pointer}.title-composer button:disabled{opacity:.4;cursor:not-allowed}.title-composer button:focus-visible{outline:2px solid var(--runway-accent);outline-offset:4px}.tc-create{border:1px solid var(--runway-accent);border-radius:8px;padding:12px 20px;background:var(--runway-accent);color:var(--runway-soft);white-space:nowrap}.tc-create:hover:not(:disabled){filter:brightness(1.06)}.tc-cancel{display:flex;align-items:center;gap:14px;padding:10px 0;border:0;background:none;color:var(--runway-muted);white-space:nowrap}.tc-cancel:hover{color:var(--runway-ink)}.tc-cancel span{font-size:18px}
.tc-shortcut{font-size:10px;color:var(--runway-muted)}.tc-shortcut kbd{font:inherit}.tc-error{flex-shrink:0;margin:12px 0;color:var(--runway-error);font-size:12px;line-height:1.5}
.tc-sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
@media(max-width:650px){.title-composer{padding:0 24px}.tc-topbar{min-height:64px}.tc-package,.tc-save,.tc-shortcut{display:none}.tc-heading{padding:28px 0 20px}.title-composer footer{min-height:80px}.tc-count small{display:block;margin-top:5px}.tc-create{padding:12px 16px}}
@media(max-height:500px){.tc-topbar{min-height:52px}.tc-heading{padding:20px 0 12px}.tc-heading h2{font-size:24px}.title-composer footer{min-height:64px}}
</style>
