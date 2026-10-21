<script setup>
import {computed, onBeforeUnmount, onMounted, ref, useId, watch} from 'vue';
import Icon from './HelmLabIcon.vue';

const props = defineProps({request:{type:Function,required:true},packageId:{type:String,default:''},contextTitle:{type:String,default:''},disabled:Boolean});
const emit = defineEmits(['complete']);
const id = useId(), dialog = ref(null), repositoryInput = ref(null);
const repository = ref(''), branch = ref('main'), dontAskAgain = ref(false), pending = ref(''), error = ref(''), completed = ref(false);
const busy = computed(() => props.disabled || !!pending.value || completed.value);
const canLink = computed(() => !busy.value && !!repository.value.trim() && !!branch.value.trim());
let sequence = 0, disposed = false, previousFocus, previousOverflow = '', scrollLocked = false;

async function act(kind, operation) {
  if (busy.value) return;
  const turn = ++sequence;
  const current = () => !disposed && turn === sequence;
  pending.value = kind;error.value = '';
  try {
    const result = await operation();
    if (!current()) return;
    completed.value = true;
    emit('complete',result);
  } catch (err) {
    if (current()) error.value = err.message || String(err);
  } finally {
    if (current()) pending.value = '';
  }
}
async function link() {
  if (!canLink.value) return;
  await act('link',async () => {
    // Onboarding always establishes the shared default, including when an
    // existing package initiated the dialog. No repository command runs here.
    await props.request('plan-git-link',{id:'',repository:repository.value.trim(),gitBranch:branch.value.trim()});
    return {linked:true,dismissed:false};
  });
}
async function skip() {
  const dismiss = dontAskAgain.value;
  await act('skip',async () => {
    if (dismiss) await props.request('plan-git-onboarding-dismiss',{dontAskAgain:true});
    return {linked:false,dismissed:dismiss};
  });
}

watch(() => [props.packageId,props.contextTitle], () => {
  // An action may finish after the parent has moved to a different creation
  // context. Its result must not continue that newer flow or replace its error.
  sequence++;pending.value = '';error.value = '';completed.value = false;
});
onMounted(() => {
  previousFocus = document.activeElement;
  previousOverflow = document.body.style.overflow;
  document.body.style.overflow = 'hidden';scrollLocked = true;
  dialog.value.showModal();
  repositoryInput.value?.focus({preventScroll:true});
});
onBeforeUnmount(() => {
  disposed = true;sequence++;
  dialog.value?.close();
  if (scrollLocked) document.body.style.overflow = previousOverflow;
  if (previousFocus instanceof HTMLElement && previousFocus.isConnected && previousFocus.getClientRects().length) {
    previousFocus.focus({preventScroll:true});
  }
});
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="repository-onboarding" :aria-labelledby="`${id}-heading`" :aria-describedby="`${id}-intro`" :aria-busy="!!pending" @cancel.prevent="skip" @keydown.stop>
      <form @submit.prevent="link">
        <header class="ro-heading"><span class="ro-icon"><Icon name="branch"/></span><span class="ro-eyebrow">YOUR TEST PLANS</span><p v-if="contextTitle" class="ro-context">{{contextTitle}}</p><h2 :id="`${id}-heading`">Keep your test plans safe.</h2><p :id="`${id}-intro`">Link your own GitHub repository to keep your Markdown plans and their history together.</p></header>
        <div class="ro-setup"><p>Create a repository such as <code>runway-test-cases</code>. Choose <strong>Public</strong> or <strong>Private</strong>, and add a README so its branch is ready.</p><a href="https://github.com/new?name=runway-test-cases" target="_blank" rel="noopener noreferrer">Create a repository on GitHub <Icon name="external"/></a></div>
        <div class="ro-fields"><label :for="`${id}-repository`">Repository URL<input ref="repositoryInput" :id="`${id}-repository`" v-model="repository" :disabled="busy" required maxlength="2048" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="https://github.com/your-account/runway-test-cases" :aria-describedby="error?`${id}-error`:undefined"/></label><label :for="`${id}-branch`">Branch<input :id="`${id}-branch`" v-model="branch" :disabled="busy" required maxlength="240" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="main"/></label></div>
        <p class="ro-hint">This becomes the default for your test plans. Imported plans keep their own source links. You’ll review and approve sync commands later in <strong>Files & repository</strong>.</p>
        <p v-if="error" :id="`${id}-error`" class="ro-error" role="alert">{{error}}</p>
        <footer><div class="ro-actions"><button type="submit" class="ro-primary" :disabled="!canLink">{{pending==='link'?'Linking repository…':'Link repository & continue'}}<Icon v-if="pending!=='link'" name="arrow"/></button><button type="button" class="ro-skip" :disabled="busy" @click="skip">{{pending==='skip'?'Saving preference…':'Skip for now'}}</button></div><label class="ro-dismiss" :for="`${id}-dismiss`"><input :id="`${id}-dismiss`" v-model="dontAskAgain" type="checkbox" :disabled="busy"/>Don’t ask again</label><p class="ro-later">You can manage your repository anytime in Files & repository.</p></footer>
      </form>
    </dialog>
  </Teleport>
