<template>
  <div ref="navigation" class="workspace-nav" @keydown.esc="dismissMenu">
    <div class="panel-nav-primary">
      <button type="button" class="panel-nav-anchor panel-nav-destination" :aria-current="activeTab === 'home' ? 'page' : undefined" aria-label="Home" title="Home" @click="navigate('home')"><Icon name="home"/><span class="panel-nav-anchor-label">Home</span></button>
      <button v-for="tool in primaryNavigationTools" :key="tool.id" type="button" class="panel-nav-anchor panel-nav-destination" :data-primary-workspace="tool.id" :aria-current="activeTab === tool.id ? 'page' : undefined" :aria-label="tool.label" :title="tool.label" @click="navigate(tool.id)"><Icon :name="tool.icon"/><span class="panel-nav-anchor-label">{{tool.label}}</span></button>
      <div class="panel-nav-groups">
        <button v-for="group in navigationGroups" :key="group.id" type="button" class="panel-nav-group"
          :class="{ 'is-current': currentGroup?.id === group.id }" :data-active-workspace="currentGroup?.id === group.id || undefined"
          :aria-label="groupLabel(group)" :aria-expanded="menu === group.id" aria-controls="workspace-picker"
          :title="groupBusy(group).map(item => item.label).join('. ') || group.description"
          @click="toggleMenu(group.id, $event)">
          <Icon :name="group.icon"/><span class="panel-group-copy"><strong>{{ group.label }}</strong><span>{{ currentGroup?.id === group.id ? currentTool.label : `${group.tabs.length} workspaces` }}</span></span><Icon name="chevron" class="panel-nav-chevron"/>
          <span v-if="groupBusy(group).length" class="tab-status tab-status--busy" :data-group-status="group.id" aria-hidden="true"></span>
        </button>
      </div>
      <button type="button" class="panel-nav-mobile" v-show="activeTab !== 'home' && !primaryNavigationTools.some(tool => tool.id === activeTab)" :aria-expanded="Boolean(menu) && menu !== 'app'" aria-controls="workspace-picker" @click="toggleMenu('all', $event)"><Icon :name="currentTool.icon"/><span><small>{{ currentGroup?.label || 'Your workspace' }}</small><strong>{{ currentTool.label }}</strong></span><Icon name="chevron"/></button>
      <button type="button" class="panel-live-clusters" :class="{'has-live-clusters': liveClusters > 0, 'is-stale': !!refreshError}" :aria-current="activeTab === 'clusters' ? 'page' : undefined" :title="clusterStatusTitle" :aria-label="`${clusterStatusTitle} Open Clusters.`" @click="navigate('clusters')"><span class="panel-live-dot" aria-hidden="true"></span><span>{{ clusterStatusLoading ? 'Clusters …' : `Live clusters ${liveClusters}` }}</span><small v-if="startingClusters">+{{ startingClusters }} starting</small><span v-if="refreshError" aria-hidden="true">?</span></button>
      <button type="button" class="panel-nav-search" aria-label="Find a workspace" :aria-keyshortcuts="isMac ? 'Meta+j' : 'Control+j'" :aria-expanded="menu === 'all'" aria-controls="workspace-picker" @click="toggleMenu('all', $event)"><Icon name="search"/><span>Jump to…</span><kbd>{{ isMac ? '⌘' : 'Ctrl' }} J</kbd><span v-if="busyStatusAnnouncement" class="tab-status tab-status--busy panel-mobile-activity" aria-hidden="true"></span></button>
      <button type="button" class="panel-nav-anchor panel-nav-settings" :aria-current="activeTab === 'settings' ? 'page' : undefined" aria-label="Runway menu" title="Runway menu" :aria-expanded="menu === 'app'" aria-controls="runway-app-menu" @click="toggleMenu('app', $event)"><Icon name="sliders"/><span v-if="lifecycleRunning" class="runway-menu-activity" aria-label="Operation running"></span></button>
      <span class="panel-nav-version" :title="buildTitle(appBuild)" aria-label="Rancher Runway version">{{appBuild.version ? `v${String(appBuild.version).replace(/^v/i, '')}` : '—'}}</span>
    </div>
    <div v-if="['tests','cache','packages'].includes(activeTab)" class="panel-cluster-return">
      <button type="button" @click="backToCluster">← {{ clusterReturnContext ? `Back to ${clusterReturnContext.name}` : 'Back to Clusters' }}</button>
      <span v-if="clusterReturnContext">Opened from {{ clusterReturnContext.tab === 'history' ? 'Retained History' : 'Clusters' }}<span v-if="clusterReturnContext.runId"> · Run {{ clusterReturnContext.runId }}</span></span>
      <button v-if="clusterReturnContext" type="button" class="panel-all-clusters" @click="navigate('clusters')">All clusters</button>
    </div>
    <RunwayAppMenu v-if="menu === 'app'" @settings="navigate('settings')" @dismiss="dismissMenu"/>
    <section v-if="menu && menu !== 'app'" id="workspace-picker" class="panel-workspace-picker" aria-label="Workspace switcher">
      <div class="panel-picker-search"><Icon name="search"/><input ref="searchInput" v-model="query" type="search" autocomplete="off" spellcheck="false" aria-label="Search workspaces" placeholder="Find a tool, task, or keyword…" @keydown="searchKeys"/><button type="button" aria-label="Close workspace switcher" @click="dismissMenu"><Icon name="close"/><kbd>Esc</kbd></button></div>
      <div class="panel-picker-heading"><span>{{ query.trim() ? `${matches.length} matching ${matches.length === 1 ? 'workspace' : 'workspaces'}` : menu === 'all' ? 'ALL WORKSPACES' : selectedMenuGroup?.description }}</span><button v-if="menu !== 'all' && !query.trim()" type="button" @click="showAll">All workspaces <Icon name="arrow"/></button></div>
      <div v-if="visibleGroups.length" class="panel-picker-groups" :class="{ 'panel-picker-single': visibleGroups.length === 1 }" @keydown="resultKeys">
        <section v-for="group in visibleGroups" :key="group.id" :aria-label="group.title">
          <h3>{{ group.title }}</h3>
          <div class="panel-picker-items">
            <!-- Status markers are positioned within each tab so refreshes never change tab geometry. -->
            <button v-for="tab in group.tabs" :key="tab.id" type="button" class="panel-workspace-option" data-workspace-option
              :aria-current="activeTab === tab.id ? 'page' : undefined" :aria-label="tabAriaLabel(tab)"
              :aria-busy="tabStatus(tab.id).busy ? 'true' : undefined" :data-operation-status="tabStatus(tab.id).kind || undefined"
              @click="navigate(tab.id)">
              <span class="panel-option-icon"><Icon :name="tab.icon"/><span v-if="tabStatus(tab.id).visible" :data-tab-status="tab.id" :data-status-kind="tabStatus(tab.id).kind" aria-hidden="true" class="tab-status" :class="`tab-status--${tabStatus(tab.id).kind}`"></span></span>
              <span class="panel-option-copy"><strong>{{ tab.label }}<span v-if="activeTab === tab.id" class="panel-option-current">Current</span></strong><span>{{ tab.description }}</span><em v-if="tabStatus(tab.id).busy">{{ tabStatus(tab.id).label }}</em></span>
            </button>
          </div>
        </section>
      </div>
      <div v-else class="panel-picker-empty"><Icon name="search"/><strong>No workspaces found</strong><p>Try “database”, “deploy”, “PR”, or “local”.</p><button type="button" @click="showAll">Browse all workspaces</button></div>
      <footer class="panel-picker-footer"><span>Your work stays open.</span><span><kbd>↑</kbd><kbd>↓</kbd> to move <kbd>Enter</kbd> to open</span></footer>
    </section>
  </div>
  <span class="sr-only" aria-live="polite" aria-atomic="true">{{ busyStatusAnnouncement }}</span>
