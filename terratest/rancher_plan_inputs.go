package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/spf13/viper"
	"regexp"
	"slices"
	"strings"
)

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
	registry, repository, tag, err := imagelookup.ParseRegistryImage(image)
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
			normalizedVersion := imagelookup.NormalizeVersionInput(version)
			if normalizedVersion == "" {
				return nil, fmt.Errorf("rancher.versions[%d] must not be empty", i)
			}
			normalized = append(normalized, normalizedVersion)
		}
		return normalized, nil
	}

	requestedVersion := imagelookup.NormalizeVersionInput(viper.GetString("rancher.version"))
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
	case imagelookup.PatchHeadAliasVersionPattern.MatchString(version):
		matches := imagelookup.PatchHeadAliasVersionPattern.FindStringSubmatch(version)
		return "head", matches[1], nil
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
	tag = imagelookup.NormalizeVersionInput(tag)
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

	registry, repository, tag, err := imagelookup.ParseRegistryImage(value)
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

func isPrimeHeadRancherVersion(version string) bool {
	return imagelookup.IsPrimeCommitHeadRancherVersion(version) || imagelookup.IsPatchHeadAliasRancherVersion(version)
}

func validateRequestedRancherDistro(requestedDistro, buildType, requestedVersion string, isCustomImage bool) error {
	if requestedDistro == "community" && !isCustomImage && isPrimeHeadRancherVersion(requestedVersion) {
		return fmt.Errorf("patch-qualified Rancher head %s is a Prime staging build; use rancher.distro=auto or rancher.distro=prime", requestedVersion)
	}
	if requestedDistro != "prime" || buildType == "release" {
		return nil
	}
	if buildType == "head" && (isCustomImage || isPrimeHeadRancherVersion(requestedVersion)) {
		return nil
	}
	return fmt.Errorf("prime distro requires a released Rancher version, an exact custom image, or a patch-qualified Prime head like 2.14.5-head or 2.14.5-{SHA}-head")
}

func effectiveRequestedRancherDistro(requestedDistro, requestedVersion string) (string, bool) {
	requestedDistro = strings.ToLower(strings.TrimSpace(requestedDistro))
	if requestedDistro == "auto" && isPrimeHeadRancherVersion(requestedVersion) {
		return "prime", true
	}
	return requestedDistro, false
}

func isRCSServerBuild(version string) bool {
	return rcsVersionPattern.MatchString(imagelookup.NormalizeVersionInput(version))
}

func shouldUseRancherLatestTagOnly(buildType, chartRepoAlias, requestedVersion string) bool {
	return buildType != "release" &&
		chartRepoAlias == "rancher-latest" &&
		!isCommitHeadRancherVersion(requestedVersion) &&
		!imagelookup.IsPatchHeadAliasRancherVersion(requestedVersion) &&
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
