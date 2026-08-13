<template>
  <div class="grid min-w-0 gap-5">
    <header class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
      <div class="flex min-w-0 items-start gap-3">
        <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-300">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <circle cx="6" cy="5" r="2" />
            <circle cx="18" cy="7" r="2" />
            <circle cx="6" cy="19" r="2" />
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 7v10m2-10h3a3 3 0 0 1 3 3v1a3 3 0 0 0 3 3h1" />
          </svg>
        </div>
        <div class="min-w-0">
          <h2 class="text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">PR Image Verifier</h2>
          <p class="mt-1 max-w-3xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">
            Check whether a GitHub pull request commit is included in a Rancher head image across every known registry.
          </p>
        </div>
      </div>

      <div class="flex shrink-0 flex-wrap items-center gap-2 text-xs font-semibold text-zinc-500 dark:text-zinc-400">
        <span class="rounded-full border border-zinc-200 bg-zinc-50 px-3 py-1.5 dark:border-white/10 dark:bg-white/[0.04]">Read-only checks</span>
        <span v-if="checkedAtLabel" class="rounded-full border border-zinc-200 bg-white px-3 py-1.5 dark:border-white/10 dark:bg-white/[0.04]">{{ checkedAtLabel }}</span>
      </div>
    </header>

    <form
      class="rounded-2xl border border-zinc-200/80 bg-zinc-50/70 p-4 shadow-sm dark:border-white/10 dark:bg-white/[0.025] sm:p-5"
      :aria-busy="loading ? 'true' : 'false'"
      novalidate
      @submit.prevent="verifyBuild"
    >
      <div class="grid gap-4 xl:grid-cols-12">
        <label class="grid min-w-0 gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300 xl:col-span-7">
          <span>GitHub pull request</span>
          <input
            v-model="pullRequest"
            type="url"
            inputmode="url"
            autocomplete="off"
            spellcheck="false"
            :disabled="loading"
            :aria-invalid="Boolean(formErrors.pullRequest)"
            aria-describedby="pr-image-check-pr-help"
            placeholder="https://github.com/rancher/rancher/pull/12345"
            class="h-11 w-full rounded-xl border bg-white px-3.5 text-sm font-semibold text-zinc-900 outline-none placeholder:font-normal placeholder:text-zinc-400 focus:ring-2 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500"
            :class="formErrors.pullRequest
              ? 'border-rose-400 focus:border-rose-500 focus:ring-rose-500/20 dark:border-rose-500/60'
              : 'border-zinc-200 focus:border-sky-500 focus:ring-sky-500/20 dark:border-white/10'"
            @input="formErrors.pullRequest = ''"
          />
          <span id="pr-image-check-pr-help" class="text-xs font-normal leading-5" :class="formErrors.pullRequest ? 'text-rose-600 dark:text-rose-300' : 'text-zinc-500 dark:text-zinc-400'">
            {{ formErrors.pullRequest || "Use a full github.com pull request URL." }}
          </span>
        </label>

        <label class="grid min-w-0 gap-1.5 text-sm font-semibold text-zinc-700 dark:text-zinc-300 xl:col-span-3">
          <span>Target head tag</span>
          <input
            v-model="tag"
            type="text"
            autocomplete="off"
            spellcheck="false"
            :disabled="loading"
            :aria-invalid="Boolean(formErrors.tag)"
            aria-describedby="pr-image-check-tag-help"
            placeholder="2.14-head"
            class="h-11 w-full rounded-xl border bg-white px-3.5 font-mono text-sm font-semibold text-zinc-900 outline-none placeholder:font-sans placeholder:font-normal placeholder:text-zinc-400 focus:ring-2 dark:bg-zinc-900 dark:text-white dark:placeholder:text-zinc-500"
            :class="formErrors.tag
              ? 'border-rose-400 focus:border-rose-500 focus:ring-rose-500/20 dark:border-rose-500/60'
              : 'border-zinc-200 focus:border-sky-500 focus:ring-sky-500/20 dark:border-white/10'"
            @input="formErrors.tag = ''"
          />
          <span id="pr-image-check-tag-help" class="text-xs font-normal leading-5" :class="formErrors.tag ? 'text-rose-600 dark:text-rose-300' : 'text-zinc-500 dark:text-zinc-400'">
            {{ formErrors.tag || (normalizedInputTag ? `Will check ${normalizedInputTag}` : "For example, 2.14-head or head.") }}
          </span>
        </label>

        <div class="flex items-start gap-2 pt-[1.625rem] xl:col-span-2">
          <button
            v-if="!loading"
            type="submit"
            class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl bg-sky-600 px-4 text-sm font-bold text-white shadow-md shadow-sky-500/15 transition-colors hover:bg-sky-700 disabled:cursor-not-allowed disabled:opacity-55"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.25" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
            </svg>
            Verify
          </button>
          <button
            v-else
            type="button"
            class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-zinc-300 bg-white px-4 text-sm font-bold text-zinc-700 hover:bg-zinc-50 dark:border-white/15 dark:bg-white/[0.05] dark:text-zinc-200 dark:hover:bg-white/[0.09]"
            @click="cancelVerification"
          >
            <span class="spinner !h-4 !w-4 !border-[1.5px]"></span>
            Cancel
          </button>
          <button
            v-if="result || requestError || cancelled"
            type="button"
            :disabled="loading"
            class="inline-flex h-11 items-center justify-center rounded-xl border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-600 hover:bg-zinc-50 disabled:opacity-55 dark:border-white/10 dark:bg-white/[0.05] dark:text-zinc-300 dark:hover:bg-white/[0.08]"
            title="Clear verification results"
            aria-label="Clear verification results"
            @click="clearResult"
          >
            Clear
          </button>
        </div>
      </div>

      <div class="mt-4 flex flex-col gap-3 border-t border-zinc-200/70 pt-4 dark:border-white/10 lg:flex-row lg:items-center lg:justify-between">
        <div class="flex flex-wrap gap-2" aria-label="Registries checked">
          <span v-for="registryLabel in knownRegistryLabels" :key="registryLabel" class="rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-xs font-bold text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-300">
            {{ registryLabel }}
          </span>
        </div>
        <p class="max-w-2xl text-xs leading-5 text-zinc-500 dark:text-zinc-400 lg:text-right">
          Resolves the PR through the configured GitHub CLI login, then inspects Rancher server and agent images without changing either system.
        </p>
      </div>
    </form>

    <div class="rounded-xl border border-sky-200 bg-sky-50/70 px-4 py-3 text-xs leading-5 text-sky-900 dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-100">
      <span class="font-bold">How proof works:</span>
      the verifier compares the PR commit with the full Git revision declared by the image. A container digest identifies image content; it is not a Git commit and is never compared directly with the PR SHA.
    </div>

    <div v-if="requestError" role="alert" class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200">
      <div class="font-bold">PR image verification failed</div>
      <div class="mt-1 whitespace-pre-wrap break-words">{{ requestError }}</div>
    </div>

    <section v-if="loading" class="grid gap-4" role="status" aria-live="polite">
      <div class="rounded-2xl border border-sky-200 bg-sky-50/60 p-4 dark:border-sky-500/20 dark:bg-sky-500/[0.07] sm:p-5">
        <div class="flex items-center gap-3">
          <span class="spinner shrink-0 text-sky-600 dark:text-sky-300"></span>
          <div>
            <h3 class="text-sm font-bold text-sky-950 dark:text-sky-100">Verification in progress</h3>
            <p class="mt-1 text-xs leading-5 text-sky-800/80 dark:text-sky-100/70">This may take a little while when registries or GitHub respond slowly.</p>
          </div>
        </div>
        <ol class="mt-4 grid gap-2 sm:grid-cols-4" aria-label="Verification work">
          <li v-for="(phase, index) in phases" :key="phase" class="flex items-center gap-2 rounded-lg border border-sky-200/70 bg-white/60 px-3 py-2 text-xs font-semibold text-sky-800 dark:border-sky-500/15 dark:bg-black/10 dark:text-sky-100">
            <span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-sky-100 text-[10px] font-extrabold text-sky-600 dark:bg-sky-500/15 dark:text-sky-200">{{ index + 1 }}</span>
            <span>{{ phase }}</span>
          </li>
        </ol>
      </div>

      <div class="grid gap-3 lg:grid-cols-2">
        <div v-for="index in 4" :key="index" class="animate-pulse rounded-2xl border border-zinc-200 bg-zinc-50/70 p-5 dark:border-white/10 dark:bg-white/[0.025]">
          <div class="h-4 w-36 rounded bg-zinc-200 dark:bg-zinc-700"></div>
          <div class="mt-3 h-3 w-56 max-w-full rounded bg-zinc-100 dark:bg-zinc-800"></div>
          <div class="mt-5 grid grid-cols-2 gap-3">
            <div class="h-28 rounded-xl bg-white dark:bg-zinc-900"></div>
            <div class="h-28 rounded-xl bg-white dark:bg-zinc-900"></div>
          </div>
        </div>
      </div>
    </section>

    <div v-else-if="cancelled" class="rounded-2xl border border-dashed border-zinc-300 bg-zinc-50/60 px-6 py-10 text-center dark:border-white/15 dark:bg-white/[0.02]">
      <h3 class="text-base font-bold text-zinc-900 dark:text-zinc-100">Verification cancelled</h3>
      <p class="mx-auto mt-2 max-w-xl text-sm leading-6 text-zinc-500 dark:text-zinc-400">No registry or GitHub data was changed. Submit the form whenever you are ready to try again.</p>
    </div>

    <div v-else-if="!result && !requestError" class="rounded-2xl border border-dashed border-zinc-300 bg-zinc-50/60 px-6 py-12 text-center dark:border-white/15 dark:bg-white/[0.02]">
      <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-white/[0.06] dark:text-zinc-500">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M20 7 12 3 4 7m16 0-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
        </svg>
      </div>
      <h3 class="mt-4 text-base font-bold text-zinc-900 dark:text-zinc-100">Verify a PR against head images</h3>
      <p class="mx-auto mt-2 max-w-xl text-sm leading-6 text-zinc-500 dark:text-zinc-400">
        Paste a GitHub pull request URL and target head tag. The check will report the image and Git evidence from each known registry.
      </p>
    </div>

    <template v-else-if="result">
      <section ref="resultSummaryElement" tabindex="-1" role="status" aria-live="polite" class="overflow-hidden rounded-2xl border p-4 outline-none sm:p-5" :class="summaryCardClass">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <span class="inline-flex rounded-full border px-3 py-1 text-xs font-extrabold" :class="statusBadgeClass(overallState)">{{ statusLabel(overallState) }}</span>
              <span class="inline-flex rounded-full border px-3 py-1 text-xs font-bold" :class="scanComplete ? 'border-emerald-300 bg-white/70 text-emerald-800 dark:border-emerald-500/30 dark:bg-black/10 dark:text-emerald-200' : 'border-amber-300 bg-white/70 text-amber-800 dark:border-amber-500/30 dark:bg-black/10 dark:text-amber-200'">
                {{ scanComplete ? "Scan complete" : "Partial scan" }}
              </span>
            </div>
            <h3 class="mt-3 text-xl font-bold tracking-tight">{{ summaryHeading }}</h3>
            <p class="mt-2 max-w-4xl text-sm leading-6 opacity-80">{{ summaryMessage }}</p>
          </div>
          <div class="flex shrink-0 flex-wrap gap-2 lg:max-w-md lg:justify-end">
            <span v-for="item in summaryCountBadges" :key="item.label" class="rounded-lg border border-current/20 bg-white/60 px-3 py-2 text-xs font-bold dark:bg-black/10">
              {{ item.label }} <span class="ml-1 text-sm">{{ item.value }}</span>
            </span>
          </div>
        </div>
      </section>

      <section v-if="resultWarnings.length" class="rounded-2xl border border-amber-200 bg-amber-50/70 p-4 text-sm leading-6 text-amber-900 dark:border-amber-500/20 dark:bg-amber-500/[0.08] dark:text-amber-100">
        <h3 class="font-bold">Interpretation notes</h3>
        <ul class="mt-2 list-disc space-y-1 pl-5">
          <li v-for="warning in resultWarnings" :key="warning">{{ warning }}</li>
        </ul>
      </section>

      <div class="grid min-w-0 gap-4 lg:grid-cols-2">
        <section class="min-w-0 rounded-2xl border border-zinc-200 bg-zinc-50/60 p-4 dark:border-white/10 dark:bg-white/[0.025] sm:p-5">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="text-[11px] font-extrabold uppercase tracking-wider text-violet-600 dark:text-violet-300">Pull request evidence</div>
              <h3 class="mt-1 break-words text-base font-bold text-zinc-900 dark:text-zinc-100">{{ pullRequestHeading }}</h3>
            </div>
            <a v-if="pullRequestLink" :href="pullRequestLink" target="_blank" rel="noopener noreferrer" class="shrink-0 text-xs font-bold text-violet-700 hover:underline dark:text-violet-300">Open PR ↗</a>
          </div>
          <dl class="mt-4 grid gap-2 sm:grid-cols-2">
            <div v-for="item in pullRequestDetails" :key="item.label" class="min-w-0 rounded-lg border border-zinc-200/70 bg-white px-3 py-2.5 dark:border-white/[0.07] dark:bg-zinc-900/60" :class="item.wide ? 'sm:col-span-2' : ''">
              <dt class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">{{ item.label }}</dt>
              <dd class="mt-1 break-all text-xs font-semibold text-zinc-800 dark:text-zinc-200" :class="item.mono ? 'font-mono' : ''" :title="item.value">{{ item.value }}</dd>
            </div>
          </dl>
          <a v-if="requiredCommitLink" :href="requiredCommitLink" target="_blank" rel="noopener noreferrer" class="mt-3 inline-flex text-xs font-bold text-violet-700 hover:underline dark:text-violet-300">Open verification commit ↗</a>
        </section>

        <section class="min-w-0 rounded-2xl border border-zinc-200 bg-zinc-50/60 p-4 dark:border-white/10 dark:bg-white/[0.025] sm:p-5">
          <div class="text-[11px] font-extrabold uppercase tracking-wider text-sky-600 dark:text-sky-300">Target image evidence</div>
          <h3 class="mt-1 break-all font-mono text-base font-bold text-zinc-900 dark:text-zinc-100">rancher/rancher:{{ resultTag }}</h3>
          <dl class="mt-4 grid gap-2 sm:grid-cols-2">
            <div v-for="item in targetDetails" :key="item.label" class="min-w-0 rounded-lg border border-zinc-200/70 bg-white px-3 py-2.5 dark:border-white/[0.07] dark:bg-zinc-900/60">
              <dt class="text-[10px] font-bold uppercase tracking-wide text-zinc-500">{{ item.label }}</dt>
              <dd class="mt-1 break-all text-xs font-semibold text-zinc-800 dark:text-zinc-200" :class="item.mono ? 'font-mono' : ''">{{ item.value }}</dd>
            </div>
          </dl>
          <p class="mt-3 text-xs leading-5 text-zinc-500 dark:text-zinc-400">The tag is mutable. These results describe the image digests observed at the checked time, not every image that has ever used this tag.</p>
        </section>
      </div>

      <section class="grid min-w-0 gap-4">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h3 class="text-base font-bold text-zinc-950 dark:text-zinc-50">Registry evidence</h3>
            <p class="mt-1 text-xs leading-5 text-zinc-500 dark:text-zinc-400">The Rancher server result determines the commit-ancestry verdict. Agent evidence confirms whether both exact server and agent tags were found.</p>
          </div>
          <span class="text-xs font-semibold text-zinc-500 dark:text-zinc-400">{{ registries.length }} registr{{ registries.length === 1 ? "y" : "ies" }} checked</span>
        </div>

        <div v-if="!registries.length" class="rounded-2xl border border-amber-200 bg-amber-50 px-5 py-8 text-center text-sm leading-6 text-amber-800 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-200">
          The verification response did not include any registry results.
        </div>

        <article v-for="(registryResult, registryIndex) in registries" :key="registryKey(registryResult, registryIndex)" class="min-w-0 overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-sm dark:border-white/10 dark:bg-zinc-900/60">
          <header class="flex flex-col gap-3 border-b border-zinc-200/70 bg-zinc-50/75 px-4 py-4 dark:border-white/10 dark:bg-white/[0.025] sm:flex-row sm:items-start sm:justify-between">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h4 class="font-bold text-zinc-900 dark:text-zinc-100">{{ registryName(registryResult) }}</h4>
                <span class="inline-flex rounded-full border px-2.5 py-0.5 text-[11px] font-bold" :class="statusBadgeClass(registryState(registryResult))">Server: {{ statusLabel(registryState(registryResult)) }}</span>
                <span class="inline-flex rounded-full border px-2.5 py-0.5 text-[11px] font-bold" :class="pairStatusClass(registryResult)">{{ pairStatusLabel(registryResult) }}</span>
              </div>
              <div class="mt-1 break-all font-mono text-xs text-zinc-500 dark:text-zinc-400">{{ displayValue(registryResult.registry) || "Registry host unavailable" }}</div>
            </div>
          </header>

          <div class="grid min-w-0 gap-3 p-3 lg:grid-cols-2 lg:p-4">
            <section v-for="imageEntry in imageEntries(registryResult)" :key="imageEntry.role" class="min-w-0 rounded-xl border p-3.5" :class="imageCardClass(imageEntry.image)">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h5 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">{{ imageEntry.label }}</h5>
                <span class="rounded-full border px-2.5 py-0.5 text-[10px] font-extrabold" :class="statusBadgeClass(imageState(imageEntry.image))">{{ statusLabel(imageState(imageEntry.image)) }}</span>
              </div>

              <template v-if="imageEntry.image && imageEntry.image.found !== false">
                <p class="mt-3 break-all font-mono text-[11px] font-semibold leading-5 text-zinc-700 dark:text-zinc-300">{{ displayValue(imageEntry.image.reference, 1024) || "Image reference unavailable" }}</p>
                <p class="mt-2 text-xs leading-5 text-zinc-600 dark:text-zinc-300">{{ matchReason(imageEntry.image) }}</p>

                <dl class="mt-3 grid gap-2 sm:grid-cols-2">
                  <div v-for="item in imageOverview(imageEntry.image)" :key="item.label" class="min-w-0 rounded-lg bg-white/75 px-3 py-2 dark:bg-black/10" :class="item.wide ? 'sm:col-span-2' : ''">
                    <dt class="text-[9px] font-extrabold uppercase tracking-wide text-zinc-500">{{ item.label }}</dt>
                    <dd class="mt-1 break-all text-[11px] font-semibold leading-5 text-zinc-800 dark:text-zinc-200" :class="item.mono ? 'font-mono' : ''" :title="item.value">{{ item.value }}</dd>
                  </div>
                </dl>

                <details class="mt-3 rounded-lg border border-zinc-200/80 bg-white/60 open:bg-white dark:border-white/10 dark:bg-black/10 dark:open:bg-black/20">
                  <summary class="cursor-pointer px-3 py-2.5 text-xs font-bold text-zinc-700 dark:text-zinc-300">Git and OCI evidence</summary>
                  <dl class="grid gap-2 border-t border-zinc-200/80 px-3 py-3 dark:border-white/10">
                    <div v-for="item in imageEvidence(imageEntry.image)" :key="item.label" class="min-w-0">
                      <dt class="text-[9px] font-extrabold uppercase tracking-wide text-zinc-500">{{ item.label }}</dt>
                      <dd class="mt-0.5 break-all font-mono text-[11px] leading-5 text-zinc-700 dark:text-zinc-300" :title="item.value">{{ item.value }}</dd>
                    </div>
                  </dl>
                  <div v-if="imageLinks(imageEntry.image).length" class="flex flex-wrap gap-3 border-t border-zinc-200/80 px-3 py-2.5 text-xs font-bold dark:border-white/10">
                    <a v-for="link in imageLinks(imageEntry.image)" :key="link.href" :href="link.href" target="_blank" rel="noopener noreferrer" class="text-sky-700 hover:underline dark:text-sky-300">{{ link.label }} ↗</a>
                  </div>
                </details>
              </template>

              <div v-else class="mt-3 rounded-lg border border-dashed border-zinc-300 bg-white/50 px-3 py-4 text-xs leading-5 text-zinc-600 dark:border-white/15 dark:bg-black/10 dark:text-zinc-300">
                {{ imageEntry.image?.error ? displayValue(imageEntry.image.error, 2000) : "This image was not found in the registry for the requested tag." }}
              </div>

              <div v-if="imageEntry.image?.error && imageEntry.image?.found !== false" class="mt-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs leading-5 text-rose-800 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200">
                {{ displayValue(imageEntry.image.error, 2000) }}
              </div>
            </section>
          </div>
        </article>
      </section>

      <section class="rounded-2xl border border-amber-200 bg-amber-50/70 p-4 text-xs leading-5 text-amber-900 dark:border-amber-500/20 dark:bg-amber-500/[0.08] dark:text-amber-100">
        <h3 class="font-bold">Proof limits</h3>
        <ul class="mt-2 list-disc space-y-1 pl-5">
          <li><span class="font-semibold">Unknown</span> means the available image or GitHub metadata could not prove either result; it does not mean the fix is absent.</li>
          <li>A cherry-pick or equivalent backport has a different Git SHA and cannot be proven by ancestry alone.</li>
          <li>Re-check immediately before testing because a head tag can move to a different image digest.</li>
        </ul>
      </section>
    </template>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref } from "vue";
