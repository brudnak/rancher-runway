package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestValidateLinodeRootPasswordAcceptsGeneratedShape(t *testing.T) {
	if err := validateLinodeRootPassword("Abcdefghijk23456!#$%&()*+,-.:;"); err != nil {
		t.Fatalf("expected generated-shape password to pass: %v", err)
	}
}

func TestValidateLinodeRootPasswordRejectsWeakValues(t *testing.T) {
	tests := []string{
		"short1",
		"alllowercase",
		"ValidButWayTooLong123!" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		"Valid1🙂",
	}
	for _, password := range tests {
		if err := validateLinodeRootPassword(password); err == nil {
			t.Fatalf("expected password %q to fail validation", password)
		}
	}
}

func TestNormalizeDockerRancherTagAddsLeadingVExceptPlainHead(t *testing.T) {
	tests := map[string]string{
		"2.14.2-alpha3": "v2.14.2-alpha3",
		"2.14-head":     "v2.14-head",
		"2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head": "v2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head",
		"v2.14.2-rc1": "v2.14.2-rc1",
		"head":        "head",
	}
	for input, want := range tests {
		if got := normalizeDockerRancherTag(input); got != want {
			t.Fatalf("normalizeDockerRancherTag(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveLinodeDockerImageSourceAutoFindsFirstRegistryWithAllTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/stg/v2/rancher/rancher/manifests/"):
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/docker/v2/rancher/rancher/manifests/head":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	previousBases := rancherRegistryBaseURLs
	rancherRegistryBaseURLs = map[string]string{
		"docker.io":            server.URL + "/docker",
		"stgregistry.suse.com": server.URL + "/stg",
		"registry.rancher.com": server.URL + "/prime",
		"registry.suse.com":    server.URL + "/suse",
	}
	t.Cleanup(func() {
		rancherRegistryBaseURLs = previousBases
		viper.Reset()
	})

	viper.Set("linode.dockerhub", "auto")
	image, label, findings, err := resolveLinodeDockerImageSource([]string{"2.14.2-alpha3"})
	if err != nil {
		t.Fatalf("resolveLinodeDockerImageSource returned error: %v", err)
	}
	if image != "stgregistry.suse.com/rancher/rancher" {
		t.Fatalf("expected staging image source, got %q (%s, %#v)", image, label, findings)
	}
}

func TestResolveLinodeDockerImageSourceHonorsExplicitCustomSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docker/v2/devuser/rancher/manifests/v2.14-head" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	previousBases := rancherRegistryBaseURLs
	rancherRegistryBaseURLs = map[string]string{"docker.io": server.URL + "/docker"}
	t.Cleanup(func() {
		rancherRegistryBaseURLs = previousBases
		viper.Reset()
	})

	viper.Set("linode.dockerhub", "devuser/rancher")
	image, _, _, err := resolveLinodeDockerImageSource([]string{"2.14-head"})
	if err != nil {
		t.Fatalf("resolveLinodeDockerImageSource returned error: %v", err)
	}
	if image != "devuser/rancher" {
		t.Fatalf("expected custom image source, got %q", image)
	}
}

func TestSearchLinodeDockerImageSourcesReportsEveryKnownSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stg/v2/rancher/rancher/manifests/v2.14.3-alpha2",
			"/prime/v2/rancher/rancher/manifests/v2.14.3-alpha2":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	previousBases := rancherRegistryBaseURLs
	rancherRegistryBaseURLs = map[string]string{
		"docker.io":            server.URL + "/docker",
		"stgregistry.suse.com": server.URL + "/stg",
		"registry.rancher.com": server.URL + "/prime",
		"registry.suse.com":    server.URL + "/suse",
	}
	t.Cleanup(func() {
		rancherRegistryBaseURLs = previousBases
	})

	tag, results, err := searchLinodeDockerImageSources("2.14.3-alpha2", "")
	if err != nil {
		t.Fatalf("searchLinodeDockerImageSources returned error: %v", err)
	}
	if tag != "v2.14.3-alpha2" {
		t.Fatalf("expected Docker tag to be normalized, got %q", tag)
	}
	if len(results) != len(linodeDockerImageSources) {
		t.Fatalf("expected one result per source, got %#v", results)
	}

	found := map[string]bool{}
	for _, result := range results {
		if result.Found {
			found[result.Key] = true
		}
		if result.Tag != tag || !strings.HasSuffix(result.Image, ":"+tag) {
			t.Fatalf("unexpected result image/tag: %#v", result)
		}
	}
	if !found["staging"] || !found["prime"] || found["dockerhub"] || found["suse"] {
		t.Fatalf("unexpected search hits: %#v", found)
	}
}

func TestSearchLinodeDockerImageSourcesIncludesCustomSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docker/v2/devuser/rancher/manifests/dev-build" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	previousBases := rancherRegistryBaseURLs
	rancherRegistryBaseURLs = map[string]string{
		"docker.io":            server.URL + "/docker",
		"stgregistry.suse.com": server.URL + "/stg",
		"registry.rancher.com": server.URL + "/prime",
		"registry.suse.com":    server.URL + "/suse",
	}
	t.Cleanup(func() {
		rancherRegistryBaseURLs = previousBases
	})

	tag, results, err := searchLinodeDockerImageSources("2.14.3-alpha2", "devuser/rancher:dev-build")
	if err != nil {
		t.Fatalf("searchLinodeDockerImageSources returned error: %v", err)
	}
	if tag != "v2.14.3-alpha2" {
		t.Fatalf("expected normalized search tag, got %q", tag)
	}

	var custom *linodeDockerImageSearchResult
	for i := range results {
		if results[i].Key == "custom" {
			custom = &results[i]
			break
		}
	}
	if custom == nil {
		t.Fatalf("expected custom search result, got %#v", results)
	}
	if !custom.Found || custom.Repository != "devuser/rancher" || custom.Tag != "dev-build" || custom.Image != "devuser/rancher:dev-build" {
		t.Fatalf("unexpected custom result: %#v", custom)
	}
}

func TestSearchLinodeDockerImageSourcesUsesCustomTagWhenSearchTagEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docker/v2/devuser/rancher/manifests/dev-build" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	previousBases := rancherRegistryBaseURLs
	rancherRegistryBaseURLs = map[string]string{
		"docker.io":            server.URL + "/docker",
		"stgregistry.suse.com": server.URL + "/stg",
		"registry.rancher.com": server.URL + "/prime",
		"registry.suse.com":    server.URL + "/suse",
	}
	t.Cleanup(func() {
		rancherRegistryBaseURLs = previousBases
	})

	tag, results, err := searchLinodeDockerImageSources("", "devuser/rancher:dev-build")
	if err != nil {
		t.Fatalf("searchLinodeDockerImageSources returned error: %v", err)
	}
	if tag != "dev-build" {
		t.Fatalf("expected tag from custom image, got %q", tag)
	}

	for _, result := range results {
		if result.Key == "custom" && !result.Found {
			t.Fatalf("expected custom image to be found: %#v", result)
		}
	}
}
