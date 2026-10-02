package test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	goversion "github.com/hashicorp/go-version"
	"log"
	"regexp"
	"strings"
	"time"
)

type supportedRKE2Resolution struct {
	HighestMinor int
	Explanation  string
	ResolvedURL  string
}

func resolveSupportMatrixWithCache(cache map[string]supportedRKE2Resolution, supportMatrixURL string) (supportedRKE2Resolution, error) {
	if cached, ok := cache[supportMatrixURL]; ok {
		log.Printf("[resolver] Reusing support matrix result for %s (RKE2 v1.%d)", supportMatrixURL, cached.HighestMinor)
		return cached, nil
	}

	startedAt := time.Now()
	log.Printf("[resolver] Resolving SUSE support matrix for %s...", supportMatrixURL)
	highestMinor, explanation, resolvedURL, err := resolveHighestSupportedRKE2Minor(supportMatrixURL)
	if err != nil {
		log.Printf("[resolver] Support matrix resolution failed after %s: %v", time.Since(startedAt).Round(time.Millisecond), err)
		return supportedRKE2Resolution{}, err
	}
	resolved := supportedRKE2Resolution{
		HighestMinor: highestMinor,
		Explanation:  explanation,
		ResolvedURL:  resolvedURL,
	}
	cache[supportMatrixURL] = resolved
	log.Printf("[resolver] Support matrix resolved in %s: RKE2 through v1.%d", time.Since(startedAt).Round(time.Millisecond), highestMinor)
	return resolved, nil
}

func resolveHighestSupportedRKE2Minor(supportMatrixURL string) (int, string, string, error) {
	page, err := resolveSupportMatrixPage(supportMatrixURL)
	if err != nil {
		highest, summary, resolvedURL, cacheErr := resolveCachedSupportRange("RKE2", supportMatrixURL, err)
		return highest, summary, resolvedURL, cacheErr
	}

	textContent, err := extractTextFromHTML(page.Body)
	if err != nil {
		highest, summary, resolvedURL, cacheErr := resolveCachedSupportRange("RKE2", page.URL, fmt.Errorf("failed to parse support matrix page: %w", err))
		return highest, supportMatrixSummary(page, summary), resolvedURL, cacheErr
	}

	rke2RangePattern := regexp.MustCompile(`(?i)RKE2\s+v1\.(\d+)\s+v1\.(\d+)`)
	matches := rke2RangePattern.FindStringSubmatch(textContent)
	if len(matches) != 3 {
		highest, summary, resolvedURL, cacheErr := resolveCachedSupportRange("RKE2", page.URL, fmt.Errorf("could not find supported RKE2 range in support matrix page"))
		return highest, supportMatrixSummary(page, summary), resolvedURL, cacheErr
	}

	highestMinorVersion, err := goversion.NewVersion(matches[2])
	if err != nil {
		return 0, "", page.URL, fmt.Errorf("failed to parse supported RKE2 minor %q: %w", matches[2], err)
	}

	majorSegments := strings.Split(highestMinorVersion.Original(), ".")
	if len(majorSegments) == 0 {
		return 0, "", page.URL, fmt.Errorf("unexpected supported RKE2 minor value %q", highestMinorVersion.Original())
	}

	var highestMinor int
	fmt.Sscanf(matches[2], "%d", &highestMinor)
	var minMinor int
	fmt.Sscanf(matches[1], "%d", &minMinor)
	rangeText := fmt.Sprintf("Support matrix certifies RKE2 from v1.%s through v1.%s", matches[1], matches[2])
	cacheResolvedSupportMatrixRange("RKE2", page, rangeText, minMinor, highestMinor)
	return highestMinor, supportMatrixSummary(page, rangeText), page.URL, nil
}

func resolveLatestRKE2Patch(highestMinor int) (string, error) {
	releaseNotesURL := fmt.Sprintf("https://docs.rke2.io/release-notes/v1.%d.X", highestMinor)
	config := releaseProductConfig{
		ProductName:       "RKE2",
		CacheKey:          "rke2",
		Pattern:           regexp.MustCompile(fmt.Sprintf(`v1\.%d\.\d+\+rke2r\d+`, highestMinor)),
		GitHubTagRefsURL:  fmt.Sprintf("https://api.github.com/repos/rancher/rke2/git/matching-refs/tags/v1.%d.", highestMinor),
		GitHubBuildPrefix: "+rke2",
		GitHubReleaseURL:  "https://api.github.com/repos/rancher/rke2/releases/tags/%s",
		GitHubAssetNames: []string{
			"rke2-images.linux-amd64.tar.zst",
			"sha256sum-amd64.txt",
		},
	}
	return resolveLatestCachedReleasePatch(config, highestMinor, releaseNotesURL, firstReleaseVersion)
}

func resolveRKE2PatchWithCache(cache map[int]string, highestMinor int) (string, error) {
	if cached := cache[highestMinor]; cached != "" {
		log.Printf("[resolver] Reusing latest RKE2 patch for v1.%d: %s", highestMinor, cached)
		return cached, nil
	}

	startedAt := time.Now()
	log.Printf("[resolver] Resolving latest RKE2 patch for v1.%d...", highestMinor)
	resolved, err := resolveLatestRKE2Patch(highestMinor)
	if err != nil {
		log.Printf("[resolver] RKE2 v1.%d patch resolution failed after %s: %v", highestMinor, time.Since(startedAt).Round(time.Millisecond), err)
		return "", err
	}
	cache[highestMinor] = resolved
	log.Printf("[resolver] Latest RKE2 v1.%d patch resolved in %s: %s", highestMinor, time.Since(startedAt).Round(time.Millisecond), resolved)
	return resolved, nil
}

