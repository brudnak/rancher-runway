<script setup>
defineProps({ name: String });
const paths = {
  terminal: 'm5 7 5 5-5 5M13 17h6',
  copy: 'M9 9h11v11H9zM5 15H3V3h12v2',
  refresh: 'M20 7v5h-5M4 17v-5h5M6.1 7a7 7 0 0 1 11.6-2L20 8M4 16l2.3 3A7 7 0 0 0 18 17',
  search: 'm21 21-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0',
  chevron: 'm8 10 4 4 4-4',
  check: 'm5 12 4 4L19 6',
  download: 'M12 3v12m-4-4 4 4 4-4M4 16v5h16v-5',
  layers: 'm12 3 10 5-10 5L2 8l10-5M2 12l10 5 10-5M2 16l10 5 10-5',
  arrow: 'M4 12h16m-6-6 6 6-6 6',
};
</script>
<template><svg class="hl-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="paths[name] || paths.terminal" /></svg></template>
