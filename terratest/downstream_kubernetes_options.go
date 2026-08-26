package test

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
)

type downstreamKubernetesRelease struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func resolveDownstreamKubernetesVersion(rancherURL, bearerToken, distribution, requested string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	return resolveDownstreamKubernetesVersionWithClient(client, rancherURL, bearerToken, distribution, requested)
}

func resolveDownstreamKubernetesVersionWithClient(client *http.Client, rancherURL, bearerToken, distribution, requested string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("Rancher HTTP client must not be nil")
	}
	rancherURL = strings.TrimRight(clickableURL(rancherURL), "/")
	if rancherURL == "" {
		return "", fmt.Errorf("Rancher URL must not be empty")
	}
	if strings.TrimSpace(bearerToken) == "" {
		return "", fmt.Errorf("bearer token must not be empty")
	}
	distribution = strings.ToLower(strings.TrimSpace(distribution))
	if distribution != "k3s" && distribution != "rke2" {
		return "", fmt.Errorf("downstream distribution must be k3s or rke2")
	}

	candidate := normalizeDownstreamKubernetesVersion(requested)
	if candidate == "" {
		settingVersion, err := downstreamDefaultKubernetesVersion(client, rancherURL, bearerToken, distribution)
		if err != nil {
			return "", err
		}
		candidate = settingVersion
	}

	releases, err := liveDownstreamKubernetesReleases(client, rancherURL, bearerToken, distribution)
	if err != nil {
		return "", err
	}
	available := make(map[string]struct{}, len(releases))
	for _, release := range releases {
		for _, rawVersion := range []string{release.Version, release.ID} {
			version := normalizeDownstreamKubernetesVersion(rawVersion)
			if !downstreamVersionMatchesDistribution(version, distribution) {
				continue
			}
			if _, err := parseDownstreamKubernetesVersion(version); err != nil {
				continue
			}
			available[version] = struct{}{}
		}
	}
	if len(available) == 0 {
		return "", fmt.Errorf("Rancher %s release endpoint did not return any valid provisionable versions", strings.ToUpper(distribution))
	}

	if candidate != "" {
		if !downstreamVersionMatchesDistribution(candidate, distribution) {
			return "", fmt.Errorf("Kubernetes version %q does not match %s", candidate, strings.ToUpper(distribution))
		}
		if _, ok := available[candidate]; !ok {
			return "", fmt.Errorf("Kubernetes version %s is not present in Rancher's live %s release list", candidate, strings.ToUpper(distribution))
		}
		return candidate, nil
	}

	latest, err := latestDownstreamKubernetesVersion(available)
	if err != nil {
		return "", err
	}
	return latest, nil
}

func downstreamDefaultKubernetesVersion(client *http.Client, rancherURL, bearerToken, distribution string) (string, error) {
	var setting struct {
		Value   string `json:"value"`
		Default string `json:"default"`
	}
	settingURL := fmt.Sprintf("%s/v3/settings/%s-default-version", rancherURL, distribution)
	if err := getRancherJSON(client, settingURL, bearerToken, &setting); err != nil {
		return "", fmt.Errorf("failed to read %s-default-version setting: %w", distribution, err)
	}
	version := strings.TrimSpace(setting.Value)
	if version == "" {
		version = strings.TrimSpace(setting.Default)
	}
	return normalizeDownstreamKubernetesVersion(version), nil
}

func liveDownstreamKubernetesReleases(client *http.Client, rancherURL, bearerToken, distribution string) ([]downstreamKubernetesRelease, error) {
	var collection struct {
		Data []downstreamKubernetesRelease `json:"data"`
	}
	releaseURL := fmt.Sprintf("%s/v1-%s-release/releases", rancherURL, distribution)
	if err := getRancherJSON(client, releaseURL, bearerToken, &collection); err != nil {
		return nil, fmt.Errorf("failed to read live Rancher %s releases: %w", strings.ToUpper(distribution), err)
	}
	return collection.Data, nil
}

func latestDownstreamKubernetesVersion(versions map[string]struct{}) (string, error) {
	var selected *goversion.Version
	selectedText := ""
	selectedRevision := int64(-1)
	for version := range versions {
		parsed, err := parseDownstreamKubernetesVersion(version)
		if err != nil {
			continue
		}
		revision, hasRevision := downstreamProviderRevision(version)
		selectCandidate := selected == nil || selected.LessThan(parsed)
		if selected != nil && selected.Equal(parsed) {
			switch {
			case hasRevision && selectedRevision < 0:
				selectCandidate = true
			case hasRevision && selectedRevision >= 0:
				selectCandidate = revision > selectedRevision || (revision == selectedRevision && version > selectedText)
			case !hasRevision && selectedRevision < 0:
				selectCandidate = version > selectedText
			default:
				selectCandidate = false
			}
		}
		if selectCandidate {
			selected = parsed
			selectedText = version
			if hasRevision {
				selectedRevision = revision
			} else {
				selectedRevision = -1
			}
		}
	}
	if selectedText == "" {
		return "", fmt.Errorf("Rancher release list did not contain a valid Kubernetes version")
	}
	return selectedText, nil
}

func downstreamProviderRevision(version string) (int64, bool) {
	metadataIndex := strings.Index(version, "+")
	if metadataIndex < 0 || metadataIndex+1 >= len(version) {
		return 0, false
	}
	metadata := strings.ToLower(version[metadataIndex+1:])
	for _, prefix := range []string{"k3s", "rke2r"} {
		if !strings.HasPrefix(metadata, prefix) {
			continue
		}
		digits := metadata[len(prefix):]
		end := 0
		for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
			end++
		}
		if end == 0 {
			return 0, false
		}
		revision, err := strconv.ParseInt(digits[:end], 10, 64)
		if err != nil {
			return 0, false
		}
		return revision, true
	}
	return 0, false
}

func parseDownstreamKubernetesVersion(version string) (*goversion.Version, error) {
	version = normalizeDownstreamKubernetesVersion(version)
	if version == "" {
		return nil, fmt.Errorf("version must not be empty")
	}
	parsed, err := goversion.NewVersion(strings.TrimPrefix(version, "v"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse version %q: %w", version, err)
	}
	return parsed, nil
}

func normalizeDownstreamKubernetesVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return version
	}
	if strings.HasPrefix(version, "V") {
		return "v" + strings.TrimPrefix(version, "V")
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func downstreamVersionMatchesDistribution(version, distribution string) bool {
	version = strings.ToLower(strings.TrimSpace(version))
	distribution = strings.ToLower(strings.TrimSpace(distribution))
	return strings.HasPrefix(version, "v1.") && strings.Contains(version, "+"+distribution)
}
