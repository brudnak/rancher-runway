package imagelookup

import (
	"fmt"
	"regexp"
	"strings"
)

func NormalizeVersionInput(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		return value
	}
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "V")
	if strings.EqualFold(value, "head") {
		return "head"
	}
	return value
}

func IsPrimeCommitHeadRancherVersion(version string) bool {
	return primeCommitHeadVersionPattern.MatchString(NormalizeVersionInput(version))
}

func IsPatchHeadAliasRancherVersion(version string) bool {
	return PatchHeadAliasVersionPattern.MatchString(NormalizeVersionInput(version))
}
func NormalizeDockerRancherTag(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return version
	}
	if NormalizeVersionInput(version) == "head" {
		return "head"
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
func ParseRegistryImage(image string) (registry, repository, tag string, err error) {
	trimmed := strings.TrimSpace(image)
	lastSlash := strings.LastIndex(trimmed, "/")
	if strings.LastIndex(trimmed, ":") <= lastSlash && strings.LastIndex(trimmed, "@") <= lastSlash {
		return "", "", "", fmt.Errorf("image must include a tag: %s", trimmed)
	}
	// Use the same OCI reference normalization as Image Lookup so namespace-only
	// Docker Hub references do not get mistaken for registry hostnames.
	parsed, parseErr := New().parseReference(image, true)
	if parseErr != nil {
		return "", "", "", parseErr
	}
	if parsed.tag == "" {
		return "", "", "", fmt.Errorf("image must include a tag: %s", trimmed)
	}
	return parsed.registry, parsed.repository, parsed.tag, nil
}

var primeCommitHeadVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+-[0-9a-fA-F]{7,40}-head$`)

var PatchHeadAliasVersionPattern = regexp.MustCompile(`^(\d+\.\d+)\.\d+-head$`)
var ReadinessHeadPattern = regexp.MustCompile(`(?i)^(?:head|v?[0-9]+\.[0-9]+(?:\.[0-9]+)?-head)$`)