import { apiFetch } from "./store.js";

const knownRegistryLabels = ["SUSE staging", "Rancher Prime", "SUSE registry", "Docker Hub"];
const phases = [
  "Reading pull request",
  "Resolving verification commit",
  "Inspecting known registries",
  "Comparing source ancestry",
];

const pullRequest = ref("");
const tag = ref("2.14-head");
const formErrors = ref({ pullRequest: "", tag: "" });
const loading = ref(false);
const cancelled = ref(false);
const requestError = ref("");
const result = ref(null);
const resultSummaryElement = ref(null);

let requestController = null;

const cleanString = (value, limit = 512) => {
  if (value == null) return "";
  const normalized = String(value).replace(/[\u0000-\u001f\u007f]+/g, " ").replace(/\s+/g, " ").trim();
  return normalized.length > limit ? `${normalized.slice(0, limit)}…` : normalized;
};

const displayValue = (value, limit) => cleanString(value, limit);

const firstValue = (...values) => {
  for (const value of values) {
    const normalized = cleanString(value, 1024);
    if (normalized) return normalized;
  }
  return "";
};

const normalizeStateToken = value => cleanString(value, 80).toLowerCase().replace(/[\s_-]+/g, "-");

const safeGitHubURL = value => {
  value = cleanString(value, 2048);
  if (!value) return "";
  try {
    const parsed = new URL(value);
    return parsed.protocol === "https:" && parsed.hostname.toLowerCase() === "github.com" && !parsed.port && !parsed.username && !parsed.password && !parsed.search && !parsed.hash
      ? parsed.href
      : "";
  } catch {
    return "";
  }
};

