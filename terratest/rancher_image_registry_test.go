package test

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
)

func TestResolvePreferredRancherImageSettingsUsesFirstCompletePair(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	var calls []string
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		calls = append(calls, reference)
		if !strings.HasPrefix(reference, "docker.io/") {
			return rancherImageProvenance{Reference: reference}, false, nil
		}
		provenance := rancherImageProvenance{Reference: reference, Digest: "sha256:" + strings.Repeat("a", 64)}
		if strings.Contains(reference, "/rancher:") {
			provenance.BuildVersion = "v2.14-head-build-991"
			provenance.SourceURL = "https://github.com/rancher/rancher"
			provenance.Revision = strings.Repeat("b", 40)
		}
		return provenance, true, nil
	}

	resolution, err := resolvePreferredRancherImageSettings("2.14-head", []string{
		"stgregistry.suse.com",
		"docker.io",
		"registry.suse.com",
	})
	if err != nil {
		t.Fatalf("resolvePreferredRancherImageSettings returned error: %v", err)
	}
	if resolution.Registry != "docker.io" || resolution.RancherImage != "docker.io/rancher/rancher" || resolution.RancherImageTag != "v2.14-head" || resolution.AgentImage != "docker.io/rancher/rancher-agent:v2.14-head" {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
	wantCalls := []string{
		"stgregistry.suse.com/rancher/rancher:v2.14-head",
		"registry.suse.com/rancher/rancher:v2.14-head",
		"docker.io/rancher/rancher:v2.14-head",
		"docker.io/rancher/rancher-agent:v2.14-head",
	}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("registry calls = %#v, want %#v", calls, wantCalls)
	}
	if got := rancherImageSourceCommitURL(resolution.RancherProvenance.SourceURL, resolution.RancherProvenance.Revision); got != "https://github.com/rancher/rancher/commit/"+strings.Repeat("b", 40) {
		t.Fatalf("source commit URL = %q", got)
	}
}

func TestResolvePreferredRancherImageSettingsFailsClosedForSelectedRegistry(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	var calls []string
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		calls = append(calls, reference)
		return rancherImageProvenance{Reference: reference}, strings.Contains(reference, "rancher-agent"), nil
	}

	_, err := resolvePreferredRancherImageSettings("2.14-head", []string{"stgregistry.suse.com"})
	if err == nil {
		t.Fatal("expected selected-registry miss to fail")
	}
	message := err.Error()
	if !strings.Contains(message, "stgregistry.suse.com/rancher/rancher:v2.14-head") || !strings.Contains(message, "No unselected registry fallback was attempted") {
		t.Fatalf("missing actionable failure detail: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("a missing server should skip the irrelevant agent lookup, got %#v", calls)
	}
}

func TestInspectExplicitRancherImagePairRecordsBothDigests(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		digestCharacter := "a"
		if strings.Contains(reference, "rancher-agent") {
			digestCharacter = "b"
		}
		return rancherImageProvenance{
			Reference:          reference,
			Digest:             "sha256:" + strings.Repeat(digestCharacter, 64),
			SourceURL:          "https://github.com/rancher/rancher-prime",
			Revision:           strings.Repeat("c", 40),
			CanonicalReference: strings.TrimPrefix(reference, "stgregistry.suse.com/"),
		}, true, nil
	}

	tag := "v2.14.5-" + strings.Repeat("d", 40) + "-head"
	resolution, err := inspectExplicitRancherImagePair(
		"stgregistry.suse.com/rancher/rancher",
		tag,
		"stgregistry.suse.com/rancher/rancher-agent:"+tag,
	)
	if err != nil {
		t.Fatalf("inspectExplicitRancherImagePair returned error: %v", err)
	}
	if resolution.Registry != "stgregistry.suse.com" {
		t.Fatalf("unexpected registry %q", resolution.Registry)
	}
	if resolution.RancherProvenance.Digest != "sha256:"+strings.Repeat("a", 64) || resolution.AgentProvenance.Digest != "sha256:"+strings.Repeat("b", 64) {
		t.Fatalf("unexpected pair provenance: %#v", resolution)
	}
}

func TestInspectExplicitRancherImagePairRejectsMismatchedAgentProvenance(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	tag := "v2.14.5-" + strings.Repeat("d", 40) + "-head"
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		canonical := "rancher/rancher:" + tag
		if strings.Contains(reference, "rancher-agent") {
			canonical = "rancher/rancher-agent:v2.14.5-" + strings.Repeat("e", 40) + "-head"
		}
		return rancherImageProvenance{Reference: reference, CanonicalReference: canonical}, true, nil
	}

	_, err := inspectExplicitRancherImagePair(
		"stgregistry.suse.com/rancher/rancher",
		tag,
		"stgregistry.suse.com/rancher/rancher-agent:"+tag,
	)
	if err == nil || !strings.Contains(err.Error(), "mismatched canonical tags") {
		t.Fatalf("expected mismatched agent provenance to be rejected, got %v", err)
	}
}

