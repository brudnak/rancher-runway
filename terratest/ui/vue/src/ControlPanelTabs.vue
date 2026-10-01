<template>
  <div ref="navigation" class="workspace-nav" @keydown.esc="dismissMenu">
    <div class="panel-nav-primary">
      <button type="button" class="panel-nav-anchor" :aria-current="activeTab === 'home' ? 'page' : undefined" aria-label="Home" title="Home" @click="navigate('home')"><Icon name="home"/><span class="panel-nav-anchor-label">Home</span></button>
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
      <button type="button" class="panel-nav-mobile" :aria-expanded="Boolean(menu)" aria-controls="workspace-picker" @click="toggleMenu('all', $event)"><Icon :name="currentTool.icon"/><span><small>{{ currentGroup?.label || 'Your workspace' }}</small><strong>{{ currentTool.label }}</strong></span><Icon name="chevron"/></button>
      <button type="button" class="panel-nav-search" aria-label="Find a workspace" :aria-keyshortcuts="isMac ? 'Meta+j' : 'Control+j'" :aria-expanded="menu === 'all'" aria-controls="workspace-picker" @click="toggleMenu('all', $event)"><Icon name="search"/><span>Jump to…</span><kbd>{{ isMac ? '⌘' : 'Ctrl' }} J</kbd><span v-if="busyStatusAnnouncement" class="tab-status tab-status--busy panel-mobile-activity" aria-hidden="true"></span></button>
      <button type="button" class="panel-nav-anchor panel-nav-settings" :aria-current="activeTab === 'settings' ? 'page' : undefined" aria-label="Settings" title="Settings" @click="navigate('settings')"><Icon name="sliders"/></button>
    </div>
    <section v-if="menu" id="workspace-picker" class="panel-workspace-picker" aria-label="Workspace switcher">
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
import Icon from './HelmLabIcon.vue';
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { workspaceTool } from './home-workspace.mjs';
import { navigationGroups, navigationGroup, matchingNavigationGroups, navigationFocusIndex } from './panel-navigation.mjs';
import { state, activeTab, setActivePanelTab } from './store.js';
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
    searchInput.value?.focus({preventScroll:true});
  });
}
function toggleMenu(id, event) { if (menu.value === id) dismissMenu(); else openMenu(id, event.currentTarget); }
function dismissMenu() { if (!menu.value) return; menu.value = ''; opener?.focus({preventScroll:true}); window.scrollTo({top:openerScroll,behavior:'instant'}); }
function showAll() { menu.value = 'all'; query.value = ''; nextTick(() => searchInput.value?.focus()); }
function navigate(id) {
  menu.value = '';
  setActivePanelTab(id);
  nextTick(() => {
    if (id === 'home') return;
    const selector = id === 'settings' ? '.panel-nav-settings' : window.matchMedia('(max-width: 760px)').matches ? '.panel-nav-mobile' : '[data-active-workspace]';
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
    event.preventDefault(); if (menu.value) dismissMenu(); else openMenu('all', document.activeElement);
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