const canonicalPullRequestURL = value => {
  if (String(value || "").trim().length > 512) return "";
  const safe = safeGitHubURL(value);
  if (!safe) return "";
  try {
    const match = new URL(safe).pathname.match(/^\/([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+)\/pull\/([1-9]\d*)\/?$/);
    if (!match || [match[1], match[2]].some(component => component === "." || component === ".." || component.length > 100)) return "";
    const number = Number(match[3]);
    if (!Number.isSafeInteger(number) || number > 1_000_000_000) return "";
    return `https://github.com/${match[1]}/${match[2]}/pull/${number}`;
  } catch {
    return "";
  }
};

const isPullRequestURL = value => Boolean(canonicalPullRequestURL(value));

const normalizedInputTag = computed(() => {
  const value = cleanString(tag.value, 160);
  if (!value) return "";
  if (value.toLowerCase() === "head" || /^v/i.test(value)) return value;
  return `v${value}`;
});

const validHeadTag = value => /^(?:head|v?\d+\.\d+-head)$/i.test(cleanString(value, 160));

const validateForm = () => {
  const errors = { pullRequest: "", tag: "" };
  if (!isPullRequestURL(pullRequest.value)) {
    errors.pullRequest = "Paste a full GitHub pull request URL, such as https://github.com/rancher/rancher/pull/12345.";
  }
  if (!validHeadTag(tag.value)) {
    errors.tag = "Enter a head tag such as 2.14-head or head.";
  }
  formErrors.value = errors;
  return !errors.pullRequest && !errors.tag;
};

const verifyBuild = async () => {
  if (!validateForm()) return;

  const canonicalPullRequest = canonicalPullRequestURL(pullRequest.value);
  pullRequest.value = canonicalPullRequest;

  requestController?.abort();
  const controller = new AbortController();
  requestController = controller;
  loading.value = true;
  cancelled.value = false;
  requestError.value = "";
  result.value = null;
  let completed = false;

  try {
    const response = await apiFetch("/api/pr-builds/verify", {
      method: "POST",
      signal: controller.signal,
      body: JSON.stringify({
        pullRequest: canonicalPullRequest,
        tag: cleanString(tag.value, 160),
      }),
    });
    const payload = await response.json();
    if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
      throw new Error("The verifier returned an invalid response.");
    }
    result.value = payload;
    completed = true;
  } catch (error) {
    if (error?.name !== "AbortError") {
      requestError.value = error instanceof Error ? error.message : "PR image verification failed.";
    }
  } finally {
    if (requestController === controller) {
      requestController = null;
      loading.value = false;
      if (completed) {
        await nextTick();
        resultSummaryElement.value?.focus({ preventScroll: true });
      }
    }
  }
};

