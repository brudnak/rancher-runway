package test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestImageLookupParseReferenceNormalizesAndRejectsUnsafeInput(t *testing.T) {
	service := &imageLookupService{}
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name           string
		input          string
		wantCanonical  string
		wantRegistry   string
		wantRepository string
		wantTag        string
		wantDigest     string
	}{
		{
			name:           "unqualified Docker Hub repository",
			input:          "rancher/rancher:v2.16.0",
			wantCanonical:  "docker.io/rancher/rancher:v2.16.0",
			wantRegistry:   "docker.io",
			wantRepository: "rancher/rancher",
			wantTag:        "v2.16.0",
		},
		{
			name:           "single component Docker Hub image",
			input:          "ubuntu:24.04",
			wantCanonical:  "docker.io/library/ubuntu:24.04",
			wantRegistry:   "docker.io",
			wantRepository: "library/ubuntu",
			wantTag:        "24.04",
		},
		{
			name:           "staging reference with transport scheme",
			input:          "docker://stgregistry.suse.com/rancher/rancher:v2.16.0-rcs-0844.1",
			wantCanonical:  "stgregistry.suse.com/rancher/rancher:v2.16.0-rcs-0844.1",
			wantRegistry:   "stgregistry.suse.com",
			wantRepository: "rancher/rancher",
			wantTag:        "v2.16.0-rcs-0844.1",
		},
		{
			name:           "digest reference",
			input:          "registry.suse.com/rancher/rancher@" + digest,
			wantCanonical:  "registry.suse.com/rancher/rancher@" + digest,
			wantRegistry:   "registry.suse.com",
			wantRepository: "rancher/rancher",
			wantDigest:     digest,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := service.parseReference(testCase.input, true)
			if err != nil {
				t.Fatalf("parseReference(%q): %v", testCase.input, err)
			}
			if got.canonical != testCase.wantCanonical || got.registry != testCase.wantRegistry || got.repository != testCase.wantRepository || got.tag != testCase.wantTag || got.digest != testCase.wantDigest {
				t.Fatalf("parseReference(%q) = %#v, want canonical=%q registry=%q repository=%q tag=%q digest=%q", testCase.input, got, testCase.wantCanonical, testCase.wantRegistry, testCase.wantRepository, testCase.wantTag, testCase.wantDigest)
			}
		})
	}

	invalid := []string{
		"",
		"rancher/rancher",
		"http://registry.example.com/rancher/rancher:v2.16.0",
		"registry.example.com:/rancher/rancher:v2.16.0",
		"registry.example.com/rancher/../admin:v2.16.0",
		"registry.example.com/rancher/rancher:v2.16.0?debug=true",
		"registry.example.com/rancher/rancher:v2.16.0 other",
		"user:password@registry.example.com/rancher/rancher:v2.16.0",
		strings.Repeat("a", 1025),
	}
	for _, input := range invalid {
		t.Run("reject_"+strings.ReplaceAll(input, "/", "_"), func(t *testing.T) {
			if _, err := service.parseReference(input, true); err == nil {
				t.Fatalf("parseReference(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestImageLookupSearchTargetsAndLimitValidation(t *testing.T) {
	service := &imageLookupService{}

	targets, query, limit, err := service.searchTargets(imageLookupSearchRequest{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		Query:      "head",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("searchTargets accepted UI limit: %v", err)
	}
	if limit != 200 || query != "head" || len(targets) != 1 || targets[0] != (imageLookupTarget{registry: "stgregistry.suse.com", repository: "rancher/rancher"}) {
		t.Fatalf("unexpected explicit search target: targets=%#v query=%q limit=%d", targets, query, limit)
	}

	targets, query, _, err = service.searchTargets(imageLookupSearchRequest{
		Registry:   "all",
		Repository: "stgregistry.suse.com/rancher/rancher:v2.16.0-rcs-0844.1",
	})
	if err != nil {
		t.Fatalf("searchTargets full reference: %v", err)
	}
	if len(targets) != 1 || targets[0].registry != "stgregistry.suse.com" || targets[0].repository != "rancher/rancher" || query != "v2.16.0-rcs-0844.1" {
		t.Fatalf("full reference was not narrowed correctly: targets=%#v query=%q", targets, query)
	}

	targets, _, limit, err = service.searchTargets(imageLookupSearchRequest{Registry: "all", Repository: "all"})
	if err != nil {
		t.Fatalf("searchTargets all known sources: %v", err)
	}
	if want := len(imageLookupKnownRegistries) * len(imageLookupKnownRepositories); len(targets) != want {
		t.Fatalf("all-source target count = %d, want %d", len(targets), want)
	}
	if limit != imageLookupDefaultResultLimit {
		t.Fatalf("default limit = %d, want %d", limit, imageLookupDefaultResultLimit)
	}

	for _, request := range []imageLookupSearchRequest{
		{Registry: "docker.io", Repository: "rancher/rancher", Limit: 201},
		{Registry: "docker.io", Repository: "rancher/rancher", Limit: -1},
		{Registry: "docker.io", Repository: "rancher/rancher", Query: "two words"},
	} {
		if _, _, _, err := service.searchTargets(request); err == nil {
			t.Fatalf("searchTargets(%#v) unexpectedly succeeded", request)
		}
	}
}

func TestImageLookupTagClassificationFilteringAndNaturalOrder(t *testing.T) {
	channelTests := map[string]string{
		"head":                         "head",
		"v2.17-head-amd64":             "head",
		"v2.17.0-devel-123":            "devel",
		"v2.17.0-alpha1":               "alpha",
		"v2.17.0-beta2":                "devel",
		"v2.16.0-rcs-0844.1":           "rcs",
		"v2.16.0-rc1":                  "rc",
		"v2.16.0":                      "stable",
		"v2.16.0-rcs-0844.1-arm64":     "rcs",
		"v2.16.0-rcs-0844.1-linux-386": "rcs",
	}
	for tag, want := range channelTests {
		if got := imageLookupTagChannel(tag); got != want {
			t.Errorf("imageLookupTagChannel(%q) = %q, want %q", tag, got, want)
		}
	}

	if !imageLookupTagMatches("v2.16.0-RCS-0844.1", "rcs", "rcs") {
		t.Fatal("quick rcs filter did not match the rcs channel")
	}
	if imageLookupTagMatches("v2.16.0-rcs-0844.1", "rcs", "rc") {
		t.Fatal("quick rc filter must not include security rcs tags")
	}
	if !imageLookupTagMatches("v2.16.0-rcs-0844.1", "rcs", "0844") {
		t.Fatal("substring filter did not match an exact build fragment")
	}
	if !imageLookupTagMatches("v2.17.0-alpha10", "alpha", "alpha") {
		t.Fatal("quick alpha filter did not match the alpha channel")
	}
	if !imageLookupTagMatches("v2.17.0-alpha10", "alpha", "devel") {
		t.Fatal("broad devel filter did not include the alpha channel")
	}
	if imageLookupTagMatches("v2.17.0-devel-10", "devel", "alpha") {
		t.Fatal("quick alpha filter must not include other devel builds")
	}

	architectureTests := []struct {
		tag              string
		wantArchitecture string
		wantBase         string
	}{
		{tag: "v2.16.0-rcs-0844.1-amd64", wantArchitecture: "amd64", wantBase: "v2.16.0-rcs-0844.1"},
		{tag: "v2.16.0-rcs-0844.1_arm64", wantArchitecture: "arm64", wantBase: "v2.16.0-rcs-0844.1"},
		{tag: "v2.16.0-linux-amd64", wantArchitecture: "amd64", wantBase: "v2.16.0"},
		{tag: "v2.16.0", wantArchitecture: "multi", wantBase: "v2.16.0"},
	}
	for _, testCase := range architectureTests {
		architecture, base := imageLookupTagArchitecture(testCase.tag)
		if architecture != testCase.wantArchitecture || base != testCase.wantBase {
			t.Errorf("imageLookupTagArchitecture(%q) = (%q, %q), want (%q, %q)", testCase.tag, architecture, base, testCase.wantArchitecture, testCase.wantBase)
		}
	}

	for tag, want := range map[string]bool{
		"sha256-deadbeef.sig":       true,
		"v2.16.0.sbom":              true,
		"v2.16.0.attestation":       true,
		"cosign-signature-deadbeef": true,
		"v2.16.0-rcs-0844.1":        false,
	} {
		if got := imageLookupArtifactTag(tag); got != want {
			t.Errorf("imageLookupArtifactTag(%q) = %t, want %t", tag, got, want)
		}
	}

	tags := []string{"v2.9.0", "v2.10.0", "v2.10.0-rc2", "v2.10.0-rc10"}
	sort.Slice(tags, func(i, j int) bool {
		return imageLookupNaturalCompare(tags[i], tags[j]) > 0
	})
	wantOrder := []string{"v2.10.0-rc10", "v2.10.0-rc2", "v2.10.0", "v2.9.0"}
	if !slices.Equal(tags, wantOrder) {
		t.Fatalf("natural descending order = %v, want %v", tags, wantOrder)
	}
	alphaTags := []string{"v2.17.0-alpha2", "v2.17.0-alpha10", "v2.17.0-alpha1"}
	sort.Slice(alphaTags, func(i, j int) bool {
		return imageLookupNaturalCompare(alphaTags[i], alphaTags[j]) > 0
	})
	wantAlphaOrder := []string{"v2.17.0-alpha10", "v2.17.0-alpha2", "v2.17.0-alpha1"}
	if !slices.Equal(alphaTags, wantAlphaOrder) {
		t.Fatalf("natural alpha order = %v, want %v", alphaTags, wantAlphaOrder)
	}

	for _, query := range []string{"v2.16.0", "2.16.0", "v2.16.0-rc1", "v2.16.0-rcs-0844.1", "v2.16.0-amd64"} {
		if !imageLookupFullVersionTag(query) {
			t.Errorf("full Rancher version tag %q was not recognized", query)
		}
	}
	for _, query := range []string{"", "0844", "deadbeef", "rcs", "v2.16", "release candidate"} {
		if imageLookupFullVersionTag(query) {
			t.Errorf("search fragment %q was misclassified as a full version tag", query)
		}
	}
}

func TestImageLookupExactTagReferenceEligibility(t *testing.T) {
	repository, err := name.NewRepository("registry.example.com/rancher/rancher", name.StrictValidation)
	if err != nil {
		t.Fatalf("parse exact-tag test repository: %v", err)
	}
	for _, query := range []string{"v2.16.0-rcs-0844.1", "0844", "deadbeef"} {
		if tag, ok := imageLookupExactTagReference(repository, query, false); !ok || tag.TagStr() != query {
			t.Errorf("valid exact-tag candidate %q produced tag %#v, eligible=%t", query, tag, ok)
		}
	}
	for _, query := range []string{"", "head", "devel", "alpha", "rcs", "rc", "stable", "all", "bad/tag"} {
		if tag, ok := imageLookupExactTagReference(repository, query, false); ok {
			t.Errorf("non-exact query %q unexpectedly produced tag %#v", query, tag)
		}
	}
}

func TestImageLookupSearchPaginatesRegistryAndFiltersArtifacts(t *testing.T) {
	var tagRequests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			response.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			response.WriteHeader(http.StatusOK)
		case "/v2/rancher/rancher/tags/list":
			tagRequests.Add(1)
			response.Header().Set("Content-Type", "application/json")
			if request.URL.Query().Get("n") != "1000" {
				t.Errorf("registry page size = %q, want 1000", request.URL.Query().Get("n"))
			}
			if request.URL.Query().Get("last") == "" {
				response.Header().Set("Link", `</v2/rancher/rancher/tags/list?n=1000&last=v2.16.0-rcs-0844.1-amd64>; rel="next"`)
				_ = json.NewEncoder(response).Encode(map[string]any{
					"name": "rancher/rancher",
					"tags": []string{"v2.9.0", "v2.16.0-rcs-0844.1-amd64", "sha256-deadbeef.sig"},
				})
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"name": "rancher/rancher",
				"tags": []string{"v2.10.0", "v2.16.0-rcs-0844.1-arm64", "head"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("Search local paginated registry: %v", err)
	}
	if tagRequests.Load() != 2 {
		t.Fatalf("tag page requests = %d, want 2", tagRequests.Load())
	}
	if len(result.Groups) != 1 {
		t.Fatalf("search groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]
	if group.Scanned != 6 || group.Matched != 5 || group.Truncated {
		t.Fatalf("unexpected page accounting: scanned=%d matched=%d truncated=%t error=%q", group.Scanned, group.Matched, group.Truncated, group.Error)
	}
	gotTags := make([]string, len(group.Tags))
	for index, tag := range group.Tags {
		gotTags[index] = tag.Name
		if imageLookupArtifactTag(tag.Name) {
			t.Fatalf("artifact tag %q leaked into default search", tag.Name)
		}
	}
	wantTags := []string{"v2.16.0-rcs-0844.1-arm64", "v2.16.0-rcs-0844.1-amd64", "v2.10.0", "v2.9.0", "head"}
	if !slices.Equal(gotTags, wantTags) {
		t.Fatalf("searched tags = %v, want %v", gotTags, wantTags)
	}
	if group.Tags[0].Architecture != "arm64" || group.Tags[0].BaseTag != "v2.16.0-rcs-0844.1" || group.Tags[0].Channel != "rcs" {
		t.Fatalf("unexpected tag metadata: %#v", group.Tags[0])
	}
}

func TestImageLookupSearchExactTagUsesManifestFastPath(t *testing.T) {
	var manifestRequests atomic.Int32
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/manifests/") {
			manifestRequests.Add(1)
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	created := time.Date(2026, time.August, 5, 15, 0, 0, 0, time.UTC)
	fixtureImage := newImageLookupFixtureImage(t, "amd64", "", created, map[string]string{
		"build.yaml": "release: exact-fast-path\n",
	})
	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse exact-tag fixture reference: %v", err)
	}
	if err := remote.Write(reference, fixtureImage,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push exact-tag fixture image: %v", err)
	}
	expectedDigest, err := fixtureImage.Digest()
	if err != nil {
		t.Fatalf("fixture image digest: %v", err)
	}
	manifestRequests.Store(0)
	tagListRequests.Store(0)

	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Query:      "v2.16.0-rcs-0844.1",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("Search exact local tag: %v", err)
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("exact-tag search made %d tag-list requests, want 0", tagListRequests.Load())
	}
	if manifestRequests.Load() == 0 {
		t.Fatal("exact-tag search did not request the manifest")
	}
	if len(result.Groups) != 1 {
		t.Fatalf("search groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]
	if group.Error != "" || group.Scanned != 1 || group.Matched != 1 || group.Truncated || len(group.Tags) != 1 {
		t.Fatalf("unexpected exact-tag group: %#v", group)
	}
	tag := group.Tags[0]
	if tag.Name != "v2.16.0-rcs-0844.1" || tag.Channel != "rcs" || tag.Architecture != "multi" || tag.BaseTag != tag.Name {
		t.Fatalf("unexpected exact-tag classification: %#v", tag)
	}
	if tag.Digest != expectedDigest.String() || tag.Size != 0 {
		t.Fatalf("exact-tag descriptor = digest %q size %d, want digest %q and unknown image size", tag.Digest, tag.Size, expectedDigest.String())
	}
}

func TestImageLookupSearchExactTagNotFoundFallsBackToListing(t *testing.T) {
	var missingManifestRequests atomic.Int32
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/manifests/0844") {
			missingManifestRequests.Add(1)
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	fixtureImage := newImageLookupFixtureImage(t, "amd64", "", time.Now().UTC(), map[string]string{
		"README.txt": "fallback fixture",
	})
	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse fallback fixture reference: %v", err)
	}
	if err := remote.Write(reference, fixtureImage,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push fallback fixture image: %v", err)
	}
	missingManifestRequests.Store(0)
	tagListRequests.Store(0)

	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Query:      "0844",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("Search partial tag after exact miss: %v", err)
	}
	if missingManifestRequests.Load() == 0 {
		t.Fatal("partial query did not attempt the valid exact tag first")
	}
	if tagListRequests.Load() == 0 {
		t.Fatal("404 exact-tag response did not fall back to tag listing")
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Tags) != 1 || result.Groups[0].Tags[0].Name != "v2.16.0-rcs-0844.1" {
		t.Fatalf("unexpected fallback result: %#v", result.Groups)
	}
}

func TestImageLookupSearchMissingFullVersionDoesNotList(t *testing.T) {
	var manifestRequests atomic.Int32
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/manifests/") {
			manifestRequests.Add(1)
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Query:      "v2.16.0-rcs-0844.1",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("Search missing full version tag: %v", err)
	}
	if manifestRequests.Load() == 0 {
		t.Fatal("missing full version did not attempt the exact manifest")
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("missing full version made %d tag-list requests, want 0", tagListRequests.Load())
	}
	if len(result.Groups) != 1 {
		t.Fatalf("search groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]
	if group.Error != "" || group.Scanned != 1 || group.Matched != 0 || group.Truncated || len(group.Tags) != 0 {
		t.Fatalf("unexpected missing full-version group: %#v", group)
	}
}

func TestImageLookupSearchExactTagNonNotFoundDoesNotList(t *testing.T) {
	var tagListRequests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/v2/":
			response.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			response.WriteHeader(http.StatusOK)
		case strings.Contains(request.URL.Path, "/manifests/"):
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"errors": []map[string]string{{"code": "UNAVAILABLE", "message": "try later"}},
			})
		case strings.HasSuffix(request.URL.Path, "/tags/list"):
			tagListRequests.Add(1)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"name": "rancher/rancher",
				"tags": []string{"v2.16.0"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	group := service.searchTarget(context.Background(), imageLookupTarget{
		registry:   imageLookupTestServerHost(t, registryServer),
		repository: "rancher/rancher",
	}, "v2.16.0", 200, false)
	if group.Error != "registry returned 503 Service Unavailable" {
		t.Fatalf("exact-tag non-404 error = %q, want registry 503", group.Error)
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("non-404 exact-tag failure made %d tag-list requests, want 0", tagListRequests.Load())
	}
}

func TestImageLookupSearchStopsWhenResultLimitIsCollected(t *testing.T) {
	var tagListRequests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			response.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			response.WriteHeader(http.StatusOK)
		case "/v2/rancher/rancher/tags/list":
			tagListRequests.Add(1)
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Link", `</v2/rancher/rancher/tags/list?n=1000&last=v3>; rel="next"`)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"name": "rancher/rancher",
				"tags": []string{"v1", "v2", "v3"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("Search with result limit: %v", err)
	}
	if tagListRequests.Load() != 1 {
		t.Fatalf("tag-list requests = %d, want 1", tagListRequests.Load())
	}
	group := result.Groups[0]
	if group.Scanned != 2 || group.Matched != 2 || !group.Truncated || len(group.Tags) != 2 {
		t.Fatalf("unexpected limited search group: %#v", group)
	}
	if group.Tags[0].Name != "v2" || group.Tags[1].Name != "v1" {
		t.Fatalf("limited search order = %#v, want v2 then v1", group.Tags)
	}
}

func TestImageLookupInspectOCIIndexConfigLayersAndBuildYAML(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	created := time.Date(2026, time.August, 5, 14, 30, 0, 0, time.UTC)
	amd64Image := newImageLookupFixtureImage(t, "amd64", "", created, map[string]string{
		"usr/src/rancher/build.yaml": "webhookVersion: v0.12.1-rcs-0844.1\nrelease: security\n",
	})
	arm64Image := newImageLookupFixtureImage(t, "arm64", "v8", created.Add(time.Minute), map[string]string{
		"README.txt": "arm64 fixture",
	})
	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: amd64Image,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{
				OS:           "linux",
				Architecture: "amd64",
			}},
		},
		mutate.IndexAddendum{
			Add: arm64Image,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{
				OS:           "linux",
				Architecture: "arm64",
				Variant:      "v8",
			}},
		},
	)
	index = mutate.IndexMediaType(index, types.OCIImageIndex)

	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse fixture reference: %v", err)
	}
	if err := remote.WriteIndex(reference, index,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push fixture image index: %v", err)
	}

	result, err := service.Inspect(context.Background(), imageLookupInspectRequest{
		Reference:        referenceText,
		Platform:         "linux/amd64",
		IncludeBuildYAML: true,
	})
	if err != nil {
		t.Fatalf("Inspect local OCI index: %v", err)
	}
	if result.Reference != referenceText || result.Registry != imageLookupTestServerHost(t, registryServer) || result.Repository != "rancher/rancher" || result.Tag != "v2.16.0-rcs-0844.1" {
		t.Fatalf("unexpected normalized inspection identity: %#v", result)
	}
	if result.MediaType != string(types.OCIImageIndex) || len(result.Platforms) != 2 {
		t.Fatalf("unexpected index metadata: mediaType=%q platforms=%#v", result.MediaType, result.Platforms)
	}
	if result.Platform != "linux/amd64" || result.Config.OS != "linux" || result.Config.Architecture != "amd64" {
		t.Fatalf("unexpected selected configuration: platform=%q config=%#v", result.Platform, result.Config)
	}
	if result.CreatedAt != created.Format(time.RFC3339Nano) || result.Config.CreatedAt != created.Format(time.RFC3339Nano) {
		t.Fatalf("created timestamp = response %q config %q, want %q", result.CreatedAt, result.Config.CreatedAt, created.Format(time.RFC3339Nano))
	}
	if result.Config.Labels["org.opencontainers.image.version"] != "fixture-amd64" || !slices.Equal(result.Config.Env, []string{"FIXTURE=yes"}) || !slices.Equal(result.Config.Entrypoint, []string{"/usr/bin/rancher"}) || !slices.Equal(result.Config.Cmd, []string{"server"}) {
		t.Fatalf("unexpected image config details: %#v", result.Config)
	}
	if len(result.Config.History) != 2 || result.Config.History[0].Created != created.Format(time.RFC3339Nano) || result.Config.History[0].CreatedBy != "RUN fixture-build --arch=amd64" || result.Config.History[0].Comment != "fixture layer" || result.Config.History[0].EmptyLayer {
		t.Fatalf("unexpected image config history: %#v", result.Config.History)
	}
	if !result.Config.History[1].EmptyLayer || result.Config.History[1].CreatedBy != "LABEL org.opencontainers.image.version=fixture-amd64" {
		t.Fatalf("unexpected metadata history entry: %#v", result.Config.History[1])
	}
	if len(result.Layers) != 1 || result.Layers[0].MediaType != string(types.OCIUncompressedLayer) || result.Size <= result.Config.Size {
		t.Fatalf("unexpected layer/size metadata: layers=%#v size=%d configSize=%d", result.Layers, result.Size, result.Config.Size)
	}
	if !result.BuildYAML.Found || result.BuildYAML.Skipped || result.BuildYAML.Path != "usr/src/rancher/build.yaml" {
		t.Fatalf("unexpected build.yaml result: %#v warnings=%v", result.BuildYAML, result.Warnings)
	}
	if result.BuildYAML.Data["webhookVersion"] != "v0.12.1-rcs-0844.1" || !strings.Contains(result.BuildYAML.Raw, "release: security") {
		t.Fatalf("unexpected build.yaml content: %#v", result.BuildYAML)
	}
}

func TestImageLookupBoundedHistoryKeepsLatestEntriesAndBoundsText(t *testing.T) {
	created := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	history := make([]v1.History, imageLookupMaxHistoryEntries+2)
	for index := range history {
		history[index] = v1.History{
			Created:    v1.Time{Time: created.Add(time.Duration(index) * time.Minute)},
			CreatedBy:  "RUN ordinary-build-step",
			Comment:    "ordinary comment",
			EmptyLayer: index == len(history)-1,
		}
	}
	history[2].CreatedBy = strings.Repeat("c", imageLookupMaxHistoryText+50)
	history[2].Comment = strings.Repeat("m", imageLookupMaxHistoryText+75)

	bounded := imageLookupBoundedHistory(history)
	if len(bounded) != imageLookupMaxHistoryEntries {
		t.Fatalf("bounded history length = %d, want %d", len(bounded), imageLookupMaxHistoryEntries)
	}
	if bounded[0].Created != imageLookupFormatTime(history[2].Created.Time) {
		t.Fatalf("first retained history timestamp = %q, want latest bounded window beginning at %q", bounded[0].Created, imageLookupFormatTime(history[2].Created.Time))
	}
	if len(bounded[0].CreatedBy) != imageLookupMaxHistoryText || len(bounded[0].Comment) != imageLookupMaxHistoryText {
		t.Fatalf("history text bounds = createdBy %d comment %d, want %d each", len(bounded[0].CreatedBy), len(bounded[0].Comment), imageLookupMaxHistoryText)
	}
	if !bounded[len(bounded)-1].EmptyLayer {
		t.Fatal("bounded history did not preserve empty-layer metadata")
	}
	if empty := imageLookupBoundedHistory(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty history = %#v, want non-nil empty slice", empty)
	}
}

func TestImageLookupBuildYAMLScanSkipsLargeLayersAndContinues(t *testing.T) {
	buildLayer := newImageLookupFixtureLayer(t, map[string]string{
		"usr/src/rancher/build.yaml": "release: bounded-layer-scan\n",
	})
	largeLayer := newImageLookupFixtureLayer(t, map[string]string{
		"large.bin": strings.Repeat("x", 32<<10),
	})
	buildSize, err := buildLayer.Size()
	if err != nil {
		t.Fatalf("build fixture layer size: %v", err)
	}
	largeSize, err := largeLayer.Size()
	if err != nil {
		t.Fatalf("large fixture layer size: %v", err)
	}
	if largeSize <= buildSize {
		t.Fatalf("large fixture layer size %d must exceed build layer size %d", largeSize, buildSize)
	}
	image, err := mutate.AppendLayers(empty.Image, buildLayer, largeLayer)
	if err != nil {
		t.Fatalf("append build.yaml fixture layers: %v", err)
	}
	service := &imageLookupService{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: buildSize,
		maxLayerScan:  8 << 20,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if !result.Found || result.Skipped || result.Error != "" || result.Path != "usr/src/rancher/build.yaml" {
		t.Fatalf("build.yaml scan result = %#v, warnings=%v", result, warnings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Skipped 1 layer larger than") {
		t.Fatalf("large-layer warning = %v, want one aggregate skip warning", warnings)
	}
}

func TestImageLookupBuildYAMLScanReturnsNonErrorReasonWhenLargeLayersWereSkipped(t *testing.T) {
	smallLayer := newImageLookupFixtureLayer(t, map[string]string{
		"README.txt": "eligible smaller layer without build metadata",
	})
	largeBuildLayer := newImageLookupFixtureLayer(t, map[string]string{
		"usr/src/rancher/build.yaml": "release: " + strings.Repeat("x", 32<<10) + "\n",
	})
	smallSize, err := smallLayer.Size()
	if err != nil {
		t.Fatalf("small fixture layer size: %v", err)
	}
	largeSize, err := largeBuildLayer.Size()
	if err != nil {
		t.Fatalf("large build fixture layer size: %v", err)
	}
	if largeSize <= smallSize {
		t.Fatalf("large build fixture layer size %d must exceed small layer size %d", largeSize, smallSize)
	}
	image, err := mutate.AppendLayers(empty.Image, smallLayer, largeBuildLayer, largeBuildLayer)
	if err != nil {
		t.Fatalf("append skipped build.yaml fixture layers: %v", err)
	}
	service := &imageLookupService{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: smallSize,
		maxLayerScan:  8 << 20,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if result.Found || !result.Skipped || result.Error != "" {
		t.Fatalf("large-layer-only build.yaml result = %#v, warnings=%v", result, warnings)
	}
	if !strings.Contains(result.Reason, "Skipped 2 layers larger than") || !strings.Contains(result.Reason, "safe scan limit") {
		t.Fatalf("non-error skip reason = %q", result.Reason)
	}
	if len(warnings) != 1 || warnings[0] != result.Reason {
		t.Fatalf("skip warnings = %v, want reason %q", warnings, result.Reason)
	}
	if got, want := imageLookupBuildYAMLSkipReason(6, 0, imageLookupMaxBuildYAMLLayer), "Skipped 6 layers larger than the 16 MiB safe scan limit."; got != want {
		t.Fatalf("default large-layer reason = %q, want %q", got, want)
	}
}

func TestImageLookupBuildYAMLScanLimitReturnsNonErrorReason(t *testing.T) {
	layer := newImageLookupFixtureLayer(t, map[string]string{
		"large-readme.txt": strings.Repeat("x", 8<<10),
	})
	compressedSize, err := layer.Size()
	if err != nil {
		t.Fatalf("scan-limit fixture layer size: %v", err)
	}
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("append scan-limit fixture layer: %v", err)
	}
	service := &imageLookupService{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: compressedSize,
		maxLayerScan:  512,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if result.Found || !result.Skipped || result.Error != "" {
		t.Fatalf("cumulative scan-limit result = %#v, warnings=%v", result, warnings)
	}
	if !strings.Contains(result.Reason, "512 bytes cumulative uncompressed safe scan limit") || !strings.Contains(result.Reason, "remaining image layer data was not scanned") {
		t.Fatalf("cumulative scan-limit reason = %q", result.Reason)
	}
	if len(warnings) != 1 || warnings[0] != result.Reason {
		t.Fatalf("scan-limit warnings = %v, want reason %q", warnings, result.Reason)
	}
	if got, want := imageLookupBuildYAMLScanLimitReason(imageLookupMaxLayerScan), "Stopped after reaching the 256 MiB cumulative uncompressed safe scan limit; remaining image layer data was not scanned."; got != want {
		t.Fatalf("default cumulative scan-limit reason = %q, want %q", got, want)
	}
}

func TestImageLookupFetchSourceBuildYAMLUsesPinnedDeclaredGitHubSource(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	revision := "9c6326c89e3f89c092ff3a80c02bbde96195bccb"
	image := newImageLookupFixtureImage(t, "amd64", "", time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), map[string]string{
		"README.txt": "source fallback fixture",
	})
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read source fixture config: %v", err)
	}
	config.Config.Labels[imageLookupSourceLabel] = "https://github.com/rancher/rancher-prime.git"
	config.Config.Labels[imageLookupRevisionLabel] = revision
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write source fixture config: %v", err)
	}
	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse source fixture reference: %v", err)
	}
	if err := remote.Write(reference, image,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push source fixture image: %v", err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatalf("source fixture digest: %v", err)
	}

	t.Setenv("GH_DEBUG", "api")
	t.Setenv("GH_PROMPT_DISABLED", "not-sanitized")
	t.Setenv("GIT_TERMINAL_PROMPT", "not-sanitized")
	t.Setenv("GH_PAGER", "not-sanitized")
	var commandCalls int
	var commandName string
	var commandArguments, commandEnvironment []string
	var commandLimit int64
	service.runCommand = func(ctx context.Context, executable string, arguments, environment []string, outputLimit int64) ([]byte, error) {
		commandCalls++
		commandName = executable
		commandArguments = append([]string(nil), arguments...)
		commandEnvironment = append([]string(nil), environment...)
		commandLimit = outputLimit
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > imageLookupGHTimeout+time.Second {
			t.Fatalf("command context deadline = %v, want a live deadline within %s", deadline, imageLookupGHTimeout)
		}
		return []byte("fleetVersion: 110.0.0+up0.16.0-rc.5\nwebhookVersion: 110.0.0+up0.11.0\n"), nil
	}

	response, err := service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest.String(),
	})
	if err != nil {
		t.Fatalf("FetchSourceBuildYAML: %v", err)
	}
	if commandCalls != 1 || commandName != "gh" || commandLimit != imageLookupMaxSourceBuildYAML {
		t.Fatalf("command invocation = calls %d name %q limit %d", commandCalls, commandName, commandLimit)
	}
	wantArguments := []string{
		"api",
		"--hostname", "github.com",
		"--method", http.MethodGet,
		"-H", "Accept:application/vnd.github.raw+json",
		"/repos/rancher/rancher-prime/contents/build.yaml?ref=" + revision,
	}
	if !slices.Equal(commandArguments, wantArguments) {
		t.Fatalf("gh arguments = %#v, want %#v", commandArguments, wantArguments)
	}
	environment := map[string]string{}
	for _, entry := range commandEnvironment {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			environment[parts[0]] = parts[1]
		}
	}
	if environment["GH_PROMPT_DISABLED"] != "1" || environment["GIT_TERMINAL_PROMPT"] != "0" || environment["GH_PAGER"] != "cat" {
		t.Fatalf("sanitized gh environment = %#v", environment)
	}
	if _, ok := environment["GH_DEBUG"]; ok {
		t.Fatalf("sanitized gh environment retained GH_DEBUG: %#v", environment)
	}
	if !response.Found || response.Path != "build.yaml" || response.Origin != "declared-source" {
		t.Fatalf("source build.yaml identity = %#v", response)
	}
	if response.Data["fleetVersion"] != "110.0.0+up0.16.0-rc.5" || response.Data["webhookVersion"] != "110.0.0+up0.11.0" {
		t.Fatalf("source build.yaml data = %#v", response.Data)
	}
	if response.Provenance.RepositoryURL != "https://github.com/rancher/rancher-prime" || response.Provenance.Revision != revision || response.Provenance.Path != "build.yaml" || response.Provenance.ImageReference != referenceText || response.Provenance.ImageDigest != digest.String() || response.Provenance.Platform != "linux/amd64" {
		t.Fatalf("source build.yaml provenance = %#v", response.Provenance)
	}
	if response.Provenance.SourceLabel != imageLookupSourceLabel || response.Provenance.RevisionLabel != imageLookupRevisionLabel {
		t.Fatalf("source build.yaml provenance labels = %#v", response.Provenance)
	}

	_, err = service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: "sha256:" + strings.Repeat("a", 64),
	})
	var conflictErr *imageLookupConflictError
	if !errors.As(err, &conflictErr) || imageLookupHTTPStatus(err) != http.StatusConflict {
		t.Fatalf("moved digest error = %T %v, want conflict", err, err)
	}
	if commandCalls != 1 {
		t.Fatalf("moved digest invoked gh; command calls = %d", commandCalls)
	}

	service.runCommand = func(context.Context, string, []string, []string, int64) ([]byte, error) {
		return nil, errors.New("secret-token-from-gh-stderr")
	}
	_, err = service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest.String(),
	})
	if err == nil || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "confirm GitHub CLI authentication") {
		t.Fatalf("sanitized gh failure = %v", err)
	}
}

