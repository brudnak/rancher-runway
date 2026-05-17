package test

import (
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
