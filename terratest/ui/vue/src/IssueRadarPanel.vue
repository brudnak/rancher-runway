<script setup>
import AppBuildStamp from "./AppBuildStamp.vue";
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { apiFetch } from './store.js';
import { writeTextToClipboard } from './clipboard.js';
import IssueRadarCard from './IssueRadarCard.vue';
import { QA_LABELS, parseUsers, milestoneLabel, buildIssueRadarReport, visibleIssueLanes, buildIssueRadarBrief } from './issue-radar.mjs';

const preferenceKey = 'rancherRunwayIssueRadar';
let saved = {};
try { saved = JSON.parse(localStorage.getItem(preferenceKey) || '{}') || {}; } catch { /* Use defaults if preferences cannot be read. */ }
const preference = (key, fallback) => typeof saved[key] === 'string' ? saved[key] : fallback;
const form = reactive({ repo: preference('repo', 'rancher/rancher'), label: preference('label', 'team/frameworks'),
  users: preference('users', ''), milestone: preference('milestone', ''), scope: ['all', 'none', 'specific'].includes(saved.scope) ? saved.scope : 'specific' });
const report = ref(null);
const loading = ref(false);
const error = ref('');
const message = ref('');
const messageError = ref(false);
const view = ref('board');
const views = [{ id: 'board', label: 'Board' }, { id: 'summary', label: 'Summary' }, { id: 'report', label: 'Report' }];
const filter = ref('all');
const query = ref('');
const owner = ref('');
const expandedLanes = ref(new Set());
const milestones = ref([]);
const milestoneLoading = ref(false);
const milestoneError = ref('');
const includeHistory = ref(false);
const historyLimit = ref(30);
const history = ref(null);
const historyLoading = ref(false);
const historyError = ref('');
const saving = ref(false);
let controller;
let milestoneController;
let historyController;
const config = computed(() => ({ repo: form.repo.trim(), label: form.label.split(',').map(s => s.trim()).filter(Boolean).join(','),
  users: parseUsers(form.users), milestone: form.scope === 'specific' ? form.milestone.trim() : '', noMilestone: form.scope === 'none' }));
const scopeKey = value => JSON.stringify([value.repo, value.label, value.users, value.milestone, value.noMilestone]);
const scopeChanged = computed(() => report.value && scopeKey(config.value) !== scopeKey(report.value.config));
const lanes = computed(() => report.value ? visibleIssueLanes(report.value, { filter: filter.value, query: query.value, owner: owner.value }) : []);
const visibleCount = computed(() => lanes.value.reduce((sum, lane) => sum + lane.issues.length, 0));
const displayedLanes = computed(() => query.value.trim() || owner.value || ['unassigned', 'no-milestone', 'missing-size'].includes(filter.value)
  ? lanes.value.filter(lane => lane.issues.length) : lanes.value);
const brief = computed(() => report.value ? buildIssueRadarBrief(report.value, includeHistory.value ? history.value : null) : '');
const maxLoad = computed(() => Math.max(1, ...report.value?.userCards.map(card => card.totalAssigned) || []));
const filters = [
  { id: 'all', label: 'All issues' }, { id: 'problems', label: 'Needs attention' },
  { id: 'missing-owner', label: 'Needs a team owner' }, { id: 'shared', label: 'Multiple team owners' },
  { id: 'clean', label: 'One team owner' }, { id: 'unassigned', label: 'Unassigned' },
  { id: 'no-milestone', label: 'No milestone' }, { id: 'missing-size', label: 'Missing QA size' }, { id: 'qa-none', label: 'QA/None' },
];
const metrics = computed(() => {
  const t = report.value?.totals;
  return t ? [
    { label: 'In QA scope', value: t.categorized, hint: `${t.fetched} fetched in total`, filter: 'all' },
    { label: 'One team owner', value: t.exactlyOne, hint: 'Assignment covered', filter: 'clean', tone: 'good' },
    { label: 'Needs a team owner', value: t.missingOwner, hint: `${t.unassigned} unassigned · ${t.outsideTeam} outside team`, filter: 'missing-owner', tone: 'warning' },
    { label: 'Multiple team owners', value: t.overAssigned, hint: 'Review shared ownership', filter: 'shared', tone: 'warning' },
    { label: 'No milestone', value: t.noMilestone, hint: 'Issues in QA scope', filter: 'no-milestone' },
    { label: 'QA/None', value: t.qaNone, hint: 'Outside assignment checks', filter: 'qa-none' },
  ] : [];
});
const snapshotTime = computed(() => report.value ? new Date(report.value.generatedAt).toLocaleString() : '');
function notify(value, failed = false) { message.value = value; messageError.value = failed; }
function selectFilter(value) { filter.value = value; owner.value = ''; query.value = ''; view.value = 'board'; }
function showOwner(user) { owner.value = owner.value === user ? '' : user; filter.value = 'all'; query.value = ''; view.value = 'board'; }
function showAll(lane) { expandedLanes.value = new Set([...expandedLanes.value, lane.id]); }
function resetFilters() { filter.value = 'all'; query.value = ''; owner.value = ''; }
watch([filter, query, owner], () => { expandedLanes.value = new Set(); });
watch(form, () => {
  try { localStorage.setItem(preferenceKey, JSON.stringify(form)); } catch { /* Preferences are optional. */ }
}, { deep: true });
watch(() => form.repo, () => { milestoneController?.abort(); milestoneLoading.value = false; milestones.value = []; milestoneError.value = ''; });

