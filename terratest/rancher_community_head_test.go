package test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

const communityHeadTestSHA = "c4246d8012e6d1b58a25fbb6776887075a4ca65e"
const communityHeadTestTag = "v2.16-" + communityHeadTestSHA + "-head"

func communityHeadTestImages() map[string]imagelookup.Provenance {
	images := make(map[string]imagelookup.Provenance)
	for _, repository := range []string{"rancher/rancher", "rancher/rancher-agent"} {
		provenance := imagelookup.Provenance{
			Digest:             "sha256:" + strings.Repeat("a", 64),
			BuildVersion:       "main",
			SourceURL:          "https://github.com/rancher/rancher",
			Revision:           communityHeadTestSHA,
			CanonicalReference: repository + ":" + communityHeadTestTag,
		}
		if repository == "rancher/rancher" {
			provenance.ServerVersion = communityHeadTestTag
		} else {
			provenance.Digest = "sha256:" + strings.Repeat("b", 64)
		}
		for _, tag := range []string{"head", communityHeadTestTag} {
			provenance.Reference = "docker.io/" + repository + ":" + tag
			images[provenance.Reference] = provenance
		}
	}
	return images
}

func stubCommunityHeadImages(t *testing.T, images map[string]imagelookup.Provenance) *[]string {
	t.Helper()
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })
	var calls []string
	inspectPreferredRancherImage = func(_ context.Context, reference string) (imagelookup.Provenance, bool, error) {
		calls = append(calls, reference)
		provenance, found := images[reference]
		return provenance, found, nil
	}
	return &calls
}

func TestResolvePreferredCommunityHeadPinsMatchingMainPair(t *testing.T) {
	for _, requested := range []string{"2.16-head", " v2.16-head ", "V2.16-head"} {
		t.Run(requested, func(t *testing.T) {
			calls := stubCommunityHeadImages(t, communityHeadTestImages())
			resolution, err := resolvePreferredRancherImageSettings(requested, []string{"docker.io"})
			if err != nil {
				t.Fatal(err)
			}
			if resolution.HeadAlias != "2.16-head" || resolution.Registry != "docker.io" || resolution.RancherImage != "docker.io/rancher/rancher" || resolution.RancherImageTag != communityHeadTestTag || resolution.AgentImage != "docker.io/rancher/rancher-agent:"+communityHeadTestTag {
				t.Fatalf("unexpected resolution: %#v", resolution)
			}
			wantCalls := []string{
				"docker.io/rancher/rancher:v2.16-head",
				"docker.io/rancher/rancher:head",
				"docker.io/rancher/rancher-agent:head",
				"docker.io/rancher/rancher:" + communityHeadTestTag,
				"docker.io/rancher/rancher-agent:" + communityHeadTestTag,
			}
			if !slices.Equal(*calls, wantCalls) {
				t.Fatalf("inspected %v, want %v", *calls, wantCalls)
			}
		})
	}
}

