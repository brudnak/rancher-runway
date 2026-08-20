package test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/viper"
	"golang.org/x/net/html"
)

const (
	rancherHelmOperationInstall = "install"
	rancherHelmOperationUpgrade = "upgrade"
	rancherHookImageRegistry    = "registry.rancher.com"
	supportMatrixIndexURL       = "https://www.suse.com/suse-rancher/support-matrix/all-supported-versions/"
	rancherResolverHTTPTimeout  = 30 * time.Second
)

var rancherRegistryHTTPClient = &http.Client{Timeout: rancherResolverHTTPTimeout}
var rancherRegistryBaseURLs = map[string]string{}
var commitHeadVersionPattern = regexp.MustCompile(`^(\d+\.\d+)(?:\.\d+)?-[0-9a-fA-F]{7,40}-head$`)
var primeCommitHeadVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+-[0-9a-fA-F]{7,40}-head$`)
var customImageMinorLinePattern = regexp.MustCompile(`(?i)^v?(\d+\.\d+)(?:[._-]|$)`)

// RCS build IDs are alphanumeric and may use compact or dotted forms, for example
// 2.15.1-rcs-c936, 2.16.0-rcs-0844.1, and 2.15.0-rcs-e20f.1.
var rcsVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+-rcs-[0-9A-Za-z]+(?:\.\d+)?$`)
var supportMatrixURLVersionPattern = regexp.MustCompile(`rancher-v(\d+)-(\d+)-(\d+)/?`)
var rancherLookupHTTPClient = &http.Client{Timeout: rancherResolverHTTPTimeout}

type customRancherImageRequest struct {
	serverRepository string
	tag              string
	agentImage       string
}

func prepareRancherConfiguration(totalHAs int) ([]*RancherResolvedPlan, error) {
	mode := rancherMode()
	switch mode {
	case "", "manual":
		return prepareManualRKE2Plans(totalHAs)
	case "auto":
		plans, err := resolveAutoRancherPlans(totalHAs)
		if err != nil {
			return nil, err
		}

		var helmCommands []string
		for _, plan := range plans {
			helmCommands = append(helmCommands, plan.HelmCommands...)
		}
		viper.Set("rancher.helm_commands", helmCommands)
		return plans, nil
	default:
		return nil, fmt.Errorf("unsupported rancher.mode %q", mode)
	}
}

func rancherMode() string {
	viperConfigMu.RLock()
	defer viperConfigMu.RUnlock()

	mode := strings.ToLower(strings.TrimSpace(viper.GetString("rancher.mode")))
	if mode != "" {
		return mode
	}

	if hasRequestedRancherVersions() && len(viper.GetStringSlice("rancher.helm_commands")) == 0 {
		return "auto"
	}

	return "manual"
}

func hasRequestedRancherVersions() bool {
	if strings.TrimSpace(viper.GetString("rancher.version")) != "" {
		return true
	}
	for _, version := range viper.GetStringSlice("rancher.versions") {
		if strings.TrimSpace(version) != "" {
			return true
		}
	}
	return false
}

func prepareManualRKE2Plans(totalHAs int) ([]*RancherResolvedPlan, error) {
	webhookImage := configuredRancherInstallWebhookImage()
	if err := validateRancherWebhookImage(webhookImage); err != nil {
		return nil, err
	}
	versions, err := getRequestedRKE2Versions(totalHAs)
	if err != nil {
		return nil, err
	}
	helmCommands := viper.GetStringSlice("rancher.helm_commands")
	helmCommands, err = rancherHelmCommandsWithWebhookImage(helmCommands, webhookImage)
	if err != nil {
		return nil, err
	}
	if len(helmCommands) != totalHAs {
		return nil, fmt.Errorf("rancher.helm_commands has %d entries but total_has is %d; please provide exactly one Helm command per HA", len(helmCommands), totalHAs)
	}

	plans := make([]*RancherResolvedPlan, 0, len(versions))
	for i, version := range versions {
		checksum, err := rke2ChecksumForVersion(version)
		if err != nil {
			return nil, err
		}

		plans = append(plans, &RancherResolvedPlan{
			Mode:                   "manual",
			RecommendedRKE2Version: version,
			InstallerSHA256:        checksum,
			HelmCommands:           []string{strings.TrimSpace(helmCommands[i])},
		})
	}
	viper.Set("rancher.helm_commands", helmCommands)

	return plans, nil
}

