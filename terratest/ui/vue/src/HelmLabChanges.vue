<script setup>
import { ref } from 'vue';
import { displayValue, sensitivePath } from './helmlab.mjs';
import HelmLabIcon from './HelmLabIcon.vue';
defineProps({ fields: Array, overrides: Object, env: Array });
defineEmits(['edit', 'reset']);
const reveal = ref(false);
function visible(path, value) { return sensitivePath(path) && !reveal.value && value ? '••••••••' : value || '(empty)'; }
</script>
<template>
  <div class="hl-changes">
    <div class="hl-changes-heading"><div><strong>Your changes</strong><p class="hl-help">Compared with this chart’s defaults.</p></div><button type="button" class="hl-text-button" :aria-pressed="reveal" @click="reveal = !reveal">{{ reveal ? 'Hide sensitive values' : 'Show sensitive values' }}</button></div>
    <div v-if="!fields.length && !env.length" class="hl-empty"><HelmLabIcon name="check" /><strong>Using chart defaults</strong><p class="hl-help">Your overrides will appear here as you edit.</p></div>
    <article v-for="field in fields" :key="field.path" class="hl-change">
      <div class="hl-row"><button type="button" class="hl-change-name" :aria-label="`Edit ${field.path}`" @click="$emit('edit', field)">{{ field.path }}</button><button type="button" class="hl-text-button" :aria-label="`Revert ${field.path}`" @click="$emit('reset', field.path)">Revert</button></div>
      <div class="hl-diff-before"><span aria-label="Chart default">−</span><code>{{ visible(field.path, displayValue(field)) }}</code></div>
      <div class="hl-diff-after"><span aria-label="Your value">+</span><code>{{ visible(field.path, overrides[field.path]) }}</code></div>
    </article>
    <article v-if="env.length" class="hl-change"><strong class="hl-value-name">extraEnv</strong><p class="hl-help">{{ env.length }} environment {{ env.length === 1 ? 'variable' : 'variables' }} supplied.</p><div v-for="(item, index) in env" :key="index" class="hl-diff-after"><span>+</span><code>{{ item.name || '(name required)' }}={{ reveal ? item.value : '••••••••' }}</code></div></article>
  </div>
</template>
