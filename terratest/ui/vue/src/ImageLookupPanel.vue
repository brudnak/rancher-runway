<template>
  <div class="grid min-w-0 gap-5">
    <header class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0">
        <div class="flex items-center gap-2.5">
          <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="m21 21-4.35-4.35m2.1-5.4a7.5 7.5 0 1 1-15 0 7.5 7.5 0 0 1 15 0Z" />
              <path stroke-linecap="round" stroke-linejoin="round" d="M8.75 9.25h5m-5 3h3" />
            </svg>
          </div>
          <div>
            <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">Image Lookup</h2>
            <p class="mt-1 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
              Discover community and Prime-head image builds, verify server/agent candidates, then inspect manifests, provenance, platforms, and embedded build metadata.
            </p>
          </div>
        </div>
      </div>

      <div class="flex shrink-0 flex-wrap items-center gap-2 text-xs font-semibold text-zinc-500 dark:text-zinc-400">
        <span class="rounded-full border border-zinc-200 bg-zinc-50 px-3 py-1.5 dark:border-white/10 dark:bg-white/[0.04]">Read-only registry requests</span>
        <span v-if="searchedAtLabel" class="rounded-full border border-zinc-200 bg-white px-3 py-1.5 dark:border-white/10 dark:bg-white/[0.04]">{{ searchedAtLabel }}</span>
      </div>
    </header>

    <form
      class="rounded-2xl border border-zinc-200/80 bg-zinc-50/70 p-4 shadow-sm dark:border-white/10 dark:bg-white/[0.025] sm:p-5"
      :aria-busy="searchLoading ? 'true' : 'false'"
      @submit.prevent="submitImageSearch"
    >
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-12">
        <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300 xl:col-span-3">
          <span>Registry</span>
          <select
            v-model="registry"
            :disabled="searchLoading || customHasExplicitRegistry"
            class="h-11 w-full rounded-xl border border-zinc-200 bg-white px-3.5 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 dark:border-white/10 dark:bg-zinc-900 dark:text-white"
          >
            <option v-for="option in registryOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
          <span v-if="customHasExplicitRegistry" class="text-xs font-normal leading-5 text-zinc-500 dark:text-zinc-400">The host in the custom repository takes priority.</span>
        </label>

        <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300 xl:col-span-3">
          <span>Image family</span>
          <select
            v-model="imageFamily"
            :disabled="searchLoading"
            class="h-11 w-full rounded-xl border border-zinc-200 bg-white px-3.5 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 dark:border-white/10 dark:bg-zinc-900 dark:text-white"
          >
            <option v-for="option in imageFamilyOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>

        <label class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300 xl:col-span-4">
          <span>Tag, patch, selector, SHA, or full reference <span class="font-normal text-zinc-400">(optional)</span></span>
          <input
            v-model.trim="query"
            type="search"
            autocomplete="off"
            :disabled="searchLoading"
            placeholder="2.15.1, v2.15.1-head, SHA, or registry/image:tag"
            class="h-11 w-full rounded-xl border border-zinc-200 bg-white px-3.5 text-sm font-semibold text-zinc-900 outline-none placeholder:font-normal placeholder:text-zinc-400 focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500"
            @input="syncQuickFilterFromQuery($event.target.value)"
          />
        </label>

        <div class="flex items-end gap-2 xl:col-span-2">
          <button
            type="submit"
            :disabled="searchLoading || !searchRepository"
            class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl bg-emerald-500 px-4 text-sm font-bold text-white shadow-md shadow-emerald-500/15 transition-colors hover:bg-emerald-600 disabled:cursor-not-allowed disabled:opacity-55"
          >
            <span v-if="searchLoading" class="spinner !h-4 !w-4 !border-[1.5px]"></span>
            <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.25" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="m21 21-4.35-4.35m2.1-5.4a7.5 7.5 0 1 1-15 0 7.5 7.5 0 0 1 15 0Z" />
            </svg>
            {{ searchButtonLabel }}
          </button>
          <button
            v-if="searched || searchError"
            type="button"
            :disabled="searchLoading"
            class="inline-flex h-11 items-center justify-center rounded-xl border border-zinc-200 bg-white px-3.5 text-sm font-semibold text-zinc-600 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.05] dark:text-zinc-300 dark:hover:bg-white/[0.08]"
            title="Clear search results"
            aria-label="Clear image search results"
            @click="clearSearch"
          >
            Clear
          </button>
        </div>

        <label
          v-if="imageFamily === 'custom'"
          class="grid gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300 md:col-span-2 xl:col-span-12"
        >
          <span>Custom full repository</span>
          <input
            v-model.trim="customRepository"
            type="text"
            autocomplete="off"
            spellcheck="false"
            :disabled="searchLoading"
            placeholder="registry.example.com/team/rancher (repository only; put the tag in Tag search)"
            class="h-11 w-full rounded-xl border border-zinc-200 bg-white px-3.5 font-mono text-sm text-zinc-900 outline-none placeholder:font-sans placeholder:text-zinc-400 focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 dark:border-white/10 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500"
          />
        </label>
      </div>

      <div
        class="mt-4 flex flex-col gap-2 rounded-xl border px-3.5 py-3 text-xs leading-5 sm:flex-row sm:items-center sm:justify-between"
        :class="lookupHintClass"
      >
        <div class="min-w-0">
          <span class="font-bold">{{ lookupHintTitle }}</span>
          <span class="ml-1">{{ lookupHint }}</span>
        </div>
        <button
          v-if="primePatchSuggestion"
          type="button"
          :disabled="searchLoading"
          class="inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-violet-300 bg-white px-3 font-bold text-violet-800 hover:bg-violet-50 disabled:opacity-55 dark:border-violet-500/30 dark:bg-white/[0.05] dark:text-violet-200 dark:hover:bg-violet-500/10"
          @click="findPrimePatchHeads"
        >
          Find {{ primePatchSuggestion }} builds
        </button>
        <button
          v-else-if="directInspectReference"
          type="button"
          :disabled="inspectLoading"
          class="inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-lg border border-emerald-300 bg-white px-3 font-bold text-emerald-800 hover:bg-emerald-50 disabled:opacity-55 dark:border-emerald-500/30 dark:bg-white/[0.05] dark:text-emerald-200 dark:hover:bg-emerald-500/10"
          @click="inspectExactInput"
        >
          <span v-if="inspectLoading" class="spinner !h-3.5 !w-3.5 !border-[1.5px]"></span>
          Inspect exact reference
        </button>
      </div>

      <div class="mt-4 flex flex-col gap-3 border-t border-zinc-200/70 pt-4 dark:border-white/10 sm:flex-row sm:items-center sm:justify-between">
        <fieldset class="min-w-0">
          <legend class="sr-only">Filter loaded tags by release channel</legend>
          <div class="flex flex-wrap gap-2" aria-label="Release channel filters">
            <button
              v-for="filter in quickFilters"
              :key="filter.id"
              type="button"
              :disabled="searchLoading"
              :aria-pressed="quickFilter === filter.id ? 'true' : 'false'"
              class="rounded-full border px-3 py-1.5 text-xs font-bold transition-colors"
              :class="quickFilter === filter.id
                ? 'border-emerald-500 bg-emerald-500 text-white shadow-sm shadow-emerald-500/15'
                : 'border-zinc-200 bg-white text-zinc-600 hover:border-emerald-300 hover:text-emerald-700 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300 dark:hover:border-emerald-500/40 dark:hover:text-emerald-300'"
              @click="applyQuickFilter(filter.id)"
            >
              {{ filter.label }}
            </button>
          </div>
        </fieldset>
        <div class="flex min-w-0 flex-col gap-2 sm:items-end">
          <div class="flex flex-wrap gap-2">
            <label
              class="inline-flex cursor-pointer items-center gap-2 rounded-lg border border-zinc-200 bg-white px-2.5 py-1.5 text-xs font-bold text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300"
              title="Show only unsuffixed base tags, hiding tags such as -linux-amd64 and -linux-arm64."
            >
              <input
                type="checkbox"
                class="h-3.5 w-3.5 rounded border-zinc-300 text-emerald-500 accent-emerald-500"
                :checked="architectureFilter === 'multi'"
                :disabled="searchLoading"
                @change="setBaseTagsOnly($event.target.checked)"
              />
              <span>Base tags only</span>
            </label>
            <label
              class="inline-flex cursor-pointer items-center gap-2 rounded-lg border border-zinc-200 bg-white px-2.5 py-1.5 text-xs font-bold text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300"
              title="Limit results to images with a reliable upload, creation, or verified-pair completion time in the last 30 days. Undated images are excluded."
            >
              <input
                v-model="recentOnly"
                type="checkbox"
                class="h-3.5 w-3.5 rounded border-zinc-300 text-emerald-500 accent-emerald-500"
                :disabled="searchLoading"
                @change="handleRecentOnlyChange($event.target.checked)"
              />
              <span>Last 30 days</span>
            </label>
          </div>
          <p class="text-xs leading-5 text-zinc-500 dark:text-zinc-400">
            <template v-if="recentOnly">Uses known timestamps and may require a complete registry scan; undated images are excluded.</template>
            <template v-else>Enter a patch such as 2.15.1 to get a fast verified Prime-head lookup.</template>
          </p>
        </div>
      </div>

      <div class="mt-4 grid gap-3 border-t border-zinc-200/70 pt-4 dark:border-white/10 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
        <label class="grid gap-1.5 text-xs font-bold text-zinc-600 dark:text-zinc-300">
          <span>Prime scope</span>
          <select v-model="primeHeadFilter" :disabled="searchLoading" class="h-10 rounded-xl border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white">
            <option value="all">All images</option>
            <option value="only">Prime heads only</option>
            <option value="exclude">Exclude Prime heads</option>
          </select>
        </label>

        <label class="grid gap-1.5 text-xs font-bold text-zinc-600 dark:text-zinc-300">
          <span>Prime head form</span>
          <select v-model="headKindFilter" :disabled="searchLoading || primeHeadFilter === 'exclude'" class="h-10 rounded-xl border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 disabled:opacity-55 dark:border-white/10 dark:bg-zinc-900 dark:text-white">
            <option value="all">Moving + immutable</option>
            <option value="moving">Moving selectors</option>
            <option value="immutable">Immutable builds</option>
          </select>
        </label>

        <label class="grid gap-1.5 text-xs font-bold text-zinc-600 dark:text-zinc-300">
          <span>Architecture</span>
          <select v-model="architectureFilter" :disabled="searchLoading" class="h-10 rounded-xl border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white">
            <option v-for="option in architectureOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>

        <label class="grid gap-1.5 text-xs font-bold text-zinc-600 dark:text-zinc-300">
          <span>Release line / patch</span>
          <input v-model.trim="versionLineFilter" type="text" inputmode="decimal" autocomplete="off" spellcheck="false" :disabled="searchLoading" placeholder="2.15 or 2.15.1" class="h-10 rounded-xl border border-zinc-200 bg-white px-3 font-mono text-sm text-zinc-900 outline-none placeholder:font-sans placeholder:text-zinc-400 focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white" />
        </label>

        <label class="grid gap-1.5 text-xs font-bold text-zinc-600 dark:text-zinc-300">
          <span>Prime commit SHA</span>
          <input v-model.trim="commitFilter" type="text" autocomplete="off" spellcheck="false" :disabled="searchLoading" placeholder="7–40 hex characters" class="h-10 rounded-xl border border-zinc-200 bg-white px-3 font-mono text-sm text-zinc-900 outline-none placeholder:font-sans placeholder:text-zinc-400 focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white" />
        </label>

        <label class="grid gap-1.5 text-xs font-bold text-zinc-600 dark:text-zinc-300">
          <span>Pair evidence</span>
          <select v-model="pairStatusFilter" :disabled="searchLoading" class="h-10 rounded-xl border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 dark:border-white/10 dark:bg-zinc-900 dark:text-white">
            <option value="all">All candidates</option>
            <option value="verified">Verified complete</option>
            <option value="attention">Needs attention</option>
          </select>
        </label>
      </div>

      <div class="mt-3 flex flex-col gap-2 text-xs leading-5 text-zinc-500 dark:text-zinc-400 sm:flex-row sm:items-center sm:justify-between">
        <span>Filters refine loaded rows immediately; Search applies supported filters at the registry lookup so matches beyond the current 200-tag page can be considered.<template v-if="recentOnly"> Images without reliable dates are excluded.</template></span>
        <button v-if="activeAdvancedFilterCount" type="button" :disabled="searchLoading" class="shrink-0 font-bold text-emerald-700 hover:underline dark:text-emerald-300" @click="resetAdvancedFilters">
          Reset {{ activeAdvancedFilterCount }} filter{{ activeAdvancedFilterCount === 1 ? "" : "s" }}
        </button>
      </div>
    </form>

    <div v-if="searchError" role="alert" class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200">
      <div class="font-bold">Image search failed</div>
      <div class="mt-1 whitespace-pre-wrap break-words">{{ searchError }}</div>
    </div>

    <div class="grid min-w-0 items-start gap-5" :class="detailVisible ? 'xl:grid-cols-[minmax(0,1.45fr)_minmax(23rem,0.85fr)]' : ''">
      <section class="grid min-w-0 gap-4">
        <div v-if="searchLoading" class="grid gap-3" role="status">
          <div v-for="index in 3" :key="index" class="animate-pulse rounded-2xl border border-zinc-200 bg-zinc-50/70 p-5 dark:border-white/10 dark:bg-white/[0.025]">
            <div class="h-4 w-44 rounded bg-zinc-200 dark:bg-zinc-700"></div>
            <div class="mt-3 h-3 w-72 max-w-full rounded bg-zinc-100 dark:bg-zinc-800"></div>
            <div class="mt-5 h-24 rounded-xl bg-white dark:bg-zinc-900"></div>
          </div>
          <span class="sr-only">Searching image registries</span>
        </div>

        <div v-else-if="searchError" class="rounded-2xl border border-dashed border-rose-200 bg-rose-50/50 px-6 py-8 text-center text-sm leading-6 text-rose-700 dark:border-rose-500/20 dark:bg-rose-500/[0.06] dark:text-rose-200">
          No results are being shown for the failed request. Review the error above, adjust the repository or tag, and try again.
        </div>

        <div v-else-if="!searched" class="rounded-2xl border border-dashed border-zinc-300 bg-zinc-50/60 px-6 py-12 text-center dark:border-white/15 dark:bg-white/[0.02]">
          <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-white/[0.06] dark:text-zinc-500">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="M20 7 12 3 4 7m16 0-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
            </svg>
          </div>
          <h3 class="mt-4 text-base font-bold text-zinc-900 dark:text-zinc-100">Search registry tags</h3>
          <p class="mx-auto mt-2 max-w-xl text-sm leading-6 text-zinc-500 dark:text-zinc-400">
            Choose a registry and image family. Leave the search blank to browse available tags, enter a patch-qualified Prime selector to discover verified candidates, or paste an exact image reference.
          </p>
        </div>

        <template v-else>
          <div class="flex flex-col gap-3 rounded-xl border border-zinc-200 bg-zinc-50/70 px-4 py-3 dark:border-white/10 dark:bg-white/[0.025] sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div class="text-sm font-bold text-zinc-900 dark:text-zinc-100">{{ resultSummary }}</div>
              <div class="mt-1 text-xs leading-5 text-zinc-500 dark:text-zinc-400">{{ scanSummary }}</div>
            </div>
            <div class="flex flex-wrap items-center gap-2 text-xs font-bold">
              <label class="inline-flex items-center gap-2 rounded-lg border border-zinc-200 bg-white px-2.5 py-1.5 text-zinc-500 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">
                <span>Sort</span>
                <select
                  v-model="resultSort"
                  class="max-w-[13rem] bg-transparent text-xs font-bold text-zinc-800 outline-none dark:text-zinc-200"
                  aria-label="Sort image tags"
                  @change="handleResultSortChange"
                >
                  <option value="fast">Fast · first matching tags</option>
                  <option value="pair-rank">Verified pair completion rank (when available)</option>
                  <option value="version-desc">Version / tag ↓</option>
                  <option value="version-asc">Version / tag ↑</option>
                  <option value="uploaded-desc">Upload time ↓ (when available)</option>
                  <option value="uploaded-asc">Upload time ↑ (when available)</option>
                  <option value="tag-asc">Tag A–Z</option>
                  <option value="tag-desc">Tag Z–A</option>
                </select>
              </label>
              <span class="rounded-full bg-emerald-100 px-3 py-1 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300">{{ visibleTagCount }} shown</span>
              <span class="rounded-full bg-zinc-200/70 px-3 py-1 text-zinc-600 dark:bg-white/[0.07] dark:text-zinc-300">{{ totalMatched }} matched</span>
            </div>
          </div>

          <div v-if="!groups.length" class="rounded-2xl border border-dashed border-zinc-300 bg-zinc-50/60 px-6 py-10 text-center dark:border-white/15 dark:bg-white/[0.02]">
            <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-100">No registry groups returned</h3>
            <p class="mt-2 text-sm leading-6 text-zinc-500 dark:text-zinc-400">Try a broader tag search or another registry and image family.</p>
          </div>

          <div v-else-if="!visibleTagCount" class="rounded-2xl border border-dashed border-zinc-300 bg-zinc-50/60 px-6 py-10 text-center dark:border-white/15 dark:bg-white/[0.02]">
            <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-100">No candidates pass the active filters</h3>
            <p class="mt-2 text-sm leading-6 text-zinc-500 dark:text-zinc-400">Reset one or more Prime, head-form, architecture, version, commit, or pair filters, then search again if the registry response was truncated.</p>
          </div>

          <article
            v-for="group in displayGroups"
            :key="group.key || `${group.registry}/${group.repository}`"
            class="min-w-0 overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-sm dark:border-white/10 dark:bg-zinc-900/60"
          >
            <div class="flex flex-col gap-3 border-b border-zinc-200/70 bg-zinc-50/75 px-4 py-4 dark:border-white/10 dark:bg-white/[0.025] sm:flex-row sm:items-start sm:justify-between">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="font-bold text-zinc-900 dark:text-zinc-100">{{ group.label || group.registry || "Registry" }}</h3>
                  <span v-if="group.imageRole" class="rounded-full border border-zinc-200 bg-white px-2.5 py-0.5 text-[11px] font-bold text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300">{{ roleLabel(group.imageRole) }}</span>
                  <span class="rounded-full border px-2.5 py-0.5 text-[11px] font-bold" :class="groupStatusClass(group)">
                    {{ group.error ? "Lookup error" : `${group.visibleTags.length} shown` }}
                  </span>
                  <span v-if="group.truncated" class="rounded-full border border-amber-200 bg-amber-50 px-2.5 py-0.5 text-[11px] font-bold text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300">Truncated</span>
                  <span v-if="group.primeHeadCount" class="rounded-full border border-violet-200 bg-violet-50 px-2.5 py-0.5 text-[11px] font-bold text-violet-700 dark:border-violet-500/25 dark:bg-violet-500/10 dark:text-violet-300">{{ group.primeHeadCount }} Prime head{{ group.primeHeadCount === 1 ? "" : "s" }}</span>
                  <span v-if="group.verifiedPrimeHeadCount" class="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-0.5 text-[11px] font-bold text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300">{{ group.verifiedPrimeHeadCount }} verified</span>
                  <span v-if="Number(group.invalidPrimeHeadCount || 0) + Number(group.missingCompanionCount || 0)" class="rounded-full border border-rose-200 bg-rose-50 px-2.5 py-0.5 text-[11px] font-bold text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-300">{{ Number(group.invalidPrimeHeadCount || 0) + Number(group.missingCompanionCount || 0) }} need attention</span>
                </div>
                <div class="mt-1 break-all font-mono text-xs text-zinc-500 dark:text-zinc-400">{{ group.reference || `${group.registry}/${group.repository}` }}</div>
                <div v-if="group.companionRepository" class="mt-1 break-all text-[11px] text-zinc-500 dark:text-zinc-500">Expected pair repository: <span class="font-mono">{{ group.companionRepository }}</span></div>
              </div>
              <div class="shrink-0 text-xs font-semibold text-zinc-500 dark:text-zinc-400">
                {{ Number(group.matched || 0) }} matched · {{ Number(group.scanned || 0) }} scanned
              </div>
            </div>

            <div v-if="group.error" class="border-b border-rose-200 bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-800 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-200">
              {{ group.error }}
            </div>
            <div v-if="group.truncated" class="border-b border-amber-200 bg-amber-50/70 px-4 py-2.5 text-xs font-semibold leading-5 text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">
              <template v-if="searchResponse?.scanMode === 'bounded'">
                Fast mode returned the first matching registry tags up to the 200-row limit, not a global newest or version order. Recent-date and verified-pair checks may scan farther for correctness. Choose an explicit sort for complete global ordering, or add a patch or SHA to narrow the search.
              </template>
              <template v-else-if="searchResponse?.scanMode === 'complete'">
                The selected global sort was applied to all scanned matches, but the registry scan or 200-row result limit truncated this view. Add a release line, patch, or SHA to narrow the result set.
              </template>
              <template v-else>
                Results were truncated by the registry scan or 200-row limit. Add a release line, patch, or SHA to narrow the result set.
              </template>
            </div>

            <div v-if="group.visibleTags.length" class="overflow-x-auto">
              <table class="w-full min-w-[1020px] table-fixed border-collapse text-left">
                <thead class="bg-zinc-50/50 text-[11px] font-bold uppercase tracking-wide text-zinc-500 dark:bg-white/[0.018] dark:text-zinc-400">
                  <tr>
                    <th scope="col" class="w-[27%] px-4 py-2.5">Tag</th>
                    <th scope="col" class="w-[23%] px-3 py-2.5">Build identity</th>
                    <th scope="col" class="w-[16%] px-3 py-2.5">Pair evidence</th>
                    <th scope="col" class="w-[10%] px-3 py-2.5">Architecture</th>
                    <th scope="col" class="w-[13%] px-3 py-2.5">Time</th>
                    <th scope="col" class="w-[11%] px-3 py-2.5">Image</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-zinc-200/70 dark:divide-white/10">
                  <tr
                    v-for="tag in group.visibleTags"
                    :key="tag.reference || tag.name"
                    class="transition-colors hover:bg-emerald-50/45 dark:hover:bg-emerald-500/[0.055]"
                    :class="selectedReference === tagReference(group, tag) ? 'bg-emerald-50/70 dark:bg-emerald-500/[0.08]' : ''"
                  >
                    <td class="px-4 py-3 align-top">
                      <button
                        type="button"
                        class="max-w-full text-left font-mono text-sm font-bold text-emerald-700 hover:underline dark:text-emerald-300"
                        :title="`Inspect ${tagReference(group, tag)}`"
                        @click="inspectTag(group, tag)"
                      >
                        <span class="break-all">{{ tag.name || tagReference(group, tag) }}</span>
                      </button>
                      <div v-if="tag.baseTag" class="mt-1 truncate text-[11px] text-zinc-500 dark:text-zinc-500" :title="tag.baseTag">Base {{ tag.baseTag }}</div>
                      <div class="mt-1 flex flex-wrap gap-1.5">
                        <span class="rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-[10px] font-bold text-zinc-600 dark:border-white/10 dark:bg-white/[0.035] dark:text-zinc-400">{{ roleLabel(tag.imageRole) }}</span>
                        <span v-if="tag.artifact" class="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[10px] font-bold text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300">Artifact</span>
                      </div>
                    </td>
                    <td class="px-3 py-3 align-top">
                      <div class="flex flex-wrap gap-1.5">
                        <span class="inline-flex rounded-full border px-2.5 py-1 text-[11px] font-bold" :class="channelClass(tag)">{{ tag.channel || "unknown" }}</span>
                        <span v-if="tag.isPrimeHead" class="inline-flex rounded-full border border-violet-200 bg-violet-50 px-2.5 py-1 text-[11px] font-bold text-violet-700 dark:border-violet-500/25 dark:bg-violet-500/10 dark:text-violet-300">Prime head</span>
                        <span v-if="tag.headKind" class="inline-flex rounded-full border px-2.5 py-1 text-[11px] font-bold" :class="headKindClass(tag.headKind)">{{ headKindLabel(tag.headKind) }}</span>
                      </div>
                      <div v-if="tag.isPrimeHead" class="mt-2 space-y-1 text-[11px] leading-4 text-zinc-500 dark:text-zinc-400" :title="primeMetadataTitle(tag)">
                        <div v-if="tag.version"><span class="font-semibold">Patch</span> <code>{{ normalizeVersionLabel(tag.version) }}</code></div>
                        <div v-if="tag.commit"><span class="font-semibold">Commit</span> <code>{{ shortCommit(tag.commit) }}</code></div>
                        <div v-if="tag.selector && tag.headKind === 'immutable'"><span class="font-semibold">Moving selector</span> <code>{{ tag.selector }}</code></div>
                        <div v-if="tag.ossRevision"><span class="font-semibold">OSS revision</span> <code>{{ shortCommit(tag.ossRevision) }}</code></div>
                        <div v-if="tag.sourceRepository" class="truncate" :title="tag.sourceRepository"><span class="font-semibold">Source</span> {{ compactSource(tag.sourceRepository) }}</div>
                        <div v-if="tag.primeSourceValid === true" class="font-bold text-emerald-700 dark:text-emerald-300">Canonical Prime source</div>
                        <div v-if="tag.pairStatus === 'invalid' && (tag.primeSourceValid === false || tag.provenanceValid === false)" class="font-bold text-rose-700 dark:text-rose-300">Prime provenance needs attention</div>
                      </div>
                    </td>
                    <td class="px-3 py-3 align-top">
                      <template v-if="tag.isPrimeHead">
                        <span class="inline-flex rounded-full border px-2.5 py-1 text-[11px] font-bold" :class="pairStatusClass(tag)">{{ pairStatusLabel(tag) }}</span>
                        <div v-if="tag.resolvedRank" class="mt-2 text-[11px] font-bold text-emerald-700 dark:text-emerald-300">Resolved rank #{{ tag.resolvedRank }}</div>
                        <div v-if="tag.companionReference" class="mt-1 truncate font-mono text-[10px] text-zinc-500 dark:text-zinc-500" :title="tag.companionReference">{{ tag.companionVerified ? "Verified with" : "Expected" }} {{ tag.companionReference }}</div>
                        <div v-if="tag.pairError" class="mt-1 line-clamp-3 text-[10px] font-semibold leading-4" :class="tag.headKind === 'moving' ? 'text-amber-700 dark:text-amber-300' : 'text-rose-700 dark:text-rose-300'" :title="tag.pairError">{{ tag.pairError }}</div>
                      </template>
                      <span v-else class="text-xs text-zinc-400">—</span>
                    </td>
                    <td class="break-words px-3 py-3 align-top text-xs font-semibold text-zinc-600 dark:text-zinc-300">{{ architectureLabel(tag.architecture) }}</td>
                    <td class="px-3 py-3 align-top text-[11px] leading-5 text-zinc-600 dark:text-zinc-300">
                      <div v-if="tag.pairCompletedAt"><span class="block text-[10px] font-bold uppercase tracking-wide text-zinc-400">Pair complete</span>{{ formatDate(tag.pairCompletedAt) }}</div>
                      <div v-if="tag.uploadedAt" :class="tag.pairCompletedAt ? 'mt-2' : ''"><span class="block text-[10px] font-bold uppercase tracking-wide text-zinc-400">Uploaded</span>{{ formatDate(tag.uploadedAt) }}</div>
                      <span v-if="!tag.pairCompletedAt && !tag.uploadedAt">—</span>
                    </td>
                    <td class="px-3 py-3 align-top text-[11px] text-zinc-600 dark:text-zinc-300">
                      <div class="font-semibold">{{ formatBytes(tag.size) }}</div>
                      <code class="mt-1 block break-all text-[10px] text-zinc-500 dark:text-zinc-400" :title="tag.digest || ''">{{ shortDigest(tag.digest) }}</code>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else-if="!group.error" class="px-5 py-8 text-center text-sm leading-6 text-zinc-500 dark:text-zinc-400">
              {{ group.tags.length ? "No loaded tags pass the current filters." : "No matching tags were found in this repository." }}
            </div>
          </article>
        </template>
      </section>

      <aside
        v-if="detailVisible"
        class="min-w-0 overflow-hidden rounded-2xl border border-zinc-200 bg-zinc-50/70 shadow-lg shadow-zinc-200/40 dark:border-white/10 dark:bg-zinc-950/70 dark:shadow-black/25 xl:sticky xl:top-24"
        :aria-busy="inspectLoading ? 'true' : 'false'"
      >
        <div class="flex items-start justify-between gap-3 border-b border-zinc-200 bg-white px-4 py-4 dark:border-white/10 dark:bg-zinc-900/80">
          <div class="min-w-0">
            <div class="text-[11px] font-bold uppercase tracking-wider text-emerald-700 dark:text-emerald-300">Image details</div>
            <h3 class="mt-1 break-all font-mono text-sm font-bold text-zinc-900 dark:text-zinc-100">{{ selectedReference || "Inspecting image" }}</h3>
          </div>
          <button
            type="button"
            class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-zinc-200 bg-white text-zinc-500 hover:bg-zinc-50 hover:text-zinc-800 dark:border-white/10 dark:bg-white/[0.05] dark:text-zinc-400 dark:hover:bg-white/[0.09] dark:hover:text-white"
            aria-label="Close image details"
            title="Close image details"
            @click="closeDetails"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.25" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="m6 6 12 12M18 6 6 18" />
            </svg>
          </button>
        </div>

        <div class="max-h-[calc(100vh-10rem)] overflow-y-auto p-4">
          <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
            <label class="grid min-w-0 flex-1 gap-1.5 text-xs font-bold text-zinc-600 dark:text-zinc-300">
              <span>Inspect platform</span>
              <input
                v-model="inspectPlatform"
                list="image-lookup-platforms"
                type="text"
                autocomplete="off"
                spellcheck="false"
                :disabled="inspectLoading"
                class="h-10 rounded-xl border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-900 outline-none focus:border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 dark:border-white/10 dark:bg-zinc-900 dark:text-white"
              />
              <datalist id="image-lookup-platforms">
                <option v-for="option in inspectPlatformOptions" :key="option" :value="option"></option>
              </datalist>
            </label>
            <button
              type="button"
              :disabled="inspectLoading || !selectedReference"
              class="inline-flex h-10 items-center justify-center gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-3.5 text-xs font-bold text-emerald-800 hover:bg-emerald-100 disabled:opacity-55 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200 dark:hover:bg-emerald-500/15"
              @click="inspectReference(selectedReference)"
            >
              <span v-if="inspectLoading" class="spinner !h-3.5 !w-3.5 !border-[1.5px]"></span>
              Refresh details
            </button>
          </div>

          <div v-if="inspectLoading" class="py-12 text-center" role="status">
            <span class="spinner text-emerald-500"></span>
            <div class="mt-3 text-sm font-semibold text-zinc-600 dark:text-zinc-300">Reading manifest and image configuration…</div>
          </div>

          <div v-else-if="inspectError" role="alert" class="mt-4 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200">
            <div class="font-bold">Inspection failed</div>
            <div class="mt-1 whitespace-pre-wrap break-words">{{ inspectError }}</div>
          </div>

          <div v-else-if="inspection" class="mt-4 grid gap-4">
            <section class="rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-zinc-900/70">
              <div class="flex items-center justify-between gap-3">
                <h4 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">Overview</h4>
                <button type="button" class="text-xs font-bold text-emerald-700 hover:underline dark:text-emerald-300" @click="copyText(inspection.reference || selectedReference, 'Image reference copied')">Copy reference</button>
              </div>
              <dl class="mt-3 grid gap-3 sm:grid-cols-2">
                <div
                  v-for="item in detailOverview"
                  :key="item.label"
                  class="min-w-0 rounded-lg px-3 py-2.5"
                  :class="item.accent
                    ? 'border border-emerald-200 bg-emerald-50 dark:border-emerald-500/25 dark:bg-emerald-500/10'
                    : 'bg-zinc-50 dark:bg-white/[0.035]'"
                >
                  <dt
                    class="text-[10px] font-bold uppercase tracking-wide"
                    :class="item.accent ? 'text-emerald-700 dark:text-emerald-300' : 'text-zinc-500 dark:text-zinc-500'"
                  >{{ item.label }}</dt>
                  <dd
                    class="mt-1 break-all text-xs font-semibold"
                    :class="[
                      item.mono ? 'font-mono' : '',
                      item.accent ? 'text-emerald-900 dark:text-emerald-100' : 'text-zinc-800 dark:text-zinc-200',
                    ]"
                    :title="item.title || item.value"
                  >{{ item.value }}</dd>
                </div>
              </dl>
            </section>

            <section v-if="platforms.length" class="rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-zinc-900/70">
              <h4 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">Platforms <span class="ml-1 text-xs font-semibold text-zinc-400">{{ platforms.length }}</span></h4>
              <div class="mt-3 flex flex-wrap gap-2">
                <button
                  v-for="(item, index) in platforms"
                  :key="`${platformLabel(item)}-${index}`"
                  type="button"
                  :disabled="inspectLoading || !platformReference(item)"
                  class="rounded-lg border border-sky-200 bg-sky-50 px-2.5 py-1.5 font-mono text-xs font-semibold text-sky-800 hover:bg-sky-100 disabled:cursor-default dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-200 dark:hover:bg-sky-500/15"
                  :aria-pressed="inspectPlatform === platformReference(item) ? 'true' : 'false'"
                  :title="`Inspect ${platformLabel(item)}`"
                  @click="inspectPlatformImage(item)"
                >
                  {{ platformLabel(item) }}
                </button>
              </div>
            </section>

            <section v-if="warnings.length" class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-amber-900 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-200">
              <h4 class="text-sm font-bold">Warnings</h4>
              <ul class="mt-2 list-disc space-y-1 pl-5 text-xs leading-5">
                <li v-for="(warning, index) in warnings" :key="`${warning}-${index}`">{{ warning }}</li>
              </ul>
            </section>

            <section v-if="hasConfiguration" class="rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-zinc-900/70">
              <h4 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">Configuration</h4>
              <dl v-if="configurationSummary.length" class="mt-3 grid gap-2 sm:grid-cols-2">
                <div v-for="item in configurationSummary" :key="item.label" class="rounded-lg bg-zinc-50 px-3 py-2 dark:bg-white/[0.035]">
                  <dt class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">{{ item.label }}</dt>
                  <dd class="mt-1 break-all font-mono text-xs text-zinc-800 dark:text-zinc-200">{{ item.value }}</dd>
                </div>
              </dl>

              <div v-if="configEntrypoint || configCommand" class="mt-3 grid gap-3">
                <div v-if="configEntrypoint">
                  <div class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">Entrypoint</div>
                  <code class="mt-1 block break-all rounded-lg bg-zinc-950 px-3 py-2 text-xs leading-5 text-emerald-300">{{ configEntrypoint }}</code>
                </div>
                <div v-if="configCommand">
                  <div class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">Command</div>
                  <code class="mt-1 block break-all rounded-lg bg-zinc-950 px-3 py-2 text-xs leading-5 text-sky-300">{{ configCommand }}</code>
                </div>
              </div>

              <details v-if="configLabels.length" class="mt-3 rounded-lg border border-zinc-200 bg-zinc-50 open:bg-white dark:border-white/10 dark:bg-white/[0.025] dark:open:bg-white/[0.04]">
                <summary class="cursor-pointer px-3 py-2.5 text-xs font-bold text-zinc-700 dark:text-zinc-300">Labels ({{ configLabels.length }})</summary>
                <dl class="border-t border-zinc-200 px-3 py-2 dark:border-white/10">
                  <div v-for="item in configLabels" :key="item.key" class="grid gap-1 border-b border-zinc-200/60 py-2 last:border-0 dark:border-white/5">
                    <dt class="break-all font-mono text-[11px] font-bold text-zinc-600 dark:text-zinc-400">{{ item.key }}</dt>
                    <dd class="break-all text-xs leading-5 text-zinc-800 dark:text-zinc-200">{{ item.value }}</dd>
                  </div>
                </dl>
              </details>

              <details v-if="configEnvironment.length" class="mt-3 rounded-lg border border-zinc-200 bg-zinc-50 open:bg-white dark:border-white/10 dark:bg-white/[0.025] dark:open:bg-white/[0.04]">
                <summary class="cursor-pointer px-3 py-2.5 text-xs font-bold text-zinc-700 dark:text-zinc-300">Environment ({{ configEnvironment.length }})</summary>
                <div class="border-t border-zinc-200 px-3 py-2 dark:border-white/10">
                  <code v-for="(item, index) in configEnvironment" :key="`${item}-${index}`" class="block break-all py-1 text-[11px] leading-5 text-zinc-700 dark:text-zinc-300">{{ item }}</code>
                </div>
              </details>
            </section>

            <details v-if="configHistory.length" class="group overflow-hidden rounded-xl border border-zinc-200 bg-white dark:border-white/10 dark:bg-zinc-900/70">
              <summary class="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3.5 marker:hidden hover:bg-zinc-50 dark:hover:bg-white/[0.025] [&::-webkit-details-marker]:hidden">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <h4 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">Build history</h4>
                    <span class="rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-[10px] font-bold text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300">{{ configHistoryTotal }} steps</span>
                    <span v-if="configHistoryEmptyLayers" class="rounded-full border border-violet-200 bg-violet-50 px-2 py-0.5 text-[10px] font-bold text-violet-700 dark:border-violet-500/25 dark:bg-violet-500/10 dark:text-violet-300">{{ configHistoryEmptyLayers }} metadata-only</span>
                  </div>
                  <p class="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">Commands recorded in the selected image configuration</p>
                </div>
                <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 shrink-0 text-zinc-400 transition-transform group-open:rotate-180" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
                  <path stroke-linecap="round" stroke-linejoin="round" d="m6 9 6 6 6-6" />
                </svg>
              </summary>

              <div class="border-t border-zinc-200 dark:border-white/10">
                <div v-if="configHistoryTruncated" class="border-b border-amber-200 bg-amber-50 px-4 py-2.5 text-xs font-semibold leading-5 text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">
                  Showing the latest {{ configHistory.length }} of {{ configHistoryTotal }} recorded steps.
                </div>
                <ol class="max-h-[30rem] space-y-0 overflow-y-auto px-4 py-2" aria-label="Container image build history">
                  <li
                    v-for="item in configHistory"
                    :key="`${item.step}-${item.created}-${item.createdBy}`"
                    class="relative grid min-w-0 grid-cols-[2rem_minmax(0,1fr)] gap-3 border-b border-zinc-200/70 py-3 last:border-0 dark:border-white/[0.07]"
                  >
                    <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-zinc-100 text-[11px] font-bold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300" :aria-label="`Build step ${item.step}`">{{ item.step }}</span>
                    <div class="min-w-0">
                      <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <time v-if="item.created" :datetime="item.created" class="text-[10px] font-bold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">{{ formatDate(item.created) }}</time>
                        <span v-else class="text-[10px] font-bold uppercase tracking-wide text-zinc-400">Time unavailable</span>
                        <span v-if="item.emptyLayer" class="rounded-full border border-violet-200 bg-violet-50 px-2 py-0.5 text-[9px] font-bold uppercase tracking-wide text-violet-700 dark:border-violet-500/25 dark:bg-violet-500/10 dark:text-violet-300">Empty layer</span>
                      </div>
                      <code v-if="item.createdBy" class="mt-1.5 block whitespace-pre-wrap break-all rounded-lg bg-zinc-950 px-3 py-2 font-mono text-[11px] leading-5 text-emerald-300">{{ item.createdBy }}</code>
                      <p v-else class="mt-1.5 text-xs italic leading-5 text-zinc-400">No build command recorded.</p>
                      <p v-if="item.comment" class="mt-2 whitespace-pre-wrap break-words text-xs leading-5 text-zinc-600 dark:text-zinc-300">
                        <span class="font-bold text-zinc-700 dark:text-zinc-200">Comment:</span> {{ item.comment }}
                      </p>
                    </div>
                  </li>
                </ol>
              </div>
            </details>

            <section v-if="layers.length" class="rounded-xl border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-zinc-900/70">
              <div class="flex items-center justify-between gap-3">
                <h4 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">Layers <span class="ml-1 text-xs font-semibold text-zinc-400">{{ layers.length }}</span></h4>
                <span class="text-xs font-bold text-zinc-500 dark:text-zinc-400">{{ layerTotalSize }}</span>
              </div>
              <div class="mt-3 grid gap-2">
                <div v-for="(layer, index) in layers" :key="layer.digest || index" class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 rounded-lg bg-zinc-50 px-3 py-2 dark:bg-white/[0.035]">
                  <span class="flex h-6 w-6 items-center justify-center rounded-md bg-zinc-200 text-[10px] font-bold text-zinc-600 dark:bg-white/[0.08] dark:text-zinc-300">{{ index + 1 }}</span>
                  <code class="truncate text-[11px] text-zinc-600 dark:text-zinc-300" :title="layer.digest || layer.mediaType || ''">{{ shortDigest(layer.digest) || layer.mediaType || "layer" }}</code>
                  <span class="text-[11px] font-bold text-zinc-500">{{ formatBytes(layer.size) }}</span>
                </div>
              </div>
            </section>

            <section v-if="buildYamlVisible" class="overflow-hidden rounded-xl border border-zinc-200 bg-white dark:border-white/10 dark:bg-zinc-900/70">
              <div class="flex flex-col gap-3 border-b border-zinc-200 px-4 py-3 dark:border-white/10 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div class="flex flex-wrap items-center gap-2">
                    <h4 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">build.yaml</h4>
                    <span class="rounded-full border px-2 py-0.5 text-[10px] font-bold" :class="buildYamlStatusClass">{{ buildYamlStatus }}</span>
                    <span class="rounded-full border px-2 py-0.5 text-[10px] font-bold" :class="buildYamlOriginClass">{{ buildYamlOriginLabel }}</span>
                  </div>
                  <div v-if="usingSourceBuildYaml || buildYaml.path" class="mt-1 break-all font-mono text-[10px] text-zinc-500">{{ usingSourceBuildYaml ? sourceBuildPath : buildYaml.path }}</div>
                </div>
                <div v-if="buildYaml.found" class="inline-flex rounded-lg border border-zinc-200 bg-zinc-50 p-0.5 dark:border-white/10 dark:bg-white/[0.04]" role="group" aria-label="build.yaml view">
                  <button type="button" class="rounded-md px-2.5 py-1 text-[11px] font-bold" :class="buildYamlMode === 'structured' ? 'bg-white text-zinc-900 shadow-sm dark:bg-white/[0.1] dark:text-white' : 'text-zinc-500 dark:text-zinc-400'" :aria-pressed="buildYamlMode === 'structured' ? 'true' : 'false'" @click="buildYamlMode = 'structured'">Structured</button>
                  <button type="button" class="rounded-md px-2.5 py-1 text-[11px] font-bold" :class="buildYamlMode === 'raw' ? 'bg-white text-zinc-900 shadow-sm dark:bg-white/[0.1] dark:text-white' : 'text-zinc-500 dark:text-zinc-400'" :aria-pressed="buildYamlMode === 'raw' ? 'true' : 'false'" @click="buildYamlMode = 'raw'">Raw</button>
                </div>
              </div>

              <div v-if="usingSourceBuildYaml" class="border-b border-sky-200 bg-sky-50/70 px-4 py-3 text-xs leading-5 text-sky-900 dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-100">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div class="font-bold">{{ sourceBuildHeading }}</div>
                  <a
                    v-if="sourceBuildLink"
                    :href="sourceBuildLink"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="font-bold text-sky-700 hover:underline dark:text-sky-300"
                  >Open build.yaml ↗</a>
                </div>
                <p class="mt-1 text-sky-800 dark:text-sky-200">This is declared source provenance. It is not proof that the file is embedded in the container image.</p>
                <dl class="mt-3 grid gap-2 sm:grid-cols-2">
                  <div class="min-w-0 rounded-lg border border-sky-200/80 bg-white/65 px-3 py-2 dark:border-sky-500/20 dark:bg-black/10">
                    <dt class="text-[9px] font-bold uppercase tracking-wide text-sky-600 dark:text-sky-400">Repository</dt>
                    <dd class="mt-1 break-all font-mono text-[11px]">{{ sourceBuildRepository }}</dd>
                  </div>
                  <div class="min-w-0 rounded-lg border border-sky-200/80 bg-white/65 px-3 py-2 dark:border-sky-500/20 dark:bg-black/10">
                    <dt class="text-[9px] font-bold uppercase tracking-wide text-sky-600 dark:text-sky-400">Revision</dt>
                    <dd class="mt-1 break-all font-mono text-[11px]" :title="sourceBuildRevision">{{ sourceBuildRevision }}</dd>
                  </div>
                  <div class="min-w-0 rounded-lg border border-sky-200/80 bg-white/65 px-3 py-2 dark:border-sky-500/20 dark:bg-black/10 sm:col-span-2">
                    <dt class="text-[9px] font-bold uppercase tracking-wide text-sky-600 dark:text-sky-400">Path</dt>
                    <dd class="mt-1 break-all font-mono text-[11px]">{{ sourceBuildPath }}</dd>
                  </div>
                </dl>
              </div>
              <template v-else>
                <div v-if="buildYaml.skipped" class="bg-amber-50 px-4 py-3 text-xs leading-5 text-amber-800 dark:bg-amber-500/10 dark:text-amber-200">
                  <div class="font-bold">Safe scan incomplete</div>
                  <div class="mt-1">{{ buildYamlSkippedLabel }}</div>
                </div>
                <div v-else-if="buildYaml.error" class="bg-rose-50 px-4 py-3 text-xs leading-5 text-rose-800 dark:bg-rose-500/10 dark:text-rose-200">{{ buildYaml.error }}</div>
                <div v-else-if="!buildYaml.found" class="px-4 py-4 text-xs leading-5 text-zinc-500 dark:text-zinc-400">No build.yaml was found in the eligible layers that were safely scanned.</div>
              </template>

              <div
                v-if="showDeclaredSourceFetch"
                class="border-t border-sky-200 bg-gradient-to-br from-sky-50 to-white px-4 py-4 dark:border-sky-500/20 dark:from-sky-500/10 dark:to-transparent"
                :aria-busy="sourceBuildLoading ? 'true' : 'false'"
              >
                <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <h5 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">Fetch from declared source</h5>
                      <span class="rounded-full border border-sky-200 bg-sky-50 px-2 py-0.5 text-[9px] font-bold uppercase tracking-wide text-sky-700 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-300">Exact revision</span>
                    </div>
                    <p id="declared-source-fetch-description" class="mt-1 max-w-2xl text-xs leading-5 text-zinc-600 dark:text-zinc-300">
                      The image labels identify an exact GitHub repository and commit. Fetching uses the configured GitHub CLI login on this machine; no credential is entered or displayed here. Source retrieval does not prove that build.yaml is embedded in the image.
                    </p>
                    <dl class="mt-3 grid gap-x-4 gap-y-2 text-[11px] sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                      <div class="min-w-0">
                        <dt class="font-bold uppercase tracking-wide text-zinc-400">Repository</dt>
                        <dd class="mt-0.5 break-all font-mono text-zinc-700 dark:text-zinc-200">{{ declaredSource.repositoryUrl }}</dd>
                      </div>
                      <div class="min-w-0">
                        <dt class="font-bold uppercase tracking-wide text-zinc-400">Revision</dt>
                        <dd class="mt-0.5 break-all font-mono text-zinc-700 dark:text-zinc-200" :title="declaredSource.revision">{{ declaredSource.revision }}</dd>
                      </div>
                    </dl>
                  </div>
                  <div class="flex shrink-0 flex-wrap gap-2">
                    <button
                      type="button"
                      :disabled="sourceBuildLoading"
                      aria-describedby="declared-source-fetch-description"
                      class="inline-flex h-10 items-center justify-center gap-2 rounded-xl bg-sky-600 px-4 text-xs font-bold text-white shadow-sm shadow-sky-600/15 hover:bg-sky-700 disabled:cursor-wait disabled:opacity-60"
                      @click="fetchSourceBuildYaml"
                    >
                      <span v-if="sourceBuildLoading" class="spinner !h-3.5 !w-3.5 !border-[1.5px]" aria-hidden="true"></span>
                      {{ sourceBuildLoading ? "Fetching…" : "Fetch from declared source" }}
                    </button>
                    <button
                      v-if="sourceBuildLoading"
                      type="button"
                      class="inline-flex h-10 items-center justify-center rounded-xl border border-zinc-200 bg-white px-3.5 text-xs font-bold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.05] dark:text-zinc-200 dark:hover:bg-white/[0.08]"
                      @click="cancelSourceBuildFetch"
                    >Cancel</button>
                  </div>
                </div>
                <div v-if="sourceBuildLoading" class="mt-3 text-xs font-semibold text-sky-700 dark:text-sky-300" role="status">Fetching build.yaml from the immutable declared revision…</div>
                <div v-else-if="sourceBuildCancelled" class="mt-3 text-xs font-semibold text-zinc-500 dark:text-zinc-400" role="status">Source fetch cancelled. Nothing was changed.</div>
                <div v-else-if="sourceBuildError" class="mt-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs leading-5 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200" role="alert">
                  <div class="font-bold">Declared source fetch failed</div>
                  <div class="mt-0.5 whitespace-pre-wrap break-words">{{ sourceBuildError }}</div>
                </div>
              </div>

              <template v-if="buildYaml.found">
                <div v-if="buildYamlMode === 'structured'" class="max-h-[34rem] overflow-auto p-3">
                  <dl v-if="structuredBuildEntries.length" class="grid gap-1.5">
                    <div v-for="entry in structuredBuildEntries" :key="entry.path" class="grid min-w-0 gap-1 rounded-lg border border-zinc-200/70 bg-zinc-50 px-3 py-2 dark:border-white/10 dark:bg-white/[0.025]">
                      <dt class="break-all font-mono text-[10px] font-bold text-emerald-700 dark:text-emerald-300">{{ entry.path }}</dt>
                      <dd class="whitespace-pre-wrap break-all font-mono text-[11px] leading-5" :class="structuredValueClass(entry.value)">{{ formatStructuredValue(entry.value) }}</dd>
                    </div>
                  </dl>
                  <div v-else class="px-2 py-5 text-center text-xs text-zinc-500">Structured build data is empty.</div>
                  <div v-if="structuredBuildTruncated" class="mt-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-semibold text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">Showing the first 250 structured values. Use Raw for the complete file.</div>
                </div>
                <div v-else class="relative">
                  <button type="button" class="absolute right-3 top-3 rounded-lg border border-white/10 bg-white/10 px-2.5 py-1 text-[11px] font-bold text-zinc-200 hover:bg-white/15" @click="copyText(rawBuildYaml, 'Raw build.yaml copied')">Copy raw</button>
                  <pre class="max-h-[34rem] overflow-auto whitespace-pre-wrap break-words bg-zinc-950 p-4 pr-20 font-mono text-[11px] leading-5 text-zinc-200">{{ rawBuildYaml || "No raw build.yaml content returned." }}</pre>
                </div>
              </template>
            </section>
          </div>
        </div>
      </aside>
    </div>

    <div
      v-if="copyNotice"
      class="pointer-events-none fixed bottom-5 right-5 z-[9999] max-w-sm rounded-xl border border-emerald-200 bg-white px-4 py-3 text-sm font-bold text-emerald-800 shadow-xl dark:border-emerald-500/25 dark:bg-zinc-900 dark:text-emerald-200"
      role="status"
    >
      {{ copyNotice }}
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { writeTextToClipboard } from "./clipboard.js";
import { apiFetch } from "./store.js";