func resolveAutoRancherPlans(totalHAs int) ([]*RancherResolvedPlan, error) {
	webhookImage := configuredRancherInstallWebhookImage()
	if err := validateRancherWebhookImage(webhookImage); err != nil {
		return nil, err
	}
	requestedVersions, err := getRequestedRancherVersions(totalHAs)
	if err != nil {
		return nil, err
	}
	agentImageOverrides, err := getRequestedAgentImageOverrides(totalHAs)
	if err != nil {
		return nil, err
	}
	preferredRegistries, err := settings.NormalizePreferredImageRegistries(viper.GetStringSlice("rancher.preferred_image_registries"))
	if err != nil {
		return nil, err
	}

	requestedDistro := strings.ToLower(strings.TrimSpace(viper.GetString("rancher.distro")))
	if requestedDistro == "" {
		requestedDistro = "auto"
	}

	bootstrapPassword := viper.GetString("rancher.bootstrap_password")
	if bootstrapPassword == "" {
		return nil, fmt.Errorf("rancher.bootstrap_password must be set when rancher.mode=auto")
	}

	preferredImageResolutions := make([]*preferredRancherImageResolution, len(requestedVersions))
	repoAliases := map[string]bool{}
	for planIndex, requestedVersion := range requestedVersions {
		_, isCustomImage, err := parseCustomRancherImageRequest(requestedVersion)
		if err != nil {
			return nil, err
		}
		buildType, _, err := classifyRancherVersionOrImage(requestedVersion)
		if err != nil {
			return nil, err
		}
		versionDistro, _ := effectiveRequestedRancherDistro(requestedDistro, requestedVersion)
		if err := validateRequestedRancherDistro(versionDistro, buildType, requestedVersion, isCustomImage); err != nil {
			return nil, err
		}
		if len(preferredRegistries) > 0 && agentImageOverrides[planIndex] != "" && !isCustomImage {
			return nil, fmt.Errorf("rancher.preferred_image_registries cannot be combined with rancher.agent_images[%d]; the preferred registry must supply the matching server and agent image pair", planIndex)
		}
		if len(preferredRegistries) > 0 && !isCustomImage {
			preferredImageResolutions[planIndex], err = resolvePreferredRancherImageSettings(requestedVersion, preferredRegistries)
			if err != nil {
				return nil, fmt.Errorf("verify preferred Rancher images for %s: %w", requestedVersion, err)
			}
		}
		repoCandidates, _, _ := chooseRancherSourceCandidates(versionDistro, buildType)
		for _, repoAlias := range repoCandidates {
			repoAliases[repoAlias] = true
		}
	}
	repoAliases["rancher-latest"] = true
	repoAliases["rancher-prime"] = true
	if err := ensureRancherHelmRepos(mapKeys(repoAliases), false); err != nil {
		return nil, err
	}
	if err := refreshHelmRepoIndexes(); err != nil {
		return nil, err
	}

	plans := make([]*RancherResolvedPlan, 0, len(requestedVersions))
	for planIndex, requestedVersion := range requestedVersions {
		customImage, isCustomImage, err := parseCustomRancherImageRequest(requestedVersion)
		if err != nil {
			return nil, err
		}
		buildType, minorLine, err := classifyRancherVersionOrImage(requestedVersion)
		if err != nil {
			return nil, err
		}
		preferredImageResolution := preferredImageResolutions[planIndex]

		versionDistro, inferredPrimeHead := effectiveRequestedRancherDistro(requestedDistro, requestedVersion)
		repoCandidates, resolvedDistro, explanation := chooseRancherSourceCandidates(versionDistro, buildType)
		if inferredPrimeHead {
			explanation = []string{"Patch-qualified Prime head selected Prime chart sources automatically"}
		}
		if len(preferredRegistries) > 0 && isCustomImage {
			explanation = append(explanation, "Preferred registry checkboxes were ignored because the exact custom image reference controls its registry")
		}
		chartRequest := requestedVersion
		if isCustomImage {
			chartRequest = rancherVersionHintFromImageTag(customImage.tag)
			if chartRequest == "" {
				chartRequest = "head"
			}
		}
		chartRepoAlias, chartVersion, compatibilityBaseline, err := resolveChartAndBaseline(repoCandidates, chartRequest, minorLine, buildType)
		if err != nil {
			return nil, err
		}
		if minorLine == "" {
			minorLine, err = rancherMinorLineFromVersion(compatibilityBaseline)
			if err != nil {
				return nil, err
			}
		}
		if buildType != "release" && chartRepoAlias == "rancher-prime" {
			explanation = append(explanation, fmt.Sprintf("Using the latest released Prime chart %s as the baseline chart, then overriding Rancher images to the requested %s build", chartVersion, buildType))
		}

		rancherImage, rancherImageTag, agentImage, imageExplanation := resolveImageSettings(requestedVersion, buildType, resolvedDistro)
		if isCustomImage {
			rancherImage = customImage.serverRepository
			rancherImageTag = customImage.tag
			agentImage = customImage.agentImage
			imageExplanation = []string{fmt.Sprintf("Using exact custom Rancher image %s and derived agent image %s", requestedVersion, agentImage)}
		}
		if explicitAgentImage := agentImageOverrides[planIndex]; explicitAgentImage != "" {
			if !allowsExplicitAgentImageOverride(requestedVersion, isCustomImage) {
				return nil, fmt.Errorf("rancher.agent_images[%d] requires a custom Rancher server or agent image, or an RCS build, in rancher.versions[%d]", planIndex, planIndex)
			}
			canonicalAgentImage, err := normalizeCustomAgentImage(explicitAgentImage)
			if err != nil {
				return nil, fmt.Errorf("rancher.agent_images[%d]: %w", planIndex, err)
			}
			agentImage = canonicalAgentImage
			if isCustomImage {
				imageExplanation = []string{fmt.Sprintf("Using exact custom Rancher image %s and explicitly supplied agent image %s", rancherImage+":"+rancherImageTag, agentImage)}
			} else {
				imageExplanation = []string{fmt.Sprintf("Using exact RCS staging Rancher image %s and explicitly supplied agent image %s", rancherImage+":"+rancherImageTag, agentImage)}
			}
		}
		if buildType == "head" && isCommitHeadRancherVersion(requestedVersion) && preferredImageResolution == nil {
			rancherImage, rancherImageTag, agentImage, imageExplanation, err = resolveCommitHeadImageSettings(requestedVersion)
			if err != nil {
				return nil, fmt.Errorf("resolve Rancher image settings for %s: %w", requestedVersion, err)
			}
		}
		var rancherLatestTagOnly bool
		if !isCustomImage && preferredImageResolution == nil {
			rancherImage, rancherImageTag, agentImage, imageExplanation, rancherLatestTagOnly = applyRancherLatestTagOnlySettings(
				buildType,
				chartRepoAlias,
				requestedVersion,
				rancherImage,
				rancherImageTag,
				agentImage,
				imageExplanation,
			)
		}
		if preferredImageResolution == nil && buildType != "release" && chartVersion == requestedVersion && chartRepoAlias == "rancher-prime" && !isRCSServerBuild(requestedVersion) {
			rancherImage = ""
			rancherImageTag = ""
			agentImage = ""
			explanation = append(explanation, fmt.Sprintf("Using exact chart match %s/rancher@%s, so no Rancher image overrides are needed", chartRepoAlias, chartVersion))
		}
		if preferredImageResolution == nil && buildType != "release" && chartVersion == requestedVersion && isExactCommunityPrereleaseChart(chartRepoAlias) {
			if rancherLatestTagOnly {
				explanation = append(explanation, fmt.Sprintf("Using exact chart match %s/rancher@%s with community image defaults", chartRepoAlias, chartVersion))
			} else if err := validateResolvedRancherImages(rancherImage, rancherImageTag, agentImage); err != nil {
				if isRCSServerBuild(requestedVersion) {
					explanation = append(explanation, fmt.Sprintf("RCS build %s requires its explicit staging Rancher images", requestedVersion))
				} else {
					rancherImage = ""
					agentImage = ""
					imageExplanation = []string{fmt.Sprintf("Staging Rancher image override was unavailable for %s, using exact community chart/image defaults", requestedVersion)}
					explanation = append(explanation, fmt.Sprintf("Using exact chart match %s/rancher@%s with community image defaults", chartRepoAlias, chartVersion))
				}
			} else {
				explanation = append(explanation, fmt.Sprintf("Using exact chart match %s/rancher@%s with explicit staging Rancher image overrides", chartRepoAlias, chartVersion))
			}
		}
		if preferredImageResolution == nil && buildType != "release" && chartVersion == requestedVersion && isExactStagingPrereleaseChart(chartRepoAlias) {
			explanation = append(explanation, fmt.Sprintf("Using exact chart match %s/rancher@%s with explicit staging Rancher image overrides", chartRepoAlias, chartVersion))
		}
		if rancherLatestTagOnly {
			explanation = append(explanation, fmt.Sprintf("Using rancher-latest for this %s build, so only the Rancher image tag is overridden to %s", buildType, rancherImageTag))
		}
		if preferredImageResolution == nil && buildType == "release" && chartRepoAlias == "rancher-prime" && !isCustomImage {
			rancherImage = "registry.rancher.com/rancher/rancher"
			explanation = append(explanation, fmt.Sprintf("Using Prime chart and Prime Rancher image for released version %s", requestedVersion))
		}

		resolvedImageRegistry := ""
		rancherImageDigest := ""
		agentImageDigest := ""
		imageBuildVersion := ""
		imageSourceURL := ""
		imageSourceRevision := ""
		imageSourceOSSRevision := ""
		imageSourceCommitURL := ""
		if preferredImageResolution != nil {
			rancherImage = preferredImageResolution.RancherImage
			rancherImageTag = preferredImageResolution.RancherImageTag
			agentImage = preferredImageResolution.AgentImage
			resolvedImageRegistry = preferredImageResolution.Registry
			rancherImageDigest = preferredImageResolution.RancherProvenance.Digest
			agentImageDigest = preferredImageResolution.AgentProvenance.Digest
			imageBuildVersion = preferredImageResolution.RancherProvenance.BuildVersion
			imageSourceURL = preferredImageResolution.RancherProvenance.SourceURL
			imageSourceRevision = preferredImageResolution.RancherProvenance.Revision
			imageSourceOSSRevision = preferredImageResolution.RancherProvenance.OSSRevision
			imageSourceCommitURL = rancherImageSourceCommitURL(imageSourceURL, imageSourceRevision)
			imageExplanation = preferredImageResolutionExplanation(preferredImageResolution, preferredRegistries)
		} else if isCustomImage {
			exactImageResolution, inspectErr := inspectExplicitRancherImagePair(rancherImage, rancherImageTag, agentImage)
			if inspectErr != nil {
				return nil, fmt.Errorf("verify exact Rancher image pair for %s: %w", requestedVersion, inspectErr)
			}
			resolvedImageRegistry = exactImageResolution.Registry
			rancherImageDigest = exactImageResolution.RancherProvenance.Digest
			agentImageDigest = exactImageResolution.AgentProvenance.Digest
			imageBuildVersion = exactImageResolution.RancherProvenance.BuildVersion
			imageSourceURL = exactImageResolution.RancherProvenance.SourceURL
			imageSourceRevision = exactImageResolution.RancherProvenance.Revision
			imageSourceOSSRevision = exactImageResolution.RancherProvenance.OSSRevision
			imageSourceCommitURL = rancherImageSourceCommitURL(imageSourceURL, imageSourceRevision)
			imageExplanation = append(imageExplanation,
				fmt.Sprintf("Recorded resolution-time OCI digests and provenance for the exact image pair in %s", exactImageResolution.RegistryLabel),
			)
		}
		if preferredImageResolution == nil && !isCustomImage {
			if err := validateResolvedRancherImages(rancherImage, rancherImageTag, agentImage); err != nil {
				return nil, fmt.Errorf("validate Rancher image settings for %s: %w", requestedVersion, err)
			}
		}
		explanation = append(explanation, imageExplanation...)
		if isCustomImage || compatibilityBaseline != requestedVersion {
			explanation = append(explanation, fmt.Sprintf("Using %s as the latest released compatibility baseline for the %s release line", compatibilityBaseline, minorLine))
		}
		useRancherImageFields, err := requireResolvedRancherChartImageFieldSupport(chartRepoAlias, chartVersion)
		if err != nil {
			return nil, err
		} else if useRancherImageFields {
			explanation = append(explanation, fmt.Sprintf("Using current image.registry/image.repository/image.tag chart values for %s/rancher@%s", chartRepoAlias, chartVersion))
		} else {
			explanation = append(explanation, fmt.Sprintf("Using legacy rancherImage/rancherImageTag chart values for %s/rancher@%s", chartRepoAlias, chartVersion))
		}

		supportMatrixURL := buildSupportMatrixURL(compatibilityBaseline)
		recommendedRKE2Version := ""
		installerSHA256 := ""
		if !isHostedTenantK3SDeployment() {
			highestRKE2Minor, supportExplanation, resolvedSupportMatrixURL, err := resolveHighestSupportedRKE2Minor(supportMatrixURL)
			if err != nil {
				return nil, err
			}
			supportMatrixURL = resolvedSupportMatrixURL
			explanation = append(explanation, supportExplanation)

			recommendedRKE2Version, err = resolveLatestRKE2Patch(highestRKE2Minor)
			if err != nil {
				return nil, err
			}
			explanation = append(explanation, fmt.Sprintf("Selected %s as the latest available RKE2 patch in the supported v1.%d line", recommendedRKE2Version, highestRKE2Minor))

			installerSHA256, err = resolveInstallerSHA256(recommendedRKE2Version)
			if err != nil {
				return nil, err
			}
		}

		helmCommands := buildAutoHelmCommands(1, rancherHelmOperationInstall, chartRepoAlias, chartVersion, bootstrapPassword, rancherImage, rancherImageTag, agentImage, useRancherImageFields)
		helmCommands, err = rancherHelmCommandsWithWebhookImage(helmCommands, webhookImage)
		if err != nil {
			return nil, fmt.Errorf("configure Rancher webhook image for %s: %w", requestedVersion, err)
		}

		var appliedPreferredRegistries []string
		if preferredImageResolution != nil {
			appliedPreferredRegistries = append([]string(nil), preferredRegistries...)
		}
		plans = append(plans, &RancherResolvedPlan{
			Mode:                     "auto",
			RequestedVersion:         requestedVersion,
			RequestedDistro:          requestedDistro,
			PreferredImageRegistries: appliedPreferredRegistries,
			BuildType:                buildType,
			ResolvedDistro:           resolvedDistro,
			ResolvedImageRegistry:    resolvedImageRegistry,
			ChartRepoAlias:           chartRepoAlias,
			ChartVersion:             chartVersion,
			RancherImage:             rancherImage,
			RancherImageTag:          rancherImageTag,
			AgentImage:               agentImage,
			RancherImageDigest:       rancherImageDigest,
			AgentImageDigest:         agentImageDigest,
			ImageBuildVersion:        imageBuildVersion,
			ImageSourceURL:           imageSourceURL,
			ImageSourceRevision:      imageSourceRevision,
			ImageSourceOSSRevision:   imageSourceOSSRevision,
			ImageSourceCommitURL:     imageSourceCommitURL,
			UseRancherImageFields:    useRancherImageFields,
			CompatibilityBaseline:    compatibilityBaseline,
			SupportMatrixURL:         supportMatrixURL,
			RecommendedRKE2Version:   recommendedRKE2Version,
			InstallerSHA256:          installerSHA256,
			HelmCommands:             helmCommands,
			Explanation:              explanation,
		})
	}

	return plans, nil
}

