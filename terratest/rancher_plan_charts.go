package test

import (
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	goversion "github.com/hashicorp/go-version"
	"log"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

type helmSearchCacheEntry struct {
	results []helmSearchResult
	err     error
}

type helmChartSearchSession struct {
	globalLoaded  bool
	globalResults []helmSearchResult
	globalErr     error
	repoResults   map[string]helmSearchCacheEntry
}

func newHelmChartSearchSession() *helmChartSearchSession {
	return &helmChartSearchSession{repoResults: map[string]helmSearchCacheEntry{}}
}

func (session *helmChartSearchSession) searchAllVersions() ([]helmSearchResult, error) {
	if session.globalLoaded {
		log.Printf("[resolver] Reusing Helm chart index search results (%d entries)", len(session.globalResults))
		return session.globalResults, session.globalErr
	}

	session.globalLoaded = true
	session.globalResults, session.globalErr = searchAllHelmRepoVersions()
	return session.globalResults, session.globalErr
}

func (session *helmChartSearchSession) searchRepoVersions(repoAlias string) ([]helmSearchResult, error) {
	if cached, ok := session.repoResults[repoAlias]; ok {
		log.Printf("[resolver] Reusing Helm chart versions for %s (%d entries)", repoAlias, len(cached.results))
		return cached.results, cached.err
	}

	if session.globalLoaded && session.globalErr == nil {
		filtered := filterHelmSearchResultsByRepoAlias(session.globalResults, repoAlias)
		if len(filtered) > 0 {
			session.repoResults[repoAlias] = helmSearchCacheEntry{results: filtered}
			log.Printf("[resolver] Reusing global Helm chart index for %s (%d entries)", repoAlias, len(filtered))
			return filtered, nil
		}
	}

	results, err := searchHelmRepoVersions(repoAlias)
	session.repoResults[repoAlias] = helmSearchCacheEntry{results: results, err: err}
	return results, err
}

func resolveChartAndBaseline(repoCandidates []string, requestedVersion, minorLine, buildType string) (string, string, string, error) {
	return resolveChartAndBaselineWithSearchSession(newHelmChartSearchSession(), repoCandidates, requestedVersion, minorLine, buildType)
}

func resolveChartAndBaselineWithSearchSession(session *helmChartSearchSession, repoCandidates []string, requestedVersion, minorLine, buildType string) (string, string, string, error) {
	startedAt := time.Now()
	log.Printf("[resolver] Resolving Rancher chart for %s across repos %s...", requestedVersion, strings.Join(repoCandidates, ", "))
	if buildType == "head" && requestedVersion == "head" {
		for _, repoAlias := range repoCandidates {
			if repoAlias != "rancher-latest" {
				continue
			}
			results, err := session.searchRepoVersions(repoAlias)
			if err != nil {
				log.Printf("[resolver] Repo candidate %s query failed for Rancher head: %v", repoAlias, err)
				continue
			}
			latestRelease, err := findLatestRelease(results)
			if err != nil {
				log.Printf("[resolver] Repo candidate %s inspection for head: latestRelease=<none>", repoAlias)
				continue
			}
			log.Printf("[resolver] Repo candidate %s inspection for head: latestRelease=%s (resolved in %s)", repoAlias, latestRelease, time.Since(startedAt).Round(time.Millisecond))
			return repoAlias, latestRelease, latestRelease, nil
		}
		return "", "", "", fmt.Errorf("could not resolve the latest rancher-latest chart for head")
	}

	if globalExactMatch, err := findExactRequestedChartAcrossReposWithSearchSession(session, repoCandidates, requestedVersion); err == nil {
		compatibilityBaseline := requestedVersion
		if buildType != "release" {
			compatibilityBaseline, err = resolveCompatibilityBaselineWithSearchSession(session, minorLine)
			if err != nil {
				compatibilityBaseline = requestedVersion
			}
		}
		log.Printf("[resolver] Global exact Rancher chart match selected for %s: %s/rancher@%s (resolved in %s)", requestedVersion, globalExactMatch.repoAlias, globalExactMatch.chartVersion, time.Since(startedAt).Round(time.Millisecond))
		return globalExactMatch.repoAlias, globalExactMatch.chartVersion, compatibilityBaseline, nil
	}

	var lastErr error
	var bestMatch *resolvedChartMatch
	for _, repoAlias := range repoCandidates {
		results, err := session.searchRepoVersions(repoAlias)
		if err != nil {
			log.Printf("[resolver] Repo candidate %s query failed for Rancher %s: %v", repoAlias, requestedVersion, err)
			lastErr = err
			continue
		}
		if len(results) == 0 {
			log.Printf("[resolver] Repo candidate %s returned no Rancher chart versions for %s", repoAlias, requestedVersion)
			continue
		}

		switch buildType {
		case "release":
			hasExactRequested := hasChartVersion(results, requestedVersion)
			log.Printf("[resolver] Repo candidate %s inspection for release %s: exactRequested=%t", repoAlias, requestedVersion, hasExactRequested)
			if hasExactRequested {
				recordResolvedChartMatch(&bestMatch, repoAlias, requestedVersion, requestedVersion, 0)
			}
		default:
			sameMinorRelease, sameMinorReleaseErr := findLatestMinorRelease(results, minorLine)
			compatibilityBaseline, baselineErr := resolveCompatibilityBaselineWithSearchSession(session, minorLine)
			hasExactRequested := hasChartVersion(results, requestedVersion)
			hasCompatibilityBaseline := baselineErr == nil && hasChartVersion(results, compatibilityBaseline)
			if sameMinorReleaseErr != nil {
				log.Printf("[resolver] Repo candidate %s inspection for %s: exactRequested=%t sameMinorRelease=<none> fallbackBaseline=%s fallbackPresent=%t", repoAlias, requestedVersion, hasExactRequested, summarizeBaselineLogValue(compatibilityBaseline, baselineErr), hasCompatibilityBaseline)
			} else {
				log.Printf("[resolver] Repo candidate %s inspection for %s: exactRequested=%t sameMinorRelease=%s fallbackBaseline=%s fallbackPresent=%t", repoAlias, requestedVersion, hasExactRequested, sameMinorRelease, summarizeBaselineLogValue(compatibilityBaseline, baselineErr), hasCompatibilityBaseline)
			}

			if hasChartVersion(results, requestedVersion) {
				if baselineErr != nil {
					compatibilityBaseline = requestedVersion
				}
				recordResolvedChartMatch(&bestMatch, repoAlias, requestedVersion, compatibilityBaseline, 0)
			}

			if sameMinorReleaseErr == nil {
				if baselineErr != nil {
					compatibilityBaseline = sameMinorRelease
				}
				recordResolvedChartMatch(&bestMatch, repoAlias, sameMinorRelease, compatibilityBaseline, 1)
			}

			if baselineErr == nil && hasChartVersion(results, compatibilityBaseline) {
				recordResolvedChartMatch(&bestMatch, repoAlias, compatibilityBaseline, compatibilityBaseline, 2)
			}
			lastErr = sameMinorReleaseErr
		}
	}

	if bestMatch != nil {
		log.Printf("[resolver] Rancher chart selected for %s: %s/rancher@%s (resolved in %s)", requestedVersion, bestMatch.repoAlias, bestMatch.chartVersion, time.Since(startedAt).Round(time.Millisecond))
		return bestMatch.repoAlias, bestMatch.chartVersion, bestMatch.compatibilityBaseline, nil
	}

	if lastErr != nil {
		return "", "", "", lastErr
	}
	return "", "", "", fmt.Errorf("could not resolve a Rancher chart version for %s from repos %s", requestedVersion, strings.Join(repoCandidates, ", "))
}

func recordResolvedChartMatch(bestMatch **resolvedChartMatch, repoAlias, chartVersion, compatibilityBaseline string, matchRank int) {
	if *bestMatch == nil || matchRank < (*bestMatch).matchRank {
		*bestMatch = &resolvedChartMatch{
			repoAlias:             repoAlias,
			chartVersion:          chartVersion,
			compatibilityBaseline: compatibilityBaseline,
			matchRank:             matchRank,
		}
	}
}

func findExactRequestedChartAcrossRepos(repoCandidates []string, requestedVersion string) (*resolvedChartMatch, error) {
	return findExactRequestedChartAcrossReposWithSearchSession(newHelmChartSearchSession(), repoCandidates, requestedVersion)
}

func findExactRequestedChartAcrossReposWithSearchSession(session *helmChartSearchSession, repoCandidates []string, requestedVersion string) (*resolvedChartMatch, error) {
	globalResults, err := session.searchAllVersions()
	if err != nil {
		return nil, err
	}

	for _, repoAlias := range repoCandidates {
		for _, result := range globalResults {
			if result.Name != fmt.Sprintf("%s/rancher", repoAlias) {
				continue
			}
			if result.Version == requestedVersion || imagelookup.NormalizeVersionInput(result.AppVersion) == requestedVersion {
				return &resolvedChartMatch{
					repoAlias:    repoAlias,
					chartVersion: result.Version,
					matchRank:    0,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no exact chart match found across repos for Rancher %s", requestedVersion)
}

func summarizeBaselineLogValue(compatibilityBaseline string, err error) string {
	if err != nil {
		return fmt.Sprintf("<unresolved: %v>", err)
	}
	return compatibilityBaseline
}

func resolveCompatibilityBaseline(minorLine string) (string, error) {
	return resolveCompatibilityBaselineWithSearchSession(newHelmChartSearchSession(), minorLine)
}

func resolveCompatibilityBaselineWithSearchSession(session *helmChartSearchSession, minorLine string) (string, error) {
	baseline, err := resolveReleasedCompatibilityBaselineWithSearchSession(session, minorLine)
	if err == nil {
		return baseline, nil
	}

	previousMinorLine, previousErr := previousRancherMinorLine(minorLine)
	if previousErr != nil {
		return "", err
	}

	return resolveReleasedCompatibilityBaselineWithSearchSession(session, previousMinorLine)
}

func resolveReleasedCompatibilityBaseline(minorLine string) (string, error) {
	return resolveReleasedCompatibilityBaselineWithSearchSession(newHelmChartSearchSession(), minorLine)
}

func resolveReleasedCompatibilityBaselineWithSearchSession(session *helmChartSearchSession, minorLine string) (string, error) {
	releaseRepos := []string{"rancher-latest", "rancher-prime"}
	var bestVersion *goversion.Version

	for _, repoAlias := range releaseRepos {
		results, err := session.searchRepoVersions(repoAlias)
		if err != nil {
			continue
		}

		versionString, err := findLatestMinorRelease(results, minorLine)
		if err != nil {
			continue
		}

		parsed, err := goversion.NewVersion(versionString)
		if err != nil {
			continue
		}

		if bestVersion == nil || parsed.GreaterThan(bestVersion) {
			bestVersion = parsed
		}
	}

	if bestVersion == nil {
		return "", fmt.Errorf("no released compatibility baseline found for Rancher %s.x", minorLine)
	}

	return bestVersion.Original(), nil
}

func previousRancherMinorLine(minorLine string) (string, error) {
	parts := strings.Split(minorLine, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid Rancher minor line %q", minorLine)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid Rancher major version in %q: %w", minorLine, err)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid Rancher minor version in %q: %w", minorLine, err)
	}
	if minor == 0 {
		return "", fmt.Errorf("no earlier Rancher minor line exists before %s", minorLine)
	}

	return fmt.Sprintf("%d.%d", major, minor-1), nil
}

func searchHelmRepoVersions(repoAlias string) ([]helmSearchResult, error) {
	chartRef := fmt.Sprintf("%s/rancher", repoAlias)
	output, err := exec.Command("helm", "search", "repo", chartRef, "--devel", "--versions", "-o", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to query helm repo %s: %w", repoAlias, err)
	}

	results, err := parseHelmSearchResults(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse helm search results for %s: %w", repoAlias, err)
	}
	// `helm search repo` treats chartRef as a fuzzy search term. For example,
	// querying rancher-latest/rancher can also return
	// optimus-rancher-latest/rancher. Keep only the exact chart here so a head
	// version published by Optimus cannot be attributed to rancher-latest and
	// later rendered into an install command for the wrong repository.
	results = filterHelmSearchResultsByRepoAlias(results, repoAlias)
	if len(results) > 0 {
		return results, nil
	}

	globalResults, err := searchAllHelmRepoVersions()
	if err != nil {
		return results, nil
	}

	filteredResults := filterHelmSearchResultsByRepoAlias(globalResults, repoAlias)
	if len(filteredResults) > 0 {
		log.Printf("[resolver] Falling back to global helm search results for repo %s", repoAlias)
		return filteredResults, nil
	}

	return results, nil
}

func searchAllHelmRepoVersions() ([]helmSearchResult, error) {
	// Helm's regexp search can omit prerelease rows even when it returns stable
	// versions for the same chart. Use the plain keyword search so exact head
	// versions remain visible; callers filter the fuzzy results by repo/chart.
	startedAt := time.Now()
	log.Printf("[resolver] Searching Helm chart indexes for exact Rancher versions...")
	output, err := exec.Command("helm", "search", "repo", "rancher", "--devel", "--versions", "-o", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to query helm repo globally for rancher charts: %w", err)
	}

	results, err := parseHelmSearchResults(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse global helm search results: %w", err)
	}
	log.Printf("[resolver] Helm chart index search completed in %s (%d entries)", time.Since(startedAt).Round(time.Millisecond), len(results))
	return results, nil
}

func parseHelmSearchResults(output []byte) ([]helmSearchResult, error) {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, fmt.Errorf("empty helm search output")
	}

	if !strings.HasPrefix(trimmed, "[") {
		jsonStart := strings.Index(trimmed, "[")
		if jsonStart < 0 {
			return nil, fmt.Errorf("helm search output did not contain a JSON array")
		}
		trimmed = strings.TrimSpace(trimmed[jsonStart:])
	}

	var results []helmSearchResult
	if err := json.Unmarshal([]byte(trimmed), &results); err != nil {
		return nil, err
	}
	return results, nil
}

func filterHelmSearchResultsByRepoAlias(results []helmSearchResult, repoAlias string) []helmSearchResult {
	chartRef := repoAlias + "/rancher"
	filteredResults := make([]helmSearchResult, 0)
	for _, result := range results {
		if result.Name == chartRef {
			filteredResults = append(filteredResults, result)
		}
	}
	return filteredResults
}

func hasChartVersion(results []helmSearchResult, version string) bool {
	for _, result := range results {
		if result.Version == version {
			return true
		}
	}
	return false
}

func findLatestMinorRelease(results []helmSearchResult, minorLine string) (string, error) {
	var candidates []*goversion.Version
	for _, result := range results {
		if !strings.HasPrefix(result.Version, minorLine+".") {
			continue
		}
		if strings.Contains(result.Version, "-") {
			continue
		}
		parsed, err := goversion.NewVersion(result.Version)
		if err != nil {
			continue
		}
		candidates = append(candidates, parsed)
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no released chart version found for Rancher %s.x", minorLine)
	}

	slices.SortFunc(candidates, func(a, b *goversion.Version) int {
		return b.Compare(a)
	})
	return candidates[0].Original(), nil
}

func findLatestRelease(results []helmSearchResult) (string, error) {
	var candidates []*goversion.Version
	for _, result := range results {
		if strings.Contains(result.Version, "-") {
			continue
		}
		parsed, err := goversion.NewVersion(result.Version)
		if err != nil {
			continue
		}
		candidates = append(candidates, parsed)
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no released chart version found")
	}

	slices.SortFunc(candidates, func(a, b *goversion.Version) int {
		return b.Compare(a)
	})
	return candidates[0].Original(), nil
}

func rancherMinorLineFromVersion(version string) (string, error) {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("could not derive Rancher minor line from %q", version)
	}
	return strings.Join(parts[:2], "."), nil
}
