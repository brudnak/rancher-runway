package test

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type deployedImageSource struct {
	Registry     string `json:"registry"`
	Label        string `json:"label"`
	Distribution string `json:"distribution"`
}

var deployedImageSources = []deployedImageSource{
	{"docker.io", "Community · Docker Hub", "community"},
	{"stgregistry.suse.com", "SUSE staging", "prime"},
	{"registry.rancher.com", "Rancher Prime", "prime"},
	{"registry.suse.com", "SUSE registry", "prime"},
}

func deployedHeadTargets(ctx context.Context, distribution, registry string, list func(context.Context, string, string) ([]string, error)) (any, error) {
	if distribution != "community" && distribution != "prime" {
		return nil, fmt.Errorf("choose Community or Prime")
	}
	if registry == "" {
		registry = "docker.io"
		if distribution == "prime" {
			registry = "stgregistry.suse.com"
		}
	}
	allowed := false
	for _, source := range deployedImageSources {
		if source.Registry == registry {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("choose a listed image source, or use an exact image reference")
	}
	tags, err := list(ctx, registry, "rancher/rancher")
	if err != nil {
		return nil, fmt.Errorf("could not list published head images from %s: %w", registry, err)
	}
	images := []string{}
	for _, tag := range tags {
		if tag == "head" || strings.Contains(tag, "-head") {
			images = append(images, registry+"/rancher/rancher:"+tag)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(images)))
	return map[string]any{"images": images, "registry": registry}, nil
}

// Read only selected-image metadata; preflight separately re-resolves and checks
// both server and agent digests before a plan can be approved.
func deployedUpgradeImageDetails(ctx context.Context, reference string, inspect rancherImageInspectFunc) (map[string]any, error) {
	custom, ok, err := parseCustomRancherImageRequest(reference)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("enter a full Rancher image repository and exact tag")
	}
	reference = custom.serverRepository + ":" + custom.tag
	metadata, found, err := inspect(ctx, reference)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("selected Rancher image was not found")
	}
	resolved, source, versionErr := resolveUpgradeImageVersion(metadata, custom.tag)
	note := ""
	if versionErr != nil {
		note = versionErr.Error()
	}
	registry, _, _ := splitRegistryRepository(custom.serverRepository)
	return map[string]any{
		"reference": reference, "registry": registry, "digest": metadata.Digest,
		"version": resolved, "versionSource": source, "versionLabel": metadata.BuildVersion,
		"revision": metadata.Revision, "ossRevision": metadata.OSSRevision,
		"canonicalReference": metadata.CanonicalReference, "created": metadata.CreatedAt,
		"source": metadata.SourceURL, "commitURL": rancherImageSourceCommitURL(metadata.SourceURL, metadata.Revision),
		"versionNote": note,
	}, nil
}
