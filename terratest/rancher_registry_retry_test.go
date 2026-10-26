package test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
)

func TestValidateRancherWebhookImageRetriesRegistryTimeouts(t *testing.T) {
	for _, stage := range []string{"manifest", "token", "authenticated manifest"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				attempts := map[string]int{}
				previousClient := rancherRegistryHTTPClient
				t.Cleanup(func() { rancherRegistryHTTPClient = previousClient })
				rancherRegistryHTTPClient = &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					current := "manifest"
					if req.URL.Path == "/auth" {
						current = "token"
						if req.Method != http.MethodGet || req.URL.Query().Get("scope") != "repository:rancher/rancher-webhook:pull" {
							t.Fatalf("unexpected token request: %s %s", req.Method, req.URL)
						}
					} else {
						if req.Method != http.MethodHead || req.URL.String() != "https://stgregistry.suse.com/v2/rancher/rancher-webhook/manifests/v0.7.13-rc.1" {
							t.Fatalf("unexpected manifest request: %s %s", req.Method, req.URL)
						}
						if !strings.Contains(req.Header.Get("Accept"), "application/vnd.oci.image.manifest.v1+json") {
							t.Fatal("manifest request lost its Accept header")
						}
						if req.Header.Get("Authorization") == "Bearer test-token" {
							current = "authenticated manifest"
						}
					}
					attempts[current]++
					if current == stage && attempts[current] == 1 {
						return nil, context.DeadlineExceeded
					}
					resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}
					switch current {
					case "manifest":
						resp.StatusCode = http.StatusUnauthorized
						resp.Header.Set("WWW-Authenticate", `Bearer realm="https://stgregistry.suse.com/auth",service="registry",scope="repository:rancher/rancher-webhook:pull"`)
					case "token":
						resp.Body = io.NopCloser(strings.NewReader(`{"token":"test-token"}`))
					}
					return resp, nil
				})}

				if err := validateRancherWebhookImage("stgregistry.suse.com/rancher/rancher-webhook:v0.7.13-rc.1"); err != nil {
					t.Fatalf("temporary %s timeout prevented preflight recovery: %v", stage, err)
				}
				if attempts[stage] != 2 {
					t.Fatalf("%s attempts = %d, want 2", stage, attempts[stage])
				}
			})
		})
	}
}

func TestValidateRancherWebhookImageKeepsTimeoutAsError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		previousClient := rancherRegistryHTTPClient
		t.Cleanup(func() { rancherRegistryHTTPClient = previousClient })
		attempts := 0
		rancherRegistryHTTPClient = &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, context.DeadlineExceeded
		})}

		err := validateRancherWebhookImage("stgregistry.suse.com/rancher/rancher-webhook:v0.7.13-rc.1")
		if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "was not found") {
			t.Fatalf("expected registry timeout, got %v", err)
		}
		if attempts != 3 {
			t.Fatalf("manifest attempts = %d, want 3", attempts)
		}
	})
}
