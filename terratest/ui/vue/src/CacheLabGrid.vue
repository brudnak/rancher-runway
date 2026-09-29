<script setup>
import { cacheCell, cacheRecord } from './cache-lab.mjs';
import Icon from './HelmLabIcon.vue';
defineProps({ result: Object, busy: Boolean, sort: String, descending: Boolean });
defineEmits(['row', 'sort']);
</script>
<template>
  <div class="cache-data-grid" tabindex="0" aria-label="Scrollable query results" :aria-busy="busy">
    <table v-if="result?.columns?.length"><thead><tr><th class="cache-row-number" scope="col">#</th><th v-for="(column,i) in result.columns" :key="i" scope="col"><button v-if="sort !== undefined" type="button" @click="$emit('sort',column)">{{ column }}<span v-if="sort===column">{{ descending ? '↓' : '↑' }}</span></button><span v-else>{{ column }}</span></th><th scope="col"><span class="lab-sr-only">Inspect</span></th></tr></thead><tbody><tr v-for="(row,index) in result.rows" :key="index"><td class="cache-row-number">{{ index+1 }}</td><td v-for="(value,i) in row" :key="i" :class="{'cache-null':value===null}"><span :title="cacheCell(value).slice(0,1000)">{{ cacheCell(value).slice(0,240) }}</span></td><td class="cache-row-open"><button type="button" :aria-label="`Inspect row ${index+1}`" @click="$emit('row',cacheRecord(result.columns,row))"><Icon name="eye" /></button></td></tr></tbody></table>
    <div v-if="result && !result.rows?.length" class="cache-grid-empty"><Icon name="search" /><strong>No matching rows</strong><p>Try a different filter or query.</p></div>
  </div>
</template>
