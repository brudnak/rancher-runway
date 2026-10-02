package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"
)

func TestServiceHandlersRejectBeforeAcquiringBackend(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		auth := func(*http.Request) bool { return authorized }
		images := ImageHandlers{Authorize: auth, Backend: func() *imagelookup.Service { t.Fatal("rejected request acquired image service"); return nil }}
		prs := PRHandlers{Authorize: auth, Backend: func() *prbuild.Service { t.Fatal("rejected request acquired PR service"); return nil }}
		cache := CacheHandlers{Authorize: auth, Backend: func() (*cachelab.Service, error) { t.Fatal("rejected request acquired cache service"); return nil, nil }}
		for name, handler := range map[string]http.HandlerFunc{"search": images.Search, "inspect": images.Inspect, "source": images.SourceBuildYAML, "verify": prs.Verify, "readiness": prs.Readiness, "cache": cache.Handle} {
			t.Run(name+map[bool]string{true: "-method", false: "-auth"}[authorized], func(t *testing.T) {
				response := httptest.NewRecorder()
				handler(response, httptest.NewRequest(http.MethodDelete, "/api/test", nil))
				want := http.StatusForbidden
				if authorized {
					want = http.StatusMethodNotAllowed
				}
				if response.Code != want || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d headers=%v", response.Code, response.Header())
				}
			})
		}
	}
}

func TestImageJSONContractPrecedesBackendLookup(t *testing.T) {
	images := ImageHandlers{Authorize: func(*http.Request) bool { return true }, Backend: func() *imagelookup.Service { t.Fatal("invalid request acquired image service"); return nil }}
	for _, body := range []string{`{"unknown":true}`, `{} {}`, `{"query":"` + strings.Repeat("x", int(imagelookup.RequestLimit)) + `"}`} {
		response := httptest.NewRecorder()
		images.Search(response, httptest.NewRequest(http.MethodPost, "/api/images/search", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
}