</template>

<style scoped>
.repository-onboarding{box-sizing:border-box;position:fixed;inset:0;width:min(640px,calc(100vw - 40px));max-width:none;max-height:calc(100vh - 40px);max-height:calc(100dvh - 40px);margin:auto;padding:0;border:1px solid var(--runway-border);border-radius:18px;background:var(--runway-card);color:var(--runway-ink);font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,sans-serif;box-shadow:0 24px 90px rgba(0,0,0,.25);overflow:auto;overscroll-behavior:contain}
.repository-onboarding::backdrop{background:rgba(8,13,18,.72);backdrop-filter:blur(5px)}
.repository-onboarding form{padding:36px 40px 27px}.repository-onboarding svg{width:15px;height:15px;flex-shrink:0}
.ro-heading{position:relative}.ro-icon{display:grid;place-items:center;width:40px;height:40px;border-radius:12px;background:var(--runway-accent-soft);color:var(--runway-accent);margin-bottom:22px}.ro-icon svg{width:22px;height:22px}
.ro-eyebrow{font-size:9px;font-weight:650;letter-spacing:.16em;color:var(--runway-accent)}.ro-context{font-size:11px!important;margin:7px 0 0!important;color:var(--runway-muted);overflow-wrap:anywhere}
.ro-heading h2{font-size:clamp(24px,4vw,29px);line-height:1.25;letter-spacing:-.035em;font-weight:550;margin:12px 0}.ro-heading>p{font-size:13px;line-height:1.8;color:var(--runway-muted);margin:0}
.ro-setup{margin:23px 0 25px;padding:15px 17px;background:var(--runway-soft);border:1px solid var(--runway-border);border-radius:10px}.ro-setup p{font-size:11px;line-height:1.85;margin:0 0 9px;color:var(--runway-muted)}.ro-setup strong{font-weight:550;color:var(--runway-ink)}.ro-setup code{font-size:10px;color:var(--runway-ink)}.ro-setup a{display:inline-flex;align-items:center;gap:7px;color:var(--runway-accent);text-decoration:none;font-size:11px;font-weight:550;line-height:1.7}.ro-setup a:hover{text-decoration:underline}.ro-setup a svg{width:12px;height:12px}
.ro-fields{display:grid;grid-template-columns:minmax(0,1fr) 110px;gap:14px}.ro-fields label{font-size:11px;font-weight:550;min-width:0}.ro-fields input{box-sizing:border-box;display:block;width:100%;min-width:0;margin-top:8px;padding:11px 12px;border:1px solid var(--runway-control);border-radius:8px;background:var(--runway-input);color:var(--runway-ink);font:inherit;font-size:12px;line-height:1.5}.ro-fields input::placeholder{color:var(--runway-muted);opacity:.75}
.ro-hint{font-size:10px;line-height:1.85;color:var(--runway-muted);margin:13px 0 22px}.ro-hint strong{font-weight:550}.ro-error{font-size:11px;line-height:1.75;color:var(--runway-error);margin:14px 0;overflow-wrap:anywhere}
.repository-onboarding footer{border-top:1px solid var(--runway-border);padding-top:21px}.ro-actions{display:flex;align-items:center;gap:18px;flex-wrap:wrap}.repository-onboarding button{display:inline-flex;align-items:center;justify-content:center;gap:8px;font:inherit;font-size:11px;font-weight:550;cursor:pointer;border-radius:8px}.ro-primary{padding:12px 15px;border:1px solid var(--runway-accent);background:var(--runway-accent-soft);color:var(--runway-accent)}.ro-primary:hover:not(:disabled){filter:brightness(1.08)}.ro-skip{padding:10px 0;background:transparent;border:0;color:var(--runway-muted)}.ro-skip:hover:not(:disabled){color:var(--runway-ink)}
.ro-dismiss{display:flex;align-items:center;gap:8px;font-size:11px;line-height:1.6;color:var(--runway-muted);margin-top:19px;cursor:pointer}.ro-dismiss input{width:14px;height:14px;margin:0;accent-color:var(--runway-accent)}.ro-later{font-size:10px;color:var(--runway-muted);line-height:1.7;margin:9px 0 0}
.repository-onboarding :focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}.repository-onboarding :disabled{opacity:.5;cursor:not-allowed}
@media(max-width:580px){.repository-onboarding{width:calc(100vw - 24px);max-height:calc(100dvh - 24px);border-radius:14px}.repository-onboarding form{padding:25px 23px 22px}.ro-fields{grid-template-columns:minmax(0,1fr) 85px;gap:10px}.ro-actions{gap:15px}.ro-icon{margin-bottom:17px}}
@media(max-width:400px){.ro-fields{grid-template-columns:1fr}.ro-actions{align-items:stretch;flex-direction:column;gap:5px}.ro-skip{align-self:center}}
@media(prefers-reduced-transparency:reduce){.repository-onboarding::backdrop{background:rgba(8,13,18,.95);backdrop-filter:none}}
</style>
