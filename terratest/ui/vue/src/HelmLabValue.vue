<script setup>
import { computed, ref } from 'vue';
import { displayValue, sensitivePath } from './helmlab.mjs';
import { fieldPresentation } from './helm-presentation.mjs';
import { randomBootstrapPassword } from './helm-workbench.mjs';
import HelmLabIcon from './HelmLabIcon.vue';
const props = defineProps({ field: Object, value: String, changed: Boolean, explicit: Boolean, error: String });
const currentValue = computed(() => props.value ?? displayValue(props.field));
const presentation = computed(() => fieldPresentation(props.field));
const descriptionId = computed(() => `hl-help-${props.field.path}${props.error ? ` hl-error-${props.field.path}` : ''}`);
defineEmits(['update', 'reset', 'blur']);
const revealed = ref(false);
</script>

<template>
  <div class="hl-value" :class="{ 'hl-value-changed': explicit, 'hl-value-invalid': error, 'hl-value-structured': field.type === 'yaml' }" @focusout="$emit('blur')">
    <div class="hl-row hl-value-heading">
      <div class="hl-field-title"><label :for="`hl-${field.path}`">{{ presentation.label }}</label><code class="hl-field-key">{{ field.path }}</code></div>
      <div class="hl-row hl-row-tight">
        <span class="hl-badge" :class="{ 'hl-badge-muted': !explicit }">{{ explicit ? (changed ? 'Override' : 'Pinned') : 'Inherited' }}</span>
        <button v-if="explicit" type="button" class="hl-revert" :aria-label="`Revert ${field.path}`" title="Use inherited value" @click="$emit('reset')"><HelmLabIcon name="undo" /></button>
      </div>
    </div>
    <div v-if="explicit && value === null" class="hl-note">Explicit null <button :id="`hl-${field.path}`" type="button" @click="$emit('update', displayValue(field))">Set a value</button></div>
    <button v-else-if="field.type === 'boolean'" :id="`hl-${field.path}`" type="button" role="switch" :aria-checked="currentValue === 'true'" :aria-label="presentation.label" :aria-invalid="Boolean(error)" :aria-describedby="descriptionId" class="hl-boolean" @click="$emit('update', currentValue === 'true' ? 'false' : 'true')"><span class="hl-switch-track" :class="{ 'hl-switch-on': currentValue === 'true' }"><span></span></span>{{ currentValue === 'true' ? 'Enabled' : field.value === undefined && !explicit ? 'Inherited' : 'Disabled' }}</button>
    <select v-else-if="field.choices" :id="`hl-${field.path}`" :value="currentValue" :aria-invalid="Boolean(error)" :aria-describedby="descriptionId" @change="$emit('update', $event.target.value)">
      <option v-if="field.value === undefined && !explicit && !field.choices.includes('')" value="" disabled>Inherited · choose to override</option>
      <option v-for="choice in field.choices" :key="String(choice)" :value="String(choice)">{{ choice === '' ? 'Use Rancher’s default' : choice }}</option>
    </select>
    <textarea v-else-if="field.type === 'yaml'" :id="`hl-${field.path}`" rows="4" :value="currentValue" :aria-invalid="Boolean(error)" :aria-describedby="descriptionId" spellcheck="false" @input="$emit('update', $event.target.value)" />
    <div v-else class="hl-input-row" :class="{ 'hl-secret-input': sensitivePath(field.path) }">
      <input :id="`hl-${field.path}`" :type="field.type === 'number' ? 'number' : sensitivePath(field.path) && !revealed ? 'password' : 'text'" :step="field.integer ? '1' : 'any'" :min="field.minimum" :max="field.maximum" :value="currentValue" :placeholder="presentation.placeholder" :aria-invalid="Boolean(error)" :aria-describedby="descriptionId" autocomplete="off" spellcheck="false" @input="$emit('update', $event.target.value)">
      <button v-if="sensitivePath(field.path)" type="button" class="hl-reveal" :aria-label="`${revealed ? 'Hide' : 'Show'} ${field.path}`" :aria-pressed="revealed" @click="revealed = !revealed"><HelmLabIcon :name="revealed ? 'eye-off' : 'eye'" /></button>
    </div>
    <p v-if="error" :id="`hl-error-${field.path}`" class="hl-error-text"><HelmLabIcon name="signal" />{{ error }}</p>
    <div :id="`hl-help-${field.path}`" class="hl-field-support">
      <p v-if="presentation.hint" class="hl-help">{{ presentation.hint }}</p>
      <button v-if="field.path === 'bootstrapPassword'" type="button" class="hl-text-button hl-generate" @click="$emit('update', randomBootstrapPassword())"><HelmLabIcon name="key" /> Generate a strong password</button>
      <details v-if="presentation.documentation" class="hl-field-docs"><summary>Chart notes</summary><p>{{ presentation.documentation }}</p></details>
      <p v-if="explicit" class="hl-default">Chart default: <code>{{ sensitivePath(field.path) && field.value ? '••••••' : field.value === undefined ? 'Not set' : displayValue(field) || 'Empty' }}</code></p>
    </div>
  </div>
</template>
