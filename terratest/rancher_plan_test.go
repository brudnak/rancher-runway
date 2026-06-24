package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestPreviousRancherMinorLine(t *testing.T) {
	previousMinorLine, err := previousRancherMinorLine("2.15")
	if err != nil {
		t.Fatalf("expected previous Rancher minor line, got error: %v", err)
	}

	if previousMinorLine != "2.14" {
		t.Fatalf("expected previous Rancher minor line 2.14, got %s", previousMinorLine)
	}
}

func TestFindLatestMinorReleaseIgnoresPrereleases(t *testing.T) {
	results := []helmSearchResult{
		{Version: "2.15.0-alpha3"},
		{Version: "2.14.1-rc1"},
		{Version: "2.14.1"},
		{Version: "2.14.0"},
	}

	version, err := findLatestMinorRelease(results, "2.14")
	if err != nil {
		t.Fatalf("expected released chart version, got error: %v", err)
	}

	if version != "2.14.1" {
		t.Fatalf("expected latest released 2.14.x chart version, got %s", version)
	}
}

func TestFindLatestMinorReleaseErrorsWithoutGA(t *testing.T) {
	results := []helmSearchResult{
		{Version: "2.15.0-alpha3"},
		{Version: "2.15.0-rc1"},
	}

	_, err := findLatestMinorRelease(results, "2.15")
	if err == nil {
		t.Fatal("expected an error when no released chart version exists")
	}
}

func TestFindLatestReleaseIgnoresPrereleases(t *testing.T) {
	results := []helmSearchResult{
		{Version: "2.15.0-alpha3"},
		{Version: "2.14.2"},
		{Version: "2.14.1"},
		{Version: "2.13.9"},
	}

	version, err := findLatestRelease(results)
	if err != nil {
		t.Fatalf("expected latest released chart version, got error: %v", err)
	}
	if version != "2.14.2" {
		t.Fatalf("expected latest released chart version 2.14.2, got %s", version)
	}
}

func TestClassifyRancherVersionAllowsPlainHead(t *testing.T) {
	buildType, minorLine, err := classifyRancherVersion("head")
	if err != nil {
		t.Fatalf("expected plain head to be valid, got error: %v", err)
	}
	if buildType != "head" || minorLine != "" {
		t.Fatalf("expected plain head classification, got buildType=%q minorLine=%q", buildType, minorLine)
	}
}

func TestClassifyRancherVersionAllowsCommitHead(t *testing.T) {
	version := "2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head"
	buildType, minorLine, err := classifyRancherVersion(version)
	if err != nil {
		t.Fatalf("expected commit head to be valid, got error: %v", err)
	}
	if buildType != "head" || minorLine != "2.13" {
		t.Fatalf("expected commit head classification for 2.13, got buildType=%q minorLine=%q", buildType, minorLine)
	}
}

func TestParseHelmSearchResultsSkipsLeadingWarnings(t *testing.T) {
	output := []byte(`WARNING: Kubernetes configuration file is group-readable. This is insecure.
WARNING: Kubernetes configuration file is world-readable. This is insecure.
[{"name":"rancher-latest/rancher","version":"2.14.1","app_version":"v2.14.1"}]`)

	results, err := parseHelmSearchResults(output)
	if err != nil {
		t.Fatalf("expected helm search results despite leading warnings, got error: %v", err)
	}
	if len(results) != 1 || results[0].Name != "rancher-latest/rancher" || results[0].Version != "2.14.1" {
		t.Fatalf("unexpected helm search results: %#v", results)
	}
}

func TestPrereleaseChartClassification(t *testing.T) {
	if !isExactStagingPrereleaseChart("optimus-rancher-alpha") {
		t.Fatal("expected optimus alpha charts to be staging prerelease charts")
	}

	if !isExactStagingPrereleaseChart("optimus-rancher-latest") {
		t.Fatal("expected optimus latest charts to be staging prerelease charts")
	}

	if !isExactCommunityPrereleaseChart("rancher-alpha") {
		t.Fatal("expected rancher-alpha charts to be community prerelease charts")
	}

	if !isExactCommunityPrereleaseChart("rancher-latest") {
		t.Fatal("expected rancher-latest charts to be community prerelease charts")
	}

	if isExactCommunityPrereleaseChart("rancher-prime") || isExactStagingPrereleaseChart("rancher-prime") {
		t.Fatal("expected rancher-prime to use embedded Prime chart image settings")
	}
}

func TestChooseRancherSourceCandidatesAutoPrefersPrimeAndStagingBeforeCommunity(t *testing.T) {
	candidates, _, _ := chooseRancherSourceCandidates("auto", "alpha")
	want := []string{"rancher-prime", "optimus-rancher-alpha", "optimus-rancher-latest", "rancher-alpha", "rancher-latest"}
	if strings.Join(candidates, ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, candidates)
	}
}

