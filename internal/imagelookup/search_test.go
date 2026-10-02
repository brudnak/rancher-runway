package imagelookup

import (
	"context"
	"encoding/json"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestImageLookupSearchDigestUsesManifestFastPath(t *testing.T) {
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
	image := newImageLookupFixtureImage(t, "amd64", "", time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC), map[string]string{
		"README.txt": "digest fixture",
	})
	host := imageLookupTestServerHost(t, registryServer)
	reference, err := name.NewTag(host+"/rancher/rancher:digest-fixture", name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse digest fixture tag: %v", err)
	}
	if err := remote.Write(reference, image, remote.WithTransport(registryServer.Client().Transport), remote.WithAuth(authn.Anonymous)); err != nil {
		t.Fatalf("push digest fixture: %v", err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatalf("digest fixture digest: %v", err)
	}
	digestText := digest.String()

	assertDigestResult := func(t *testing.T, request SearchRequest) {
		t.Helper()
		manifestRequests.Store(0)
		tagListRequests.Store(0)
		result, searchErr := service.Search(context.Background(), request)
		if searchErr != nil {
			t.Fatalf("digest search: %v", searchErr)
		}
		if manifestRequests.Load() == 0 || tagListRequests.Load() != 0 {
			t.Fatalf("digest search requests: manifests=%d tag-lists=%d", manifestRequests.Load(), tagListRequests.Load())
		}
		if len(result.Groups) != 1 || result.Groups[0].Scanned != 1 || result.Groups[0].Matched != 1 || len(result.Groups[0].Tags) != 1 {
			t.Fatalf("digest search result = %#v", result.Groups)
		}
		got := result.Groups[0].Tags[0]
		wantReference := host + "/rancher/rancher@" + digestText
		if got.Name != digestText || got.Reference != wantReference || got.Digest != digestText || got.Channel != "digest" || got.Architecture != "unknown" || got.ImageRole != "server" || got.CompanionReference != "" {
			t.Fatalf("digest metadata = %#v, want reference %q", got, wantReference)
		}
	}

	assertDigestResult(t, SearchRequest{
		Registry:   host,
		Repository: "rancher/rancher",
		Query:      digestText,
	})
	assertDigestResult(t, SearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      host + "/rancher/rancher@" + digestText,
	})

	manifestRequests.Store(0)
	tagListRequests.Store(0)
	missingDigest := "sha256:" + strings.Repeat("0", 64)
	missing, err := service.Search(context.Background(), SearchRequest{
		Registry:   host,
		Repository: "rancher/rancher",
		Query:      missingDigest,
	})
	if err != nil {
		t.Fatalf("missing digest search: %v", err)
	}
	if manifestRequests.Load() == 0 || tagListRequests.Load() != 0 || len(missing.Groups) != 1 || missing.Groups[0].Scanned != 1 || missing.Groups[0].Matched != 0 || len(missing.Groups[0].Tags) != 0 {
		t.Fatalf("missing digest fast path = %#v, manifests=%d tag-lists=%d", missing.Groups, manifestRequests.Load(), tagListRequests.Load())
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
	result, err := service.Search(context.Background(), SearchRequest{
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

func TestImageLookupBarePatchFilterScansThenSortsBeforeLimit(t *testing.T) {
	var manifestRequests atomic.Int32
	var tagRequests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/manifests/") {
			manifestRequests.Add(1)
		}
		switch request.URL.Path {
		case "/v2/":
			response.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			response.WriteHeader(http.StatusOK)
		case "/v2/rancher/rancher/tags/list":
			tagRequests.Add(1)
			response.Header().Set("Content-Type", "application/json")
			if request.URL.Query().Get("last") == "" {
				response.Header().Set("Link", `</v2/rancher/rancher/tags/list?n=1000&last=v2.15.1-beta1>; rel="next"`)
				_ = json.NewEncoder(response).Encode(map[string]any{
					"name": "rancher/rancher",
					"tags": []string{"v2.15.1-rc1", "v2.15.1-beta1"},
				})
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"name": "rancher/rancher",
				"tags": []string{"v2.15.1", "v2.15.1-alpha2"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	result, err := service.Search(context.Background(), SearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Query:      "v2.15.1",
		Limit:      2,
		SortBy:     "tag",
		SortOrder:  "asc",
	})
	if err != nil {
		t.Fatalf("bare patch search: %v", err)
	}
	if manifestRequests.Load() != 0 {
		t.Fatalf("bare patch filter made %d exact manifest requests", manifestRequests.Load())
	}
	if tagRequests.Load() != 2 {
		t.Fatalf("bare patch filter tag pages = %d, want 2", tagRequests.Load())
	}
	group := result.Groups[0]
	if group.Scanned != 4 || group.Matched != 4 || !group.Truncated || len(group.Tags) != 2 {
		t.Fatalf("bare patch accounting = %#v", group)
	}
	got := []string{group.Tags[0].Name, group.Tags[1].Name}
	want := []string{"v2.15.1", "v2.15.1-alpha2"}
	if !slices.Equal(got, want) {
		t.Fatalf("globally sorted limited tags = %v, want %v", got, want)
	}
	if group.Tags[0].Version != "2.15.1" || group.Tags[0].VersionLine != "2.15" {
		t.Fatalf("normalized stable metadata = %#v", group.Tags[0])
	}
}
