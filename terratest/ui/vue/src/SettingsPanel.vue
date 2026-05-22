<template>
  <div class="mx-auto max-w-4xl">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Settings</h2>
        <p class="mt-2 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">Local panel preferences for this browser session.</p>
      </div>
      <div :class="statusClass">{{ disabled ? "GPU reminders off" : "GPU reminders on" }}</div>
    </div>

    <div class="mt-5 rounded-xl border border-rose-200 bg-rose-50 p-4 dark:border-rose-500/25 dark:bg-rose-500/10">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div class="min-w-0">
          <h3 class="text-base font-semibold text-rose-950 dark:text-rose-100">GPU infrastructure reminders</h3>
          <p class="mt-2 text-sm leading-6 text-rose-800/90 dark:text-rose-100/80">{{ detail }}</p>
        </div>
        <div class="flex shrink-0 flex-wrap gap-2">
          <button
            v-for="interval in intervals"
            :key="interval.minutes"
            type="button"
            :data-gpu-reminder-interval="interval.minutes"
            :disabled="disabled"
            class="gpu-reminder-interval rounded-lg px-3.5 py-2 text-sm font-semibold shadow-sm"
            :class="intervalClass(interval.minutes)"
          >
            {{ interval.label }}
          </button>
        </div>
      </div>
      <div class="mt-4 flex flex-wrap justify-end gap-3">
        <button
          id="gpuReminderEnableBtn"
          type="button"
          :hidden="!disabled"
          class="rounded-lg bg-emerald-500 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400"
        >
          Enable reminders
        </button>
        <button
          id="gpuReminderDisableBtn"
          type="button"
          :hidden="disabled"
          class="rounded-lg border border-rose-200 bg-white px-4 py-2.5 text-sm font-semibold text-rose-700 shadow-sm hover:bg-rose-50 dark:border-rose-500/25 dark:bg-white/[0.06] dark:text-rose-300 dark:hover:bg-rose-500/10"
        >
          Disable reminders
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";

const settings = ref(window.rancherGpuReminderSettings || { intervalMinutes: 15, disabled: false });
const intervals = [
  { minutes: 15, label: "15 min" },
  { minutes: 30, label: "30 min" },
  { minutes: 60, label: "1 hr" },
];

const disabled = computed(() => Boolean(settings.value.disabled));
const intervalMinutes = computed(() => Number(settings.value.intervalMinutes || 15));
const intervalLabel = computed(() => intervalMinutes.value === 60 ? "1 hour" : `${intervalMinutes.value} minutes`);

const statusClass = computed(() => disabled.value
  ? "inline-flex items-center justify-center rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold text-rose-700 dark:bg-rose-500/15 dark:text-rose-300"
  : "inline-flex items-center justify-center rounded-full bg-emerald-100 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300");

const detail = computed(() => disabled.value
  ? `Reminders disabled. Last interval: ${intervalLabel.value}. Close-time GPU warnings still remain active.`
  : `Reminder interval: ${intervalLabel.value}.`);

const intervalClass = minutes => (
  minutes === intervalMinutes.value && !disabled.value
    ? "bg-rose-500 text-white shadow-rose-500/20 hover:bg-rose-400"
    : "border border-rose-200 bg-white text-rose-700 hover:bg-rose-50 dark:border-rose-500/25 dark:bg-white/[0.06] dark:text-rose-200 dark:hover:bg-rose-500/10"
);

const handleSettingsEvent = event => {
  settings.value = event.detail?.settings || settings.value;
};

onMounted(() => {
  window.addEventListener("rancher-control-panel:gpu-reminders", handleSettingsEvent);
});

onUnmounted(() => {
  window.removeEventListener("rancher-control-panel:gpu-reminders", handleSettingsEvent);
});
</script>