const registryOptions = [
  { value: "all", label: "All known registries" },
  { value: "stgregistry.suse.com", label: "SUSE staging · Prime heads" },
  { value: "registry.rancher.com", label: "Rancher Prime · released" },
  { value: "registry.suse.com", label: "SUSE registry" },
  { value: "docker.io", label: "Docker Hub" },
];

const imageFamilyOptions = [
  { value: "all", label: "All Rancher images" },
  { value: "rancher/rancher", label: "Rancher server" },
  { value: "rancher/rancher-agent", label: "Rancher agent" },
  { value: "rancher/rancher-webhook", label: "Rancher webhook" },
  { value: "custom", label: "Custom repository" },
];

const quickFilters = [
  { id: "prime-head", label: "Prime heads" },
  { id: "head", label: "Head" },
  { id: "devel", label: "Devel" },
  { id: "alpha", label: "Alpha" },
  { id: "rcs", label: "RCS" },
  { id: "rc", label: "RC" },
  { id: "stable", label: "Stable" },
  { id: "all", label: "All" },
];

const registry = ref("all");
const imageFamily = ref("all");
const customRepository = ref("");
const query = ref("head");
const quickFilter = ref("head");
const searched = ref(false);
const searchLoading = ref(false);
const searchError = ref("");
const searchResponse = ref(null);
const primeHeadFilter = ref("all");
const headKindFilter = ref("all");
const architectureFilter = ref("all");
const versionLineFilter = ref("");
const commitFilter = ref("");
const pairStatusFilter = ref("all");
const recentOnly = ref(false);
const resultSort = ref("fast");
const resultSortTouched = ref(false);

