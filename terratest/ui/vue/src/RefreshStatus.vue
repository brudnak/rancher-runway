<script setup>
defineProps({ refreshing: Boolean, label: { type: String, required: true } });
</script>

<template>
  <p class="panel-refresh-status" role="status">
    <span class="panel-refresh-indicator" :class="{ 'is-refreshing': refreshing }" aria-hidden="true"></span>
    <!-- Both labels reserve the same space, including when a narrow window wraps. -->
    <span class="panel-refresh-labels">
      <span :class="{ 'is-hidden': !refreshing }" :aria-hidden="!refreshing">Refreshing {{ label }}…</span>
      <span :class="{ 'is-hidden': refreshing }" :aria-hidden="refreshing">{{ label }} · automatic updates</span>
    </span>
  </p>
</template>
