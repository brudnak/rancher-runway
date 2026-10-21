<script setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import Automation from './IssuePackageAutomation.vue';
import {caseFieldWritingProblem, formatCaseSteps, reconcileCaseSteps} from './issue-package-case-editor.mjs';

const props = defineProps({modelValue:{type:Object,required:true}, number:{type:Number,default:1}, disabled:Boolean, removable:{type:Boolean,default:true}, automationEditable:{type:Boolean,default:true}});
const emit = defineEmits(['update:modelValue','remove']);
const root = ref(null), writingError = ref(''), fieldErrors = ref({});
const stepText = ref(formatCaseSteps(props.modelValue.steps));
let acceptedText = stepText.value, emittedText = stepText.value;
let resizeObserver, observedWidth;
const steps = computed(() => props.modelValue.steps || []);
const automationLabel = computed(() => ({manual:'Manual coverage', planned:'Automation planned', automated:'Automated coverage available'}[props.modelValue.automation] || 'Manual coverage'));
const set = (key, value) => {if (!props.disabled) emit('update:modelValue', {...props.modelValue, [key]:value});};

function writeField(key, event) {
  if (props.disabled) return;
  if (key === 'title') event.target.value = event.target.value.replace(/[\r\n]+/g, ' ');
  const error = caseFieldWritingProblem(key, event.target.value);
  fieldErrors.value = {...fieldErrors.value, [key]:error};
  if (error) event.target.value = props.modelValue[key] || '';
  else set(key, event.target.value);
  if (event.target.tagName === 'TEXTAREA') grow(event);
}

function titleKey(event) {
  if (event.key === 'Enter' && !event.isComposing) event.preventDefault();
}

function grow(event) {
  const fields = event?.target ? [event.target] : root.value?.querySelectorAll('textarea') || [];
  for (const field of fields) {
    field.style.height = 'auto';
    field.style.height = `${field.scrollHeight}px`;
  }
}

function writeSteps(event) {
  if (props.disabled) return;
  const result = reconcileCaseSteps(steps.value, event.target.value);
  writingError.value = result.error || '';
  if (result.error) {
    event.target.value = acceptedText;
    stepText.value = acceptedText;
    return;
  }
  stepText.value = acceptedText = event.target.value;
  emittedText = formatCaseSteps(result.steps);
  set('steps', result.steps);
  grow(event);
}

watch(() => formatCaseSteps(props.modelValue.steps), value => {
  if (value !== emittedText) stepText.value = acceptedText = value;
  emittedText = value;
  nextTick(grow);
});
watch(() => props.modelValue.title, () => nextTick(grow));
onMounted(() => {
  nextTick(grow);
  if (typeof ResizeObserver === 'undefined') return;
  resizeObserver = new ResizeObserver(entries => {
    const width = entries[0]?.contentRect.width;
    if (width === observedWidth) return;
    observedWidth = width;
    grow();
  });
  if (root.value) resizeObserver.observe(root.value);
});
onBeforeUnmount(() => resizeObserver?.disconnect());
</script>

<template>
  <section ref="root" class="package-case-editor" aria-label="Manual test case editor">
    <header class="pce-header"><span>CASE {{ String(number).padStart(2,'0') }}</span><span>{{ steps.length ? `${steps.length} ${steps.length === 1 ? 'step' : 'steps'}` : 'Details can come later' }}</span></header>

    <label class="pce-title"><span class="pce-sr-only">Case title</span><textarea :value="modelValue.title" :disabled="disabled" rows="1" :aria-invalid="!!fieldErrors.title" :aria-describedby="fieldErrors.title ? `case-${modelValue.id}-title-error` : undefined" placeholder="What do you want to test?" @keydown="titleKey" @input="writeField('title',$event)"/></label>
    <p v-if="fieldErrors.title" :id="`case-${modelValue.id}-title-error`" class="pce-error" role="alert">{{fieldErrors.title}} Your previous title is unchanged.</p>

    <label class="pce-field pce-steps"><span>Steps</span><textarea :value="stepText" :disabled="disabled" rows="6" spellcheck="true" :aria-invalid="!!writingError" :aria-describedby="`case-${modelValue.id}-steps-hint`" placeholder="1. Start with the first thing you do&#10;2. Write the next action&#10;3. Keep going as you test…" @input="writeSteps"/><span :id="`case-${modelValue.id}-steps-hint`" class="pce-hint">One step per line. Numbers are optional. Indent a line to continue a step.</span></label>
    <p v-if="writingError" class="pce-error" role="alert">{{ writingError }} Your previous writing is unchanged.</p>
    <details class="pce-writing-help"><summary>Adding a check to a step</summary><p>Put an indented <code>Expected:</code> line below its action. Existing step checks stay here too.</p><pre>1. Open the project
   Expected: Only permitted namespaces appear.
