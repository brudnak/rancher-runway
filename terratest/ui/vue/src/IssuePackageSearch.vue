<script setup>
import {computed, onBeforeUnmount, ref, watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {packageDate, packageStatus} from './issue-packages.mjs';

const props = defineProps({request:{type:Function,required:true}, query:{type:String,default:''}, disabled:Boolean});
const emit = defineEmits(['open-plan','open-history','import-repo']);
const results = ref([]), loading = ref(false), error = ref(''), truncated = ref(false), warnings = ref([]);
let timer, sequence = 0, disposed = false;
const phrase = computed(() => props.query.trim());
const groups = computed(() => [
  {id:'plans',title:'Plans & sessions',icon:'file'},
  {id:'history',title:'Saved Markdown versions',icon:'clock'},
  {id:'repository',title:'Repository Markdown',icon:'branch'},
].map(group => ({...group,matches:results.value.filter(item => resultGroup(item) === group.id)})).filter(group => group.matches.length));

function resultGroup(item) {
  if (item.kind === 'history') return 'history';
  if (item.kind === 'repository') return 'repository';
  return 'plans';
}
function segments(value) {
  const text = String(value || '');
  if (!phrase.value || !text) return [{text,match:false}];
  // Render matches as text nodes, including any authored HTML in a snippet.
  // Regex match offsets refer to the original text; lowercasing the entire
  // snippet first can shift offsets for characters such as capital dotted I.
  const escaped = phrase.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const parts = [];
  let offset = 0;
  for (const match of text.matchAll(new RegExp(escaped, 'giu'))) {
    const found = match.index;
    if (found > offset) parts.push({text:text.slice(offset,found),match:false});
    parts.push({text:match[0],match:true});
    offset = found + match[0].length;
  }
  if (offset < text.length) parts.push({text:text.slice(offset),match:false});
  return parts.length ? parts : [{text,match:false}];
}
function resultKey(item, index) {
  return item.id || JSON.stringify([item.kind,item.packageId,item.caseId,item.sessionId,item.sourceId,item.gitPath,item.field,index]);
}
function actionLabel(item) {
  if (resultGroup(item) === 'history') return 'Open saved Markdown';
  if (resultGroup(item) === 'repository') return 'Import as new plan';
  if (item.sourceId) return 'Open preserved baseline';
  return item.sessionId ? 'Open session' : item.caseId ? 'Open test case' : 'Open plan';
}
function open(item) {
  if (props.disabled || loading.value) return;
  if (resultGroup(item) === 'history') {
    emit('open-history', {packageId:item.packageId,sourceId:item.sourceId,path:item.path});
  } else if (resultGroup(item) === 'repository') {
    emit('import-repo', {packageId:item.packageId,gitPath:item.gitPath,repository:item.repository,gitBranch:item.gitBranch});
  } else {
    emit('open-plan', {packageId:item.packageId,caseId:item.caseId,sessionId:item.sessionId,sourceId:item.sourceId});
  }
}
async function search(query, turn) {
  try {
    const result = await props.request('plan-search', {query});
    if (disposed || turn !== sequence) return;
    if (!Array.isArray(result.matches)) throw new Error('The search response was incomplete. Try searching again.');
    results.value = result.matches;
    truncated.value = !!result.truncated;
    warnings.value = Array.isArray(result.warnings) ? result.warnings.filter(item => typeof item === 'string') : [];
  } catch (err) {
    if (!disposed && turn === sequence) error.value = err.message || String(err);
  } finally {
    if (!disposed && turn === sequence) loading.value = false;
  }
}
function schedule(immediate = false) {
  const turn = ++sequence, query = phrase.value;
  clearTimeout(timer);
  results.value = [];error.value = '';warnings.value = [];truncated.value = false;
  loading.value = !!query;
  if (!query) return;
  if (immediate) search(query, turn);
  else timer = setTimeout(() => search(query, turn), 250);
}
watch(phrase, () => schedule(), {immediate:true});
onBeforeUnmount(() => {disposed = true;sequence++;clearTimeout(timer);});
</script>

<template>
  <section class="plan-search-results" aria-label="Search across test plans" :aria-busy="loading">
    <header class="psr-heading">
      <div><Icon name="search"/><strong>Search all test plans</strong></div>
      <span role="status" aria-live="polite">{{loading?'Searching…':error?'Search unavailable':`${results.length}${truncated?'+':''} ${results.length===1?'match':'matches'}`}}</span>
    </header>
    <p class="psr-scope">Saved plans, notes, sessions, archived plans, and locally available Markdown.</p>
    <p v-if="error" class="psr-error" role="alert">{{error}} <button type="button" :disabled="disabled||loading" @click="schedule(true)">Try again</button></p>
    <p v-for="(warning,index) in warnings" :key="index" class="psr-warning">{{warning}}</p>
    <p v-if="!loading&&!error&&!results.length" class="psr-empty">No matches for “{{phrase}}”. Try another keyword.</p>
    <section v-for="group in groups" :key="group.id" class="psr-group" :aria-label="group.title">
      <h3><Icon :name="group.icon"/>{{group.title}}<span>{{group.matches.length}}</span></h3>
      <ol>
        <li v-for="(item,index) in group.matches" :key="resultKey(item,index)">
          <button type="button" class="psr-result" :disabled="disabled||loading" @click="open(item)">
            <span class="psr-result-heading"><strong>{{item.packageTitle || item.title || 'Test plan'}}</strong><span v-if="item.archived||item.status==='archived'||item.status==='closed'" class="psr-state">{{item.archived?'Archived':packageStatus(item.status)}}</span></span>
            <span v-if="item.title&&item.title!==item.packageTitle" class="psr-context">{{item.title}}</span>
            <span v-if="item.field" class="psr-field">{{item.field}}</span>
            <span v-if="item.snippet" class="psr-snippet"><template v-for="(part,partIndex) in segments(item.snippet)" :key="partIndex"><mark v-if="part.match">{{part.text}}</mark><template v-else>{{part.text}}</template></template></span>
            <span v-if="item.gitPath||item.path" class="psr-path">{{item.gitPath || item.path}}</span>
            <span v-if="item.repository" class="psr-path">{{item.repository}}<template v-if="item.gitBranch"> · {{item.gitBranch}}</template></span>
            <span class="psr-result-footer"><span v-if="item.savedAt||item.updatedAt">{{packageDate(item.savedAt || item.updatedAt,true)}}</span><span class="psr-action">{{actionLabel(item)}}<Icon name="arrow"/></span></span>
          </button>
        </li>
      </ol>
    </section>
    <p v-if="truncated" class="psr-limit">Showing the first {{results.length}} matches. Use a more specific search to narrow the results.</p>
    <p v-if="!loading&&!error" class="psr-footer">Repository matches use files already pulled onto this computer.</p>
  </section>
</template>

<style scoped>
.plan-search-results{width:100%;min-width:0;padding:15px 20px 24px;color:var(--runway-ink);box-sizing:border-box}
.plan-search-results svg{width:14px;height:14px;flex-shrink:0}
.psr-heading{display:flex;align-items:center;justify-content:space-between;gap:16px}
.psr-heading>div{display:flex;align-items:center;gap:8px;font-size:12px}
.psr-heading>div>svg{color:var(--runway-accent)}
.psr-heading>span{font-size:11px;color:var(--runway-muted);white-space:nowrap}
.psr-scope,.psr-footer,.psr-limit,.psr-warning,.psr-error,.psr-empty{font-size:11px;line-height:1.75;color:var(--runway-muted);margin:8px 0 16px;overflow-wrap:anywhere}
.psr-error{color:var(--runway-error)}.psr-warning{color:var(--runway-gold)}
.psr-error button{font:inherit;background:transparent;color:var(--runway-accent);border:0;padding:3px 6px;text-decoration:underline;cursor:pointer}
.psr-empty{padding:24px 0}
.psr-group{margin:22px 0}
.psr-group h3{display:flex;align-items:center;gap:8px;color:var(--runway-muted);font-size:10px;font-weight:600;margin:0 0 8px;letter-spacing:.04em}
.psr-group h3>span{margin-left:auto;font-size:10px}
.psr-group ol{list-style:none;margin:0;padding:0}
.psr-group li+li{border-top:1px solid var(--runway-border)}
.psr-result{display:block;width:100%;min-width:0;text-align:left;background:transparent;border:1px solid transparent;border-radius:7px;color:var(--runway-ink);padding:13px 10px;cursor:pointer;font:inherit}
.psr-result:hover:not(:disabled){background:var(--runway-soft);border-color:var(--runway-border)}
.psr-result:disabled,.psr-error button:disabled{opacity:.45;cursor:not-allowed}
.psr-result:focus-visible,.psr-error button:focus-visible{outline:2px solid var(--runway-accent);outline-offset:2px}
.psr-result-heading{display:flex;align-items:baseline;gap:12px}
.psr-result-heading strong{font-size:12px;font-weight:600;line-height:1.6;overflow-wrap:anywhere}
.psr-state{font-size:10px;color:var(--runway-muted);white-space:nowrap;margin-left:auto}
.psr-context,.psr-field,.psr-path{display:block;font-size:10px;line-height:1.6;color:var(--runway-muted);overflow-wrap:anywhere;margin-top:3px}
.psr-context{font-size:11px;color:var(--runway-ink)}
.psr-snippet{display:block;font:11px/1.8 ui-monospace,monospace;white-space:pre-wrap;overflow-wrap:anywhere;margin:9px 0}
.psr-snippet mark{background:var(--runway-accent-soft);color:var(--runway-accent);border-radius:2px;padding:1px 0;font-weight:600}
.psr-result-footer{display:flex;align-items:center;justify-content:space-between;gap:14px;font-size:10px;color:var(--runway-muted);margin-top:9px}
.psr-action{display:flex;align-items:center;gap:6px;color:var(--runway-accent);margin-left:auto}
.psr-action svg{width:12px;height:12px}
.psr-limit{padding:10px 12px;background:var(--runway-soft);border-radius:7px}
.psr-footer{font-size:10px;margin:20px 0 0}
@media(max-width:650px){.plan-search-results{padding:14px 13px 20px}.psr-heading{align-items:flex-start;gap:10px}.psr-result{padding:12px 5px}.psr-result-heading{gap:8px}.psr-result-footer{align-items:flex-start}}
</style>
