<template>
  <div class="app-identity">
    <span class="app-identity-name">Rancher Runway</span>
    <AppBuildStamp :brand="false" />
  </div>
</template>

<script setup>
import AppBuildStamp from "./AppBuildStamp.vue";
</script>
