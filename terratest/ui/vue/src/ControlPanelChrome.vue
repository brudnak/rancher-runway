<template>
  <div v-if="(refreshError || bootPending) && activeTab !== 'home'" class="runway-alerts">
      <div v-if="refreshError && activeTab !== 'home'" role="alert" :class="{ 'panel-refresh-alert': !bootPending }" class="mt-4 max-w-4xl rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-900 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200">
        <div class="font-semibold">{{ bootPending ? 'Startup check could not finish' : 'Status could not refresh' }}</div>
        <p class="mt-1">{{ refreshError }}</p>
        <p class="mt-1">{{ bootPending ? 'Setup stays locked until a fresh safety check succeeds.' : 'Showing the last successful status. It may be out of date.' }}</p>
        <AppBuildStamp />
        <button type="button" class="chrome-button mt-3" :disabled="refreshInFlight" @click="refreshWorkspace">{{ refreshInFlight ? 'Retrying…' : 'Retry checks' }}</button>
      </div>
      <div v-else-if="bootPending && activeTab !== 'home'" class="mt-4 max-w-4xl rounded-xl border border-sky-200 bg-white px-4 py-3 text-sm text-sky-900 shadow-sm dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-100">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start">
          <span class="spinner mt-0.5 shrink-0 text-sky-600 dark:text-sky-300"></span>
          <div class="min-w-0">
            <div class="font-semibold">Startup safety check running</div>
            <div class="mt-1 leading-6 text-sky-800/80 dark:text-sky-100/75">
              {{ bootDetail }}
            </div>
          </div>
        </div>
      </div>

  </div>
  <IssueReadinessDialog />
</template>
<script setup>
import {onMounted,onBeforeUnmount} from 'vue';
import IssueReadinessDialog from './IssueReadinessDialog.vue';
import AppBuildStamp from './AppBuildStamp.vue';
import {startDailyReadiness,stopDailyReadiness} from './daily-readiness-store.mjs';
import {startMyWorkRefresh,stopMyWorkRefresh,refreshMyWork} from './my-work-store.mjs';
import {activeTab,bootPending,bootDetail,refreshChecks,refreshError,refreshInFlight} from './store.js';
onMounted(startMyWorkRefresh);
onBeforeUnmount(stopMyWorkRefresh);
onMounted(startDailyReadiness);
onBeforeUnmount(stopDailyReadiness);
function refreshWorkspace(){refreshChecks();void refreshMyWork({force:true});}
</script>
<style scoped>.runway-alerts{margin-bottom:16px}.runway-alerts>div{margin-top:0}</style>