func TestImageLookupFetchSourceBuildYAMLFallsBackToPinnedOSSRevisionOnlyForRancherPrime(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	privateRevision := "aa818cc35ef525f237c376c136703263113082fa"
	ossRevision := "f7821ed280af6d120b93917cbded7b6946d77d35"
	referenceText, digest := pushImageLookupSourceFixture(t, registryServer, "v2.11.16-alpha6", map[string]string{
		imageLookupSourceLabel:      "https://github.com/rancher/rancher-prime.git",
		imageLookupRevisionLabel:    privateRevision,
		imageLookupOSSRevisionLabel: ossRevision,
	})

	var commandArguments [][]string
	service.runCommand = func(_ context.Context, executable string, arguments, _ []string, outputLimit int64) ([]byte, error) {
		if executable != "gh" || outputLimit != imageLookupMaxSourceBuildYAML {
			t.Fatalf("command = %q limit = %d", executable, outputLimit)
		}
		commandArguments = append(commandArguments, append([]string(nil), arguments...))
		if len(commandArguments) == 1 {
			return nil, errors.New("private repository returned 404 with sensitive details")
		}
		return []byte("fleetVersion: 110.0.0+up0.16.0-rc.5\nwebhookVersion: 110.0.0+up0.11.0\n"), nil
	}

	response, err := service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest,
	})
	if err != nil {
		t.Fatalf("FetchSourceBuildYAML with OSS fallback: %v", err)
	}
	if len(commandArguments) != 2 {
		t.Fatalf("gh command calls = %d, want private source then OSS source", len(commandArguments))
	}
	wantPrivateEndpoint := "/repos/rancher/rancher-prime/contents/build.yaml?ref=" + privateRevision
	wantOSSEndpoint := "/repos/rancher/rancher/contents/build.yaml?ref=" + ossRevision
	if commandArguments[0][len(commandArguments[0])-1] != wantPrivateEndpoint || commandArguments[1][len(commandArguments[1])-1] != wantOSSEndpoint {
		t.Fatalf("gh endpoints = %q then %q, want %q then %q", commandArguments[0][len(commandArguments[0])-1], commandArguments[1][len(commandArguments[1])-1], wantPrivateEndpoint, wantOSSEndpoint)
	}
	if !response.Found || response.Origin != "declared-oss-source" || response.Data["webhookVersion"] != "110.0.0+up0.11.0" {
		t.Fatalf("OSS fallback response = %#v", response)
	}
	if response.Provenance.RepositoryURL != "https://github.com/rancher/rancher" || response.Provenance.Revision != ossRevision || response.Provenance.RevisionLabel != imageLookupOSSRevisionLabel || response.Provenance.SourceLabel != imageLookupSourceLabel {
		t.Fatalf("OSS fallback provenance = %#v", response.Provenance)
	}

	arbitraryReference, arbitraryDigest := pushImageLookupSourceFixture(t, registryServer, "v2.11.16-alpha7", map[string]string{
		imageLookupSourceLabel:      "https://github.com/rancher/embargoed-security",
		imageLookupRevisionLabel:    privateRevision,
		imageLookupOSSRevisionLabel: ossRevision,
	})
	var arbitraryCalls int
	service.runCommand = func(context.Context, string, []string, []string, int64) ([]byte, error) {
		arbitraryCalls++
		return nil, errors.New("secret arbitrary source failure")
	}
	_, err = service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      arbitraryReference,
		Platform:       "linux/amd64",
		ExpectedDigest: arbitraryDigest,
	})
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "confirm GitHub CLI authentication") {
		t.Fatalf("arbitrary source failure = %v", err)
	}
	if arbitraryCalls != 1 {
		t.Fatalf("arbitrary source gh calls = %d, want no OSS fallback", arbitraryCalls)
	}
}