type manualRKE2RecommendationResult struct {
	Index                  int    `json:"index"`
	OK                     bool   `json:"ok"`
	Summary                string `json:"summary"`
	Detail                 string `json:"detail,omitempty"`
	RancherVersion         string `json:"rancherVersion,omitempty"`
	ChartVersion           string `json:"chartVersion,omitempty"`
	CompatibilityBaseline  string `json:"compatibilityBaseline,omitempty"`
	RecommendedRKE2Version string `json:"recommendedRKE2Version,omitempty"`
	KubernetesVersion      string `json:"kubernetesVersion,omitempty"`
	SupportMatrixURL       string `json:"supportMatrixUrl,omitempty"`
}

func recommendManualRKE2Versions(helmCommands []string) []manualRKE2RecommendationResult {
	results := make([]manualRKE2RecommendationResult, 0, len(helmCommands))
	repoAliases := helmRepoAliasesFromCommands(helmCommands)
	if err := ensureRancherHelmRepos(repoAliases, true); err != nil {
		for i := range helmCommands {
			results = append(results, manualRKE2RecommendationResult{
				Index:   i,
				Summary: "Helm repo setup failed",
				Detail:  err.Error(),
			})
		}
		return results
	}
	if err := refreshHelmRepoIndexes(); err != nil {
		for i := range helmCommands {
			results = append(results, manualRKE2RecommendationResult{
				Index:   i,
				Summary: "Helm repo update failed",
				Detail:  err.Error(),
			})
		}
		return results
	}

	for i, command := range helmCommands {
		result := manualRKE2RecommendationResult{Index: i}
		recommended, err := recommendManualRKE2Version(command, &result)
		if err != nil {
			result.Summary = "Could not recommend RKE2"
			result.Detail = err.Error()
			results = append(results, result)
			continue
		}
		result.OK = true
		result.Summary = "Recommended RKE2 version found"
		result.RecommendedRKE2Version = recommended
		result.KubernetesVersion = helmKubeVersionFromRKE2Version(recommended)
		results = append(results, result)
	}
	return results
}

func recommendManualRKE2Version(helmCommand string, result *manualRKE2RecommendationResult) (string, error) {
	fields, err := parseHelmCommandFields(helmCommand)
	if err != nil {
		return "", err
	}
	invocation, err := manualHelmInvocationFromFields(fields)
	if err != nil {
		return "", err
	}
	repoAlias := strings.TrimSuffix(invocation.chartRef, "/rancher")
	if repoAlias == "" || repoAlias == invocation.chartRef {
		return "", fmt.Errorf("chart reference must look like rancher-latest/rancher")
	}

	chartVersion := helmFlagValue(fields, "--version")
	if chartVersion == "" {
		return "", fmt.Errorf("add --version to the Rancher Helm command so the support matrix can be selected")
	}
	requestedVersion := imagelookup.NormalizeVersionInput(chartVersion)
	result.RancherVersion = requestedVersion
	result.ChartVersion = chartVersion

	buildType, minorLine, err := classifyRancherVersion(requestedVersion)
	if err != nil {
		return "", err
	}
	compatibilityBaseline := requestedVersion
	if buildType != "release" {
		_, resolvedChartVersion, resolvedBaseline, err := resolveChartAndBaseline([]string{repoAlias}, requestedVersion, minorLine, buildType)
		if err != nil {
			return "", err
		}
		result.ChartVersion = resolvedChartVersion
		compatibilityBaseline = resolvedBaseline
	}
	result.CompatibilityBaseline = compatibilityBaseline
	supportMatrixURL := buildSupportMatrixURL(compatibilityBaseline)
	result.SupportMatrixURL = supportMatrixURL
	highestRKE2Minor, supportExplanation, resolvedSupportMatrixURL, err := resolveHighestSupportedRKE2Minor(supportMatrixURL)
	if err != nil {
		return "", err
	}
	result.SupportMatrixURL = resolvedSupportMatrixURL
	result.Detail = supportExplanation
	return resolveLatestRKE2Patch(highestRKE2Minor)
}

func resolveInstallerSHA256(rke2Version string) (string, error) {
	installScriptURL := fmt.Sprintf("https://raw.githubusercontent.com/rancher/rke2/%s/install.sh", rke2Version)
	body, err := fetchURLBody(installScriptURL)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:]), nil
}

func resolveInstallerSHA256WithCache(cache map[string]string, rke2Version string) (string, error) {
	if cached := cache[rke2Version]; cached != "" {
		log.Printf("[resolver] Reusing installer SHA256 for %s", rke2Version)
		return cached, nil
	}

	startedAt := time.Now()
	log.Printf("[resolver] Fetching the %s installer to pin its SHA256...", rke2Version)
	resolved, err := resolveInstallerSHA256(rke2Version)
	if err != nil {
		log.Printf("[resolver] Installer SHA256 resolution for %s failed after %s: %v", rke2Version, time.Since(startedAt).Round(time.Millisecond), err)
		return "", err
	}
	cache[rke2Version] = resolved
	log.Printf("[resolver] Installer SHA256 for %s resolved in %s", rke2Version, time.Since(startedAt).Round(time.Millisecond))
	return resolved, nil
}
