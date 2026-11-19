package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
  *"get clusters.management.cattle.io local -o json") cat "$CHART_FIXTURE/cluster";;
  *"get settings.management.cattle.io imported-cluster-version-management -o json") cat "$CHART_FIXTURE/setting";;
  *"get settings.management.cattle.io system-catalog -o json") cat "$CHART_FIXTURE/catalog";;
  *"get clusterrepos.catalog.cattle.io rancher-charts -o json") cat "$CHART_FIXTURE/repo";;
  *) exit 1;;
esac
`}
	files["cluster"] = `{"status":{"driver":"rke2","version":{"gitVersion":"v1.36.6+rke2r1"}}}`
	files["setting"] = `{"default":"true"}`
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
			ready, summary, err := new(rancherSystemReadiness).deploymentsReady(kc)
			if err != nil || ready != tc.ready || !strings.Contains(summary, tc.detail) {
				t.Fatalf("got ready=%t summary=%q err=%v", ready, summary, err)
			}
		})
	}
}

func TestRancherLocalUpgradeControllerReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, driver, provider, kubeVersion string
		annotation, setting, flags          string
		ready                               bool
		detail                              string
	}{
		{"required on local RKE2", "rke2", "", "v1.36.6+rke2r1", "", "true", "", false, "version management=true (global setting)"},
		{"required on local K3s", "k3s", "", "v1.35.1+k3s1", "true", "false", "", false, "version management=true (annotation)"},
		{"disabled locally", "rke2", "", "", "false", "true", "", true, "version management=false (annotation)"},
		{"disabled globally", "k3s", "", "", "system-default", "false", "", true, "version management=false (global setting)"},
		{"generic local Kubernetes", "local", "", "v1.35.1", "", "true", "", true, `driver="local"`},
		{"imported Harvester local", "imported", "harvester", "v1.35.1", "", "true", "multi-cluster-management=false", false, "version management=true"},
		{"imported other provider", "imported", "other", "v1.35.1", "", "true", "", true, `driver="imported"`},
		{"driver not yet reported", "", "", "", "", "true", "", false, `driver=""`},
		{"local before RKE2 detection", "local", "", "v1.36.6+rke2r1", "", "true", "", true, "controller enabled=false for current driver"},
		{"version not yet reported", "local", "", "", "", "true", "", true, `kubernetes=""`},
		{"imported without version", "imported", "", "", "", "true", "", true, "controller enabled=false for current driver"},
		{"imported RKE2 awaiting driver update", "imported", "", "v1.36.6+rke2r1", "", "true", "", true, "controller enabled=false for current driver"},
		{"setting not yet available", "rke2", "", "", "", "", "", false, "waiting for version-management policy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := readyDeploymentsFixture()[:4]
			d[0].Spec.Template.Spec.Containers[0] = systemChartContainer{Name: "rancher", Image: "rancher/rancher:v2.15.3", Env: []systemChartEnvVar{{Name: "CATTLE_FEATURES", Value: tc.flags}}}
			kc := installSystemKubectlFixture(t, d, "")
			cluster := map[string]any{
				"metadata": map[string]any{"annotations": map[string]string{"rancher.io/imported-cluster-version-management": tc.annotation}},
				"status":   map[string]any{"driver": tc.driver, "provider": tc.provider, "version": map[string]string{"gitVersion": tc.kubeVersion}},
			}
			for name, fixture := range map[string]any{"cluster": cluster, "setting": map[string]string{"value": tc.setting}} {
				data, err := json.Marshal(fixture)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(kc), name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ready, summary, err := new(rancherSystemReadiness).deploymentsReady(kc)
			if err != nil || ready != tc.ready || !strings.Contains(summary, tc.detail) || strings.Contains(summary, upgradeDeployment+" missing") == tc.ready {
				t.Fatalf("got ready=%t summary=%q err=%v", ready, summary, err)
			}
		})
	}
	t.Run("unreadable local cluster is retryable", func(t *testing.T) {
		kc := installSystemKubectlFixture(t, readyDeploymentsFixture()[:4], "")
		if err := os.Remove(filepath.Join(filepath.Dir(kc), "cluster")); err != nil {
			t.Fatal(err)
		}
		if ready, summary, err := new(rancherSystemReadiness).deploymentsReady(kc); ready || err != nil || !strings.Contains(summary, "waiting for local cluster status") {
			t.Fatalf("got ready=%t summary=%q err=%v", ready, summary, err)
		}
	})
}

func TestRancherLocalUpgradeControllerDriverTransition(t *testing.T) {
	for _, image := range []string{
		"registry.rancher.com/rancher/rancher:v2.15.3",
		"stgregistry.suse.com/rancher/rancher:v2.15.4-a0ebc0e-head",
	} {
		t.Run(image, func(t *testing.T) {
			deployments := readyDeploymentsFixture()
			deployments[0].Spec.Template.Spec.Containers[0].Image = image
			kc := installSystemKubectlFixture(t, deployments[:4], "")
			for _, observation := range []struct {
				name, cluster string
				deployments   []rancherDeployment
				ready         bool
			}{
				// Both failed runs stayed in this state with all other system deployments ready.
				{"imported", `{"status":{"driver":"imported"}}`, deployments[:4], true},
				{"detected RKE2", `{"status":{"driver":"rke2","version":{"gitVersion":"v1.36.6+rke2r1"}}}`, deployments[:4], false},
				{"controller ready", `{"status":{"driver":"rke2"}}`, deployments, true},
			} {
				t.Run(observation.name, func(t *testing.T) {
					data, err := json.Marshal(map[string]any{"items": observation.deployments})
					if err != nil {
						t.Fatal(err)
					}
					for name, value := range map[string]string{"cluster": observation.cluster, "deployments": string(data)} {
						if err := os.WriteFile(filepath.Join(filepath.Dir(kc), name), []byte(value), 0600); err != nil {
							t.Fatal(err)
						}
					}
					ready, summary, err := new(rancherSystemReadiness).deploymentsReady(kc)
					if err != nil || ready != observation.ready {
						t.Fatalf("got ready=%t summary=%q err=%v", ready, summary, err)
					}
				})
			}
		})
	}
}

func TestRancherLocalUpgradeControllerExistingRollout(t *testing.T) {
	deployments := readyDeploymentsFixture()
	deployments[4].Status.UpdatedReplicas = 0
	kc := installSystemKubectlFixture(t, deployments, "")
	// Even when the current driver would not cause a new install, an existing
	// controller's incomplete rollout must hold up readiness.
	if err := os.WriteFile(filepath.Join(filepath.Dir(kc), "cluster"), []byte(`{"status":{"driver":"imported"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(kc), "setting"), []byte(`{"value":"false"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ready, summary, err := new(rancherSystemReadiness).deploymentsReady(kc)
	if err != nil || ready || !strings.Contains(summary, upgradeDeployment+" ready=1/1 updated=0") {
		t.Fatalf("got ready=%t summary=%q err=%v", ready, summary, err)
	}
}

func TestRancherSystemReadinessStaleAssets(t *testing.T) {
	d := readyDeploymentsFixture()[:2] // webhook has already recovered
	const logs = `2026/11/12 20:36:58 [ERROR] Failed to install system chart fleet-crd (release name: fleet-crd), will retry: specified version 111.0.0+up0.17.0-rc.2 doesn't exist in the index
2026/11/12 20:36:58 [ERROR] Failed to install system chart rancher-turtles (release name: rancher-turtles), will retry: specified version 111.0.0+up0.28.0-rc.5 doesn't exist in the index
2026/11/12 20:36:58 [ERROR] Failed to install system chart rancher-webhook (release name: rancher-webhook), will retry: specified version 111.0.0+up0.12.1-rc.17 doesn't exist in the index
unrelated password=must-never-appear`
	kc := installSystemKubectlFixture(t, d, logs)
	writeChartFixture(t, kc, "catalog", `{"default":"external","value":"bundled"}`)
	writeChartFixture(t, kc, "repo", runtimeChartRepoFixture)
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

const runtimeChartRepoFixture = `{
	"metadata":{"generation":1},
	"spec":{"gitRepo":"https://git.rancher.io/charts","gitBranch":"dev-v2.16"},
	"status":{"observedGeneration":1,"branch":"dev-v2.16","url":"https://git.rancher.io/charts",
		"commit":"5d3ae9ecd269bb9ae1cbbe3b72506ad4b5fa618c","conditions":[{"type":"Downloaded","status":"True"}]}
}`

const fleetCRDRetryLog = "Failed to install system chart fleet-crd (release name: fleet-crd), will retry: specified version 111.0.0+up0.17.0-rc.5 doesn't exist in the index"

func writeChartFixture(t *testing.T, kc, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(kc), name), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRancherSystemReadinessCatalogRefresh(t *testing.T) {
	const pin = "fleet-crd@111.0.0+up0.17.0-rc.5"
	for _, tc := range []struct{ name, mode, repo, diagnostic string }{
		{"online bootstrap", `{"default":"external"}`, runtimeChartRepoFixture, `rancher-charts branch="dev-v2.16" commit="5d3ae9ecd269bb9ae1cbbe3b72506ad4b5fa618c"`},
		{"unknown mode", "", runtimeChartRepoFixture, "catalog mode=unknown"},
		{"repo unavailable", `{"value":"bundled"}`, "", "rancher-charts status unavailable"},
		{"custom repo", `{"value":"bundled"}`, strings.ReplaceAll(runtimeChartRepoFixture, "git.rancher.io", "mirror.example.test"), "catalog mode=bundled"},
		{"bundle still reconciling", `{"value":"bundled"}`, strings.Replace(runtimeChartRepoFixture, `"observedGeneration":1`, `"observedGeneration":0`, 1), "catalog mode=bundled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			all := readyDeploymentsFixture()
			kc := installSystemKubectlFixture(t, all[:2], fleetCRDRetryLog)
			writeChartFixture(t, kc, "catalog", tc.mode)
			writeChartFixture(t, kc, "repo", tc.repo)
			var charts rancherSystemReadiness
			ready, summary, err := charts.deploymentsReady(kc)
			if ready || err != nil || !strings.Contains(summary, pin) || !strings.Contains(summary, tc.diagnostic) {
				t.Fatalf("initial index: ready=%t summary=%q err=%v", ready, summary, err)
			}
			writeChartFixture(t, kc, "logs", "")
			// The repo can advance between the log observation and the deadline.
			const newRevision = "28a8008e7e80abcbfcdc64fd7b3d8494d8c109dc"
			writeChartFixture(t, kc, "repo", strings.Replace(runtimeChartRepoFixture, "5d3ae9ecd269bb9ae1cbbe3b72506ad4b5fa618c", newRevision, 1))
			ready, summary, err = charts.deploymentsReady(kc)
			timeout := charts.timeoutSummary(kc, summary)
			if ready || err != nil || !strings.Contains(timeout, pin) || !strings.Contains(timeout, newRevision) {
				t.Fatalf("no recent logs: ready=%t summary=%q err=%v", ready, timeout, err)
			}
			raw, err := json.Marshal(map[string]any{"items": all})
			if err != nil {
				t.Fatal(err)
			}
			writeChartFixture(t, kc, "deployments", string(raw))
			writeChartFixture(t, kc, "logs", fleetCRDRetryLog)
			ready, summary, err = charts.deploymentsReady(kc)
			if !ready || err != nil || strings.Contains(charts.timeoutSummary(kc, summary), pin) {
				t.Fatalf("after rollout: ready=%t summary=%q err=%v", ready, summary, err)
			}
		})
	}
}

func TestRancherSystemReadinessPersistentMissBlocksProvisioning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	kc := installSystemKubectlFixture(t, readyDeploymentsFixture()[:2], fleetCRDRetryLog)
	writeChartFixture(t, kc, "catalog", `{"default":"external"}`)
	writeChartFixture(t, kc, "repo", strings.Replace(runtimeChartRepoFixture, `"status":"True"`, `"status":"False","reason":"DownloadFailed","message":"private diagnostic"`, 1))
	t.Setenv(haOutputRootEnv, filepath.Dir(kc))
	if err := os.Mkdir(haInstanceDir(1), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(kc, filepath.Join(haInstanceDir(1), "kube_config.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, wait := range []func() error{
		func() error {
			return waitForHAReady(1, map[string]string{"ha_1_rancher_url": server.URL}, 250*time.Millisecond, 0, 0)
		},
		func() error { return waitForHostedTenantRancherStable(server.URL, kc, 250*time.Millisecond) },
	} {
		err := wait()
		if err == nil {
			t.Fatal("healthy HTTP allowed a missing fleet controller through readiness")
		}
		for _, want := range []string{"timed out", "root=200 api=401", "fleet-crd@111.0.0+up0.17.0-rc.5", `reason="DownloadFailed"`} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("missing %q in %v", want, err)
			}
		}
		if strings.Contains(err.Error(), "private diagnostic") {
			t.Fatalf("unexpected raw catalog condition message in %v", err)
		}
	}
}
