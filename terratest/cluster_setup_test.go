package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestRKE2IngressNginxConfigManifestEnablesForwardedHeaders(t *testing.T) {
	manifest := rke2IngressNginxConfigManifest()
	expectedSnippets := []string{
		"kind: HelmChartConfig",
		"name: rke2-ingress-nginx",
		"namespace: kube-system",
		`use-forwarded-headers: "true"`,
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(manifest, snippet) {
			t.Fatalf("expected RKE2 ingress config manifest to contain %q, got:\n%s", snippet, manifest)
		}
	}
}

func TestRancherHelmCommandForHASetsSingleServerReplicas(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rke2.server_count", 1)

	command := rancherHelmCommandForHA("helm install rancher rancher-latest/rancher --set tls=external", "rancher.example.test")

	for _, want := range []string{
		"--set hostname=rancher.example.test",
		"--set replicas=1",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected command to contain %q, got:\n%s", want, command)
		}
	}
}

func TestRancherHelmCommandForHAKeepsExplicitReplicas(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rke2.server_count", 1)

	command := rancherHelmCommandForHA("helm install rancher rancher-latest/rancher --set tls=external,replicas=2", "rancher.example.test")

	if strings.Contains(command, "--set replicas=1") {
		t.Fatalf("expected explicit replicas to be preserved, got:\n%s", command)
	}
}

func TestResolvedRancherInstallCommandPreservesPrimeHeadPlan(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rancher.helm_commands", []string{
		"helm install rancher rancher-latest/rancher --version 2.15.0 --set image.tag=stale",
	})

	const headTag = "v2.15.1-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head"
	plannedCommand := buildAutoHelmCommand(
		rancherHelmOperationInstall,
		"rancher-prime",
		"2.15.0",
		"admin",
		"stgregistry.suse.com/rancher/rancher",
		headTag,
		"stgregistry.suse.com/rancher/rancher-agent:"+headTag,
		true,
	)
	plan := &RancherResolvedPlan{
		ChartRepoAlias: "rancher-prime",
		ChartVersion:   "2.15.0",
		HelmCommands:   []string{plannedCommand},
	}

	command, err := resolvedRancherInstallCommand(1, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"helm install rancher rancher-prime/rancher",
		"--version 2.15.0",
		"--set image.registry=stgregistry.suse.com",
		"--set image.repository=rancher/rancher",
		"--set image.tag=" + headTag,
		"stgregistry.suse.com/rancher/rancher-agent:" + headTag,
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("resolved install command lost %q:\n%s", want, command)
		}
	}
	if strings.Contains(command, "image.tag=stale") || strings.Contains(command, "rancher-latest/rancher") {
		t.Fatalf("resolved plan was replaced by stale global Helm state:\n%s", command)
	}

	haDir := filepath.Join(t.TempDir(), "high-availability-1")
	CreateInstallScript(command, haDir)
	installScript, err := os.ReadFile(filepath.Join(haDir, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installScript), "helm install rancher rancher-prime/rancher") ||
		!strings.Contains(string(installScript), "--set image.tag="+headTag) {
		t.Fatalf("generated install.sh lost the resolved Prime-head command:\n%s", installScript)
	}
}

func TestResolvedRancherInstallCommandFallsBackWithoutPlan(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rancher.helm_commands", []string{
		"helm install rancher rancher-latest/rancher --version 2.15.0",
	})

	command, err := resolvedRancherInstallCommand(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command, "rancher-latest/rancher --version 2.15.0") {
		t.Fatalf("legacy install command was not preserved: %s", command)
	}
}
