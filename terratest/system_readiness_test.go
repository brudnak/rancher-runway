package test

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

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
	if !toolMajorVersionSupported(*helmConfig, "3.21.3") {
		t.Fatal("expected Helm 3 to satisfy readiness")
	}
	if toolMajorVersionSupported(*helmConfig, "4.1.3") {
		t.Fatal("expected Helm 4 to fail readiness")
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