func TestImageLookupParseGitHubSourceRequiresExactPinnedGitHubLabels(t *testing.T) {
	revision := "9c6326c89e3f89c092ff3a80c02bbde96195bccb"
	for _, source := range []string{
		"https://github.com/rancher/embargoed-security",
		"https://github.com/rancher/rancher-prime.git",
	} {
		owner, repository, err := imageLookupParseGitHubSource(source, revision)
		wantRepository := "embargoed-security"
		if strings.HasSuffix(source, "/rancher-prime.git") {
			wantRepository = "rancher-prime"
		}
		if err != nil || owner != "rancher" || repository != wantRepository {
			t.Errorf("valid GitHub source %q = owner %q repository %q error %v", source, owner, repository, err)
		}
	}
	for _, testCase := range []struct {
		source   string
		revision string
	}{
		{"", revision},
		{"https://github.com/rancher/embargoed-security", ""},
		{"https://github.com/rancher/embargoed-security", "9c6326c"},
		{" https://github.com/rancher/embargoed-security", revision},
		{"https://github.com/rancher/embargoed-security", revision + " "},
		{"http://github.com/rancher/embargoed-security", revision},
		{"https://user@github.com/rancher/embargoed-security", revision},
		{"https://github.com/rancher/embargoed-security/", revision},
		{"https://github.com/rancher/embargoed-security?ref=main", revision},
		{"https://github.com/rancher/embargoed-security#build", revision},
		{"https://github.com/rancher/embargoed-security/extra", revision},
		{"https://github.com/rancher/rancher-prime.git/", revision},
		{"https://github.com/rancher/rancher-prime.git?ref=main", revision},
		{"https://github.com/rancher/rancher-prime.git#build", revision},
		{"https://github.com/rancher/.git", revision},
		{"https://github.com/rancher/rancher-prime.git.git", revision},
		{"https://github.com/rancher/rancher-prime.GIT", revision},
		{"https://gitlab.com/rancher/embargoed-security", revision},
	} {
		_, _, err := imageLookupParseGitHubSource(testCase.source, testCase.revision)
		var metadataErr *imageLookupSourceMetadataError
		if !errors.As(err, &metadataErr) || imageLookupHTTPStatus(err) != http.StatusUnprocessableEntity {
			t.Errorf("source %q revision %q error = %T %v, want source metadata error", testCase.source, testCase.revision, err, err)
		}
	}
}

