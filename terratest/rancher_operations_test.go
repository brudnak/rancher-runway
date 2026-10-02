package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateUpgradeStep(t *testing.T) {
	for _, tt := range []struct {
		from, to           string
		experimental, want bool
	}{
		{"v2.15.2", "v2.15.3", false, true}, {"v2.15.2", "v2.16.0", false, true},
		{"v2.15.2", "v2.15.3-head", true, true}, {"v2.15.2", "v2.16.0-alpha1", true, true},
		{"v2.15.2", "v2.16.0-head", true, true}, {"v2.15.2", "v2.17.0", true, false},
		{"v2.15.2", "v2.14.9", true, false}, {"v2.15.2", "v2.15.1", true, false},
		{"v2.15.2", "v2.15.2", false, false}, {"v2.15.2", "v2.16.0-rc1", false, false},
		{"head", "v2.16.0", true, false}, {"v2.15.2", "head", true, false},
	} {
		t.Run(tt.from+"-"+tt.to, func(t *testing.T) {
			err := validateUpgradeStep(tt.from, tt.to, tt.experimental)
			if (err == nil) != tt.want {
				t.Fatalf("got %v; allowed=%v", err, tt.want)
			}
		})
	}
}
func TestDeployedMachineValidation(t *testing.T) {
	fields := map[string]deployedMachineField{"region": {Type: "string", Required: true, Options: []string{"us-east"}}, "quantity": {Type: "int"}, "labels": {Type: "map[string]", Required: true}}
	good := map[string]any{"region": "us-east", "quantity": float64(3), "labels": map[string]any{"owner": "qa"}}
	if err := validateDeployedMachine(good, fields); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{{"region": "other"}, {"metadata": map[string]any{}}, {"region": "us-east", "quantity": 1.5}, {"region": true}, {"quantity": float64(3)}} {
		if validateDeployedMachine(bad, fields) == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
}
func TestDeployedDownstreamPayloads(t *testing.T) {
	for _, provider := range []string{"linode", "amazonec2"} {
		in := rancherOperationRequest{Provider: provider, Name: "qa-cluster", KubernetesVersion: "v1.34.1+rke2r1", Quantity: 3, Machine: map[string]any{"region": "us-east"}}
		cluster := downstreamClusterPayload(in, "cattle-global-data:cc-test", "machine-test")
		spec := cluster["spec"].(map[string]any)
		if spec["cloudCredentialSecretName"] != "cattle-global-data:cc-test" || spec["kubernetesVersion"] != in.KubernetesVersion {
			t.Fatal(spec)
		}
		pool := spec["rkeConfig"].(map[string]any)["machinePools"].([]any)[0].(map[string]any)
		want := "LinodeConfig"
		if provider == "amazonec2" {
			want = "Amazonec2Config"
		}
		if pool["machineConfigRef"].(map[string]any)["kind"] != want || pool["quantity"] != 3 {
			t.Fatal(pool)
		}
		if downstreamMachinePayload(in, "machine-test")["region"] != "us-east" {
			t.Fatal("machine fields lost")
		}
	}
}
func TestDeployedEnvironmentCredentials(t *testing.T) {
	t.Setenv("LINODE_TOKEN", "linode-test-secret")
	t.Setenv("AWS_ACCESS_KEY_ID", "aws-test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	got, err := deployedEnvironmentCredentials("linode")
	if err != nil || got["token"] != "linode-test-secret" {
		t.Fatalf("%v %v", got, err)
	}
	got, err = deployedEnvironmentCredentials("amazonec2")
	if err != nil || got["secretKey"] != "aws-test-secret" {
		t.Fatalf("%v", err)
	}
	t.Setenv("AWS_SESSION_TOKEN", "temporary-secret")
	if _, err = deployedEnvironmentCredentials("amazonec2"); err == nil || strings.Contains(err.Error(), "temporary-secret") {
		t.Fatal("must reject unsupported session credentials without leaking them")
	}
}
func TestRancherOperationHistoryAndExclusion(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
	p := &localControlPanel{token: "test-panel-token"}
	if err := p.reserveRancherOperation("cluster", "op1"); err != nil {
		t.Fatal(err)
	}
	if err := p.reserveRancherOperation("cluster", "op2"); err == nil {
		t.Fatal("concurrent operation accepted")
	}
	if !p.anyOperationRunning() {
		t.Fatal("operation should block destructive lifecycle work")
	}
	p.mu.Lock()
	conflict := p.conflictingOperationRunningLocked(panelOperationCleanup)
	p.mu.Unlock()
	if !conflict {
		t.Fatal("cleanup was not blocked")
	}
	record := &rancherOperationRecord{ID: operationID(), ClusterID: "cluster", Kind: "upgrade", Status: "running", Started: time.Now().UTC(), Plan: &rancherUpgradePlan{kubeconfig: "PRIVATE_KUBECONFIG", archive: []byte("PRIVATE_CHART")}}
	if err := p.operationEvent(record, "Started"); err != nil {
		t.Fatal(err)
	}
	p.releaseRancherOperation("cluster")
	req := httptest.NewRequest(http.MethodGet, "/api/rancher/operations?clusterId=cluster", nil)
	req.Header.Set("X-Control-Panel-Token", p.token)
	w := httptest.NewRecorder()
	p.handleRancherOperations(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "interrupted") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "PRIVATE_") {
		t.Fatal("private execution material leaked")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/rancher/operations?clusterId=cluster", nil)
	w = httptest.NewRecorder()
	p.handleRancherOperations(w, req)
	if w.Code != 403 {
		t.Fatalf("unauthorized request: %d", w.Code)
	}
}
func TestDeployedRancherNoSecretErrorsOrRedirects(t *testing.T) {
	redirected := false
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, 302)
			return
		}
		http.Error(w, "secret-token and provider-secret", 500)
	}))
	defer server.Close()
	client, err := deployedRancherClient(rancherOperationRequest{Token: "secret-token", Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	for _, path := range []string{"/failure", "/redirect"} {
		err := deployedRancherJSON(context.Background(), client, server.URL, "secret-token", "GET", path, nil, nil)
		if err == nil || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "provider-secret") {
			t.Fatalf("unsafe error %v", err)
		}
	}
	if redirected {
		t.Fatal("credentials followed redirect")
	}
	if err := deployedRancherJSON(context.Background(), client, "http://example.com", "secret-token", "GET", "/", nil, nil); err == nil {
		t.Fatal("plaintext credentials allowed")
	}
}
func TestDeployedDownstreamLiveOptionsAndExecution(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
	var mu sync.Mutex
	writes := []string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer rancher-test-secret" {
			t.Error("missing auth")
			w.WriteHeader(401)
			return
		}
		if r.Method == "POST" {
			mu.Lock()
			writes = append(writes, r.URL.Path)
			mu.Unlock()
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if r.URL.Path == "/v3/cloudcredentials" && body["linodecredentialConfig"].(map[string]any)["token"] != "linode-test-secret" {
				t.Error("credential not passed")
			}
			fmt.Fprint(w, `{"id":"cattle-global-data:cc-created"}`)
			return
		}
		switch r.URL.Path {
		case "/v3/settings/machine-provision-image":
			fmt.Fprint(w, `{"value":"rancher/machine:test"}`)
		case "/v3/settings/system-default-registry":
			fmt.Fprint(w, `{"value":"registry.rancher.com"}`)
		case "/v1-rke2-release/releases":
			fmt.Fprint(w, `{"data":[{"id":"v1.34.1+rke2r1","version":"v1.34.1+rke2r1"}]}`)
		case "/v1/schemas/rke-machine-config.cattle.io.linodeconfig":
			fmt.Fprint(w, `{"resourceFields":{"region":{"type":"string","create":true,"required":true,"options":["us-east"]},"metadata":{"type":"map","create":true}}}`)
		case "/v3/cloudcredentials":
			fmt.Fprint(w, `{"data":[{"id":"cc-existing","name":"existing","linodecredentialConfig":{"token":"DO-NOT-RETURN"}}]}`)
		case "/v1/provisioning.cattle.io.cluster/fleet-default/qa-cluster":
			mu.Lock()
			exists := len(writes) > 0
			mu.Unlock()
			if !exists {
				w.WriteHeader(404)
				return
			}
			fmt.Fprint(w, `{"status":{"clusterName":"c-m-test","conditions":[{"type":"Ready","status":"True","message":"DO-NOT-LOG"}]}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cluster := clusterView{ID: "management", RancherURL: server.URL}
	in := rancherOperationRequest{Provider: "linode", Distribution: "rke2", Token: "rancher-test-secret", Insecure: true, Name: "qa-cluster", Quantity: 1, KubernetesVersion: "v1.34.1+rke2r1", Credentials: map[string]string{"token": "linode-test-secret"}, Machine: map[string]any{"region": "us-east"}}
	catalog, err := deployedDownstreamOptions(context.Background(), cluster, in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(catalog)
	if strings.Contains(string(raw), "DO-NOT-RETURN") {
		t.Fatal("credential value leaked")
	}
	if _, ok := catalog.Fields["metadata"]; ok {
		t.Fatal("reserved field exposed")
	}
	p := &localControlPanel{}
	record := &rancherOperationRecord{ID: operationID(), ClusterID: cluster.ID, Kind: "downstream", Status: "running"}
	if err := p.runDeployedDownstream(cluster, in, record, downstreamProvisioningRuntime{inspect: func(context.Context, string) (imagelookup.Provenance, bool, error) {
		return imagelookup.Provenance{Digest: "sha256:verified"}, true, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if len(record.Resources) != 3 {
		t.Fatalf("missing resource trail: %v", record.Resources)
	}
	saved, err := os.ReadFile(filepath.Join(p.rancherOperationsDir(), record.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"rancher-test-secret", "linode-test-secret", "DO-NOT-LOG"} {
		if strings.Contains(string(saved), secret) {
			t.Fatalf("history leaked %s", secret)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(writes, ",") != "/v3/cloudcredentials,/v1/rke-machine-config.cattle.io.linodeconfigs,/v1/provisioning.cattle.io.cluster" {
		t.Fatal(writes)
	}
}
func TestInspectInstalledRancherUsesLiveVersionAndReadiness(t *testing.T) {
	bin := t.TempDir()
	helm := `#!/bin/sh
printf '%s' '[{"chart":"rancher-2.15.0","app_version":"v2.15.0","revision":"4","status":"deployed"}]'
`
	kubectl := `#!/bin/sh
case "$*" in
 *deployment*) printf '%s' '{"metadata":{"generation":3},"spec":{"replicas":1,"template":{"spec":{"containers":[{"name":"rancher","image":"rancher/rancher:v2.15.2"}]}}},"status":{"observedGeneration":3,"readyReplicas":1,"updatedReplicas":1,"availableReplicas":1}}';;
 *server-version*) printf '%s' '{"default":"v2.15.2"}';;
 *) printf '%s' '{"items":[]}';;
