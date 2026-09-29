<script setup>
import { computed } from 'vue';
import { cacheObject, cacheCell } from './cache-lab.mjs';
const props = defineProps({ value: null, depth: { type: Number, default: 0 } });
const parsed = computed(() => props.depth < 20 ? cacheObject(props.value) : props.value);
const tree = computed(() => props.depth < 20 && parsed.value !== null && typeof parsed.value === 'object');
const entries = computed(() => tree.value ? Object.entries(parsed.value).slice(0, 100) : []);
</script>
<template>
  <details v-if="tree" class="cache-json-node" :open="depth < 2"><summary>{{ Array.isArray(parsed) ? 'Array' : 'Object' }} <span>{{ Object.keys(parsed).length }} {{ Array.isArray(parsed) ? 'items' : 'fields' }}</span></summary><dl><div v-for="[key, item] in entries" :key="key"><dt>{{ key }}</dt><dd><CacheLabValue :value="item" :depth="depth + 1" /></dd></div></dl><small v-if="Object.keys(parsed).length > 100">First 100 fields shown. Copy the record for all fields.</small></details>
  <span v-else class="cache-json-value" :data-null="parsed === null">{{ cacheCell(parsed).slice(0,3000) }}<small v-if="cacheCell(parsed).length>3000"> … Copy the record for the full value.</small></span>
</template>