func getRequestedAgentImageOverrides(totalHAs int) ([]string, error) {
	overrides := viper.GetStringSlice("rancher.agent_images")
	if len(overrides) == 0 {
		if single := strings.TrimSpace(viper.GetString("rancher.agent_image")); single != "" {
			overrides = []string{single}
		}
	}
	if len(overrides) == 0 {
		return make([]string, totalHAs), nil
	}
	if len(overrides) != totalHAs {
		return nil, fmt.Errorf("rancher.agent_images has %d entries but total_has is %d; provide one entry per Rancher image (blank entries use automatic derivation)", len(overrides), totalHAs)
	}
	for i := range overrides {
		overrides[i] = strings.TrimSpace(overrides[i])
	}
	return overrides, nil
}

func normalizeCustomAgentImage(image string) (string, error) {
	registry, repository, tag, err := parseRegistryImage(image)
	if err != nil {
		return "", fmt.Errorf("invalid custom Rancher agent image %q: %w", image, err)
	}
	if pathBase := repository[strings.LastIndex(repository, "/")+1:]; pathBase != "rancher-agent" {
		return "", fmt.Errorf("custom Rancher agent image repository must end in /rancher-agent, got %q", image)
	}
	return registry + "/" + repository + ":" + tag, nil
}

func validateCustomAgentImage(image string) error {
	_, err := normalizeCustomAgentImage(image)
	return err
}

func allowsExplicitAgentImageOverride(requestedVersion string, isCustomImage bool) bool {
	return isCustomImage || isRCSServerBuild(requestedVersion)
}

func mapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func getRequestedRancherVersions(totalHAs int) ([]string, error) {
	requestedVersions := viper.GetStringSlice("rancher.versions")
	if len(requestedVersions) > 0 {
		if len(requestedVersions) != totalHAs {
			return nil, fmt.Errorf("rancher.versions has %d entries but total_has is %d; please provide exactly one Rancher version per HA", len(requestedVersions), totalHAs)
		}

		normalized := make([]string, 0, len(requestedVersions))
		for i, version := range requestedVersions {
			normalizedVersion := normalizeVersionInput(version)
			if normalizedVersion == "" {
				return nil, fmt.Errorf("rancher.versions[%d] must not be empty", i)
			}
			normalized = append(normalized, normalizedVersion)
		}
		return normalized, nil
	}

	requestedVersion := normalizeVersionInput(viper.GetString("rancher.version"))
	if requestedVersion == "" {
		return nil, fmt.Errorf("set rancher.version for a single HA or rancher.versions with %d entries for auto mode", totalHAs)
	}
	if totalHAs > 1 {
		return nil, fmt.Errorf("total_has is %d, so rancher.versions must contain %d versions", totalHAs, totalHAs)
	}

	return []string{requestedVersion}, nil
}