func TestResolvePreferredRancherImageSettingsSurfacesLookupErrorWithoutFallback(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	var calls []string
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		calls = append(calls, reference)
		return rancherImageProvenance{}, false, errors.New("registry returned 429 Too Many Requests")
	}

	_, err := resolvePreferredRancherImageSettings("head", []string{"stgregistry.suse.com", "docker.io"})
	if err == nil || !strings.Contains(err.Error(), "429 Too Many Requests") {
		t.Fatalf("expected registry lookup error, got %v", err)
	}
	if len(calls) != 1 || calls[0] != "stgregistry.suse.com/rancher/rancher:head" {
		t.Fatalf("lookup error should stop before fallback, got calls %#v", calls)
	}
}

func TestPreferredRancherImageSettingsFeedExplicitHelmImages(t *testing.T) {
	resolution := &preferredRancherImageResolution{
		Registry:        "registry.suse.com",
		RancherImage:    "registry.suse.com/rancher/rancher",
		RancherImageTag: "v2.14-head",
		AgentImage:      "registry.suse.com/rancher/rancher-agent:v2.14-head",
	}
	command := buildAutoHelmCommand(
		rancherHelmOperationInstall,
		"rancher-latest",
		"2.14.9",
		"secret",
		resolution.RancherImage,
		resolution.RancherImageTag,
		resolution.AgentImage,
		true,
	)
	for _, want := range []string{
		"image.registry=registry.suse.com",
		"image.repository=rancher/rancher",
		"image.tag=v2.14-head",
		"registry.suse.com/rancher/rancher-agent:v2.14-head",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("Helm command did not contain %q:\n%s", want, command)
		}
	}
}

func TestRancherImageSourceCommitURLRequiresCanonicalLabels(t *testing.T) {
	revision := strings.Repeat("c", 40)
	if got := rancherImageSourceCommitURL("https://example.com/rancher/rancher", revision); got != "" {
		t.Fatalf("non-GitHub source produced link %q", got)
	}
	if got := rancherImageSourceCommitURL("https://github.com/rancher/rancher", "short-sha"); got != "" {
		t.Fatalf("short revision produced link %q", got)
	}
}

func TestSafeOCIProvenanceLabelFlattensControlWhitespaceAndBoundsLength(t *testing.T) {
	got := safeOCIProvenanceLabel("  build\nvalue\t\x00" + strings.Repeat("x", 600))
	if strings.ContainsAny(got, "\n\r\t") {
		t.Fatalf("provenance label retained control whitespace: %q", got)
	}
	if !strings.HasPrefix(got, "build value ") || len(got) != 512 {
		t.Fatalf("unexpected sanitized provenance label length/prefix: len=%d value=%q", len(got), got)
	}
}

func TestInspectRancherImageReferenceWithServiceExtractsProvenanceAndMapsNotFound(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)
	revision := strings.Repeat("d", 40)
	reference, digest := pushImageLookupSourceFixture(t, registryServer, "v2.14-head", map[string]string{
		imageLookupVersionLabel:            "v2.14-head-build-42",
		imageLookupSourceLabel:             "https://github.com/rancher/rancher",
		imageLookupRevisionLabel:           revision,
		imageLookupOSSRevisionLabel:        strings.Repeat("e", 40),
		imageLookupCanonicalReferenceLabel: "rancher/rancher:v2.14-head",
	})

	provenance, found, err := inspectRancherImageReferenceWithService(context.Background(), service, reference)
	if err != nil {
		t.Fatalf("inspect fixture: %v", err)
	}
	if !found || provenance.Reference != reference || provenance.Digest != digest || provenance.BuildVersion != "v2.14-head-build-42" || provenance.SourceURL != "https://github.com/rancher/rancher" || provenance.Revision != revision || provenance.CanonicalReference != "rancher/rancher:v2.14-head" {
		t.Fatalf("unexpected provenance: found=%v provenance=%#v", found, provenance)
	}

	missingReference := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:missing"
	missing, found, err := inspectRancherImageReferenceWithService(context.Background(), service, missingReference)
	if err != nil || found || missing.Reference != missingReference {
		t.Fatalf("missing tag mapping: found=%v provenance=%#v err=%v", found, missing, err)
	}
}
