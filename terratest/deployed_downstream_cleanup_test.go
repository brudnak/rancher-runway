package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeployedCleanupUIDAndWait(t *testing.T) {
	for _, scenario := range []string{"success", "replacement", "unowned", "inaccessible", "machines-remain", "invalid-machine-list"} {
		t.Run(scenario, func(t *testing.T) {
			deleted := false
			calls := 0
			record := deployedDownstreamRecord{OperationID: operationID(), Name: "qa-cluster", Namespace: "fleet-default", Kubeconfig: "fixture", UID: "original"}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			command := func(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
				calls++
				joined := strings.Join(args, " ")
				if scenario == "inaccessible" {
					return nil, fmt.Errorf("offline")
				}
				if strings.Contains(joined, "delete --raw") {
					var options struct {
						Preconditions map[string]string `json:"preconditions"`
						Propagation   string            `json:"propagationPolicy"`
					}
					if err := json.Unmarshal(input, &options); err != nil || options.Preconditions["uid"] != "original" || options.Propagation != "Foreground" {
						t.Fatal("missing UID protection", string(input), err)
					}
					deleted = true
					return []byte(`{}`), nil
				}
				if strings.Contains(joined, "machines.cluster.x-k8s.io") {
					if scenario == "invalid-machine-list" {
						return []byte(`{}`), nil
					}
					if scenario == "machines-remain" {
						return []byte(`{"items":[{}]}`), nil
					}
					return []byte(`{"items":[]}`), nil
				}
				if deleted {
					return nil, nil
				}
				uid := "original"
				if scenario == "replacement" {
					uid = "new"
				}
				return []byte(fmt.Sprintf(`{"metadata":{"uid":%q}}`, uid)), nil
			}
			if scenario == "unowned" {
				record.UID = ""
			}
			err := deleteDeployedDownstream(ctx, record, command)
			if (err == nil) != (scenario == "success") {
				t.Fatal(scenario, err)
			}
			if scenario != "success" && scenario != "machines-remain" && scenario != "invalid-machine-list" && deleted {
				t.Fatal("deleted unverified cluster")
			}
			if scenario == "success" && (!deleted || calls < 4) {
				t.Fatal("did not verify after deletion")
			}
		})
	}
}

func TestDeployedCleanupKubectlRawPreconditions(t *testing.T) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl unavailable")
	}
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/apis/provisioning.cattle.io/v1/namespaces/fleet-default/clusters/qa" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		var options struct {
			Preconditions map[string]string `json:"preconditions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&options); err != nil || options.Preconditions["uid"] != "exact-uid" {
			t.Error("precondition missing", err)
		}
		deleted = true
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Success"}`)
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "kubeconfig")
	config := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: fake\n  cluster:\n    server: %s\ncontexts:\n- name: fake\n  context:\n    cluster: fake\ncurrent-context: fake\n", server.URL)
	if err := os.WriteFile(file, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := operationCommand(ctx, []byte(`{"apiVersion":"v1","kind":"DeleteOptions","preconditions":{"uid":"exact-uid"}}`), "kubectl", "--kubeconfig", file, "delete", "--raw", "/apis/provisioning.cattle.io/v1/namespaces/fleet-default/clusters/qa", "-f", "-")
	if err != nil || !deleted {
		t.Fatal("kubectl did not send protected DELETE to fixture", err)
	}
}

func TestDeployedCleanupTrackingMultipleClusters(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
	for _, name := range []string{"first", "second"} {
		r := deployedDownstreamRecord{OperationID: operationID(), RunID: "run-a", ParentID: "parent", Name: name, Namespace: "fleet-default", Provider: "linode", CreatedAt: time.Now()}
		if err := saveDeployedDownstream(r); err != nil {
			t.Fatal(err)
		}
	}
	records, err := readDeployedDownstreams("run-a")
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	other, err := readDeployedDownstreams("run-b")
	if err != nil || len(other) != 0 {
		t.Fatal(other, err)
	}
}

func TestDeployedCleanupOlderOperationsRequireOwnership(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
	id := operationID()
	dir := durableDataPath("rancher-operations")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rancherOperationRecord{ID: id, Kind: "downstream", ClusterID: "run-run-a-ha-1-local", Resources: []string{"provisioning cluster fleet-default/qa-cluster"}})
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkUntrackedDeployedDownstreams("run-a"); err == nil {
		t.Fatal("older untracked downstream must block management destroy")
	}
	if err := checkUntrackedDeployedDownstreams("run-b"); err != nil {
		t.Fatal("unrelated run blocked", err)
	}
	now := time.Now()
	record := deployedDownstreamRecord{OperationID: id, RunID: "run-a", ParentID: "run-run-a-ha-1-local", Name: "qa-cluster", Namespace: "fleet-default", Provider: "linode", UID: "verified-uid", CreatedAt: now, DeletedAt: &now}
	if err := saveDeployedDownstream(record); err != nil {
		t.Fatal(err)
	}
	if err := checkUntrackedDeployedDownstreams("run-a"); err != nil {
		t.Fatal("completed cleanup must remain tracked", err)
	}
}
