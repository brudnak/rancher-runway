package test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/viper"
)

func prepareHostedTenantRancherConfiguration(totalInstances int) ([]*RancherResolvedPlan, error) {
	mode := rancherMode()
	switch mode {
	case "", "manual":
		return prepareManualHostedTenantPlans(totalInstances)
	case "auto":
		plans, err := resolveAutoHostedTenantPlans(totalInstances)
		if err != nil {
			return nil, err
		}

		helmCommands := make([]string, 0, len(plans))
		k3sVersions := make([]string, 0, len(plans))
		installChecksums := map[string]string{}
		airgapChecksums := map[string]string{}
		for _, plan := range plans {
			helmCommands = append(helmCommands, plan.HelmCommands...)
			k3sVersions = append(k3sVersions, plan.RecommendedK3SVersion)
			installChecksums[plan.RecommendedK3SVersion] = plan.K3SInstallerSHA256
			if plan.K3SAirgapImageSHA256 != "" {
				airgapChecksums[plan.RecommendedK3SVersion] = plan.K3SAirgapImageSHA256
			}
		}
		viper.Set("rancher.helm_commands", helmCommands)
		viper.Set("k3s.versions", k3sVersions)
		viper.Set("k3s.install_script_sha256s", installChecksums)
		viper.Set("k3s.airgap_image_sha256s", airgapChecksums)
		return plans, nil
	default:
		return nil, fmt.Errorf("unsupported rancher.mode %q", mode)
	}
}

func prepareManualHostedTenantPlans(totalInstances int) ([]*RancherResolvedPlan, error) {
	helmCommands := viper.GetStringSlice("rancher.helm_commands")
	if len(helmCommands) != totalInstances {
		return nil, fmt.Errorf("rancher.helm_commands has %d entries but total_rancher_instances is %d", len(helmCommands), totalInstances)
	}

	k3sVersions, err := requestedHostedK3SVersions(totalInstances)
	if err != nil {
		return nil, err
	}

	plans := make([]*RancherResolvedPlan, 0, len(k3sVersions))
	for i, version := range k3sVersions {
		installChecksum, err := hostedK3SChecksumForVersion("k3s.install_script_sha256s", "k3s.install_script_sha256", version)
		if err != nil {
			return nil, err
		}
		airgapChecksum := ""
		if viper.GetBool("k3s.preload_images") {
			airgapChecksum, err = hostedK3SChecksumForVersion("k3s.airgap_image_sha256s", "k3s.airgap_image_sha256", version)
			if err != nil {
				return nil, err
			}
		}

		plans = append(plans, &RancherResolvedPlan{
			Mode:                  "manual",
			RecommendedK3SVersion: version,
			K3SInstallerSHA256:    installChecksum,
			K3SAirgapImageSHA256:  airgapChecksum,
			HelmCommands:          []string{strings.TrimSpace(helmCommands[i])},
		})
	}
	return plans, nil
}

func resolveAutoHostedTenantPlans(totalInstances int) ([]*RancherResolvedPlan, error) {
	plans, err := resolveAutoRancherPlans(totalInstances)
	if err != nil {
		return nil, err
	}

	for _, plan := range plans {
		highestK3SMinor, supportExplanation, err := resolveHighestSupportedHostedK3SMinor(plan.SupportMatrixURL)
		if err != nil {
			return nil, err
		}
		recommendedK3S, err := resolveLatestHostedK3SPatch(highestK3SMinor)
		if err != nil {
			return nil, err
		}
		installSHA, err := resolveHostedRemoteSHA256(hostedK3SInstallScriptURL(recommendedK3S))
		if err != nil {
			return nil, err
		}
		airgapSHA := ""
		if viper.GetBool("k3s.preload_images") {
			airgapSHA, err = resolveHostedRemoteSHA256(hostedK3SAirgapImageURL(recommendedK3S))
			if err != nil {
				return nil, err
			}
		}

		plan.RecommendedK3SVersion = recommendedK3S
		plan.K3SInstallerSHA256 = installSHA
		plan.K3SAirgapImageSHA256 = airgapSHA
		plan.Explanation = append(plan.Explanation,
			supportExplanation,
			fmt.Sprintf("Selected %s as the latest available K3s patch in the supported v1.%d line", recommendedK3S, highestK3SMinor),
		)
	}
	return plans, nil
}

