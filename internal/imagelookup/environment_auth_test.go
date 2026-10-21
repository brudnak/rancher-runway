package imagelookup

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

func TestEnvironmentOnlyCredentialsStayScopedToDockerHub(t *testing.T) {
	for _, test := range []struct {
		name, registry, username, password string
		wantCredentials                    bool
	}{
		{"Docker Hub", "docker.io", "fixture-user", "fixture-password", true},
		{"Docker Hub canonical host", "index.docker.io", " fixture-user ", "fixture-password", true},
		{"SUSE staging", "stgregistry.suse.com", "fixture-user", "fixture-password", false},
		{"Rancher registry", "registry.rancher.com", "fixture-user", "fixture-password", false},
		{"SUSE registry", "registry.suse.com", "fixture-user", "fixture-password", false},
		{"other registry", "private.example.test", "fixture-user", "fixture-password", false},
		{"no credentials", "docker.io", "", "", false},
		{"missing password", "docker.io", "fixture-user", "", false},
		{"missing username", "docker.io", "", "fixture-password", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DOCKERHUB_USERNAME", test.username)
			t.Setenv("DOCKERHUB_PASSWORD", test.password)
			resource, err := name.NewRepository(test.registry + "/rancher/rancher")
			if err != nil {
				t.Fatal(err)
			}
			credential, err := (imageLookupEnvironmentKeychain{}).Resolve(resource)
			if err != nil {
				t.Fatal(err)
			}
			if !test.wantCredentials {
				if credential != authn.Anonymous {
					t.Fatal("unexpected credentials for an anonymous registry request")
				}
				return
			}
			config, err := credential.Authorization()
			if err != nil || config.Username != "fixture-user" || config.Password != "fixture-password" {
				t.Fatal("explicit Docker Hub credentials were not retained")
			}
		})
	}
}

func TestEnvironmentOnlyServiceRetainsDependenciesWithoutChangingOriginal(t *testing.T) {
	transport := &imageLookupCloseTrackingRoundTripper{}
	original := NewWithOptions(Options{Transport: transport, Keychain: imageLookupAnonymousTestKeychain{}, AllowHTTP: true, MaxTagScan: 37, MaxBuildYAML: 100, MaxBuildLayer: 200, MaxLayerScan: 300})
	public := original.WithEnvironmentCredentialsOnly()
	if public == original || public.transport != transport || !public.allowHTTP || public.maxTagScan != 37 || public.maxBuildYML != 100 || public.maxBuildLayer != 200 || public.maxLayerScan != 300 {
		t.Fatal("environment-only service changed dependencies or reused mutable configuration")
	}
	if _, ok := original.keychain.(imageLookupAnonymousTestKeychain); !ok {
		t.Fatal("shared Image Lookup authentication was changed")
	}
	if _, ok := public.keychain.(imageLookupEnvironmentKeychain); !ok {
		t.Fatal("environment-only service retained a local credential resolver")
	}
}
