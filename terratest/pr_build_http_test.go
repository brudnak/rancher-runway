package test

import (
	"context"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/http"
	"net/http/httptest"

	"strings"

	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestPRBuildVerifyHandlerAuthMethodStrictJSONAndSuccess(t *testing.T) {
	rawPull := testPRBuildPull(42)
	service := prbuild.NewWithOptions(prbuild.Options{
		FetchPull: func(context.Context, prbuild.Target) (prbuild.GitHubPull, error) { return rawPull, nil },
		Inspect: func(context.Context, imagelookup.InspectRequest) (imagelookup.InspectResponse, error) {
			return imagelookup.InspectResponse{}, &transport.Error{StatusCode: http.StatusNotFound}
		},
		Now: func() time.Time { return time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC) },
	})
	panel := &localControlPanel{token: "test-token", prBuildVerifier: service}
	tests := []struct {
		name        string
		method      string
		body        string
		authorized  bool
		wantStatus  int
		wantBody    string
		wantAllowed string
	}{
		{name: "auth", method: http.MethodPost, body: `{}`, wantStatus: http.StatusForbidden, wantBody: "invalid control panel token"},
		{name: "method", method: http.MethodGet, authorized: true, wantStatus: http.StatusMethodNotAllowed, wantBody: "method not allowed", wantAllowed: http.MethodPost},
		{name: "unknown field", method: http.MethodPost, authorized: true, body: `{"pullRequest":"https://github.com/rancher/rancher/pull/42","tag":"2.14-head","unexpected":true}`, wantStatus: http.StatusBadRequest, wantBody: "unknown field"},
		{name: "invalid URL", method: http.MethodPost, authorized: true, body: `{"pullRequest":"https://example.com/rancher/rancher/pull/42","tag":"2.14-head"}`, wantStatus: http.StatusBadRequest, wantBody: "github.com"},
		{name: "success", method: http.MethodPost, authorized: true, body: `{"pullRequest":"https://github.com/rancher/rancher/pull/42","tag":"2.14-head"}`, wantStatus: http.StatusOK, wantBody: `"tag": "v2.14-head"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "/api/pr-builds/verify", strings.NewReader(testCase.body))
			request.RemoteAddr = "198.51.100.5:12345"
			if testCase.authorized {
				request.Header.Set("X-Control-Panel-Token", "test-token")
			}
			recorder := httptest.NewRecorder()
			panel.handlePRBuildVerify(recorder, request)
			if recorder.Code != testCase.wantStatus || !strings.Contains(recorder.Body.String(), testCase.wantBody) {
				t.Fatalf("status=%d body=%q, want status=%d containing %q", recorder.Code, recorder.Body.String(), testCase.wantStatus, testCase.wantBody)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
			}
			if testCase.wantAllowed != "" && recorder.Header().Get("Allow") != testCase.wantAllowed {
				t.Fatalf("Allow = %q, want %q", recorder.Header().Get("Allow"), testCase.wantAllowed)
			}
		})
	}
}