func getRequestedRKE2Versions(totalHAs int) ([]string, error) {
	requestedVersions := viper.GetStringSlice("k8s.versions")
	if len(requestedVersions) > 0 {
		if len(requestedVersions) != totalHAs {
			return nil, fmt.Errorf("k8s.versions has %d entries but total_has is %d; please provide exactly one RKE2 version per HA", len(requestedVersions), totalHAs)
		}

		normalized := make([]string, 0, len(requestedVersions))
		for i, version := range requestedVersions {
			normalizedVersion, err := normalizeRKE2VersionInput(version)
			if err != nil {
				return nil, fmt.Errorf("k8s.versions[%d] is invalid: %w", i, err)
			}
			normalized = append(normalized, normalizedVersion)
		}
		return normalized, nil
	}

	requestedVersion, err := normalizeRKE2VersionInput(viper.GetString("k8s.version"))
	if err != nil {
		return nil, fmt.Errorf("set k8s.version for a single HA or k8s.versions with %d entries", totalHAs)
	}
	if totalHAs > 1 {
		return nil, fmt.Errorf("total_has is %d, so k8s.versions must contain %d versions in manual mode", totalHAs, totalHAs)
	}

	return []string{requestedVersion}, nil
}

func rke2ChecksumForVersion(version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("RKE2 version must not be empty")
	}

	checksums := viper.GetStringMapString("rke2.install_script_sha256s")
	if checksum := strings.TrimSpace(checksums[version]); checksum != "" {
		return checksum, nil
	}

	if strings.TrimSpace(viper.GetString("k8s.version")) == version {
		if checksum := strings.TrimSpace(viper.GetString("rke2.install_script_sha256")); checksum != "" {
			return checksum, nil
		}
	}

	return "", fmt.Errorf("rancher.mode=manual requires pinned RKE2 installer checksums; set rke2.install_script_sha256s.%s or use rancher.mode=auto to resolve the RKE2 version and checksum automatically", version)
}

func normalizeVersionInput(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		return value
	}
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "V")
	return value
}

var rke2VersionPattern = regexp.MustCompile(`^v1\.\d+\.\d+\+rke2r\d+$`)

func normalizeRKE2VersionInput(value string) (string, error) {
	version := strings.TrimSpace(value)
	if version == "" {
		return "", fmt.Errorf("RKE2 version must not be empty")
	}
	version = strings.TrimPrefix(version, "V")
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !rke2VersionPattern.MatchString(version) {
		return "", fmt.Errorf("RKE2 version must look like v1.34.6+rke2r1")
	}
	return version, nil
}

func classifyRancherVersion(version string) (buildType string, minorLine string, err error) {
	headPattern := regexp.MustCompile(`^\d+\.\d+-head$`)
	alphaPattern := regexp.MustCompile(`^\d+\.\d+\.\d+-alpha\d+$`)
	rcPattern := regexp.MustCompile(`^\d+\.\d+\.\d+-rc\d+$`)
	releasePattern := regexp.MustCompile(`^\d+\.\d+\.\d+$`)

	switch {
	case version == "head":
		return "head", "", nil
	case headPattern.MatchString(version):
		parts := strings.Split(version, "-")
		return "head", parts[0], nil
	case commitHeadVersionPattern.MatchString(version):
		matches := commitHeadVersionPattern.FindStringSubmatch(version)
		return "head", matches[1], nil
	case alphaPattern.MatchString(version):
		parts := strings.Split(version, "-")
		return "alpha", strings.Join(strings.Split(parts[0], ".")[:2], "."), nil
	case rcPattern.MatchString(version):
		parts := strings.Split(version, "-")
		return "rc", strings.Join(strings.Split(parts[0], ".")[:2], "."), nil
	case rcsVersionPattern.MatchString(version):
		parts := strings.Split(version, "-")
		// Returns "rcs" to ensure the resolver uses the staging registry (stgregistry.suse.com)
		return "rcs", strings.Join(strings.Split(parts[0], ".")[:2], "."), nil
	case releasePattern.MatchString(version):
		return "release", strings.Join(strings.Split(version, ".")[:2], "."), nil
	default:
		return "", "", fmt.Errorf("unsupported rancher.version format %q", version)
	}
}

func classifyRancherVersionOrImage(value string) (buildType string, minorLine string, err error) {
	if image, ok, imageErr := parseCustomRancherImageRequest(value); ok || imageErr != nil {
		if imageErr != nil {
			return "", "", imageErr
		}
		if versionHint := rancherVersionHintFromImageTag(image.tag); versionHint != "" {
			_, minorLine, err := classifyRancherVersion(versionHint)
			if err != nil {
				return "", "", err
			}
			// A custom reference remains an image override even when its tag looks
			// like a release. The hint selects compatibility; it must not make the
			// resolver replace or require an exact released image/chart path.
			return "head", minorLine, nil
		}
		return "head", "", nil
	}
	return classifyRancherVersion(value)
}

func rancherVersionHintFromImageTag(tag string) string {
	tag = normalizeVersionInput(tag)
	if _, _, err := classifyRancherVersion(tag); err == nil {
		return tag
	}

	// Local image tags often add a free-form suffix to a Rancher minor line.
	// Retain that line for chart and support-matrix lookup while continuing to
	// treat an otherwise opaque custom tag as generic head.
	matches := customImageMinorLinePattern.FindStringSubmatch(tag)
	if len(matches) == 2 {
		return matches[1] + "-head"
	}
	return ""
}

func parseCustomRancherImageRequest(value string) (customRancherImageRequest, bool, error) {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "/") {
		return customRancherImageRequest{}, false, nil
	}

	registry, repository, tag, err := parseRegistryImage(value)
	if err != nil {
		return customRancherImageRequest{}, true, fmt.Errorf("invalid custom Rancher image %q: %w", value, err)
	}
	pathBase := repository[strings.LastIndex(repository, "/")+1:]
	var serverRepository, agentRepository string
	switch pathBase {
	case "rancher":
		serverRepository = repository
		agentRepository = strings.TrimSuffix(repository, "/rancher") + "/rancher-agent"
	case "rancher-agent":
		agentRepository = repository
		serverRepository = strings.TrimSuffix(repository, "/rancher-agent") + "/rancher"
	default:
		return customRancherImageRequest{}, true, fmt.Errorf("custom Rancher image repository must end in /rancher or /rancher-agent, got %q", value)
	}

	return customRancherImageRequest{
		serverRepository: registry + "/" + serverRepository,
		tag:              tag,
		agentImage:       registry + "/" + agentRepository + ":" + tag,
	}, true, nil
}

func isCommitHeadRancherVersion(version string) bool {
	return commitHeadVersionPattern.MatchString(strings.TrimSpace(version))
}

func isPrimeCommitHeadRancherVersion(version string) bool {
	return primeCommitHeadVersionPattern.MatchString(normalizeVersionInput(version))
}

func validateRequestedRancherDistro(requestedDistro, buildType, requestedVersion string, isCustomImage bool) error {
	if requestedDistro != "prime" || buildType == "release" {
		return nil
	}
	if buildType == "head" && (isCustomImage || isPrimeCommitHeadRancherVersion(requestedVersion)) {
		return nil
	}
	return fmt.Errorf("prime distro requires a released Rancher version, an exact custom image, or a patch-qualified Prime head like 2.14.5-{SHA}-head")
}

func effectiveRequestedRancherDistro(requestedDistro, requestedVersion string) (string, bool) {
	requestedDistro = strings.ToLower(strings.TrimSpace(requestedDistro))
	if requestedDistro == "auto" && isPrimeCommitHeadRancherVersion(requestedVersion) {
		return "prime", true
	}
	return requestedDistro, false
}

func isRCSServerBuild(version string) bool {
	return rcsVersionPattern.MatchString(normalizeVersionInput(version))
}

func shouldUseRancherLatestTagOnly(buildType, chartRepoAlias, requestedVersion string) bool {
	return buildType != "release" &&
		chartRepoAlias == "rancher-latest" &&
		!isCommitHeadRancherVersion(requestedVersion) &&
		!isRCSServerBuild(requestedVersion)
}

