package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestCoreSystemReadinessUsesPackagedWorkerInsteadOfGo(t *testing.T) {
	worker := filepath.Join(t.TempDir(), "rancher-runway-lifecycle")
	if err := os.WriteFile(worker, []byte("worker"), 0o755); err != nil {
		t.Fatalf("create packaged worker: %v", err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)

	item := checkCoreSystemReadinessTool(systemReadinessToolConfig{Name: "Go", Command: "go"})
	if item.Status != "ok" || item.Version != "bundled" || !strings.Contains(item.Detail, "Go is not required") {
		t.Fatalf("packaged Go readiness = %#v", item)
	}
}

func TestSystemReadinessRequiresHelm3(t *testing.T) {
	config := loadSystemReadinessConfig()
	var helmConfig *systemReadinessToolConfig
	for i := range config.Tools {
		if config.Tools[i].Command == "helm" {
			helmConfig = &config.Tools[i]
			break
		}
	}
	if helmConfig == nil {
		t.Fatal("system readiness config does not include Helm")
	}
	if helmConfig.RequiredMajorVersion != 3 {
		t.Fatalf("Helm required major = %d, want 3", helmConfig.RequiredMajorVersion)
	}
	if helmConfig.RecommendedVersion != "3.21.3" {
		t.Fatalf("Helm recommended version = %q, want 3.21.3", helmConfig.RecommendedVersion)
	}
}

func TestToolMajorVersionSupported(t *testing.T) {
	tests := []struct {
		name          string
		requiredMajor int
		version       string
		want          bool
	}{
		{name: "requirement disabled accepts Helm 4", version: "4.1.3", want: true},
		{name: "requirement disabled accepts empty version", version: "", want: true},
		{name: "Helm 3 exact", requiredMajor: 3, version: "3.21.3", want: true},
		{name: "Helm 3 v prefix", requiredMajor: 3, version: "v3.21.3", want: true},
		{name: "Helm 3 build metadata", requiredMajor: 3, version: "3.21.3+g1234567", want: true},
		{name: "Helm 3 prerelease", requiredMajor: 3, version: "3.22.0-rc.1", want: true},
		{name: "Helm 3 older minor", requiredMajor: 3, version: "3.18.0", want: true},
		{name: "Helm 2 rejected", requiredMajor: 3, version: "2.17.0", want: false},
		{name: "Helm 4 rejected", requiredMajor: 3, version: "4.1.3", want: false},
		{name: "future major rejected", requiredMajor: 3, version: "10.0.0", want: false},
		{name: "empty rejected", requiredMajor: 3, version: "", want: false},
		{name: "malformed rejected", requiredMajor: 3, version: "development", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tool := systemReadinessToolConfig{RequiredMajorVersion: test.requiredMajor}
			if got := toolMajorVersionSupported(tool, test.version); got != test.want {
				t.Fatalf("toolMajorVersionSupported(required=%d, version=%q) = %t, want %t", test.requiredMajor, test.version, got, test.want)
			}
		})
	}
}

func TestExtractToolVersion(t *testing.T) {
	helm := systemReadinessToolConfig{VersionPattern: `v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`}
	terraform := systemReadinessToolConfig{JSONVersionKey: "terraform_version"}
	tests := []struct {
		name   string
		tool   systemReadinessToolConfig
		output string
		want   string
	}{
		{name: "Helm 3 short output", tool: helm, output: "v3.21.3+g1ad6e68\n", want: "3.21.3"},
		{name: "Helm 4 short output", tool: helm, output: "v4.1.3+gc94d381\n", want: "4.1.3"},
		{name: "Helm version embedded in text", tool: helm, output: `version.BuildInfo{Version:"v3.18.0", GitCommit:"abc"}`, want: "3.18.0"},
		{name: "missing Helm version", tool: helm, output: "development"},
		{name: "empty Helm output", tool: helm},
		{name: "invalid pattern", tool: systemReadinessToolConfig{VersionPattern: "["}, output: "v3.21.3"},
		{name: "Terraform JSON", tool: terraform, output: `{"terraform_version":"1.14.8"}`, want: "1.14.8"},
		{name: "Terraform JSON normalizes v prefix", tool: terraform, output: `{"terraform_version":"v1.14.8"}`, want: "1.14.8"},
		{name: "Terraform malformed JSON", tool: terraform, output: `{`},
		{name: "Terraform non-string version", tool: terraform, output: `{"terraform_version":1148}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractToolVersion(test.tool, test.output); got != test.want {
				t.Fatalf("extractToolVersion(%q) = %q, want %q", test.output, got, test.want)
			}
		})
	}
}

func TestCompareVersionStrings(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		want        int
	}{
		{name: "equal", left: "3.21.3", right: "3.21.3"},
		{name: "v prefix equal", left: "v3.21.3", right: "3.21.3"},
		{name: "major less", left: "3.99.99", right: "4.0.0", want: -1},
		{name: "major greater", left: "4.0.0", right: "3.99.99", want: 1},
		{name: "minor less", left: "3.20.9", right: "3.21.0", want: -1},
		{name: "minor greater", left: "3.22.0", right: "3.21.99", want: 1},
		{name: "patch less", left: "3.21.2", right: "3.21.3", want: -1},
		{name: "patch greater", left: "3.21.4", right: "3.21.3", want: 1},
		{name: "missing patch equals zero", left: "3.21", right: "3.21.0"},
		{name: "build metadata ignored", left: "3.21.3+g123", right: "3.21.3"},
		{name: "prerelease suffix ignored", left: "3.22.0-rc.1", right: "3.22.0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := compareVersionStrings(test.left, test.right); got != test.want {
				t.Fatalf("compareVersionStrings(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestDeploymentSecretReadinessItemsRequireLinodeSecrets(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeLinodeDocker)
	t.Setenv("LINODE_TOKEN", "")
	t.Setenv("LINODE_ACCESS_TOKEN", "")

	items := deploymentSecretReadinessItems()
	if len(items) != 1 {
		t.Fatalf("expected one Linode readiness item, got %#v", items)
	}
	for _, item := range items {
		if item.Status != "error" {
			t.Fatalf("expected missing %s to be an error, got %#v", item.Name, item)
		}
		if !strings.Contains(item.Detail, ".zprofile") {
			t.Fatalf("expected %s detail to mention .zprofile, got %q", item.Name, item.Detail)
		}
	}
}

func TestDeploymentSecretReadinessItemsAcceptLinodeEnvAliases(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeLinodeDocker)
	t.Setenv("LINODE_ACCESS_TOKEN", "token")

	items := deploymentSecretReadinessItems()
	if len(items) != 1 {
		t.Fatalf("expected one Linode readiness item, got %#v", items)
	}
	for _, item := range items {
		if item.Status != "ok" {
			t.Fatalf("expected %s to pass with env alias, got %#v", item.Name, item)
		}
	}
}

func TestDeploymentSecretReadinessItemsAcceptLinodeConfigValues(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeLinodeDocker)
	viper.Set("linode.access_token", "token")

	items := deploymentSecretReadinessItems()
	if len(items) != 1 {
		t.Fatalf("expected one Linode readiness item, got %#v", items)
	}
	for _, item := range items {
		if item.Status != "ok" {
			t.Fatalf("expected %s to pass with config value, got %#v", item.Name, item)
		}
	}
}

func TestDeploymentSecretReadinessItemsSkipNonLinodeDeployments(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeHARKE2)

	if items := deploymentSecretReadinessItems(); len(items) != 0 {
		t.Fatalf("expected non-Linode deployment to skip Linode readiness, got %#v", items)
	}
}
