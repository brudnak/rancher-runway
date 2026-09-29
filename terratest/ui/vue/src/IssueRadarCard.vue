<script setup>
import RadarIcon from './HelmLabIcon.vue';
import { issueEffort } from './issue-radar-prompt.mjs';
defineProps({ issue: { type: Object, required: true } });
defineEmits(['open', 'copy']);
</script>

<template>
  <article class="ir-issue">
    <div class="ir-row ir-issue-meta"><span class="font-mono">#{{ issue.number }}</span><span class="ir-kind" :data-kind="issue.kind">{{ issue.kind }}</span><button type="button" class="ir-copy-link" :aria-label="`Copy link to issue ${issue.number}`" title="Copy issue link" @click="$emit('copy', issue.url)"><RadarIcon name="copy"/></button></div>
    <a :href="issue.url" class="ir-issue-title" @click.prevent="$emit('open', issue.url)">{{ issue.title }} <span aria-hidden="true">↗</span></a>
    <div class="ir-tags"><span class="ir-tag ir-size-tag" :data-size="issue.qaSize" :class="{ 'ir-tag-warning': issue.qaSize === 'Lacks QA Size' || issue.sizeConflict }">{{ issue.sizeConflict ? (issue.qaNone ? 'QA/None · conflicting size' : 'Conflicting QA sizes') : issue.qaSize }}</span><span class="ir-tag" :class="{ 'ir-tag-warning': !issue.milestone }">{{ issue.milestone || 'No milestone' }}</span></div>
    <p class="ir-assignees"><span v-if="!issue.assignees.length" class="ir-warning-text">Unassigned</span><template v-else><span v-for="user in issue.assignees" :key="user" :class="{ 'ir-team-owner': issue.matchedUsers.includes(user) }">@{{ user }}</span></template></p>
    <p v-if="!issue.qaNone && issue.assignees.length && !issue.matchedUsers.length" class="ir-caption ir-warning-text">Assigned outside the selected team</p>
    <details v-if="issue.body || issue.labels.length" class="ir-issue-context"><summary><span>Issue context</span><span v-if="!issue.qaNone">{{ issueEffort(issue).points }} pts{{ issueEffort(issue).provisional ? ' · provisional' : '' }}</span></summary><p v-if="issue.body" class="ir-excerpt">{{ issue.body }}</p><p v-else class="ir-caption">No description excerpt. Open the issue for full context.</p><div class="ir-tags"><span v-for="label in issue.labels" :key="label" class="ir-tag">{{ label }}</span></div><p class="ir-caption">Excerpts may be shortened. Open the issue to verify details.</p></details>
  </article>
</template>