const selectedReference = ref("");
const inspectPlatform = ref("linux/amd64");
const inspectLoading = ref(false);
const inspectError = ref("");
const inspection = ref(null);
const buildYamlMode = ref("structured");
const sourceBuildYaml = ref(null);
const sourceBuildLoading = ref(false);
const sourceBuildError = ref("");
const sourceBuildCancelled = ref(false);
const copyNotice = ref("");

let searchController = null;
let inspectController = null;
let sourceBuildController = null;
let copyNoticeTimer = null;

watch([
  registry,
  imageFamily,
  customRepository,
  query,
  primeHeadFilter,
  headKindFilter,
  architectureFilter,
  versionLineFilter,
  commitFilter,
  pairStatusFilter,
  recentOnly,
], () => {
  if (!searchLoading.value) searchError.value = "";
});

watch(primeHeadFilter, value => {
  if (value === "exclude") headKindFilter.value = "all";
  if (value !== "only" && quickFilter.value === "prime-head") quickFilter.value = "head";
  if (value === "only" && imageFamily.value !== "custom") {
    registry.value = "stgregistry.suse.com";
    if (imageFamily.value === "rancher/rancher-webhook") imageFamily.value = "all";
  }
});

watch(headKindFilter, value => {
  if (value !== "all") primeHeadFilter.value = "only";
});

