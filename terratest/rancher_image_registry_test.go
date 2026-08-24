package test

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestResolvePreferredRancherImageSettingsValidatesPrimeHeadProvenance(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	sha := strings.Repeat("a", 40)
	tag := "v2.15.1-" + sha + "-head"
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		repository := "rancher/rancher"
		if strings.Contains(reference, "rancher-agent") {
			repository = "rancher/rancher-agent"
		}
		return rancherImageProvenance{
			Reference:          reference,
			SourceURL:          "https://github.com/rancher/rancher",
			Revision:           sha,
			CanonicalReference: repository + ":" + tag,
		}, true, nil
	}

	_, err := resolvePreferredRancherImageSettings(strings.TrimPrefix(tag, "v"), []string{"stgregistry.suse.com"})
	if err == nil || !strings.Contains(err.Error(), "canonical Rancher Prime source") {
		t.Fatalf("expected a patch-qualified Prime head to require Prime provenance, got %v", err)
	}
}

func TestResolvePatchHeadStagingBundlePinsNewestCompletePair(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	const newestSHA = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
	const olderSHA = "2e25858ec600c4f6ae1c005f9f4eb7683ec9fff0"
	created := map[string]time.Time{
		newestSHA: time.Date(2026, 8, 23, 17, 30, 51, 0, time.UTC),
		olderSHA:  time.Date(2026, 8, 22, 9, 32, 38, 0, time.UTC),
	}
	var callsMu sync.Mutex
	var calls []string
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		callsMu.Lock()
		calls = append(calls, reference)
		callsMu.Unlock()
		if !strings.HasPrefix(reference, "stgregistry.suse.com/") {
			return rancherImageProvenance{}, false, errors.New("patch alias inspected a non-staging image")
		}
		sha := newestSHA
		if strings.Contains(reference, olderSHA) {
			sha = olderSHA
		}
		tag := "v2.15.1-" + sha + "-head"
		repository := "rancher/rancher"
		if strings.Contains(reference, "rancher-agent") {
			repository = "rancher/rancher-agent"
		}
		return rancherImageProvenance{
			Reference:          reference,
			Digest:             "sha256:" + strings.Repeat(string(sha[0]), 64),
			CreatedAt:          created[sha],
			SourceURL:          "https://github.com/rancher/rancher-prime",
			OSSRevision:        sha,
			CanonicalReference: repository + ":" + tag,
		}, true, nil
	}

	stubPatchHeadTagList(t, []string{
		"v2.15.1-" + olderSHA + "-head",
		"v2.15.0-" + strings.Repeat("a", 40) + "-head",
		"v2.15.1-" + newestSHA + "-head",
		"v2.15.1-head",
	})
	resolvedVersion, resolution, err := resolvePatchHeadStagingBundle("2.15.1-head")
	if err != nil {
		t.Fatalf("resolvePatchHeadStagingBundle returned error: %v", err)
	}
	wantVersion := "2.15.1-" + newestSHA + "-head"
	if resolvedVersion != wantVersion {
		t.Fatalf("resolved version = %q, want %q", resolvedVersion, wantVersion)
	}
	if resolution.Registry != "stgregistry.suse.com" || resolution.RancherImageTag != "v"+wantVersion || resolution.AgentImage != "stgregistry.suse.com/rancher/rancher-agent:v"+wantVersion {
		t.Fatalf("unexpected staging resolution: %#v", resolution)
	}
	if len(calls) != 4 {
		t.Fatalf("expected one server/agent inspection per matching immutable tag, got %#v", calls)
	}
}

func TestResolvePatchHeadStagingBundleRejectsMismatchedCanonicalPair(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	sha := strings.Repeat("a", 40)
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		canonicalSHA := sha
		repository := "rancher/rancher"
		if strings.Contains(reference, "rancher-agent") {
			canonicalSHA = strings.Repeat("b", 40)
			repository = "rancher/rancher-agent"
		}
		return rancherImageProvenance{
			Reference:          reference,
			CreatedAt:          time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC),
			CanonicalReference: repository + ":v2.15.1-" + canonicalSHA + "-head",
		}, true, nil
	}

	stubPatchHeadTagList(t, []string{"v2.15.1-" + sha + "-head"})
	_, _, err := resolvePatchHeadStagingBundle("2.15.1-head")
	if err == nil || !strings.Contains(err.Error(), "mismatched canonical tags") {
		t.Fatalf("expected canonical mismatch to fail closed, got %v", err)
	}
}

