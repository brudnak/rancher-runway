<script setup>
import { computed, ref } from 'vue';
import { displayValue, sensitivePath } from './helmlab.mjs';
const props = defineProps({ field: Object, value: String, changed: Boolean, error: String });
const currentValue = computed(() => props.value ?? displayValue(props.field));
defineEmits(['update', 'reset']);
const revealed = ref(false);
</script>

<template>
  <div class="hl-value" :class="{ 'hl-value-changed': changed, 'hl-value-invalid': error }">
    <div class="hl-row">
      <label :for="`hl-${field.path}`" class="hl-value-name">{{ field.path }}</label>
      <div class="hl-row hl-row-tight">
        <span v-if="changed" class="hl-badge">Modified</span>
        <button v-if="changed" type="button" class="hl-text-button" :aria-label="`Reset ${field.path}`" @click="$emit('reset')">Reset</button>
      </div>
    </div>
    <button v-if="field.type === 'boolean'" :id="`hl-${field.path}`" type="button" role="switch" :aria-checked="currentValue === 'true'" :aria-label="field.path" :aria-describedby="`hl-help-${field.path}`" class="hl-boolean" @click="$emit('update', currentValue === 'true' ? 'false' : 'true')"><span class="hl-switch-track" :class="{ 'hl-switch-on': currentValue === 'true' }"><span></span></span>{{ currentValue === 'true' ? 'Enabled' : 'Disabled' }}</button>
    <select v-else-if="field.choices" :id="`hl-${field.path}`" :value="value ?? displayValue(field)" :aria-invalid="Boolean(error)" :aria-describedby="`hl-help-${field.path}`" @change="$emit('update', $event.target.value)">
      <option v-for="choice in field.choices || [true, false]" :key="String(choice)" :value="String(choice)">{{ choice }}</option>
    </select>
    <textarea v-else-if="field.type === 'yaml'" :id="`hl-${field.path}`" rows="4" :value="value ?? displayValue(field)" :aria-invalid="Boolean(error)" :aria-describedby="`hl-help-${field.path}`" spellcheck="false" @input="$emit('update', $event.target.value)" />
    <div v-else class="hl-input-row">
      <input :id="`hl-${field.path}`" :type="field.type === 'number' ? 'number' : sensitivePath(field.path) && !revealed ? 'password' : 'text'" :step="field.integer ? '1' : 'any'" :min="field.minimum" :max="field.maximum" :value="value ?? displayValue(field)" :aria-invalid="Boolean(error)" :aria-describedby="`hl-help-${field.path}`" autocomplete="off" spellcheck="false" @input="$emit('update', $event.target.value)">
      <button v-if="sensitivePath(field.path)" type="button" class="hl-text-button" :aria-label="`${revealed ? 'Hide' : 'Show'} ${field.path}`" @click="revealed = !revealed">{{ revealed ? 'Hide' : 'Show' }}</button>
    </div>
    <div :id="`hl-help-${field.path}`">
      <p v-if="error" class="hl-error-text">{{ error }}</p>
      <p v-else-if="field.description" class="hl-help">{{ field.description }}</p>
      <p v-if="changed" class="hl-default">Default: <code>{{ sensitivePath(field.path) && field.value ? '••••••' : displayValue(field) || '(empty)' }}</code></p>
    </div>
  </div>
</template>
