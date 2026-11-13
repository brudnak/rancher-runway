package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLastNonEmptyLines(t *testing.T) {
	got := lastNonEmptyLines("one\ntwo\nthree\n", 2)
	if got != "two\nthree" {
		t.Fatalf("lastNonEmptyLines() = %q, want two trailing lines", got)
	}
}

func TestSanitizeKubeDiagnosticOutput(t *testing.T) {
	t.Setenv("RANCHER_BOOTSTRAP_PASSWORD", "secret-value")

	got := sanitizeKubeDiagnosticOutput("password=secret-value")
	if strings.Contains(got, "secret-value") {
		t.Fatalf("expected secret to be redacted, got %q", got)
	}
}

func readyDeploymentsFixture() []rancherDeployment {
	var result []rancherDeployment
	for _, id := range []string{serverDeployment, webhookDeployment, fleetDeployment, turtlesDeployment, upgradeDeployment} {
		var d rancherDeployment
		d.Metadata.Namespace, d.Metadata.Name, _ = strings.Cut(id, "/")
		d.Metadata.Generation, d.Status.ObservedGeneration = 2, 2
		d.Spec.Replicas, d.Status.Replicas, d.Status.AvailableReplicas, d.Status.ReadyReplicas, d.Status.UpdatedReplicas = 1, 1, 1, 1, 1
		result = append(result, d)
	}
	result[0].Spec.Template.Spec.Containers = []systemChartContainer{{Name: "rancher", Image: "stgregistry.suse.com/rancher/rancher-server:v2.16.0-e3599b9-head"}}
	return result
}

func installSystemKubectlFixture(t *testing.T, deployments []rancherDeployment, logs string) string {
	t.Helper()
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kube_config.yaml")
	raw, err := json.Marshal(map[string]any{"items": deployments})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"deployments": string(raw), "logs": logs, "kube_config.yaml": "fixture", "kubectl": `#!/bin/sh
case "$*" in
  *"get deployments.apps -A -o json"*) cat "$CHART_FIXTURE/deployments";;
  *"logs "*"-l app=rancher -c rancher --since=90s --tail=300"*) cat "$CHART_FIXTURE/logs";;
  *"get clusters.management.cattle.io local"*) printf '%s' "$LOCAL_SUC_OVERRIDE";;
  *"get settings.management.cattle.io imported-cluster-version-management"*) echo '{"default":"true"}';;
  *) exit 1;;
esac
`}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CHART_FIXTURE", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return kubeconfig
}

func TestRancherSystemReadiness(t *testing.T) {
	cases := []struct {
		name   string
		change func([]rancherDeployment)
		ready  bool
		detail string
	}{
		{"healthy", func(d []rancherDeployment) {}, true, turtlesDeployment},
		{"wrong server", func(d []rancherDeployment) { d[0].Metadata.Name = "rancher-scc-operator" }, false, serverDeployment + " missing"},
		{"missing webhook", func(d []rancherDeployment) { d[1].Metadata.Namespace = "other" }, false, webhookDeployment + " missing"},
		{"missing fleet", func(d []rancherDeployment) { d[2].Metadata.Name = "fleet-agent" }, false, fleetDeployment + " missing"},
		{"missing turtles", func(d []rancherDeployment) { d[3].Metadata.Namespace = "other" }, false, turtlesDeployment + " missing"},
		{"missing SUC", func(d []rancherDeployment) { d[4].Metadata.Namespace = "system-upgrade" }, false, upgradeDeployment + " missing"},
		{"stale status", func(d []rancherDeployment) { d[2].Status.ObservedGeneration-- }, false, "generation=1/2"},
		{"partial rollout", func(d []rancherDeployment) { d[3].Status.UpdatedReplicas = 0 }, false, "updated=0"},
		{"scaled to zero", func(d []rancherDeployment) { d[1].Spec.Replicas = 0 }, false, "ready=1/0"},
		{"old release", func(d []rancherDeployment) {
			d[0].Spec.Template.Spec.Containers[0].Image = "rancher/rancher:v2.12.15-abcdeff-head"
			d[3].Metadata.Name = "absent"
		}, true, fleetDeployment},
		{"turtles disabled", func(d []rancherDeployment) {
			d[0].Spec.Template.Spec.Containers[0].Env = []systemChartEnvVar{{Name: "CATTLE_FEATURES", Value: "turtles=false"}}
			d[3].Metadata.Name = "absent"
		}, true, fleetDeployment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := readyDeploymentsFixture()
			tc.change(d)
			kc := installSystemKubectlFixture(t, d, "WARN unable to find exact version requested: falling back to closest available version chart=rancher-turtles")
			ready, summary, err := rancherSystemDeploymentsReady(kc)
			if err != nil || ready != tc.ready || !strings.Contains(summary, tc.detail) {
				t.Fatalf("got ready=%t summary=%q err=%v", ready, summary, err)
			}
		})
	}
	t.Run("version management disabled locally", func(t *testing.T) {
		t.Setenv("LOCAL_SUC_OVERRIDE", "false")
		kc := installSystemKubectlFixture(t, readyDeploymentsFixture()[:4], "")
		if ready, summary, err := rancherSystemDeploymentsReady(kc); !ready || err != nil {
			t.Fatalf("%s: %v", summary, err)
		}
	})
}

func TestRancherSystemReadinessStaleAssets(t *testing.T) {
	d := readyDeploymentsFixture()[:2] // webhook has already recovered
	const logs = `2026/11/12 20:36:58 [ERROR] Failed to install system chart fleet-crd (release name: fleet-crd), will retry: specified version 111.0.0+up0.17.0-rc.2 doesn't exist in the index
2026/11/12 20:36:58 [ERROR] Failed to install system chart rancher-turtles (release name: rancher-turtles), will retry: specified version 111.0.0+up0.28.0-rc.5 doesn't exist in the index
2026/11/12 20:36:58 [ERROR] Failed to install system chart rancher-webhook (release name: rancher-webhook), will retry: specified version 111.0.0+up0.12.1-rc.17 doesn't exist in the index
unrelated password=must-never-appear`
	kc := installSystemKubectlFixture(t, d, logs)
	t.Setenv(haOutputRootEnv, filepath.Dir(kc))
	ha := haInstanceDir(1)
	if err := os.Mkdir(ha, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(kc, filepath.Join(ha, "kube_config.yaml")); err != nil {
		t.Fatal(err)
	}
	err := waitForHAReady(1, nil, 0, time.Second, 0)
	if err == nil {
		t.Fatal("expected an immediate chart failure")
	}
	for _, want := range []string{"fleet-crd@111.0.0+up0.17.0-rc.2", "rancher-turtles@111.0.0+up0.28.0-rc.5", "e3599b9-head", "rancher-assets"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %q in %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "must-never-appear") || strings.Contains(err.Error(), "rancher-webhook") {
		t.Fatalf("unexpected details: %v", err)
	}
}
