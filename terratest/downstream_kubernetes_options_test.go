package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveDownstreamKubernetesVersionUsesVerifiedDefaultSetting(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/settings/k3s-default-version", func(w http.ResponseWriter, r *http.Request) {
		assertDownstreamRancherAuth(t, r)
		writeJSON(w, map[string]any{"value": "1.35.8+k3s1"})
	})
	mux.HandleFunc("/v1-k3s-release/releases", func(w http.ResponseWriter, r *http.Request) {
		assertDownstreamRancherAuth(t, r)
		writeJSON(w, map[string]any{"data": []map[string]any{
			{"id": "v1.35.8+k3s1"},
			{"id": "v1.36.3+k3s1"},
		}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	got, err := resolveDownstreamKubernetesVersionWithClient(server.Client(), server.URL, "rancher-token", "k3s", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.35.8+k3s1" {
		t.Fatalf("resolved version = %q, want verified default", got)
	}
}

func TestNormalizeDownstreamKubernetesVersion(t *testing.T) {
	tests := map[string]string{
		"1.35.8+k3s1":      "v1.35.8+k3s1",
		" v1.36.3+rke2r1 ": "v1.36.3+rke2r1",
		"V1.36.3+rke2r1":   "v1.36.3+rke2r1",
		"":                 "",
	}
	for input, want := range tests {
		if got := normalizeDownstreamKubernetesVersion(input); got != want {
			t.Fatalf("normalizeDownstreamKubernetesVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveDownstreamKubernetesVersionUsesLatestLiveReleaseOnlyWhenDefaultEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/settings/rke2-default-version", func(w http.ResponseWriter, r *http.Request) {
		assertDownstreamRancherAuth(t, r)
		writeJSON(w, map[string]any{"value": "", "default": ""})
	})
	mux.HandleFunc("/v1-rke2-release/releases", func(w http.ResponseWriter, r *http.Request) {
		assertDownstreamRancherAuth(t, r)
		writeJSON(w, map[string]any{"data": []map[string]any{
			{"id": "v1.35.8+rke2r1"},
			{"version": "1.36.3+rke2r1"},
			{"id": "v1.37.0+k3s1"},
			{"id": "not-a-version"},
		}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	got, err := resolveDownstreamKubernetesVersionWithClient(server.Client(), server.URL, "rancher-token", "rke2", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.36.3+rke2r1" {
		t.Fatalf("resolved version = %q, want latest live RKE2 release", got)
	}
}

func TestLatestDownstreamKubernetesVersionSortsProviderRevisionsNumerically(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{
			name:     "k3s",
			versions: []string{"v1.36.3+k3s9", "v1.36.3+k3s10", "v1.36.3+k3s2"},
			want:     "v1.36.3+k3s10",
		},
		{
			name:     "rke2",
			versions: []string{"v1.36.3+rke2r9", "v1.36.3+rke2r10", "v1.36.3+rke2r2"},
			want:     "v1.36.3+rke2r10",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available := map[string]struct{}{}
			for _, version := range tt.versions {
				available[version] = struct{}{}
			}
			got, err := latestDownstreamKubernetesVersion(available)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("latest version = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveDownstreamKubernetesVersionRejectsStaleNonEmptyDefault(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/settings/k3s-default-version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"value": "1.34.9+k3s1"})
	})
	mux.HandleFunc("/v1-k3s-release/releases", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"data": []map[string]any{{"id": "v1.36.3+k3s1"}}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := resolveDownstreamKubernetesVersionWithClient(server.Client(), server.URL, "rancher-token", "k3s", "")
	if err == nil || !strings.Contains(err.Error(), "is not present") {
		t.Fatalf("expected stale default rejection, got %v", err)
	}
}

func TestResolveDownstreamKubernetesVersionVerifiesExplicitVersionWithoutReadingDefault(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/settings/rke2-default-version", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("explicit version must not read the mutable default setting")
	})
	mux.HandleFunc("/v1-rke2-release/releases", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"data": []map[string]any{{"id": "v1.36.3+rke2r1"}}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	got, err := resolveDownstreamKubernetesVersionWithClient(server.Client(), server.URL, "rancher-token", "rke2", "1.36.3+rke2r1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.36.3+rke2r1" {
		t.Fatalf("resolved version = %q", got)
	}
}

func assertDownstreamRancherAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer rancher-token" {
		t.Fatalf("Authorization = %q", got)
	}
}
