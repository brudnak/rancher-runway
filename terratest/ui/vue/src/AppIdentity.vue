<template>
  <div class="app-identity">
    <button type="button" class="app-home" aria-label="Home — Rancher Runway" title="Go to Home" :aria-current="activeTab === 'home' ? 'page' : undefined" @click="setActivePanelTab('home')"><Icon name="home"/><span class="app-identity-name">Rancher Runway</span></button>
    <AppBuildStamp :brand="false" />
  </div>
</template>

<script setup>
import Icon from "./HelmLabIcon.vue";
import { activeTab, setActivePanelTab } from "./store.js";
import AppBuildStamp from "./AppBuildStamp.vue";
</script>
