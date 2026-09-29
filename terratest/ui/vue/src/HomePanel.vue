<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import Icon from './HelmLabIcon.vue';
import {money} from './cloud-workspace.mjs';
import { activeTab, previousWorkspaceTab, state, bootPending, refreshError, refreshChecks, manualRefreshInFlight, setActiveDestroyTab, setActivePanelTab } from './store.js';
import { filterWorkspaceTools, guidedPaths, homeSnapshot, toolGroups, workspaceTool } from './home-workspace.mjs';

const search = ref(''), searchInput = ref(null), pathId = ref('deploy');
const snapshot = computed(() => homeSnapshot(state.value, { bootPending: bootPending.value, error: refreshError.value }));
const resume = computed(() => workspaceTool(previousWorkspaceTab.value));
const path = computed(() => guidedPaths.find(item => item.id === pathId.value) || guidedPaths[0]);
const matches = computed(() => filterWorkspaceTools(search.value));
const groups = computed(() => toolGroups.map(group => ({ ...group, tools: matches.value.filter(tool => tool.group === group.id) })).filter(group => group.tools.length));
function openTool(tab) { setActivePanelTab(tab); }
function openCosts(){setActiveDestroyTab('costs');openTool('destroy');}
const cleanupCandidates=computed(()=>(state.value?.aws?.items||[]).filter(item=>item.cleanupEligible).length);
async function exploreTools() { await nextTick(); searchInput.value?.scrollIntoView({ block: 'center' }); searchInput.value?.focus({ preventScroll: true }); }
function shortcut(event) {
  if (activeTab.value !== 'home' || event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey || event.target.closest?.('input,textarea,select,[contenteditable="true"]')) return;
  event.preventDefault(); exploreTools();
}
function navigatePaths(event) {
  const index = guidedPaths.findIndex(item => item.id === pathId.value);
  const next = event.key === 'ArrowRight' ? (index + 1) % guidedPaths.length : event.key === 'ArrowLeft' ? (index + guidedPaths.length - 1) % guidedPaths.length : event.key === 'Home' ? 0 : event.key === 'End' ? guidedPaths.length - 1 : -1;
  if (next < 0) return;
  event.preventDefault(); pathId.value = guidedPaths[next].id;
  nextTick(() => document.getElementById(`home-path-${pathId.value}`)?.focus());
}
onMounted(() => window.addEventListener('keydown', shortcut));
onBeforeUnmount(() => window.removeEventListener('keydown', shortcut));
</script>

