<template>
  <div class="image-workspace pr-workspace">
    <ImageWorkspaceHeader title="PR Image Check" eyebrow="Commit to container" icon="branch" description="Check which builds include a pull request." :timestamp="checkedAtLabel"/>
    <form
      class="iw-search-form pr-search-form"
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
    <div v-if="requestError" role="alert" class="iw-alert iw-error"><strong>The check could not finish.</strong><p>{{ requestError }}</p><AppBuildStamp/></div>
    <section v-if="loading" class="iw-progress" role="status"><span class="spinner"></span><div><h3>Following the evidence…</h3><p>Reading the PR, inspecting four registries, and comparing source ancestry.</p><span class="iw-caption">{{ elapsedSeconds }}s elapsed · cancel at any time</span></div><span class="iw-progress-orbit" aria-hidden="true"><Icon name="branch"/></span></section>
    <section v-else-if="!result" class="pr-start">
      <span class="iw-eyebrow">{{ cancelled ? 'Check cancelled' : 'From code review to testable build' }}</span>
      <h3>{{ cancelled ? 'Ready whenever you are.' : 'Has the change reached an image?' }}</h3>
      <p>Follow the PR’s verification commit through the declared image revision. See the evidence for each registry, then inspect the exact image.</p>
      <div class="pr-method"><div><Icon name="branch"/><strong>01 · Resolve the commit</strong><span>Merged integration commit, or the current PR head.</span></div><div><Icon name="boxes"/><strong>02 · Read the images</strong><span>Server and agent manifests across four registries.</span></div><div><Icon name="check"/><strong>03 · Compare ancestry</strong><span>A clear verdict, with the source evidence behind it.</span></div></div>
      <p class="iw-caption">Uses your GitHub CLI login and existing registry credentials.</p>
    </section>
    <template v-else>
      <div v-if="scopeChanged" class="iw-alert" role="status">Inputs changed. These results still describe <strong>{{ resultTag }}</strong> and the previous PR. Run a new check to apply your changes.</div>
      <section ref="resultSummaryElement" tabindex="-1" class="pr-verdict" :data-state="overallState" aria-label="Verification result">
        <div class="pr-verdict-symbol"><Icon :name="includedRegistryCount ? 'check' : 'signal'"/></div>
        <div class="pr-verdict-copy"><div class="iw-inline"><span class="iw-status" :data-state="overallState">{{ statusLabel(overallState) }}</span><span class="iw-caption">{{ scanComplete ? 'Scan complete' : 'Partial scan' }} · {{ registries.length }} registries</span></div><h3>{{ summaryHeading }}</h3><p>{{ summaryMessage }}</p></div>
        <button type="button" class="iw-button" @click="copyEvidence"><Icon name="copy"/>Copy evidence brief</button>
      </section>
      <div class="pr-evidence-path">
        <div><span class="iw-eyebrow">Pull request</span><h3>{{ pullRequestHeading }}</h3><a v-if="pullRequestLink" :href="pullRequestLink" target="_blank" rel="noopener noreferrer">Open PR <Icon name="external"/></a></div>
        <Icon name="arrow" class="pr-path-arrow"/>
        <div><span class="iw-eyebrow">Verification commit</span><code :title="requiredRevision">{{ shortRevision(requiredRevision) || 'Not available' }}</code><span class="iw-caption">{{ formatBasis(pullRequestResult.inclusionBasis) || 'See PR details' }}</span><button v-if="requiredRevision" type="button" class="iw-text-button" @click="copyValue(requiredRevision, 'Verification commit copied.')">Copy SHA</button></div>
        <Icon name="arrow" class="pr-path-arrow"/>
        <div><span class="iw-eyebrow">Target image</span><code>{{ resultTag }}</code><span class="iw-caption">{{ result.platform || 'linux/amd64' }} · observed {{ checkedAtLabel.replace(/^Checked /, '') }}</span><span class="iw-caption">Head tags can move. Re-check before testing.</span></div>
      </div>
      <details class="iw-disclosure pr-source-details"><summary>Pull request &amp; verification details <Icon name="chevron"/></summary><dl class="pr-source-grid"><div v-for="item in pullRequestDetails" :key="item.label"><dt>{{ item.label }}</dt><dd :class="{ 'font-mono': item.mono }">{{ item.value }}</dd></div></dl><a v-if="requiredCommitLink" :href="requiredCommitLink" target="_blank" rel="noopener noreferrer" class="iw-text-button">Open verification commit ↗</a></details>
      <div v-if="resultWarnings.length" class="iw-alert"><strong>Interpretation notes</strong><ul><li v-for="warning in resultWarnings" :key="warning">{{ warning }}</li></ul></div>
      <section class="pr-registry-workspace">
        <div class="iw-section-heading"><div><span class="iw-eyebrow">Across the registries</span><h3>Where the commit shows up</h3><p class="iw-caption">Server ancestry determines the verdict. Agent ancestry and pair availability are shown separately.</p></div><div class="iw-segments" aria-label="Registry result filters"><button v-for="option in registryFilters" :key="option.id" type="button" :aria-pressed="registryFilter === option.id" @click="registryFilter = option.id">{{ option.label }} <span>{{ option.count }}</span></button></div></div>
        <div class="pr-matrix-scroll" v-if="filteredRegistries.length" tabindex="0" role="region" aria-label="Registry comparison, scroll horizontally on narrow screens"><table class="pr-matrix"><thead><tr><th scope="col">Registry</th><th scope="col">Server ancestry</th><th scope="col">Agent ancestry</th><th scope="col">Image pair</th><th scope="col"><span class="sr-only">Actions</span></th></tr></thead><tbody><tr v-for="(item,index) in filteredRegistries" :key="registryKey(item,index)"><th scope="row"><button type="button" @click="focusRegistry(item,index)">{{ registryName(item) }} <Icon name="chevron"/></button><small>{{ item.registry }}</small></th><td><span class="iw-status" :data-state="registryState(item)">{{ statusLabel(registryState(item)) }}</span><code :title="item.server?.match?.candidateRevision">{{ shortRevision(item.server?.match?.candidateRevision) }}</code></td><td><span class="iw-status" :data-state="imageState(item.agent)">{{ statusLabel(imageState(item.agent)) }}</span></td><td><span class="pr-pair" :data-complete="item.pairAvailable"><Icon :name="item.pairAvailable ? 'check' : 'signal'"/>{{ item.pairAvailable ? 'Both found' : 'Incomplete' }}</span></td><td><button v-if="pinnedReference(item.server)" type="button" class="iw-button" @click="inspectImage(item.server)">Inspect server <Icon name="arrow"/></button><button v-else type="button" class="iw-text-button" @click="focusRegistry(item,index)">View evidence</button></td></tr></tbody></table></div>
        <div v-else class="iw-empty-small">{{ registries.length ? 'No registries match this view.' : 'No registry evidence was returned.' }}<button v-if="registries.length" type="button" class="iw-text-button" @click="registryFilter = 'all'">Show all registries</button></div>
        <p v-if="filteredRegistries.length" class="pr-matrix-hint iw-caption">Scroll sideways to compare all image evidence.</p>
        <div class="pr-registry-details">
          <details v-for="(registryResult, registryIndex) in filteredRegistries" :key="registryKey(registryResult, registryIndex)" :id="`pr-evidence-${registryKey(registryResult,registryIndex)}`" :open="expandedRegistry === registryKey(registryResult,registryIndex)" class="pr-registry-detail">
            <summary><span class="pr-registry-letter">{{ registryName(registryResult).slice(0,1) }}</span><span><strong>{{ registryName(registryResult) }}</strong><small>{{ pairStatusLabel(registryResult) }}</small></span><span class="iw-status" :data-state="registryState(registryResult)">{{ statusLabel(registryState(registryResult)) }}</span><Icon name="chevron"/></summary>
          <div class="grid min-w-0 gap-3 p-3 lg:grid-cols-2 lg:p-4">
            <section v-for="imageEntry in imageEntries(registryResult)" :key="imageEntry.role" class="min-w-0 rounded-xl border p-3.5" :class="imageCardClass(imageEntry.image)">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h5 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">{{ imageEntry.label }}</h5>
                <span class="rounded-full border px-2.5 py-0.5 text-[10px] font-extrabold" :class="statusBadgeClass(imageState(imageEntry.image))">{{ statusLabel(imageState(imageEntry.image)) }}</span>
              </div>

              <template v-if="imageEntry.image && imageEntry.image.found !== false">
                <p class="mt-3 break-all font-mono text-[11px] font-semibold leading-5 text-zinc-700 dark:text-zinc-300">{{ displayValue(imageEntry.image.reference, 1024) || "Image reference unavailable" }}</p>
                <p class="mt-2 text-xs leading-5 text-zinc-600 dark:text-zinc-300">{{ matchReason(imageEntry.image) }}</p><div v-if="pinnedReference(imageEntry.image)" class="iw-inline mt-3"><button type="button" class="iw-text-button" @click="copyValue(pinnedReference(imageEntry.image), 'Digest-pinned reference copied.')"><Icon name="copy"/>Copy pinned reference</button><button type="button" class="iw-text-button" @click="inspectImage(imageEntry.image)">Inspect this image <Icon name="arrow"/></button></div>

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

          </details>
        </div>
      </section>
      <details class="iw-disclosure pr-proof-limits"><summary>What this evidence can—and cannot—tell you <Icon name="chevron"/></summary><ul><li>A digest identifies image content. A Git SHA identifies source; the two are never compared directly.</li><li>Image labels are producer-declared. Commit ancestry is evidence, not a binary attestation, and a later commit may revert the change.</li><li>Unknown is not proof of absence. Equivalent cherry-picks and backports have different SHAs.</li><li>These results describe the observed digests. Mutable head tags can change after this check.</li></ul></details>
    </template>
    <footer class="iw-footer"><span>GitHub + OCI registry evidence</span><span>Inspection only · no registry or cluster changes</span></footer>
    <div v-if="copyNotice" class="iw-toast" role="status"><Icon name="check"/><span>{{ copyNotice }}</span><button type="button" aria-label="Dismiss notification" @click="copyNotice = ''"><Icon name="close"/></button></div>
  </div>