watch([commitFilter, pairStatusFilter], ([commit, pairStatus]) => {
  if (commit.trim() || pairStatus !== "all") primeHeadFilter.value = "only";
});

const searchRepository = computed(() => (
  imageFamily.value === "custom" ? customRepository.value.trim() : imageFamily.value
));
const customHasExplicitRegistry = computed(() => {
  if (imageFamily.value !== "custom") return false;
  const normalized = normalizeCustomRepository(customRepository.value);
  const first = normalized.split("/", 1)[0].toLowerCase();
  return normalized.includes("/") && (first.includes(".") || first.includes(":") || first === "localhost");
});

const channelQueryIDs = new Set(["head", "devel", "alpha", "rcs", "rc", "stable", "all"]);
const quickFilterIDs = new Set(quickFilters.map(filter => filter.id));

const syncQuickFilterFromQuery = value => {
  const normalized = String(value || "").trim().toLowerCase();
  if (normalized === "prime-head" || normalized === "primehead") {
    quickFilter.value = "prime-head";
    primeHeadFilter.value = "only";
    registry.value = "stgregistry.suse.com";
    if (imageFamily.value === "rancher/rancher-webhook") imageFamily.value = "all";
    return;
  }
  quickFilter.value = normalized && channelQueryIDs.has(normalized) ? normalized : "all";
};

const applyQuickFilter = filter => {
  quickFilter.value = quickFilterIDs.has(filter) ? filter : "all";
  if (quickFilter.value === "prime-head") {
    query.value = "head";
    primeHeadFilter.value = "only";
    headKindFilter.value = "all";
    registry.value = "stgregistry.suse.com";
    if (imageFamily.value === "rancher/rancher-webhook") imageFamily.value = "all";
  } else {
    query.value = quickFilter.value === "all" ? "" : quickFilter.value;
  }
  if (searched.value && !searchLoading.value) {
    void searchImages();
  }
};

const rerunCompletedSearch = () => {
  if (searched.value && !searchLoading.value && searchResponse.value) void searchImages();
};

const setBaseTagsOnly = checked => {
  architectureFilter.value = checked ? "multi" : "all";
  rerunCompletedSearch();
};

const handleRecentOnlyChange = checked => {
  recentOnly.value = checked === true;
  rerunCompletedSearch();
};

const resetAdvancedFilters = () => {
  primeHeadFilter.value = "all";
  headKindFilter.value = "all";
  architectureFilter.value = "all";
  versionLineFilter.value = "";
  commitFilter.value = "";
  pairStatusFilter.value = "all";
  recentOnly.value = false;
  if (quickFilter.value === "prime-head") {
    quickFilter.value = "head";
    query.value = "head";
  }
};