<template>
  <div class="runway-home">
    <section class="home-hero" aria-labelledby="home-title">
      <div class="home-hero-copy">
        <span class="home-eyebrow"><span class="home-spark" aria-hidden="true">✦</span> Rancher Runway / Your starting point</span>
        <h1 id="home-title" tabindex="-1">Your next test<br>starts <em>here.</em></h1>
        <p>Provision Rancher, understand a build, or experiment locally. A place for every step—and a clear way to get there.</p>
        <div class="home-hero-actions">
          <button type="button" class="home-primary" @click="openTool(snapshot.action)">{{ snapshot.actionLabel }} <Icon name="arrow"/></button>
          <button v-if="resume && resume.id !== snapshot.action" type="button" class="home-resume" @click="openTool(resume.id)"><Icon name="undo"/>Back to {{ resume.label }}</button>
          <button v-else type="button" class="home-resume" @click="exploreTools">Explore the tools <Icon name="search"/></button>
        </div>
        <span class="home-hero-footnote">Opening a workspace never starts an operation.</span>
      </div>
      <aside class="home-pulse" :data-tone="snapshot.tone" aria-label="Workspace overview">
        <div class="home-pulse-top"><span class="home-eyebrow">Right now</span><span class="home-status" role="status" aria-atomic="true"><i :class="{ 'home-breathe': snapshot.checking || (!snapshot.stale && snapshot.cloud.length) }"></i>{{ snapshot.label }}</span></div>
        <h2>{{ snapshot.title }}</h2><p>{{ snapshot.detail }}</p>
        <div v-if="refreshError" class="home-state-error" role="status"><span>{{ refreshError }}</span><button type="button" @click="refreshChecks" :disabled="manualRefreshInFlight">{{ manualRefreshInFlight ? 'Refreshing…' : 'Retry checks' }}<Icon name="refresh"/></button></div>
        <div class="home-pulse-lines">
          <button type="button" @click="openTool(snapshot.cloud[0]?.tab || 'runs')"><span class="home-line-icon"><Icon name="globe"/></span><span><strong>Cloud workspace</strong><small>{{ !snapshot.known ? 'Waiting for workspace state' : snapshot.cloud.length ? snapshot.cloud.map(item => item.label).join(' · ') : `${snapshot.runs} recorded ${snapshot.runs === 1 ? 'run' : 'runs'} · no active operation` }}</small></span><Icon name="arrow"/></button>
          <button type="button" @click="openTool(snapshot.steve || state.steve?.operation?.running ? 'steve' : 'k3d')"><span class="home-line-icon"><Icon name="flask"/></span><span><strong>Local experiments</strong><small>{{ !snapshot.known ? 'Waiting for workspace state' : `${snapshot.k3d} active K3D · ${snapshot.steve} active Steve${snapshot.localBusy ? ' · operation running' : ''}` }}</small></span><Icon name="arrow"/></button>
          <button type="button" @click="openTool('aws')"><span class="home-line-icon"><Icon name="layers"/></span><span><strong>AWS inventory</strong><small>{{ !snapshot.known ? 'Waiting for inventory state' : `${snapshot.aws} resources shown${snapshot.awsRefreshing ? ' · refreshing' : ' · review in inventory'}` }}</small></span><Icon name="arrow"/></button>
        </div>
        <div class="home-pulse-note"><Icon :name="snapshot.stale ? 'signal' : 'lock'"/><span>{{ snapshot.stale ? 'Activity may be out of date.' : 'Each workspace keeps its own checks and approval steps.' }}</span></div>
      </aside>
    </section>

    <section class="home-paths" aria-labelledby="home-paths-title">
      <div class="home-section-heading"><div><span class="home-eyebrow">A good place to begin</span><h2 id="home-paths-title">What are you working on?</h2></div><div class="home-path-tabs" role="tablist" aria-label="Suggested workflows"><button v-for="item in guidedPaths" :id="`home-path-${item.id}`" :key="item.id" type="button" role="tab" :aria-selected="pathId === item.id" :tabindex="pathId === item.id ? 0 : -1" aria-controls="home-path-content" @click="pathId = item.id" @keydown="navigatePaths"><Icon :name="item.icon"/>{{ item.label }}</button></div></div>
      <div id="home-path-content" role="tabpanel" :aria-labelledby="`home-path-${pathId}`" tabindex="0">
        <p class="home-path-description">{{ path.description }}</p>
        <ol class="home-path-steps"><li v-for="(step,index) in path.steps" :key="step.title"><span class="home-step-number">0{{ index + 1 }}</span><div><h3>{{ step.title }}</h3><p>{{ step.detail }}</p><button type="button" @click="openTool(step.tab)">{{ workspaceTool(step.tab).label }} <Icon name="arrow"/></button></div><Icon v-if="index < path.steps.length - 1" name="arrow" class="home-step-arrow"/></li></ol>
      </div>
    </section>

    <section class="home-parallel" aria-labelledby="home-parallel-title">
      <div class="home-parallel-story"><span class="home-eyebrow"><Icon name="branch"/> Two tracks. One workspace.</span><h2 id="home-parallel-title">While the cloud gets ready,<br><em>keep exploring.</em></h2><p>Steve Lab and K3D Lab run locally in Docker, independently of cloud provisioning. You can open either while a run is building, then return to Runs to check its progress.</p><div class="home-parallel-note"><Icon name="terminal"/><span>Each lab creates its own cluster and kubeconfig. Local prerequisites, ports, and available memory still apply.</span></div><button type="button" class="home-text-link" @click="openTool('runs')">{{ snapshot.provisioning && !snapshot.stale ? 'Follow your provisioning run' : 'Find cloud progress in Runs' }} <Icon name="arrow"/></button></div>
      <div class="home-parallel-tools">
        <button type="button" class="home-lab-link" @click="openTool('steve')"><span class="home-lab-icon"><Icon name="flask"/></span><span><span class="home-eyebrow">Explore the API</span><strong>Steve Lab</strong><span>Try a Steve ref, follow startup, and inspect the endpoint and SQL cache. It supplies its own k3d environment.</span></span><Icon name="arrow"/></button>
        <button type="button" class="home-lab-link" @click="openTool('k3d')"><span class="home-lab-icon"><Icon name="boxes"/></span><span><span class="home-eyebrow">Explore Kubernetes</span><strong>K3D Lab</strong><span>Spin up a K3s cluster for a local test. Stop, restart, or remove it from the lab’s session controls.</span></span><Icon name="arrow"/></button>
        <p class="home-parallel-foot">You can also <button type="button" @click="openTool('helm')">prepare Helm values</button> or <button type="button" @click="openTool('pr-builds')">check a PR’s image</button> while you wait.</p>
      </div>
    </section>

    <section class="home-cloud-journal" aria-labelledby="home-cloud-journal-title"><div class="home-section-heading"><div><span class="home-eyebrow">AFTER THE EXPERIMENT</span><h2 id="home-cloud-journal-title">Clean up. Keep the history.</h2><p>A clear place for what is left in AWS, and what your Runway environments were estimated to cost.</p></div></div><div class="home-cloud-cards"><article><div class="home-cloud-card-top"><span class="home-line-icon"><Icon name="globe"/></span><span class="home-eyebrow">AWS INVENTORY / LEFTOVERS</span><span v-if="snapshot.known&&!snapshot.stale&&cleanupCandidates" class="home-cloud-count">{{ cleanupCandidates }} candidates</span></div><h3>Find resources without a local run.</h3><p>AWS Inventory finds matching tagged resources, explains what is protected, and lets you review eligible orphaned resources for cleanup. Every deletion rechecks dependencies and requires typed confirmation.</p><button class="home-text-link" @click="openTool('aws')">Inspect & clean up leftovers <Icon name="arrow"/></button></article><article><div class="home-cloud-card-top"><span class="home-line-icon"><Icon name="pulse"/></span><span class="home-eyebrow">DESTROY / LOCAL COST JOURNAL</span></div><h3>See the cost of your experiments.</h3><p>After a successful Runway AWS destroy, available estimates are saved locally. Explore service breakdowns and trends, export a backup, or restore history from another installation.</p><div class="home-cost-preview"><strong>{{ snapshot.known&&!state.costs?.error&&state.costs?.totals?money(state.costs.totals.lifetime):'Local history' }}</strong><span>Recorded estimates · AWS Price List rates × observed resource age</span></div><button class="home-text-link" @click="openCosts">Explore costs & local data <Icon name="arrow"/></button></article></div><p class="home-cloud-footnote">The journal estimates supported EC2, EBS, RDS/Aurora, and load-balancer charges from Runway cleanup. It is not your AWS bill; live runs, orphan cleanup, and untracked services are not included. Coverage and limitations are explained in the journal.</p></section>

    <section class="home-directory" aria-labelledby="home-directory-title">
      <div class="home-section-heading"><div><span class="home-eyebrow">Know your workspace</span><h2 id="home-directory-title">Every tool, a clear purpose.</h2><p>Go directly to what you need. Your open work stays in place when you switch tabs.</p></div><label class="home-search"><Icon name="search"/><span class="sr-only">Find a workspace tool</span><input ref="searchInput" v-model="search" type="search" placeholder="Find a tool or task…" @keydown.esc="search = ''"><kbd aria-hidden="true">/</kbd></label></div>
      <p v-if="search.trim()" class="home-search-count" role="status">{{ matches.length }} {{ matches.length === 1 ? 'tool matches' : 'tools match' }} “{{ search.trim() }}”<button type="button" @click="search = ''; searchInput?.focus()">Clear search</button></p>
      <div v-if="groups.length" class="home-tool-groups"><section v-for="group in groups" :key="group.id" :aria-label="group.title"><header><h3>{{ group.title }}</h3><p>{{ group.description }}</p></header><button v-for="tool in group.tools" :key="tool.id" type="button" class="home-tool" @click="openTool(tool.id)"><Icon :name="tool.icon"/><span><span class="home-tool-title"><strong>{{ tool.label }}</strong><small>{{ tool.kind }}</small></span><span class="home-tool-description">{{ tool.description }}</span></span><Icon name="arrow"/></button></section></div>
      <div v-else class="home-no-results"><Icon name="search"/><h3>No tools found for that search.</h3><p>Try “Kubernetes”, “image”, “logs”, or “cleanup”.</p><button type="button" class="home-text-link" @click="search = ''; searchInput?.focus()">Show every tool <Icon name="arrow"/></button></div>
    </section>
    <footer class="home-footer"><span><Icon name="home"/>A familiar place to come back to.</span><span>Use Home in the top bar from any workspace.</span></footer>
  </div>
</template>