func applyRancherLatestTagOnlySettings(
	buildType string,
	chartRepoAlias string,
	requestedVersion string,
	rancherImage string,
	rancherImageTag string,
	agentImage string,
	imageExplanation []string,
) (string, string, string, []string, bool) {
	if !shouldUseRancherLatestTagOnly(buildType, chartRepoAlias, requestedVersion) {
		return rancherImage, rancherImageTag, agentImage, imageExplanation, false
	}
	return "", rancherImageTag, "", nil, true
}

func chooseRancherSourceCandidates(requestedDistro, buildType string) ([]string, string, []string) {
	switch requestedDistro {
	case "prime":
		if buildType == "head" {
			return []string{"rancher-prime", "rancher-latest", "optimus-rancher-latest"}, "prime", []string{"Prime head build requested; preferring Prime charts, then exact staging or same-line community chart fallbacks"}
		}
		return []string{"rancher-prime"}, "prime", []string{"Prime distro was requested explicitly"}
	case "community":
		switch buildType {
		case "head":
			return []string{"rancher-latest", "optimus-rancher-latest"}, "community", []string{"Head build requested, using community chart and image sources"}
		case "alpha":
			return []string{"optimus-rancher-alpha", "optimus-rancher-latest", "rancher-alpha", "rancher-latest"}, "community-staging", []string{"Alpha build requested, trying community alpha/staging chart sources first"}
		case "rc":
			return []string{"optimus-rancher-latest", "rancher-latest"}, "community-staging", []string{"RC build requested, trying community staging chart sources first"}
		case "rcs":
			return []string{"optimus-rancher-latest", "rancher-latest"}, "community-staging", []string{"RCS build requested, trying community staging chart sources first"}
		default:
			return []string{"rancher-latest", "optimus-rancher-latest"}, "community", []string{"Released community build requested"}
		}
	default:
		switch buildType {
		case "head":
			return []string{"rancher-latest", "optimus-rancher-latest", "rancher-prime"}, "community", []string{"Head build requested in auto mode, favoring community chart and image sources"}
		case "alpha":
			return []string{"rancher-prime", "optimus-rancher-alpha", "optimus-rancher-latest", "rancher-alpha", "rancher-latest"}, "community-staging", []string{"Alpha build requested in auto mode, favoring Prime/staging chart sources before community charts"}
		case "rc":
			return []string{"rancher-prime", "optimus-rancher-latest", "rancher-latest"}, "community-staging", []string{"RC build requested in auto mode, favoring Prime/staging chart sources before community charts"}
		case "rcs":
			return []string{"rancher-prime", "optimus-rancher-latest", "rancher-latest"}, "community-staging", []string{"RCS build requested in auto mode, favoring Prime/staging chart sources before community charts"}
		default:
			return []string{"rancher-prime", "optimus-rancher-latest", "rancher-latest"}, "community", []string{"Released build requested in auto mode, favoring Prime/staging chart sources before community charts"}
		}
	}
}