async function runReport() {
  if (loading.value) return;
  error.value = ''; message.value = '';
  if (form.scope === 'specific' && !form.milestone.trim()) { error.value = 'Enter a milestone, or choose All milestones.'; return; }
  if (!config.value.users.length) { error.value = 'Add the GitHub usernames whose assignments you want to compare.'; return; }
  loading.value = true;
  controller = new AbortController();
  const request = controller;
  historyController?.abort();
  historyLoading.value = false;
  try {
    const response = await apiFetch('/api/issue-radar', { method: 'POST', body: JSON.stringify(config.value), signal: request.signal });
    const snapshot = await response.json();
    if (request.signal.aborted) return;
    report.value = buildIssueRadarReport(snapshot);
    history.value = null; historyError.value = ''; resetFilters(); view.value = 'board';
  } catch (err) {
    if (err.name === 'AbortError') error.value = 'Refresh cancelled. Any previous snapshot is still shown.';
    else error.value = err.message || 'The issue board could not be loaded.';
  } finally { if (controller === request) loading.value = false; }
}

async function loadMilestones() {
  milestoneController?.abort();
  milestoneController = new AbortController();
  const request = milestoneController;
  milestoneLoading.value = true; milestoneError.value = '';
  const repo = form.repo.trim();
  try {
    const response = await apiFetch('/api/issue-radar/milestones', { method: 'POST', body: JSON.stringify({ repo }), signal: request.signal });
    const data = await response.json();
    if (request.signal.aborted || repo !== form.repo.trim()) return;
    milestones.value = data.milestones;
    if (!data.milestones.length) milestoneError.value = 'This repository has no milestones. Choose All milestones or No milestone.';
  } catch (err) { if (err.name !== 'AbortError') milestoneError.value = err.message; }
  finally { if (milestoneController === request) milestoneLoading.value = false; }
}

async function loadHistory() {
  if (!report.value || historyLoading.value) return;
  historyController?.abort();
  historyController = new AbortController();
  const request = historyController;
  const snapshot = report.value;
  historyLoading.value = true; historyError.value = '';
  try {
    const response = await apiFetch('/api/issue-radar/history', { method: 'POST', body: JSON.stringify({ config: snapshot.config, limit: historyLimit.value }), signal: request.signal });
    const result = await response.json();
    if (!request.signal.aborted && report.value === snapshot) history.value = result;
  } catch (err) { if (err.name !== 'AbortError') historyError.value = err.message; }
  finally { if (historyController === request) historyLoading.value = false; }
}
watch(historyLimit, () => { historyController?.abort(); historyLoading.value = false; history.value = null; historyError.value = ''; });

async function copyReport() {
  try { await writeTextToClipboard(brief.value); notify('Report copied to clipboard.'); }
  catch (err) { notify(err.message || 'Could not copy the report.', true); }
}
async function saveReport() {
  if (!brief.value || saving.value) return;
  saving.value = true;
  try {
    const response = await apiFetch('/api/issue-radar/save', { method: 'POST', body: JSON.stringify({ content: brief.value }) });
    const data = await response.json();
    notify(`Saved ${data.filename} to Downloads.`);
  } catch (err) { notify(err.message || 'The report could not be saved.', true); }
  finally { saving.value = false; }
}
async function openIssue(url) {
  try { await apiFetch('/api/open-url', { method: 'POST', body: JSON.stringify({ url }) }); }
  catch (err) { notify(err.message || 'Could not open the issue.', true); }
}
function navigateView(event) {
  const current = views.findIndex(item => item.id === view.value);
  const next = event.key === 'ArrowRight' ? (current + 1) % views.length : event.key === 'ArrowLeft' ? (current + views.length - 1) % views.length
    : event.key === 'Home' ? 0 : event.key === 'End' ? views.length - 1 : -1;
  if (next < 0) return;
  event.preventDefault(); view.value = views[next].id; document.getElementById(`ir-tab-${view.value}`)?.focus();
}
onBeforeUnmount(() => { controller?.abort(); milestoneController?.abort(); historyController?.abort(); });
</script>

