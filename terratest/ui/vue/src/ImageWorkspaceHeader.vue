<script setup>
import Icon from './HelmLabIcon.vue';
defineProps({ title: String, description: String, icon: String, eyebrow: String, timestamp: String });
</script>
<template>
  <header class="iw-header">
    <div class="iw-brand"><span class="iw-emblem" aria-hidden="true"><Icon :name="icon"/></span><div><span class="iw-eyebrow">Rancher Runway / {{ eyebrow }}</span><h2>{{ title }}</h2><p>{{ description }}</p></div></div>
    <div class="iw-header-meta"><span class="iw-readonly"><span></span>Read-only workspace</span><span v-if="timestamp" class="iw-caption">{{ timestamp }}</span></div>
  </header>
</template>