func TestResolvePreferredCommunityHeadKeepsPublishedAlias(t *testing.T) {
	images := communityHeadTestImages()
	for _, repository := range []string{"rancher/rancher", "rancher/rancher-agent"} {
		images["docker.io/"+repository+":v2.16-head"] = images["docker.io/"+repository+":head"]
	}
	calls := stubCommunityHeadImages(t, images)
	resolution, err := resolvePreferredRancherImageSettings("2.16-head", []string{"docker.io"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.RancherImageTag != "v2.16-head" || resolution.HeadAlias != "" {
		t.Fatalf("published alias was replaced: %#v", resolution)
	}
	wantCalls := []string{"docker.io/rancher/rancher:v2.16-head", "docker.io/rancher/rancher-agent:v2.16-head"}
	if !slices.Equal(*calls, wantCalls) {
		t.Fatalf("unexpected main-head lookup: %v", *calls)
	}
}

func TestResolvePreferredCommunityHeadRejectsUnverifiedFallback(t *testing.T) {
	const serverHead = "docker.io/rancher/rancher:head"
	const agentHead = "docker.io/rancher/rancher-agent:head"
	const serverPinned = "docker.io/rancher/rancher:" + communityHeadTestTag
	const agentPinned = "docker.io/rancher/rancher-agent:" + communityHeadTestTag
	cases := []struct {
		name      string
		reference string
		change    func(*imagelookup.Provenance)
		wantError string
	}{
		{"wrong runtime line", serverHead, func(p *imagelookup.Provenance) { p.ServerVersion = "v2.17-" + communityHeadTestSHA + "-head" }, "does not identify an immutable 2.16-head build"},
		{"missing runtime", serverHead, func(p *imagelookup.Provenance) { p.ServerVersion = "" }, "does not identify an immutable 2.16-head build"},
		{"patch-qualified runtime", serverHead, func(p *imagelookup.Provenance) { p.ServerVersion = "v2.16.0-" + communityHeadTestSHA + "-head" }, "does not identify an immutable 2.16-head build"},
		{"Prime server source", serverHead, func(p *imagelookup.Provenance) { p.SourceURL = "https://github.com/rancher/rancher-prime" }, "public Rancher Community source"},
		{"Prime agent source", agentHead, func(p *imagelookup.Provenance) { p.SourceURL = "https://github.com/rancher/rancher-prime" }, "public Rancher Community source"},
		{"mismatched canonical agent tag", agentHead, func(p *imagelookup.Provenance) {
			p.CanonicalReference = "rancher/rancher-agent:v2.16-" + strings.Repeat("d", 40) + "-head"
		}, "mismatched canonical tags"},
		{"missing immutable agent", agentPinned, nil, "exact Rancher agent image " + agentPinned + " was not found"},
		{"server digest mismatch", serverPinned, func(p *imagelookup.Provenance) { p.Digest = "sha256:" + strings.Repeat("c", 64) }, "image digests differ"},
		{"agent digest mismatch", agentPinned, func(p *imagelookup.Provenance) { p.Digest = "sha256:" + strings.Repeat("c", 64) }, "image digests differ"},
		{"missing digest", serverHead, func(p *imagelookup.Provenance) { p.Digest = "" }, "missing an image digest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			images := communityHeadTestImages()
			if tc.change == nil {
				delete(images, tc.reference)
			} else {
				provenance := images[tc.reference]
				tc.change(&provenance)
				images[tc.reference] = provenance
			}
			stubCommunityHeadImages(t, images)
			resolution, err := resolvePreferredRancherImageSettings("2.16-head", []string{"docker.io"})
			if err == nil || resolution != nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("resolution = %#v, error = %v; want rejection containing %q", resolution, err, tc.wantError)
			}
		})
	}
}

func TestResolvePreferredCommunityHeadDoesNotUseUnselectedDockerHub(t *testing.T) {
	calls := stubCommunityHeadImages(t, communityHeadTestImages())
	resolution, err := resolvePreferredRancherImageSettings("2.16-head", []string{"stgregistry.suse.com"})
	if err == nil || resolution != nil || !strings.Contains(err.Error(), "No unselected registry fallback was attempted") {
		t.Fatalf("resolution = %#v, error = %v; want selected registry failure", resolution, err)
	}
	if !slices.Equal(*calls, []string{"stgregistry.suse.com/rancher/rancher:v2.16-head"}) {
		t.Fatalf("unexpected Docker Hub lookup: %v", *calls)
	}
}

func TestCommunityHeadFallbackOnlyAcceptsMinorSelectors(t *testing.T) {
	for _, requested := range []string{"2.16.0-head", "2.16-" + communityHeadTestSHA + "-head", "2.16.0-" + communityHeadTestSHA + "-head", "head", "2.16.0", "2.16.0-rc1"} {
		t.Run(requested, func(t *testing.T) {
			calls := stubCommunityHeadImages(t, nil)
			if isCommunityMinorHeadSelector(requested) {
				t.Fatal("non-minor selector accepted")
			}
			if _, err := resolveCommunityMainHeadAlias(requested); err == nil {
				t.Fatal("direct alias fallback accepted a non-minor selector")
			}
			if len(*calls) != 0 {
				t.Fatalf("invalid selector caused registry requests: %v", *calls)
			}
			if _, err := resolvePreferredRancherImageSettings(requested, []string{"docker.io"}); err == nil {
				t.Fatal("missing exact image unexpectedly resolved")
			}
			want := "docker.io/rancher/rancher:" + imagelookup.NormalizeDockerRancherTag(requested)
			if !slices.Equal(*calls, []string{want}) {
				t.Fatalf("non-minor request caused fallback lookups: %v", *calls)
			}
		})
	}
}
