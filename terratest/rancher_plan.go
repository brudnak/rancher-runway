package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	rancherHelmOperationInstall = "install"
	rancherHelmOperationUpgrade = "upgrade"
	supportMatrixIndexURL       = "https://www.suse.com/suse-rancher/support-matrix/all-supported-versions/"
	rancherResolverHTTPTimeout  = 30 * time.Second
)

var rancherRegistryHTTPClient = &http.Client{Timeout: rancherResolverHTTPTimeout}

var rancherRegistryBaseURLs = map[string]string{}

var commitHeadVersionPattern = regexp.MustCompile(`^(\d+\.\d+)(?:\.\d+)?-[0-9a-fA-F]{7,40}-head$`)

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
		if isPrimeHeadRancherVersion(requestedVersion) && len(preferredRegistries) > 0 && !slices.Contains(preferredRegistries, "stgregistry.suse.com") {
			return nil, fmt.Errorf("patch-qualified Rancher head %s is staging-only; include stgregistry.suse.com in rancher.preferred_image_registries or clear the preference", requestedVersion)
		}
		switch {
		case !isCustomImage && imagelookup.IsPrimeCommitHeadRancherVersion(requestedVersion):
			preferredImageResolutions[planIndex], err = resolvePreferredRancherImageSettings(requestedVersion, []string{"stgregistry.suse.com"})
			if err != nil {
				return nil, fmt.Errorf("verify staging Rancher images for %s: %w", requestedVersion, err)
			}
		case len(preferredRegistries) > 0 && !isCustomImage && !imagelookup.IsPatchHeadAliasRancherVersion(requestedVersion):
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
	patchHeadResolutionCache := map[string]patchHeadStagingResolution{}
	helmSearchSession := newHelmChartSearchSession()
	chartImageFieldSupportCache := map[string]bool{}
	supportMatrixResolutionCache := map[string]supportedRKE2Resolution{}
	rke2PatchResolutionCache := map[int]string{}
	installerSHA256Cache := map[string]string{}
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
		appliedImageRegistries := preferredRegistries
		if inferredPrimeHead {
			explanation = []string{"Patch-qualified Prime head selected Prime chart sources automatically"}
		}
		if len(preferredRegistries) > 0 && isCustomImage {
			explanation = append(explanation, "Preferred registry checkboxes were ignored because the exact custom image reference controls its registry")
		}
		chartRequest := requestedVersion
		resolvedPatchHeadVersion := ""
		if imagelookup.IsPatchHeadAliasRancherVersion(requestedVersion) {
			resolvedPatchHeadVersion, preferredImageResolution, err = resolveCachedPatchHeadStagingBundle(patchHeadResolutionCache, requestedVersion, resolvePatchHeadStagingBundle)
			if err != nil {
				return nil, fmt.Errorf("resolve patch-qualified Rancher head %s: %w", requestedVersion, err)
			}
			chartRequest = resolvedPatchHeadVersion
			appliedImageRegistries = []string{"stgregistry.suse.com"}
			explanation = append(explanation,
				fmt.Sprintf("Resolved mutable %s to the newest complete staging server/agent pair %s", requestedVersion, resolvedPatchHeadVersion),
				"Restricted patch-qualified head image resolution to SUSE staging",
			)
		} else if imagelookup.IsPrimeCommitHeadRancherVersion(requestedVersion) {
			appliedImageRegistries = []string{"stgregistry.suse.com"}
		}
		if isCustomImage {
			chartRequest = rancherVersionHintFromImageTag(customImage.tag)
			if chartRequest == "" {
				chartRequest = "head"
			}
		}
		chartRepoAlias, chartVersion, compatibilityBaseline, err := resolveChartAndBaselineWithSearchSession(helmSearchSession, repoCandidates, chartRequest, minorLine, buildType)
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
			if imagelookup.IsPatchHeadAliasRancherVersion(requestedVersion) {
				imageExplanation = []string{
					fmt.Sprintf("Pinned %s to staging Rancher server and agent images tagged v%s", requestedVersion, resolvedPatchHeadVersion),
					"Recorded resolution-time OCI digests and provenance for the immutable staging pair",
				}
			} else if imagelookup.IsPrimeCommitHeadRancherVersion(requestedVersion) {
				imageExplanation = []string{
					"Verified the patch-qualified Rancher server and agent image pair in SUSE staging before provisioning",
					"Restricted patch-qualified head image resolution to SUSE staging",
					"Recorded resolution-time OCI digests and provenance for the immutable staging pair",
				}
			} else {
				imageExplanation = preferredImageResolutionExplanation(preferredImageResolution, preferredRegistries)
			}
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
		useRancherImageFields, err := resolveChartImageFieldSupportWithCache(chartImageFieldSupportCache, chartRepoAlias, chartVersion)
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
			supportResolution, err := resolveSupportMatrixWithCache(supportMatrixResolutionCache, supportMatrixURL)
			if err != nil {
				return nil, err
			}
			highestRKE2Minor := supportResolution.HighestMinor
			supportMatrixURL = supportResolution.ResolvedURL
			explanation = append(explanation, supportResolution.Explanation)

			recommendedRKE2Version, err = resolveRKE2PatchWithCache(rke2PatchResolutionCache, highestRKE2Minor)
			if err != nil {
				return nil, err
			}
			explanation = append(explanation, fmt.Sprintf("Selected %s as the latest available RKE2 patch in the supported v1.%d line", recommendedRKE2Version, highestRKE2Minor))

			installerSHA256, err = resolveInstallerSHA256WithCache(installerSHA256Cache, recommendedRKE2Version)
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
			appliedPreferredRegistries = append([]string(nil), appliedImageRegistries...)
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
