<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { apiFetch, activeTab } from './store.js';
import { writeTextToClipboard } from './clipboard.js';
import AppBuildStamp from './AppBuildStamp.vue';
import RadarIcon from './HelmLabIcon.vue';
import IssueRadarCard from './IssueRadarCard.vue';
import { QA_LABELS, parseUsers, milestoneLabel, buildIssueRadarReport, visibleIssueLanes } from './issue-radar.mjs';
import { RADAR_HISTORY_LIMIT, radarWorkload, radarHistorySummary, radarScopeKey, buildIssueRadarPrompt, normalizeCapacity } from './issue-radar-prompt.mjs';
import { readRadarJSON } from './issue-radar-request.mjs';

const preferenceKey = 'rancherRunwayIssueRadar';
let saved = {};
try { saved = JSON.parse(localStorage.getItem(preferenceKey) || '{}') || {}; } catch { /* Preferences are optional. */ }
const preference = (key, fallback) => typeof saved[key] === 'string' ? saved[key] : fallback;
const form = reactive({ repo: preference('repo', 'rancher/rancher'), label: preference('label', 'team/frameworks'), users: preference('users', ''), milestone: preference('milestone', ''), scope: ['all', 'none', 'specific'].includes(saved.scope) ? saved.scope : 'specific' });
const report = ref(null), loading = ref(false), error = ref(''), message = ref(''), messageError = ref(false);
const scopeOpen = ref(true), view = ref('board'), filter = ref('all'), query = ref(''), owner = ref(''), compact = ref(false);
const views = [{ id: 'board', label: 'Board', icon: 'layers' }, { id: 'summary', label: 'Summary', icon: 'pulse' }, { id: 'report', label: 'Create prompt', icon: 'file' }];
const expandedLanes = ref(new Set()), searchInput = ref(null), workspace = ref(null);
const milestones = ref([]), milestoneLoading = ref(false), milestoneError = ref('');
const history = ref(null), historyLoading = ref(false), historyError = ref(''), skipHistory = ref(false), saving = ref(false), copying = ref(false);
const planning = reactive({ mode: 'preserve', notes: '', capacities: {} });
let controller, milestoneController, historyController, saveController, noticeTimer;
let disposed = false;
const config = computed(() => {
  const labels = new Map();
  for (const label of form.label.split(',').map(value => value.trim()).filter(Boolean)) if (!labels.has(label.toLowerCase())) labels.set(label.toLowerCase(), label);
  return { repo: form.repo.trim(), label: [...labels.values()].join(','), users: parseUsers(form.users), milestone: form.scope === 'specific' ? form.milestone.trim() : '', noMilestone: form.scope === 'none' };
});
const scopeChanged = computed(() => !!report.value && radarScopeKey(config.value) !== radarScopeKey(report.value.config));
const lanes = computed(() => report.value ? visibleIssueLanes(report.value, { filter: filter.value, query: query.value, owner: owner.value }) : []);
const visibleCount = computed(() => lanes.value.reduce((sum, lane) => sum + lane.issues.length, 0));
const displayedLanes = computed(() => query.value.trim() || owner.value || ['unassigned', 'no-milestone', 'missing-size'].includes(filter.value) ? lanes.value.filter(lane => lane.issues.length) : lanes.value);
const workload = computed(() => report.value ? radarWorkload(report.value) : { rows: [], totalPoints: 0, unownedPoints: 0 });
const ownerCards = computed(() => (report.value?.userCards || []).map(card => ({ ...card, ...workload.value.rows.find(row => row.user === card.user) })));
const maxLoad = computed(() => Math.max(1, ...workload.value.rows.map(row => row.points)));
const pointLabel = value => new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value);
const historyRows = computed(() => report.value ? radarHistorySummary(report.value, history.value) : []);
const historyCount = computed(() => historyRows.value.reduce((sum, row) => sum + row.count, 0));
const historyPartial = computed(() => !!history.value && (historyRows.value.some(row => row.status === 'unavailable') || !!history.value.warnings?.length));
const promptReady = computed(() => !!report.value && (!!history.value || skipHistory.value));
const prompt = computed(() => promptReady.value ? buildIssueRadarPrompt(report.value, history.value, planning) : '');
const exportDisabled = computed(() => !promptReady.value || loading.value || historyLoading.value || scopeChanged.value || saving.value || copying.value);
const snapshotTime = computed(() => report.value ? new Date(report.value.generatedAt).toLocaleString() : '');
const coverage = computed(() => report.value?.totals.categorized ? Math.round(report.value.totals.exactlyOne / report.value.totals.categorized * 100) : 0);
const attentionCount = computed(() => (report.value?.totals.missingOwner || 0) + (report.value?.totals.overAssigned || 0));
const filters = [{ id: 'all', label: 'All issues' }, { id: 'problems', label: 'Needs attention' }, { id: 'missing-owner', label: 'Needs a team owner' }, { id: 'shared', label: 'Multiple team owners' }, { id: 'clean', label: 'One team owner' }, { id: 'unassigned', label: 'Unassigned' }, { id: 'no-milestone', label: 'No milestone' }, { id: 'missing-size', label: 'Needs QA size review' }, { id: 'qa-none', label: 'QA/None' }];
const metrics = computed(() => {
  const t = report.value?.totals;
  return t ? [
    { label: 'In QA scope', value: t.categorized, hint: `${t.fetched} issues in snapshot`, filter: 'all' },
    { label: 'One team owner', value: t.exactlyOne, hint: 'Assignment covered', filter: 'clean', tone: 'good' },
    { label: 'Needs an owner', value: t.missingOwner, hint: `${t.unassigned} completely unassigned`, filter: 'missing-owner', tone: 'warning' },
    { label: 'Shared ownership', value: t.overAssigned, hint: 'Choose one primary owner', filter: 'shared', tone: 'warning' },
    { label: 'No milestone', value: t.noMilestone, hint: 'Within QA scope', filter: 'no-milestone' },
    { label: 'QA/None', value: t.qaNone, hint: 'Outside assignment planning', filter: 'qa-none' },
  ] : [];
});
function notify(value, failed = false) { if (disposed) return; clearTimeout(noticeTimer); message.value = value; messageError.value = failed; noticeTimer = setTimeout(() => { message.value = ''; }, 8500); }
function selectFilter(value) { filter.value = value; owner.value = ''; query.value = ''; view.value = 'board'; }
function showOwner(user) { owner.value = owner.value === user ? '' : user; filter.value = 'all'; query.value = ''; view.value = 'board'; }
function toggleLane(lane) { const next = new Set(expandedLanes.value); next.has(lane.id) ? next.delete(lane.id) : next.add(lane.id); expandedLanes.value = next; }
function resetFilters() { filter.value = 'all'; query.value = ''; owner.value = ''; }
watch([filter, query, owner], () => { expandedLanes.value = new Set(); });
watch(form, () => { try { localStorage.setItem(preferenceKey, JSON.stringify(form)); } catch { /* Preferences are optional. */ } }, { deep: true });
watch(() => form.repo, () => { milestoneController?.abort(); milestoneLoading.value = false; milestones.value = []; milestoneError.value = ''; });
const requestJSON = (path, payload, request, label, timeout) => readRadarJSON(signal => apiFetch(path, { method: 'POST', body: JSON.stringify(payload), signal }), request, label, timeout);
async function runReport() {
  if (loading.value) return;
  error.value = ''; message.value = '';
  if (form.scope === 'specific' && !form.milestone.trim()) { error.value = 'Enter a milestone, or choose All milestones.'; return; }
  if (!config.value.users.length) { error.value = 'Add the GitHub usernames whose assignments you want to compare.'; return; }
  loading.value = true; controller = new AbortController();
  const request = controller;
  historyController?.abort(); historyLoading.value = false;
  try {
    const snapshot = await requestJSON('/api/issue-radar', config.value, request, 'Issue Radar');
    if (request.signal.aborted || disposed) return;
    report.value = buildIssueRadarReport(snapshot);
    planning.capacities = Object.fromEntries(report.value.config.users.map(user => [user, normalizeCapacity(planning.capacities[user]) ]));
    history.value = null; skipHistory.value = false; historyError.value = ''; resetFilters(); view.value = 'board'; scopeOpen.value = false;
    await nextTick(); workspace.value?.focus({ preventScroll: true });
  } catch (err) { if (!disposed) error.value = err.name === 'AbortError' ? 'Refresh cancelled. Your previous snapshot is still available.' : err.message || 'The issue board could not be loaded.'; }
  finally { if (controller === request) loading.value = false; }
}
async function loadMilestones() {
  milestoneController?.abort(); milestoneController = new AbortController();
  const request = milestoneController, repo = form.repo.trim();
  milestoneLoading.value = true; milestoneError.value = '';
  try {
    const data = await requestJSON('/api/issue-radar/milestones', { repo }, request, 'Milestone lookup', 65000);
    if (request.signal.aborted || repo !== form.repo.trim() || disposed) return;
    milestones.value = data.milestones || [];
    if (!milestones.value.length) milestoneError.value = 'No milestones found. Choose All milestones or No milestone.';
  } catch (err) { if (err.name !== 'AbortError' && !disposed) milestoneError.value = err.message; }
  finally { if (milestoneController === request) milestoneLoading.value = false; }
}
async function loadHistory() {
  if (!report.value || historyLoading.value || loading.value || scopeChanged.value) return;
  historyController?.abort(); historyController = new AbortController();
  const request = historyController, snapshot = report.value;
  historyLoading.value = true; historyError.value = ''; history.value = null; skipHistory.value = false;
  try {
    const result = await requestJSON('/api/issue-radar/history', { config: snapshot.config, limit: RADAR_HISTORY_LIMIT }, request, 'Owner history');
    if (request.signal.aborted || report.value !== snapshot || disposed) return;
    buildIssueRadarPrompt(snapshot, result, planning); // Reject history from a different scope or sample size.
    history.value = result;
  } catch (err) { if (!disposed && report.value === snapshot) historyError.value = err.name === 'AbortError' ? 'History loading cancelled. Retry when you are ready.' : err.message; }
  finally { if (historyController === request) historyLoading.value = false; }
}
function activateView(value) { view.value = value; if (value === 'report' && !history.value && !skipHistory.value) loadHistory(); }
async function copyPrompt() {
  if (exportDisabled.value) return;
  copying.value = true;
  try { await writeTextToClipboard(prompt.value); notify(historyPartial.value || skipHistory.value ? 'Prompt copied with its history limitations clearly marked.' : 'Assignment prompt copied. Paste it into your AI assistant.'); }
  catch (err) { notify(err.message || 'Could not copy the prompt. Select the preview text to copy it manually.', true); }
  finally { copying.value = false; }
}
async function savePrompt() {
  if (exportDisabled.value) return;
  saving.value = true; saveController = new AbortController();
  try { const data = await requestJSON('/api/issue-radar/save', { content: prompt.value, kind: 'prompt' }, saveController, 'Prompt export', 30000); notify(`Saved ${data.filename} to Downloads.`); }
  catch (err) { if (err.name !== 'AbortError') notify(err.message || 'The prompt could not be saved.', true); }
  finally { saving.value = false; }
}
async function openIssue(url) { try { await apiFetch('/api/open-url', { method: 'POST', body: JSON.stringify({ url }) }); } catch (err) { notify(err.message || 'Could not open the issue.', true); } }
async function copyIssue(url) { try { await writeTextToClipboard(url); notify('Issue link copied.'); } catch (err) { notify(err.message, true); } }
function navigateView(event) {
  const current = views.findIndex(item => item.id === view.value);
  const next = event.key === 'ArrowRight' ? (current + 1) % views.length : event.key === 'ArrowLeft' ? (current + views.length - 1) % views.length : event.key === 'Home' ? 0 : event.key === 'End' ? views.length - 1 : -1;
  if (next < 0) return;
  event.preventDefault(); activateView(views[next].id); nextTick(() => document.getElementById(`ir-tab-${view.value}`)?.focus());
}
function searchShortcut(event) { if (activeTab.value !== 'issues' || !report.value || view.value !== 'board' || event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey || event.target.closest?.('input,textarea,select,[contenteditable="true"]')) return; event.preventDefault(); searchInput.value?.focus(); }
onMounted(() => window.addEventListener('keydown', searchShortcut));
onBeforeUnmount(() => { disposed = true; controller?.abort(); milestoneController?.abort(); historyController?.abort(); saveController?.abort(); clearTimeout(noticeTimer); window.removeEventListener('keydown', searchShortcut); });
</script>

