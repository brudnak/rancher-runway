package test

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
)

func TestPanelImportedAWSPrefixCollisionBlocksAdmission(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	for _, prefix := range []string{"atb-r12345678", "gha-atb-12345678-pr"} {
		t.Run(prefix, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
			p := &localControlPanel{repoRoot: root, testDir: root}
			viper.Set("deployment.type", deploymentTypeHARKE2)
			viper.Set("tf_vars.aws_prefix", prefix)
			p.writeRunRecord(panelRunRecord{RunID: "existing", AWSPrefix: prefix, DeploymentType: deploymentTypeHARKE2})
			if ok, reason := p.awsPrefixAvailableForIsolatedRun(""); ok || !strings.Contains(reason, "existing") {
				t.Fatalf("setup availability accepted a reused prefix: %v, %s", ok, reason)
			}
			err := p.startPanelCommand(panelCommandSpec{
				Operation: panelOperationSetup, RunID: "new12345", DisplayName: "setup", TestName: "TestHaSetup",
			})
			if err == nil || !strings.Contains(err.Error(), "AWS prefix "+prefix) {
				t.Fatalf("admission accepted reused AWS prefix: %v", err)
			}
			if _, err := os.Stat(p.runConfigPath("new12345")); !os.IsNotExist(err) {
				t.Fatalf("rejected admission created a snapshot: %v", err)
			}
			if viper.GetString("tf_vars.aws_prefix") != prefix {
				t.Fatal("collision check rewrote the configured prefix")
			}
		})
	}
}

func TestPanelAWSPrefixAvailabilityPreservesDistinctRuns(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	p := &localControlPanel{repoRoot: root, testDir: root}
	p.writeRunRecord(panelRunRecord{RunID: "existing", AWSPrefix: "atb-r12345678", DeploymentType: deploymentTypeHARKE2})
	for _, tc := range []struct{ deployment, prefix, runID string }{
		{deploymentTypeHARKE2, "atb", ""},
		{deploymentTypeHARKE2, "atb", "87654321"},
		{deploymentTypeHARKE2, "atb-r87654321", "87654321"},
		{deploymentTypeHARKE2, "gha-atb-87654321-pr", "87654321"},
		{deploymentTypeLinodeDocker, "atb-r12345678", "87654321"},
	} {
		viper.Set("deployment.type", tc.deployment)
		viper.Set("tf_vars.aws_prefix", tc.prefix)
		if ok, reason := p.awsPrefixAvailableForIsolatedRun(tc.runID); !ok {
			t.Fatalf("blocked distinct run with %s: %s", tc.prefix, reason)
		}
	}
	viper.Set("deployment.type", deploymentTypeHARKE2)
	viper.Set("tf_vars.aws_prefix", "atb")
	if ok, reason := p.awsPrefixAvailableForIsolatedRun("12345678"); ok || !strings.Contains(reason, "atb-r12345678") {
		t.Fatalf("did not check final scoped prefix: %v, %s", ok, reason)
	}
}

func TestPanelCustomHostnameAvailabilityDuringConfigurationChanges(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	p := &localControlPanel{repoRoot: root, testDir: root}
	p.writeRunRecord(panelRunRecord{RunID: "existing", CustomHostnamePrefix: "taken", Route53FQDN: "example.com"})
	viper.Set("deployment.type", deploymentTypeHARKE2)
	viper.Set(settings.CustomHostnameConfigKey, "taken")
	viper.Set("tf_vars.aws_route53_fqdn", "example.com")
	if ok, reason := p.customHostnameAvailableForIsolatedRun(); ok || !strings.Contains(reason, "taken.example.com") {
		t.Fatalf("hostname collision was not detected: %v, %s", ok, reason)
	}
	viper.Set(settings.CustomHostnameConfigKey, "free")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			viperConfigMu.Lock()
			viper.Set("deployment.type", deploymentTypeHARKE2)
			viper.Set(settings.CustomHostnameConfigKey, "free")
			viper.Set("tf_vars.aws_route53_fqdn", "example.com")
			viper.Set("tf_vars.aws_prefix", "atb")
			viperConfigMu.Unlock()
		}
	}()
	for i := 0; i < 100; i++ {
		if ok, reason := p.customHostnameAvailableForIsolatedRun(); !ok {
			t.Errorf("free hostname unavailable: %s", reason)
		}
		if ok, reason := p.awsPrefixAvailableForIsolatedRun("87654321"); !ok {
			t.Errorf("free AWS prefix unavailable: %s", reason)
		}
	}
	wg.Wait()
}
