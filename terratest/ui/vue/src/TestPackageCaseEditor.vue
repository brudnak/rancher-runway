<script setup>
import {computed, nextTick, ref} from 'vue';
import Icon from './HelmLabIcon.vue';
import {MAX_CASE_STEPS, insertCaseStep, removeCaseStep, restoreCaseStep, caseWritingSummary} from './test-package-case-editor.mjs';

const props = defineProps({modelValue:{type:Object,required:true}, number:{type:Number,default:1}, disabled:Boolean});
const emit = defineEmits(['update:modelValue','remove']);
const mode = ref('write'), root = ref(null), removedSteps = ref([]), announcement = ref('');
const steps = computed(() => props.modelValue.steps || []);
const summary = computed(() => caseWritingSummary(props.modelValue));
const canAdd = computed(() => !props.disabled && steps.value.length < MAX_CASE_STEPS);
const automationLabel = computed(() => ({manual:'Manual coverage', planned:'Automation planned', automated:'Automated coverage available'}[props.modelValue.automation] || 'Manual coverage'));
const set = (key, value) => {if (!props.disabled) emit('update:modelValue', {...props.modelValue, [key]:value});};
const stepSet = (id, key, value) => set('steps', steps.value.map(step => step.id === id ? {...step, [key]:value} : step));
const say = text => {announcement.value = text;};

async function focusStep(id) {
  await nextTick();
  const row = [...(root.value?.querySelectorAll('[data-step-id]') || [])].find(element => element.dataset.stepId === id);
  row?.querySelector('textarea')?.focus({preventScroll:true});
  row?.scrollIntoView({block:'nearest', behavior:'auto'});
}

async function insert(index = steps.value.length, source = null, event) {
  if (!canAdd.value) return;
  closeMenu(event);
  const result = insertCaseStep(steps.value, index, source);
  if (!result) return;
  set('steps', result.steps);
  say(source ? `Step ${index} duplicated. Edit the new step ${index + 1}.` : `Step ${index + 1} added.`);
  await focusStep(result.step.id);
}

async function move(index, direction) {
  if (props.disabled) return;
  const destination = index + direction;
  if (destination < 0 || destination >= steps.value.length) return;
  const next = [...steps.value];
  [next[index], next[destination]] = [next[destination], next[index]];
  set('steps', next);
  say(`Step ${index + 1} moved to position ${destination + 1}.`);
  await nextTick();
  const row = [...(root.value?.querySelectorAll('[data-step-id]') || [])].find(element => element.dataset.stepId === next[destination].id);
  const sameDirection = row?.querySelector(`[data-move="${direction}"]`);
  const focusTarget = sameDirection && !sameDirection.disabled ? sameDirection : row?.querySelector('.pce-step-actions button:not(:disabled)');
  focusTarget?.focus({preventScroll:true});
  row?.scrollIntoView({block:'nearest', behavior:'auto'});
}

async function remove(id, event) {
  if (props.disabled) return;
  closeMenu(event);
  const result = removeCaseStep(steps.value, id);
  if (!result) return;
  removedSteps.value.push(result.removed);
  set('steps', result.steps);
  say(`Step ${result.removed.index + 1} removed. Use Undo to restore it.`);
  await nextTick();
  root.value?.querySelector('[data-undo-step]')?.focus({preventScroll:true});
}

async function undo() {
  if (props.disabled) return;
  const removed = removedSteps.value.at(-1);
  const result = restoreCaseStep(steps.value, removed);
  if (!result) return;
  removedSteps.value.pop();
  set('steps', result.steps);
  say(`Step restored at position ${result.index + 1}.`);
  await focusStep(removed.step.id);
}

function closeMenu(event) {const details = event?.target?.closest('details');if (details) details.open = false;}
function menuKey(event) {if (event.key !== 'Escape') return;closeMenu(event);event.currentTarget.querySelector('summary')?.focus();event.stopPropagation();}
function menuBlur(event) {if (!event.currentTarget.contains(event.relatedTarget)) event.currentTarget.open = false;}
function closeOtherMenus(event) {for (const menu of root.value?.querySelectorAll('.pce-step-menu[open]') || []) if (!menu.contains(event.target)) menu.open = false;}
function addWithKeyboard(event, index) {
  if (props.disabled || event.isComposing || event.key !== 'Enter' || (!event.metaKey && !event.ctrlKey) || event.altKey || event.shiftKey) return;
  event.preventDefault();
  if (canAdd.value) insert(index + 1);
  else say(`A case can contain up to ${MAX_CASE_STEPS} steps.`);
}
async function findMissingAction() {const step = steps.value[summary.value.missingActions[0]];if (step) await focusStep(step.id);}
</script>