func TestResolvePatchHeadStagingBundleSkipsNewerIncompletePair(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	newerSHA := strings.Repeat("a", 40)
	olderSHA := strings.Repeat("b", 40)
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		sha := olderSHA
		createdAt := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
		if strings.Contains(reference, newerSHA) {
			sha = newerSHA
			createdAt = createdAt.Add(24 * time.Hour)
			if strings.Contains(reference, "rancher-agent") {
				return rancherImageProvenance{Reference: reference}, false, nil
			}
		}
		repository := "rancher/rancher"
		if strings.Contains(reference, "rancher-agent") {
			repository = "rancher/rancher-agent"
		}
		return rancherImageProvenance{
			Reference:          reference,
			CreatedAt:          createdAt,
			SourceURL:          "https://github.com/rancher/rancher-prime",
			OSSRevision:        sha,
			CanonicalReference: repository + ":v2.15.1-" + sha + "-head",
		}, true, nil
	}

	stubPatchHeadTagList(t, []string{
		"v2.15.1-" + newerSHA + "-head",
		"v2.15.1-" + olderSHA + "-head",
	})
	resolvedVersion, _, err := resolvePatchHeadStagingBundle("2.15.1-head")
	if err != nil {
		t.Fatalf("expected older complete pair to resolve, got %v", err)
	}
	if want := "2.15.1-" + olderSHA + "-head"; resolvedVersion != want {
		t.Fatalf("resolved version = %q, want complete pair %q", resolvedVersion, want)
	}
}

func TestResolvePatchHeadStagingBundleFailsClosedOnLookupError(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	newerSHA := strings.Repeat("a", 40)
	olderSHA := strings.Repeat("b", 40)
	inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
		if strings.Contains(reference, newerSHA) {
			return rancherImageProvenance{Reference: reference}, false, errors.New("registry returned 429 Too Many Requests")
		}
		repository := "rancher/rancher"
		if strings.Contains(reference, "rancher-agent") {
			repository = "rancher/rancher-agent"
		}
		return rancherImageProvenance{
			Reference:          reference,
			CreatedAt:          time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC),
			SourceURL:          "https://github.com/rancher/rancher-prime",
			OSSRevision:        olderSHA,
			CanonicalReference: repository + ":v2.15.1-" + olderSHA + "-head",
		}, true, nil
	}

	stubPatchHeadTagList(t, []string{
		"v2.15.1-" + newerSHA + "-head",
		"v2.15.1-" + olderSHA + "-head",
	})
	_, _, err := resolvePatchHeadStagingBundle("2.15.1-head")
	if err == nil || !strings.Contains(err.Error(), "429 Too Many Requests") || !strings.Contains(err.Error(), "could not safely resolve") {
		t.Fatalf("expected lookup error to prevent stale fallback, got %v", err)
	}
}

func TestResolvePatchHeadStagingBundleRequiresBothCreationTimestamps(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })

	sha := strings.Repeat("a", 40)
	for _, missingRole := range []string{"server", "agent"} {
		t.Run(missingRole, func(t *testing.T) {
			inspectPreferredRancherImage = func(_ context.Context, reference string) (rancherImageProvenance, bool, error) {
				role := "server"
				repository := "rancher/rancher"
				if strings.Contains(reference, "rancher-agent") {
					role = "agent"
					repository = "rancher/rancher-agent"
				}
				createdAt := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
				if role == missingRole {
					createdAt = time.Time{}
				}
				return rancherImageProvenance{
					Reference:          reference,
					CreatedAt:          createdAt,
					SourceURL:          "https://github.com/rancher/rancher-prime",
					OSSRevision:        sha,
					CanonicalReference: repository + ":v2.15.1-" + sha + "-head",
				}, true, nil
			}

			stubPatchHeadTagList(t, []string{"v2.15.1-" + sha + "-head"})
			_, _, err := resolvePatchHeadStagingBundle("2.15.1-head")
			if err == nil || !strings.Contains(err.Error(), missingRole+" image did not declare a creation timestamp") {
				t.Fatalf("expected missing %s timestamp to fail, got %v", missingRole, err)
			}
		})
	}
}

