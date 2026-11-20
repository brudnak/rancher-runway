package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollectLinodeCatalogUsesAuthenticatedProviderData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/linode/instances", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer catalog-token" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	mux.HandleFunc("/v4/regions", func(w http.ResponseWriter, r *http.Request) {
		assertLinodeCatalogRequest(t, r)
		writeJSON(w, map[string]any{
			"page": 1, "pages": 1,
			"data": []map[string]any{
				{"id": "us-ord", "label": "Chicago, IL", "country": "us", "status": "ok"},
				{"id": "old", "label": "Unavailable", "country": "us", "status": "closed"},
			},
		})
	})
	mux.HandleFunc("/v4/linode/types", func(w http.ResponseWriter, r *http.Request) {
		assertLinodeCatalogRequest(t, r)
		writeJSON(w, map[string]any{
			"page": 1, "pages": 1,
			"data": []map[string]any{{
				"id": "g6-standard-2", "label": "Linode 4GB", "memory": 4096, "vcpus": 2, "disk": 81920,
				"class": "standard", "price": map[string]any{"hourly": 0.036, "monthly": 24.0},
			}},
		})
	})
	mux.HandleFunc("/v4/images", func(w http.ResponseWriter, r *http.Request) {
		assertLinodeCatalogRequest(t, r)
		writeJSON(w, map[string]any{
			"page": 1, "pages": 1,
			"data": []map[string]any{
				{"id": "linode/ubuntu22.04", "label": "Ubuntu 22.04 LTS", "status": "available", "is_public": true},
				{"id": "private/qa", "label": "QA image", "status": "available", "is_public": false},
				{"id": "linode/old", "label": "Old", "status": "creating", "is_public": true},
			},
		})
	})
	client := linodeTestClient(mux)
	if _, err := collectLinodeCatalog(t.Context(), client, linodeCatalogDefaultAPIBaseURL, "expired-token"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("catalog should return the credential error: %v", err)
	}
	catalog, err := collectLinodeCatalog(t.Context(), client, linodeCatalogDefaultAPIBaseURL, "catalog-token")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Source != "Linode API v4" || catalog.CollectedAt.IsZero() {
		t.Fatalf("unexpected catalog metadata: %#v", catalog)
	}
	if len(catalog.Regions) != 1 || catalog.Regions[0].ID != "us-ord" {
		t.Fatalf("unexpected regions: %#v", catalog.Regions)
	}
	if len(catalog.Types) != 1 || catalog.Types[0].MemoryMB != 4096 || catalog.Types[0].DiskMB != 81920 {
		t.Fatalf("unexpected types: %#v", catalog.Types)
	}
	if catalog.Types[0].Label != "Linode 4GB, 2 vCPUs, 80 GB Disk" {
		t.Fatalf("type label = %q", catalog.Types[0].Label)
	}
	if len(catalog.Images) != 2 || catalog.Images[1].ID != "linode/ubuntu22.04" {
		t.Fatalf("unexpected images: %#v", catalog.Images)
	}
	if catalog.Defaults.Distribution != "k3s" || catalog.Defaults.Region != "us-ord" {
		t.Fatalf("unexpected defaults: %#v", catalog.Defaults)
	}
}

func linodeTestClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: testLabRoundTrip(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})}
}

func TestCollectLinodeCatalogRequiresToken(t *testing.T) {
	if _, err := collectLinodeCatalog(context.Background(), http.DefaultClient, "https://example.invalid", ""); err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestLinodeCatalogErrorsRedactToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `provider rejected catalog-token`, http.StatusUnauthorized)
	}))
	defer server.Close()
	var regions []linodeAPIRegion
	err := fetchAllLinodePages(context.Background(), server.Client(), server.URL, "catalog-token", &regions)
	if err == nil || strings.Contains(err.Error(), "catalog-token") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("unexpected redacted error: %v", err)
	}
}

func assertLinodeCatalogRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer catalog-token" {
		t.Errorf("Authorization = %q", got)
	}
	if r.URL.Query().Get("page_size") != "500" {
		t.Errorf("page_size = %q", r.URL.Query().Get("page_size"))
	}
}