const cancelVerification = () => {
  requestController?.abort();
  requestController = null;
  loading.value = false;
  requestError.value = "";
  result.value = null;
  cancelled.value = true;
};

const clearResult = () => {
  requestController?.abort();
  requestController = null;
  loading.value = false;
  cancelled.value = false;
  requestError.value = "";
  result.value = null;
};

const registries = computed(() => Array.isArray(result.value?.registries) ? result.value.registries : []);
const summary = computed(() => result.value?.summary && typeof result.value.summary === "object" ? result.value.summary : {});
const pullRequestResult = computed(() => result.value?.pullRequest && typeof result.value.pullRequest === "object" ? result.value.pullRequest : {});
const resultWarnings = computed(() => Array.isArray(result.value?.warnings) ? result.value.warnings.map(value => cleanString(value, 1200)).filter(Boolean) : []);
const scanComplete = computed(() => summary.value.scanComplete !== false);

const imageState = image => {
  if (!image || image.found === false) return image?.error ? "error" : "unavailable";
  if (image.error) return "error";
  const match = image.match && typeof image.match === "object" ? image.match : {};
  const verdict = normalizeStateToken(match.verdict);
  const relation = normalizeStateToken(match.relation);

  if (["exact", "equal", "same"].includes(relation) || ["exact", "exact-revision", "exact-match", "exact-commit"].includes(verdict)) return "exact";
  if (["descendant", "included", "contains", "present", "verified", "candidate-is-descendant", "required-is-ancestor"].includes(relation)
      || ["descendant", "included", "included-descendant", "contains", "contains-commit", "commit-included", "present", "verified", "match"].includes(verdict)) return "descendant";
  if (["not-included", "not-present", "absent", "diverged", "unrelated", "does-not-contain"].includes(verdict)
      || ["not-included", "not-present", "ancestor", "candidate-is-ancestor", "required-is-descendant", "diverged", "unrelated"].includes(relation)) return "not-included";
  return "unknown";
};

