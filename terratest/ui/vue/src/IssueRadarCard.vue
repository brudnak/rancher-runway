<script setup>
defineProps({ issue: { type: Object, required: true } });
defineEmits(['open']);
</script>

<template>
  <article class="ir-issue">
    <div class="ir-row ir-issue-meta"><span class="font-mono">#{{ issue.number }}</span><span>{{ issue.kind }}</span></div>
    <a :href="issue.url" class="ir-issue-title" @click.prevent="$emit('open', issue.url)">{{ issue.title }} <span aria-hidden="true">↗</span></a>
    <div class="ir-tags"><span class="ir-tag" :class="{ 'ir-tag-warning': issue.qaSize === 'Lacks QA Size' }">{{ issue.qaSize }}</span><span class="ir-tag" :class="{ 'ir-tag-warning': !issue.milestone }">{{ issue.milestone || 'No milestone' }}</span></div>
    <p class="ir-assignees"><span v-if="!issue.assignees.length" class="ir-warning-text">Unassigned</span><template v-else><span v-for="user in issue.assignees" :key="user" :class="{ 'ir-team-owner': issue.matchedUsers.includes(user) }">@{{ user }}</span></template></p>
    <p v-if="!issue.qaNone && issue.assignees.length && !issue.matchedUsers.length" class="ir-caption ir-warning-text">Assigned outside the selected team</p>
  </article>
</template>
