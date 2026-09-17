<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { filterVersions } from './helmlab.mjs';
import HelmLabIcon from './HelmLabIcon.vue';
const props = defineProps({ modelValue: String, versions: Array, latest: String, disabled: Boolean });
const emit = defineEmits(['update:modelValue']);
const root = ref(null), trigger = ref(null), searchInput = ref(null), open = ref(false), query = ref('');
const matches = computed(() => filterVersions(props.versions || [], query.value));
const custom = computed(() => query.value.trim() && !/\s/.test(query.value.trim()) && !props.versions?.some(item => item.version === query.value.trim()));
async function toggle() {
  open.value = !open.value;
  if (open.value) { query.value = ''; await nextTick(); searchInput.value?.focus(); }
}
function select(version) { emit('update:modelValue', version); close(); }
function close() { open.value = false; trigger.value?.focus(); }
function outside(event) { if (open.value && !root.value?.contains(event.target)) open.value = false; }
function navigate(event) {
  if (event.key === 'Escape') { event.preventDefault(); close(); return; }
  if (!['ArrowDown', 'ArrowUp'].includes(event.key)) return;
  event.preventDefault();
  const options = [...root.value.querySelectorAll('[data-version-option]')];
  if (!options.length) return;
  const current = options.indexOf(event.target);
  const next = event.key === 'ArrowDown' ? (current + 1) % options.length : (current <= 0 ? options.length : current) - 1;
  options[next].focus();
}
onMounted(() => document.addEventListener('pointerdown', outside));
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside));
</script>

<template>
  <div ref="root" class="hl-version-picker" @keydown="navigate" @focusout="event => { if (event.relatedTarget && !root.contains(event.relatedTarget)) open = false; }">
    <span id="hl-version-label" class="hl-input-label">Chart version</span>
    <button ref="trigger" type="button" class="hl-version-trigger" aria-labelledby="hl-version-label hl-version-selected" :aria-expanded="open" aria-controls="hl-version-popover" :disabled="disabled" @click="toggle">
      <span><span v-if="!modelValue" class="hl-latest-label">Latest</span><span id="hl-version-selected">{{ modelValue || latest || 'Choose a chart version' }}</span></span><HelmLabIcon name="chevron" />
    </button>
    <p class="hl-help">{{ modelValue ? 'Pinned to this version.' : 'The generated command is pinned to the latest resolved version.' }}</p>
    <div v-if="open" id="hl-version-popover" class="hl-version-popover" role="region" aria-label="Choose chart version">
      <div class="hl-version-search"><HelmLabIcon name="search" /><input ref="searchInput" v-model="query" type="search" aria-label="Search chart versions" placeholder="Search versions or enter a custom version" autocomplete="off" spellcheck="false"></div>
      <div class="hl-version-options">
        <button type="button" data-version-option class="hl-version-option" :aria-pressed="!modelValue" @click="select('')"><span><strong>Use latest</strong><small>{{ latest }}</small></span><HelmLabIcon v-if="!modelValue" name="check" /></button>
        <button v-for="item in matches.slice(0, 50)" :key="item.version" type="button" data-version-option class="hl-version-option" :aria-pressed="modelValue === item.version" @click="select(item.version)"><span><strong>{{ item.version }}</strong><small v-if="item.created">Published {{ new Date(item.created).toLocaleDateString() }}</small></span><HelmLabIcon v-if="modelValue === item.version" name="check" /></button>
        <button v-if="custom" type="button" data-version-option class="hl-version-option hl-custom-version" @click="select(query.trim())"><span><strong>Use “{{ query.trim() }}”</strong><small>Custom version · values use the latest chart as a reference</small></span><HelmLabIcon name="arrow" /></button>
      </div>
      <div class="hl-version-footer"><span>{{ matches.length }} published versions{{ matches.length > 50 ? ' · showing 50; type to narrow' : '' }}</span><button type="button" class="hl-text-button" @click="close">Done</button></div>
    </div>
  </div>
</template>