func TestImageLookupSafeTransportRejectsPrivateAddresses(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0",
		"10.0.0.1",
		"100.64.0.1",
		"127.0.0.1",
		"169.254.169.254",
		"172.16.0.1",
		"192.168.1.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"2001:db8::1",
		"::ffff:127.0.0.1",
	} {
		if imageLookupPublicIP(netip.MustParseAddr(address)) {
			t.Errorf("private or reserved address %s was allowed", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !imageLookupPublicIP(netip.MustParseAddr(address)) {
			t.Errorf("public address %s was blocked", address)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "https://127.0.0.1/v2/", nil)
	_, err := newImageLookupSafeTransport().RoundTrip(request)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "private or reserved") {
		t.Fatalf("safe transport error = %v, want private/reserved rejection", err)
	}
}

func TestImageLookupHandlersEnforceAuthMethodAndStrictJSON(t *testing.T) {
	panel := &localControlPanel{
		token: "test-token",
		imageLookup: &imageLookupService{
			transport:  http.DefaultTransport,
			keychain:   imageLookupAnonymousTestKeychain{},
			allowHTTP:  true,
			maxTagScan: 10,
		},
	}
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		method      string
		body        string
		authorized  bool
		wantStatus  int
		wantBody    string
		wantAllowed string
	}{
		{
			name:       "search rejects unauthenticated request",
			handler:    panel.handleImageLookupSearch,
			method:     http.MethodPost,
			body:       `{}`,
			wantStatus: http.StatusForbidden,
			wantBody:   "invalid control panel token",
		},
		{
			name:        "search only accepts POST",
			handler:     panel.handleImageLookupSearch,
			method:      http.MethodGet,
			authorized:  true,
			wantStatus:  http.StatusMethodNotAllowed,
			wantBody:    "method not allowed",
			wantAllowed: http.MethodPost,
		},
		{
			name:       "search rejects unknown field",
			handler:    panel.handleImageLookupSearch,
			method:     http.MethodPost,
			body:       `{"registry":"docker.io","repository":"rancher/rancher","surprise":true}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "unknown field",
		},
		{
			name:       "search rejects trailing JSON",
			handler:    panel.handleImageLookupSearch,
			method:     http.MethodPost,
			body:       `{} {}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "exactly one JSON object",
		},
		{
			name:       "inspect rejects unknown field",
			handler:    panel.handleImageLookupInspect,
			method:     http.MethodPost,
			body:       `{"reference":"rancher/rancher:v2.16.0","unknown":"value"}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "unknown field",
		},
		{
			name:       "inspect maps validation error to bad request",
			handler:    panel.handleImageLookupInspect,
			method:     http.MethodPost,
			body:       `{"reference":"rancher/rancher"}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "tag or digest",
		},
		{
			name:       "source build yaml rejects unauthenticated request",
			handler:    panel.handleImageLookupSourceBuildYAML,
			method:     http.MethodPost,
			body:       `{}`,
			wantStatus: http.StatusForbidden,
			wantBody:   "invalid control panel token",
		},
		{
			name:        "source build yaml only accepts POST",
			handler:     panel.handleImageLookupSourceBuildYAML,
			method:      http.MethodGet,
			authorized:  true,
			wantStatus:  http.StatusMethodNotAllowed,
			wantBody:    "method not allowed",
			wantAllowed: http.MethodPost,
		},
		{
			name:       "source build yaml rejects unknown field",
			handler:    panel.handleImageLookupSourceBuildYAML,
			method:     http.MethodPost,
			body:       `{"reference":"rancher/rancher:v2.16.0","platform":"linux/amd64","expectedDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","unknown":true}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "unknown field",
		},
		{
			name:       "source build yaml requires inspected digest",
			handler:    panel.handleImageLookupSourceBuildYAML,
			method:     http.MethodPost,
			body:       `{"reference":"rancher/rancher:v2.16.0","platform":"linux/amd64"}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "expectedDigest",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "/api/images/test", strings.NewReader(testCase.body))
			request.RemoteAddr = "198.51.100.5:12345"
			if testCase.authorized {
				request.Header.Set("X-Control-Panel-Token", "test-token")
			}
			recorder := httptest.NewRecorder()
			testCase.handler(recorder, request)

			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), testCase.wantBody) {
				t.Fatalf("body = %q, want containing %q", recorder.Body.String(), testCase.wantBody)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
			}
			if testCase.wantAllowed != "" && recorder.Header().Get("Allow") != testCase.wantAllowed {
				t.Fatalf("Allow = %q, want %q", recorder.Header().Get("Allow"), testCase.wantAllowed)
			}
		})
	}
}