<template>
  <section ref="root" class="package-case-editor" aria-label="Manual test case editor" @pointerdown="closeOtherMenus">
    <header class="pce-header">
      <div class="pce-heading"><span class="pce-eyebrow">CASE {{ String(number).padStart(2,'0') }}</span><span class="pce-count">{{ summary.total }} {{ summary.total === 1 ? 'step' : 'steps' }}</span></div>
      <div class="pce-mode" role="group" aria-label="Case editor view">
        <button type="button" :aria-pressed="mode === 'write'" :class="{active:mode === 'write'}" @click="mode = 'write'"><Icon name="file"/>Write</button>
        <button type="button" :aria-pressed="mode === 'preview'" :class="{active:mode === 'preview'}" @click="mode = 'preview'"><Icon name="eye"/>Preview</button>
      </div>
    </header>
    <span class="pce-sr-only" aria-live="polite" aria-atomic="true">{{ announcement }}</span>

    <div v-show="mode === 'write'" class="pce-writing">
      <label class="pce-name"><span>Case name</span><input :value="modelValue.title" :disabled="disabled" maxlength="200" placeholder="Describe the behavior you want to verify" @input="set('title',$event.target.value)"/></label>
      <label class="pce-preconditions"><span>Before you begin <small>optional</small></span><textarea maxlength="16000" :value="modelValue.preconditions" :disabled="disabled" rows="2" placeholder="Account role, starting data, feature flags, or setup needed for this case…" @input="set('preconditions',$event.target.value)"/><span class="pce-field-hint">Give the next tester the same starting point.</span></label>

      <div class="pce-section-heading"><div><h4>Steps & expectations</h4><p>One action at a time, with a clear way to verify it.</p></div><span v-if="summary.total" class="pce-count">{{ summary.expectations }} / {{ summary.total }} expectations</span></div>
      <div v-if="!steps.length" class="pce-empty"><span class="pce-empty-icon"><Icon name="layers"/></span><div><h5>Make the path repeatable.</h5><p>Add the first action, then describe what the tester should see.</p></div><button type="button" class="pce-quiet-button" :disabled="!canAdd" @click="insert()"><Icon name="plus"/>Add first step</button></div>

      <div class="pce-step-list">
        <article v-for="(step,index) in steps" :key="step.id" class="pce-step" :data-step-id="step.id" :aria-label="`Step ${index + 1}`" @keydown="addWithKeyboard($event,index)">
          <header class="pce-step-header"><div class="pce-step-label"><span class="pce-step-number">{{ String(index + 1).padStart(2,'0') }}</span><span>Step {{ index + 1 }}</span></div>
            <div class="pce-step-actions">
              <button type="button" class="pce-icon-button" data-move="-1" :disabled="disabled || index === 0" :aria-label="`Move step ${index + 1} up`" title="Move up" @click="move(index,-1)"><Icon class="pce-up" name="arrow"/></button>
              <button type="button" class="pce-icon-button" data-move="1" :disabled="disabled || index === steps.length - 1" :aria-label="`Move step ${index + 1} down`" title="Move down" @click="move(index,1)"><Icon class="pce-down" name="arrow"/></button>
              <details class="pce-step-menu" @keydown="menuKey" @focusout="menuBlur"><summary :aria-label="`Options for step ${index + 1}`" title="Step options"><Icon name="sliders"/></summary><div class="pce-menu-items"><button type="button" :disabled="!canAdd" @click="insert(index + 1,null,$event)"><Icon name="plus"/>Insert step below</button><button type="button" :disabled="!canAdd" @click="insert(index + 1,step,$event)"><Icon name="copy"/>Duplicate step</button><button type="button" class="pce-menu-remove" :disabled="disabled" @click="remove(step.id,$event)"><Icon name="trash"/>Remove step</button></div></details>
            </div>
          </header>
          <div class="pce-step-fields"><label><span>Action</span><textarea maxlength="16000" :value="step.instruction" :disabled="disabled" rows="3" placeholder="What should the tester do?" @input="stepSet(step.id,'instruction',$event.target.value)"/></label><label class="pce-expectation"><span><Icon name="check"/>Expected result <small>optional</small></span><textarea maxlength="16000" :value="step.expected" :disabled="disabled" rows="3" placeholder="What should they see or be able to verify?" @input="stepSet(step.id,'expected',$event.target.value)"/></label></div>
        </article>
      </div>

      <div v-if="removedSteps.length" class="pce-undo"><span><Icon name="undo"/>Step removed<span v-if="removedSteps.length > 1"> · {{ removedSteps.length }} available to restore</span></span><button type="button" data-undo-step :disabled="!canAdd" @click="undo">Undo last removal</button></div>
      <div v-if="steps.length" class="pce-add-row"><button type="button" class="pce-add" aria-keyshortcuts="Control+Enter Meta+Enter" :disabled="!canAdd" @click="insert()"><Icon name="plus"/>Add a step</button><span v-if="steps.length < MAX_CASE_STEPS" class="pce-keyboard-hint"><kbd>⌘ / Ctrl</kbd> + <kbd>Enter</kbd> inserts below</span><span v-else class="pce-keyboard-hint">100 steps reached · split into another case</span></div>
      <div v-if="summary.missingActions.length" class="pce-writing-hint"><Icon name="file"/><span>{{ summary.missingActions.length === 1 ? `Step ${summary.missingActions[0] + 1} needs an action before saving.` : `${summary.missingActions.length} steps still need an action before saving.` }}</span><button type="button" :disabled="disabled" @click="findMissingAction">Go to step</button></div>

      <label class="pce-overall"><span>Overall expected behavior <small>optional</small></span><textarea maxlength="16000" :value="modelValue.expected" :disabled="disabled" rows="2" placeholder="Describe the final behavior this case establishes…" @input="set('expected',$event.target.value)"/></label>
      <details class="pce-automation"><summary><span class="pce-automation-icon"><Icon name="code"/></span><span><strong>Automation reference</strong><small>{{ automationLabel }}</small></span><Icon class="pce-disclosure" name="chevron"/></summary><div class="pce-automation-body"><label><span>Coverage</span><select :value="modelValue.automation" :disabled="disabled" @change="set('automation',$event.target.value)"><option value="manual">Manual</option><option value="planned">Automation planned</option><option value="automated">Automated coverage available</option></select></label><label><span>Test or pull request URL <small>optional</small></span><input :value="modelValue.automationUrl" :disabled="disabled" placeholder="https://github.com/rancher/tests/…" @input="set('automationUrl',$event.target.value)"/></label><p>This reference documents coverage. Attach Test Lab results to a session when you run the automation.</p></div></details>
    </div>

    <div v-if="mode === 'preview'" class="pce-preview" aria-label="Case preview">
      <div class="pce-preview-title"><span class="pce-eyebrow">THE TESTER'S VIEW</span><h3>{{ modelValue.title || 'Untitled case' }}</h3><p>Preview of this draft. Outcomes are recorded in a session.</p></div>
      <section v-if="modelValue.preconditions?.trim()" class="pce-preview-start"><h4>Before you begin</h4><p>{{ modelValue.preconditions }}</p></section>
      <ol v-if="steps.length" class="pce-preview-steps"><li v-for="(step,index) in steps" :key="step.id"><span class="pce-step-number">{{ String(index + 1).padStart(2,'0') }}</span><div><h4>Action</h4><p :class="{'pce-preview-missing':!step.instruction?.trim()}">{{ step.instruction?.trim() ? step.instruction : 'Add an action in Write mode.' }}</p><div v-if="step.expected?.trim()" class="pce-preview-expect"><h5><Icon name="check"/>Expected result</h5><p>{{ step.expected }}</p></div></div></li></ol>
      <div v-else class="pce-empty"><Icon name="layers"/><p>No steps yet. Switch to Write to build this case.</p></div>
      <section v-if="modelValue.expected?.trim()" class="pce-preview-conclusion"><h4>Overall expected behavior</h4><p>{{ modelValue.expected }}</p></section>
      <div class="pce-preview-coverage"><Icon name="code"/><span>{{ automationLabel }}<small v-if="modelValue.automationUrl">{{ modelValue.automationUrl }}</small></span></div>
    </div>

    <footer class="pce-footer"><p><Icon name="lock"/>Earlier sessions keep their original cases.</p><button type="button" :disabled="disabled" class="pce-remove" @click="$emit('remove')"><Icon name="trash"/>Remove case…</button></footer>
  </section>