func resolveChartAndBaseline(repoCandidates []string, requestedVersion, minorLine, buildType string) (string, string, string, error) {
	if buildType == "head" && requestedVersion == "head" {
		for _, repoAlias := range repoCandidates {
			if repoAlias != "rancher-latest" {
				continue
			}
			results, err := searchHelmRepoVersions(repoAlias)
			if err != nil {
				log.Printf("[resolver] Repo candidate %s query failed for Rancher head: %v", repoAlias, err)
				continue
			}
			latestRelease, err := findLatestRelease(results)
			if err != nil {
				log.Printf("[resolver] Repo candidate %s inspection for head: latestRelease=<none>", repoAlias)
				continue
			}
			log.Printf("[resolver] Repo candidate %s inspection for head: latestRelease=%s", repoAlias, latestRelease)
			return repoAlias, latestRelease, latestRelease, nil
		}
		return "", "", "", fmt.Errorf("could not resolve the latest rancher-latest chart for head")
	}

	if globalExactMatch, err := findExactRequestedChartAcrossRepos(repoCandidates, requestedVersion); err == nil {
		compatibilityBaseline := requestedVersion
		if buildType != "release" {
			compatibilityBaseline, err = resolveCompatibilityBaseline(minorLine)
			if err != nil {
				compatibilityBaseline = requestedVersion
			}
		}
		log.Printf("[resolver] Global exact Rancher chart match selected for %s: %s/rancher@%s", requestedVersion, globalExactMatch.repoAlias, globalExactMatch.chartVersion)
		return globalExactMatch.repoAlias, globalExactMatch.chartVersion, compatibilityBaseline, nil
	}

	var lastErr error
	var bestMatch *resolvedChartMatch
	for _, repoAlias := range repoCandidates {
		results, err := searchHelmRepoVersions(repoAlias)
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
			compatibilityBaseline, baselineErr := resolveCompatibilityBaseline(minorLine)
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
	globalResults, err := searchAllHelmRepoVersions()
	if err != nil {
		return nil, err
	}

	for _, repoAlias := range repoCandidates {
		for _, result := range globalResults {
			if result.Name != fmt.Sprintf("%s/rancher", repoAlias) {
				continue
			}
			if result.Version == requestedVersion || normalizeVersionInput(result.AppVersion) == requestedVersion {
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
	baseline, err := resolveReleasedCompatibilityBaseline(minorLine)
	if err == nil {
		return baseline, nil
	}

	previousMinorLine, previousErr := previousRancherMinorLine(minorLine)
	if previousErr != nil {
		return "", err
	}

	return resolveReleasedCompatibilityBaseline(previousMinorLine)
}

func resolveReleasedCompatibilityBaseline(minorLine string) (string, error) {
	releaseRepos := []string{"rancher-latest", "rancher-prime"}
	var bestVersion *goversion.Version

	for _, repoAlias := range releaseRepos {
		results, err := searchHelmRepoVersions(repoAlias)
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
	output, err := exec.Command("helm", "search", "repo", "rancher", "--devel", "--versions", "-o", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to query helm repo globally for rancher charts: %w", err)
	}

	results, err := parseHelmSearchResults(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse global helm search results: %w", err)
	}
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

func resolveImageSettings(requestedVersion, buildType, resolvedDistro string) (string, string, string, []string) {
	switch resolvedDistro {
	case "prime":
		if buildType == "release" {
			return "registry.rancher.com/rancher/rancher", "", "", []string{"Using Rancher Prime registry because distro=prime was requested explicitly"}
		}
		return "registry.rancher.com/rancher/rancher", "v" + requestedVersion, "", []string{"Using Rancher Prime registry because distro=prime was requested explicitly"}
	case "community-staging":
		imageTag := "v" + requestedVersion
		agentImage := fmt.Sprintf("stgregistry.suse.com/rancher/rancher-agent:%s", imageTag)
		return "stgregistry.suse.com/rancher/rancher", imageTag, agentImage, []string{"Using staging Rancher images because the requested version is not a standard released community build"}
	default:
		if buildType == "release" {
			return "", "", "", []string{"Using released community Rancher chart/image defaults"}
		}
		if requestedVersion == "head" {
			return "", "head", "", []string{"Using released community Rancher chart with the Docker Hub rancher/rancher:head image tag"}
		}
		return "", "v" + requestedVersion, "", []string{"Using released community Rancher chart/image settings"}
	}
}

type commitHeadImageCandidate struct {
	label        string
	registry     string
	rancherImage string
	agentImage   string
}

func resolveCommitHeadImageSettings(requestedVersion string) (string, string, string, []string, error) {
	imageTag := "v" + requestedVersion
	candidates := []commitHeadImageCandidate{
		{
			label:        "staging registry",
			registry:     "stgregistry.suse.com",
			rancherImage: "stgregistry.suse.com/rancher/rancher",
			agentImage:   "stgregistry.suse.com/rancher/rancher-agent:" + imageTag,
		},
		{
			label:        "Docker Hub",
			registry:     "docker.io",
			rancherImage: "docker.io/rancher/rancher",
			agentImage:   "docker.io/rancher/rancher-agent:" + imageTag,
		},
		{
			label:        "Rancher registry",
			registry:     "registry.rancher.com",
			rancherImage: "registry.rancher.com/rancher/rancher",
			agentImage:   "registry.rancher.com/rancher/rancher-agent:" + imageTag,
		},
	}

	var misses []string
	for _, candidate := range candidates {
		serverFound, serverErr := registryImageTagExists(candidate.registry, "rancher/rancher", imageTag)
		agentFound, agentErr := registryImageTagExists(candidate.registry, "rancher/rancher-agent", imageTag)
		if serverErr == nil && agentErr == nil && serverFound && agentFound {
			return candidate.rancherImage, imageTag, candidate.agentImage, []string{
				fmt.Sprintf("Found commit-specific head images in %s; using %s:%s and %s", candidate.label, candidate.rancherImage, imageTag, candidate.agentImage),
			}, nil
		}

		var detail []string
		if serverErr != nil {
			detail = append(detail, "rancher/rancher lookup error: "+serverErr.Error())
		} else if !serverFound {
			detail = append(detail, "rancher/rancher missing")
		}
		if agentErr != nil {
			detail = append(detail, "rancher/rancher-agent lookup error: "+agentErr.Error())
		} else if !agentFound {
			detail = append(detail, "rancher/rancher-agent missing")
		}
		misses = append(misses, fmt.Sprintf("%s (%s)", candidate.label, strings.Join(detail, "; ")))
	}

	return "", "", "", nil, fmt.Errorf("commit-specific head tag %s was not found as a matching Rancher server and agent image pair in checked registries: %s", imageTag, strings.Join(misses, "; "))
}

func isExactCommunityPrereleaseChart(chartRepoAlias string) bool {
	return chartRepoAlias == "rancher-alpha" || chartRepoAlias == "rancher-latest"
}

func isExactStagingPrereleaseChart(chartRepoAlias string) bool {
	return strings.HasPrefix(chartRepoAlias, "optimus-") || chartRepoAlias == "rancher-optimus-alpha" || chartRepoAlias == "optimus-s3"
}

func validateResolvedRancherImages(rancherImage, rancherImageTag, agentImage string) error {
	var images []string
	if rancherImage != "" && rancherImageTag != "" {
		images = append(images, rancherImage+":"+rancherImageTag)
	}
	if rancherImage == "" && rancherImageTag != "" {
		images = append(images, "docker.io/rancher/rancher:"+rancherImageTag)
	}
	if agentImage != "" {
		images = append(images, agentImage)
	}

	for _, image := range images {
		registry, repository, tag, err := parseRegistryImage(image)
		if err != nil {
			return err
		}
		found, err := registryImageTagExists(registry, repository, tag)
		if err != nil {
			return fmt.Errorf("%s: %w", image, err)
		}
		if !found {
			return fmt.Errorf("%s was not found in registry", image)
		}
	}
	return nil
}

func registryImageTagExists(registry, repository, tag string) (bool, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registryBaseURL(registry), repository, tag)
	req, err := http.NewRequest(http.MethodHead, manifestURL, nil)
	if err != nil {
		return false, err
	}
	setRegistryManifestAcceptHeader(req)

	resp, err := rancherRegistryHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	case http.StatusUnauthorized:
		token, err := registryBearerToken(resp.Header.Get("WWW-Authenticate"))
		if err != nil {
			return false, err
		}
		return registryImageTagExistsWithToken(manifestURL, token)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("registry manifest lookup failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
}

func registryImageTagExistsWithToken(manifestURL, token string) (bool, error) {
	req, err := http.NewRequest(http.MethodHead, manifestURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	setRegistryManifestAcceptHeader(req)

	resp, err := rancherRegistryHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("registry authenticated manifest lookup failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
}

func registryBearerToken(authenticate string) (string, error) {
	params, err := parseRegistryBearerChallenge(authenticate)
	if err != nil {
		return "", err
	}
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("registry Bearer challenge missing realm")
	}

	req, err := http.NewRequest(http.MethodGet, realm, nil)
	if err != nil {
		return "", err
	}
	query := req.URL.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	if scope := params["scope"]; scope != "" {
		query.Set("scope", scope)
	}
	req.URL.RawQuery = query.Encode()

	resp, err := rancherRegistryHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("registry token request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var tokenResponse struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", err
	}
	token := tokenResponse.Token
	if token == "" {
		token = tokenResponse.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("registry token response did not include a token")
	}
	return token, nil
}

func parseRegistryBearerChallenge(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return nil, fmt.Errorf("unsupported registry auth challenge %q", value)
	}
	value = strings.TrimSpace(value[len("Bearer "):])
	params := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		key, rawValue, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		params[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(rawValue), `"`)
	}
	return params, nil
}

func setRegistryManifestAcceptHeader(req *http.Request) {
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", "))
}

func parseRegistryImage(image string) (registry, repository, tag string, err error) {
	trimmed := strings.TrimSpace(image)
	lastSlash := strings.LastIndex(trimmed, "/")
	if strings.LastIndex(trimmed, ":") <= lastSlash && strings.LastIndex(trimmed, "@") <= lastSlash {
		return "", "", "", fmt.Errorf("image must include a tag: %s", trimmed)
	}
	// Use the same OCI reference normalization as Image Lookup so namespace-only
	// Docker Hub references do not get mistaken for registry hostnames.
	parsed, parseErr := newImageLookupService().parseReference(image, true)
	if parseErr != nil {
		return "", "", "", parseErr
	}
	if parsed.tag == "" {
		return "", "", "", fmt.Errorf("image must include a tag: %s", trimmed)
	}
	return parsed.registry, parsed.repository, parsed.tag, nil
}

func registryBaseURL(registry string) string {
	if base := rancherRegistryBaseURLs[registry]; base != "" {
		return strings.TrimRight(base, "/")
	}
	if registry == "docker.io" {
		return "https://registry-1.docker.io"
	}
	return "https://" + registry
}

func buildSupportMatrixURL(releasedVersion string) string {
	pathVersion := strings.ReplaceAll(releasedVersion, ".", "-")
	return fmt.Sprintf("https://www.suse.com/suse-rancher/support-matrix/all-supported-versions/rancher-v%s/", pathVersion)
}

type supportMatrixVersion struct {
	Major int
	Minor int
	Patch int
}

func (version supportMatrixVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}

func (version supportMatrixVersion) equal(other supportMatrixVersion) bool {
	return version.Major == other.Major && version.Minor == other.Minor && version.Patch == other.Patch
}

type resolvedSupportMatrixPage struct {
	RequestedURL string
	URL          string
	Body         string
	FallbackNote string
}

type supportMatrixRedirectError struct {
	Requested supportMatrixVersion
	Resolved  supportMatrixVersion
}

func (err supportMatrixRedirectError) Error() string {
	return fmt.Sprintf("support matrix request for Rancher %s resolved to Rancher %s", err.Requested, err.Resolved)
}

func resolveSupportMatrixPage(requestedURL string) (resolvedSupportMatrixPage, error) {
	requestedVersion, err := supportMatrixVersionFromURL(requestedURL)
	if err != nil {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, err
	}

	body, resolvedURL, fetchErr := fetchURLBodyWithResolvedURL(requestedURL)
	if fetchErr == nil {
		if resolvedVersion, resolvedErr := supportMatrixVersionFromURL(resolvedURL); resolvedErr == nil && !resolvedVersion.equal(requestedVersion) {
			fetchErr = supportMatrixRedirectError{Requested: requestedVersion, Resolved: resolvedVersion}
		} else {
			return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL, Body: body}, nil
		}
	}
	if !isMissingSupportMatrixPage(fetchErr) {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fetchErr
	}

	indexBody, resolvedIndexURL, indexErr := fetchURLBodyWithResolvedURL(supportMatrixIndexURL)
	if indexErr != nil {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fmt.Errorf("%w; published support-matrix discovery failed: %v", fetchErr, indexErr)
	}
	versions := supportMatrixVersionsFromHTML(indexBody)
	if resolvedIndexVersion, resolvedErr := supportMatrixVersionFromURL(resolvedIndexURL); resolvedErr == nil {
		versions = append(versions, resolvedIndexVersion)
	}
	fallbackVersion, ok := selectSupportMatrixFallback(requestedVersion, versions)
	if !ok {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fmt.Errorf("%w; no published Rancher support matrix at or before the %d.%d release line was listed by %s", fetchErr, requestedVersion.Major, requestedVersion.Minor, supportMatrixIndexURL)
	}

	fallbackURL := buildSupportMatrixURL(fallbackVersion.String())
	fallbackBody, resolvedFallbackURL, fallbackErr := fetchURLBodyWithResolvedURL(fallbackURL)
	if fallbackErr != nil {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fmt.Errorf("%w; fallback support matrix %s also failed: %v", fetchErr, fallbackURL, fallbackErr)
	}
	if resolvedVersion, resolvedErr := supportMatrixVersionFromURL(resolvedFallbackURL); resolvedErr == nil && !resolvedVersion.equal(fallbackVersion) {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fmt.Errorf("%w; fallback support matrix %s resolved to Rancher %s", fetchErr, fallbackURL, resolvedVersion)
	}

	return resolvedSupportMatrixPage{
		RequestedURL: requestedURL,
		URL:          fallbackURL,
		Body:         fallbackBody,
		FallbackNote: fmt.Sprintf("No SUSE support matrix is published for Rancher %s; using Rancher %s as the nearest published compatibility proxy (the requested build is not being reported as certified)", requestedVersion, fallbackVersion),
	}, nil
}

func supportMatrixVersionFromURL(value string) (supportMatrixVersion, error) {
	match := supportMatrixURLVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 4 {
		return supportMatrixVersion{}, fmt.Errorf("could not determine Rancher version from support matrix URL %q", value)
	}
	major, majorErr := strconv.Atoi(match[1])
	minor, minorErr := strconv.Atoi(match[2])
	patch, patchErr := strconv.Atoi(match[3])
	if majorErr != nil || minorErr != nil || patchErr != nil {
		return supportMatrixVersion{}, fmt.Errorf("invalid Rancher version in support matrix URL %q", value)
	}
	return supportMatrixVersion{Major: major, Minor: minor, Patch: patch}, nil
}

func supportMatrixVersionsFromHTML(body string) []supportMatrixVersion {
	matches := supportMatrixURLVersionPattern.FindAllStringSubmatch(body, -1)
	versions := make([]supportMatrixVersion, 0, len(matches))
	seen := map[supportMatrixVersion]bool{}
	for _, match := range matches {
		if len(match) != 4 {
			continue
		}
		major, majorErr := strconv.Atoi(match[1])
		minor, minorErr := strconv.Atoi(match[2])
		patch, patchErr := strconv.Atoi(match[3])
		if majorErr != nil || minorErr != nil || patchErr != nil {
			continue
		}
		version := supportMatrixVersion{Major: major, Minor: minor, Patch: patch}
		if !seen[version] {
			seen[version] = true
			versions = append(versions, version)
		}
	}
	return versions
}

func selectSupportMatrixFallback(requested supportMatrixVersion, versions []supportMatrixVersion) (supportMatrixVersion, bool) {
	var sameMinor *supportMatrixVersion
	sameMinorDistance := 0
	for _, candidate := range versions {
		if candidate.equal(requested) || candidate.Major != requested.Major || candidate.Minor != requested.Minor {
			continue
		}
		distance := candidate.Patch - requested.Patch
		if distance < 0 {
			distance = -distance
		}
		if sameMinor == nil || distance < sameMinorDistance || (distance == sameMinorDistance && candidate.Patch < sameMinor.Patch) {
			candidateCopy := candidate
			sameMinor = &candidateCopy
			sameMinorDistance = distance
		}
	}
	if sameMinor != nil {
		return *sameMinor, true
	}

	var earlier *supportMatrixVersion
	for _, candidate := range versions {
		if candidate.Major != requested.Major || candidate.Minor >= requested.Minor {
			continue
		}
		if earlier == nil || candidate.Minor > earlier.Minor || (candidate.Minor == earlier.Minor && candidate.Patch > earlier.Patch) {
			candidateCopy := candidate
			earlier = &candidateCopy
		}
	}
	if earlier == nil {
		return supportMatrixVersion{}, false
	}
	return *earlier, true
}

func isMissingSupportMatrixPage(err error) bool {
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusNotFound || statusErr.StatusCode == http.StatusGone
	}
	var redirectErr supportMatrixRedirectError
	return errors.As(err, &redirectErr)
}

func supportMatrixSummary(page resolvedSupportMatrixPage, rangeText string) string {
	if page.FallbackNote == "" {
		return rangeText
	}
	return page.FallbackNote + ". " + rangeText
}

func cacheResolvedSupportMatrixRange(product string, page resolvedSupportMatrixPage, rangeText string, minMinor, maxMinor int) {
	updateSupportRangeCacheWithResolvedURL(product, page.URL, page.URL, rangeText, minMinor, maxMinor)
	if page.RequestedURL != "" && page.RequestedURL != page.URL {
		updateSupportRangeCacheWithResolvedURL(product, page.RequestedURL, page.URL, supportMatrixSummary(page, rangeText), minMinor, maxMinor)
	}
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
	requestedVersion := normalizeVersionInput(chartVersion)
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

func buildAutoHelmCommands(totalHAs int, operation, chartRepoAlias, chartVersion, bootstrapPassword, rancherImage, rancherImageTag, agentImage string, useRancherImageFields bool) []string {
	command := buildAutoHelmCommand(operation, chartRepoAlias, chartVersion, bootstrapPassword, rancherImage, rancherImageTag, agentImage, useRancherImageFields)
	commands := make([]string, totalHAs)
	for i := 0; i < totalHAs; i++ {
		commands[i] = command
	}
	return commands
}

func buildAutoHelmCommand(operation, chartRepoAlias, chartVersion, bootstrapPassword, rancherImage, rancherImageTag, agentImage string, useRancherImageFields bool) string {
	operation = strings.ToLower(strings.TrimSpace(operation))
	if operation == "" {
		operation = rancherHelmOperationInstall
	}
	helmImages := normalizeHelmImageSettings(chartRepoAlias, rancherImage, rancherImageTag, agentImage, useRancherImageFields)

	var baseSettings []string
	switch operation {
	case rancherHelmOperationInstall:
		baseSettings = []string{
			"helm install rancher " + chartRepoAlias + "/rancher \\",
		}
	case rancherHelmOperationUpgrade:
		baseSettings = []string{
			"helm upgrade rancher " + chartRepoAlias + "/rancher \\",
			"  --install \\",
		}
	default:
		panic(fmt.Sprintf("unsupported Rancher Helm operation %q", operation))
	}

	baseSettings = append(baseSettings, []string{
		"  --namespace cattle-system \\",
		"  --version " + chartVersion + " \\",
		"  --set hostname=placeholder \\",
		"  --set-string " + shellQuoteHelmSetString("bootstrapPassword", bootstrapPassword) + " \\",
		"  --set tls=external \\",
		"  --set global.cattle.psp.enabled=false \\",
		"  --set agentTLSMode=system-store",
	}...)

	if helmImages.clearSystemDefaultRegistry {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set systemDefaultRegistry= \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.imageRegistry != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set image.registry=" + helmImages.imageRegistry + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.imageRepository != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set image.repository=" + helmImages.imageRepository + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.imageTag != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set image.tag=" + helmImages.imageTag + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.rancherImage != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set rancherImage=" + helmImages.rancherImage + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.rancherImageTag != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set rancherImageTag=" + helmImages.rancherImageTag + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.agentImage != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set 'extraEnv[0].name=CATTLE_AGENT_IMAGE' \\",
			"  --set 'extraEnv[0].value=" + helmImages.agentImage + "' \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if operation == rancherHelmOperationUpgrade {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set preUpgrade.image.registry=" + rancherHookImageRegistry + " \\",
			"  --wait \\",
			"  --wait-for-jobs \\",
			"  --timeout 30m \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if viper.GetInt("rke2.server_count") == 1 {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set replicas=1 \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}

	return strings.Join(baseSettings, "\n")
}

func shellQuoteHelmSetString(key, value string) string {
	return shellQuote(key + "=" + escapeHelmSetValue(value))
}

func escapeHelmSetValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `,`, `\,`)
	return value
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

type rancherWebhookOverrideValues struct {
	Global rancherWebhookGlobalValues `json:"global"`
	Image  rancherWebhookImageValues  `json:"image"`
}

type rancherWebhookGlobalValues struct {
	Cattle rancherWebhookCattleValues `json:"cattle"`
}

type rancherWebhookCattleValues struct {
	SystemDefaultRegistry string `json:"systemDefaultRegistry"`
}

type rancherWebhookImageValues struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

func rancherHelmCommandWithWebhookImage(command, image string) (string, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return command, nil
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("Rancher Helm command must not be empty")
	}

	payload, err := rancherWebhookValuesJSON(image)
	if err != nil {
		return "", err
	}

	// Rancher's chart forwards this scalar only to its managed webhook chart.
	// This overrides an inherited Prime registry without redirecting Rancher or
	// any other system image.
	return strings.TrimSpace(command) + " \\\n  --set-literal " + shellQuote("webhook="+payload), nil
}

func rancherHelmCommandsWithWebhookImage(commands []string, image string) ([]string, error) {
	configured := make([]string, len(commands))
	for i, command := range commands {
		var err error
		configured[i], err = rancherHelmCommandWithWebhookImage(command, image)
		if err != nil {
			return nil, fmt.Errorf("Helm command %d: %w", i+1, err)
		}
	}
	return configured, nil
}

func rancherWebhookValuesJSON(image string) (string, error) {
	registry, repository, tag, err := parseRegistryImage(image)
	if err != nil {
		return "", fmt.Errorf("parse Rancher webhook image %q: %w", strings.TrimSpace(image), err)
	}
	values := rancherWebhookOverrideValues{
		Global: rancherWebhookGlobalValues{
			Cattle: rancherWebhookCattleValues{
				SystemDefaultRegistry: registry,
			},
		},
		Image: rancherWebhookImageValues{
			Repository: repository,
			Tag:        tag,
		},
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode Rancher webhook Helm values: %w", err)
	}
	return string(payload), nil
}

type helmImageSettings struct {
	clearSystemDefaultRegistry bool
	rancherImage               string
	rancherImageTag            string
	imageRegistry              string
	imageRepository            string
	imageTag                   string
	agentImage                 string
}

func normalizeHelmImageSettings(chartRepoAlias, rancherImage, rancherImageTag, agentImage string, useRancherImageFields bool) helmImageSettings {
	settings := helmImageSettings{
		agentImage: strings.TrimSpace(agentImage),
	}
	rancherImage = strings.TrimSpace(rancherImage)
	rancherImageTag = strings.TrimSpace(rancherImageTag)

	if useRancherImageFields {
		settings.imageTag = rancherImageTag
		if imageRegistry, imageRepository, ok := splitRegistryRepository(rancherImage); ok {
			settings.imageRegistry = imageRegistry
			settings.imageRepository = imageRepository
		} else {
			settings.imageRepository = rancherImage
		}
	} else {
		settings.rancherImage = rancherImage
		settings.rancherImageTag = rancherImageTag
	}

	agentRegistry, _, agentOK := splitRegistryRepository(settings.agentImage)
	// Internal Rancher validation docs for Optimus alpha/head/RC builds pass
	// staging Rancher and agent image refs directly. Newer charts express the
	// Rancher server image via image.* fields, but the intent is the same:
	// staging registry, rancher/rancher repository, requested tag, full staging
	// CATTLE_AGENT_IMAGE. Only the Prime fallback path needs this pressure valve:
	// Prime charts default systemDefaultRegistry to registry.rancher.com, which
	// would otherwise prefix the explicit staging CATTLE_AGENT_IMAGE. Avoid
	// webhook overrides here; the chart defaults webhook to a string and Helm
	// warns when we force it into a nested table from --set.
	if chartRepoAlias == "rancher-prime" && agentOK && agentRegistry != "registry.rancher.com" {
		settings.clearSystemDefaultRegistry = true
	}
	return settings
}

func chartSupportsRancherImageFields(chartRepoAlias, chartVersion string) (bool, error) {
	output, err := exec.Command("helm", "show", "values", chartRepoAlias+"/rancher", "--version", chartVersion).Output()
	if err != nil {
		return false, fmt.Errorf("helm show values failed: %w", err)
	}
	return valuesSupportTopLevelRancherImageFields(string(output)), nil
}

func requireResolvedRancherChartImageFieldSupport(chartRepoAlias, chartVersion string) (bool, error) {
	supported, err := chartSupportsRancherImageFields(chartRepoAlias, chartVersion)
	if err != nil {
		return false, fmt.Errorf("inspect resolved Rancher chart %s/rancher@%s before use: %w", chartRepoAlias, chartVersion, err)
	}
	return supported, nil
}

func valuesSupportTopLevelRancherImageFields(values string) bool {
	inImageBlock := false
	hasRepository := false
	hasTag := false

	for _, line := range strings.Split(values, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			if inImageBlock {
				return hasRepository && hasTag
			}
			if trimmed == "image:" {
				inImageBlock = true
			}
			continue
		}
		if !inImageBlock {
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "repository:"):
			hasRepository = true
		case strings.HasPrefix(trimmed, "tag:"):
			hasTag = true
		}
	}

	return inImageBlock && hasRepository && hasTag
}