const registryState = registryResult => imageState(registryResult?.server);

const registryStats = computed(() => registries.value.reduce((counts, registryResult) => {
  const state = registryState(registryResult);
  counts[state] = (counts[state] || 0) + 1;
  return counts;
}, { exact: 0, descendant: 0, "not-included": 0, unknown: 0, unavailable: 0, error: 0 }));

const overallState = computed(() => {
  const verdict = normalizeStateToken(summary.value.verdict);
  if (["exact", "exact-revision"].includes(verdict)) return "exact";
  if (["included", "fully-included", "present", "verified", "contains", "descendant"].includes(verdict)) return "included";
  if (["not-included", "not-present", "absent"].includes(verdict)) return "not-included";
  if (["error", "failed"].includes(verdict)) return "error";
  if (["unknown", "partial", "mixed", "incomplete"].includes(verdict)) return "unknown";

  const included = registryStats.value.exact + registryStats.value.descendant;
  if (included > 0) return included === registries.value.length ? "included" : "unknown";
  if (registries.value.length && registryStats.value["not-included"] === registries.value.length) return "not-included";
  return "unknown";
});

const statusLabel = state => ({
  exact: "Exact revision",
  descendant: "Included (descendant)",
  included: "Included",
  "not-included": "Not included",
  unknown: "Unknown",
  unavailable: "Image not found",
  error: "Registry error",
}[state] || "Unknown");

