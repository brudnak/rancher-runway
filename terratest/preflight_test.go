package test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
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
		name          string
		version       string
		wantErr       bool
		wantErrSubstr string
	}{
		{name: "recommended Helm 3", version: "v3.21.3"},
		{name: "Helm 3 without v prefix", version: "3.21.3"},
		{name: "Helm 3 with surrounding whitespace", version: "  v3.21.3\n"},
		{name: "Helm 3 with build metadata", version: "v3.21.3+g1234567"},
		{name: "Helm 3 prerelease", version: "v3.22.0-rc.1"},
		{name: "minimum Helm 3 line", version: "v3.0.0"},
		{name: "future Helm 3 minor", version: "v3.99.0"},
		{name: "unsupported Helm 2", version: "v2.17.0", wantErr: true, wantErrSubstr: "require Helm 3"},
		{name: "unsupported Helm 4", version: "v4.1.3", wantErr: true, wantErrSubstr: "found v4.1.3"},
		{name: "unsupported future major", version: "v5.0.0", wantErr: true, wantErrSubstr: "require Helm 3"},
		{name: "malformed", version: "development", wantErr: true, wantErrSubstr: "could not parse"},
		{name: "version prefix only", version: "v", wantErr: true, wantErrSubstr: "could not parse"},
		{name: "empty", version: "", wantErr: true, wantErrSubstr: "could not parse"},
		{name: "whitespace only", version: " \n\t", wantErr: true, wantErrSubstr: "could not parse"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRancherHelmVersion(test.version)
			if !test.wantErr && err != nil {
				t.Fatalf("validateRancherHelmVersion(%q) failed: %v", test.version, err)
			}
			if test.wantErr && err == nil {
				t.Fatalf("validateRancherHelmVersion(%q) succeeded, want error", test.version)
			}
			if test.wantErrSubstr != "" && !strings.Contains(err.Error(), test.wantErrSubstr) {
				t.Fatalf("validateRancherHelmVersion(%q) error = %q, want it to contain %q", test.version, err, test.wantErrSubstr)
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
	command, err := buildRKE2ImagesDownloadCommand("v1.34.6+rke2r3", settings.RKE2IngressControllerTraefik)
	if err != nil {
		t.Fatalf("buildRKE2ImagesDownloadCommand() error = %v", err)
	}

	for _, want := range []string{
		"curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 600",
		"rke2-images.linux-amd64.tar.zst",
		"rke2-images-traefik.linux-amd64.tar.zst",
		"curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 120 -o /tmp/rke2-sha256sum-amd64.txt",
		"awk -v archive='rke2-images.linux-amd64.tar.zst'",
		"awk -v archive='rke2-images-traefik.linux-amd64.tar.zst'",
		"[ ! -s '/tmp/rke2-selected-sha256sum.txt' ]",
		"sha256sum -c '/tmp/rke2-selected-sha256sum.txt'",
		"SECURITY ERROR: RKE2 images checksum validation failed",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected RKE2 image download command to contain %q:\n%s", want, command)
		}
	}
}

func TestBuildRKE2ImagesDownloadCommandRejectsMissingSupplementalChecksum(t *testing.T) {
	tempDir := t.TempDir()
	fakeBin := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	basePayload := "base-image"
	baseChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte(basePayload)))
	curlScript := strings.ReplaceAll(`#!/bin/bash
set -euo pipefail
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    output="$2"
    shift 2
    continue
  fi
  shift
done
case "${output##*/}" in
  rke2-images.linux-amd64.tar.zst)
    printf '%s' 'base-image' >"${output}"
    ;;
  rke2-images-traefik.linux-amd64.tar.zst)
    printf '%s' 'traefik-image' >"${output}"
    ;;
  rke2-sha256sum-amd64.txt)
    printf '%s  %s\n' '__BASE_CHECKSUM__' 'rke2-images.linux-amd64.tar.zst' >"${output}"
    ;;
  *)
    echo "unexpected curl output path: ${output}" >&2
    exit 1
    ;;
esac
`, "__BASE_CHECKSUM__", baseChecksum)
	if err := os.WriteFile(filepath.Join(fakeBin, "curl"), []byte(curlScript), 0o700); err != nil {
		t.Fatal(err)
	}

	command, err := buildRKE2ImagesDownloadCommand("v1.35.8+rke2r1", settings.RKE2IngressControllerTraefik)
	if err != nil {
		t.Fatal(err)
	}
	command = strings.ReplaceAll(command, "/tmp/", tempDir+string(os.PathSeparator))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := exec.Command("bash", "-c", command).CombinedOutput()
	if err == nil {
		t.Fatalf("expected missing supplemental checksum to fail:\n%s", output)
	}
	if !strings.Contains(string(output), "SECURITY ERROR: RKE2 images checksum validation failed") {
		t.Fatalf("expected checksum failure diagnostic, got:\n%s", output)
	}
	for _, name := range []string{
		"rke2-images.linux-amd64.tar.zst",
		"rke2-images-traefik.linux-amd64.tar.zst",
		"rke2-sha256sum-amd64.txt",
		"rke2-selected-sha256sum.txt",
	} {
		if _, statErr := os.Stat(filepath.Join(tempDir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("expected failed checksum validation to remove %s, stat err=%v", name, statErr)
		}
	}
}

