package test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveLatestRKE2PatchUsesArchivedNotesAndPublicAssets(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	t.Setenv(releaseLookupCachePathEnv, cachePath)
	var requests []string
	previousClient := rancherLookupHTTPClient
	rancherLookupHTTPClient = &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		status := http.StatusOK
		var body string
		switch req.URL.String() {
		case "https://docs.rke2.io/release-notes/v1.32.X":
			status = http.StatusNotFound
		case "https://docs.rke2.io/release-notes-old/v1.32.X":
			body = `<table><tr><td>v1.32.13+rke2r2*</td></tr><tr><td>v1.32.13+rke2r1</td></tr><tr><td>v1.32.12+rke2r1</td></tr></table><p>* PRIME Only Release</p>`
		case "https://api.github.com/repos/rancher/rke2/releases/tags/v1.32.13+rke2r2":
			body = `{"assets":[{"name":"sha256sum-amd64.txt"}]}`
		case "https://api.github.com/repos/rancher/rke2/releases/tags/v1.32.13+rke2r1":
			body = `{"assets":[{"name":"rke2-images.linux-amd64.tar.zst"},{"name":"sha256sum-amd64.txt"}]}`
		default:
			t.Fatalf("unexpected RKE2 lookup: %s", req.URL)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
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
		"https://api.github.com/repos/rancher/rke2/releases/tags/v1.32.13+rke2r2",
		"https://api.github.com/repos/rancher/rke2/releases/tags/v1.32.13+rke2r1",
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
		switch req.URL.String() {
		case "https://docs.rke2.io/release-notes/v1.32.X":
			body = `v1.32.13+rke2r2`
		case "https://api.github.com/repos/rancher/rke2/releases/tags/v1.32.13+rke2r2":
			body = `{"assets":[{"name":"sha256sum-amd64.txt"}]}`
		default:
			t.Fatalf("unexpected RKE2 lookup: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	t.Cleanup(func() { rancherLookupHTTPClient = previousClient })

	got, err := resolveLatestRKE2Patch(32)
	if got != "" || err == nil || !strings.Contains(err.Error(), "no RKE2 releases have required public assets") {
		t.Fatalf("expected unavailable public release error, got version %q, error %v", got, err)
	}
}