2. Change the project role</pre></details>

    <label class="pce-field"><span>Expected outcome <small>optional</small></span><textarea :value="modelValue.expected" :disabled="disabled" :aria-invalid="!!fieldErrors.expected" :aria-describedby="fieldErrors.expected ? `case-${modelValue.id}-expected-error` : undefined" rows="2" placeholder="What should be true when this case passes?" @input="writeField('expected',$event)"/></label>
    <p v-if="fieldErrors.expected" :id="`case-${modelValue.id}-expected-error`" class="pce-error" role="alert">{{fieldErrors.expected}} Your previous writing is unchanged.</p>
    <label class="pce-field"><span>Setup & notes <small>optional</small></span><textarea :value="modelValue.preconditions" :disabled="disabled" :aria-invalid="!!fieldErrors.preconditions" :aria-describedby="fieldErrors.preconditions ? `case-${modelValue.id}-preconditions-error` : undefined" rows="2" placeholder="Anything you need before testing — an account, a setting, or starting data…" @input="writeField('preconditions',$event)"/></label>
    <p v-if="fieldErrors.preconditions" :id="`case-${modelValue.id}-preconditions-error`" class="pce-error" role="alert">{{fieldErrors.preconditions}} Your previous writing is unchanged.</p>

    <details v-if="automationEditable" class="pce-automation"><summary><span>Automation</span><small>{{ automationLabel }}</small><Icon name="chevron"/></summary><div class="pce-automation-body"><label><span>Coverage</span><select :value="modelValue.automation" :disabled="disabled" @change="set('automation',$event.target.value)"><option value="manual">Manual</option><option value="planned">Automation planned</option><option value="automated">Automated coverage available</option></select></label><label><span>Test or pull request URL <small>optional</small></span><input :value="modelValue.automationUrl" :disabled="disabled" :aria-invalid="!!fieldErrors.automationUrl" :aria-describedby="fieldErrors.automationUrl ? `case-${modelValue.id}-automation-url-error` : undefined" placeholder="https://github.com/rancher/tests/…" @input="writeField('automationUrl',$event)"/></label><p v-if="fieldErrors.automationUrl" :id="`case-${modelValue.id}-automation-url-error`" class="pce-error" role="alert">{{fieldErrors.automationUrl}} Your previous URL is unchanged.</p><Automation :model-value="modelValue" :disabled="disabled" @update:model-value="$emit('update:modelValue',$event)"/></div></details>

    <footer class="pce-footer"><span>A title is enough to begin.</span><button v-if="removable" type="button" :disabled="disabled" @click="$emit('remove')">Remove case</button></footer>
  </section>
</template>

<style scoped>
.package-case-editor{max-width:740px;min-width:0;margin:0 auto;color:var(--runway-ink)}
.pce-header{display:flex;align-items:center;justify-content:space-between;gap:16px;margin:0 0 23px;color:var(--runway-muted);font-size:11px}.pce-header>span:first-child{font-size:10px;font-weight:650;letter-spacing:.16em}
.package-case-editor label{display:grid;gap:9px;min-width:0}.package-case-editor label>span:first-child{display:flex;align-items:center;gap:10px;font-size:12px;font-weight:600}.package-case-editor label small{font-size:10px;font-weight:400;color:var(--runway-muted)}
.package-case-editor input,.package-case-editor textarea,.package-case-editor select{box-sizing:border-box;width:100%;min-width:0;border:1px solid transparent;border-radius:8px;background:transparent;color:var(--runway-ink);font:inherit;font-size:14px;font-weight:400;line-height:1.85;padding:10px 11px;transition:background .15s,border-color .15s}.package-case-editor textarea{resize:vertical;overflow:hidden;min-height:80px}.package-case-editor input:focus,.package-case-editor textarea:focus{outline:none;border-color:var(--runway-border);background:var(--runway-input)}.package-case-editor input:disabled,.package-case-editor textarea:disabled{opacity:.55}.package-case-editor ::placeholder{color:var(--runway-muted);opacity:.65;font-weight:400}
.pce-title textarea{padding:0 10px 12px;margin-left:-10px;width:calc(100% + 20px);min-height:0;resize:none;border-radius:5px;font-size:27px;line-height:1.45;font-weight:550;letter-spacing:-.035em}.pce-field{margin-top:29px}.pce-field textarea{margin-left:-11px;width:calc(100% + 22px)}.pce-steps textarea{min-height:184px;font-size:15px;line-height:2}.pce-hint{color:var(--runway-muted);font-size:10px;font-weight:400;line-height:1.6}
.pce-writing-help{margin-top:6px;color:var(--runway-muted);font-size:10px;line-height:1.7}.pce-writing-help summary{cursor:pointer;width:fit-content}.pce-writing-help p{margin:10px 0 6px}.pce-writing-help pre{padding:12px 15px;white-space:pre-wrap;border-radius:7px;background:var(--runway-soft);font:11px/1.8 ui-monospace,monospace}.pce-error{color:var(--runway-error);font-size:12px;line-height:1.6}
.pce-automation{margin-top:32px;padding-top:16px;border-top:1px solid var(--runway-border)}.pce-automation>summary{display:flex;align-items:center;gap:12px;list-style:none;cursor:pointer;color:var(--runway-muted);font-size:12px}.pce-automation>summary::-webkit-details-marker{display:none}.pce-automation summary small{margin-left:auto;font-size:10px}.pce-automation summary svg{width:13px;height:13px;transition:transform .15s}.pce-automation[open]>summary svg{transform:rotate(180deg)}.pce-automation-body{display:grid;gap:16px;padding-top:20px}.pce-automation-body input,.pce-automation-body select{border-color:var(--runway-border);background:var(--runway-input);font-size:12px}.pce-automation-body p{font-size:11px;line-height:1.7;margin:0;color:var(--runway-muted)}
.pce-footer{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-top:28px;padding-top:15px;border-top:1px solid var(--runway-border);color:var(--runway-muted);font-size:10px}.pce-footer button{border:0;padding:4px 0;background:none;color:var(--runway-muted);font:inherit;cursor:pointer}.pce-footer button:hover:not(:disabled){color:var(--runway-error)}.pce-footer button:disabled{opacity:.5;cursor:not-allowed}.package-case-editor button:focus-visible,.package-case-editor summary:focus-visible,.package-case-editor select:focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}
.pce-sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
.pce-automation-body .pce-error{color:var(--runway-error)}
@media(max-width:650px){.pce-title textarea{font-size:23px}.pce-header{gap:10px}.pce-steps textarea{font-size:14px}.pce-automation summary small{font-size:9px}}
@media(prefers-reduced-motion:reduce){.package-case-editor *{transition:none!important}}
</style>