func splitRegistryRepository(image string) (string, string, bool) {
	image = strings.TrimSpace(image)
	registry, repository, ok := strings.Cut(image, "/")
	if !ok || registry == "" || repository == "" {
		return "", "", false
	}
	if !strings.Contains(registry, ".") && !strings.Contains(registry, ":") && registry != "localhost" {
		return "", "", false
	}
	return registry, repository, true
}

func fetchURLBody(url string) (string, error) {
	body, _, err := fetchURLBodyWithResolvedURL(url)
	return body, err
}

func fetchURLBodyWithResolvedURL(url string) (string, string, error) {
	resp, err := rancherLookupHTTPClient.Get(url)
	if err != nil {
		return "", url, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	resolvedURL := url
	if resp.Request != nil && resp.Request.URL != nil {
		resolvedURL = resp.Request.URL.String()
	}
	if resp.StatusCode != http.StatusOK {
		return "", resolvedURL, httpStatusError{URL: url, StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resolvedURL, fmt.Errorf("failed to read %s: %w", url, err)
	}
	return string(body), resolvedURL, nil
}

func extractTextFromHTML(document string) (string, error) {
	root, err := html.Parse(strings.NewReader(document))
	if err != nil {
		return "", err
	}

	var textParts []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			text := strings.TrimSpace(node.Data)
			if text != "" {
				textParts = append(textParts, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)

	return strings.Join(textParts, " "), nil
}
