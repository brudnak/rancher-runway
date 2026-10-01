<script setup>
import Icon from './HelmLabIcon.vue';
import {openIssueReadiness} from './issue-readiness-store.mjs';
defineProps({issueUrl:{type:String,default:''},compact:Boolean});
</script>
<template><div class="readiness" :class="{compact}"><button type="button" class="readiness-trigger" :disabled="!issueUrl" @click.stop="openIssueReadiness(issueUrl)"><Icon name="pulse"/>Is this issue ready to test?</button></div></template>
<style scoped>.readiness-trigger{display:inline-flex;align-items:center;gap:7px;padding:7px 10px;border:1px solid var(--runway-border,#dfe7e4);border-radius:8px;background:var(--runway-card,#fff);color:var(--runway-accent,#16735e);font-size:11px;font-weight:650;cursor:pointer}.readiness-trigger svg{width:15px;height:15px}.compact .readiness-trigger{font-size:10px;padding:6px 8px}.readiness-trigger:focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}</style>