func TestRKE2ImageArchiveNamesFollowControllerPackaging(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		controller string
		want       []string
	}{
		{
			name:       "Traefik before RKE2 1.36 uses supplemental bundle",
			version:    "v1.35.8+rke2r1",
			controller: settings.RKE2IngressControllerTraefik,
			want:       []string{"rke2-images.linux-amd64.tar.zst", "rke2-images-traefik.linux-amd64.tar.zst"},
		},
		{
			name:       "Traefik at RKE2 1.36 boundary is in the main bundle",
			version:    "v1.36.0+rke2r1",
			controller: settings.RKE2IngressControllerTraefik,
			want:       []string{"rke2-images.linux-amd64.tar.zst"},
		},
		{
			name:       "nginx before RKE2 1.36 is in the main bundle",
			version:    "v1.35.8+rke2r1",
			controller: settings.RKE2IngressControllerNginx,
			want:       []string{"rke2-images.linux-amd64.tar.zst"},
		},
		{
			name:       "nginx at RKE2 1.36 boundary uses supplemental bundle",
			version:    "v1.36.0+rke2r1",
			controller: settings.RKE2IngressControllerNginx,
			want:       []string{"rke2-images.linux-amd64.tar.zst", "rke2-images-ingress-nginx.linux-amd64.tar.zst"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rke2ImageArchiveNames(tt.version, tt.controller)
			if err != nil {
				t.Fatalf("rke2ImageArchiveNames() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("rke2ImageArchiveNames() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestValidateRKE2IngressControllerVersionRejectsRemovedNginx(t *testing.T) {
	err := validateRKE2IngressControllerVersion("v1.37.0+rke2r1", settings.RKE2IngressControllerNginx)
	if err == nil {
		t.Fatal("expected ingress-nginx on RKE2 1.37 to fail")
	}
	if !strings.Contains(err.Error(), "not available") || !strings.Contains(err.Error(), "use traefik") {
		t.Fatalf("unexpected compatibility error: %v", err)
	}

	if err := validateRKE2IngressControllerVersion("v1.36.3+rke2r1", settings.RKE2IngressControllerNginx); err != nil {
		t.Fatalf("expected ingress-nginx on RKE2 1.36 to remain supported, got %v", err)
	}
}

func TestValidateRKE2IngressControllerVersionEnforcesTraefikMinimum(t *testing.T) {
	err := validateRKE2IngressControllerVersion("v1.30.2+rke2r1", settings.RKE2IngressControllerTraefik)
	if err == nil {
		t.Fatal("expected Traefik before RKE2 v1.30.3 to fail")
	}
	if !strings.Contains(err.Error(), "requires RKE2 v1.30.3+rke2r1 or newer") || !strings.Contains(err.Error(), "use ingress-nginx") {
		t.Fatalf("unexpected Traefik minimum-version error: %v", err)
	}
	if err := validateRKE2IngressControllerVersion("v1.30.3+rke2r1", settings.RKE2IngressControllerTraefik); err != nil {
		t.Fatalf("expected Traefik at RKE2 v1.30.3 boundary to pass, got %v", err)
	}
}

func TestValidateResolvedRKE2IngressControllerVersionsIdentifiesPlan(t *testing.T) {
	err := validateResolvedRKE2IngressControllerVersions([]*RancherResolvedPlan{
		{RecommendedRKE2Version: "v1.36.3+rke2r1"},
		{RecommendedRKE2Version: "v1.37.0+rke2r1"},
	}, settings.RKE2IngressControllerNginx)
	if err == nil {
		t.Fatal("expected a resolved RKE2 1.37 nginx plan to fail")
	}
	if !strings.Contains(err.Error(), "plan 2") {
		t.Fatalf("expected compatibility error to identify plan 2, got %v", err)
	}
}

func TestBuildRKE2ImagesMoveCommandIncludesSupplementalArchive(t *testing.T) {
	command, err := buildRKE2ImagesMoveCommand("v1.36.3+rke2r1", settings.RKE2IngressControllerNginx)
	if err != nil {
		t.Fatalf("buildRKE2ImagesMoveCommand() error = %v", err)
	}
	for _, want := range []string{"rke2-images.linux-amd64.tar.zst", "rke2-images-ingress-nginx.linux-amd64.tar.zst"} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected move command to contain %q: %s", want, command)
		}
	}
}
