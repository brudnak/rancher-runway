<script setup>
import {computed, nextTick, onBeforeUnmount, ref, watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {packageBytes, packageDate} from './issue-packages.mjs';

const props = defineProps({request:{type:Function,required:true}, packageId:{type:String,required:true}, revision:{type:String,default:''}, writingKey:{type:String,default:''}, beforeSync:Function, disabled:Boolean});
const emit = defineEmits(['imported','changed']);
const expanded = ref(false), folder = ref(null), status = ref(null), pending = ref(''), error = ref(''), notice = ref('');
const folderError = ref(''), repositoryError = ref(''), repository = ref(''), branch = ref('main'), message = ref('');
const review = ref(null), loadedFor = ref(''), clock = ref(Date.now());
const relinking = ref(false), repositoryInput = ref(null), connectionTest = ref(null);
let sequence = 0, disposed = false, expiryTimer;
const busy = computed(() => props.disabled || !!pending.value);
const expired = computed(() => !!review.value && (!Number.isFinite(Date.parse(review.value.expiresAt)) || Date.parse(review.value.expiresAt) <= clock.value));
const history = computed(() => status.value?.localHistory || []);
const gitHistory = computed(() => status.value?.gitHistory || []);
const markdownFiles = computed(() => status.value?.markdownFiles || []);
const folderHistory = computed(() => folder.value?.history || []);
const connection = computed(() => status.value?.connection);
const planLink = computed(() => (status.value?.linkScope || status.value?.connectionScope) === 'plan');
const linkForm = computed(() => !connection.value || relinking.value);
const reviewLabels = {save:'Review save to Git',pull:'Review pull from Git',check:'Review connection test'};
const approvalLabels = {save:'Approve & save to Git',pull:'Approve & pull from Git',check:'Approve & test connection'};
const reviewInputs = () => JSON.stringify([props.packageId, props.revision, props.writingKey, connection.value, repository.value, branch.value, message.value]);

function clearReview() {review.value = null;clearInterval(expiryTimer);}
function safeLink(value) {
  try {const url = new URL(value);return url.protocol === 'https:' && !url.username && !url.password ? url.href : '';}
  catch {return '';}
}
async function perform(label, operation) {
  if (busy.value || !props.packageId) return;
  const id = props.packageId, turn = ++sequence;
  const current = () => !disposed && turn === sequence && id === props.packageId;
  pending.value = label;error.value = '';notice.value = '';
  try {await operation(id, current);}
  catch (err) {if (current()) error.value = err.message || String(err);}
  finally {if (current()) pending.value = '';}
}
async function checkpoint(id) {
  if (props.beforeSync && await props.beforeSync() === false) throw new Error('Save your plan changes before continuing. Your writing is still available.');
  await nextTick();
  if (props.packageId !== id) throw new Error('The selected plan changed. Open its file settings and try again.');
  return props.revision;
}
function acceptStatus(result, fillConnection = false) {
  status.value = result;
  repositoryError.value = '';
  if (fillConnection) {
    repository.value = result.connection?.repository || '';
    branch.value = result.connection?.branch || 'main';
  }
}
function editLink() {
  clearReview();relinking.value = true;
  repository.value = connection.value?.repository || '';branch.value = connection.value?.branch || 'main';
  nextTick(() => repositoryInput.value?.focus());
}
function cancelRelink() {
  relinking.value = false;repository.value = connection.value?.repository || '';branch.value = connection.value?.branch || 'main';
  nextTick(() => connectionTest.value?.focus());
}
async function linkRepository() {
  clearReview();
  const packageId = props.packageId;
  let linked = false;
  await perform('link', async (id, current) => {
    const result = await props.request('plan-git-link', {id,repository:repository.value.trim(),gitBranch:branch.value.trim()});
    if (!current()) return;
    acceptStatus(result, true);relinking.value = false;
    notice.value = result.message || 'Repository linked. Test the connection to check read access.';
    linked = true;
    emit('changed', result);nextTick(() => connectionTest.value?.focus());
  });
  if (linked && !disposed && props.packageId === packageId) await preview('pull');
}
async function unlinkRepository() {
  clearReview();
  await perform('unlink', async (id, current) => {
    const result = await props.request('plan-git-unlink', {id});
    if (!current()) return;
    acceptStatus(result, true);relinking.value = false;
    notice.value = result.message || 'Repository unlinked. Your local plans and repository files are preserved.';
    emit('changed', result);nextTick(() => repositoryInput.value?.focus());
  });
}
async function load() {
  await perform('load', async (id, current) => {
    const results = await Promise.allSettled([
      props.request('plan-folder', {id, revision:props.revision}),
      props.request('plan-git-status', {id}),
    ]);
    if (!current()) return;
    const [local, remote] = results;
    folderError.value = local.status === 'rejected' ? local.reason?.message || String(local.reason) : '';
    repositoryError.value = remote.status === 'rejected' ? remote.reason?.message || String(remote.reason) : '';
    if (local.status === 'fulfilled') folder.value = local.value.folder;
    if (remote.status === 'fulfilled') acceptStatus(remote.value, !relinking.value && !!remote.value.connection);
    loadedFor.value = id;
  });
}
async function localFiles(openFolder = false) {
  await perform(openFolder ? 'open-folder' : 'save-files', async (id, current) => {
    const revision = openFolder ? props.revision : await checkpoint(id);
    const result = await props.request(openFolder ? 'plan-folder-open' : 'plan-folder', {id, revision});
    if (!current()) return;
    folder.value = result.folder;folderError.value = '';
    notice.value = result.folder?.warning ? '' : openFolder ? 'Opened this plan’s folder.' : 'Markdown files are saved in this plan’s folder.';
    emit('changed', result);
  });
}
async function preview(kind) {
  if (!connection.value || relinking.value) return;
  clearReview();
  await perform(`review-${kind}`, async (id, current) => {
    const revision = kind === 'save' ? await checkpoint(id) : props.revision;
    const inputs = reviewInputs();
    const result = await props.request('plan-git-preview', {id, revision, kind, repository:connection.value.repository, gitBranch:connection.value.branch, gitMessage:message.value.trim()});
    if (!current()) return;
    if (inputs !== reviewInputs()) throw new Error('Your writing or repository settings changed during the review. Review again to use the latest version.');
    if (!result.reviewToken || result.kind !== kind || !Array.isArray(result.commands) || !result.commands.length || result.commands.some(command => typeof command.display !== 'string' || !command.display.trim()) || !Array.isArray(result.files)) {
      throw new Error('The command review was incomplete. Review again before approving.');
    }
    review.value = result;clock.value = Date.now();
    expiryTimer = setInterval(() => {clock.value = Date.now();}, 1000);
  });
}
async function openHistory(item) {
  await perform(`history:${item.id}`, async (id, current) => {
    await props.request('plan-folder-history-open', {id, sourceId:item.id});
    if (current()) notice.value = 'Opened the saved Markdown location.';
  });
}
async function execute() {
  if (!review.value || expired.value) return;
  const approved = review.value;
  await perform('execute', async (id, current) => {
    try {
      const result = await props.request('plan-git-execute', {id, reviewToken:approved.reviewToken, confirm:'execute'});
      if (!current()) return;
      acceptStatus(result, true);
      notice.value = result.message || (approved.kind === 'check' ? 'Connection verified for read access.' : approved.kind === 'pull' ? 'Repository files are ready to review below.' : 'This plan was saved to the repository.');
      emit('changed', result);
    } catch (err) {
      // A failed check still records useful status. Refresh that local record
      // without running any further repository commands.
      try {
        const latest = await props.request('plan-git-status', {id});
        if (current()) acceptStatus(latest, true);
      } catch { /* Keep the original operation error. */ }
      throw err;
    } finally {
      // An attempted approval is never silently retried. A new review obtains
      // the commands and file state for any subsequent attempt.
      if (current()) clearReview();
    }
  });
}
async function importMarkdown(file) {
  await perform(`import:${file.path}`, async (id, current) => {
    await checkpoint(id);
    const result = await props.request('plan-git-import', {id, gitPath:file.path,repository:connection.value?.repository,gitBranch:connection.value?.branch});
    if (!current()) return;
    if (!result.package?.id) throw new Error('The imported plan response was incomplete. Refresh the plan library before trying again.');
    notice.value = result.message || 'Markdown imported as a new local plan.';
    emit('changed', result);emit('imported', result.package);
  });
}
function toggle(event) {expanded.value = event.target.open;if (expanded.value && loadedFor.value !== props.packageId) load();}
watch([repository, branch, message, () => connection.value?.repository, () => connection.value?.branch, () => props.revision, () => props.writingKey], clearReview);
watch(() => props.disabled, value => {if (!value && expanded.value && !pending.value && loadedFor.value !== props.packageId) load();});
watch(() => props.packageId, () => {
  sequence++;clearReview();folder.value = null;status.value = null;loadedFor.value = '';pending.value = '';error.value = '';notice.value = '';folderError.value = '';repositoryError.value = '';repository.value = '';branch.value = 'main';message.value = '';relinking.value = false;
  if (expanded.value) nextTick(load);
});
onBeforeUnmount(() => {disposed = true;sequence++;clearInterval(expiryTimer);});
</script>

<template>
  <details class="plan-storage" @toggle="toggle">
    <summary><Icon name="folder"/><span>Files & repository</span><small>{{folder?.milestone || 'Markdown saved locally'}}</small><Icon name="chevron"/></summary>
    <div class="pst-content">
      <p v-if="pending==='load'" class="pst-hint" role="status">Opening file settings…</p>
      <p v-if="error" class="pst-error" role="alert">{{error}}</p>
      <p v-if="notice" class="pst-notice" role="status">{{notice}}</p>

      <section aria-label="Local plan files">
        <div class="pst-heading"><div><h4>Your Markdown files</h4><p>Saving the plan also keeps its Markdown file in a local folder.</p></div><button type="button" class="pst-link" :disabled="busy" @click="localFiles(true)"><Icon name="folder"/>Open folder location</button></div>
        <p v-if="folderError" class="pst-error" role="alert">{{folderError}}</p>
        <p v-if="folder?.warning" class="pst-warning" role="alert">{{folder.warning}}</p>
        <dl v-if="folder" class="pst-location"><div><dt>Folder</dt><dd><code>{{folder.path}}</code></dd></div><div v-if="folder.milestone"><dt>Milestone</dt><dd>{{folder.milestone}}</dd></div><div v-if="folder.repository"><dt>Issue repository</dt><dd>{{folder.repository}}<span v-if="folder.issueNumber"> · #{{folder.issueNumber}}</span></dd></div></dl>
        <div class="pst-local-footer"><span v-if="folder?.files?.length">{{folder.files.join(' · ')}}</span><button type="button" :disabled="busy" @click="localFiles()">{{pending==='save-files'?'Saving files…':'Save Markdown files'}}</button></div>
        <details v-if="folderHistory.length" class="pst-history"><summary>Saved Markdown history</summary><ol><li v-for="item in folderHistory" :key="item.id"><div><strong>{{item.title || 'Saved test plan'}}</strong><small>{{packageDate(item.savedAt,true)}}</small><code>{{item.path}}</code></div><button type="button" class="pst-link" :disabled="busy" @click="openHistory(item)">Open saved Markdown</button></li></ol></details>
      </section>

      <section class="pst-repository" aria-label="Repository connection">
        <div class="pst-heading"><div><h4>Keep your test plans safe in GitHub</h4><p>Link a public or private repository to keep a copy of your Markdown and its history.</p></div><button type="button" class="pst-link" :disabled="busy" title="Refresh the locally saved repository status" @click="load">{{pending==='load'?'Refreshing…':'Refresh'}}</button></div>
        <p v-if="repositoryError" class="pst-error" role="alert">{{repositoryError}}</p>
        <p v-if="status?.warning" class="pst-warning" role="alert">{{status.warning}}</p>
        <div v-if="connection" class="pst-linked">
          <p class="pst-connection"><strong>Linked · {{planLink?'This imported plan':'Default for test plans'}}</strong><br/><a v-if="safeLink(connection.webUrl)" :href="safeLink(connection.webUrl)" target="_blank" rel="noopener noreferrer">{{connection.repository}}</a><span v-else>{{connection.repository}}</span> · {{connection.branch}}</p>
          <p v-if="status.checkStatus==='readable'" class="pst-verified">Connection verified · Read access only</p>
          <p v-else-if="status.checkStatus==='failed'" class="pst-warning">Connection check failed</p>
          <p v-else class="pst-hint">Connection not checked yet.</p>
          <p v-if="status.checkedAt" class="pst-hint">Last checked {{packageDate(status.checkedAt,true)}}<span v-if="status.checkMessage"> · {{status.checkMessage}}</span></p>
          <p class="pst-hint">{{planLink?'Relink or unlink only this imported plan. Its source repository is separate from the default.':'Relink or unlink changes the default for all test plans. Imported plans keep their own repository links.'}} Unlinking preserves local plans and repository files.</p>
          <div v-if="!relinking" class="pst-actions pst-connection-actions"><button ref="connectionTest" type="button" :disabled="busy" @click="preview('check')">{{pending==='review-check'?'Preparing review…':'Test connection'}}</button><button type="button" class="pst-link" :disabled="busy" @click="editLink">Relink</button><button type="button" class="pst-link" :disabled="busy" :title="planLink?'Remove only this imported plan’s link':'Remove the default repository link for test plans'" @click="unlinkRepository">{{pending==='unlink'?'Unlinking…':'Unlink'}}</button></div>
        </div>
        <form v-if="linkForm" class="pst-link-form" @submit.prevent="linkRepository">
          <h5 v-if="relinking">{{planLink?'Change this imported plan’s repository':'Change the default repository'}}</h5>
          <p class="pst-hint">Create a repository such as <code>runway-test-cases</code>, or use one you already have. Choose Public or Private and add a README so the branch exists.</p>
          <p><a href="https://github.com/new?name=runway-test-cases" target="_blank" rel="noopener noreferrer">Create a repository on GitHub ↗</a></p>
          <div class="pst-fields"><label>Repository URL<input ref="repositoryInput" v-model="repository" :disabled="busy" required autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="https://github.com/your-account/runway-test-cases.git"/></label><label>Branch<input v-model="branch" :disabled="busy" required autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="main"/></label></div>
          <p class="pst-hint">{{planLink?'Linking saves a repository for this imported plan only.':'Linking sets the default repository for all test plans. Imported plans keep their own source links.'}} Test the connection afterward using your local Git authentication.</p>
          <div class="pst-actions"><button type="submit" class="pst-approve" :disabled="busy||!repository.trim()||!branch.trim()">{{pending==='link'?'Saving link…':relinking?'Save repository link':'Link repository'}}</button><button v-if="relinking" type="button" class="pst-link" :disabled="busy" @click="cancelRelink">Cancel</button></div>
        </form>
        <template v-if="connection&&!relinking"><label class="pst-message">Commit message <small>optional</small><input v-model="message" :disabled="busy" maxlength="240" placeholder="Update test plan"/></label><p class="pst-hint">Review commands before approving. Saving uploads this plan’s Markdown; pulling lists files to import as new plans. Refresh reloads the saved status; Test connection checks GitHub.</p></template>
        <div class="pst-actions pst-sync-actions"><button type="button" :disabled="busy||!connection||relinking" @click="preview('save')"><Icon name="upload"/>{{pending==='review-save'?'Preparing review…':'Review save to Git'}}</button><button type="button" :disabled="busy||!connection||relinking" @click="preview('pull')"><Icon name="download"/>{{pending==='review-pull'?'Preparing review…':'Review pull from Git'}}</button></div>
      </section>

      <section v-if="review" class="pst-review" aria-label="Review repository operation">
        <h4>{{reviewLabels[review.kind]}}</h4>
        <p>{{review.repository}} · {{review.branch}}</p>
        <p v-if="review.checkoutPath" class="pst-hint">Local repository: <code>{{review.checkoutPath}}</code></p>
        <p v-for="(warning,index) in review.warnings||[]" :key="index" class="pst-warning">{{warning}}</p>
        <h5>Commands to run</h5><ol class="pst-commands"><li v-for="(command,index) in review.commands" :key="index"><span>In <code>{{command.directory}}</code></span><span v-if="command.condition" class="pst-warning">{{command.condition}}</span><pre tabindex="0">{{command.display}}</pre></li></ol>
        <h5>Files in this review</h5><ul v-if="review.files.length" class="pst-files"><li v-for="file in review.files" :key="file.path"><code>{{file.path}}</code><span>{{packageBytes(file.bytes)}}</span></li></ul><p v-else class="pst-hint">{{review.kind==='check'?'This checks read access to the linked branch. No plan files are uploaded or downloaded, and push permission is not tested.':'Repository Markdown files will be listed after the approved pull.'}}</p>
        <p class="pst-hint">This approval applies only to the commands and files above. Expires {{packageDate(review.expiresAt,true)}}.</p>
        <p v-if="expired" class="pst-error" role="alert">This review expired. Cancel and review the operation again.</p>
        <div class="pst-actions"><button type="button" class="pst-approve" :disabled="busy||expired" @click="execute">{{pending==='execute'?'Running approved commands…':approvalLabels[review.kind]}}</button><button type="button" class="pst-link" :disabled="busy" @click="clearReview">Cancel</button></div>
      </section>

      <section v-if="markdownFiles.length" class="pst-imports" aria-label="Repository Markdown files">
        <h4>Markdown available to import</h4><p class="pst-hint">Each import creates a new local plan. Your existing plans stay available.</p>
        <ul><li v-for="file in markdownFiles" :key="file.path"><div><strong v-if="file.title">{{file.title}}</strong><code>{{file.path}}</code><small>{{packageBytes(file.bytes)}}</small></div><button type="button" :disabled="busy" @click="importMarkdown(file)">{{pending===`import:${file.path}`?'Importing…':'Import as new plan'}}</button></li></ul>
      </section>

      <details v-if="history.length||gitHistory.length" class="pst-history"><summary>Recent saves & pulls</summary><ol v-if="history.length"><li v-for="(item,index) in history" :key="`${item.at}-${index}`"><div><strong>{{item.summary || item.operation}}</strong><small>{{packageDate(item.at,true)}} · {{item.status}}</small></div><a v-if="safeLink(item.url)" :href="safeLink(item.url)" target="_blank" rel="noopener noreferrer">{{item.commit?item.commit.slice(0,10):'View'}}</a><code v-else-if="item.commit">{{item.commit.slice(0,10)}}</code></li></ol><template v-if="gitHistory.length"><h5>Repository commits</h5><ol><li v-for="item in gitHistory" :key="item.url || item.commit"><div><strong>{{item.subject}}</strong><small>{{packageDate(item.at,true)}}</small></div><a v-if="safeLink(item.url)" :href="safeLink(item.url)" target="_blank" rel="noopener noreferrer">{{item.commit.slice(0,10)}}</a><code v-else>{{item.commit.slice(0,10)}}</code></li></ol></template></details>
    </div>
  </details>
</template>

<style scoped>
.pst-linked{margin:16px 0;padding:14px 16px;border:1px solid var(--runway-border);border-radius:9px;background:var(--runway-soft)}.pst-linked p{margin:5px 0;font-size:11px;line-height:1.75}.pst-linked .pst-verified{color:var(--runway-accent)}.pst-linked .pst-hint{font-size:10px}.pst-connection-actions{margin-top:12px}.pst-link-form{margin:16px 0}.pst-link-form>p{font-size:11px;line-height:1.75}.pst-sync-actions{margin-top:18px}
.plan-storage{margin:0 28px;border-bottom:1px solid var(--runway-border);color:var(--runway-ink);font-size:12px;min-width:0}.plan-storage>summary{display:flex;align-items:center;gap:10px;padding:14px 0;list-style:none;cursor:pointer;font-size:11px}.plan-storage>summary::-webkit-details-marker{display:none}.plan-storage>summary>small{font-size:10px;color:var(--runway-muted)}.plan-storage>summary>svg:first-child{color:var(--runway-accent)}.plan-storage>summary>svg:last-child{margin-left:auto;width:12px}.plan-storage[open]>summary>svg:last-child{transform:rotate(180deg)}.plan-storage svg{width:15px;height:15px;flex-shrink:0}.plan-storage :focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}.pst-content{max-width:900px;padding:8px 0 24px}.pst-content>section+section{margin-top:26px;padding-top:24px;border-top:1px solid var(--runway-border)}.pst-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.pst-content h4{font-size:13px;font-weight:600;line-height:1.5;margin:0 0 6px}.pst-content h5{font-size:11px;font-weight:600;margin:19px 0 9px}.pst-heading p,.pst-hint,.pst-content section>p{font-size:11px;line-height:1.75;color:var(--runway-muted);margin:5px 0 13px}.pst-content code{font:11px/1.65 ui-monospace,monospace;overflow-wrap:anywhere}.pst-content button{display:inline-flex;align-items:center;justify-content:center;gap:7px;border:1px solid var(--runway-border);border-radius:8px;background:var(--runway-soft);color:var(--runway-ink);padding:9px 12px;font:inherit;font-size:11px;cursor:pointer}.pst-content button:disabled{opacity:.45;cursor:not-allowed}.pst-content .pst-link{border:0;background:transparent;color:var(--runway-accent);padding:6px 0;white-space:nowrap}.pst-content button:hover:not(:disabled){border-color:var(--runway-accent)}.pst-content .pst-approve{background:var(--runway-accent-soft);color:var(--runway-accent);border-color:var(--runway-accent)}.pst-content a{color:var(--runway-accent);text-decoration:underline;overflow-wrap:anywhere}.pst-location{display:grid;gap:10px;margin:15px 0}.pst-location>div{display:grid;grid-template-columns:95px minmax(0,1fr);gap:12px}.pst-location dt{color:var(--runway-muted);font-size:10px;padding-top:2px}.pst-location dd{font-size:11px;line-height:1.7;margin:0;min-width:0;overflow-wrap:anywhere}.pst-local-footer,.pst-actions{display:flex;align-items:center;flex-wrap:wrap;gap:12px}.pst-local-footer{justify-content:space-between;margin-top:14px}.pst-local-footer>span{color:var(--runway-muted);font-size:10px}.pst-fields{display:grid;grid-template-columns:minmax(0,1fr) minmax(120px,180px);gap:15px;margin-top:15px}.pst-content label{display:block;font-size:11px;font-weight:550}.pst-content label small{font-size:10px;font-weight:400;color:var(--runway-muted);margin-left:6px}.pst-content input{box-sizing:border-box;display:block;width:100%;min-width:0;margin-top:7px;padding:10px 12px;border:1px solid var(--runway-control);border-radius:8px;background:var(--runway-input);color:var(--runway-ink);font:inherit;font-size:12px;line-height:1.5}.pst-message{margin:14px 0}.pst-connection{overflow-wrap:anywhere}.pst-content .pst-error{color:var(--runway-error);font-size:11px;line-height:1.75;margin:10px 0}.pst-notice{padding:10px 13px;border-radius:8px;background:var(--runway-accent-soft);color:var(--runway-accent);font-size:11px;line-height:1.75}.pst-content .pst-warning{color:var(--runway-gold)}.pst-review{padding:18px!important;border:1px solid var(--runway-border);border-radius:10px;background:var(--runway-soft)}.pst-commands{margin:0;padding-left:20px;display:grid;gap:13px}.pst-commands li>span{display:block;font-size:10px;color:var(--runway-muted);overflow-wrap:anywhere}.pst-commands pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--runway-input);border:1px solid var(--runway-border);border-radius:7px;padding:11px 13px;margin:5px 0 0;font:11px/1.75 ui-monospace,monospace}.pst-files{list-style:none;padding:0;margin:0;display:grid;gap:8px}.pst-files li{display:flex;align-items:baseline;justify-content:space-between;gap:15px}.pst-files li>span{font-size:10px;color:var(--runway-muted);white-space:nowrap}.pst-imports>ul,.pst-history ol{margin:12px 0 0;padding:0;list-style:none}.pst-imports li,.pst-history li{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:12px 0;border-bottom:1px solid var(--runway-border)}.pst-imports li>div,.pst-history li>div{min-width:0;flex:1}.pst-imports strong,.pst-history strong{display:block;font-size:11px;font-weight:550;line-height:1.65;overflow-wrap:anywhere}.pst-imports code{display:block}.pst-imports small,.pst-history small{display:block;color:var(--runway-muted);font-size:10px;line-height:1.7;margin-top:3px}.pst-imports button{flex-shrink:0}.pst-history{margin-top:24px;font-size:11px}.pst-history>summary{cursor:pointer;color:var(--runway-muted);padding:6px 0}.pst-history li>a,.pst-history li>code{flex-shrink:0;font-size:10px}
@media(max-width:650px){.plan-storage{margin:0 20px}.plan-storage>summary>small{display:none}.pst-fields{grid-template-columns:1fr}.pst-location>div{grid-template-columns:1fr;gap:2px}.pst-imports li{align-items:flex-start;flex-direction:column;gap:8px}.pst-heading{gap:12px}.pst-review{padding:14px!important}.pst-history li{align-items:flex-start}}
</style>
