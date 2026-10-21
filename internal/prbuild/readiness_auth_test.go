package prbuild

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type readinessCredentialProbe struct{ calls atomic.Int32 }

func (p *readinessCredentialProbe) Resolve(authn.Resource) (authn.Authenticator, error) {
	p.calls.Add(1)
	return nil, errors.New("fixture local credential access was attempted")
}

type readinessRegistryTransport func(*http.Request) (*http.Response, error)

func (f readinessRegistryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestReadinessImageReadsDoNotConsultLocalCredentials(t *testing.T) {
	// Only an in-process public registry and an injected sentinel keychain are
	// used here. No Docker config, credential helper, or real registry is read.
	t.Setenv("DOCKERHUB_USERNAME", "fixture-user")
	t.Setenv("DOCKERHUB_PASSWORD", "fixture-password")
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	config, err := empty.Image.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	config.OS, config.Architecture = "linux", "amd64"
	config.Config.Labels = map[string]string{imagelookup.SourceLabel: "https://github.com/rancher/rancher", imagelookup.RevisionLabel: strings.Repeat("a", 40)}
	image, err := mutate.ConfigFile(empty.Image, config)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewTag(endpoint.Host+"/rancher/rancher:head", name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, image, remote.WithAuth(authn.Anonymous), remote.WithTransport(server.Client().Transport)); err != nil {
		t.Fatal(err)
	}
	probe := &readinessCredentialProbe{}
	lookup := imagelookup.NewWithOptions(imagelookup.Options{
		Keychain: probe,
		Transport: readinessRegistryTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "stgregistry.suse.com" {
				t.Errorf("unexpected registry request host %s", req.URL.Host)
				return nil, errors.New("unexpected request")
			}
			if req.Header.Get("Authorization") != "" {
				t.Error("Docker Hub credentials leaked to a different registry")
			}
			copy := req.Clone(req.Context())
			copy.URL.Scheme, copy.URL.Host, copy.Host = endpoint.Scheme, endpoint.Host, endpoint.Host
			return server.Client().Transport.RoundTrip(copy)
		}),
		RunCommand: func(context.Context, string, []string, []string, int64) ([]byte, error) {
			t.Error("readiness image inspection invoked a command")
			return nil, errors.New("unexpected command")
		},
	})
	service := New(lookup)
	service.defaults()
	result := service.inspectImage(context.Background(), "stgregistry.suse.com/rancher/rancher:head")
	if !result.Found || result.Error != "" || result.Revision != strings.Repeat("a", 40) {
		t.Fatalf("public image inspection failed: found=%v, error=%s", result.Found, result.Error)
	}
	group := service.imageLookup.SearchMovingHeads(context.Background(), "stgregistry.suse.com", "rancher/rancher", 100)
	if group.Error != "" || len(group.Tags) != 1 || group.Tags[0].Name != "head" {
		t.Fatalf("public head discovery failed: %s, tags=%d", group.Error, len(group.Tags))
	}
	if probe.calls.Load() != 0 {
		t.Fatal("readiness consulted local registry credentials")
	}
	// A separately requested Image Lookup still owns its explicit auth policy.
	_, err = lookup.Inspect(context.Background(), imagelookup.InspectRequest{Reference: "stgregistry.suse.com/rancher/rancher:head", Platform: "linux/amd64", SkipTagMetadata: true})
	if err == nil || !strings.Contains(err.Error(), "fixture local credential access") || probe.calls.Load() != 1 {
		t.Fatal("readiness changed the shared Image Lookup credential policy")
	}
}
