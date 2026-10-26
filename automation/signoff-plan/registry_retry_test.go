package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
)

type registryRetryTransport func(*http.Request) (*http.Response, error)

func (f registryRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestValidateWebhookImageRecoversFromRegistryTimeouts(t *testing.T) {
	for _, failingStage := range []string{"manifest", "token", "authenticated manifest"} {
		t.Run(failingStage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := map[string]int{}
				client := githubClient{httpClient: &http.Client{Transport: registryRetryTransport(func(req *http.Request) (*http.Response, error) {
					stage := "manifest"
					if req.URL.Host == "auth.example.test" {
						stage = "token"
						if req.Method != http.MethodGet || req.URL.Path != "/token" || req.URL.Query().Get("service") != "registry" || req.URL.Query().Get("scope") != "repository:rancher/rancher-webhook:pull" {
							t.Fatalf("unexpected token request: %s %s", req.Method, req.URL)
						}
						if req.Header.Get("Authorization") != "" {
							t.Fatal("unexpected authorization on token request")
						}
					} else {
						if req.Method != http.MethodHead || req.URL.String() != "https://stgregistry.suse.com/v2/rancher/rancher-webhook/manifests/v0.7.13-rc.1" {
							t.Fatalf("unexpected manifest request: %s %s", req.Method, req.URL)
						}
						if !strings.Contains(req.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") {
							t.Fatal("manifest request lost its Accept header")
						}
						if authorization := req.Header.Get("Authorization"); authorization != "" {
							if authorization != "Bearer test-token" {
								t.Fatalf("unexpected manifest authorization %q", authorization)
							}
							stage = "authenticated manifest"
						}
					}
					calls[stage]++
					if stage == failingStage && calls[stage] == 1 {
						return nil, context.DeadlineExceeded
					}
					resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}
					switch stage {
					case "manifest":
						resp.StatusCode = http.StatusUnauthorized
						resp.Header.Set("WWW-Authenticate", `Bearer realm="https://auth.example.test/token",service="registry",scope="repository:rancher/rancher-webhook:pull"`)
					case "token":
						resp.Body = io.NopCloser(strings.NewReader(`{"token":"test-token"}`))
					}
					return resp, nil
				})}}
				if err := client.validateWebhookImage(context.Background(), "stgregistry.suse.com/rancher/rancher-webhook:v0.7.13-rc.1", "v0.7.13-rc.1"); err != nil {
					t.Fatalf("expected transient registry timeout to recover, got %v", err)
				}
				for _, stage := range []string{"manifest", "token", "authenticated manifest"} {
					want := 1
					if stage == failingStage {
						want = 2
					}
					if calls[stage] != want {
						t.Errorf("%s requests = %d, want %d", stage, calls[stage], want)
					}
				}
			})
		})
	}
}

func TestValidateWebhookImagePreservesExhaustedTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := githubClient{httpClient: &http.Client{Transport: registryRetryTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			return nil, context.DeadlineExceeded
		})}}
		err := client.validateWebhookImage(context.Background(), "stgregistry.suse.com/rancher/rancher-webhook:v0.7.13-rc.1", "v0.7.13-rc.1")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected original timeout after retries, got %v", err)
		}
		if strings.Contains(err.Error(), "was not found") {
			t.Fatalf("registry outage was reported as a missing image: %v", err)
		}
		if calls != 3 {
			t.Fatalf("manifest requests = %d, want 3", calls)
		}
	})
}