</template>

<script setup>
import RunwayAppMenu from "./RunwayAppMenu.vue";
import {buildTitle} from "./build-info.mjs";
import Icon from './HelmLabIcon.vue';
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { workspaceTool } from './home-workspace.mjs';
import { primaryNavigationTools, navigationGroups, navigationGroup, matchingNavigationGroups, navigationFocusIndex } from './panel-navigation.mjs';
import { state, activeTab, setActivePanelTab, appBuild, lifecycleRunning, bootPending, refreshError } from './store.js';
import {clusterReturnContext,returnToCluster,selectedClusterWorkspaceId} from './cluster-workspace-store.mjs';
const discoveredClusters = computed(() => [...new Map((state.value?.clusters?.items || []).filter(item => item.id).map(item => [item.id,item])).values()]);
const liveClusters = computed(() => discoveredClusters.value.filter(item => item.reachable).length);
const startingClusters = computed(() => discoveredClusters.value.filter(item => item.provisioning && !item.reachable).length);
const clusterStatusLoading = computed(() => bootPending.value && !state.value?.clusters?.updatedAt && !discoveredClusters.value.length);
const clusterStatusTitle = computed(() => clusterStatusLoading.value ? 'Checking cluster status.' : `${liveClusters.value} reachable cluster${liveClusters.value === 1 ? '' : 's'}, ${startingClusters.value} starting. ${refreshError.value ? 'Last known status; refresh failed.' : 'Based on the latest cluster discovery.'}`);
function backToCluster() {if(!clusterReturnContext.value){navigate('clusters');return;}menu.value='';returnToCluster();}
const tabs = navigationGroups.flatMap(group => group.tabs);
const navigation = ref(null), searchInput = ref(null), menu = ref(''), query = ref('');
const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
const currentGroup = computed(() => navigationGroup(activeTab.value));
const currentTool = computed(() => workspaceTool(activeTab.value) || {id:'home',label:'Home',icon:'home'});
const selectedMenuGroup = computed(() => navigationGroups.find(group => group.id === menu.value));
const visibleGroups = computed(() => matchingNavigationGroups(query.value, menu.value));
const matches = computed(() => visibleGroups.value.flatMap(group => group.tabs));
let opener, openerScroll = 0;
function openMenu(id, target) {
  opener = target || navigation.value?.querySelector('.panel-nav-search'); openerScroll = window.scrollY; menu.value = id; query.value = '';
  nextTick(() => {
    const picker = navigation.value?.querySelector('#workspace-picker');
    const bounds = picker?.getBoundingClientRect();
    if (bounds && (bounds.bottom > window.innerHeight || bounds.top < 64)) navigation.value?.closest('nav')?.scrollIntoView({block:'start'});
    if (id === 'app') navigation.value?.querySelector('#runway-app-menu button')?.focus({preventScroll:true});
    else searchInput.value?.focus({preventScroll:true});
  });
}
function toggleMenu(id, event) { if (menu.value === id) dismissMenu(); else openMenu(id, event.currentTarget); }
function dismissMenu() { if (!menu.value) return; menu.value = ''; opener?.focus({preventScroll:true}); window.scrollTo({top:openerScroll,behavior:'instant'}); }
function showAll() { menu.value = 'all'; query.value = ''; nextTick(() => searchInput.value?.focus()); }
function navigate(id) {
  menu.value = '';
  if (id === 'clusters') selectedClusterWorkspaceId.value = ''; 
  setActivePanelTab(id);
  nextTick(() => {
    if (id === 'home') return;
    if (id === 'clusters') document.querySelector('.cluster-home')?.scrollIntoView({block:'start',behavior:'auto'});
    const primary = primaryNavigationTools.some(tool => tool.id === id);
    const selector = primary ? `[data-primary-workspace="${id}"]` : id === 'settings' ? '.panel-nav-settings' : window.matchMedia('(max-width: 760px)').matches ? '.panel-nav-mobile' : '[data-active-workspace]';
    navigation.value?.querySelector(selector)?.focus({preventScroll:true});
  });
}
function options() { return [...(navigation.value?.querySelectorAll('[data-workspace-option]') || [])]; }
function searchKeys(event) {
  if (!['ArrowDown','ArrowUp','Enter'].includes(event.key)) return;
  const items = options(); if (!items.length) return;
  event.preventDefault();
  if (event.key === 'Enter') navigate(matches.value[0].id);
  else items[event.key === 'ArrowUp' ? items.length-1 : 0]?.focus();
}
function resultKeys(event) {
  const items = options(), index = items.indexOf(event.target.closest('button'));
  if (index < 0 || event.altKey || event.metaKey || event.ctrlKey) return;
  const next = navigationFocusIndex(event.key, index, items.length);
  if (next !== null) { event.preventDefault(); items[next].focus(); }
}
function outsidePointer(event) { if (menu.value && !navigation.value?.contains(event.target)) menu.value = ''; }
function outsideFocus(event) { if (menu.value && !navigation.value?.contains(event.target)) menu.value = ''; }
function shortcut(event) {
  if ((event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 'j') {
    event.preventDefault(); if (menu.value && menu.value !== 'app') dismissMenu(); else openMenu('all', document.activeElement);
  }
}
onMounted(() => { document.addEventListener('pointerdown', outsidePointer); document.addEventListener('focusin', outsideFocus); document.addEventListener('keydown', shortcut); });
onBeforeUnmount(() => { document.removeEventListener('pointerdown', outsidePointer); document.removeEventListener('focusin', outsideFocus); document.removeEventListener('keydown', shortcut); });

const clusterItems = currentState => (
  currentState && currentState.clusters && Array.isArray(currentState.clusters.items)
    ? currentState.clusters.items
    : []
);

const activeK3DClusterCount = currentState => {
  const clusters = Array.isArray(currentState?.k3d?.clusters) ? currentState.k3d.clusters : [];
  return clusters.filter(cluster => ["creating", "running"].includes(cluster.status)).length;
};

const status = (kind = "", label = "") => ({
  kind,
  label,
  busy: kind === "busy",
  visible: Boolean(kind && label),
});

const countLabel = (count, singular, plural = `${singular}s`) => (
  count ? `${count} ${Number(count) === 1 ? singular : plural}` : ""
);

const contentStatus = (count, singular, plural = `${singular}s`) => (
  count ? status("content", countLabel(count, singular, plural)) : status()
);

const statuses = computed(() => {
  const runs = Array.isArray(state.value?.workspace?.runs) ? state.value.workspace.runs : [];
  const clusters = clusterItems(state.value);
  const awsItems = Array.isArray(state.value?.aws?.items) ? state.value.aws.items : [];
  const k3dCount = activeK3DClusterCount(state.value);
  const k3dRunning = Boolean(state.value?.k3d?.operation?.running);
  const steveRunning = Boolean(state.value?.steve?.operation?.running);
  const awsSetupRunning = Boolean(state.value?.setup?.running);
  const linodeSetupRunning = Boolean(state.value?.linodeSetup?.running);

  return {
    setup: awsSetupRunning
      ? status("busy", "AWS setup running; lifecycle actions are locked")
      : linodeSetupRunning
        ? status("busy", "Linode setup running; lifecycle actions are locked")
        : status(),
    runs: contentStatus(runs.length, "recorded run"),
    clusters: contentStatus(clusters.length, "cluster record"),
    aws: state.value?.awsCleanup?.running
      ? status("busy", "AWS inventory cleanup running; lifecycle actions are locked")
      : contentStatus(awsItems.length, "visible AWS resource"),
    destroy: contentStatus(runs.length, "run available to destroy", "runs available to destroy"),
    k3d: k3dRunning
      ? status("busy", "K3D operation running; K3D controls are locked")
      : contentStatus(k3dCount, "active K3D cluster"),
    tests: state.value?.testLab?.running ? status("busy", "Local tests running in Test Lab") : status(),
    steve: steveRunning
      ? status("busy", "Steve Lab operation running; Steve Lab controls are locked")
      : status(),
  };
});

const tabStatus = tab => statuses.value[tab] || status();
const groupBusy = group => group.tabs.map(tab => tabStatus(tab.id)).filter(item => item.busy);
const groupLabel = group => `${group.label}${currentGroup.value?.id === group.id ? `, current workspace: ${currentTool.value.label}` : ''}${groupBusy(group).length ? `, ${groupBusy(group).length} running ${groupBusy(group).length === 1 ? 'operation' : 'operations'}` : ''}`;

const tabAriaLabel = tab => {
  const label = tabStatus(tab.id).label;
  return label ? `${tab.label}, ${label}` : tab.label;
};

const busyStatusAnnouncement = computed(() => tabs
  .map(tab => tabStatus(tab.id))
  .filter(item => item.busy)
  .map(item => item.label)
  .join(". "));

</script>

<style scoped>
.panel-live-clusters{display:inline-flex;align-items:center;justify-content:center;gap:6px;align-self:center;flex-shrink:0;min-height:36px;padding:6px 10px;border:1px solid var(--runway-border);border-radius:9px;background:var(--runway-soft);color:var(--runway-muted);font-size:11px;font-weight:650;white-space:nowrap;cursor:pointer}.panel-live-clusters.has-live-clusters{color:var(--runway-accent);border-color:color-mix(in srgb,var(--runway-accent) 30%,var(--runway-border))}.panel-live-clusters.is-stale{color:var(--runway-gold)}.panel-live-dot{width:6px;height:6px;border-radius:50%;background:currentColor}.panel-live-clusters small{font-size:10px;color:var(--runway-muted)}.panel-cluster-return{display:flex;align-items:center;gap:12px;flex-wrap:wrap;padding:10px 12px;border-top:1px solid var(--runway-border);font-size:11px;color:var(--runway-muted)}.panel-cluster-return button{border:1px solid var(--runway-border);border-radius:7px;padding:7px 10px;color:var(--runway-accent);background:var(--runway-soft);font:inherit;font-weight:650;cursor:pointer;overflow-wrap:anywhere;text-align:left}.panel-cluster-return .panel-all-clusters{margin-left:auto;border:0;background:transparent}.panel-live-clusters:focus-visible,.panel-cluster-return button:focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}@media(max-width:760px){.panel-live-clusters{font-size:10px;padding:5px 7px;gap:4px}.panel-live-clusters small{display:none}.panel-cluster-return{gap:6px}.panel-cluster-return>span{flex-basis:100%;order:3}}@media(max-width:420px){.panel-live-clusters{white-space:normal;max-width:92px;line-height:1.2}}
</style>