func requestedHostedK3SVersions(totalInstances int) ([]string, error) {
	requestedVersions := viper.GetStringSlice("k3s.versions")
	if len(requestedVersions) > 0 {
		if len(requestedVersions) != totalInstances {
			return nil, fmt.Errorf("k3s.versions has %d entries but total_rancher_instances is %d", len(requestedVersions), totalInstances)
		}
		out := make([]string, 0, len(requestedVersions))
		for i, version := range requestedVersions {
			normalized := normalizeHostedK3SVersion(version)
			if normalized == "" {
				return nil, fmt.Errorf("k3s.versions[%d] must not be empty", i)
			}
			out = append(out, normalized)
		}
		return out, nil
	}

	requestedVersion := normalizeHostedK3SVersion(viper.GetString("k3s.version"))
	if requestedVersion == "" {
		return nil, fmt.Errorf("set k3s.version for a single instance or k3s.versions with %d entries", totalInstances)
	}
	if totalInstances > 1 {
		return nil, fmt.Errorf("total_rancher_instances is %d, so k3s.versions must contain %d versions", totalInstances, totalInstances)
	}
	return []string{requestedVersion}, nil
}

func hostedK3SChecksumForVersion(mapKey, singleKey, version string) (string, error) {
	checksums := viper.GetStringMapString(mapKey)
	if checksum := strings.TrimSpace(checksums[version]); checksum != "" {
		return checksum, nil
	}
	if strings.TrimSpace(viper.GetString("k3s.version")) == version {
		if checksum := strings.TrimSpace(viper.GetString(singleKey)); checksum != "" {
			return checksum, nil
		}
	}
	return "", fmt.Errorf("%s.%s must be set", mapKey, version)
}

func resolveHighestSupportedHostedK3SMinor(supportMatrixURL string) (int, string, error) {
	body, err := fetchURLBody(supportMatrixURL)
	if err != nil {
		return resolveCachedSupportRange("K3s", supportMatrixURL, err)
	}
	textContent, err := extractTextFromHTML(body)
	if err != nil {
		return resolveCachedSupportRange("K3s", supportMatrixURL, fmt.Errorf("failed to parse support matrix page: %w", err))
	}

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`K3s\s+v1\.(\d+)\s+v1\.(\d+)`),
		regexp.MustCompile(`K3s[^\n\r]*?v1\.(\d+)[^\n\r]*?v1\.(\d+)`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(textContent)
		if len(matches) != 3 {
			continue
		}
		highest, err := goversion.NewVersion(matches[2])
		if err != nil {
			return 0, "", fmt.Errorf("failed to parse supported K3s minor %q: %w", matches[2], err)
		}
		segments := highest.Segments64()
		if len(segments) == 0 {
			return 0, "", fmt.Errorf("unexpected supported K3s minor value %q", highest.Original())
		}
		maxMinor := int(segments[0])
		var minMinor int
		fmt.Sscanf(matches[1], "%d", &minMinor)
		rangeText := fmt.Sprintf("Support matrix certifies K3s from v1.%s through v1.%s", matches[1], matches[2])
		updateSupportRangeCache("K3s", supportMatrixURL, rangeText, minMinor, maxMinor)
		return maxMinor, rangeText, nil
	}
	return resolveCachedSupportRange("K3s", supportMatrixURL, fmt.Errorf("could not find supported K3s range in support matrix page"))
}

func resolveLatestHostedK3SPatch(highestMinor int) (string, error) {
	releaseNotesURL := fmt.Sprintf("https://docs.k3s.io/release-notes/v1.%d.X", highestMinor)
	config := releaseProductConfig{
		ProductName:       "K3s",
		CacheKey:          "k3s",
		Pattern:           regexp.MustCompile(fmt.Sprintf(`v1\.%d\.\d+\+k3s\d+`, highestMinor)),
		GitHubTagRefsURL:  fmt.Sprintf("https://api.github.com/repos/k3s-io/k3s/git/matching-refs/tags/v1.%d.", highestMinor),
		GitHubBuildPrefix: "+k3s",
	}
	return resolveLatestCachedReleasePatch(config, highestMinor, releaseNotesURL, func(matches []string) (string, error) {
		return highestSemverReleaseVersion(matches, "+k3s")
	})
}

func resolveHostedRemoteSHA256(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status %d downloading %s", resp.StatusCode, url)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, resp.Body); err != nil {
		return "", fmt.Errorf("failed hashing %s: %w", url, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func hostedK3SAirgapImageURL(version string) string {
	return fmt.Sprintf("https://github.com/k3s-io/k3s/releases/download/%s/k3s-airgap-images-amd64.tar.zst", strings.ReplaceAll(version, "+", "%2B"))
}

func hostedK3SInstallScriptURL(version string) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/k3s-io/k3s/%s/install.sh", strings.ReplaceAll(version, "+", "%2B"))
}

func normalizeHostedK3SVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
