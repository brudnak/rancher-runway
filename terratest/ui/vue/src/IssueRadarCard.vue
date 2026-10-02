<script setup>
import AddIssueToTracker from './AddIssueToTracker.vue';
import RadarIcon from './HelmLabIcon.vue';
import IssueReadiness from './IssueReadiness.vue';
import { issueEffort } from './issue-radar-prompt.mjs';
defineProps({ issue: { type: Object, required: true } });
defineEmits(['open', 'copy', 'package']);
</script>

<template>
  <article class="ir-issue">
    <div class="ir-card-actions"><AddIssueToTracker :issue="issue"/><button type="button" class="ir-package-action" @click="$emit('package', issue)"><RadarIcon name="folder"/> Create issue package</button><IssueReadiness :issue-url="issue.url" compact/></div>
    <div class="ir-row ir-issue-meta"><span class="font-mono">#{{ issue.number }}</span><span class="ir-kind" :data-kind="issue.kind">{{ issue.kind }}</span><button type="button" class="ir-copy-link" :aria-label="`Copy link to issue ${issue.number}`" title="Copy issue link" @click="$emit('copy', issue.url)"><RadarIcon name="copy"/></button></div>
    <a :href="issue.url" class="ir-issue-title" @click.prevent="$emit('open', issue.url)">{{ issue.title }} <span aria-hidden="true">↗</span></a>
    <div class="ir-tags"><span class="ir-tag ir-size-tag" :data-size="issue.qaSize" :class="{ 'ir-tag-warning': issue.qaSize === 'Lacks QA Size' || issue.sizeConflict }">{{ issue.sizeConflict ? (issue.qaNone ? 'QA/None · conflicting size' : 'Conflicting QA sizes') : issue.qaSize }}</span><span class="ir-tag" :class="{ 'ir-tag-warning': !issue.milestone }">{{ issue.milestone || 'No milestone' }}</span></div>
    <p class="ir-assignees"><span v-if="!issue.assignees.length" class="ir-warning-text">Unassigned</span><template v-else><span v-for="user in issue.assignees" :key="user" :class="{ 'ir-team-owner': issue.matchedUsers.includes(user) }">@{{ user }}</span></template></p>
    <p v-if="!issue.qaNone && issue.assignees.length && !issue.matchedUsers.length" class="ir-caption ir-warning-text">Assigned outside the selected team</p>
    <details v-if="issue.body || issue.labels.length" class="ir-issue-context"><summary><span>Issue context</span><span v-if="!issue.qaNone">{{ issueEffort(issue).points }} pts{{ issueEffort(issue).provisional ? ' · provisional' : '' }}</span></summary><p v-if="issue.body" class="ir-excerpt">{{ issue.body }}</p><p v-else class="ir-caption">No description excerpt. Open the issue for full context.</p><div class="ir-tags"><span v-for="label in issue.labels" :key="label" class="ir-tag">{{ label }}</span></div><p class="ir-caption">Excerpts may be shortened. Open the issue to verify details.</p></details>
  </article>
</template>

<style scoped>
.ir-card-actions{display:flex;align-items:center;justify-content:flex-end;gap:7px;flex-wrap:wrap}.ir-package-action{display:flex;align-items:center;gap:.45rem;margin:0 0 .8rem 0;padding:.4rem .55rem;border:1px solid var(--runway-border);border-radius:.5rem;background:var(--runway-card);color:var(--runway-accent);font-size:.75rem;font-weight:650;cursor:pointer}.ir-package-action:hover{background:var(--runway-raised)}.ir-package-action:focus-visible{outline:2px solid var(--runway-accent);outline-offset:3px}.ir-package-action svg{width:1rem;height:1rem}
</style>
