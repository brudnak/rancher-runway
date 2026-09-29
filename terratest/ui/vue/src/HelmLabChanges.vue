<script setup>
import { ref } from 'vue';
import { changedField, displayValue, sensitivePath } from './helmlab.mjs';
import HelmLabIcon from './HelmLabIcon.vue';
defineProps({ fields: Array, overrides: Object, env: Array, envExplicit: Boolean });
defineEmits(['edit', 'reset', 'reset-env']);
const reveal = ref(false);
function visible(path, value, structured = false) { return (sensitivePath(path) || structured) && !reveal.value && value ? '••••••••' : value === null ? 'null' : value || '(empty)'; }
</script>
<template>
  <div class="hl-changes">
    <div class="hl-changes-heading"><div><strong>Explicit overrides</strong><p class="hl-help">These values take precedence over inherited values.</p></div><button type="button" class="hl-text-button" :aria-pressed="reveal" @click="reveal = !reveal">{{ reveal ? 'Hide private values' : 'Reveal private values' }}</button></div>
    <div v-if="!fields.length && !env.length && !envExplicit" class="hl-empty"><HelmLabIcon name="check" /><strong>No explicit overrides</strong><p class="hl-help">The release uses its inherited values.</p></div>
    <article v-for="field in fields" :key="field.path" class="hl-change">
      <div class="hl-row"><button type="button" class="hl-change-name" :aria-label="`Edit ${field.path}`" @click="$emit('edit', field)">{{ field.path }} <HelmLabIcon name="arrow" /></button><button type="button" class="hl-text-button" :aria-label="`Revert ${field.path}`" @click="$emit('reset', field.path)">Revert</button></div>
      <div v-if="changedField(field, overrides)" class="hl-diff-before"><span>Default</span><code>{{ field.value === undefined ? 'Not set' : visible(field.path, displayValue(field)) }}</code></div>
      <div class="hl-diff-after"><span>{{ changedField(field, overrides) ? 'Yours' : 'Pinned' }}</span><code>{{ visible(field.path, overrides[field.path], field.type === 'yaml') }}</code></div>
      <p v-if="!changedField(field, overrides)" class="hl-help">Pins the chart default, even if the installed release has a different value.</p>
    </article>
    <article v-if="env.length || envExplicit" class="hl-change">
      <div class="hl-row"><strong class="hl-value-name">extraEnv</strong><button type="button" class="hl-text-button" aria-label="Revert environment overrides" @click="$emit('reset-env')">Revert</button></div>
      <template v-if="env.length"><p class="hl-help">{{ env.length }} environment {{ env.length === 1 ? 'variable' : 'variables' }} supplied.</p><div v-for="(item, index) in env" :key="index" class="hl-diff-after"><span>Yours</span><code>{{ item.name || '(name required)' }}={{ reveal ? item.value : '••••••••' }}</code></div></template>
      <template v-else><div class="hl-diff-after"><span>Yours</span><code>[]</code></div><p class="hl-help">Explicitly clears environment variables inherited from the installed release.</p></template>
    </article>
  </div>
</template>
