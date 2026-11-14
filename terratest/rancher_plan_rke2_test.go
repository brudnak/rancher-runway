package test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveLatestRKE2PatchUsesArchivedNotesAndPublicAssets(t *testing.T) {
	const downloads = "https://github.com/rancher/rke2/releases/download/"
	const image = "/rke2-images.linux-amd64.tar.zst"
	const redirectedImage = "https://release-assets.githubusercontent.com/example-image"
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	t.Setenv(releaseLookupCachePathEnv, cachePath)
	var requests []string
	previousClient := rancherLookupHTTPClient
	rancherLookupHTTPClient = &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		status := http.StatusOK
		header := make(http.Header)
		var body string
		if req.URL.Host != "docs.rke2.io" && req.Method != http.MethodHead {
			t.Fatalf("expected HEAD for %s, got %s", req.URL, req.Method)
		}
		switch req.URL.String() {
		case "https://docs.rke2.io/release-notes/v1.32.X":
			status = http.StatusNotFound
		case "https://docs.rke2.io/release-notes-old/v1.32.X":
			body = `<table><tr><td>v1.32.13+rke2r2*</td></tr><tr><td>v1.32.13+rke2r1</td></tr><tr><td>v1.32.12+rke2r1</td></tr></table><p>* PRIME Only Release</p>`
		case downloads + "v1.32.13+rke2r2" + image:
			status = http.StatusNotFound
		case downloads + "v1.32.13+rke2r1" + image:
			status = http.StatusFound
			header.Set("Location", redirectedImage)
		case redirectedImage, downloads + "v1.32.13+rke2r1/sha256sum-amd64.txt":
			status = http.StatusOK
		default:
			t.Fatalf("unexpected RKE2 lookup: %s", req.URL)
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	t.Cleanup(func() { rancherLookupHTTPClient = previousClient })

	got, err := resolveLatestRKE2Patch(32)
	if err != nil {
		t.Fatalf("resolve archived RKE2 release: %v", err)
	}
	if got != "v1.32.13+rke2r1" {
		t.Fatalf("expected latest release with public assets, got %q", got)
	}
	wantRequests := []string{
		"https://docs.rke2.io/release-notes/v1.32.X",
		"https://docs.rke2.io/release-notes-old/v1.32.X",
		downloads + "v1.32.13+rke2r2" + image,
		downloads + "v1.32.13+rke2r1" + image,
		redirectedImage,
		downloads + "v1.32.13+rke2r1/sha256sum-amd64.txt",
	}
	if !slices.Equal(requests, wantRequests) {
		t.Fatalf("unexpected release lookup order: %v", requests)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read release cache: %v", err)
	}
	var cache releaseLookupCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("decode release cache: %v", err)
	}
	entry := cache.Products["rke2"]["v1.32"]
	if entry.ReleaseNotesURL != wantRequests[1] || entry.LatestVersion != got {
		t.Fatalf("expected archive source and public release in cache, got %#v", entry)
	}
}

func TestResolveLatestRKE2PatchRejectsDocsWithoutPublicAssets(t *testing.T) {
	t.Setenv(releaseLookupCachePathEnv, filepath.Join(t.TempDir(), "release-cache.json"))
	previousClient := rancherLookupHTTPClient
	rancherLookupHTTPClient = &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		status := http.StatusOK
		switch req.URL.String() {
		case "https://docs.rke2.io/release-notes/v1.32.X":
			body = `v1.32.13+rke2r2`
		case "https://github.com/rancher/rke2/releases/download/v1.32.13+rke2r2/rke2-images.linux-amd64.tar.zst":
			status = http.StatusOK
		case "https://github.com/rancher/rke2/releases/download/v1.32.13+rke2r2/sha256sum-amd64.txt":
			status = http.StatusNotFound
		default:
			t.Fatalf("unexpected RKE2 lookup: %s", req.URL)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	t.Cleanup(func() { rancherLookupHTTPClient = previousClient })

	got, err := resolveLatestRKE2Patch(32)
	if got != "" || err == nil || !strings.Contains(err.Error(), "no RKE2 releases have required public assets") {
		t.Fatalf("expected unavailable public release error, got version %q, error %v", got, err)
	}
}

func TestResolveLatestRKE2PatchReportsAssetLookupFailure(t *testing.T) {
	const imageURL = "https://github.com/rancher/rke2/releases/download/v1.36.6+rke2r1/rke2-images.linux-amd64.tar.zst"
	for _, code := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			cachePath := filepath.Join(t.TempDir(), "release-cache.json")
			t.Setenv(releaseLookupCachePathEnv, cachePath)
			previousClient := rancherLookupHTTPClient
			t.Cleanup(func() { rancherLookupHTTPClient = previousClient })
			rancherLookupHTTPClient = &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.String() {
				case "https://docs.rke2.io/release-notes/v1.36.X":
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("v1.36.6+rke2r1 v1.36.5+rke2r1"))}, nil
				case imageURL:
					return &http.Response{StatusCode: code, Body: http.NoBody}, nil
				default:
					t.Fatalf("unexpected RKE2 lookup after access failure: %s", req.URL)
					return nil, nil
				}
			})}
			got, err := resolveLatestRKE2Patch(36)
			var status httpStatusError
			if got != "" || !errors.As(err, &status) || status.StatusCode != code || status.URL != imageURL {
				t.Fatalf("expected HTTP %d for %s, got version %q and error %v", code, imageURL, got, err)
			}
			if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected no cache after asset lookup failure, stat returned %v", err)
			}
		})
	}
}
