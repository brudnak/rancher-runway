package imagelookup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	result, err := service.Search(context.Background(), SearchRequest{
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

	result, err := service.Search(context.Background(), SearchRequest{
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
	result, err := service.Search(context.Background(), SearchRequest{
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
	group := service.searchTarget(context.Background(), searchTarget{
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
	result, err := service.Search(context.Background(), SearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Limit:      2,
		SortBy:     "version",
		SortOrder:  "desc",
		ScanMode:   "bounded",
	})
	if err != nil {
		t.Fatalf("Search with result limit: %v", err)
	}
	if tagListRequests.Load() != 1 {
		t.Fatalf("tag-list requests = %d, want 1", tagListRequests.Load())
	}
	if result.ScanMode != "bounded" || result.SortBy != "version" {
		t.Fatalf("bounded search response = %#v", result)
	}
	group := result.Groups[0]
	if group.Scanned != 2 || group.Matched != 2 || !group.Truncated || len(group.Tags) != 2 {
		t.Fatalf("unexpected limited search group: %#v", group)
	}
	if group.Tags[0].Name != "v2" || group.Tags[1].Name != "v1" {
		t.Fatalf("limited search order = %#v, want v2 then v1", group.Tags)
	}
}

func TestImageLookupDockerHubUploadedSortPaginatesBeyondOneHundred(t *testing.T) {
	const tagCount = 150
	baseTime := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	tags := make([]Tag, tagCount)
	metadata := make([]imageLookupDockerHubTag, tagCount)
	for index := 0; index < tagCount; index++ {
		name := fmt.Sprintf("build-%03d", index)
		tags[index] = Tag{Name: name}
		metadata[index] = imageLookupDockerHubTag{
			Name:          name,
			FullSize:      int64(1000 + index),
			TagLastPushed: baseTime.Add(time.Duration(index) * time.Hour),
		}
	}

	requestedPages := []string{}
	service := &Service{
		maxTagScan: MaxTagScan,
		transport: imageLookupTestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Scheme != "https" || request.URL.Host != "hub.docker.com" || request.URL.Path != "/v2/namespaces/rancher/repositories/rancher/tags" {
				return nil, fmt.Errorf("unexpected Docker Hub metadata URL %s", request.URL.String())
			}
			page := request.URL.Query().Get("page")
			requestedPages = append(requestedPages, page)
			var results []imageLookupDockerHubTag
			next := ""
			switch page {
			case "1":
				results = metadata[:100]
				// The implementation must treat Next only as a continuation signal,
				// never as a URL to request.
				next = "https://metadata.invalid/untrusted-next-page"
			case "2":
				results = metadata[100:]
			default:
				return nil, fmt.Errorf("unexpected Docker Hub page %q", page)
			}
			payload, err := json.Marshal(map[string]any{"next": next, "results": results})
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(payload)),
				Request:    request,
			}, nil
		}),
	}
	complete, err := service.enrichDockerHubTags(context.Background(), "rancher/rancher", "", tags, true)
	if err != nil || !complete {
		t.Fatalf("paginate complete Docker Hub metadata: complete=%t err=%v", complete, err)
	}
	if !slices.Equal(requestedPages, []string{"1", "2"}) {
		t.Fatalf("Docker Hub metadata pages = %v, want [1 2]", requestedPages)
	}
	for index := range tags {
		if tags[index].UploadedAt != imageLookupFormatTime(metadata[index].TagLastPushed) || tags[index].Size != metadata[index].FullSize {
			t.Fatalf("metadata for candidate %d = %#v", index, tags[index])
		}
	}

	ascending := append([]Tag(nil), tags...)
	imageLookupSortTags(ascending, "uploaded", "asc")
	if ascending[0].Name != "build-000" || ascending[len(ascending)-1].Name != "build-149" {
		t.Fatalf("ascending upload order endpoints = %q ... %q", ascending[0].Name, ascending[len(ascending)-1].Name)
	}
	descending := append([]Tag(nil), tags...)
	imageLookupSortTags(descending, "uploaded", "desc")
	if descending[0].Name != "build-149" || descending[len(descending)-1].Name != "build-000" {
		t.Fatalf("descending upload order endpoints = %q ... %q", descending[0].Name, descending[len(descending)-1].Name)
	}
}