esac
`
	for name, script := range map[string]string{"helm": helm, "kubectl": kubectl} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	installed, err := inspectInstalledRancher(context.Background(), "test-kubeconfig")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "v2.15.2" || installed.Revision != 4 {
		t.Fatalf("used stale chart appVersion: %#v", installed)
	}
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(strings.ReplaceAll(kubectl, `"readyReplicas":1`, `"readyReplicas":0`)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectInstalledRancher(context.Background(), "test-kubeconfig"); err == nil {
		t.Fatal("unready deployment accepted")
	}
}

func TestRancherUpgradeExecutionBoundaries(t *testing.T) {
	for _, scenario := range []string{"success", "minor-head-success", "digest-drift", "dry-run-failure", "upgrade-failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
			p := &localControlPanel{}
			plan := &rancherUpgradePlan{Image: "rancher/rancher", ImageTag: "v2.15.3", Digest: "server-digest", AgentImage: "rancher/rancher-agent:v2.15.3", AgentDigest: "agent-digest", TargetVersion: "v2.15.3", kubeconfig: "test", archive: []byte("chart")}
			if scenario == "minor-head-success" {
				plan.ImageTag = "v2.15-head"
				plan.AgentImage = "rancher/rancher-agent:v2.15-head"
				plan.TargetVersion = "v2.15-19c92983f6f9d7f455de668e62fbfe55c045cde2-head"
			}
			record := &rancherOperationRecord{ID: operationID(), ClusterID: "management", Status: "running"}
			var commands []string
			var mu sync.Mutex
			runtime := rancherUpgradeRuntime{
				inspectImage: func(ctx context.Context, ref string) (imagelookup.Provenance, bool, error) {
					digest := plan.Digest
					if strings.Contains(ref, "rancher-agent") {
						digest = plan.AgentDigest
					}
					if scenario == "digest-drift" {
						digest = "changed"
					}
					return imagelookup.Provenance{Digest: digest}, true, nil
				},
				command: func(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
					joined := strings.Join(args, " ")
					mu.Lock()
					commands = append(commands, joined)
					mu.Unlock()
					if strings.Contains(joined, "get settings.management.cattle.io") {
						return []byte(`{"value":"docker.io"}`), nil
					}
					if strings.Contains(joined, "get values") {
						return []byte(`{"extraEnv":[{"name":"OTHER","value":"preserved"},{"name":"CATTLE_AGENT_IMAGE","value":"old-agent"}]}`), nil
					}
					for i, arg := range args {
						if arg == "-f" {
							raw, err := os.ReadFile(args[i+1])
							if err != nil {
								t.Error(err)
							}
							if !strings.Contains(string(raw), "preserved") || strings.Contains(string(raw), "old-agent") || !strings.Contains(string(raw), plan.AgentImage) {
								t.Errorf("invalid merged values: %s", raw)
							}
						}
					}
					dry := strings.Contains(joined, "--dry-run=server")
					if (scenario == "dry-run-failure" && dry) || (scenario == "upgrade-failure" && !dry) {
						return nil, fmt.Errorf("fixture command failed")
					}
					return nil, nil
				},
				installed: func(context.Context, string) (rancherInstalledState, error) {
					return rancherInstalledState{Version: plan.TargetVersion, Image: plan.Image + ":" + plan.ImageTag}, nil
				},
				pods: func(string) ([]podView, error) {
					return []podView{{Name: "new-pod", Ready: "1/1", Status: "Running"}}, nil
				},
			}
			err := p.runRancherUpgrade(record, plan, runtime)
			if (err == nil) != (scenario == "success" || scenario == "minor-head-success") {
				t.Fatalf("scenario %s: %v", scenario, err)
			}
			mu.Lock()
			defer mu.Unlock()
			joined := strings.Join(commands, "\n")
			if strings.Contains(joined, "rollback") {
				t.Fatal("unsafe automatic rollback")
			}
			if scenario == "digest-drift" && len(commands) != 0 {
				t.Fatal("commands executed after digest drift")
			}
			if scenario == "dry-run-failure" && strings.Contains(joined, "--wait-for-jobs") {
				t.Fatal("mutation followed failed dry-run")
			}
			if scenario == "success" || scenario == "minor-head-success" || scenario == "upgrade-failure" {
				expected := 4
				if scenario == "upgrade-failure" {
					expected = 3
				}
				if len(commands) != expected || !strings.Contains(commands[1], "--dry-run=server") || !strings.Contains(commands[2], "--wait-for-jobs") {
					t.Fatal(commands)
				}
			}
		})
	}
}
