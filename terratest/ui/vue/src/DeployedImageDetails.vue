<template>
  <section class="mt-4 min-w-0 overflow-hidden rounded-2xl border border-emerald-200 bg-emerald-50/50 dark:border-emerald-500/20 dark:bg-emerald-500/[0.055]">
    <div class="flex flex-col gap-3 border-b border-emerald-200/80 px-4 py-4 dark:border-emerald-500/15 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <h3 class="text-sm font-bold text-zinc-950 dark:text-zinc-100">Deployment versions</h3>
          <span class="rounded-full border border-emerald-200 bg-white px-2 py-0.5 text-[10px] font-bold text-emerald-700 dark:border-emerald-500/25 dark:bg-white/[0.06] dark:text-emerald-300">
            {{ deployedImages.length }} runtime image{{ deployedImages.length === 1 ? "" : "s" }}
          </span>
          <span
            v-if="hasRuntimeDrift"
            class="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[10px] font-bold text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300"
          >Rolling versions observed</span>
        </div>
        <p class="mt-1 max-w-3xl text-xs leading-5 text-zinc-600 dark:text-zinc-400">
          Versions below come from deployed container image references. Registry metadata is inspected only when you open image details.
        </p>
      </div>
      <div class="flex shrink-0 flex-col gap-2 sm:items-end">
        <div v-if="optionalDetailSummary.length" class="flex flex-wrap gap-2 sm:justify-end">
          <span
            v-for="item in optionalDetailSummary"
            :key="item.label"
            class="rounded-full border border-zinc-200 bg-white px-2.5 py-1 text-[10px] font-bold text-zinc-600 dark:border-white/10 dark:bg-white/[0.05] dark:text-zinc-300"
            :title="item.title"
          >{{ item.label }} {{ item.value }}</span>
        </div>
        <div class="flex flex-wrap items-center gap-2 sm:justify-end">
          <span v-if="liveDetailsCollectedAt" class="text-[10px] font-semibold text-zinc-500 dark:text-zinc-400">Collected {{ formatDate(liveDetailsCollectedAt) }}</span>
          <button
            type="button"
            :disabled="liveDetailsLoading"
            class="inline-flex min-h-9 items-center justify-center gap-2 rounded-lg border border-zinc-200 bg-white px-3 text-xs font-bold text-zinc-700 hover:bg-zinc-50 disabled:opacity-55 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]"
            @click="loadLiveDetails"
          >
            <span v-if="liveDetailsLoading" class="spinner !h-3.5 !w-3.5 !border-[1.5px]"></span>
            {{ liveDetailsLoading ? "Collecting…" : "Refresh live details" }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="liveDetailsError" class="border-b border-rose-200 bg-rose-50 px-4 py-2.5 text-xs leading-5 text-rose-800 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-200">
      <strong>Live details unavailable:</strong> {{ liveDetailsError }}
    </div>
    <div v-if="liveDetailsWarnings.length" class="border-b border-amber-200 bg-amber-50 px-4 py-2.5 text-xs leading-5 text-amber-900 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-100">
      <strong>Collection warnings:</strong> {{ liveDetailsWarnings.join(" · ") }}
    </div>

    <div v-if="deployedImages.length" class="grid min-w-0 gap-3 p-4 lg:grid-cols-2">
      <article
        v-for="image in deployedImages"
        :key="image.key"
        class="min-w-0 rounded-xl border border-zinc-200 bg-white p-4 shadow-sm dark:border-white/10 dark:bg-zinc-950/55"
      >
        <div class="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <span class="rounded-md px-2 py-1 text-[10px] font-bold uppercase tracking-wide" :class="roleClass(image.role)">
                {{ roleLabel(image.role) }}
              </span>
              <span class="text-[11px] font-semibold text-zinc-500 dark:text-zinc-400">
                {{ image.readyCount }}/{{ image.observations }} ready
              </span>
            </div>
            <div class="mt-2 break-all font-mono text-base font-bold text-zinc-950 dark:text-zinc-100" :title="image.declaredTag">
              {{ image.declaredTag || "Tag unavailable" }}
            </div>
            <div class="mt-1 text-[10px] font-bold uppercase tracking-wide text-zinc-500 dark:text-zinc-500">
              {{ roleTagLabel(image.role) }}
            </div>
          </div>
          <button
            type="button"
            :disabled="!image.inspectReference"
            class="inline-flex min-h-10 shrink-0 items-center justify-center gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-3.5 text-xs font-bold text-emerald-800 hover:bg-emerald-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200 dark:hover:bg-emerald-500/15"
            @click="openImageDetails(image)"
          >
            Image details
          </button>
        </div>

        <dl class="mt-3 grid gap-2 text-xs">
          <div class="min-w-0 rounded-lg bg-zinc-50 px-3 py-2 dark:bg-white/[0.035]">
            <dt class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">Declared reference</dt>
            <dd class="mt-1 break-all font-mono text-zinc-800 dark:text-zinc-200">{{ image.declaredReference || "Unavailable" }}</dd>
          </div>
          <div class="min-w-0 rounded-lg bg-zinc-50 px-3 py-2 dark:bg-white/[0.035]">
            <dt class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">Runtime digest</dt>
            <dd class="mt-1 break-all font-mono text-zinc-800 dark:text-zinc-200" :title="image.runtimeDigest || image.runtimeImageId">
              {{ shortDigest(image.runtimeDigest) || "Unavailable" }}
            </dd>
          </div>
        </dl>

        <div class="mt-3 flex flex-wrap items-center gap-2 text-[11px] text-zinc-500 dark:text-zinc-400">
          <span>{{ image.pods.length }} pod{{ image.pods.length === 1 ? "" : "s" }}</span>
          <span aria-hidden="true">•</span>
          <span>{{ image.containers.length }} container name{{ image.containers.length === 1 ? "" : "s" }}</span>
          <span v-if="image.runtimeDigest" class="font-semibold text-emerald-700 dark:text-emerald-300">Digest-pinned inspection available</span>
          <span v-else class="font-semibold text-amber-700 dark:text-amber-300">Declared-tag inspection only</span>
        </div>
      </article>
    </div>

    <div v-else class="px-4 py-5 text-sm leading-6 text-zinc-600 dark:text-zinc-400">
      No Rancher server, webhook, or agent runtime images were reported for this cluster. Runtime image details appear after reachable pods expose container image status.
    </div>
  </section>

  <Teleport to="body">
    <div
      v-if="modalOpen"
      class="fixed inset-0 z-[80] flex bg-zinc-950/70 p-3 backdrop-blur-sm sm:p-5"
      role="dialog"
      aria-modal="true"
      aria-labelledby="deployedImageDetailsTitle"
      @click.self="closeImageDetails"
      @keydown="handleDialogKeydown"
    >
      <section ref="dialogPanel" tabindex="-1" class="mx-auto flex h-full w-full max-w-[1500px] flex-col overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-2xl shadow-zinc-950/30 outline-none dark:border-white/10 dark:bg-zinc-950 dark:shadow-black/60">
        <header class="border-b border-zinc-200 bg-white px-4 py-4 dark:border-white/10 dark:bg-zinc-900 sm:px-5">
          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-[11px] font-bold uppercase tracking-wider text-emerald-700 dark:text-emerald-300">Deployed image details</span>
                <span v-if="activeImage" class="rounded-md px-2 py-0.5 text-[10px] font-bold uppercase tracking-wide" :class="roleClass(activeImage.role)">
                  {{ roleLabel(activeImage.role) }}
                </span>
              </div>
              <h2 id="deployedImageDetailsTitle" class="mt-1 break-all font-mono text-base font-bold text-zinc-950 dark:text-zinc-50 sm:text-lg">
                {{ activeImage?.declaredReference || "Container image" }}
              </h2>
              <p class="mt-1 text-xs leading-5 text-zinc-500 dark:text-zinc-400">
                {{ cluster.name }} · {{ activeImage?.pods?.length || 0 }} pod{{ activeImage?.pods?.length === 1 ? "" : "s" }} · {{ activeImage?.readyCount || 0 }}/{{ activeImage?.observations || 0 }} ready
              </p>
            </div>
            <button
              ref="modalCloseButton"
              type="button"
              class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-zinc-200 bg-white text-zinc-500 hover:bg-zinc-50 hover:text-zinc-900 dark:border-white/10 dark:bg-white/[0.05] dark:text-zinc-400 dark:hover:bg-white/[0.09] dark:hover:text-white"
              aria-label="Close deployed image details"
              title="Close deployed image details"
              @click="closeImageDetails"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.25" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" d="m6 6 12 12M18 6 6 18" />
              </svg>
            </button>
          </div>

          <div class="mt-4 flex flex-col gap-2 lg:flex-row lg:items-end">
            <label class="grid min-w-0 flex-1 gap-1.5 text-xs font-bold text-zinc-600 dark:text-zinc-300">
              <span>Inspect platform</span>
              <input
                v-model.trim="inspectPlatform"
                list="deployed-image-platforms"
                type="text"
                autocomplete="off"
                spellcheck="false"
                :disabled="inspectLoading"
                class="h-10 rounded-xl border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 dark:border-white/10 dark:bg-zinc-950 dark:text-white"
              />
              <datalist id="deployed-image-platforms">
                <option v-for="option in inspectPlatformOptions" :key="option" :value="option"></option>
              </datalist>
            </label>
            <button
              v-if="usingDeclaredFallback && activeImage?.exactReference !== activeImage?.declaredReference"
              type="button"
              :disabled="inspectLoading"
              class="inline-flex h-10 items-center justify-center rounded-xl border border-sky-200 bg-sky-50 px-3.5 text-xs font-bold text-sky-800 hover:bg-sky-100 disabled:opacity-55 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-200"
              @click="useExactRuntimeReference"
            >Use runtime digest</button>
            <button
              type="button"
              :disabled="inspectLoading || !inspectionTarget"
              class="inline-flex h-10 items-center justify-center gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-3.5 text-xs font-bold text-emerald-800 hover:bg-emerald-100 disabled:opacity-55 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200"
              @click="loadInspection(true)"
            >
              <span v-if="inspectLoading" class="spinner !h-3.5 !w-3.5 !border-[1.5px]"></span>
              Refresh details
            </button>
          </div>

          <div
            class="mt-3 rounded-xl border px-3.5 py-2.5 text-xs leading-5"
            :class="usingDeclaredFallback || !activeImage?.runtimeDigest
              ? 'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-100'
              : 'border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-100'"
          >
            <strong>{{ inspectionTargetLabel }}:</strong>
            <code class="ml-1 break-all">{{ inspectionTarget || "Unavailable" }}</code>
            <span v-if="usingDeclaredFallback" class="ml-1">This fallback reads the tag as it exists now and does not prove which artifact is running.</span>
          </div>
        </header>

        <div class="min-h-0 flex-1 overflow-y-auto bg-zinc-50/70 p-4 dark:bg-black/15 sm:p-5">
          <div v-if="inspectLoading" class="py-16 text-center" role="status">
            <span class="spinner text-emerald-500"></span>
            <div class="mt-3 text-sm font-semibold text-zinc-600 dark:text-zinc-300">Reading the deployed image manifest and configuration…</div>
          </div>

          <div v-else-if="inspectError" role="alert" class="grid gap-3">
            <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200">
              <div class="font-bold">Image inspection failed</div>
              <div class="mt-1 whitespace-pre-wrap break-words">{{ inspectError }}</div>
            </div>
            <div class="rounded-xl border border-zinc-200 bg-white px-4 py-3 text-xs leading-5 text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300">
              Registry inspection uses credentials available on this machine. A private image can be running successfully in the cluster while remaining unavailable to this local registry client.
            </div>
            <button
              v-if="declaredFallbackAvailable"
              type="button"
              class="inline-flex min-h-10 w-fit items-center justify-center rounded-xl border border-amber-200 bg-amber-50 px-4 text-xs font-bold text-amber-900 hover:bg-amber-100 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-100"
              @click="inspectDeclaredFallback"
            >Inspect declared tag instead</button>
          </div>

          <div v-else-if="inspection" class="grid gap-4">
            <section class="rounded-2xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-zinc-900/70">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h3 class="text-sm font-bold text-zinc-950 dark:text-zinc-100">Overview</h3>
                  <p class="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">Runtime evidence alongside registry metadata for the selected image.</p>
                </div>
                <span class="rounded-full border px-2.5 py-1 text-[10px] font-bold" :class="digestMatchClass">{{ digestMatchLabel }}</span>
              </div>
              <dl class="mt-3 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                <div
                  v-for="item in overviewItems"
                  :key="item.label"
                  class="min-w-0 rounded-xl px-3 py-2.5"
                  :class="item.accent
                    ? 'border border-emerald-200 bg-emerald-50 dark:border-emerald-500/25 dark:bg-emerald-500/10'
                    : 'bg-zinc-50 dark:bg-white/[0.035]'"
                >
                  <dt class="text-[10px] font-bold uppercase tracking-wide" :class="item.accent ? 'text-emerald-700 dark:text-emerald-300' : 'text-zinc-500 dark:text-zinc-500'">{{ item.label }}</dt>
                  <dd class="mt-1 break-all text-xs font-semibold text-zinc-800 dark:text-zinc-200" :class="item.mono ? 'font-mono' : ''" :title="item.title || item.value">{{ item.value || "Unavailable" }}</dd>
                </div>
              </dl>
            </section>

            <section v-if="platforms.length" class="rounded-2xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-zinc-900/70">
              <h3 class="text-sm font-bold text-zinc-950 dark:text-zinc-100">Platforms <span class="ml-1 text-xs font-semibold text-zinc-400">{{ platforms.length }}</span></h3>
              <div class="mt-3 flex flex-wrap gap-2">
                <button
                  v-for="(platform, index) in platforms"
                  :key="`${platformLabel(platform)}-${index}`"
                  type="button"
                  :disabled="inspectLoading || !platformLabel(platform)"
                  class="rounded-lg border border-sky-200 bg-sky-50 px-2.5 py-1.5 font-mono text-xs font-semibold text-sky-800 hover:bg-sky-100 disabled:cursor-default dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-200"
                  @click="selectPlatform(platform)"
                >{{ platformLabel(platform) }}</button>
              </div>
            </section>

            <section v-if="warnings.length" class="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-amber-900 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-100">
              <h3 class="text-sm font-bold">Warnings</h3>
              <ul class="mt-2 list-disc space-y-1 pl-5 text-xs leading-5">
                <li v-for="(warning, index) in warnings" :key="`${warning}-${index}`">{{ warning }}</li>
              </ul>
            </section>

            <section v-if="hasConfiguration" class="rounded-2xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-zinc-900/70">
              <h3 class="text-sm font-bold text-zinc-950 dark:text-zinc-100">Configuration</h3>
              <dl v-if="configurationSummary.length" class="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                <div v-for="item in configurationSummary" :key="item.label" class="rounded-lg bg-zinc-50 px-3 py-2 dark:bg-white/[0.035]">
                  <dt class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">{{ item.label }}</dt>
                  <dd class="mt-1 break-all font-mono text-xs text-zinc-800 dark:text-zinc-200">{{ item.value }}</dd>
                </div>
              </dl>

              <div v-if="configEntrypoint || configCommand" class="mt-3 grid gap-3 lg:grid-cols-2">
                <div v-if="configEntrypoint">
                  <div class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">Entrypoint</div>
                  <code class="mt-1 block break-all rounded-lg bg-zinc-950 px-3 py-2 text-xs leading-5 text-emerald-300">{{ configEntrypoint }}</code>
                </div>
                <div v-if="configCommand">
                  <div class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">Command</div>
                  <code class="mt-1 block break-all rounded-lg bg-zinc-950 px-3 py-2 text-xs leading-5 text-sky-300">{{ configCommand }}</code>
                </div>
              </div>

              <details v-if="configLabels.length" class="mt-3 rounded-xl border border-zinc-200 bg-zinc-50 open:bg-white dark:border-white/10 dark:bg-white/[0.025] dark:open:bg-white/[0.04]">
                <summary class="cursor-pointer px-3 py-2.5 text-xs font-bold text-zinc-700 dark:text-zinc-300">Labels ({{ configLabels.length }})</summary>
                <dl class="max-h-80 overflow-y-auto border-t border-zinc-200 px-3 py-2 dark:border-white/10">
                  <div v-for="item in configLabels" :key="item.key" class="grid gap-1 border-b border-zinc-200/60 py-2 last:border-0 dark:border-white/5">
                    <dt class="break-all font-mono text-[11px] font-bold text-zinc-600 dark:text-zinc-400">{{ item.key }}</dt>
                    <dd class="break-all text-xs leading-5 text-zinc-800 dark:text-zinc-200">{{ item.value }}</dd>
                  </div>
                </dl>
              </details>

              <details v-if="configEnvironment.length" class="mt-3 rounded-xl border border-zinc-200 bg-zinc-50 open:bg-white dark:border-white/10 dark:bg-white/[0.025] dark:open:bg-white/[0.04]">
                <summary class="cursor-pointer px-3 py-2.5 text-xs font-bold text-zinc-700 dark:text-zinc-300">Environment ({{ configEnvironment.length }})</summary>
                <div class="max-h-80 overflow-y-auto border-t border-zinc-200 px-3 py-2 dark:border-white/10">
                  <code v-for="(item, index) in configEnvironment" :key="`${item}-${index}`" class="block break-all py-1 text-[11px] leading-5 text-zinc-700 dark:text-zinc-300">{{ item }}</code>
                </div>
              </details>
            </section>

            <details v-if="configHistory.length" class="group overflow-hidden rounded-2xl border border-zinc-200 bg-white dark:border-white/10 dark:bg-zinc-900/70">
              <summary class="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3.5 marker:hidden hover:bg-zinc-50 dark:hover:bg-white/[0.025] [&::-webkit-details-marker]:hidden">
                <div>
                  <h3 class="text-sm font-bold text-zinc-950 dark:text-zinc-100">Build history <span class="ml-1 text-xs font-semibold text-zinc-400">{{ configHistoryTotal }}</span></h3>
                  <p class="mt-1 text-[11px] text-zinc-500 dark:text-zinc-400">Commands recorded in the selected image configuration</p>
                </div>
                <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-zinc-400 transition-transform group-open:rotate-180" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m6 9 6 6 6-6" /></svg>
              </summary>
              <ol class="max-h-[32rem] overflow-y-auto border-t border-zinc-200 px-4 py-2 dark:border-white/10">
                <li v-for="item in configHistory" :key="`${item.step}-${item.created}-${item.createdBy}`" class="grid min-w-0 grid-cols-[2rem_minmax(0,1fr)] gap-3 border-b border-zinc-200/70 py-3 last:border-0 dark:border-white/[0.07]">
                  <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-zinc-100 text-[11px] font-bold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">{{ item.step }}</span>
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2 text-[10px] font-bold uppercase tracking-wide text-zinc-500">
                      <time v-if="item.created" :datetime="item.created">{{ formatDate(item.created) }}</time>
                      <span v-if="item.emptyLayer" class="rounded-full border border-violet-200 bg-violet-50 px-2 py-0.5 text-violet-700 dark:border-violet-500/25 dark:bg-violet-500/10 dark:text-violet-300">Empty layer</span>
                    </div>
                    <code v-if="item.createdBy" class="mt-1.5 block whitespace-pre-wrap break-all rounded-lg bg-zinc-950 px-3 py-2 font-mono text-[11px] leading-5 text-emerald-300">{{ item.createdBy }}</code>
                    <p v-if="item.comment" class="mt-2 whitespace-pre-wrap break-words text-xs leading-5 text-zinc-600 dark:text-zinc-300"><strong>Comment:</strong> {{ item.comment }}</p>
                  </div>
                </li>
              </ol>
            </details>

            <section v-if="layers.length" class="rounded-2xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-zinc-900/70">
              <div class="flex items-center justify-between gap-3">
                <h3 class="text-sm font-bold text-zinc-950 dark:text-zinc-100">Layers <span class="ml-1 text-xs font-semibold text-zinc-400">{{ layers.length }}</span></h3>
                <span class="text-xs font-bold text-zinc-500">{{ layerTotalSize }}</span>
              </div>
              <div class="mt-3 grid gap-2 lg:grid-cols-2">
                <div v-for="(layer, index) in layers" :key="layer.digest || index" class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 rounded-lg bg-zinc-50 px-3 py-2 dark:bg-white/[0.035]">
                  <span class="flex h-6 w-6 items-center justify-center rounded-md bg-zinc-200 text-[10px] font-bold text-zinc-600 dark:bg-white/[0.08] dark:text-zinc-300">{{ index + 1 }}</span>
                  <code class="truncate text-[11px] text-zinc-600 dark:text-zinc-300" :title="layer.digest || layer.mediaType || ''">{{ shortDigest(layer.digest) || layer.mediaType || "layer" }}</code>
                  <span class="text-[11px] font-bold text-zinc-500">{{ formatBytes(layer.size) }}</span>
                </div>
              </div>
            </section>

            <section class="overflow-hidden rounded-2xl border border-zinc-200 bg-white dark:border-white/10 dark:bg-zinc-900/70">
              <div class="flex flex-wrap items-center justify-between gap-3 border-b border-zinc-200 px-4 py-3 dark:border-white/10">
                <div>
                  <div class="flex flex-wrap items-center gap-2">
                    <h3 class="text-sm font-bold text-zinc-950 dark:text-zinc-100">build.yaml</h3>
                    <span class="rounded-full border px-2 py-0.5 text-[10px] font-bold" :class="buildYamlStatusClass">{{ buildYamlStatus }}</span>
                  </div>
                  <div v-if="buildYaml.path" class="mt-1 break-all font-mono text-[10px] text-zinc-500">{{ buildYaml.path }}</div>
                </div>
                <span class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">Embedded image scan</span>
              </div>
              <div v-if="buildYaml.found" class="grid gap-4 p-4 xl:grid-cols-2">
                <div>
                  <h4 class="text-xs font-bold uppercase tracking-wide text-zinc-500">Metadata</h4>
                  <dl v-if="structuredBuildEntries.length" class="mt-2 max-h-[28rem] overflow-y-auto rounded-xl border border-zinc-200 bg-zinc-50 px-3 dark:border-white/10 dark:bg-white/[0.025]">
                    <div v-for="item in structuredBuildEntries" :key="item.path" class="grid gap-1 border-b border-zinc-200/60 py-2 last:border-0 dark:border-white/5">
                      <dt class="break-all font-mono text-[11px] font-bold text-zinc-600 dark:text-zinc-400">{{ item.path }}</dt>
                      <dd class="break-all text-xs text-zinc-800 dark:text-zinc-200">{{ structuredValue(item.value) }}</dd>
                    </div>
                  </dl>
                  <p v-else class="mt-2 text-xs leading-5 text-zinc-500 dark:text-zinc-400">No structured metadata was returned.</p>
                </div>
                <div>
                  <h4 class="text-xs font-bold uppercase tracking-wide text-zinc-500">Raw</h4>
                  <pre class="mt-2 max-h-[28rem] overflow-auto whitespace-pre-wrap break-words rounded-xl bg-zinc-950 p-4 font-mono text-[11px] leading-5 text-zinc-200">{{ rawBuildYaml || "No raw build.yaml content returned." }}</pre>
                </div>
              </div>
              <div v-else class="px-4 py-4 text-xs leading-5 text-zinc-500 dark:text-zinc-400">
                {{ buildYaml.reason || buildYaml.error || "No build.yaml was found in the eligible image layers that were safely scanned." }}
              </div>
            </section>

            <div class="rounded-xl border border-zinc-200 bg-white px-4 py-3 text-xs leading-5 text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300">
              Registry metadata is read from this machine and can differ from cluster access. Runtime digest evidence remains displayed above so tag movement and fallback inspection stay explicit.
            </div>
          </div>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { apiFetch } from "./store.js";

const inspectionCache = new Map();
const sha256Pattern = /sha256:[0-9a-f]{64}/i;
const webhookEnvironmentKeys = [
  "CATTLE_RANCHER_WEBHOOK_VERSION",
  "CATTLE_WEBHOOK_VERSION",
  "RANCHER_WEBHOOK_VERSION",
  "WEBHOOK_VERSION",
];

const props = defineProps({
  cluster: {
    type: Object,
    required: true,
  },
  pods: {
    type: Array,
    default: () => [],
  },
  deploymentDetails: {
    type: Object,
    default: null,
  },
});

const stripRuntimeScheme = value => String(value || "")
  .trim()
  .replace(/^(?:docker-pullable|docker|oci|https|containerd|cri-o):\/\//i, "");

const parseImageReference = value => {
  const reference = stripRuntimeScheme(value);
  if (!reference) return { reference: "", repository: "", tag: "", digest: "" };
  const lastSlash = reference.lastIndexOf("/");
  const digestAt = reference.lastIndexOf("@");
  const tagAt = reference.lastIndexOf(":");
  const hasDigest = digestAt > lastSlash;
  const hasTag = !hasDigest && tagAt > lastSlash;
  const selectorAt = hasDigest ? digestAt : hasTag ? tagAt : -1;
  return {
    reference,
    repository: selectorAt >= 0 ? reference.slice(0, selectorAt) : reference,
    tag: hasTag ? reference.slice(tagAt + 1) : "",
    digest: hasDigest ? reference.slice(digestAt + 1) : "",
  };
};

const runtimeDigest = value => String(value || "").match(sha256Pattern)?.[0]?.toLowerCase() || "";

const classifyImage = (declaredReference, containerName) => {
  const parsed = parseImageReference(declaredReference);
  const repositoryName = parsed.repository.split("/").filter(Boolean).pop()?.toLowerCase() || "";
  const normalizedContainer = String(containerName || "").trim().toLowerCase();
  if (repositoryName === "rancher-webhook" || normalizedContainer === "rancher-webhook") return "webhook";
  if (repositoryName === "rancher-agent" || normalizedContainer === "rancher-agent") return "agent";
  if (repositoryName === "rancher" || normalizedContainer === "rancher") return "server";
  return "other";
};

const liveDetails = ref(null);
const liveDetailsLoading = ref(false);
const liveDetailsError = ref("");
let liveDetailsController = null;

const loadLiveDetails = async () => {
  const clusterID = String(props.cluster?.id || "").trim();
  if (!clusterID || liveDetailsLoading.value) return;
  liveDetailsController?.abort();
  const controller = new AbortController();
  liveDetailsController = controller;
  liveDetailsLoading.value = true;
  liveDetailsError.value = "";
  try {
    const response = await apiFetch(`/api/clusters/details?cluster=${encodeURIComponent(clusterID)}`, {
      cache: "no-store",
      signal: controller.signal,
    });
    liveDetails.value = await response.json();
  } catch (error) {
    if (error?.name !== "AbortError") {
      liveDetailsError.value = error instanceof Error ? error.message : "Could not collect live cluster details.";
    }
  } finally {
    if (liveDetailsController === controller) {
      liveDetailsController = null;
      liveDetailsLoading.value = false;
    }
  }
};

const liveDetailsCollectedAt = computed(() => String(liveDetails.value?.collectedAt || ""));
const liveDetailsWarnings = computed(() => Array.isArray(liveDetails.value?.warnings)
  ? liveDetails.value.warnings.map(value => String(value).trim()).filter(Boolean)
  : []);

const normalizedImageRole = (role, declaredReference, containerName) => {
  const normalized = String(role || "").trim().toLowerCase();
  if (normalized === "rancher" || normalized === "server") return "server";
  if (normalized === "webhook" || normalized === "rancher-webhook") return "webhook";
  if (normalized === "agent" || normalized === "rancher-agent") return "agent";
  return classifyImage(declaredReference, containerName);
};

const imageObservations = computed(() => {
  const observations = new Map();
  const addObservation = raw => {
    const declared = parseImageReference(raw.declaredReference);
    const imageId = stripRuntimeScheme(raw.imageId);
    const digest = runtimeDigest(raw.digest) || runtimeDigest(imageId);
    const role = normalizedImageRole(raw.role, declared.reference || raw.inspectReference, raw.container);
    if (role !== "server" && role !== "webhook" && role !== "agent") return false;
    const identity = digest || imageId;
    const key = [
      role,
      String(raw.namespace || ""),
      String(raw.pod || ""),
      String(raw.container || ""),
      declared.reference,
      identity,
    ].join("\u0000");
    const existing = observations.get(key);
    if (existing) {
      existing.ready = existing.ready || raw.ready === true;
      existing.inspectReference ||= stripRuntimeScheme(raw.inspectReference);
      existing.version ||= String(raw.version || "").trim();
      existing.imageId ||= imageId;
      existing.digest ||= digest;
      return true;
    }
    observations.set(key, {
      role,
      namespace: String(raw.namespace || ""),
      pod: String(raw.pod || ""),
      container: String(raw.container || ""),
      declaredReference: declared.reference,
      imageId,
      digest,
      inspectReference: stripRuntimeScheme(raw.inspectReference),
      version: String(raw.version || "").trim(),
      ready: raw.ready === true,
    });
    return true;
  };

  let currentPodImageCount = 0;
  for (const pod of Array.isArray(props.pods) ? props.pods : []) {
    for (const rawImage of Array.isArray(pod?.images) ? pod.images : []) {
      if (addObservation({
        namespace: pod?.namespace,
        pod: pod?.name,
        container: rawImage?.name,
        declaredReference: rawImage?.image,
        imageId: rawImage?.imageId,
        ready: rawImage?.ready,
      })) currentPodImageCount++;
    }
  }
  if (currentPodImageCount === 0) {
    for (const rawImage of Array.isArray(liveDetails.value?.images) ? liveDetails.value.images : []) {
      addObservation({
        role: rawImage?.role,
        namespace: rawImage?.namespace,
        pod: rawImage?.pod,
        container: rawImage?.container,
        declaredReference: rawImage?.declaredImage,
        imageId: rawImage?.imageId,
        digest: rawImage?.digest,
        inspectReference: rawImage?.inspectReference,
        version: rawImage?.version,
        ready: rawImage?.ready,
      });
    }
  }
  return [...observations.values()];
});

const deployedImages = computed(() => {
  const groups = new Map();
  for (const observation of imageObservations.value) {
    const declared = parseImageReference(observation.declaredReference);
    const runtimeParsed = parseImageReference(observation.imageId);
    const runtimeRepository = runtimeParsed.repository && runtimeParsed.repository !== "sha256" && !runtimeParsed.repository.startsWith("sha256:")
      ? runtimeParsed.repository
      : "";
    const repository = runtimeRepository || declared.repository;
    const exactReference = observation.inspectReference
      || (repository && observation.digest ? `${repository}@${observation.digest}` : declared.reference);
    const declaredInspectReference = declared.reference && !declared.tag && !declared.digest
      ? `${declared.repository}:latest`
      : declared.reference;
    const runtimeIdentity = observation.digest || observation.imageId;
    const key = `${observation.role}\u0000${declared.reference}\u0000${runtimeIdentity}`;
    if (!groups.has(key)) {
      groups.set(key, {
        key,
        role: observation.role,
        declaredReference: declared.reference,
        declaredInspectReference,
        declaredRepository: declared.repository,
        declaredTag: declared.tag || observation.version || (!declared.digest && declared.repository ? "latest" : ""),
        runtimeImageId: observation.imageId,
        runtimeDigest: observation.digest,
        exactReference,
        inspectReference: exactReference || declaredInspectReference,
        observations: 0,
        readyCount: 0,
        pods: new Set(),
        containers: new Set(),
      });
    }
    const item = groups.get(key);
    item.observations++;
    if (observation.ready) item.readyCount++;
    const podName = [observation.namespace, observation.pod].filter(Boolean).join("/")
      || (observation.container ? "Docker container" : "Runtime observation");
    item.pods.add(podName);
    if (observation.container) item.containers.add(observation.container);
    item.runtimeImageId ||= observation.imageId;
    item.runtimeDigest ||= observation.digest;
    item.exactReference ||= exactReference;
  }
  const roleOrder = { server: 0, webhook: 1, agent: 2 };
  return [...groups.values()]
    .map(item => ({ ...item, pods: [...item.pods].sort(), containers: [...item.containers].sort() }))
    .sort((left, right) => roleOrder[left.role] - roleOrder[right.role]
      || left.declaredReference.localeCompare(right.declaredReference)
      || left.runtimeImageId.localeCompare(right.runtimeImageId));
});

const hasRuntimeDrift = computed(() => {
  const counts = deployedImages.value.reduce((result, image) => {
    result[image.role] = (result[image.role] || 0) + 1;
    return result;
  }, {});
  return Object.values(counts).some(count => count > 1);
});

const optionalDeploymentDetails = computed(() => liveDetails.value?.details || liveDetails.value || props.deploymentDetails || props.cluster?.deploymentDetails || {});
const firstDetailValue = keys => {
  for (const key of keys) {
    const value = optionalDeploymentDetails.value?.[key];
    if (value != null && String(value).trim()) return String(value).trim();
  }
  return "";
};
const optionalDetailSummary = computed(() => [
  {
    label: "Live Rancher",
    value: firstDetailValue(["fullRancherVersion", "rancherVersion", "serverVersion"]),
    title: "Version reported by the live Rancher server",
  },
  {
    label: "Webhook",
    value: firstDetailValue(["webhookVersion", "webhookChartVersion"]),
    title: "Version reported by the deployed Rancher webhook",
  },
  {
    label: "Kubernetes",
    value: firstDetailValue(["kubernetesVersion", "k3sVersion"]),
    title: "Version reported by the cluster runtime",
  },
].filter(item => item.value));

const roleLabel = role => {
  if (role === "webhook") return "Rancher webhook";
  if (role === "agent") return "Rancher agent";
  return "Rancher server";
};
const roleTagLabel = role => {
  if (role === "webhook") return "Deployed webhook image tag";
  if (role === "agent") return "Deployed Rancher agent image tag";
  return "Deployed Rancher image tag";
};
const roleVersionLabel = role => {
  if (role === "webhook") return "Webhook image version";
  if (role === "agent") return "Rancher agent image version";
  return "Full Rancher build version";
};
const roleClass = role => {
  if (role === "webhook") return "bg-violet-100 text-violet-800 dark:bg-violet-500/15 dark:text-violet-200";
  if (role === "agent") return "bg-sky-100 text-sky-800 dark:bg-sky-500/15 dark:text-sky-200";
  return "bg-emerald-100 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-200";
};

const modalOpen = ref(false);
const activeImage = ref(null);
const dialogPanel = ref(null);
const modalCloseButton = ref(null);
const inspectPlatform = ref("linux/amd64");
const inspectLoading = ref(false);
const inspectError = ref("");
const inspection = ref(null);
const usingDeclaredFallback = ref(false);
let inspectController = null;
let modalReturnFocus = null;

const modalFocusableSelector = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

const modalFocusableElements = () => {
  const panel = dialogPanel.value;
  if (!panel) return [];
  return [...panel.querySelectorAll(modalFocusableSelector)].filter(element => (
    element instanceof HTMLElement
    && element.tabIndex >= 0
    && element.getClientRects().length > 0
  ));
};

const inspectionTarget = computed(() => {
  if (!activeImage.value) return "";
  return usingDeclaredFallback.value
    ? activeImage.value.declaredInspectReference || activeImage.value.declaredReference
    : activeImage.value.exactReference || activeImage.value.declaredInspectReference || activeImage.value.declaredReference;
});
const inspectionTargetLabel = computed(() => {
  if (usingDeclaredFallback.value) return "Declared-tag fallback (not runtime proof)";
  if (activeImage.value?.runtimeDigest) return "Exact runtime digest";
  return "Declared tag (runtime digest unavailable)";
});
const declaredFallbackAvailable = computed(() => Boolean(
  inspectError.value
  && !usingDeclaredFallback.value
  && (activeImage.value?.declaredInspectReference || activeImage.value?.declaredReference)
  && activeImage.value?.exactReference
  && activeImage.value.exactReference !== (activeImage.value.declaredInspectReference || activeImage.value.declaredReference)
));

const openImageDetails = image => {
  modalReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  activeImage.value = image;
  inspectPlatform.value = "linux/amd64";
  usingDeclaredFallback.value = false;
  inspectError.value = "";
  inspection.value = null;
  modalOpen.value = true;
  void nextTick(() => {
    if (!modalOpen.value) return;
    (modalCloseButton.value || dialogPanel.value)?.focus();
  });
  void loadInspection(false);
};

const closeImageDetails = () => {
  const returnFocus = modalReturnFocus;
  modalReturnFocus = null;
  inspectController?.abort();
  inspectController = null;
  inspectLoading.value = false;
  modalOpen.value = false;
  inspectError.value = "";
  inspection.value = null;
  activeImage.value = null;
  usingDeclaredFallback.value = false;
  void nextTick(() => {
    if (!modalOpen.value && returnFocus?.isConnected) returnFocus.focus();
  });
};

const handleDialogKeydown = event => {
  if (event.key === "Escape") {
    event.preventDefault();
    closeImageDetails();
    return;
  }
  if (event.key !== "Tab") return;
  const panel = dialogPanel.value;
  if (!panel) return;
  const focusable = modalFocusableElements();
  if (!focusable.length) {
    event.preventDefault();
    panel.focus();
    return;
  }
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  const current = document.activeElement;
  if (event.shiftKey && (current === first || !panel.contains(current))) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && (current === last || !panel.contains(current))) {
    event.preventDefault();
    first.focus();
  }
};

const loadInspection = async (force = false) => {
  const reference = String(inspectionTarget.value || "").trim();
  const platform = String(inspectPlatform.value || "linux/amd64").trim() || "linux/amd64";
  if (!reference) return;
  const cacheKey = `${reference}\u0000${platform}`;
  if (!force && inspectionCache.has(cacheKey)) {
    inspection.value = inspectionCache.get(cacheKey);
    inspectError.value = "";
    inspectLoading.value = false;
    return;
  }

  inspectController?.abort();
  const controller = new AbortController();
  inspectController = controller;
  inspectLoading.value = true;
  inspectError.value = "";
  inspection.value = null;
  try {
    const response = await apiFetch("/api/images/inspect", {
      method: "POST",
      signal: controller.signal,
      body: JSON.stringify({ reference, platform, includeBuildYaml: true }),
    });
    const payload = await response.json();
    const result = { ...payload, reference: payload?.reference || reference };
    inspectionCache.set(cacheKey, result);
    inspection.value = result;
  } catch (error) {
    if (error?.name !== "AbortError") {
      inspectError.value = error instanceof Error ? error.message : "Image inspection failed.";
    }
  } finally {
    if (inspectController === controller) {
      inspectController = null;
      inspectLoading.value = false;
    }
  }
};

const inspectDeclaredFallback = () => {
  usingDeclaredFallback.value = true;
  void loadInspection(false);
};

const useExactRuntimeReference = () => {
  usingDeclaredFallback.value = false;
  void loadInspection(false);
};

const imageConfig = computed(() => inspection.value?.config && typeof inspection.value.config === "object" ? inspection.value.config : {});
const platforms = computed(() => Array.isArray(inspection.value?.platforms) ? inspection.value.platforms : []);
const layers = computed(() => Array.isArray(inspection.value?.layers) ? inspection.value.layers : []);
const buildYaml = computed(() => inspection.value?.buildYaml && typeof inspection.value.buildYaml === "object" ? inspection.value.buildYaml : {});
const warnings = computed(() => {
  const values = Array.isArray(inspection.value?.warnings) ? inspection.value.warnings.filter(Boolean) : [];
  const primeIssues = Array.isArray(inspection.value?.primeHead?.issues)
    ? inspection.value.primeHead.issues.filter(Boolean).map(issue => `Prime provenance: ${issue}`)
    : [];
  return [...values, ...primeIssues];
});

const platformLabel = value => {
  if (!value || typeof value !== "object") return "";
  const os = String(value.os || value.OS || "").trim();
  const architecture = String(value.architecture || value.arch || "").trim();
  const variant = String(value.variant || "").trim();
  return os && architecture ? [os, architecture, variant].filter(Boolean).join("/") : "";
};
const inspectPlatformOptions = computed(() => [...new Set([
  "linux/amd64",
  "linux/arm64",
  "linux/s390x",
  "linux/ppc64le",
  ...platforms.value.map(platformLabel).filter(Boolean),
])]);
const selectPlatform = platform => {
  const value = platformLabel(platform);
  if (!value) return;
  inspectPlatform.value = value;
  void loadInspection(false);
};

const configLabels = computed(() => {
  const labels = imageConfig.value.labels;
  if (!labels || typeof labels !== "object" || Array.isArray(labels)) return [];
  return Object.entries(labels)
    .map(([key, value]) => ({ key, value: value == null ? "" : String(value) }))
    .sort((left, right) => left.key.localeCompare(right.key));
});
const configEnvironment = computed(() => Array.isArray(imageConfig.value.env)
  ? imageConfig.value.env.map(value => String(value))
  : imageConfig.value.env == null || imageConfig.value.env === "" ? [] : [String(imageConfig.value.env)]);

const normalizeVersionValue = rawValue => {
  const value = String(rawValue ?? "").trim();
  if (value.length >= 2 && ((value[0] === "\"" && value[value.length - 1] === "\"") || (value[0] === "'" && value[value.length - 1] === "'"))) {
    return value.slice(1, -1).trim();
  }
  return value;
};
const parseEnvironmentEntry = entry => {
  const raw = String(entry ?? "");
  const separator = raw.indexOf("=");
  if (separator <= 0) return null;
  const key = raw.slice(0, separator).trim();
  return key ? { key, rawValue: raw.slice(separator + 1) } : null;
};
const expectedWebhookVersion = computed(() => {
  const environment = configEnvironment.value.map(parseEnvironmentEntry).filter(Boolean);
  for (const key of webhookEnvironmentKeys) {
    const entry = environment.find(item => item.key === key);
    const value = normalizeVersionValue(entry?.rawValue);
    if (entry && value) return { value, rawValue: entry.rawValue, sourceKey: entry.key, sourceType: "image environment" };
  }
  const label = configLabels.value
    .filter(item => item.key.toLowerCase().endsWith("webhook.version"))
    .sort((left, right) => left.key.length - right.key.length || left.key.localeCompare(right.key))[0];
  const labelValue = normalizeVersionValue(label?.value);
  if (label && labelValue) return { value: labelValue, rawValue: label.value, sourceKey: label.key, sourceType: "image label" };
  const data = buildYaml.value?.data;
  if (data && typeof data === "object" && !Array.isArray(data)) {
    const entry = Object.entries(data).find(([key]) => key.toLowerCase() === "webhookversion");
    const value = normalizeVersionValue(entry?.[1]);
    if (entry && value) return { value, rawValue: String(entry[1]), sourceKey: entry[0], sourceType: "embedded build.yaml" };
  }
  return null;
});

const imageBuildVersion = computed(() => {
  const label = configLabels.value.find(item => item.key === "org.opencontainers.image.version");
  const value = normalizeVersionValue(label?.value) || activeImage.value?.declaredTag || "";
  return {
    value,
    source: label ? "org.opencontainers.image.version" : activeImage.value?.declaredTag ? "declared image tag fallback" : "",
  };
});
const deployedWebhookTags = computed(() => [...new Set(deployedImages.value
  .filter(image => image.role === "webhook" && image.declaredTag)
  .map(image => image.declaredTag))]);

const inspectedDigest = computed(() => String(inspection.value?.digest || "").toLowerCase());
const observedRuntimeDigest = computed(() => String(activeImage.value?.runtimeDigest || "").toLowerCase());
const digestMatchState = computed(() => {
  if (!observedRuntimeDigest.value) return "unknown";
  if (!inspectedDigest.value) return "unknown";
  return inspectedDigest.value === observedRuntimeDigest.value ? "match" : "mismatch";
});
const digestMatchLabel = computed(() => {
  if (usingDeclaredFallback.value) {
    if (digestMatchState.value === "match") return "Tag currently matches runtime digest · not runtime proof";
    if (digestMatchState.value === "mismatch") return "Tag differs from runtime digest";
    return "Declared-tag fallback · not runtime proof";
  }
  if (digestMatchState.value === "match") return "Exact runtime digest verified";
  if (digestMatchState.value === "mismatch") return "Runtime digest mismatch";
  return "Runtime digest unavailable";
});
const digestMatchClass = computed(() => digestMatchState.value === "match" && !usingDeclaredFallback.value
  ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300"
  : digestMatchState.value === "mismatch"
    ? "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-300"
    : "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300");

const overviewItems = computed(() => {
  const selected = activeImage.value || {};
  const expectedWebhookTitle = expectedWebhookVersion.value
    ? `${expectedWebhookVersion.value.sourceKey} (${expectedWebhookVersion.value.sourceType}) · raw value: ${expectedWebhookVersion.value.rawValue}`
    : "";
  return [
    { label: "Runtime component", value: roleLabel(selected.role) },
    { label: "Cluster", value: props.cluster.name || props.cluster.id },
    { label: "Pods", value: selected.pods?.join(", "), title: selected.pods?.join("\n") },
    { label: "Containers", value: selected.containers?.join(", ") },
    { label: roleVersionLabel(selected.role), value: imageBuildVersion.value.value, title: imageBuildVersion.value.source, mono: true, accent: true },
    { label: "Declared image tag", value: selected.declaredTag, mono: true, accent: true },
    { label: "Expected webhook version", value: expectedWebhookVersion.value?.value, title: expectedWebhookTitle, mono: true, accent: true },
    { label: "Deployed webhook tag", value: deployedWebhookTags.value.join(", "), mono: true, accent: true },
    { label: "Declared reference", value: selected.declaredReference, mono: true },
    { label: "Runtime image ID", value: selected.runtimeImageId, mono: true },
    { label: "Inspection evidence", value: inspectionTargetLabel.value },
    { label: "Runtime digest match", value: digestMatchLabel.value },
    { label: "Registry", value: inspection.value?.registry },
    { label: "Repository", value: inspection.value?.repository, mono: true },
    { label: "Registry tag", value: inspection.value?.tag, mono: true },
    { label: "Selected platform", value: inspection.value?.platform || inspectPlatform.value, mono: true },
    { label: "Reference digest", value: shortDigest(inspection.value?.digest), title: inspection.value?.digest, mono: true },
    { label: "Runtime digest", value: shortDigest(selected.runtimeDigest), title: selected.runtimeDigest, mono: true },
    { label: "Reference media type", value: inspection.value?.mediaType, mono: true },
    { label: "Created", value: formatDate(inspection.value?.createdAt) },
    { label: "Uploaded", value: formatDate(inspection.value?.uploadedAt) },
    { label: "Selected image size", value: formatBytes(inspection.value?.size) },
  ];
});

const configurationSummary = computed(() => [
  { label: "OS", value: imageConfig.value.os },
  { label: "Architecture", value: imageConfig.value.architecture },
  { label: "Variant", value: imageConfig.value.variant },
  { label: "Created", value: formatDate(imageConfig.value.createdAt) },
  { label: "Config digest", value: shortDigest(imageConfig.value.digest) },
  { label: "Config size", value: formatBytes(imageConfig.value.size) },
].filter(item => item.value && item.value !== "Unavailable"));
const commandLabel = value => Array.isArray(value) ? value.map(item => String(item)).join(" ") : value == null ? "" : String(value);
const configEntrypoint = computed(() => commandLabel(imageConfig.value.entrypoint));
const configCommand = computed(() => commandLabel(imageConfig.value.cmd));
const hasConfiguration = computed(() => Boolean(configurationSummary.value.length || configLabels.value.length || configEnvironment.value.length || configEntrypoint.value || configCommand.value));

const configHistoryTotal = computed(() => Array.isArray(imageConfig.value.history) ? imageConfig.value.history.length : 0);
const configHistory = computed(() => {
  const history = Array.isArray(imageConfig.value.history) ? imageConfig.value.history : [];
  const offset = Math.max(history.length - 200, 0);
  return history.slice(offset).map((item, index) => ({
    step: offset + index + 1,
    created: item?.created == null ? "" : String(item.created),
    createdBy: item?.createdBy == null ? "" : String(item.createdBy),
    comment: item?.comment == null ? "" : String(item.comment),
    emptyLayer: Boolean(item?.emptyLayer),
  }));
});
const layerTotalSize = computed(() => formatBytes(layers.value.reduce((total, layer) => {
  const size = Number(layer?.size);
  return total + (Number.isFinite(size) ? size : 0);
}, 0)));

const buildYamlStatus = computed(() => {
  if (buildYaml.value.found) return "Found";
  if (buildYaml.value.skipped) return "Skipped";
  if (buildYaml.value.error) return "Error";
  return "Not found";
});
const buildYamlStatusClass = computed(() => {
  if (buildYaml.value.found) return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300";
  if (buildYaml.value.skipped) return "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300";
  if (buildYaml.value.error) return "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-300";
  return "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300";
});
const rawBuildYaml = computed(() => {
  if (typeof buildYaml.value.raw === "string") return buildYaml.value.raw;
  if (buildYaml.value.data == null) return "";
  try {
    return JSON.stringify(buildYaml.value.data, null, 2);
  } catch (_) {
    return String(buildYaml.value.data);
  }
});
const flattenStructuredData = (value, path = "root", entries = []) => {
  if (entries.length >= 250) return entries;
  if (Array.isArray(value)) {
    if (!value.length) entries.push({ path, value: [] });
    value.forEach((item, index) => flattenStructuredData(item, `${path}[${index}]`, entries));
    return entries;
  }
  if (value && typeof value === "object") {
    const items = Object.entries(value).sort(([left], [right]) => left.localeCompare(right));
    if (!items.length) entries.push({ path, value: {} });
    items.forEach(([key, item]) => flattenStructuredData(item, path === "root" ? key : `${path}.${key}`, entries));
    return entries;
  }
  entries.push({ path, value });
  return entries;
};
const structuredBuildEntries = computed(() => buildYaml.value.data == null ? [] : flattenStructuredData(buildYaml.value.data));
const structuredValue = value => {
  if (value == null) return "null";
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value);
  } catch (_) {
    return String(value);
  }
};

function shortDigest(value) {
  const text = String(value || "");
  if (!text) return "";
  return text.length > 25 ? `${text.slice(0, 22)}…` : text;
}

function formatDate(value) {
  if (!value) return "Unavailable";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
}

function formatBytes(value) {
  const bytes = Number(value);
  if (!Number.isFinite(bytes) || bytes <= 0) return "Unavailable";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const amount = bytes / (1024 ** unit);
  return `${amount >= 100 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`;
}

const handleEscape = event => {
  if (event.key === "Escape" && modalOpen.value) closeImageDetails();
};
onMounted(() => {
  window.addEventListener("keydown", handleEscape);
  void loadLiveDetails();
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", handleEscape);
  inspectController?.abort();
  liveDetailsController?.abort();
});
</script>
