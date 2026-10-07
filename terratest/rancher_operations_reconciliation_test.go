package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

func TestDownstreamReconciliationReleasesLifecycleLock(t *testing.T) {
	for _, scenario := range []string{"deleted", "deleting", "replaced", "different-owner", "transient-not-found", "server-error"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
			panel := newCleanupBatchTestPanel(t, panelRunRecord{RunID: "run-a", Status: "ready"})
			created := false
			polls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					if r.URL.Path == "/v1/provisioning.cattle.io.cluster" {
						created = true
					}
					fmt.Fprint(w, `{"id":"created","metadata":{"uid":"original"}}`)
					return
				}
				switch r.URL.Path {
				case "/v3/settings/machine-provision-image":
					fmt.Fprint(w, `{"value":"rancher/machine:test"}`)
				case "/v3/settings/system-default-registry":
					fmt.Fprint(w, `{"value":""}`)
				case "/v1/provisioning.cattle.io.cluster/fleet-default/qa":
					if !created {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					polls++
					switch {
					case scenario == "deleted":
						w.WriteHeader(http.StatusNotFound)
					case scenario == "deleting":
						fmt.Fprint(w, `{"metadata":{"deletionTimestamp":"2026-10-07T20:00:00Z"}}`)
					case scenario == "replaced":
						fmt.Fprint(w, `{"metadata":{"uid":"replacement"},"status":{"clusterName":"c-new","conditions":[{"type":"Ready","status":"True"}]}}`)
					case scenario == "different-owner":
						fmt.Fprintf(w, `{"metadata":{"annotations":{%q:"another-operation"}}}`, deployedOwnerAnnotation)
					case scenario == "transient-not-found" && polls == 1:
						w.WriteHeader(http.StatusNotFound)
					case scenario == "server-error" && polls <= 3:
						w.WriteHeader(http.StatusServiceUnavailable)
					default:
						fmt.Fprint(w, `{"metadata":{"uid":"original"},"status":{"clusterName":"c-ready","conditions":[{"type":"Ready","status":"True"}]}}`)
					}
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			record := &rancherOperationRecord{ID: operationID(), ClusterID: "management", Kind: "downstream", Status: "running", Started: time.Now()}
			if err := panel.reserveRancherOperation(record.ClusterID, record.ID); err != nil {
				t.Fatal(err)
			}
			if err := panel.startCleanupBatch([]string{"run-a"}); err == nil || !strings.Contains(err.Error(), "downstream creation") {
				t.Fatalf("active creation must reject batch before starting: %v", err)
			}
			if snapshot := panel.snapshotCleanupBatch(); snapshot.Running || len(snapshot.Failures) > 0 {
				t.Fatalf("rejected batch changed state: %#v", snapshot)
			}
			if panel.runningOperationName() != "Rancher upgrade or downstream creation" {
				t.Fatal("quit should identify the active Rancher operation")
			}
			err := panel.runDeployedDownstream(clusterView{ID: record.ClusterID, RunID: "run-a", RancherURL: server.URL}, rancherOperationRequest{Name: "qa", Provider: "linode", CredentialID: "existing", Token: "fixture-token", Insecure: true}, record, downstreamProvisioningRuntime{inspect: func(context.Context, string) (imagelookup.Provenance, bool, error) {
				return imagelookup.Provenance{Digest: "sha256:test"}, true, nil
			}})
			wantSuccess := scenario == "transient-not-found" || scenario == "server-error"
			if (err == nil) != wantSuccess {
				t.Fatalf("reconciliation result: %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "stopped waiting") {
				t.Fatalf("failed before reconciling live state: %v", err)
			}
			panel.finishRancherOperation(record, err)
			if panel.anyOperationRunning() {
				t.Fatal("finished downstream still prevents quitting")
			}
			if len(record.Resources) != 2 || record.Finished == nil {
				t.Fatalf("missing completion or cleanup resource trail: %#v", record)
			}
			// Exercise batch startup after reconciliation without invoking cloud deletion.
			panel.cleanupBatchRunner = func(string) error { return nil }
			if err := panel.startCleanupBatch([]string{"run-a"}); err != nil {
				t.Fatalf("reconciled operation still blocks cleanup: %v", err)
			}
			if snapshot := waitForCleanupBatch(t, panel); len(snapshot.CompletedRunIDs) != 1 || snapshot.Error != "" {
				t.Fatalf("cleanup did not finish: %#v", snapshot)
			}
		})
	}
}