func TestResolvePatchHeadStagingBundleSurfacesTagListError(t *testing.T) {
	previousLister := listPatchHeadStagingImageTags
	t.Cleanup(func() { listPatchHeadStagingImageTags = previousLister })
	listPatchHeadStagingImageTags = func(_ context.Context, registry, repository string) ([]string, error) {
		return nil, errors.New("registry returned 429 Too Many Requests")
	}

	_, _, err := resolvePatchHeadStagingBundle("2.15.1-head")
	if err == nil || !strings.Contains(err.Error(), "429 Too Many Requests") {
		t.Fatalf("expected staging tag-list error, got %v", err)
	}
}

func TestResolveCachedPatchHeadStagingBundlePinsDuplicateRowsToOneSnapshot(t *testing.T) {
	cache := map[string]patchHeadStagingResolution{}
	calls := 0
	resolver := func(requestedVersion string) (string, *preferredRancherImageResolution, error) {
		calls++
		sha := strings.Repeat(string(rune('a'+calls-1)), 40)
		version := "2.15.1-" + sha + "-head"
		return version, &preferredRancherImageResolution{RancherImageTag: "v" + version}, nil
	}

	firstVersion, firstImages, err := resolveCachedPatchHeadStagingBundle(cache, "v2.15.1-head", resolver)
	if err != nil {
		t.Fatal(err)
	}
	secondVersion, secondImages, err := resolveCachedPatchHeadStagingBundle(cache, "2.15.1-head", resolver)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want one staging snapshot", calls)
	}
	if firstVersion != secondVersion || firstImages != secondImages {
		t.Fatalf("duplicate aliases did not reuse one resolution: first=%q %#v second=%q %#v", firstVersion, firstImages, secondVersion, secondImages)
	}
}

func stubPatchHeadTagList(t *testing.T, tags []string) {
	t.Helper()
	previousLister := listPatchHeadStagingImageTags
	t.Cleanup(func() { listPatchHeadStagingImageTags = previousLister })
	listPatchHeadStagingImageTags = func(_ context.Context, registry, repository string) ([]string, error) {
		if registry != "stgregistry.suse.com" || repository != "rancher/rancher" {
			t.Fatalf("unexpected patch-head tag listing target %s/%s", registry, repository)
		}
		return append([]string(nil), tags...), nil
	}
}

func TestValidateExactHeadImagePairRejectsWrongCanonicalRepositories(t *testing.T) {
	tag := "v2.15.1-" + strings.Repeat("a", 40) + "-head"
	err := validateExactHeadImagePair(tag,
		rancherImageProvenance{CanonicalReference: "rancher/rancher-agent:" + tag},
		rancherImageProvenance{CanonicalReference: "rancher/rancher-agent:" + tag},
	)
	if err == nil || !strings.Contains(err.Error(), "unexpected canonical repositories") {
		t.Fatalf("expected canonical repository roles to be enforced, got %v", err)
	}
}

func TestValidatePatchHeadServerProvenanceRejectsMismatchedOSSRevision(t *testing.T) {
	sha := strings.Repeat("a", 40)
	err := validatePatchHeadServerProvenance("2.15.1-"+sha+"-head", rancherImageProvenance{
		SourceURL:   "https://github.com/rancher/rancher-prime",
		OSSRevision: strings.Repeat("b", 40),
	})
	if err == nil || !strings.Contains(err.Error(), "public OSS revision") {
		t.Fatalf("expected public revision mismatch to fail, got %v", err)
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

func TestInspectRancherImageReferenceReusesRequestScopedService(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)
	reference, digest := pushImageLookupSourceFixture(t, registryServer, "v2.15.1-head", nil)
	ctx := context.WithValue(context.Background(), preferredRancherImageLookupServiceContextKey{}, service)

	provenance, found, err := inspectRancherImageReference(ctx, reference)
	if err != nil {
		t.Fatalf("inspect through request-scoped service: %v", err)
	}
	if !found || provenance.Reference != reference || provenance.Digest != digest {
		t.Fatalf("unexpected provenance: found=%v provenance=%#v", found, provenance)
	}
}
