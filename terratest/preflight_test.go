package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestHelmRepoAliasFromCommand(t *testing.T) {
	command := `helm upgrade rancher optimus-rancher-alpha/rancher \
  --namespace cattle-system \
  --set hostname=rancher.example.com`

	if got := helmRepoAliasFromCommand(command); got != "optimus-rancher-alpha" {
		t.Fatalf("helmRepoAliasFromCommand() = %q, want optimus-rancher-alpha", got)
	}
}

func TestHelmRepoAliasesFromCommandsDeduplicatesAndSorts(t *testing.T) {
	got := helmRepoAliasesFromCommands([]string{
		"helm install rancher rancher-latest/rancher --namespace cattle-system",
		"helm upgrade rancher optimus-rancher-alpha/rancher --namespace cattle-system",
		"helm upgrade rancher rancher-latest/rancher --namespace cattle-system",
	})

	want := []string{"optimus-rancher-alpha", "rancher-latest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helmRepoAliasesFromCommands() = %#v, want %#v", got, want)
	}
}

func TestFindMissingHelmReposAfterKnownRepos(t *testing.T) {
	commands := []string{
		"helm install rancher rancher-latest/rancher --namespace cattle-system",
		"helm install other custom-repo/thing --namespace cattle-system",
	}
	output := `NAME             URL
rancher-latest   https://releases.rancher.com/server-charts/latest
`

	missing := findMissingHelmRepos(output, commands)
	if len(missing) != 1 || missing[0] != "custom-repo" {
		t.Fatalf("findMissingHelmRepos() = %#v, want custom-repo", missing)
	}
}

func TestKnownRancherHelmRepoURLs(t *testing.T) {
	required := []string{
		"rancher-latest",
		"rancher-stable",
		"rancher-alpha",
		"rancher-prime",
		"optimus-rancher-latest",
		"optimus-rancher-alpha",
	}

	for _, repoAlias := range required {
		if rancherHelmRepoURLs[repoAlias] == "" {
			t.Fatalf("expected %s to have a known URL", repoAlias)
		}
	}
}

func TestValidateRancherHelmVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "supported Helm 3", version: "v3.21.3"},
		{name: "Helm 3 with build metadata", version: "v3.21.3+g1234567"},
		{name: "unsupported Helm 4", version: "v4.1.3", wantErr: true},
		{name: "malformed", version: "development", wantErr: true},
		{name: "empty", version: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRancherHelmVersion(test.version)
			if test.wantErr && err == nil {
				t.Fatalf("validateRancherHelmVersion(%q) succeeded, want error", test.version)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateRancherHelmVersion(%q) failed: %v", test.version, err)
			}
		})
	}
}

func TestHelmFlagConsumesSetLiteralValue(t *testing.T) {
	if !helmFlagConsumesValue("--set-literal") {
		t.Fatal("expected --set-literal to consume the following argument")
	}
	if helmFlagConsumesValue("--set-literal=webhook={}") {
		t.Fatal("expected an inline --set-literal value not to consume another argument")
	}
}

func TestPrepareDockerHubCredentialsForProvisioningKeepsAcceptedCredentials(t *testing.T) {
	t.Setenv("DOCKERHUB_USERNAME", "valid-user")
	t.Setenv("DOCKERHUB_PASSWORD", "valid-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "valid-user" || password != "valid-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if err := prepareDockerHubCredentialsForProvisioningWithClient(server.Client(), server.URL); err != nil {
		t.Fatalf("credential preparation failed: %v", err)
	}
	if os.Getenv("DOCKERHUB_USERNAME") != "valid-user" || os.Getenv("DOCKERHUB_PASSWORD") != "valid-token" {
		t.Fatal("accepted Docker Hub credentials were unexpectedly cleared")
	}
}

func TestPrepareDockerHubCredentialsForProvisioningDropsRejectedCredentials(t *testing.T) {
	t.Setenv("DOCKERHUB_USERNAME", "expired-user")
	t.Setenv("DOCKERHUB_PASSWORD", "expired-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	if err := prepareDockerHubCredentialsForProvisioningWithClient(server.Client(), server.URL); err != nil {
		t.Fatalf("credential preparation failed: %v", err)
	}
	if os.Getenv("DOCKERHUB_USERNAME") != "" || os.Getenv("DOCKERHUB_PASSWORD") != "" {
		t.Fatal("rejected Docker Hub credentials must be cleared before writing RKE2 registries.yaml")
	}
}

func TestPrepareDockerHubCredentialsForProvisioningFailsOnIndeterminateResponse(t *testing.T) {
	t.Setenv("DOCKERHUB_USERNAME", "valid-user")
	t.Setenv("DOCKERHUB_PASSWORD", "valid-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	err := prepareDockerHubCredentialsForProvisioningWithClient(server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("expected indeterminate Docker Hub response to fail clearly, got %v", err)
	}
	if os.Getenv("DOCKERHUB_USERNAME") == "" || os.Getenv("DOCKERHUB_PASSWORD") == "" {
		t.Fatal("indeterminate validation must not silently clear configured credentials")
	}
}

func TestRancherHelmCommandUsesExternalTLS(t *testing.T) {
	tests := []string{
		`helm install rancher rancher-latest/rancher --set tls=external`,
		`helm install rancher rancher-latest/rancher --set=tls=external`,
		`helm install rancher rancher-latest/rancher --set-string tls=external`,
		`helm install rancher rancher-latest/rancher --set-string=tls=external`,
		`helm install rancher rancher-latest/rancher --set 'tls=external'`,
		`helm install rancher rancher-latest/rancher --set tls=external,hostname=example.test`,
	}

	for _, command := range tests {
		if !rancherHelmCommandUsesExternalTLS(command) {
			t.Fatalf("expected command to use external TLS:\n%s", command)
		}
	}
}

func TestValidateRancherHelmCommandsUseExternalTLSRejectsIngressTLSDefault(t *testing.T) {
	err := validateRancherHelmCommandsUseExternalTLS([]string{
		`helm install rancher rancher-latest/rancher --set hostname=placeholder`,
	})
	if err == nil {
		t.Fatal("expected missing tls=external to fail")
	}
	if !strings.Contains(err.Error(), "tls=external") {
		t.Fatalf("expected error to mention tls=external, got %v", err)
	}
}

func TestValidateRancherHelmCommandsUseExternalTLSRejectsSecretIngressTLS(t *testing.T) {
	err := validateRancherHelmCommandsUseExternalTLS([]string{
		`helm install rancher rancher-latest/rancher --set ingress.tls.source=secret`,
	})
	if err == nil {
		t.Fatal("expected ingress TLS secret mode to fail")
	}
}

func TestBuildRKE2ImagesDownloadCommandRetriesDownloadsAndValidatesChecksum(t *testing.T) {
	command := buildRKE2ImagesDownloadCommand("v1.34.6+rke2r3")

	for _, want := range []string{
		"curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 600 -o /tmp/rke2-images.linux-amd64.tar.zst",
		"curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 120 -o /tmp/rke2-sha256sum-amd64.txt",
		"grep 'rke2-images.linux-amd64.tar.zst' /tmp/rke2-sha256sum-amd64.txt | sha256sum -c -",
		"SECURITY ERROR: RKE2 images checksum validation failed",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected RKE2 image download command to contain %q:\n%s", want, command)
		}
	}
}