func TestChooseRancherSourceCandidatesAutoHeadPrefersCommunity(t *testing.T) {
	candidates, distro, _ := chooseRancherSourceCandidates("auto", "head")
	want := []string{"rancher-latest", "optimus-rancher-latest", "rancher-prime"}
	if strings.Join(candidates, ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, candidates)
	}
	if distro != "community" {
		t.Fatalf("expected head to resolve as community, got %q", distro)
	}
}

func TestChooseRancherSourceCandidatesAutoReleasePrefersPrimeBeforeCommunity(t *testing.T) {
	candidates, _, _ := chooseRancherSourceCandidates("auto", "release")
	want := []string{"rancher-prime", "optimus-rancher-latest", "rancher-latest"}
	if strings.Join(candidates, ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, candidates)
	}
}

func TestRancherModeInfersAutoFromVersionsWithoutHelmCommands(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("rancher.versions", []string{"2.14-head"})

	if mode := rancherMode(); mode != "auto" {
		t.Fatalf("expected auto mode for Rancher versions without Helm commands, got %q", mode)
	}
}

func TestRancherModeKeepsManualDefaultForHelmCommands(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("rancher.helm_commands", []string{"helm install rancher rancher-latest/rancher"})

	if mode := rancherMode(); mode != "manual" {
		t.Fatalf("expected manual mode for Helm commands without explicit mode, got %q", mode)
	}
}

func TestManualHelmCommandParserAllowsQuotedSetString(t *testing.T) {
	command := `helm install rancher rancher-latest/rancher \
  --namespace cattle-system \
  --version 2.14.1 \
  --set-string 'bootstrapPassword=abc'\''def\,ghi' \
  --set tls=external`

	fields, err := parseHelmCommandFields(command)
	if err != nil {
		t.Fatalf("parseHelmCommandFields returned error: %v", err)
	}
	if len(fields) < 8 {
		t.Fatalf("expected parsed fields, got %#v", fields)
	}
	invocation, err := manualHelmInvocationFromFields(fields)
	if err != nil {
		t.Fatalf("manualHelmInvocationFromFields returned error: %v", err)
	}
	if invocation.releaseName != "rancher" || invocation.chartRef != "rancher-latest/rancher" {
		t.Fatalf("unexpected invocation: %#v", invocation)
	}
	if err := validateManualHelmCommandStructure(command); err != nil {
		t.Fatalf("expected manual command structure to validate, got %v", err)
	}
}

func TestManualHelmCommandParserRejectsShellControlOperators(t *testing.T) {
	command := `helm install rancher rancher-latest/rancher --set tls=external && rm -rf /`
	if err := validateManualHelmCommandStructure(command); err == nil {
		t.Fatal("expected shell control operator to be rejected")
	}
}

func TestHelmKubeVersionFromRKE2VersionStripsRKE2BuildMetadata(t *testing.T) {
	got := helmKubeVersionFromRKE2Version("v1.34.6+rke2r1")
	if got != "1.34.6" {
		t.Fatalf("expected Helm kube version 1.34.6, got %q", got)
	}
}