const activeAdvancedFilterCount = computed(() => [
  primeHeadFilter.value !== "all",
  headKindFilter.value !== "all",
  architectureFilter.value !== "all",
  Boolean(versionLineFilter.value.trim()),
  Boolean(commitFilter.value.trim()),
  pairStatusFilter.value !== "all",
  recentOnly.value,
].filter(Boolean).length);

const stripReferenceScheme = value => String(value || "")
  .trim()
  .replace(/^(?:docker|oci):\/\//i, "")
  .replace(/^https:\/\//i, "");

const parseFullReferenceInput = value => {
  const reference = stripReferenceScheme(value);
  if (!reference || /\s/.test(reference)) return null;
  const lastSlash = reference.lastIndexOf("/");
  if (lastSlash <= 0) return null;
  const digestSeparator = reference.lastIndexOf("@");
  const tagSeparator = reference.lastIndexOf(":");
  const separator = digestSeparator > lastSlash ? digestSeparator : tagSeparator > lastSlash ? tagSeparator : -1;
  if (separator <= lastSlash || separator === reference.length - 1) return null;
  return {
    reference,
    selector: reference.slice(separator + 1),
    digest: reference[separator] === "@",
  };
};

const queryReferenceDetails = computed(() => parseFullReferenceInput(query.value));
const querySelector = computed(() => String(queryReferenceDetails.value?.selector || query.value || "").trim());
const primePatchSuggestion = computed(() => {
  if (queryReferenceDetails.value) return "";
  const queryMatch = querySelector.value.match(/^v?(\d+\.\d+\.\d+)$/i);
  const versionMatch = (quickFilter.value === "prime-head" || primeHeadFilter.value === "only")
    ? versionLineFilter.value.trim().match(/^v?(\d+\.\d+\.\d+)$/i)
    : null;
  const patch = queryMatch?.[1] || versionMatch?.[1] || "";
  return patch ? `v${patch}-head` : "";
});
const isPrimeMovingSelectorInput = computed(() => primeMovingTagPattern.test(querySelector.value));
const isImmutablePrimeHeadInput = computed(() => primeImmutableTagPattern.test(querySelector.value));
const selectedLookupBase = computed(() => {
  const repository = imageFamily.value === "custom"
    ? normalizeCustomRepository(customRepository.value)
    : imageFamily.value;
  if (!repository || repository === "all" || repository === "custom" || parseFullReferenceInput(repository)) return "";
  if (customHasExplicitRegistry.value) return repository;
  return registry.value !== "all" ? `${registry.value}/${repository.replace(/^\/+/, "")}` : "";
});
const directInspectReference = computed(() => {
  const details = queryReferenceDetails.value;
  if (isPrimeMovingSelectorInput.value) return "";
  if (details) return details.reference;
  const selector = querySelector.value;
  if (!selectedLookupBase.value || !selector || channelQueryIDs.has(selector.toLowerCase()) || ["prime-head", "primehead"].includes(selector.toLowerCase())) return "";
  if (/^sha256:[0-9a-f]{64}$/i.test(selector)) return `${selectedLookupBase.value}@${selector}`;
  return /^[0-9A-Za-z_][0-9A-Za-z_.-]{0,127}$/.test(selector) ? `${selectedLookupBase.value}:${selector}` : "";
});
const searchButtonLabel = computed(() => searchLoading.value
  ? "Searching"
  : primePatchSuggestion.value
    ? "Find Prime builds"
    : isPrimeMovingSelectorInput.value
      ? "Discover candidates"
      : "Search"
);
const lookupHintTitle = computed(() => primePatchSuggestion.value
  ? "Patch version detected."
  : isPrimeMovingSelectorInput.value
    ? "Moving Prime selector detected."
    : directInspectReference.value
      ? "Exact reference detected."
      : "Direct lookup supported."
);
const lookupHint = computed(() => {
  if (primePatchSuggestion.value) {
    return `Use ${primePatchSuggestion.value} to discover and rank its immutable staging server/agent pairs instead of scanning every head tag.`;
  }
  if (isPrimeMovingSelectorInput.value) {
    return "A patch alias often has no literal manifest. Discovery expands it to immutable staging tags and, when available, ranks verified server/agent pairs by pair completion time.";
  }
  if (directInspectReference.value) {
    return "Search looks up the exact repository tag or digest; Inspect reads that reference immediately without depending on a tag-list result.";
  }
  return "Enter a patch such as 2.15.1 for a one-click Prime lookup, or paste a full image:tag or image@sha256 reference to inspect it directly.";
});
const lookupHintClass = computed(() => primePatchSuggestion.value || isPrimeMovingSelectorInput.value
  ? "border-violet-200 bg-violet-50/70 text-violet-800 dark:border-violet-500/25 dark:bg-violet-500/10 dark:text-violet-200"
  : directInspectReference.value
    ? "border-emerald-200 bg-emerald-50/70 text-emerald-800 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200"
    : "border-zinc-200 bg-white text-zinc-600 dark:border-white/10 dark:bg-white/[0.025] dark:text-zinc-300"
);

const inspectExactInput = () => {
  if (!directInspectReference.value) return;
  const inferredPlatform = inspectPlatformFromReference(directInspectReference.value);
  if (inferredPlatform) inspectPlatform.value = inferredPlatform;
  void inspectReference(directInspectReference.value);
};

const findPrimePatchHeads = () => {
  if (!primePatchSuggestion.value) return;
  query.value = primePatchSuggestion.value;
  quickFilter.value = "prime-head";
  primeHeadFilter.value = "only";
  headKindFilter.value = "all";
  registry.value = "stgregistry.suse.com";
  if (imageFamily.value === "rancher/rancher-webhook") imageFamily.value = "all";
  void searchImages();
};

const submitImageSearch = () => {
  if (primePatchSuggestion.value) {
    findPrimePatchHeads();
    return;
  }
  void searchImages();
};

const groups = computed(() => (
  Array.isArray(searchResponse.value?.groups) ? searchResponse.value.groups : []
));

const architectureOptions = computed(() => {
  const architectures = new Set(["multi", "amd64", "arm64", "s390x", "ppc64le", "386", "arm"]);
  groups.value.forEach(group => {
    (Array.isArray(group?.tags) ? group.tags : []).forEach(tag => {
      const values = Array.isArray(tag?.architecture) ? tag.architecture : [tag?.architecture];
      values.flatMap(value => String(value || "").split(/[\s,]+/)).forEach(value => {
        const normalized = value.trim().toLowerCase();
        if (normalized) architectures.add(normalized);
      });
    });
  });
  return [
    { value: "all", label: "All architectures" },
    ...[...architectures].sort((left, right) => left.localeCompare(right)).map(value => ({
      value,
      label: value === "multi" ? "Unsuffixed / base tag" : value,
    })),
  ];
});

const quickFilterLabel = computed(() => (
  quickFilters.find(filter => filter.id === quickFilter.value)?.label || "All"
));

const optionalBoolean = value => typeof value === "boolean" ? value : null;
const normalizedHeadKind = value => {
  const normalized = String(value || "").trim().toLowerCase();
  if (["moving", "mutable", "alias", "selector"].includes(normalized)) return "moving";
  if (["immutable", "exact", "commit"].includes(normalized)) return "immutable";
  return "";
};

const normalizedImageRole = (value, repository = "") => {
  const normalized = String(value || "").trim().toLowerCase();
  if (["server", "rancher", "rancher-server"].includes(normalized)) return "server";
  if (["agent", "rancher-agent"].includes(normalized)) return "agent";
  if (["webhook", "rancher-webhook"].includes(normalized)) return "webhook";
  const path = String(repository || "").toLowerCase().replace(/\/+$/, "");
  if (path.endsWith("/rancher-agent")) return "agent";
  if (path.endsWith("/rancher-webhook")) return "webhook";
  if (path.endsWith("/rancher")) return "server";
  return normalized || "other";
};

const primeMovingTagPattern = /^v?(\d+)\.(\d+)\.(\d+)-head$/i;
const primeImmutableTagPattern = /^v?(\d+)\.(\d+)\.(\d+)-([0-9a-f]{7,40})-head$/i;

const deriveTagMetadata = (tag, group = {}) => {
  const source = tag && typeof tag === "object" ? tag : {};
  const baseName = String(source.baseTag || source.name || "").trim();
  const immutableMatch = baseName.match(primeImmutableTagPattern);
  const movingMatch = baseName.match(primeMovingTagPattern);
  const syntaxIsPrime = Boolean(immutableMatch || movingMatch);
  const explicitPrime = optionalBoolean(source.isPrimeHead);
  const isPrimeHead = explicitPrime == null ? syntaxIsPrime : explicitPrime;
  const inferredKind = movingMatch ? "moving" : immutableMatch ? "immutable" : "";
  const headKind = normalizedHeadKind(source.headKind) || inferredKind;
  const version = String(source.version || (immutableMatch || movingMatch
    ? `${(immutableMatch || movingMatch)[1]}.${(immutableMatch || movingMatch)[2]}.${(immutableMatch || movingMatch)[3]}`
    : "")).replace(/^v/i, "");
  const versionParts = version.split(".");
  const versionLine = String(source.versionLine || (versionParts.length >= 2 ? `${versionParts[0]}.${versionParts[1]}` : "")).replace(/^v/i, "");
  const commit = String(source.commit || immutableMatch?.[4] || "").trim().toLowerCase();
  const pairStatus = String(source.pairStatus || "").trim().toLowerCase()
    || (source.companionVerified === true || source.pairComplete === true ? "verified" : isPrimeHead ? "unverified" : "");
  const rank = Number(source.resolvedRank);
  const imageRole = normalizedImageRole(source.imageRole || group.imageRole, group.repository);
  const primeSourceValid = optionalBoolean(source.primeSource);
  const sourceRepository = typeof source.primeSource === "string"
    ? source.primeSource
    : typeof source.source === "string"
      ? source.source
      : typeof source.sourceUrl === "string"
        ? source.sourceUrl
        : "";

  return {
    ...source,
    isPrimeHead,
    headKind,
    mutable: optionalBoolean(source.mutable) ?? headKind === "moving",
    version,
    versionLine,
    commit,
    selector: String(source.selector || (version ? `v${version}-head` : "")),
    imageRole,
    companionReference: String(source.companionReference || ""),
    companionVerified: source.companionVerified === true,
    pairStatus,
    pairComplete: source.pairComplete === true || pairStatus === "verified",
    pairCompletedAt: source.pairCompletedAt || "",
    pairError: String(source.pairError || ""),
    provenanceValid: optionalBoolean(source.provenanceValid) ?? optionalBoolean(source.consistent),
    primeSourceValid,
    sourceRepository,
    revision: String(source.revision || ""),
    canonicalMatchesRequest: optionalBoolean(source.canonicalMatchesRequest),
    commitMatchesOss: optionalBoolean(source.commitMatchesOss),
    issues: Array.isArray(source.issues) ? source.issues.map(issue => String(issue)).filter(Boolean) : [],
    canonicalReference: String(source.canonicalReference || ""),
    ossRevision: String(source.ossRevision || ""),
    resolvedRank: Number.isFinite(rank) && rank > 0 ? rank : 0,
    artifact: source.artifact === true,
  };
};

const tagMatchesQuickFilter = tag => {
  if (quickFilter.value === "all") return true;
  if (quickFilter.value === "prime-head") return tag.isPrimeHead === true;
  const channel = String(tag?.channel || "").trim().toLowerCase();
  const name = String(tag?.name || "").trim().toLowerCase();
  const combined = `${channel} ${name}`;
  switch (quickFilter.value) {
  case "head":
    return channel === "head" || name === "head" || /(?:^|[-.])head(?:$|[-.])/.test(name);
  case "devel":
    return ["devel", "development", "dev", "alpha", "beta"].some(value => channel.includes(value)) || /(?:^|[-.])(devel|dev|alpha|beta)[-.]?\d*/.test(name);
  case "alpha":
    return channel.includes("alpha") || /(?:^|[-.])alpha[-.]?\d*/.test(name);
  case "rcs":
    return channel.includes("rcs") || /(?:^|[-.])rcs[-.]?\d*/.test(name);
  case "rc":
    return !combined.includes("rcs") && (channel === "rc" || channel.includes("release-candidate") || /(?:^|[-.])rc[-.]?\d*/.test(name));
  case "stable":
    return ["stable", "release", "ga"].includes(channel) || /^v?\d+\.\d+\.\d+(?:\+[0-9a-z.-]+)?$/i.test(name);
  default:
    return true;
  }
};

const tagArchitectures = tag => {
  const raw = Array.isArray(tag?.architecture) ? tag.architecture : [tag?.architecture];
  return raw
    .flatMap(value => String(value || "").split(/[\s,]+/))
    .map(value => value.trim().toLowerCase())
    .filter(Boolean);
};

const tagMatchesAdvancedFilters = tag => {
  if (primeHeadFilter.value === "only" && !tag.isPrimeHead) return false;
  if (primeHeadFilter.value === "exclude" && tag.isPrimeHead) return false;
  if (headKindFilter.value !== "all" && tag.headKind !== headKindFilter.value) return false;
  if (architectureFilter.value !== "all" && !tagArchitectures(tag).includes(architectureFilter.value)) return false;

  const requestedVersion = versionLineFilter.value.trim().toLowerCase().replace(/^v/, "");
  if (requestedVersion) {
    const searchable = [tag.version, tag.versionLine, tag.selector, tag.name, tag.baseTag]
      .map(value => String(value || "").toLowerCase().replace(/^v/, ""));
    if (!searchable.some(value => value === requestedVersion || value.startsWith(`${requestedVersion}-`) || value.startsWith(`${requestedVersion}.`))) return false;
  }

  const requestedCommit = commitFilter.value.trim().toLowerCase();
  if (requestedCommit) {
    const searchable = [tag.commit, tag.ossRevision, tag.name, tag.canonicalReference]
      .map(value => String(value || "").toLowerCase());
    if (!searchable.some(value => value.includes(requestedCommit))) return false;
  }

  if (pairStatusFilter.value === "verified" && !(tag.pairStatus === "verified" && tag.pairComplete)) return false;
  if (pairStatusFilter.value === "attention" && (!tag.isPrimeHead || (tag.pairStatus === "verified" && tag.pairComplete))) return false;
  return true;
};

const tagSortValue = tag => String(tag?.name || tag?.reference || "");
const compareTagText = (left, right) => tagSortValue(left).localeCompare(tagSortValue(right), undefined, {
  numeric: true,
  sensitivity: "base",
});
const versionTuple = tag => String(tag?.version || tag?.versionLine || "")
  .replace(/^v/i, "")
  .split(".")
  .map(value => Number(value));
const compareVersion = (left, right, direction) => {
  const leftVersion = versionTuple(left);
  const rightVersion = versionTuple(right);
  const leftValid = leftVersion.length >= 2 && leftVersion.every(Number.isFinite);
  const rightValid = rightVersion.length >= 2 && rightVersion.every(Number.isFinite);
  if (leftValid !== rightValid) return leftValid ? -1 : 1;
  if (!leftValid) return compareTagText(left, right) * direction;
  for (let index = 0; index < Math.max(leftVersion.length, rightVersion.length); index += 1) {
    const compared = (leftVersion[index] || 0) - (rightVersion[index] || 0);
    if (compared) return compared * direction;
  }
  return compareTagText(left, right) * direction;
};
const sortableTime = value => {
  const timestamp = new Date(value || "").getTime();
  return Number.isFinite(timestamp) ? timestamp : null;
};
const compareOptionalTime = (left, right, direction) => {
  const leftTime = sortableTime(left);
  const rightTime = sortableTime(right);
  if ((leftTime == null) !== (rightTime == null)) return leftTime == null ? 1 : -1;
  if (leftTime == null) return 0;
  return (leftTime - rightTime) * direction;
};

const sortVisibleTags = (tags, group) => {
  const visible = tags
    .map(tag => deriveTagMetadata(tag, group))
    .filter(tag => tagMatchesQuickFilter(tag) && tagMatchesAdvancedFilters(tag));
  if (resultSort.value === "fast") return visible;
  return visible
    .map((tag, index) => ({ tag, index }))
    .sort((left, right) => {
      let compared = 0;
      switch (resultSort.value) {
      case "pair-rank": {
        const leftRank = left.tag.resolvedRank || Number.POSITIVE_INFINITY;
        const rightRank = right.tag.resolvedRank || Number.POSITIVE_INFINITY;
        compared = leftRank - rightRank;
        if (!compared) compared = compareOptionalTime(left.tag.pairCompletedAt, right.tag.pairCompletedAt, -1);
        break;
      }
      case "uploaded-desc":
        compared = compareOptionalTime(left.tag.uploadedAt, right.tag.uploadedAt, -1);
        break;
      case "uploaded-asc":
        compared = compareOptionalTime(left.tag.uploadedAt, right.tag.uploadedAt, 1);
        break;
      case "version-desc":
        compared = compareVersion(left.tag, right.tag, -1);
        break;
      case "version-asc":
        compared = compareVersion(left.tag, right.tag, 1);
        break;
      case "tag-desc":
        compared = compareTagText(left.tag, right.tag) * -1;
        break;
      default:
        compared = compareTagText(left.tag, right.tag);
      }
      return compared === 0 ? left.index - right.index : compared;
    })
    .map(item => item.tag);
};

const displayGroups = computed(() => groups.value.map(group => {
  const tags = Array.isArray(group?.tags) ? group.tags : [];
  const decoratedTags = tags.map(tag => deriveTagMetadata(tag, group));
  const primeHeadCount = Number(group?.primeHeadCount);
  const movingPrimeHeadCount = Number(group?.movingPrimeHeadCount);
  const immutablePrimeHeadCount = Number(group?.immutablePrimeHeadCount);
  return {
    ...group,
    tags,
    imageRole: normalizedImageRole(group?.imageRole, group?.repository),
    companionRepository: String(group?.companionRepository || ""),
    primeHeadCount: Number.isFinite(primeHeadCount) ? primeHeadCount : decoratedTags.filter(tag => tag.isPrimeHead).length,
    movingPrimeHeadCount: Number.isFinite(movingPrimeHeadCount) ? movingPrimeHeadCount : decoratedTags.filter(tag => tag.isPrimeHead && tag.headKind === "moving").length,
    immutablePrimeHeadCount: Number.isFinite(immutablePrimeHeadCount) ? immutablePrimeHeadCount : decoratedTags.filter(tag => tag.isPrimeHead && tag.headKind === "immutable").length,
    visibleTags: sortVisibleTags(tags, group),
  };
}).filter(group => group.visibleTags.length || group.error || activeAdvancedFilterCount.value === 0));

const visibleTagCount = computed(() => displayGroups.value.reduce((total, group) => total + group.visibleTags.length, 0));
const totalMatched = computed(() => groups.value.reduce((total, group) => total + Number(group?.matched || 0), 0));
const totalScanned = computed(() => groups.value.reduce((total, group) => total + Number(group?.scanned || 0), 0));
const totalRecentExcluded = computed(() => groups.value.reduce((total, group) => total + Number(group?.recentExcludedCount || 0), 0));
const totalUnknownTimestamps = computed(() => groups.value.reduce((total, group) => total + Number(group?.unknownTimestampCount || 0), 0));
const failedGroupCount = computed(() => groups.value.filter(group => group?.error).length);
const visiblePrimeHeadCount = computed(() => displayGroups.value.reduce((total, group) => (
  total + group.visibleTags.filter(tag => tag.isPrimeHead).length
), 0));
const verifiedPairCount = computed(() => {
  const exactPairs = new Set();
  displayGroups.value.forEach(group => {
    group.visibleTags.forEach(tag => {
      if (!tag.isPrimeHead || tag.pairStatus !== "verified" || !tag.pairComplete) return;
      const registryHost = String(group.registry || "").trim().toLowerCase();
      const exactTag = String(tag.name || "").trim().toLowerCase();
      if (registryHost && exactTag) exactPairs.add(`${registryHost}:${exactTag}`);
    });
  });
  return exactPairs.size;
});

const resultSummary = computed(() => {
  const sourceCount = groups.value.length;
  const sourceLabel = `${sourceCount} source group${sourceCount === 1 ? "" : "s"}`;
  const queryLabel = quickFilter.value === "prime-head"
    ? "Prime heads"
    : String(searchResponse.value?.requestedQuery || searchResponse.value?.query || query.value || "").trim();
  return queryLabel
    ? `${totalMatched.value} matching tag${totalMatched.value === 1 ? "" : "s"} for “${queryLabel}” across ${sourceLabel}.`
    : `${totalMatched.value} tag${totalMatched.value === 1 ? "" : "s"} returned across ${sourceLabel}.`;
});

const scanSummary = computed(() => {
  const pieces = [`${totalScanned.value} tag${totalScanned.value === 1 ? "" : "s"} scanned`];
  if (recentOnly.value) pieces.push("complete date-evidence scan");
  else if (searchResponse.value?.scanMode === "bounded") pieces.push("fast bounded registry order");
  if (searchResponse.value?.scanMode === "complete") pieces.push("complete global ordering");
  if (quickFilter.value !== "all") pieces.push(`${quickFilterLabel.value} filter active`);
  if (activeAdvancedFilterCount.value) pieces.push(`${activeAdvancedFilterCount.value} detailed filter${activeAdvancedFilterCount.value === 1 ? "" : "s"}`);
  if (recentOnly.value) {
    const cutoff = String(searchResponse.value?.recentCutoff || "").trim();
    const cutoffDate = cutoff ? new Date(cutoff) : null;
    const cutoffLabel = cutoffDate && !Number.isNaN(cutoffDate.getTime()) ? cutoffDate.toLocaleString() : cutoff;
    pieces.push(cutoffLabel ? `30-day cutoff ${cutoffLabel}` : "last 30 days");
    if (totalRecentExcluded.value) pieces.push(`${totalRecentExcluded.value} older excluded`);
    if (totalUnknownTimestamps.value) pieces.push(`${totalUnknownTimestamps.value} undated excluded`);
  }
  if (searchResponse.value?.aliasFallbackExpanded) pieces.push("selector expanded with compatibility fallback");
  if (searchResponse.value?.legacyRequestFallback) pieces.push("legacy API compatibility mode");
  if (visiblePrimeHeadCount.value) pieces.push(`${visiblePrimeHeadCount.value} Prime-head candidate${visiblePrimeHeadCount.value === 1 ? "" : "s"}`);
  if (verifiedPairCount.value) pieces.push(`${verifiedPairCount.value} verified complete pair${verifiedPairCount.value === 1 ? "" : "s"}`);
  if (failedGroupCount.value) pieces.push(`${failedGroupCount.value} registry error${failedGroupCount.value === 1 ? "" : "s"}`);
  return pieces.join(" · ");
});

const searchedAtLabel = computed(() => {
  const value = searchResponse.value?.searchedAt;
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : `Searched ${date.toLocaleTimeString()}`;
});

const detailVisible = computed(() => Boolean(selectedReference.value || inspectLoading.value || inspectError.value || inspection.value));
const platforms = computed(() => Array.isArray(inspection.value?.platforms) ? inspection.value.platforms : []);
const platformReference = value => {
  if (!value || typeof value !== "object") return "";
  const os = String(value.os || value.OS || "").trim();
  const architecture = String(value.architecture || value.arch || "").trim();
  const variant = String(value.variant || "").trim();
  if (!os || !architecture) return "";
  return [os, architecture, variant].filter(Boolean).join("/");
};
const inspectPlatformOptions = computed(() => [...new Set([
  "linux/amd64",
  "linux/arm64",
  "linux/s390x",
  "linux/ppc64le",
  ...platforms.value.map(platformReference).filter(Boolean),
])]);
const warnings = computed(() => {
  const values = Array.isArray(inspection.value?.warnings) ? inspection.value.warnings.filter(Boolean) : [];
  const primeIssues = Array.isArray(inspection.value?.primeHead?.issues)
    ? inspection.value.primeHead.issues.filter(Boolean).map(issue => `Prime provenance: ${issue}`)
    : [];
  return [...values, ...primeIssues];
});
const imageConfig = computed(() => inspection.value?.config && typeof inspection.value.config === "object" ? inspection.value.config : {});
const layers = computed(() => Array.isArray(inspection.value?.layers) ? inspection.value.layers : []);
const embeddedBuildYaml = computed(() => inspection.value?.buildYaml && typeof inspection.value.buildYaml === "object" ? inspection.value.buildYaml : {});
const buildYaml = computed(() => sourceBuildYaml.value?.found ? sourceBuildYaml.value : embeddedBuildYaml.value);

const canonicalGitHubRepository = value => {
  value = String(value || "");
  if (!value) return "";
  const match = value.match(/^https:\/\/github\.com\/([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+)$/);
  if (!match) return "";
  const validComponent = component => (
    component.length > 0
    && component.length <= 100
    && component !== "."
    && component !== ".."
  );
  const owner = match[1];
  let repository = match[2];
  if (repository.endsWith(".git")) {
    repository = repository.slice(0, -4);
    if (!repository || repository.endsWith(".git")) return "";
  } else if (repository.length >= 4 && repository.slice(-4).toLowerCase() === ".git") {
    return "";
  }
  return validComponent(owner) && validComponent(repository)
    ? `https://github.com/${owner}/${repository}`
    : "";
};

const findConfigLabel = keys => {
  const labels = imageConfig.value.labels;
  if (!labels || typeof labels !== "object" || Array.isArray(labels)) return null;
  for (const key of keys) {
    const value = String(labels[key] || "");
    if (value) return { key, value };
  }
  return null;
};

const declaredSource = computed(() => {
  const source = findConfigLabel(["org.opencontainers.image.source"]);
  const revision = findConfigLabel(["org.opencontainers.image.revision"]);
  const repositoryUrl = canonicalGitHubRepository(source?.value);
  const exactRevision = String(revision?.value || "");
  if (!repositoryUrl || !/^[0-9a-f]{40}$/i.test(exactRevision)) return null;
  return {
    repositoryUrl,
    revision: exactRevision,
    path: "build.yaml",
    sourceLabel: source.key,
    revisionLabel: revision.key,
  };
});

const sourceBuildProvenance = computed(() => {
  const value = sourceBuildYaml.value?.provenance;
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
});
const sourceBuildRepository = computed(() => (
  canonicalGitHubRepository(sourceBuildProvenance.value.repositoryUrl) || declaredSource.value?.repositoryUrl || ""
));
const sourceBuildRevision = computed(() => {
  const revision = String(sourceBuildProvenance.value.revision || declaredSource.value?.revision || "").trim();
  return /^[0-9a-f]{40}$/i.test(revision) ? revision : "";
});
const sourceBuildPath = computed(() => {
  const path = String(sourceBuildProvenance.value.path || declaredSource.value?.path || "build.yaml")
    .trim()
    .replace(/^\/+/, "");
  if (!path || path.split("/").some(part => !part || part === "." || part === "..")) return "build.yaml";
  return path;
});
const sourceBuildLink = computed(() => {
  if (!sourceBuildRepository.value || !sourceBuildRevision.value || !sourceBuildPath.value) return "";
  const encodedPath = sourceBuildPath.value.split("/").map(part => encodeURIComponent(part)).join("/");
  return `${sourceBuildRepository.value}/blob/${encodeURIComponent(sourceBuildRevision.value)}/${encodedPath}`;
});
const sourceFetchAvailable = computed(() => Boolean(
  inspection.value
  && !embeddedBuildYaml.value.found
  && declaredSource.value
  && String(inspection.value.digest || "").trim()
));
const showDeclaredSourceFetch = computed(() => sourceFetchAvailable.value && !sourceBuildYaml.value?.found);
const usingSourceBuildYaml = computed(() => Boolean(sourceBuildYaml.value?.found));
const usingDeclaredOSSSource = computed(() => (
  usingSourceBuildYaml.value && sourceBuildYaml.value?.origin === "declared-oss-source"
));
const sourceBuildHeading = computed(() => usingDeclaredOSSSource.value
  ? "Fetched from the image-declared OSS revision"
  : "Fetched from the exact image-declared source revision"
);

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
const configHistoryEmptyLayers = computed(() => (
  Array.isArray(imageConfig.value.history)
    ? imageConfig.value.history.reduce((total, item) => total + (item?.emptyLayer ? 1 : 0), 0)
    : 0
));
const configHistoryTruncated = computed(() => configHistoryTotal.value > configHistory.value.length);

const inspectionPrimeHead = computed(() => {
  const raw = inspection.value?.primeHead;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  return deriveTagMetadata({
    ...raw,
    isPrimeHead: optionalBoolean(raw.isPrimeHead) ?? true,
    name: raw.tag || inspection.value?.tag || "",
  }, {
    registry: inspection.value?.registry,
    repository: inspection.value?.repository,
    imageRole: raw.imageRole,
  });
});

const detailOverview = computed(() => {
  if (!inspection.value) return [];
  const values = [
    { label: "Registry", value: inspection.value.registry },
    { label: "Repository", value: inspection.value.repository, mono: true },
    { label: "Tag", value: inspection.value.tag, mono: true },
    { label: "Selected platform", value: platformLabel(inspection.value.platform) || inspectPlatform.value, mono: true },
    {
      label: "Webhook version",
      value: webhookVersion.value?.value,
      title: webhookVersion.value
        ? `${webhookVersion.value.sourceKey} (${webhookVersion.value.sourceType}) · raw value: ${webhookVersion.value.rawValue}`
        : "",
      mono: true,
      accent: true,
    },
    { label: "Reference digest", value: shortDigest(inspection.value.digest), title: inspection.value.digest, mono: true },
    { label: "Reference media type", value: inspection.value.mediaType, mono: true },
    { label: "Created", value: formatDate(inspection.value.createdAt) },
    { label: "Uploaded", value: formatDate(inspection.value.uploadedAt) },
    { label: "Selected image size", value: formatBytes(inspection.value.size) },
  ];
  const primeHead = inspectionPrimeHead.value;
  if (primeHead?.isPrimeHead) {
    values.push(
      { label: "Prime head form", value: headKindLabel(primeHead.headKind), accent: true },
      { label: "Prime patch", value: normalizeVersionLabel(primeHead.version), mono: true, accent: true },
      { label: "Moving selector", value: primeHead.selector, mono: true },
      { label: "Tag commit", value: shortCommit(primeHead.commit), title: primeHead.commit, mono: true },
      { label: "Image role", value: roleLabel(primeHead.imageRole) },
      { label: "Pair evidence", value: pairStatusLabel(primeHead), title: primeHead.pairError },
      { label: "Companion image", value: primeHead.companionReference, mono: true },
      { label: "Pair completed", value: formatDate(primeHead.pairCompletedAt) },
      { label: "Resolved rank", value: primeHead.resolvedRank ? `#${primeHead.resolvedRank}` : "" },
      { label: "Canonical image", value: primeHead.canonicalReference, mono: true },
      { label: "Prime source", value: primeHead.sourceRepository, mono: true },
      { label: "Source revision", value: shortCommit(primeHead.revision), title: primeHead.revision, mono: true },
      { label: "OSS revision", value: shortCommit(primeHead.ossRevision), title: primeHead.ossRevision, mono: true },
      { label: "Canonical tag matches", value: primeHead.canonicalMatchesRequest == null ? "" : primeHead.canonicalMatchesRequest ? "Yes" : "No" },
      {
        label: "Tag commit matches OSS",
        value: primeHead.imageRole !== "server" || primeHead.commitMatchesOss == null
          ? ""
          : primeHead.commitMatchesOss ? "Yes" : "No",
      },
      {
        label: "Prime provenance",
        value: primeHead.provenanceValid === true && (primeHead.imageRole !== "server" || primeHead.primeSourceValid !== false)
          ? "Validated"
          : primeHead.provenanceValid === false || (primeHead.imageRole === "server" && primeHead.primeSourceValid === false)
            ? "Needs attention"
            : "Unverified",
      },
    );
  }
  return values.filter(item => item.value && item.value !== "—");
});

const configurationSummary = computed(() => [
  { label: "OS", value: imageConfig.value.os },
  { label: "Architecture", value: imageConfig.value.architecture },
  { label: "Variant", value: imageConfig.value.variant },
  { label: "Created", value: formatDate(imageConfig.value.createdAt) },
  { label: "Config digest", value: shortDigest(imageConfig.value.digest) },
  { label: "Config size", value: formatBytes(imageConfig.value.size) },
].filter(item => item.value && item.value !== "—"));

const objectEntries = value => {
  if (!value || typeof value !== "object" || Array.isArray(value)) return [];
  return Object.entries(value)
    .map(([key, item]) => ({ key, value: item == null ? "" : typeof item === "string" ? item : JSON.stringify(item) }))
    .sort((left, right) => left.key.localeCompare(right.key));
};

const configLabels = computed(() => objectEntries(imageConfig.value.labels));
const configEnvironment = computed(() => {
  const value = imageConfig.value.env;
  if (Array.isArray(value)) return value.map(item => String(item));
  return value == null || value === "" ? [] : [String(value)];
});

const webhookEnvironmentKeys = [
  "CATTLE_RANCHER_WEBHOOK_VERSION",
  "CATTLE_WEBHOOK_VERSION",
  "RANCHER_WEBHOOK_VERSION",
  "WEBHOOK_VERSION",
];

const parseEnvironmentEntry = entry => {
  const rawEntry = String(entry ?? "");
  const separator = rawEntry.indexOf("=");
  if (separator <= 0) return null;
  const key = rawEntry.slice(0, separator).trim();
  if (!key) return null;
  return {
    key,
    rawValue: rawEntry.slice(separator + 1),
  };
};

const normalizeWebhookVersionDisplay = rawValue => {
  const trimmed = String(rawValue ?? "").trim();
  if (trimmed.length >= 2) {
    const first = trimmed[0];
    const last = trimmed[trimmed.length - 1];
    if ((first === "\"" && last === "\"") || (first === "'" && last === "'")) {
      return trimmed.slice(1, -1).trim();
    }
  }
  return trimmed;
};

const webhookVersion = computed(() => {
  const environment = configEnvironment.value
    .map(parseEnvironmentEntry)
    .filter(Boolean);

  for (const key of webhookEnvironmentKeys) {
    const match = environment.find(entry => entry.key === key);
    const value = normalizeWebhookVersionDisplay(match?.rawValue);
    if (match && value) {
      return {
        value,
        rawValue: match.rawValue,
        sourceKey: match.key,
        sourceType: "environment variable",
      };
    }
  }

  const label = configLabels.value
    .filter(entry => entry.key.toLowerCase().endsWith("webhook.version"))
    .sort((left, right) => left.key.length - right.key.length || left.key.localeCompare(right.key))[0];
  const value = normalizeWebhookVersionDisplay(label?.value);
  return label && value
    ? {
        value,
        rawValue: label.value,
        sourceKey: label.key,
        sourceType: "image label",
      }
    : null;
});

const commandLabel = value => Array.isArray(value) ? value.map(item => String(item)).join(" ") : value == null ? "" : String(value);
const configEntrypoint = computed(() => commandLabel(imageConfig.value.entrypoint));
const configCommand = computed(() => commandLabel(imageConfig.value.cmd));
const hasConfiguration = computed(() => Boolean(
  configurationSummary.value.length || configLabels.value.length || configEnvironment.value.length || configEntrypoint.value || configCommand.value
));

const layerTotalSize = computed(() => formatBytes(layers.value.reduce((total, layer) => {
  const size = Number(layer?.size);
  return total + (Number.isFinite(size) ? size : 0);
}, 0)));

const buildYamlVisible = computed(() => Boolean(
  inspection.value && (
    Object.prototype.hasOwnProperty.call(inspection.value, "buildYaml")
    || declaredSource.value
    || sourceBuildYaml.value
  )
));
const buildYamlStatus = computed(() => {
  if (buildYaml.value.found) return "Found";
  if (buildYaml.value.skipped) return "Skipped";
  return buildYaml.value.error ? "Error" : "Not found";
});
const buildYamlStatusClass = computed(() => {
  if (buildYaml.value.found) return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300";
  if (buildYaml.value.skipped) return "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300";
  if (buildYaml.value.error) return "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-300";
  return "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300";
});
const buildYamlOriginLabel = computed(() => {
  if (usingDeclaredOSSSource.value) return "Declared OSS source";
  if (usingSourceBuildYaml.value) return "Declared source";
  return embeddedBuildYaml.value.found ? "Embedded image" : "Embedded scan";
});
const buildYamlOriginClass = computed(() => usingSourceBuildYaml.value
  ? "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-300"
  : "border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-500/25 dark:bg-violet-500/10 dark:text-violet-300"
);
const buildYamlSkippedLabel = computed(() => {
  const reason = typeof buildYaml.value.reason === "string" && buildYaml.value.reason.trim()
    ? buildYaml.value.reason.trim()
    : typeof buildYaml.value.skipped === "string" && buildYaml.value.skipped.trim()
      ? buildYaml.value.skipped.trim()
      : typeof buildYaml.value.error === "string" && buildYaml.value.error.trim()
        ? buildYaml.value.error.trim()
        : "The bounded scan stopped before every eligible layer could be checked.";
  return `No build.yaml was found in the eligible layers scanned safely. ${reason} The file may still exist elsewhere in the image.`;
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
  if (entries.length >= 251) return entries;
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

const allStructuredBuildEntries = computed(() => (
  buildYaml.value.data == null ? [] : flattenStructuredData(buildYaml.value.data)
));
const structuredBuildEntries = computed(() => allStructuredBuildEntries.value.slice(0, 250));
const structuredBuildTruncated = computed(() => allStructuredBuildEntries.value.length > 250);

const normalizeCustomRepository = value => String(value || "")
  .trim()
  .replace(/^docker:\/\//i, "")
  .replace(/^https?:\/\//i, "")
  .replace(/\/+$/, "");

const resultSortRequest = pairLookupEligible => {
  switch (resultSort.value) {
  case "fast": return { scanMode: "bounded", sortBy: "natural", sortOrder: "desc" };
  case "pair-rank": return pairLookupEligible
    ? { scanMode: "complete", sortBy: "pair-completed", sortOrder: "desc" }
    : { scanMode: "complete", sortBy: "natural", sortOrder: "desc" };
  case "uploaded-desc": return { scanMode: "complete", sortBy: "uploaded", sortOrder: "desc" };
  case "uploaded-asc": return { scanMode: "complete", sortBy: "uploaded", sortOrder: "asc" };
  case "version-asc": return { scanMode: "complete", sortBy: "version", sortOrder: "asc" };
  case "version-desc": return { scanMode: "complete", sortBy: "version", sortOrder: "desc" };
  case "tag-desc": return { scanMode: "complete", sortBy: "tag", sortOrder: "desc" };
  case "tag-asc": return { scanMode: "complete", sortBy: "tag", sortOrder: "asc" };
  default: return { scanMode: "bounded", sortBy: "natural", sortOrder: "desc" };
  }
};

const searchImages = async () => {
  if (!resultSortTouched.value) {
    resultSort.value = isPrimeMovingSelectorInput.value ? "pair-rank" : "fast";
  }
  if ((isPrimeMovingSelectorInput.value || isImmutablePrimeHeadInput.value)
    && !queryReferenceDetails.value
    && imageFamily.value !== "custom") {
    registry.value = "stgregistry.suse.com";
    if (imageFamily.value === "rancher/rancher-webhook") imageFamily.value = "all";
  }

  const normalizedVersionLine = versionLineFilter.value.trim().replace(/^v/i, "");
  if (normalizedVersionLine && !/^\d+\.\d+(?:\.\d+)?$/.test(normalizedVersionLine)) {
    searchResponse.value = null;
    searched.value = false;
    searchError.value = "Release line / patch must look like 2.15 or 2.15.1.";
    return;
  }
  const normalizedCommit = commitFilter.value.trim().toLowerCase();
  if (normalizedCommit && !/^[0-9a-f]{7,40}$/.test(normalizedCommit)) {
    searchResponse.value = null;
    searched.value = false;
    searchError.value = "Commit SHA must contain 7–40 hexadecimal characters.";
    return;
  }

  const repository = imageFamily.value === "custom"
    ? normalizeCustomRepository(customRepository.value)
    : imageFamily.value;
  if (!repository) {
    searchResponse.value = null;
    searched.value = false;
    closeDetails();
    searchError.value = "Enter a full custom repository before searching.";
    return;
  }
  if (repository !== "all" && !repository.includes("/")) {
    searchResponse.value = null;
    searched.value = false;
    closeDetails();
    searchError.value = "Repository must include an image path, such as rancher/rancher or registry.example.com/team/image.";
    return;
  }

  searchController?.abort();
  const controller = new AbortController();
  searchController = controller;
  searchLoading.value = true;
  searchError.value = "";
  searched.value = false;
  searchResponse.value = null;
  closeDetails();

  try {
    const normalizedQuery = query.value.trim();
    const requestQuery = ["prime-head", "primehead"].includes(normalizedQuery.toLowerCase()) ? "head" : normalizedQuery;
    const queryReference = queryReferenceDetails.value?.reference || "";
    const referenceSelectorOffset = queryReferenceDetails.value?.digest
      ? queryReference.lastIndexOf("@")
      : queryReference.lastIndexOf(":");
    const referenceBase = queryReference
      ? queryReference.slice(0, referenceSelectorOffset)
      : "";
    const referenceParts = referenceBase.split("/");
    const effectiveRegistry = queryReference ? referenceParts[0]?.toLowerCase() : registry.value;
    const effectiveRepository = queryReference ? referenceParts.slice(1).join("/") : repository.replace(/^stgregistry\.suse\.com\//i, "");
    const primePairSelector = isPrimeMovingSelectorInput.value
      || isImmutablePrimeHeadInput.value
      || ((quickFilter.value === "prime-head" || primeHeadFilter.value === "only") && /^\d+\.\d+\.\d+$/.test(normalizedVersionLine));
    const pairLookupEligible = primePairSelector
      && (effectiveRegistry === "all" || effectiveRegistry === "stgregistry.suse.com")
      && ["all", "rancher/rancher", "rancher/rancher-agent"].includes(effectiveRepository);
    const sortRequest = resultSortRequest(pairLookupEligible);
    const supportedArchitectures = new Set(["all", "multi", "amd64", "arm64", "s390x", "ppc64le", "386", "arm"]);
    const requestedChannel = quickFilter.value === "prime-head"
      ? "head"
      : channelQueryIDs.has(quickFilter.value) && quickFilter.value !== "all"
        ? quickFilter.value
        : "all";
    const requestBody = {
      registry: registry.value,
      repository,
      query: requestQuery,
      limit: 200,
      includeArtifacts: false,
      channel: requestedChannel,
      architecture: supportedArchitectures.has(architectureFilter.value) ? architectureFilter.value : "all",
      primeHead: quickFilter.value === "prime-head" ? "only" : primeHeadFilter.value,
      headKind: primeHeadFilter.value === "exclude" ? "all" : headKindFilter.value,
      versionLine: normalizedVersionLine,
      commit: normalizedCommit,
      pairStatus: pairLookupEligible && pairStatusFilter.value === "verified" ? "verified" : "all",
      recentDays: recentOnly.value ? 30 : 0,
      scanMode: sortRequest.scanMode,
      sortBy: sortRequest.sortBy,
      sortOrder: sortRequest.sortOrder,
    };
    let legacyRequestFallback = false;
    const legacyBody = body => ({
      registry: body.registry,
      repository: body.repository,
      query: body.query,
      limit: body.limit,
      includeArtifacts: body.includeArtifacts,
    });
    const fetchSearchPayload = async body => {
      const send = async payload => {
        const response = await apiFetch("/api/images/search", {
          method: "POST",
          signal: controller.signal,
          body: JSON.stringify(payload),
        });
        return response.json();
      };
      if (legacyRequestFallback) return send(legacyBody(body));
      try {
        return await send(body);
      } catch (error) {
        if (!/unknown field/i.test(String(error?.message || ""))) throw error;
        legacyRequestFallback = true;
        return send(legacyBody(body));
      }
    };
    let payload = await fetchSearchPayload(requestBody);
    const returnedTags = Array.isArray(payload?.groups)
      ? payload.groups.reduce((total, group) => total + (Array.isArray(group?.tags) ? group.tags.length : 0), 0)
      : 0;
    const returnedGroupError = Array.isArray(payload?.groups) && payload.groups.some(group => group?.error);
    let aliasFallbackExpanded = false;
    if (legacyRequestFallback && isPrimeMovingSelectorInput.value && returnedTags === 0 && !returnedGroupError) {
      const selectorMatch = querySelector.value.match(primeMovingTagPattern);
      if (selectorMatch) {
        const prefix = `${querySelector.value.toLowerCase().startsWith("v") ? "v" : ""}${selectorMatch[1]}.${selectorMatch[2]}.${selectorMatch[3]}-`;
        let fallbackQuery = prefix;
        if (queryReferenceDetails.value) {
          const exactReference = queryReferenceDetails.value.reference;
          const separator = exactReference.lastIndexOf("@");
          const tagSeparator = exactReference.lastIndexOf(":");
          const selectorOffset = separator > exactReference.lastIndexOf("/") ? separator : tagSeparator;
          fallbackQuery = `${exactReference.slice(0, selectorOffset)}:${prefix}`;
        }
        payload = await fetchSearchPayload({ ...requestBody, query: fallbackQuery });
        aliasFallbackExpanded = true;
      }
    }
    searchResponse.value = {
      ...payload,
      requestedQuery: normalizedQuery,
      aliasFallbackExpanded,
      legacyRequestFallback,
      groups: Array.isArray(payload?.groups) ? payload.groups : [],
    };
    searched.value = true;
  } catch (error) {
    if (error?.name !== "AbortError") {
      searchError.value = error instanceof Error ? error.message : "Image search failed.";
    }
  } finally {
    if (searchController === controller) {
      searchLoading.value = false;
    }
  }
};

const handleResultSortChange = async () => {
  resultSortTouched.value = true;
  if (!searched.value || searchLoading.value || !searchResponse.value) return;
  await searchImages();
};

const clearSearch = () => {
  searchController?.abort();
  searchLoading.value = false;
  searchError.value = "";
  searchResponse.value = null;
  searched.value = false;
  closeDetails();
};

const tagReference = (group, tag) => {
  if (tag?.reference) return String(tag.reference);
  const repository = String(group?.repository || "").replace(/^\/+|\/+$/g, "");
  const registryHost = String(group?.registry || "").replace(/^https?:\/\//i, "").replace(/\/+$/g, "");
  const groupReference = String(group?.reference || "").trim();
  const base = groupReference || (registryHost && repository
    ? (repository === registryHost || repository.startsWith(`${registryHost}/`) ? repository : `${registryHost}/${repository}`)
    : repository || registryHost);
  return tag?.name && base ? `${base}:${tag.name}` : base;
};

const inspectPlatformFromReference = reference => {
  const selector = parseFullReferenceInput(reference)?.selector || String(reference || "").trim();
  if (!selector || /^sha256:[0-9a-f]{64}$/i.test(selector)) return "";
  const explicitOS = selector.match(/(?:^|[-_.])(linux|windows)[-_.](amd64|arm64|s390x|ppc64le|386|arm)(?:[-_.](v\d+))?$/i);
  if (explicitOS) {
    return [explicitOS[1].toLowerCase(), explicitOS[2].toLowerCase(), explicitOS[3]?.toLowerCase()].filter(Boolean).join("/");
  }
  const bareArchitecture = selector.match(/(?:^|[-_.])(amd64|arm64|s390x|ppc64le|386|arm)(?:[-_.](v\d+))?$/i);
  if (!bareArchitecture) return "";
  return ["linux", bareArchitecture[1].toLowerCase(), bareArchitecture[2]?.toLowerCase()].filter(Boolean).join("/");
};

const inspectTag = (group, tag) => {
  const reference = tagReference(group, tag);
  if (!reference) return;
  const inferredPlatform = inspectPlatformFromReference(tag?.name) || inspectPlatformFromReference(reference);
  if (inferredPlatform) inspectPlatform.value = inferredPlatform;
  inspectReference(reference);
};

const inspectPlatformImage = item => {
  const platform = platformReference(item);
  if (!platform || !selectedReference.value) return;
  inspectPlatform.value = platform;
  void inspectReference(selectedReference.value);
};

const cancelSourceBuildFetch = () => {
  if (!sourceBuildController) return;
  sourceBuildController.abort();
  sourceBuildController = null;
  sourceBuildLoading.value = false;
  sourceBuildError.value = "";
  sourceBuildCancelled.value = true;
};

const resetSourceBuildState = () => {
  sourceBuildController?.abort();
  sourceBuildController = null;
  sourceBuildYaml.value = null;
  sourceBuildLoading.value = false;
  sourceBuildError.value = "";
  sourceBuildCancelled.value = false;
};

const fetchSourceBuildYaml = async () => {
  if (!sourceFetchAvailable.value || sourceBuildLoading.value) return;

  sourceBuildController?.abort();
  const controller = new AbortController();
  sourceBuildController = controller;
  sourceBuildLoading.value = true;
  sourceBuildError.value = "";
  sourceBuildCancelled.value = false;

  const selectedPlatform = platformReference(inspection.value?.platform)
    || platformLabel(inspection.value?.platform)
    || inspectPlatform.value;

  try {
    const response = await apiFetch("/api/images/build-yaml/source", {
      method: "POST",
      signal: controller.signal,
      body: JSON.stringify({
        reference: inspection.value?.reference || selectedReference.value,
        platform: selectedPlatform,
        expectedDigest: inspection.value?.digest,
      }),
    });
    const payload = await response.json();
    const result = payload?.buildYaml && typeof payload.buildYaml === "object" ? payload.buildYaml : payload;
    sourceBuildYaml.value = {
      ...result,
      origin: result?.origin || "declared-source",
      provenance: result?.provenance && typeof result.provenance === "object" ? result.provenance : {},
      found: result?.found !== false,
      skipped: false,
      error: "",
    };
    buildYamlMode.value = "structured";
  } catch (error) {
    if (error?.name !== "AbortError") {
      sourceBuildError.value = error instanceof Error ? error.message : "Could not fetch build.yaml from the declared source.";
    }
  } finally {
    if (sourceBuildController === controller) {
      sourceBuildController = null;
      sourceBuildLoading.value = false;
    }
  }
};

const inspectReference = async reference => {
  reference = String(reference || "").trim();
  if (!reference) return;

  inspectController?.abort();
  resetSourceBuildState();
  const controller = new AbortController();
  inspectController = controller;
  selectedReference.value = reference;
  inspectLoading.value = true;
  inspectError.value = "";
  inspection.value = null;
  buildYamlMode.value = "structured";

  try {
    const response = await apiFetch("/api/images/inspect", {
      method: "POST",
      signal: controller.signal,
      body: JSON.stringify({
        reference,
        platform: inspectPlatform.value,
        includeBuildYaml: true,
      }),
    });
    const payload = await response.json();
    inspection.value = { ...payload, reference: payload?.reference || reference };
    buildYamlMode.value = payload?.buildYaml?.error && payload?.buildYaml?.raw ? "raw" : "structured";
  } catch (error) {
    if (error?.name !== "AbortError") {
      inspectError.value = error instanceof Error ? error.message : "Image inspection failed.";
    }
  } finally {
    if (inspectController === controller) {
      inspectLoading.value = false;
    }
  }
};

const closeDetails = () => {
  inspectController?.abort();
  resetSourceBuildState();
  inspectLoading.value = false;
  inspectError.value = "";
  inspection.value = null;
  selectedReference.value = "";
  buildYamlMode.value = "structured";
};

const copyText = async (value, successMessage) => {
  value = String(value || "");
  if (!value) return;
  try {
    await writeTextToClipboard(value);
    copyNotice.value = successMessage;
  } catch (_) {
    copyNotice.value = "Clipboard access is unavailable.";
  }
  window.clearTimeout(copyNoticeTimer);
  copyNoticeTimer = window.setTimeout(() => {
    copyNotice.value = "";
  }, 2600);
};

const groupStatusClass = group => group?.error
  ? "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-300"
  : group?.visibleTags?.length
    ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300"
    : "border-zinc-200 bg-white text-zinc-500 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400";

const roleLabel = value => {
  switch (normalizedImageRole(value)) {
  case "server": return "Server";
  case "agent": return "Agent";
  case "webhook": return "Webhook";
  default: return "Image";
  }
};

const headKindLabel = value => {
  const kind = normalizedHeadKind(value);
  return kind === "moving" ? "Moving selector" : kind === "immutable" ? "Immutable" : "Head";
};
const headKindClass = value => normalizedHeadKind(value) === "moving"
  ? "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300"
  : "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300";

const pairStatusLabel = tag => {
  if (tag?.headKind === "moving") return "Selector only";
  switch (String(tag?.pairStatus || "").toLowerCase()) {
  case "verified": return tag?.pairComplete ? "Verified complete" : "Verified";
  case "missing": return "Companion missing";
  case "invalid": return "Invalid pair";
  case "error": return "Pair lookup error";
  default: return "Unverified";
  }
};

const pairStatusClass = tag => {
  if (tag?.headKind === "moving") {
    return "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300";
  }
  if (tag?.pairStatus === "verified" && tag?.pairComplete) {
    return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300";
  }
  if (["missing", "invalid", "error"].includes(String(tag?.pairStatus || "").toLowerCase()) || tag?.pairError) {
    return "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-300";
  }
  return "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300";
};

const normalizeVersionLabel = value => {
  const normalized = String(value || "").trim();
  return normalized ? (normalized.toLowerCase().startsWith("v") ? normalized : `v${normalized}`) : "";
};

const shortCommit = value => {
  const normalized = String(value || "").trim();
  return normalized.length > 12 ? `${normalized.slice(0, 12)}…` : normalized;
};

const compactSource = value => String(value || "")
  .replace(/^https?:\/\/github\.com\//i, "")
  .replace(/\.git$/i, "");

const primeMetadataTitle = tag => [
  tag?.version ? `Patch: ${normalizeVersionLabel(tag.version)}` : "",
  tag?.versionLine ? `Release line: ${normalizeVersionLabel(tag.versionLine)}` : "",
  tag?.commit ? `Tag commit: ${tag.commit}` : "",
  tag?.selector ? `Moving selector: ${tag.selector}` : "",
  tag?.canonicalReference ? `Canonical reference: ${tag.canonicalReference}` : "",
  tag?.sourceRepository ? `Source: ${tag.sourceRepository}` : "",
  tag?.ossRevision ? `OSS revision: ${tag.ossRevision}` : "",
].filter(Boolean).join("\n");

const channelClass = tag => {
  const value = `${String(tag?.channel || "")} ${String(tag?.name || "")}`.toLowerCase();
  if (value.includes("head") || value.includes("devel") || value.includes("alpha") || value.includes("beta")) {
    return "border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-500/25 dark:bg-violet-500/10 dark:text-violet-300";
  }
  if (value.includes("rcs")) {
    return "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-300";
  }
  if (/(?:^|[-.\s])rc(?:$|[-.\s\d])/.test(value)) {
    return "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300";
  }
  if (value.includes("stable") || value.includes("release") || /^v?\d+\.\d+\.\d+$/.test(String(tag?.name || ""))) {
    return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300";
  }
  return "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300";
};

const architectureLabel = value => {
  const displayValue = item => String(item || "").toLowerCase() === "multi" ? "Unsuffixed / base tag" : String(item || "");
  if (Array.isArray(value)) return value.filter(Boolean).map(displayValue).join(", ") || "—";
  return displayValue(value) || "—";
};

const platformLabel = value => {
  if (!value) return "";
  if (typeof value === "string") return value;
  if (typeof value !== "object") return String(value);
  const os = value.os || value.OS || "";
  const architecture = value.architecture || value.arch || "";
  const variant = value.variant || "";
  const label = [os, architecture].filter(Boolean).join("/");
  return variant ? `${label || "platform"}/${variant}` : label || value.digest || "platform";
};

const formatDate = value => {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
};

const formatBytes = value => {
  if (value == null || value === "") return "—";
  const bytes = Number(value);
  if (!Number.isFinite(bytes)) return String(value);
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024)), units.length - 1);
  const amount = bytes / (1024 ** Math.max(index, 0));
  return `${amount >= 10 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[Math.max(index, 0)]}`;
};

const shortDigest = value => {
  value = String(value || "");
  if (!value) return "—";
  const [algorithm, digest = ""] = value.includes(":") ? value.split(":", 2) : ["", value];
  const shortened = digest.length > 14 ? `${digest.slice(0, 14)}…` : digest;
  return algorithm ? `${algorithm}:${shortened}` : shortened;
};

const formatStructuredValue = value => {
  if (value === null) return "null";
  if (value === undefined) return "undefined";
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value);
  } catch (_) {
    return String(value);
  }
};

const structuredValueClass = value => {
  if (value === null || value === undefined) return "text-zinc-400";
  if (typeof value === "boolean") return "text-violet-700 dark:text-violet-300";
  if (typeof value === "number") return "text-sky-700 dark:text-sky-300";
  return "text-zinc-800 dark:text-zinc-200";
};

onBeforeUnmount(() => {
  searchController?.abort();
  inspectController?.abort();
  sourceBuildController?.abort();
  window.clearTimeout(copyNoticeTimer);
});
</script>