const statusBadgeClass = state => ({
  exact: "border-emerald-300 bg-emerald-100 text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-200",
  descendant: "border-sky-300 bg-sky-100 text-sky-800 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-200",
  included: "border-emerald-300 bg-emerald-100 text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-200",
  "not-included": "border-rose-300 bg-rose-100 text-rose-800 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-200",
  error: "border-rose-300 bg-rose-100 text-rose-800 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-200",
  unavailable: "border-zinc-300 bg-zinc-100 text-zinc-700 dark:border-white/15 dark:bg-white/[0.06] dark:text-zinc-300",
  unknown: "border-amber-300 bg-amber-100 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/15 dark:text-amber-200",
}[state] || "border-amber-300 bg-amber-100 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/15 dark:text-amber-200");

const summaryCardClass = computed(() => ({
  exact: "border-emerald-300 bg-emerald-50 text-emerald-950 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-100",
  included: "border-emerald-300 bg-emerald-50 text-emerald-950 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-100",
  "not-included": "border-rose-300 bg-rose-50 text-rose-950 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-100",
  error: "border-rose-300 bg-rose-50 text-rose-950 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-100",
  unknown: "border-amber-300 bg-amber-50 text-amber-950 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-100",
}[overallState.value]));

const includedRegistryCount = computed(() => registryStats.value.exact + registryStats.value.descendant);