</template>

<style scoped>
.package-case-editor{color:var(--runway-ink);min-width:0;container-type:inline-size}
.pce-header{display:flex;justify-content:space-between;align-items:center;gap:16px;flex-wrap:wrap;padding-bottom:23px;margin-bottom:24px;border-bottom:1px solid var(--runway-border)}
.pce-heading{display:flex;align-items:center;gap:12px}.pce-eyebrow{font-size:10px;font-weight:650;letter-spacing:.15em;color:var(--runway-muted)}.pce-count{font-size:11px;color:var(--runway-muted);white-space:nowrap}.pce-heading .pce-count{border-left:1px solid var(--runway-border);padding-left:12px}
.pce-mode{display:flex;gap:3px;padding:4px;border:1px solid var(--runway-border);border-radius:11px;background:var(--runway-soft)}.package-case-editor button{font:inherit;cursor:pointer;transition:background .15s,color .15s,border-color .15s}.package-case-editor button:disabled{opacity:.4;cursor:not-allowed}.package-case-editor :focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}.package-case-editor svg{width:16px;height:16px;flex-shrink:0}.pce-mode button{display:flex;align-items:center;gap:7px;border:1px solid transparent;border-radius:7px;color:var(--runway-muted);background:none;padding:8px 12px;font-size:12px;font-weight:550}.pce-mode button.active{color:var(--runway-accent);background:var(--runway-card);border-color:var(--runway-border);box-shadow:0 2px 4px #00000008}
.package-case-editor label{display:grid;gap:9px;font-size:12px;font-weight:600;min-width:0}.package-case-editor label>span:first-child{display:flex;align-items:center;gap:6px}.package-case-editor label small{font-size:10px;color:var(--runway-muted);font-weight:400;margin-left:auto}.package-case-editor select,.package-case-editor input,.package-case-editor textarea{box-sizing:border-box;background:var(--runway-input);border:1px solid var(--runway-control);border-radius:10px;padding:12px 14px;color:var(--runway-ink);font:inherit;font-size:13px;font-weight:400;line-height:1.7;min-width:0;width:100%;resize:vertical;transition:border-color .15s}.package-case-editor textarea{min-height:84px}.package-case-editor input:hover:not(:disabled),.package-case-editor textarea:hover:not(:disabled),.package-case-editor select:hover:not(:disabled){border-color:var(--runway-muted)}.package-case-editor ::placeholder{color:var(--runway-muted);font-weight:400;opacity:.8}.pce-name input{font-size:19px;font-weight:550;line-height:1.5;padding:13px 15px;letter-spacing:-.025em}.pce-preconditions{margin-top:20px}.pce-field-hint{font-size:11px!important;line-height:1.5;font-weight:400;color:var(--runway-muted)}
.pce-section-heading{display:flex;align-items:center;justify-content:space-between;gap:18px;margin:31px 0 17px}.pce-section-heading h4{font-size:16px;font-weight:650;letter-spacing:-.025em;margin:0}.pce-section-heading p{font-size:12px;line-height:1.6;color:var(--runway-muted);margin:5px 0 0}.pce-step-list{display:grid;gap:14px}.pce-step{min-width:0;border:1px solid var(--runway-border);border-radius:13px;background:var(--runway-card);transition:border-color .15s,box-shadow .15s}.pce-step:focus-within{border-color:var(--runway-accent);box-shadow:0 0 0 1px var(--runway-accent-soft)}.pce-step-header{display:flex;justify-content:space-between;align-items:center;gap:12px;border-bottom:1px solid var(--runway-border);padding:10px 15px;background:var(--runway-soft);border-radius:12px 12px 0 0}.pce-step-label{display:flex;gap:10px;align-items:center;font-size:12px;font-weight:600}.pce-step-number{display:inline-grid;place-items:center;width:27px;height:27px;flex-shrink:0;border-radius:8px;background:var(--runway-accent-soft);color:var(--runway-accent);font:11px ui-monospace,monospace}.pce-step-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px;padding:19px}.pce-step-fields textarea{min-height:115px}.pce-expectation>span>svg{width:14px;color:var(--runway-accent)}
.pce-step-actions{display:flex;gap:4px;align-items:center}.pce-icon-button,.pce-step-menu summary{display:grid;place-items:center;width:30px;height:30px;border:1px solid transparent;background:none;color:var(--runway-muted);border-radius:7px}.pce-icon-button:hover:not(:disabled),.pce-step-menu summary:hover,.pce-step-menu[open] summary{color:var(--runway-ink);border-color:var(--runway-border);background:var(--runway-raised)}.pce-up{transform:rotate(-90deg)}.pce-down{transform:rotate(90deg)}.pce-step-menu{position:relative}.pce-step-menu summary{cursor:pointer;list-style:none}.pce-step-menu summary::-webkit-details-marker{display:none}.pce-menu-items{position:absolute;right:0;top:calc(100% + 5px);z-index:4;display:grid;width:190px;max-width:75vw;padding:5px;background:var(--runway-raised);border:1px solid var(--runway-border);border-radius:10px;box-shadow:var(--runway-shadow)}.pce-menu-items button{display:flex;align-items:center;gap:9px;border:0;border-radius:6px;text-align:left;padding:10px;color:var(--runway-ink);background:transparent;font-size:12px}.pce-menu-items button:hover:not(:disabled){background:var(--runway-soft)}.pce-menu-items .pce-menu-remove{color:var(--runway-error);border-top:1px solid var(--runway-border);border-radius:0 0 6px 6px;margin-top:4px}
.pce-add-row{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:10px;margin-top:13px}.pce-add,.pce-quiet-button{display:inline-flex;gap:7px;align-items:center;border:1px solid var(--runway-border);border-radius:9px;background:var(--runway-soft);padding:10px 13px;color:var(--runway-accent);font-size:12px!important;font-weight:550!important}.pce-add:hover:not(:disabled),.pce-quiet-button:hover:not(:disabled){border-color:var(--runway-accent);background:var(--runway-accent-soft)}.pce-keyboard-hint{font-size:10px;color:var(--runway-muted);line-height:1.8}.pce-keyboard-hint kbd{font:inherit;border:1px solid var(--runway-border);border-radius:4px;background:var(--runway-soft);padding:2px 4px}.pce-undo{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:10px;padding:11px 13px;border:1px solid var(--runway-border);border-radius:9px;background:var(--runway-accent-soft);font-size:11px;margin-top:13px}.pce-undo>span{display:flex;align-items:center;gap:7px;flex-wrap:wrap}.pce-undo>span>span{color:var(--runway-muted)}.pce-undo button,.pce-writing-hint button{border:0;background:none;color:var(--runway-accent);font-size:11px;white-space:nowrap;padding:3px 0;font-weight:600}.pce-undo button:hover:not(:disabled),.pce-writing-hint button:hover:not(:disabled){text-decoration:underline}.pce-writing-hint{display:flex;align-items:center;gap:8px;margin:14px 0;color:var(--runway-muted);font-size:11px;line-height:1.5;flex-wrap:wrap}.pce-writing-hint>svg{width:14px}.pce-writing-hint button{margin-left:auto}.pce-overall{margin-top:28px;padding-top:25px;border-top:1px solid var(--runway-border)}
.pce-automation{margin-top:24px;border:1px solid var(--runway-border);border-radius:12px;background:var(--runway-soft)}.pce-automation>summary{display:flex;gap:12px;align-items:center;padding:15px;cursor:pointer;list-style:none}.pce-automation>summary::-webkit-details-marker{display:none}.pce-automation-icon{display:grid;place-items:center;border:1px solid var(--runway-border);border-radius:9px;width:33px;height:33px;color:var(--runway-accent)}.pce-automation summary strong{display:block;font-size:12px;font-weight:600}.pce-automation summary small{display:block;color:var(--runway-muted);font-size:11px;margin-top:4px}.pce-disclosure{margin-left:auto;color:var(--runway-muted);transition:transform .15s}.pce-automation[open] .pce-disclosure{transform:rotate(180deg)}.pce-automation-body{display:grid;gap:17px;padding:18px;border-top:1px solid var(--runway-border)}.pce-automation-body p{font-size:11px;line-height:1.7;margin:0;color:var(--runway-muted)}
.pce-footer{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:12px;margin-top:24px;padding-top:17px;border-top:1px solid var(--runway-border)}.pce-footer p{display:flex;align-items:center;gap:7px;margin:0;color:var(--runway-muted);font-size:10px;line-height:1.5}.pce-footer p svg{width:13px}.pce-remove{display:inline-flex;gap:7px;align-items:center;color:var(--runway-muted);border:0;background:none;font-size:11px!important;padding:6px 0}.pce-remove:hover:not(:disabled){color:var(--runway-error)}.pce-empty{display:flex;align-items:center;gap:15px;flex-wrap:wrap;padding:22px;border:1px dashed var(--runway-border);border-radius:12px;background:var(--runway-soft)}.pce-empty-icon{display:grid;place-items:center;width:38px;height:38px;border:1px solid var(--runway-border);border-radius:11px;color:var(--runway-accent)}.pce-empty>div{flex:1;min-width:150px}.pce-empty h5{font-size:13px;font-weight:600;margin:0 0 5px}.pce-empty p{font-size:12px;color:var(--runway-muted);margin:0;line-height:1.7}.pce-empty>.pce-quiet-button{flex-basis:100%;justify-content:center}
.pce-preview-title h3{font-size:25px;line-height:1.35;font-weight:600;letter-spacing:-.035em;margin:12px 0 8px;overflow-wrap:anywhere}.pce-preview-title>p{font-size:11px;color:var(--runway-muted);line-height:1.7;margin:0 0 24px}.pce-preview p{white-space:pre-wrap;overflow-wrap:anywhere}.pce-preview-start,.pce-preview-conclusion{border:1px solid var(--runway-border);border-radius:11px;padding:18px;background:var(--runway-soft)}.pce-preview-start h4,.pce-preview-conclusion h4{font-size:12px;font-weight:600;margin:0 0 8px}.pce-preview-start p,.pce-preview-conclusion p{font-size:13px;line-height:1.8;margin:0}.pce-preview-steps{display:grid;gap:0;margin:23px 0;padding:0;list-style:none}.pce-preview-steps>li{display:grid;grid-template-columns:28px minmax(0,1fr);gap:15px;padding:20px 0;border-bottom:1px solid var(--runway-border)}.pce-preview-steps>li:first-child{padding-top:0}.pce-preview-steps>li:last-child{border-bottom:0;padding-bottom:0}.pce-preview-steps h4{font-size:10px;text-transform:uppercase;letter-spacing:.08em;color:var(--runway-muted);font-weight:600;margin:5px 0 8px}.pce-preview-steps p{font-size:13px;line-height:1.8;margin:0}.pce-preview-expect{margin-top:14px;padding:12px 15px;background:var(--runway-accent-soft);border-left:2px solid var(--runway-accent);border-radius:0 8px 8px 0}.pce-preview-expect h5{display:flex;align-items:center;gap:6px;color:var(--runway-accent);font-size:11px;font-weight:600;margin:0 0 5px}.pce-preview-expect svg{width:13px;height:13px}.pce-preview-missing{color:var(--runway-muted);font-style:italic}.pce-preview-coverage{display:flex;align-items:flex-start;gap:8px;margin-top:22px;color:var(--runway-muted);font-size:11px;line-height:1.7}.pce-preview-coverage>svg{margin-top:2px}.pce-preview-coverage small{display:block;overflow-wrap:anywhere;font-size:11px;margin-top:5px}
.pce-sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
@container(max-width:560px){.pce-step-fields{grid-template-columns:1fr;gap:18px;padding:16px}.pce-step-fields textarea{min-height:95px}.pce-section-heading{align-items:flex-start;flex-direction:column;gap:8px}.pce-header{gap:13px}.pce-name input{font-size:17px}.pce-keyboard-hint{display:none}}
@media(prefers-reduced-motion:reduce){.package-case-editor *{transition:none!important}}
</style>