<template>
  <div class="issue-radar">
    <header class="ir-header">
      <div class="ir-title-group"><span class="ir-mark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><path d="m12 12 6.5-6.5"/><circle cx="12" cy="12" r="1" fill="currentColor"/></svg></span><div><h2>Issue Radar</h2><p class="ir-muted">Ownership, coverage, and the work still waiting.</p></div></div>
      <span class="ir-readonly"><span aria-hidden="true"></span>GitHub · read only</span>
    </header>

    <form class="ir-config" @submit.prevent="runReport" :aria-busy="loading">
      <div class="ir-config-heading"><div><h3>Set your team's scope</h3><p class="ir-caption">Use your GitHub CLI login. No token needs to be pasted here.</p></div></div>
      <div class="ir-form-grid">
        <label for="ir-repo">Repository<input id="ir-repo" v-model="form.repo" placeholder="rancher/rancher" autocomplete="off" spellcheck="false" :disabled="loading"></label>
        <label for="ir-label">Team labels<input id="ir-label" v-model="form.label" placeholder="area/frameworks" autocomplete="off" spellcheck="false" :disabled="loading"><span class="ir-caption">Comma-separated; issues must match every label.</span></label>
        <label for="ir-users" class="ir-form-wide">GitHub usernames<input id="ir-users" v-model="form.users" placeholder="octocat, another-owner" autocomplete="off" spellcheck="false" :disabled="loading"><span class="ir-caption">Up to eight people. Usernames define owner lanes; unassigned issues stay in the results.</span></label>
        <label for="ir-scope">Milestone scope<select id="ir-scope" v-model="form.scope" :disabled="loading"><option value="specific">Specific milestone</option><option value="all">All milestones</option><option value="none">No milestone</option></select></label>
        <div v-if="form.scope === 'specific'" class="ir-milestone-field"><label for="ir-milestone">Milestone title<input id="ir-milestone" v-model="form.milestone" placeholder="v2.14.0" autocomplete="off" :disabled="loading"></label><div class="ir-row"><button type="button" class="ir-text-button" :disabled="milestoneLoading || loading" @click="loadMilestones">{{ milestoneLoading ? 'Loading milestones…' : 'Load milestones from GitHub' }}</button></div><select v-if="milestones.length" aria-label="Choose a repository milestone" :disabled="loading" :value="form.milestone" @change="form.milestone = $event.target.value"><option value="" disabled>Choose a milestone…</option><option v-for="item in milestones" :key="item.number" :value="item.title">{{ item.title }}{{ item.state === 'closed' ? ' · closed' : '' }}</option></select><p v-if="milestoneError" class="ir-error-text" role="alert">{{ milestoneError }}<AppBuildStamp /></p></div>
        <div class="ir-form-submit"><button v-if="!loading" class="ir-button ir-primary" type="submit">{{ report ? 'Refresh board' : 'Build board' }} <span aria-hidden="true">↗</span></button><button v-else class="ir-button" type="button" @click="controller?.abort()"><span class="spinner" aria-hidden="true"></span> Cancel refresh</button><span class="ir-caption">{{ loading ? 'Reading all matching issue pages…' : 'Open issues only · pull requests excluded' }}</span></div>
      </div>
      <p v-if="error" class="ir-alert ir-alert-error" role="alert">{{ error }}<AppBuildStamp /></p>
    </form>

    <div v-if="!report" class="ir-empty-start"><svg viewBox="0 0 48 48" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><rect x="5" y="9" width="10" height="30" rx="3"/><rect x="19" y="9" width="10" height="22" rx="3"/><rect x="33" y="9" width="10" height="26" rx="3"/><path d="M8 15h4m10 0h4m10 0h4"/></svg><h3>A clear view of who's covering what.</h3><p>Choose a milestone, team labels, and GitHub usernames to see owner lanes, assignment gaps, and QA coverage.</p><div class="ir-empty-features"><span>Owner board</span><span>QA & kind summaries</span><span>Assignment reports</span></div></div>

    <template v-else>
      <div v-if="scopeChanged" class="ir-alert" role="status">Scope changed. The views below still show <strong>{{ report.config.repo }} · {{ milestoneLabel(report.config) }}</strong>. Refresh the board to apply your edits.</div>
      <div class="ir-snapshot"><div><strong>{{ report.config.repo }}</strong><span>{{ milestoneLabel(report.config) }}</span><span>{{ report.config.label }}</span></div><span>Snapshot · {{ snapshotTime }}</span></div>
      <div class="ir-metrics"><button v-for="metric in metrics" :key="metric.label" class="ir-metric" :class="metric.tone ? `ir-metric-${metric.tone}` : ''" type="button" @click="selectFilter(metric.filter)"><span>{{ metric.label }}</span><strong>{{ metric.value }}</strong><small>{{ metric.hint }}</small></button></div>

      <div class="ir-workspace-heading"><div class="ir-view-tabs" role="tablist" aria-label="Issue Radar views"><button v-for="item in views" :id="`ir-tab-${item.id}`" :key="item.id" type="button" role="tab" :aria-selected="view === item.id" :tabindex="view === item.id ? 0 : -1" :aria-controls="`ir-view-${item.id}`" :class="{ 'ir-selected': view === item.id }" @click="view = item.id" @keydown="navigateView">{{ item.label }}</button></div><p class="ir-caption">One selected team owner per issue; QA/None is tracked separately.</p></div>

      <section v-show="view === 'board'" id="ir-view-board" role="tabpanel" aria-labelledby="ir-tab-board">
        <div class="ir-owner-grid"><button v-for="card in report.userCards" :key="card.user" class="ir-owner-card" :class="{ 'ir-owner-active': owner === card.user }" :aria-pressed="owner === card.user" type="button" @click="showOwner(card.user)"><div class="ir-row"><span class="ir-avatar">{{ card.user.slice(0, 2).toUpperCase() }}</span><strong>@{{ card.user }}</strong><span class="ir-owner-count">{{ card.totalAssigned }}</span></div><div class="ir-load-track"><span :style="{ width: `${card.totalAssigned / maxLoad * 100}%` }"></span></div><p>{{ card.issueCount }} single owner <span>· {{ card.sharedCount }} shared</span></p><p class="ir-caption">{{ card.bug }} bugs · {{ card.enhancement }} enhancements · {{ card.lacksQaSize }} missing size</p></button></div>
        <div class="ir-filter-bar"><label for="ir-search" class="ir-search"><span class="sr-only">Search issues</span><input id="ir-search" v-model="query" type="search" placeholder="Search title, #number, label, or owner…"></label><select v-model="filter" aria-label="Assignment filter"><option v-for="item in filters" :key="item.id" :value="item.id">{{ item.label }}</option></select><button v-if="owner" type="button" class="ir-chip" @click="owner = ''">@{{ owner }} ×</button><span class="ir-caption">{{ visibleCount }} of {{ report.totals.fetched }} issues</span><button v-if="filter !== 'all' || query || owner" type="button" class="ir-text-button" @click="resetFilters">Reset filters</button></div>
        <p v-if="!visibleCount" class="ir-empty-lane">{{ report.totals.fetched ? 'No issues match these filters.' : 'No open issues match this repository, milestone, and team-label scope.' }}</p>
        <div v-if="displayedLanes.length && visibleCount" class="ir-board"><section v-for="lane in displayedLanes" :key="lane.id" class="ir-lane" :class="`ir-lane-${lane.kind}`"><header class="ir-lane-heading"><span class="ir-lane-dot" aria-hidden="true"></span><h3>{{ lane.title }}</h3><span class="ir-count">{{ lane.issues.length }}<template v-if="lane.issues.length !== lane.total"> / {{ lane.total }}</template></span></header><div class="ir-lane-body"><IssueRadarCard v-for="issue in (expandedLanes.has(lane.id) ? lane.issues : lane.issues.slice(0, 20))" :key="issue.number" :issue="issue" @open="openIssue"/><p v-if="!lane.issues.length" class="ir-empty-lane">Nothing in this lane.</p><button v-if="lane.issues.length > 20 && !expandedLanes.has(lane.id)" type="button" class="ir-button ir-show-more" @click="showAll(lane)">Show all {{ lane.issues.length }} issues</button></div></section></div>
      </section>

      <section v-show="view === 'summary'" id="ir-view-summary" role="tabpanel" aria-labelledby="ir-tab-summary" class="ir-summary">
        <div class="ir-summary-intro"><h3>Coverage at a glance</h3><p class="ir-muted">These totals cover the full snapshot, independent of board filters. Every in-scope issue appears once; QA/None is excluded.</p></div>
        <div class="ir-summary-split"><div class="ir-table-card"><h4>Assignments</h4><div class="ir-table-scroll"><table><thead><tr><th scope="col">Owner lane</th><th scope="col">Issues</th></tr></thead><tbody><tr v-for="row in report.summaryRows" :key="row.label"><th scope="row">{{ row.label }}</th><td>{{ row.total }}</td></tr></tbody></table></div></div><div class="ir-table-card"><h4>Issue kinds</h4><div class="ir-table-scroll"><table><thead><tr><th scope="col">Owner lane</th><th scope="col">Bugs</th><th scope="col">Enhancements</th><th scope="col">Other</th><th scope="col">Total</th></tr></thead><tbody><tr v-for="row in report.summaryRows" :key="row.label"><th scope="row">{{ row.label }}</th><td>{{ row.bug }}</td><td>{{ row.enhancement }}</td><td>{{ row.other }}</td><td>{{ row.total }}</td></tr></tbody></table></div></div></div>
        <div class="ir-table-card"><h4>QA size distribution</h4><div class="ir-table-scroll"><table><thead><tr><th scope="col">Owner lane</th><th v-for="label in QA_LABELS" :key="label" scope="col">{{ label }}</th><th scope="col">Total</th></tr></thead><tbody><tr v-for="row in report.summaryRows" :key="row.label"><th scope="row">{{ row.label }}</th><td v-for="(count, index) in row.counts" :key="index" :class="{ 'ir-cell-warning': index === QA_LABELS.length - 1 && count }">{{ count }}</td><td>{{ row.total }}</td></tr></tbody></table></div></div>
      </section>

      <section v-show="view === 'report'" id="ir-view-report" role="tabpanel" aria-labelledby="ir-tab-report" class="ir-report">
        <div class="ir-report-heading"><div><h3>Assignment review, ready to take with you.</h3><p class="ir-muted">The full snapshot, owner workload, QA summaries, and every issue link in one Markdown report.</p></div><div class="ir-actions"><button type="button" class="ir-button" @click="copyReport">Copy report</button><button type="button" class="ir-button ir-primary" :disabled="saving" @click="saveReport">{{ saving ? 'Saving…' : 'Save to Downloads' }}</button></div></div>
        <div class="ir-history"><label class="ir-checkbox"><input v-model="includeHistory" type="checkbox">Include recent closed issue samples</label><p class="ir-caption">Optional context for each selected owner, using the same team labels across all milestones. Sorted by most recently updated.</p><div v-if="includeHistory" class="ir-actions"><select v-model.number="historyLimit" aria-label="History samples per owner"><option :value="30">Up to 30 per owner</option><option :value="50">Up to 50 per owner</option></select><button type="button" class="ir-button" :disabled="historyLoading || loading" @click="loadHistory">{{ historyLoading ? 'Loading history…' : history ? 'Refresh history' : 'Load history' }}</button><span v-if="!history" class="ir-caption">History is not yet included in the report.</span></div><p v-if="includeHistory && historyError" class="ir-error-text" role="alert">{{ historyError }}<AppBuildStamp /></p><p v-for="warning in (includeHistory ? history?.warnings || [] : [])" :key="warning" class="ir-warning-text">{{ warning }}</p></div>
        <p v-if="message" class="ir-alert" :class="{ 'ir-alert-error': messageError }" role="status">{{ message }}<AppBuildStamp /></p>
        <label for="ir-report-text" class="sr-only">Assignment report Markdown</label><textarea id="ir-report-text" class="ir-report-text" readonly :value="brief" spellcheck="false"></textarea><p class="ir-caption">Saves as issue-radar-report.md. Existing files are preserved with numbered copies.</p>
      </section>
      <p v-if="message && view !== 'report'" class="ir-alert" :class="{ 'ir-alert-error': messageError }" role="status">{{ message }}<AppBuildStamp /></p>
    </template>
  </div>
</template>