<template>
  <div class="issue-radar" :class="{ 'ir-compact': compact }">
    <header class="ir-header">
      <div class="ir-title-group"><span class="ir-mark" aria-hidden="true"><svg viewBox="0 0 48 48" fill="none" stroke="currentColor"><circle cx="24" cy="24" r="19"/><circle cx="24" cy="24" r="12"/><circle cx="24" cy="24" r="5"/><path d="M24 24 38 10M24 3v5M3 24h5M24 40v5M40 24h5"/><circle cx="15" cy="17" r="2" fill="currentColor"/><circle cx="31" cy="34" r="2" fill="currentColor"/></svg></span><div><span class="ir-eyebrow">Rancher Runway / Team workspace</span><h2>Issue Radar</h2><p class="ir-muted">See the gaps. Understand the workload. Plan the next move.</p></div></div>
      <div class="ir-header-right"><span class="ir-readonly"><span aria-hidden="true"></span>GitHub · read only</span><span class="ir-caption">{{ report ? `${report.config.users.length} owners in view` : 'A clearer picture of your team’s work' }}</span></div>
    </header>

    <section class="ir-config" aria-label="Issue scope">
      <div class="ir-config-heading"><button type="button" class="ir-scope-toggle" :aria-expanded="scopeOpen" aria-controls="ir-scope-form" @click="scopeOpen = !scopeOpen"><RadarIcon name="sliders"/><span><strong>{{ report ? `${form.repo || 'Repository'} · ${milestoneLabel(config)}` : 'Set your team’s scope' }}</strong><small>{{ report ? (scopeChanged ? 'Edits ready · refresh to apply' : `${report.config.label} · ${report.config.users.map(user => '@' + user).join(', ')}`) : 'Choose a repository, team, and milestone.' }}</small></span><RadarIcon name="chevron" :class="{ 'ir-rotate': scopeOpen }"/></button><div v-if="report" class="ir-actions"><button type="button" class="ir-button" :disabled="loading" @click="scopeOpen = !scopeOpen">{{ scopeOpen ? 'Hide settings' : 'Edit scope' }}</button><button type="button" class="ir-button ir-primary" @click="loading ? controller?.abort() : runReport()"><RadarIcon name="refresh" :class="{ 'ir-spin': loading }"/>{{ loading ? 'Cancel refresh' : 'Refresh board' }}</button></div></div>
      <form v-show="scopeOpen" id="ir-scope-form" @submit.prevent="runReport" :aria-busy="loading">
        <div class="ir-form-grid">
          <label for="ir-repo">Repository<input id="ir-repo" v-model="form.repo" placeholder="rancher/rancher" autocomplete="off" spellcheck="false" :disabled="loading"></label>
          <label for="ir-label">Team labels<input id="ir-label" v-model="form.label" placeholder="team/frameworks" autocomplete="off" spellcheck="false" :disabled="loading"><span class="ir-caption">Comma-separated. Every label must match.</span></label>
          <label for="ir-users" class="ir-form-wide">GitHub usernames<input id="ir-users" v-model="form.users" placeholder="octocat, another-owner" autocomplete="off" spellcheck="false" :disabled="loading"><span class="ir-caption">Up to eight owners. Unassigned issues are included.</span></label>
          <label for="ir-scope">Milestone scope<select id="ir-scope" v-model="form.scope" :disabled="loading"><option value="specific">Specific milestone</option><option value="all">All milestones</option><option value="none">No milestone</option></select></label>
          <div v-if="form.scope === 'specific'" class="ir-milestone-field"><label for="ir-milestone">Milestone title<input id="ir-milestone" v-model="form.milestone" placeholder="v2.14.0" autocomplete="off" :disabled="loading"></label><button type="button" class="ir-text-button" :disabled="milestoneLoading || loading" @click="loadMilestones">{{ milestoneLoading ? 'Loading milestones…' : 'Choose from GitHub' }}</button><select v-if="milestones.length" aria-label="Choose a repository milestone" :disabled="loading" :value="form.milestone" @change="form.milestone = $event.target.value"><option value="" disabled>Choose a milestone…</option><option v-for="item in milestones" :key="item.number" :value="item.title">{{ item.title }}{{ item.state === 'closed' ? ' · closed' : '' }}</option></select><p v-if="milestoneError" class="ir-error-text" role="alert">{{ milestoneError }}</p></div>
          <div class="ir-form-submit"><button class="ir-button ir-primary" type="submit" :disabled="loading"><RadarIcon name="layers"/>{{ loading ? 'Reading GitHub…' : report ? 'Apply & refresh' : 'Build my board' }}<RadarIcon name="arrow"/></button><span class="ir-caption">Uses your existing GitHub CLI login.</span><button v-if="loading && !report" type="button" class="ir-text-button" @click="controller?.abort()">Cancel</button></div>
        </div>
      </form>
      <p v-if="error" class="ir-alert ir-alert-error" role="alert">{{ error }}<AppBuildStamp /></p>
    </section>

    <div v-if="!report" class="ir-empty-start"><div class="ir-empty-orbit" aria-hidden="true"><RadarIcon name="layers"/></div><span class="ir-eyebrow">From a queue to a clear plan</span><h3>Your team’s work, in focus.</h3><p>Explore the owner board, uncover assignment gaps, and prepare an AI planning prompt with real issue history.</p><div class="ir-empty-features"><span><RadarIcon name="layers"/>Owner board</span><span><RadarIcon name="pulse"/>Effort & coverage</span><span><RadarIcon name="file"/>AI assignment prompt</span></div></div>

    <template v-else>
      <div v-if="scopeChanged" class="ir-alert" role="status">Scope changed. This snapshot still shows <strong>{{ report.config.repo }} · {{ milestoneLabel(report.config) }}</strong>. Refresh the board before exporting a prompt.</div>
      <div class="ir-snapshot"><div><strong>{{ report.config.repo }}</strong><span>{{ milestoneLabel(report.config) }}</span><span>{{ report.config.label }}</span></div><span><RadarIcon name="clock"/>{{ loading ? 'Refreshing · previous snapshot shown' : `Snapshot · ${snapshotTime}` }}</span></div>
      <div class="ir-metrics"><button v-for="metric in metrics" :key="metric.label" class="ir-metric" :class="[metric.tone ? `ir-metric-${metric.tone}` : '', { 'ir-metric-selected': filter === metric.filter && view === 'board' && !owner && !query }]" :aria-pressed="filter === metric.filter && view === 'board' && !owner && !query" type="button" @click="selectFilter(metric.filter)"><span>{{ metric.label }}</span><strong>{{ metric.value }}</strong><small>{{ metric.hint }}</small></button></div>
      <div class="ir-coverage-strip"><span class="ir-coverage-symbol"><RadarIcon :name="attentionCount ? 'pulse' : 'check'"/></span><div><strong>{{ report.totals.categorized ? `${coverage}% of QA issues have one team owner` : 'No QA-scoped issues in this snapshot' }}</strong><span>{{ attentionCount ? `${attentionCount} ownership ${attentionCount === 1 ? 'decision' : 'decisions'} to review. Start there, then compare effort.` : 'Ownership is clear. Review effort and sizing before planning the next handoff.' }}</span></div><div class="ir-coverage-track" :aria-label="`${coverage}% assignment coverage`" role="img"><span :style="{ width: `${coverage}%` }"></span></div><button v-if="attentionCount" type="button" class="ir-text-button" @click="selectFilter('problems')">Review gaps <RadarIcon name="arrow"/></button></div>

      <div ref="workspace" tabindex="-1" class="ir-workspace-heading"><div class="ir-view-tabs" role="tablist" aria-label="Issue Radar views"><button v-for="item in views" :id="`ir-tab-${item.id}`" :key="item.id" type="button" role="tab" :aria-selected="view === item.id" :tabindex="view === item.id ? 0 : -1" :aria-controls="`ir-view-${item.id}`" :class="{ 'ir-selected': view === item.id }" @click="activateView(item.id)" @keydown="navigateView"><RadarIcon :name="item.icon"/>{{ item.label }}</button></div><p class="ir-caption">{{ view === 'report' ? 'Assignment planning, grounded in your snapshot.' : 'One primary QA owner. Clear accountability.' }}</p></div>

      <section v-show="view === 'board'" id="ir-view-board" role="tabpanel" aria-labelledby="ir-tab-board">
        <div class="ir-section-heading"><div><h3>Workload, with context</h3><p class="ir-caption">Estimated QA points · XS 1 / S 2 / M 3 / L 5 / XL 8 · missing or conflicting sizes: 3 provisional · shared effort split</p></div><span v-if="workload.unownedPoints" class="ir-unowned">{{ pointLabel(workload.unownedPoints) }} points need an owner</span></div>
        <div class="ir-owner-grid"><button v-for="card in ownerCards" :key="card.user" class="ir-owner-card" :class="{ 'ir-owner-active': owner === card.user }" :aria-pressed="owner === card.user" type="button" @click="showOwner(card.user)"><div class="ir-row"><span class="ir-avatar">{{ card.user.slice(0, 2).toUpperCase() }}</span><strong>@{{ card.user }}</strong><RadarIcon :name="owner === card.user ? 'check' : 'arrow'"/></div><div class="ir-owner-load"><strong>{{ pointLabel(card.points) }}<small>est. points</small></strong><span>{{ card.totalAssigned }} {{ card.totalAssigned === 1 ? 'issue' : 'issues' }}</span></div><div class="ir-load-track"><span :style="{ width: `${card.points / maxLoad * 100}%` }"></span></div><p>{{ card.issueCount }} single owner <span>· {{ card.sharedCount }} shared</span><span v-if="card.provisional" class="ir-warning-text"> · {{ card.provisional }} provisional</span></p></button></div>
        <div class="ir-filter-bar"><label for="ir-search" class="ir-search"><span class="sr-only">Search issues</span><RadarIcon name="search"/><input id="ir-search" ref="searchInput" v-model="query" type="search" placeholder="Search title, #number, label, or owner…"><kbd aria-hidden="true">/</kbd></label><select v-model="filter" aria-label="Assignment filter"><option v-for="item in filters" :key="item.id" :value="item.id">{{ item.label }}</option></select><button type="button" class="ir-button ir-density" :aria-pressed="compact" @click="compact = !compact"><RadarIcon name="layers"/>{{ compact ? 'Compact' : 'Comfortable' }}</button><button v-if="owner" type="button" class="ir-chip" @click="owner = ''">@{{ owner }} <RadarIcon name="close"/></button><span class="ir-caption ir-visible-count" aria-live="polite">{{ visibleCount }} of {{ report.totals.fetched }}</span><button v-if="filter !== 'all' || query || owner" type="button" class="ir-text-button" @click="resetFilters">Clear filters</button></div>
        <div v-if="!visibleCount" class="ir-empty-results"><RadarIcon name="search"/><h3>{{ report.totals.fetched ? 'Nothing matches this view.' : 'A quiet queue.' }}</h3><p>{{ report.totals.fetched ? 'Try a different search or clear the filters to return to the full board.' : 'No open issues match the selected repository, labels, and milestone.' }}</p><button v-if="report.totals.fetched" type="button" class="ir-button" @click="resetFilters">Clear filters</button></div>
        <div v-if="displayedLanes.length && visibleCount" class="ir-board" aria-label="Issue owner lanes" tabindex="0"><section v-for="lane in displayedLanes" :key="lane.id" class="ir-lane" :class="`ir-lane-${lane.kind}`"><header class="ir-lane-heading"><span class="ir-lane-dot" aria-hidden="true"></span><h3>{{ lane.title }}</h3><span class="ir-count">{{ lane.issues.length }}<template v-if="lane.issues.length !== lane.total"> / {{ lane.total }}</template></span></header><div class="ir-lane-body"><IssueRadarCard v-for="issue in (expandedLanes.has(lane.id) ? lane.issues : lane.issues.slice(0, 20))" :key="issue.number" :issue="issue" @open="openIssue" @copy="copyIssue"/><p v-if="!lane.issues.length" class="ir-empty-lane"><RadarIcon name="check"/>{{ lane.kind === 'problem' ? 'All clear here.' : 'No issues in this lane.' }}</p><button v-if="lane.issues.length > 20" type="button" class="ir-button ir-show-more" @click="toggleLane(lane)">{{ expandedLanes.has(lane.id) ? 'Show first 20' : `Show all ${lane.issues.length} issues` }}</button></div></section></div>
      </section>
      <section v-show="view === 'summary'" id="ir-view-summary" role="tabpanel" aria-labelledby="ir-tab-summary" class="ir-summary">
        <div class="ir-summary-intro"><h3>Coverage at a glance</h3><p class="ir-muted">These totals cover the full snapshot, independent of board filters. Every in-scope issue appears once; QA/None is excluded.</p></div>
        <div class="ir-summary-split"><div class="ir-table-card"><h4>Assignments</h4><div class="ir-table-scroll"><table><thead><tr><th scope="col">Owner lane</th><th scope="col">Issues</th></tr></thead><tbody><tr v-for="row in report.summaryRows" :key="row.label"><th scope="row">{{ row.label }}</th><td>{{ row.total }}</td></tr></tbody></table></div></div><div class="ir-table-card"><h4>Issue kinds</h4><div class="ir-table-scroll"><table><thead><tr><th scope="col">Owner lane</th><th scope="col">Bugs</th><th scope="col">Enhancements</th><th scope="col">Other</th><th scope="col">Total</th></tr></thead><tbody><tr v-for="row in report.summaryRows" :key="row.label"><th scope="row">{{ row.label }}</th><td>{{ row.bug }}</td><td>{{ row.enhancement }}</td><td>{{ row.other }}</td><td>{{ row.total }}</td></tr></tbody></table></div></div></div>
        <div class="ir-table-card"><h4>QA size distribution</h4><div class="ir-table-scroll"><table><thead><tr><th scope="col">Owner lane</th><th v-for="label in QA_LABELS" :key="label" scope="col">{{ label === 'Lacks QA Size' ? 'Missing / conflicting' : label }}</th><th scope="col">Total</th></tr></thead><tbody><tr v-for="row in report.summaryRows" :key="row.label"><th scope="row">{{ row.label }}</th><td v-for="(count, index) in row.counts" :key="index" :class="{ 'ir-cell-warning': index === QA_LABELS.length - 1 && count }">{{ count }}</td><td>{{ row.total }}</td></tr></tbody></table></div></div>
      </section>


      <section v-show="view === 'report'" id="ir-view-report" role="tabpanel" aria-labelledby="ir-tab-report" class="ir-report">
        <div class="ir-prompt-intro"><div><span class="ir-eyebrow">An informed handoff</span><h3>Give your AI the full context.</h3><p>A complete assignment-planning prompt, your open queue, and up to 50 past issues per owner. Ready to analyze, balance, and turn into clear work packages.</p></div><span class="ir-prompt-seal"><RadarIcon name="file"/><strong>50</strong><small>past issues<br>per owner</small></span></div>
        <div class="ir-prompt-layout">
          <aside class="ir-planning">
            <div class="ir-planning-section"><span class="ir-eyebrow">01 / Assignment strategy</span><h4>How much should change?</h4><fieldset class="ir-strategy"><legend class="sr-only">Assignment strategy</legend><label :class="{ 'ir-strategy-selected': planning.mode === 'preserve' }"><input v-model="planning.mode" type="radio" value="preserve" name="ir-strategy"><span><strong>Preserve ownership</strong><small>Fill gaps, resolve shared issues, minimize handoffs.</small></span></label><label :class="{ 'ir-strategy-selected': planning.mode === 'rebalance' }"><input v-model="planning.mode" type="radio" value="rebalance" name="ir-strategy"><span><strong>Rebalance the team</strong><small>Consider the whole queue and explain every move.</small></span></label></fieldset></div>
            <div class="ir-planning-section"><span class="ir-eyebrow">02 / Team capacity</span><h4>Equal by default. Adjustable by person.</h4><p class="ir-caption">Relative availability, not a performance score. 50% means half a full share; 0% means no new work.</p><div class="ir-capacity-list"><label v-for="user in report.config.users" :key="user" :for="`ir-capacity-${user}`"><span>@{{ user }}</span><span class="ir-capacity-input"><input :id="`ir-capacity-${user}`" v-model.number="planning.capacities[user]" type="number" min="0" max="200" step="1" placeholder="100" @blur="planning.capacities[user] = normalizeCapacity(planning.capacities[user])" :aria-label="`Relative capacity for ${user}, percent`"><span>%</span></span></label></div></div>
            <div class="ir-planning-section"><label for="ir-planning-notes"><span class="ir-eyebrow">03 / Planning context</span><strong>Anything the AI should account for?</strong><textarea id="ir-planning-notes" v-model="planning.notes" maxlength="2000" rows="4" placeholder="Upcoming leave, release priorities, domain constraints, pairing opportunities…"></textarea></label><span class="ir-caption">{{ planning.notes.length }} / 2,000 · stays in the exported prompt</span></div>
          </aside>
          <div class="ir-prompt-document">
            <div class="ir-history"><div class="ir-section-heading"><div><h4>History gives the plan context.</h4><p class="ir-caption">Same repository and labels · all milestones · most recently updated closed issues</p></div><button v-if="!historyLoading" type="button" class="ir-text-button" :disabled="loading || scopeChanged" @click="loadHistory"><RadarIcon name="refresh"/>{{ history ? 'Refresh history' : 'Load history' }}</button><button v-else type="button" class="ir-text-button" @click="historyController?.abort()">Cancel</button></div><div v-if="historyLoading" class="ir-history-progress" role="status"><RadarIcon name="refresh" class="ir-spin"/><span>Gathering up to 50 past issues for each owner…</span></div><div class="ir-history-owners"><div v-for="row in historyRows" :key="row.user" :data-state="history ? row.status : 'waiting'"><span>@{{ row.user }}</span><strong>{{ history ? row.status === 'available' ? `${row.count} / 50` : 'Unavailable' : historyLoading ? 'Reading…' : 'Not loaded' }}</strong></div></div><p v-if="history && !historyPartial" class="ir-history-status"><RadarIcon name="check"/>{{ historyCount }} owner samples loaded. Shared historical issues are deduplicated in the prompt.</p><p v-if="historyPartial" class="ir-alert">Some history is unavailable. The prompt identifies missing evidence and keeps those recommendations provisional.</p><p v-for="warning in history?.warnings || []" :key="warning" class="ir-warning-text ir-caption">{{ warning }}</p><div v-if="historyError" class="ir-alert ir-alert-error" role="alert"><p>{{ historyError }}</p><button type="button" class="ir-text-button" v-if="!skipHistory" @click="skipHistory = true">Create with current issues only</button></div><p v-if="skipHistory" class="ir-caption ir-warning-text">History is explicitly marked as unavailable in this prompt.</p></div>
            <div class="ir-document-heading"><span class="ir-document-icon"><RadarIcon name="file"/></span><div><h4>AI assignment prompt</h4><p class="ir-caption">{{ report.totals.categorized }} QA issues · {{ report.config.users.length }} owners · full snapshot, independent of board filters</p></div><span class="ir-document-status" :data-ready="promptReady && !historyLoading">{{ historyLoading ? 'Preparing' : promptReady ? historyPartial || skipHistory ? 'Ready with notes' : 'Ready to use' : 'Awaiting history' }}</span></div>
            <div class="ir-prompt-preview"><template v-if="promptReady"><details><summary><span><RadarIcon name="eye"/>Preview the complete prompt</span><span class="ir-caption">{{ pointLabel(prompt.length) }} characters</span></summary><label for="ir-report-text" class="sr-only">AI assignment planning prompt</label><textarea id="ir-report-text" class="ir-report-text" readonly :value="prompt" spellcheck="false"></textarea></details></template><div v-else class="ir-prompt-wait"><RadarIcon name="file"/><strong>Your brief is taking shape.</strong><p>The prompt will include the full queue, weighted workload, history evidence, and a structured delegation plan.</p></div><ul class="ir-prompt-includes"><li><RadarIcon name="check"/>Balance effort and capacity</li><li><RadarIcon name="check"/>Explain every assignment</li><li><RadarIcon name="check"/>Create owner work packages</li><li><RadarIcon name="check"/>Reconcile every issue</li></ul></div>
            <div class="ir-export-bar"><div><strong>Your next step: review the proposed plan.</strong><p class="ir-caption">Paste into your AI assistant. Review its proposal before changing GitHub.</p></div><div class="ir-actions"><button type="button" class="ir-button" :disabled="exportDisabled" @click="savePrompt"><RadarIcon name="download"/>{{ saving ? 'Saving…' : 'Save .md' }}</button><button type="button" class="ir-button ir-primary" :disabled="exportDisabled" @click="copyPrompt"><RadarIcon :name="copying ? 'check' : 'copy'"/>{{ copying ? 'Copying…' : 'Copy assignment prompt' }}</button></div></div><p class="ir-caption ir-save-hint">Save to Downloads as issue-radar-assignment-prompt.md. Existing files are preserved.</p>
          </div>
        </div>
      </section>
      <footer class="ir-footer"><span><span class="ir-footer-dot"></span>Open issues only · pull requests excluded</span><span>{{ report.totals.fetched }} issues in this snapshot · no GitHub changes</span></footer>
    </template>
    <div v-if="message" class="ir-notice" :class="{ 'ir-notice-error': messageError }" role="status"><RadarIcon :name="messageError ? 'signal' : 'check'"/><span>{{ message }}</span><button type="button" aria-label="Dismiss notification" @click="message = ''"><RadarIcon name="close"/></button></div>
  </div>
</template>
