<template>
  <span class="app-build-stamp" :title="buildTitle(appBuild)" aria-label="Rancher Runway build">
    <span v-if="brand">Rancher Runway</span>
    <strong>{{ buildLabel(appBuild) }}</strong>
  </span>
</template>

<script setup>
import { appBuild } from "./store.js";
import { buildLabel, buildTitle } from "./build-info.mjs";
defineProps({ brand: { type: Boolean, default: true } });
</script>