const summaryHeading = computed(() => ({
  exact: "The checked image is built from the PR commit",
  included: "The PR fix is included in a checked head image",
  "not-included": "The PR fix is not included in the checked head images",
  error: "Registry verification could not complete",
  unknown: "Inclusion could not be proven across all registries",
}[overallState.value]));

const summaryMessage = computed(() => {
  const backendMessage = firstValue(summary.value.message, summary.value.reason);
  if (backendMessage) return backendMessage;
  const total = registries.value.length;
  if (!total) return "No registry evidence was returned.";
  if (includedRegistryCount.value) {
    return `${includedRegistryCount.value} of ${total} registries have a Rancher server image whose declared source revision includes the required PR commit.`;
  }
  if (registryStats.value["not-included"] === total) {
    return `GitHub ancestry checks showed that none of the ${total} inspected Rancher server images include the required PR commit.`;
  }
  return "At least one image was missing trustworthy revision metadata, was unavailable, or could not be compared through GitHub.";
});

const summaryCount = (keys, fallback) => {
  const counts = summary.value.counts && typeof summary.value.counts === "object" && !Array.isArray(summary.value.counts)
    ? summary.value.counts
    : {};
  for (const source of [counts, summary.value]) {
    for (const key of keys) {
      const value = Number(source[key]);
      if (Number.isFinite(value) && value >= 0) return value;
    }
  }
  return fallback;
};

const summaryCountBadges = computed(() => [
  { label: "Exact", value: summaryCount(["exact", "exactRevision", "exactRevisions"], registryStats.value.exact) },
  { label: "Descendant", value: summaryCount(["descendant", "descendants"], registryStats.value.descendant) },
  { label: "Not included", value: summaryCount(["notIncluded", "not_included", "absent"], registryStats.value["not-included"]) },
  { label: "Unknown", value: summaryCount(["unknown", "unverified"], registryStats.value.unknown) },
  { label: "Unavailable", value: summaryCount(["unavailable", "imageUnavailable", "notFound"], registryStats.value.unavailable) },
  { label: "Errors", value: summaryCount(["errors", "registryErrors", "failed"], registryStats.value.error) },
]);

const firstRequiredRevision = computed(() => {
  for (const registryResult of registries.value) {
    for (const image of [registryResult?.server, registryResult?.agent]) {
      const revision = firstValue(image?.match?.requiredRevision);
      if (revision) return revision;
    }
  }
  return "";
});

const requiredRevision = computed(() => firstValue(
  pullRequestResult.value.requiredRevision,
  pullRequestResult.value.verificationRevision,
  pullRequestResult.value.verificationCommit,
  pullRequestResult.value.inclusionCommitSha,
  pullRequestResult.value.commitSha,
  pullRequestResult.value.commitSHA,
  result.value?.requiredRevision,
  firstRequiredRevision.value,
));

const pullRequestLink = computed(() => safeGitHubURL(firstValue(pullRequestResult.value.url, pullRequestResult.value.htmlUrl, pullRequestResult.value.pullRequestUrl, pullRequest.value)));
const requiredCommitLink = computed(() => safeGitHubURL(firstValue(pullRequestResult.value.inclusionCommitUrl, pullRequestResult.value.commitUrl, pullRequestResult.value.commitURL, pullRequestResult.value.verificationCommitUrl, pullRequestResult.value.verificationCommitURL)));

const pullRequestHeading = computed(() => {
  const number = firstValue(pullRequestResult.value.number);
  const title = firstValue(pullRequestResult.value.title);
  if (number && title) return `#${number} ${title}`;
  if (number) return `Pull request #${number}`;
  return title || "GitHub pull request";
});

const pullRequestDetails = computed(() => [
  { label: "Repository", value: firstValue(pullRequestResult.value.repository, pullRequestResult.value.repositoryName, pullRequestResult.value.repo), mono: true },
  { label: "State", value: firstValue(pullRequestResult.value.state, pullRequestResult.value.merged === true ? "Merged" : ""), mono: false },
  { label: "Base branch", value: firstValue(pullRequestResult.value.baseBranch, pullRequestResult.value.baseRef, pullRequestResult.value.baseRefName), mono: true },
  { label: "Head branch", value: firstValue(pullRequestResult.value.headBranch, pullRequestResult.value.headRef, pullRequestResult.value.headRefName), mono: true },
  { label: "Head repository", value: firstValue(pullRequestResult.value.headRepository), mono: true },
  { label: "Draft", value: pullRequestResult.value.draft === true ? "Yes" : pullRequestResult.value.draft === false ? "No" : "", mono: false },
  { label: "Merged at", value: firstValue(pullRequestResult.value.mergedAt), mono: false },
  { label: "PR head SHA", value: firstValue(pullRequestResult.value.headSha, pullRequestResult.value.headSHA, pullRequestResult.value.headRevision), mono: true, wide: true },
  { label: "Merge commit SHA", value: firstValue(pullRequestResult.value.mergeCommitSha, pullRequestResult.value.mergeCommitSHA, pullRequestResult.value.mergeSha), mono: true, wide: true },
  { label: "Verification commit", value: requiredRevision.value, mono: true, wide: true },
  { label: "Verification basis", value: formatBasis(firstValue(pullRequestResult.value.inclusionBasis, pullRequestResult.value.verificationBasis, pullRequestResult.value.revisionBasis, pullRequestResult.value.requiredRevisionBasis, pullRequestResult.value.requiredRevisionKind)), mono: false, wide: true },
].filter(item => item.value));

