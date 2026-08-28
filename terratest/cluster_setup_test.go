package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
)

func TestRKE2TraefikConfigManifestTrustsOnlyALBSubnets(t *testing.T) {
	fileName, manifest, err := rke2IngressConfigManifest(settings.RKE2IngressControllerTraefik, []string{
		"10.0.2.42/24",
		"10.0.1.0/24",
		"10.0.1.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fileName != "rke2-traefik-config.yaml" {
		t.Fatalf("unexpected Traefik manifest filename %q", fileName)
	}
	expectedSnippets := []string{
		"kind: HelmChartConfig",
		"name: rke2-traefik",
		"namespace: kube-system",
		"ports:",
		"web:",
		"forwardedHeaders:",
		"insecure: false",
		`- "10.0.1.0/24"`,
		`- "10.0.2.0/24"`,
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(manifest, snippet) {
			t.Fatalf("expected Traefik config manifest to contain %q, got:\n%s", snippet, manifest)
		}
	}
	if strings.Count(manifest, `10.0.1.0/24`) != 1 {
		t.Fatalf("expected trusted CIDRs to be de-duplicated, got:\n%s", manifest)
	}
}

func TestRKE2IngressNginxConfigManifestConstrainsForwardedHeaders(t *testing.T) {
	fileName, manifest, err := rke2IngressConfigManifest(settings.RKE2IngressControllerNginx, []string{"10.0.2.0/24", "10.0.1.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	if fileName != "rke2-ingress-nginx-config.yaml" {
		t.Fatalf("unexpected ingress-nginx manifest filename %q", fileName)
	}
	for _, snippet := range []string{
		"name: rke2-ingress-nginx",
		`use-forwarded-headers: "true"`,
		`proxy-real-ip-cidr: "10.0.1.0/24,10.0.2.0/24"`,
	} {
		if !strings.Contains(manifest, snippet) {
			t.Fatalf("expected ingress-nginx config manifest to contain %q, got:\n%s", snippet, manifest)
		}
	}
}

func TestRKE2IngressConfigManifestRejectsMissingOrInvalidCIDRs(t *testing.T) {
	for _, cidrs := range [][]string{nil, {"not-a-cidr"}} {
		if _, _, err := rke2IngressConfigManifest(settings.RKE2IngressControllerTraefik, cidrs); err == nil {
			t.Fatalf("expected CIDRs %#v to fail", cidrs)
		}
	}
}

func TestRKE2ServerConfigsSetIngressControllerExplicitly(t *testing.T) {
	haOutputs := TerraformOutputs{
		RancherURL:       "rancher.example.test",
		ServerIPs:        []string{"203.0.113.10", "203.0.113.11"},
		ServerPrivateIPs: []string{"10.0.1.10", "10.0.2.10"},
	}

	first := rke2FirstServerConfigContent(haOutputs, settings.RKE2IngressControllerTraefik)
	additional := rke2AdditionalServerConfigContent("203.0.113.10", "join-token", haOutputs, settings.RKE2IngressControllerTraefik)
	for name, config := range map[string]string{"first": first, "additional": additional} {
		if !strings.Contains(config, "ingress-controller: traefik") {
			t.Fatalf("%s server config omitted explicit Traefik selection:\n%s", name, config)
		}
	}
}

func TestRKE2IngressDaemonSetName(t *testing.T) {
	tests := []struct {
		controller string
		want       string
		wantErr    bool
	}{
		{controller: settings.RKE2IngressControllerTraefik, want: "rke2-traefik"},
		{controller: settings.RKE2IngressControllerNginx, want: "rke2-ingress-nginx-controller"},
		{controller: "unknown", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.controller, func(t *testing.T) {
			got, err := rke2IngressDaemonSetName(tt.controller)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("rke2IngressDaemonSetName(%q) unexpectedly succeeded with %q", tt.controller, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("rke2IngressDaemonSetName(%q) error = %v", tt.controller, err)
			}
			if got != tt.want {
				t.Fatalf("rke2IngressDaemonSetName(%q) = %q, want %q", tt.controller, got, tt.want)
			}
		})
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
	CreateInstallScript(command, haDir, "rke2-traefik")
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
