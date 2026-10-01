<script setup>
import {computed} from 'vue';
import Icon from './HelmLabIcon.vue';
import ControlPanelHeader from './ControlPanelHeader.vue';
import {fullscreen,theme,setTheme,setPanelFullscreen,refreshChecks,manualRefreshInFlight,stopPanel,lifecycleRunning,state,quitPending,quitError} from './store.js';
import {refreshMyWork} from './my-work-store.mjs';
defineEmits(['settings','dismiss']);
const busy=computed(()=>lifecycleRunning.value||state.value?.testLab?.running);
function refreshWorkspace(){refreshChecks();void refreshMyWork({force:true});}
</script>
<template>
 <section id="runway-app-menu" class="runway-app-menu" aria-label="Runway controls">
  <header><strong>Rancher Runway</strong><button aria-label="Close Runway menu" @click="$emit('dismiss')"><Icon name="close"/></button></header>
  <div class="runway-menu-appearance"><span>Appearance</span><div role="group" aria-label="Color theme"><button :aria-pressed="theme==='light'" @click="setTheme('light')">Light</button><button :aria-pressed="theme==='dark'" @click="setTheme('dark')">Dark</button></div></div>
  <button class="runway-menu-action" :aria-pressed="fullscreen" @click="setPanelFullscreen(!fullscreen)"><Icon name="expand"/><span>{{fullscreen?'Exit fullscreen':'Enter fullscreen'}}</span></button>
  <button class="runway-menu-action" :disabled="manualRefreshInFlight" @click="refreshWorkspace"><Icon name="refresh"/><span>{{manualRefreshInFlight?'Checking…':'Refresh checks'}}</span></button>
  <div class="runway-menu-status"><ControlPanelHeader/></div>
  <button class="runway-menu-action" @click="$emit('settings')"><Icon name="sliders"/><span>All settings</span><Icon name="arrow"/></button>
  <div class="runway-menu-quit"><button class="runway-menu-action" :disabled="busy||quitPending" @click="stopPanel"><Icon name="power"/><span>{{quitPending?'Quitting…':'Quit Runway'}}</span></button><p v-if="busy">A run is active. Stop it in its workspace before quitting.</p><p v-if="quitError" role="alert">{{quitError}}</p></div>
 </section>
</template>
<style scoped>
.runway-app-menu{position:absolute;right:0;top:calc(100% + 8px);width:330px;max-width:calc(100vw - 32px);max-height:calc(100vh - 110px);overflow:auto;overscroll-behavior:contain;padding:14px;border:1px solid var(--runway-border);border-radius:14px;background:var(--runway-card);color:var(--runway-ink);box-shadow:0 18px 55px #0004;font-size:12px}
.runway-app-menu header{display:flex;align-items:center;justify-content:space-between;padding:3px 7px 13px;gap:12px}.runway-app-menu header strong{font-size:13px;font-weight:600}.runway-app-menu header button{padding:6px;border-radius:6px;color:var(--runway-muted)}
.runway-menu-appearance{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px 8px;margin-bottom:5px}.runway-menu-appearance>div{display:flex;padding:3px;gap:3px;background:var(--runway-soft);border:1px solid var(--runway-border);border-radius:8px}.runway-menu-appearance button{padding:5px 12px;border-radius:5px;color:var(--runway-muted);font-size:11px!important}.runway-menu-appearance button[aria-pressed=true]{background:var(--runway-raised);color:var(--runway-ink)}
.runway-menu-action{display:flex;align-items:center;width:100%;gap:12px;padding:11px 8px!important;border-radius:8px;text-align:left;font-size:12px!important;color:var(--runway-ink)}.runway-menu-action>span{flex:1}.runway-menu-action>svg{color:var(--runway-muted)}.runway-menu-action:hover,.runway-app-menu header button:hover{background:var(--runway-raised)}.runway-menu-status{padding:7px 8px;border-top:1px solid var(--runway-border);border-bottom:1px solid var(--runway-border);margin:8px 0}.runway-menu-status :deep(.runway-status-popover){position:static;width:auto;padding:8px 0;box-shadow:none;border:0}.runway-menu-status :deep(.runway-status-details){width:100%}.runway-menu-status :deep(.panel-chip){font-size:10px;min-height:26px;padding:4px 8px}.runway-menu-quit{margin-top:8px;border-top:1px solid var(--runway-border);padding-top:6px}.runway-menu-quit p{font-size:11px;color:var(--runway-muted);line-height:1.6;padding:4px 8px}.runway-menu-quit p[role=alert]{color:var(--runway-error)}
</style>