func TestResolveLatestReleasePatchWritesCacheOnSuccess(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	t.Setenv(releaseLookupCachePathEnv, cachePath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release-notes/v1.31.X" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`RKE2 releases v1.31.9+rke2r1 and v1.31.8+rke2r1`))
	}))
	t.Cleanup(server.Close)

	config := releaseProductConfig{
		ProductName: "RKE2",
		CacheKey:    "rke2",
		Pattern:     rke2ReleasePattern(31),
	}
	got, err := resolveLatestCachedReleasePatch(config, 31, server.URL+"/release-notes/v1.31.X", firstReleaseVersion)
	if err != nil {
		t.Fatalf("expected release lookup to succeed, got %v", err)
	}
	if got != "v1.31.9+rke2r1" {
		t.Fatalf("expected latest RKE2 release from docs order, got %q", got)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("expected cache file to be written: %v", err)
	}
	for _, want := range []string{`"rke2"`, `"v1.31"`, `"v1.31.9+rke2r1"`, `"v1.31.8+rke2r1"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected cache to contain %s, got:\n%s", want, string(data))
		}
	}
}

func TestHighestSemverReleaseVersionSelectsLatestK3sPatch(t *testing.T) {
	got, err := highestSemverReleaseVersion([]string{
		"v1.31.8+k3s1",
		"v1.31.10+k3s1",
		"v1.31.9+k3s2",
	}, "+k3s")
	if err != nil {
		t.Fatalf("expected K3s release selection to succeed, got %v", err)
	}
	if got != "v1.31.10+k3s1" {
		t.Fatalf("expected highest K3s patch release, got %q", got)
	}
}

func TestResolveLatestReleasePatchFallsBackToValidatedCacheOn404(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	t.Setenv(releaseLookupCachePathEnv, cachePath)
	updateReleaseCache(releaseProductConfig{
		ProductName: "RKE2",
		CacheKey:    "rke2",
		Pattern:     rke2ReleasePattern(31),
	}, 31, "https://docs.rke2.io/release-notes/v1.31.X", "v1.31.8+rke2r1", []string{"v1.31.8+rke2r1"})

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	got, err := resolveLatestCachedReleasePatch(releaseProductConfig{
		ProductName: "RKE2",
		CacheKey:    "rke2",
		Pattern:     rke2ReleasePattern(31),
	}, 31, server.URL+"/release-notes/v1.31.X", firstReleaseVersion)
	if err != nil {
		t.Fatalf("expected cached release lookup to be used, got %v", err)
	}
	if got != "v1.31.8+rke2r1" {
		t.Fatalf("expected cached RKE2 release, got %q", got)
	}
}

func TestResolveLatestReleasePatchFallsBackToGitHubTagsWithoutCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	t.Setenv(releaseLookupCachePathEnv, cachePath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release-notes/v1.31.X":
			http.NotFound(w, r)
		case "/repos/rancher/rke2/git/matching-refs/tags/v1.31.":
			_, _ = w.Write([]byte(`[
				{"ref":"refs/tags/v1.31.8+rke2r1"},
				{"ref":"refs/tags/v1.31.10+rke2r1"},
				{"ref":"refs/tags/v1.31.10+rke2r2"},
				{"ref":"refs/tags/v1.32.1+rke2r1"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	got, err := resolveLatestCachedReleasePatch(releaseProductConfig{
		ProductName:       "RKE2",
		CacheKey:          "rke2",
		Pattern:           rke2ReleasePattern(31),
		GitHubTagRefsURL:  server.URL + "/repos/rancher/rke2/git/matching-refs/tags/v1.31.",
		GitHubBuildPrefix: "+rke2",
	}, 31, server.URL+"/release-notes/v1.31.X", firstReleaseVersion)
	if err != nil {
		t.Fatalf("expected GitHub tag fallback to be used, got %v", err)
	}
	if got != "v1.31.10+rke2r2" {
		t.Fatalf("expected highest RKE2 tag fallback release, got %q", got)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("expected fallback lookup to write cache: %v", err)
	}
	for _, want := range []string{`"rke2"`, `"v1.31"`, `"v1.31.10+rke2r2"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected cache to contain %s, got:\n%s", want, string(data))
		}
	}
}

func TestResolveLatestReleasePatchGitHubFallbackSkipsTagsMissingRequiredAssets(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	t.Setenv(releaseLookupCachePathEnv, cachePath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/release-notes/v1.31.X":
			http.NotFound(w, r)
		case r.URL.Path == "/repos/rancher/rke2/git/matching-refs/tags/v1.31.":
			_, _ = w.Write([]byte(`[
				{"ref":"refs/tags/v1.31.9+rke2r1"},
				{"ref":"refs/tags/v1.31.10+rke2r1"},
				{"ref":"refs/tags/v1.31.10+rke2r2"}
			]`))
		case strings.HasSuffix(r.URL.Path, "/repos/rancher/rke2/releases/tags/v1.31.10+rke2r2"):
			_, _ = w.Write([]byte(`{"assets":[]}`))
		case strings.HasSuffix(r.URL.Path, "/repos/rancher/rke2/releases/tags/v1.31.10+rke2r1"):
			_, _ = w.Write([]byte(`{"assets":[{"name":"rke2-images.linux-amd64.tar.zst"},{"name":"sha256sum-amd64.txt"}]}`))
		case strings.HasSuffix(r.URL.Path, "/repos/rancher/rke2/releases/tags/v1.31.9+rke2r1"):
			_, _ = w.Write([]byte(`{"assets":[{"name":"rke2-images.linux-amd64.tar.zst"},{"name":"sha256sum-amd64.txt"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	got, err := resolveLatestCachedReleasePatch(releaseProductConfig{
		ProductName:       "RKE2",
		CacheKey:          "rke2",
		Pattern:           rke2ReleasePattern(31),
		GitHubTagRefsURL:  server.URL + "/repos/rancher/rke2/git/matching-refs/tags/v1.31.",
		GitHubBuildPrefix: "+rke2",
		GitHubReleaseURL:  server.URL + "/repos/rancher/rke2/releases/tags/%s",
		GitHubAssetNames: []string{
			"rke2-images.linux-amd64.tar.zst",
			"sha256sum-amd64.txt",
		},
	}, 31, server.URL+"/release-notes/v1.31.X", firstReleaseVersion)
	if err != nil {
		t.Fatalf("expected GitHub tag fallback to skip assetless tags, got %v", err)
	}
	if got != "v1.31.10+rke2r1" {
		t.Fatalf("expected latest RKE2 tag with required assets, got %q", got)
	}
}

func TestResolveLatestReleasePatchReturnsHelpfulErrorWithoutCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	t.Setenv(releaseLookupCachePathEnv, cachePath)

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	_, err := resolveLatestCachedReleasePatch(releaseProductConfig{
		ProductName: "RKE2",
		CacheKey:    "rke2",
		Pattern:     rke2ReleasePattern(31),
	}, 31, server.URL+"/release-notes/v1.31.X", firstReleaseVersion)
	if err == nil {
		t.Fatal("expected release lookup to fail without cache")
	}
	for _, want := range []string{"RKE2 release-note lookup is unavailable", "HTTP 404", "no cached lookup"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %q", want, err.Error())
		}
	}
}

func TestResolveCachedSupportRangeFallsBackToValidatedCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	t.Setenv(releaseLookupCachePathEnv, cachePath)
	supportMatrixURL := "https://www.suse.com/suse-rancher/support-matrix/all-supported-versions/rancher-v2-14-1/"
	updateSupportRangeCache("RKE2", supportMatrixURL, "Support matrix certifies RKE2 from v1.30 through v1.31", 30, 31)

	highestMinor, summary, err := resolveCachedSupportRange("RKE2", supportMatrixURL, httpStatusError{URL: supportMatrixURL, StatusCode: http.StatusNotFound})
	if err != nil {
		t.Fatalf("expected cached support range, got %v", err)
	}
	if highestMinor != 31 || summary != "Support matrix certifies RKE2 from v1.30 through v1.31" {
		t.Fatalf("unexpected cached support range: minor=%d summary=%q", highestMinor, summary)
	}
}

func rke2ReleasePattern(highestMinor int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`v1\.%d\.\d+\+rke2r\d+`, highestMinor))
}

func TestNormalizeRKE2VersionInputAddsLeadingV(t *testing.T) {
	got, err := normalizeRKE2VersionInput("1.34.6+rke2r1")
	if err != nil {
		t.Fatalf("normalizeRKE2VersionInput returned error: %v", err)
	}
	if got != "v1.34.6+rke2r1" {
		t.Fatalf("expected normalized RKE2 version, got %q", got)
	}
}

func TestNormalizeRKE2VersionInputRejectsBadValue(t *testing.T) {
	if _, err := normalizeRKE2VersionInput("banana"); err == nil {
		t.Fatal("expected invalid RKE2 version to be rejected")
	}
}

func TestHelmFlagValueReadsEqualsAndSeparateForms(t *testing.T) {
	if got := helmFlagValue([]string{"helm", "install", "rancher", "rancher-latest/rancher", "--version=2.14.0"}, "--version"); got != "2.14.0" {
		t.Fatalf("expected equals flag value, got %q", got)
	}
	if got := helmFlagValue([]string{"helm", "install", "rancher", "rancher-latest/rancher", "--version", "2.13.3"}, "--version"); got != "2.13.3" {
		t.Fatalf("expected separate flag value, got %q", got)
	}
}

func TestRecordResolvedChartMatchPrefersExactTargetOverFallbackBaseline(t *testing.T) {
	var best *resolvedChartMatch
	recordResolvedChartMatch(&best, "rancher-prime", "2.14.0", "2.14.0", 1)
	recordResolvedChartMatch(&best, "optimus-rancher-alpha", "2.14.1-alpha7", "2.14.0", 0)

	if best == nil {
		t.Fatal("expected a chart match")
	}
	if best.repoAlias != "optimus-rancher-alpha" || best.chartVersion != "2.14.1-alpha7" {
		t.Fatalf("expected exact alpha chart to beat fallback baseline, got %#v", best)
	}
}

func TestRecordResolvedChartMatchKeepsPrimeOnExactTie(t *testing.T) {
	var best *resolvedChartMatch
	recordResolvedChartMatch(&best, "rancher-prime", "2.14.1-alpha7", "2.14.0", 0)
	recordResolvedChartMatch(&best, "rancher-alpha", "2.14.1-alpha7", "2.14.0", 0)

	if best == nil {
		t.Fatal("expected a chart match")
	}
	if best.repoAlias != "rancher-prime" {
		t.Fatalf("expected first exact Prime match to win the tie, got %#v", best)
	}
}

func TestResolveImageSettingsAllowsMixedReleaseAndAlphaSources(t *testing.T) {
	releaseImage, releaseTag, releaseAgent, _ := resolveImageSettings("2.14.0", "release", "community")
	if releaseImage != "" || releaseTag != "" || releaseAgent != "" {
		t.Fatalf("expected community release to use chart defaults, got image=%q tag=%q agent=%q", releaseImage, releaseTag, releaseAgent)
	}

	alphaImage, alphaTag, alphaAgent, _ := resolveImageSettings("2.14.1-alpha7", "alpha", "community-staging")
	if alphaImage != "stgregistry.suse.com/rancher/rancher" || alphaTag != "v2.14.1-alpha7" {
		t.Fatalf("expected staging Rancher image for alpha, got image=%q tag=%q", alphaImage, alphaTag)
	}
	if alphaAgent != "stgregistry.suse.com/rancher/rancher-agent:v2.14.1-alpha7" {
		t.Fatalf("expected staging agent image for alpha, got %q", alphaAgent)
	}

	headImage, headTag, headAgent, _ := resolveImageSettings("2.14-head", "head", "community")
	if headImage != "" || headTag != "v2.14-head" || headAgent != "" {
		t.Fatalf("expected community head to use chart image with tag override only, got image=%q tag=%q agent=%q", headImage, headTag, headAgent)
	}

	commitHeadImage, commitHeadTag, commitHeadAgent, _ := resolveImageSettings("2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head", "head", "community")
	if commitHeadImage != "" || commitHeadTag != "v2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head" || commitHeadAgent != "" {
		t.Fatalf("expected community commit head to use chart image with tag override only, got image=%q tag=%q agent=%q", commitHeadImage, commitHeadTag, commitHeadAgent)
	}

	plainHeadImage, plainHeadTag, plainHeadAgent, _ := resolveImageSettings("head", "head", "community")
	if plainHeadImage != "" || plainHeadTag != "head" || plainHeadAgent != "" {
		t.Fatalf("expected plain head to use Docker Hub head tag without agent override, got image=%q tag=%q agent=%q", plainHeadImage, plainHeadTag, plainHeadAgent)
	}
}

func TestResolveCommitHeadImageSettingsFindsStagingPair(t *testing.T) {
	tag := "v2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/rancher/rancher/manifests/" + tag,
			"/v2/rancher/rancher-agent/manifests/" + tag:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	previousBases := rancherRegistryBaseURLs
	rancherRegistryBaseURLs = map[string]string{
		"stgregistry.suse.com": server.URL,
		"docker.io":            server.URL,
		"registry.rancher.com": server.URL,
	}
	t.Cleanup(func() {
		rancherRegistryBaseURLs = previousBases
	})

	image, imageTag, agentImage, _, err := resolveCommitHeadImageSettings("2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head")
	if err != nil {
		t.Fatalf("expected commit head image settings to resolve, got error: %v", err)
	}
	if image != "stgregistry.suse.com/rancher/rancher" || imageTag != tag || agentImage != "stgregistry.suse.com/rancher/rancher-agent:"+tag {
		t.Fatalf("unexpected commit head image settings: image=%q tag=%q agent=%q", image, imageTag, agentImage)
	}
}

func TestRancherLatestTagOnlyDoesNotClearCommitHeadImages(t *testing.T) {
	if shouldUseRancherLatestTagOnly("head", "rancher-latest", "2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head") {
		t.Fatal("commit-specific head builds must keep discovered explicit image registry settings")
	}
	if !shouldUseRancherLatestTagOnly("head", "rancher-latest", "2.13-head") {
		t.Fatal("minor-line head builds should keep the rancher-latest tag-only behavior")
	}
}

func TestValidateResolvedRancherImagesChecksExplicitRancherAndAgentImages(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth":
			_, _ = w.Write([]byte(`{"token":"test-token"}`))
		case "/v2/rancher/rancher/manifests/v2.14.1-alpha7",
			"/v2/rancher/rancher-agent/manifests/v2.14.1-alpha7":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+serverURL+`/auth",service="registry",scope="repository:rancher/rancher:pull"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	serverURL = server.URL
	t.Cleanup(server.Close)

	previousClient := rancherRegistryHTTPClient
	previousBases := rancherRegistryBaseURLs
	rancherRegistryHTTPClient = server.Client()
	rancherRegistryBaseURLs = map[string]string{"stgregistry.suse.com": server.URL}
	t.Cleanup(func() {
		rancherRegistryHTTPClient = previousClient
		rancherRegistryBaseURLs = previousBases
	})

	err := validateResolvedRancherImages(
		"stgregistry.suse.com/rancher/rancher",
		"v2.14.1-alpha7",
		"stgregistry.suse.com/rancher/rancher-agent:v2.14.1-alpha7",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildAutoHelmCommandsUsesImageFieldsForNewOptimusAlpha(t *testing.T) {
	commands := buildAutoHelmCommands(
		1,
		rancherHelmOperationInstall,
		"optimus-rancher-alpha",
		"2.14.1-alpha3",
		"admin",
		"stgregistry.suse.com/rancher/rancher",
		"v2.14.1-alpha3",
		"stgregistry.suse.com/rancher/rancher-agent:v2.14.1-alpha3",
		true,
	)

	command := commands[0]
	expectedSnippets := []string{
		"--set tls=external",
		"--set image.registry=stgregistry.suse.com",
		"--set image.repository=rancher/rancher",
		"--set image.tag=v2.14.1-alpha3",
		"--set 'extraEnv[0].name=CATTLE_AGENT_IMAGE'",
		"--set 'extraEnv[0].value=stgregistry.suse.com/rancher/rancher-agent:v2.14.1-alpha3'",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(command, snippet) {
			t.Fatalf("expected helm command to contain %q, got:\n%s", snippet, command)
		}
	}
	if strings.Contains(command, "ingress.tls.source=secret") {
		t.Fatalf("expected external TLS termination, got:\n%s", command)
	}
	if strings.Contains(command, "rancherImage") || strings.Contains(command, "systemDefaultRegistry") || strings.Contains(command, "webhook.global") {
		t.Fatalf("expected Optimus alpha command to use new image fields without default registry or webhook overrides, got:\n%s", command)
	}
}

func TestBuildAutoHelmCommandSetsSingleServerReplicasInPlan(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rke2.server_count", 1)

	command := buildAutoHelmCommand(
		rancherHelmOperationInstall,
		"rancher-latest",
		"2.14.1",
		"admin",
		"",
		"",
		"",
		false,
	)

	if !strings.Contains(command, "--set replicas=1") {
		t.Fatalf("expected single-server auto plan command to include replicas=1, got:\n%s", command)
	}
	if strings.Index(command, "--set replicas=1") > strings.Index(command, "--set agentTLSMode=system-store") {
		t.Fatalf("expected replicas setting before final command line, got:\n%s", command)
	}
}

func TestBuildAutoHelmCommandsKeepsLegacyOverridesForOldOptimusAlpha(t *testing.T) {
	commands := buildAutoHelmCommands(
		1,
		rancherHelmOperationInstall,
		"optimus-rancher-alpha",
		"2.11.13-alpha5",
		"admin",
		"stgregistry.suse.com/rancher/rancher",
		"v2.11.13-alpha5",
		"stgregistry.suse.com/rancher/rancher-agent:v2.11.13-alpha5",
		false,
	)

	command := commands[0]
	expectedSnippets := []string{
		"--set rancherImage=stgregistry.suse.com/rancher/rancher",
		"--set rancherImageTag=v2.11.13-alpha5",
		"--set 'extraEnv[0].value=stgregistry.suse.com/rancher/rancher-agent:v2.11.13-alpha5'",
	}
	for _, snippet := range expectedSnippets {
		if !strings.Contains(command, snippet) {
			t.Fatalf("expected helm command to contain %q, got:\n%s", snippet, command)
		}
	}
	if strings.Contains(command, "image.registry") || strings.Contains(command, "image.repository") || strings.Contains(command, "image.tag") {
		t.Fatalf("expected old Optimus alpha command to keep legacy image values, got:\n%s", command)
	}
}

func TestBuildAutoHelmCommandClearsPrimeDefaultRegistryForStagingFallback(t *testing.T) {
	command := buildAutoHelmCommand(
		rancherHelmOperationInstall,
		"rancher-prime",
		"2.13.4",
		"admin",
		"stgregistry.suse.com/rancher/rancher",
		"v2.13.5-alpha6",
		"stgregistry.suse.com/rancher/rancher-agent:v2.13.5-alpha6",
		true,
	)

	expectedSnippets := []string{
		"helm install rancher rancher-prime/rancher",
		"--version 2.13.4",
		"--set systemDefaultRegistry=",
		"--set image.registry=stgregistry.suse.com",
		"--set image.repository=rancher/rancher",
		"--set image.tag=v2.13.5-alpha6",
		"--set 'extraEnv[0].value=stgregistry.suse.com/rancher/rancher-agent:v2.13.5-alpha6'",
	}
	for _, snippet := range expectedSnippets {
		if !strings.Contains(command, snippet) {
			t.Fatalf("expected helm command to contain %q, got:\n%s", snippet, command)
		}
	}
}

func TestBuildAutoHelmCommandsCanUseCommunityAlphaImageFallback(t *testing.T) {
	commands := buildAutoHelmCommands(
		1,
		rancherHelmOperationInstall,
		"rancher-alpha",
		"2.15.0-alpha3",
		"admin",
		"",
		"v2.15.0-alpha3",
		"",
		true,
	)

	command := commands[0]
	expectedSnippets := []string{
		"helm install rancher rancher-alpha/rancher",
		"--set image.tag=v2.15.0-alpha3",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(command, snippet) {
			t.Fatalf("expected helm command to contain %q, got:\n%s", snippet, command)
		}
	}
	if strings.Contains(command, "stgregistry.suse.com") || strings.Contains(command, "CATTLE_AGENT_IMAGE") {
		t.Fatalf("expected community fallback command not to include staging overrides, got:\n%s", command)
	}
}

func TestBuildAutoHelmCommandsCommunityHeadDoesNotOverrideAgentImage(t *testing.T) {
	commands := buildAutoHelmCommands(
		1,
		rancherHelmOperationInstall,
		"rancher-latest",
		"2.14.1",
		"admin",
		"",
		"v2.14-head",
		"",
		true,
	)

	command := commands[0]
	expectedSnippets := []string{
		"helm install rancher rancher-latest/rancher",
		"--version 2.14.1",
		"--set image.tag=v2.14-head",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(command, snippet) {
			t.Fatalf("expected helm command to contain %q, got:\n%s", snippet, command)
		}
	}
	forbiddenSnippets := []string{
		"rancher-agent:v2.14-head",
		"CATTLE_AGENT_IMAGE",
		"stgregistry.suse.com",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(command, snippet) {
			t.Fatalf("expected community head command not to contain %q, got:\n%s", snippet, command)
		}
	}
}

func TestBuildAutoHelmCommandsPlainHeadUsesDockerHubHeadTag(t *testing.T) {
	commands := buildAutoHelmCommands(
		1,
		rancherHelmOperationInstall,
		"rancher-latest",
		"2.14.1",
		"admin",
		"",
		"head",
		"",
		true,
	)

	command := commands[0]
	expectedSnippets := []string{
		"helm install rancher rancher-latest/rancher",
		"--version 2.14.1",
		"--set image.tag=head",
	}
	for _, snippet := range expectedSnippets {
		if !strings.Contains(command, snippet) {
			t.Fatalf("expected helm command to contain %q, got:\n%s", snippet, command)
		}
	}
	forbiddenSnippets := []string{
		"image.tag=vhead",
		"CATTLE_AGENT_IMAGE",
		"stgregistry.suse.com",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(command, snippet) {
			t.Fatalf("expected plain head command not to contain %q, got:\n%s", snippet, command)
		}
	}
}

func TestBuildAutoHelmCommandUpgradeUsesSameResolvedSettings(t *testing.T) {
	command := buildAutoHelmCommand(
		rancherHelmOperationUpgrade,
		"optimus-rancher-alpha",
		"2.14.1-alpha6",
		"admin",
		"stgregistry.suse.com/rancher/rancher",
		"v2.14.1-alpha6",
		"stgregistry.suse.com/rancher/rancher-agent:v2.14.1-alpha6",
		true,
	)

	expectedSnippets := []string{
		"helm upgrade rancher optimus-rancher-alpha/rancher",
		"--install",
		"--version 2.14.1-alpha6",
		"--set hostname=placeholder",
		"--set tls=external",
		"--set image.registry=stgregistry.suse.com",
		"--set image.repository=rancher/rancher",
		"--set image.tag=v2.14.1-alpha6",
		"--set 'extraEnv[0].name=CATTLE_AGENT_IMAGE'",
		"--set 'extraEnv[0].value=stgregistry.suse.com/rancher/rancher-agent:v2.14.1-alpha6'",
		"--wait",
		"--wait-for-jobs",
		"--timeout 30m",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(command, snippet) {
			t.Fatalf("expected helm command to contain %q, got:\n%s", snippet, command)
		}
	}
	if strings.Contains(command, "ingress.tls.source=secret") {
		t.Fatalf("expected external TLS termination, got:\n%s", command)
	}
	if strings.Contains(command, "webhook.global") {
		t.Fatalf("expected Optimus upgrade command not to include webhook overrides, got:\n%s", command)
	}
}

func TestBuildAutoHelmCommandShellQuotesBootstrapPassword(t *testing.T) {
	password := `abc&Vfw8_Qr7*YVh1DE'with,comma\slash`
	command := buildAutoHelmCommand(
		rancherHelmOperationInstall,
		"rancher-latest",
		"2.14.1",
		password,
		"",
		"",
		"",
		true,
	)

	expected := `--set-string 'bootstrapPassword=abc&Vfw8_Qr7*YVh1DE'\''with\,comma\\slash'`
	if !strings.Contains(command, expected) {
		t.Fatalf("expected shell-quoted bootstrap password %q, got:\n%s", expected, command)
	}
	if strings.Contains(command, "--set bootstrapPassword=") {
		t.Fatalf("expected bootstrap password to use --set-string, got:\n%s", command)
	}
	if strings.Index(command, "--set-string 'bootstrapPassword=") > strings.Index(command, "--set tls=external") {
		t.Fatalf("expected bootstrap password before tls=external to remain part of the same helm command, got:\n%s", command)
	}
}

func TestShellQuoteHelmSetString(t *testing.T) {
	got := shellQuoteHelmSetString("bootstrapPassword", `a'b,c\d`)
	want := `'bootstrapPassword=a'\''b\,c\\d'`
	if got != want {
		t.Fatalf("shellQuoteHelmSetString() = %q, want %q", got, want)
	}
}

func TestNormalizeHelmImageSettingsLeavesOptimusAlphaOverridesDocShaped(t *testing.T) {
	settings := normalizeHelmImageSettings(
		"optimus-rancher-alpha",
		"stgregistry.suse.com/rancher/rancher",
		"v2.13.5-alpha6",
		"stgregistry.suse.com/rancher/rancher-agent:v2.13.5-alpha6",
		true,
	)

	if settings.clearSystemDefaultRegistry {
		t.Fatal("expected Optimus alpha command not to clear system default registry")
	}
	if settings.imageRegistry != "stgregistry.suse.com" || settings.imageRepository != "rancher/rancher" || settings.imageTag != "v2.13.5-alpha6" {
		t.Fatalf("expected staging Rancher image fields, got registry=%q repository=%q tag=%q", settings.imageRegistry, settings.imageRepository, settings.imageTag)
	}
	if settings.agentImage != "stgregistry.suse.com/rancher/rancher-agent:v2.13.5-alpha6" {
		t.Fatalf("expected qualified agent image, got %q", settings.agentImage)
	}
}

func TestNormalizeHelmImageSettingsLeavesDefaultRegistryForChartDefaultAgent(t *testing.T) {
	settings := normalizeHelmImageSettings(
		"rancher-prime",
		"registry.rancher.com/rancher/rancher",
		"v2.13.4",
		"",
		true,
	)

	if settings.clearSystemDefaultRegistry {
		t.Fatal("expected no system default registry override")
	}
	if settings.imageRegistry != "registry.rancher.com" || settings.imageRepository != "rancher/rancher" || settings.imageTag != "v2.13.4" {
		t.Fatalf("expected Prime image fields, got registry=%q repository=%q tag=%q", settings.imageRegistry, settings.imageRepository, settings.imageTag)
	}
	if settings.agentImage != "" {
		t.Fatalf("expected empty agent image to be preserved, got %q", settings.agentImage)
	}
}

func TestValuesSupportTopLevelRancherImageFields(t *testing.T) {
	values := `
auditLog:
  image:
    repository: rancher/mirrored-bci-micro
    tag: 15.6.24.2
image:
  repository: rancher/rancher
  tag: ""
`

	if !valuesSupportTopLevelRancherImageFields(values) {
		t.Fatal("expected top-level Rancher image fields to be detected")
	}
}

func TestValuesSupportTopLevelRancherImageFieldsIgnoresNestedOnly(t *testing.T) {
	values := `
auditLog:
  image:
    repository: rancher/mirrored-bci-micro
    tag: 15.6.24.2
rancherImage: stgregistry.suse.com/rancher/rancher
`

	if valuesSupportTopLevelRancherImageFields(values) {
		t.Fatal("expected nested image fields not to count as Rancher image field support")
	}
}

func TestRancherHelmCommandForHAReplacesPlaceholder(t *testing.T) {
	command := buildAutoHelmCommand(
		rancherHelmOperationUpgrade,
		"rancher-alpha",
		"2.14.1-alpha6",
		"admin",
		"",
		"",
		"",
		false,
	)

	command = rancherHelmCommandForHA(command, "rancher.example.com")
	if !strings.Contains(command, "--set hostname=rancher.example.com") {
		t.Fatalf("expected hostname replacement, got:\n%s", command)
	}
	if strings.Contains(command, "--set hostname=placeholder") {
		t.Fatalf("expected placeholder to be replaced, got:\n%s", command)
	}
}