const formatBasis = value => ({
  merged_commit: "Merged integration commit",
  pr_head: "Current PR head commit",
}[cleanString(value, 80).toLowerCase()] || cleanString(value, 80));

const resultTag = computed(() => firstValue(result.value?.tag, normalizedInputTag.value) || "head");
const checkedAtLabel = computed(() => {
  const value = result.value?.checkedAt;
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? `Checked ${displayValue(value)}` : `Checked ${date.toLocaleString()}`;
});

const targetDetails = computed(() => [
  { label: "Resolved tag", value: resultTag.value, mono: true },
  { label: "Platform", value: firstValue(result.value?.platform) || "linux/amd64", mono: true },
  { label: "Registries returned", value: String(registries.value.length), mono: false },
  { label: "Scan status", value: scanComplete.value ? "Complete" : "Partial", mono: false },
]);

const registryKey = (registryResult, index) => firstValue(registryResult?.registry, registryResult?.label) || `registry-${index}`;
const registryName = registryResult => firstValue(registryResult?.label, registryResult?.registry) || "Registry";

const pairStatusLabel = registryResult => {
  if (registryResult?.pairAvailable === true) return "Server + agent found";
  if (registryResult?.pairAvailable === false) return "Image pair incomplete";
  return "Pair status unknown";
};

const pairStatusClass = registryResult => {
  if (registryResult?.pairAvailable === true) return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-300";
  if (registryResult?.pairAvailable === false) return "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300";
  return "border-zinc-200 bg-zinc-100 text-zinc-600 dark:border-white/10 dark:bg-white/[0.05] dark:text-zinc-300";
};

const imageEntries = registryResult => [
  { role: "server", label: "Rancher server", image: registryResult?.server || null },
  { role: "agent", label: "Rancher agent", image: registryResult?.agent || null },
];

const imageCardClass = image => ({
  exact: "border-emerald-200 bg-emerald-50/50 dark:border-emerald-500/20 dark:bg-emerald-500/[0.06]",
  descendant: "border-sky-200 bg-sky-50/50 dark:border-sky-500/20 dark:bg-sky-500/[0.06]",
  "not-included": "border-rose-200 bg-rose-50/50 dark:border-rose-500/20 dark:bg-rose-500/[0.06]",
  error: "border-rose-200 bg-rose-50/50 dark:border-rose-500/20 dark:bg-rose-500/[0.06]",
  unknown: "border-amber-200 bg-amber-50/50 dark:border-amber-500/20 dark:bg-amber-500/[0.06]",
  unavailable: "border-zinc-200 bg-zinc-50 dark:border-white/10 dark:bg-white/[0.025]",
}[imageState(image)]);

const matchReason = image => {
  const reason = firstValue(image?.match?.reason);
  if (reason) return reason;
  return ({
    exact: "The image declares the exact PR verification commit.",
    descendant: "GitHub confirms that the image revision descends from the PR verification commit.",
    "not-included": "GitHub confirms that the image revision does not descend from the PR verification commit.",
    unknown: "The available image metadata could not prove whether the PR commit is included.",
    error: "The registry or GitHub comparison returned an error.",
    unavailable: "The requested image was not found.",
  }[imageState(image)]);
};

const imageOverview = image => [
  { label: "Manifest digest", value: firstValue(image?.digest), mono: true, wide: true },
  { label: "Platform digest", value: firstValue(image?.platformDigest), mono: true, wide: true },
  { label: "Platform", value: firstValue(image?.platform), mono: true },
  { label: "Build tag", value: firstValue(image?.buildVersion), mono: true },
].filter(item => item.value);

const imageEvidence = image => [
  { label: "Required PR revision", value: firstValue(image?.match?.requiredRevision, requiredRevision.value) },
  { label: "Compared image revision", value: firstValue(image?.match?.candidateRevision, image?.ossRevision, image?.revision) },
  { label: "Revision label", value: firstValue(image?.match?.revisionLabel) },
  { label: "Comparison basis", value: firstValue(image?.match?.basis) },
  { label: "Declared source", value: firstValue(image?.sourceUrl) },
  { label: "Declared revision", value: firstValue(image?.revision) },
  { label: "Declared OSS revision", value: firstValue(image?.ossRevision) },
].filter(item => item.value);

const imageLinks = image => {
  const candidates = [
    { label: "View comparison", href: safeGitHubURL(image?.match?.compareUrl) },
    { label: "View image commit", href: safeGitHubURL(image?.match?.commitUrl) },
    { label: "Declared source", href: safeGitHubURL(image?.sourceUrl) },
  ];
  const seen = new Set();
  return candidates.filter(candidate => {
    if (!candidate.href || seen.has(candidate.href)) return false;
    seen.add(candidate.href);
    return true;
  });
};

onBeforeUnmount(() => {
  requestController?.abort();
  requestController = null;
});
</script>
