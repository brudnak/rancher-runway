package test

import (
	"testing"

	"github.com/spf13/viper"
)

func TestHostedTenantRancherInstanceCountIgnoresStaleCountForHARKE2(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("deployment.type", deploymentTypeHARKE2)
	viper.Set("total_has", 1)
	viper.Set("total_rancher_instances", 1)

	if got := configuredRancherInstanceCount(); got != 1 {
		t.Fatalf("expected HA count to use total_has, got %d", got)
	}
	if got := hostedTenantRancherInstanceCount(); got != 0 {
		t.Fatalf("expected non-hosted deployment to suppress total_rancher_instances, got %d", got)
	}
}

func TestHostedTenantRancherInstanceCountUsesHostedCountForHostedDeployment(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("deployment.type", deploymentTypeHostedTenantK3S)
	viper.Set("total_has", 1)
	viper.Set("total_rancher_instances", 3)

	if got := configuredRancherInstanceCount(); got != 3 {
		t.Fatalf("expected configured count to use hosted total, got %d", got)
	}
	if got := hostedTenantRancherInstanceCount(); got != 3 {
		t.Fatalf("expected hosted count to use total_rancher_instances, got %d", got)
	}
}
