package test

import (
	"fmt"
	"strings"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

func isCommunityMinorHeadSelector(version string) bool {
	version = imagelookup.NormalizeVersionInput(version)
	buildType, minorLine, err := classifyRancherVersion(version)
	return err == nil && buildType == "head" && minorLine != "" && version == minorLine+"-head"
}

// Rancher publishes main as head, and release branches as vX.Y-head. A missing
// minor alias may use main only if the public build identifies that exact line.
// Pin its verified immutable pair so a later movement of head cannot change it.
func resolveCommunityMainHeadAlias(requestedVersion string) (*preferredRancherImageResolution, error) {
	requestedVersion = imagelookup.NormalizeVersionInput(requestedVersion)
	if !isCommunityMinorHeadSelector(requestedVersion) {
		return nil, fmt.Errorf("%s is not a Community minor-head selector", requestedVersion)
	}
	head, err := inspectExplicitRancherImagePair("docker.io/rancher/rancher", "head", "docker.io/rancher/rancher-agent:head")
	if err != nil {
		return nil, fmt.Errorf("could not verify Docker Hub Community head for %s: %w", requestedVersion, err)
	}
	version := imagelookup.NormalizeVersionInput(head.RancherProvenance.ServerVersion)
	_, minorLine, versionErr := classifyRancherVersion(version)
	if versionErr != nil || !isCommitHeadRancherVersion(version) || imagelookup.IsPrimeCommitHeadRancherVersion(version) || minorLine+"-head" != requestedVersion {
		return nil, fmt.Errorf("Docker Hub Community head reports runtime version %q, which does not identify an immutable %s build; refusing to substitute another release line", head.RancherProvenance.ServerVersion, requestedVersion)
	}
	for _, provenance := range []imagelookup.Provenance{head.RancherProvenance, head.AgentProvenance} {
		if strings.TrimSuffix(provenance.SourceURL, ".git") != "https://github.com/rancher/rancher" {
			return nil, fmt.Errorf("Docker Hub head pair does not declare the public Rancher Community source for both images")
		}
		if provenance.Digest == "" {
			return nil, fmt.Errorf("Docker Hub head pair is missing an image digest")
		}
	}
	tag := imagelookup.NormalizeDockerRancherTag(version)
	if err := validateExactHeadImagePair(tag, head.RancherProvenance, head.AgentProvenance); err != nil {
		return nil, err
	}
	pinned, err := inspectExplicitRancherImagePair("docker.io/rancher/rancher", tag, "docker.io/rancher/rancher-agent:"+tag)
	if err != nil {
		return nil, fmt.Errorf("verify immutable Community head pair for %s: %w", requestedVersion, err)
	}
	if pinned.RancherProvenance.Digest != head.RancherProvenance.Digest || pinned.AgentProvenance.Digest != head.AgentProvenance.Digest {
		return nil, fmt.Errorf("Docker Hub head and immutable %s image digests differ; retry after image publication completes", tag)
	}
	pinned.HeadAlias = requestedVersion
	return pinned, nil
}
