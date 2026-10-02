package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

func TestDeployedProvisionerPreflight(t *testing.T) {
	for _, result := range []string{"found", "missing", "auth"} {
		t.Run(result, func(t *testing.T) {
			call := func(method, path string, payload, out any) error {
				if method != "GET" {
					t.Fatal("preflight wrote resources")
				}
				raw := `{"value":"registry.rancher.com"}`
				if strings.HasSuffix(path, "machine-provision-image") {
					raw = `{"value":"rancher/machine:v0.15.0-rancher146-rc.5"}`
				}
				return json.Unmarshal([]byte(raw), out)
			}
			message, err := checkDownstreamProvisioner(context.Background(), call, func(ctx context.Context, ref string) (imagelookup.Provenance, bool, error) {
				if ref != "registry.rancher.com/rancher/machine:v0.15.0-rancher146-rc.5" {
					t.Fatal(ref)
				}
				if result == "auth" {
					return imagelookup.Provenance{}, false, fmt.Errorf("DO-NOT-LOG")
				}
				return imagelookup.Provenance{Digest: "sha256:abc"}, result == "found", nil
			})
			if (err == nil) != (result == "found") {
				t.Fatal(message, err)
			}
			if err != nil && (!strings.Contains(err.Error(), "No resources were created") || strings.Contains(err.Error(), "DO-NOT-LOG")) {
				t.Fatal(err)
			}
		})
	}
}

func TestDeployedProvisioningPodDiagnostics(t *testing.T) {
	for _, reason := range []string{"ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "CrashLoopBackOff", "ContainerCreating", "terminated", "unrelated"} {
		t.Run(reason, func(t *testing.T) {
			command := func(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, "get ") {
					t.Fatal("diagnostics attempted mutation")
				}
				if strings.Contains(joined, "machines.cluster.x-k8s.io") {
					if !strings.Contains(joined, "cluster.x-k8s.io/cluster-name=qa") {
						t.Fatal(joined)
					}
					return []byte(`{"items":[{"metadata":{"name":"qa-machine"}}]}`), nil
				}
				machine := "qa-machine"
				if reason == "unrelated" {
					machine = "different-cluster"
				}
				state := map[string]any{"waiting": map[string]any{"reason": reason, "message": "DO-NOT-LOG"}}
				if reason == "terminated" {
					state = map[string]any{"terminated": map[string]any{"exitCode": 1, "message": "DO-NOT-LOG"}}
				}
				return json.Marshal(map[string]any{"items": []any{map[string]any{"metadata": map[string]any{"name": "provisioner", "labels": map[string]string{"rke.cattle.io/capi-machine-name": machine}}, "status": map[string]any{"phase": "Pending", "containerStatuses": []any{map[string]any{"state": state}}}}}})
			}
			message, err := observeDownstreamProvisioning(context.Background(), "fixture", "qa", command)
			blocked := reason != "ContainerCreating" && reason != "unrelated"
			if (err != nil) != blocked {
				t.Fatal(message, err)
			}
			if strings.Contains(fmt.Sprint(message, err), "DO-NOT-LOG") {
				t.Fatal("raw provider message leaked")
			}
		})
	}
}

func TestDeployedProvisionerPreflightStopsBeforeWrites(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
	writes := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			w.WriteHeader(500)
			return
		}
		if strings.Contains(r.URL.Path, "provisioning.cattle.io.cluster/") {
			w.WriteHeader(404)
			return
		}
		if strings.HasSuffix(r.URL.Path, "machine-provision-image") {
			fmt.Fprint(w, `{"value":"rancher/machine:missing"}`)
			return
		}
		fmt.Fprint(w, `{"value":"registry.rancher.com"}`)
	}))
	defer server.Close()
	p := &localControlPanel{}
	err := p.runDeployedDownstream(clusterView{RancherURL: server.URL}, rancherOperationRequest{Name: "qa", Insecure: true}, &rancherOperationRecord{ID: operationID()}, downstreamProvisioningRuntime{inspect: func(context.Context, string) (imagelookup.Provenance, bool, error) {
		return imagelookup.Provenance{}, false, nil
	}})
	if err == nil || writes != 0 {
		t.Fatal("missing provisioner must prevent every resource write", err, writes)
	}
}

func TestDeployedProvisioningPodDiagnosticsIgnoresRetriedPod(t *testing.T) {
	command := func(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "machines.cluster.x-k8s.io") {
			return []byte(`{"items":[{"metadata":{"name":"qa-machine"}}]}`), nil
		}
		return []byte(`{"items":[
   {"metadata":{"name":"new","creationTimestamp":"2026-10-02T02:00:00Z","labels":{"rke.cattle.io/capi-machine-name":"qa-machine"}},"status":{"phase":"Running"}},
   {"metadata":{"name":"old","creationTimestamp":"2026-10-02T01:00:00Z","labels":{"rke.cattle.io/capi-machine-name":"qa-machine"}},"status":{"phase":"Failed","containerStatuses":[{"state":{"terminated":{"exitCode":1}}}]}}
  ]}`), nil
	}
	message, err := observeDownstreamProvisioning(context.Background(), "fixture", "qa", command)
	if err != nil || message != "Provisioner pods: new=Running" {
		t.Fatal(message, err)
	}
}