</template>

<script setup>
import AppBuildStamp from "./AppBuildStamp.vue";
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { apiFetch, activeTab } from "./store.js";
import { writeTextToClipboard } from './clipboard.js';
import Icon from './HelmLabIcon.vue';
import ImageWorkspaceHeader from './ImageWorkspaceHeader.vue';
import { readImageJSON, digestReference, imageEvidenceState, evidenceStateLabel, buildPRImageBrief } from './image-workspace.mjs';

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
  if (loading.value || !validateForm()) return;

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
    const payload = await readImageJSON(signal => apiFetch("/api/pr-builds/verify", {
      method: "POST",
      signal,
      body: JSON.stringify({
        pullRequest: canonicalPullRequest,
        tag: cleanString(tag.value, 160),
      }),
    }), controller, "PR image check", 130000);
    if (disposed || requestController !== controller || controller.signal.aborted) return;
    if (!payload || typeof payload !== "object" || !Array.isArray(payload.registries)) {
      throw new Error("The verifier returned an invalid response.");
    }
    result.value = payload;
    checkedScope.value = { pullRequest: canonicalPullRequest, tag: normalizedInputTag.value };
    registryFilter.value = "all"; expandedRegistry.value = "";
    completed = true;
  } catch (error) {
    if (!disposed && requestController === controller && error?.name !== "AbortError") {
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
const scanComplete = computed(() => summary.value.scanComplete === true);

const imageState = imageEvidenceState;

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

const statusLabel = evidenceStateLabel;

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
  exact: "Exact source revision confirmed",
  included: "Commit ancestry confirmed in a checked image",
  "not-included": "No matching ancestry in the checked images",
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


const copyNotice = ref(''), registryFilter = ref('all'), expandedRegistry = ref(''), checkedScope = ref(null), elapsedSeconds = ref(0);
let disposed = false, noticeTimer, elapsedTimer;
watch(loading, value => { clearInterval(elapsedTimer); elapsedSeconds.value = 0; if (value) { const started = Date.now(); elapsedTimer = setInterval(() => { elapsedSeconds.value = Math.floor((Date.now() - started) / 1000); }, 1000); } });
const scopeChanged = computed(() => !!result.value && !!checkedScope.value && (canonicalPullRequestURL(pullRequest.value) !== checkedScope.value.pullRequest || normalizedInputTag.value !== checkedScope.value.tag));
const pinnedReference = image => image?.found === false ? '' : digestReference(image?.reference, image?.digest);
const shortRevision = value => value ? String(value).slice(0,12) : '';
const registryFilters = computed(() => [{ id:'all', label:'All', count:registries.value.length }, { id:'included', label:'Confirmed', count:includedRegistryCount.value }, { id:'attention', label:'Needs review', count:registries.value.length-includedRegistryCount.value }]);
const filteredRegistries = computed(() => registries.value.filter(item => registryFilter.value === 'all' || (registryFilter.value === 'included') === ['exact','descendant'].includes(registryState(item))));
async function focusRegistry(item,index) { expandedRegistry.value = registryKey(item,index); await nextTick(); const element = document.getElementById(`pr-evidence-${expandedRegistry.value}`); element?.scrollIntoView({block:'nearest',behavior:'instant'}); element?.querySelector('summary')?.focus({preventScroll:true}); }
async function copyValue(value,message) { try { await writeTextToClipboard(value); if (!disposed) copyNotice.value=message; } catch(error) { if (!disposed) copyNotice.value=error.message || 'Could not copy.'; } clearTimeout(noticeTimer); if (!disposed) noticeTimer=setTimeout(()=>{copyNotice.value='';},6000); }
function copyEvidence() { if (result.value) copyValue(buildPRImageBrief(result.value),'Evidence brief copied with the observed digests and check time.'); }
async function inspectImage(image) { const reference=pinnedReference(image); if (!reference) return; activeTab.value='images'; await nextTick(); window.dispatchEvent(new CustomEvent('rancher-control-panel:inspect-image',{detail:{reference,platform:image.platform || 'linux/amd64'}})); }

onBeforeUnmount(() => {
  disposed = true; clearTimeout(noticeTimer); clearInterval(elapsedTimer);
  requestController?.abort();
  requestController = null;
});
</script>
