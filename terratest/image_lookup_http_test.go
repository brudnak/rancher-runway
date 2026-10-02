package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImageLookupHandlersEnforceAuthMethodAndStrictJSON(t *testing.T) {
	panel := &localControlPanel{
		token: "test-token",
		imageLookup: imagelookup.NewWithOptions(imagelookup.Options{
			Transport:  http.DefaultTransport,
			Keychain:   imageLookupAnonymousTestKeychain{},
			AllowHTTP:  true,
			MaxTagScan: 10,
		}),
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