func newImageLookupTestService(t *testing.T, server *httptest.Server) *imageLookupService {
	t.Helper()
	return &imageLookupService{
		transport:     server.Client().Transport,
		keychain:      imageLookupAnonymousTestKeychain{},
		allowHTTP:     true,
		now:           func() time.Time { return time.Date(2026, time.August, 5, 18, 0, 0, 0, time.UTC) },
		maxTagScan:    100,
		maxBuildYML:   1 << 20,
		maxBuildLayer: 4 << 20,
		maxLayerScan:  8 << 20,
	}
}

type imageLookupAnonymousTestKeychain struct{}

func (imageLookupAnonymousTestKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

func imageLookupTestServerHost(t *testing.T, server *httptest.Server) string {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return parsed.Host
}

func newImageLookupFixtureImage(t *testing.T, architecture, variant string, created time.Time, files map[string]string) v1.Image {
	t.Helper()
	layer := newImageLookupFixtureLayer(t, files)
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("append fixture image layer: %v", err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}
	config.OS = "linux"
	config.Architecture = architecture
	config.Variant = variant
	config.Created = v1.Time{Time: created}
	config.Config.Labels = map[string]string{"org.opencontainers.image.version": "fixture-" + architecture}
	config.Config.Env = []string{"FIXTURE=yes"}
	config.Config.Entrypoint = []string{"/usr/bin/rancher"}
	config.Config.Cmd = []string{"server"}
	config.History = []v1.History{
		{
			Created:   v1.Time{Time: created},
			CreatedBy: "RUN fixture-build --arch=" + architecture,
			Comment:   "fixture layer",
		},
		{
			Created:    v1.Time{Time: created.Add(time.Second)},
			CreatedBy:  "LABEL org.opencontainers.image.version=fixture-" + architecture,
			Comment:    "fixture metadata",
			EmptyLayer: true,
		},
	}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	image = mutate.MediaType(image, types.OCIManifestSchema1)
	return mutate.ConfigMediaType(image, types.OCIConfigJSON)
}

func pushImageLookupSourceFixture(t *testing.T, server *httptest.Server, tag string, labels map[string]string) (string, string) {
	t.Helper()
	image := newImageLookupFixtureImage(t, "amd64", "", time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), map[string]string{
		"README.txt": "source fallback fixture",
	})
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read source fixture config: %v", err)
	}
	for key, value := range labels {
		config.Config.Labels[key] = value
	}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write source fixture config: %v", err)
	}
	referenceText := imageLookupTestServerHost(t, server) + "/rancher/rancher:" + tag
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse source fixture reference: %v", err)
	}
	if err := remote.Write(reference, image,
		remote.WithTransport(server.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push source fixture image: %v", err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatalf("source fixture digest: %v", err)
	}
	return referenceText, digest.String()
}

func newImageLookupFixtureLayer(t *testing.T, files map[string]string) v1.Layer {
	t.Helper()
	var layerArchive bytes.Buffer
	archive := tar.NewWriter(&layerArchive)
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	for _, filePath := range paths {
		content := []byte(files[filePath])
		if err := archive.WriteHeader(&tar.Header{
			Name:     filePath,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write fixture layer header: %v", err)
		}
		if _, err := archive.Write(content); err != nil {
			t.Fatalf("write fixture layer content: %v", err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close fixture layer: %v", err)
	}
	return static.NewLayer(layerArchive.Bytes(), types.OCIUncompressedLayer)
}
