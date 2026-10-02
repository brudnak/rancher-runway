package test

import (
	"context"
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func regressionWrite(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func regressionHistory(t *testing.T, p *localControlPanel) string {
	t.Helper()
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "create", Name: "Regression evidence", Version: "v2.15.3-head"}); err != nil {
		t.Fatal(err)
	}
	rows, err := p.labClusterCandidates()
	if err != nil || len(rows) != 1 {
		t.Fatalf("history setup failed: %v", err)
	}
	return rows[0].ID
}
func TestRegressionMigrationResumesAndPreservesLinkedEvidence(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	t.Setenv(runIDEnv, "")
	manifest, _ := json.Marshal(clusterWorkspaceManifest{Version: 1, Clusters: []clusterWorkspaceRecord{{ID: "original", Name: "Retained", Milestone: "v2.16.0", Version: "v2.15.3-head"}}})
	files := map[string][]byte{
		"cluster-workspaces.json":                   manifest,
		"cluster-history/original/observation.json": []byte(`{"clusterId":"original","digest":"sha256:exact"}`),
		"rancher-operations/upgrade.json":           []byte(`{"clusterId":"original","from":"v2.15.2","to":"v2.15.3-head"}`),
		"test-lab/library.json":                     []byte(`{"runs":[{"id":"run-original","clusterId":"original"}]}`),
		"cache-lab/snapshot/data.db":                {0, 1, 2, 255, 42},
		"test-packages/package/evidence.json":       []byte(`{"runId":"run-original","clusterId":"original"}`),
	}
	for name, data := range files {
		regressionWrite(t, filepath.Join(automationOutputDir(), "control-panel", name), data)
	}
	// Mimic an interruption between completed directory renames.
	if err := migrateDurableEntry("cluster-workspaces.json"); err != nil {
		t.Fatal(err)
	}
	if err := migrateDurableEntry("cluster-history"); err != nil {
		t.Fatal(err)
	}
	if err := migrateDurableData(); err != nil {
		t.Fatal(err)
	}
	cleanupAutomationOutput()
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(durableDataDir(), name))
		if err != nil || string(got) != string(want) {
			t.Fatalf("migration changed %s: %v", name, err)
		}
	}
	rows, err := p.labClusterCandidates()
	if err != nil || len(rows) != 1 || rows[0].ID != "original" || rows[0].Milestone != "v2.16.0" {
		t.Fatalf("reopened identity changed: %+v %v", rows, err)
	}
}
func TestRegressionHistoryWriteFailureCanRetrySameObservation(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	id := regressionHistory(t, p)
	first := map[string]string{"version": "v2.15.2"}
	next := map[string]string{"version": "v2.15.3"}
	if err := p.saveClusterHistory(id, "deployment", first); err != nil {
		t.Fatal(err)
	}
	dir := p.clusterHistoryDir(id)
	backup := dir + ".held"
	if err := os.Rename(dir, backup); err != nil {
		t.Fatal(err)
	}
	regressionWrite(t, dir, []byte("filesystem obstruction"))
	if p.saveClusterHistory(id, "deployment", next) == nil {
		t.Fatal("failed write reported success")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, dir); err != nil {
		t.Fatal(err)
	}
	if err := p.saveClusterHistory(id, "deployment", next); err != nil {
		t.Fatal(err)
	}
	events, err := (&localControlPanel{}).readClusterHistory(id)
	if err != nil || len(events) != 2 {
		t.Fatalf("retry lost an observation: %d %v", len(events), err)
	}
}
func TestRegressionPartialPurgeCanRetryAfterRestart(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	id := regressionHistory(t, p)
	if err := p.saveClusterHistory(id, "deployment", map[string]string{"version": "v2.15.2"}); err != nil {
		t.Fatal(err)
	}
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "delete", ID: id}); err != nil {
		t.Fatal(err)
	}
	// Fail after the observation directory is deleted, before operation cleanup.
	regressionWrite(t, p.rancherOperationsDir(), []byte("filesystem obstruction"))
	req := clusterHistoryRequest{Action: "purge", ID: id, ConfirmID: id, ConfirmPhrase: "DELETE PERMANENTLY"}
	if p.mutateClusterHistory(req) == nil {
		t.Fatal("partial deletion reported success")
	}
	reopened := &localControlPanel{}
	if _, err := reopened.labClusterCandidates(); err != nil {
		t.Fatal(err)
	}
	record := reopened.clusterWorkspaces[id]
	if !record.Deleted || record.Purged {
		t.Fatal("failed deletion cannot be retried from Trash")
	}
	if err := os.Remove(p.rancherOperationsDir()); err != nil {
		t.Fatal(err)
	}
	other := &rancherOperationRecord{ID: operationID(), ClusterID: "other", Status: "succeeded"}
	if err := reopened.saveRancherOperation(other); err != nil {
		t.Fatal(err)
	}
	if err := reopened.mutateClusterHistory(req); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.rancherOperationsDir(), other.ID+".json")); err != nil {
		t.Fatal("unrelated evidence deleted", err)
	}
}
func TestRegressionWorkerRegistrationRacesShutdown(t *testing.T) {
	var workers panelWorkers
	var contenders sync.WaitGroup
	gate := make(chan struct{})
	release := make(chan struct{})
	for i := 0; i < 64; i++ {
		contenders.Add(1)
		go func() {
			defer contenders.Done()
			<-gate
			_, done, err := workers.Begin()
			if err == nil {
				<-release
				done()
				done()
			}
		}()
	}
	close(gate)
	workers.Stop()
	close(release)
	contenders.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := workers.Wait(ctx); err != nil {
		t.Fatal("worker registration leaked during shutdown", err)
	}
	if _, _, err := workers.Begin(); err == nil {
		t.Fatal("work accepted after shutdown")
	}
}
func TestRegressionShutdownDuringUpgradePersistsFailure(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	id := regressionHistory(t, p)
	plan := &rancherUpgradePlan{Image: "rancher/rancher", ImageTag: "v2.15.3", Digest: "server", AgentImage: "rancher/rancher-agent:v2.15.3", AgentDigest: "agent", archive: []byte("fixture")}
	record := &rancherOperationRecord{ID: operationID(), ClusterID: id, Kind: "upgrade", Status: "running", Started: time.Now(), From: "v2.15.2", To: "v2.15.3", Plan: plan}
	started := make(chan struct{})
	runtime := rancherUpgradeRuntime{
		inspectImage: func(_ context.Context, ref string) (imagelookup.Provenance, bool, error) {
			digest := "server"
			if strings.Contains(ref, "agent") {
				digest = "agent"
			}
			return imagelookup.Provenance{Digest: digest}, true, nil
		},
		command: func(ctx context.Context, _ []byte, _ string, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "get values") {
				return []byte(`{}`), nil
			}
			if strings.Contains(joined, "--dry-run=server") {
				return nil, nil
			}
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		pods: func(string) ([]podView, error) { return []podView{}, nil },
		installed: func(context.Context, string) (rancherInstalledState, error) {
			t.Error("readiness reported after cancellation")
			return rancherInstalledState{}, nil
		},
	}
	if err := p.reserveRancherOperation(id, record.ID); err != nil {
		t.Fatal(err)
	}
	if !p.workers.Start(func(context.Context) { p.finishRancherOperation(record, p.runRancherUpgrade(record, plan, runtime)) }) {
		t.Fatal("start rejected")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("mock upgrade did not start")
	}
	p.workers.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.workers.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(p.rancherOperationsDir(), record.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var restored rancherOperationRecord
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Status != "failed" || restored.Finished == nil || restored.From != "v2.15.2" || restored.To != "v2.15.3" {
		t.Fatalf("misleading final state: %+v", restored)
	}
	if len(p.rancherOps.active) != 0 {
		t.Fatal("operation lock retained after final write")
	}
}

func TestRegressionSecretsExcludedFromHistoryDiskAndExport(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	id := regressionHistory(t, p)
	private := t.TempDir()
	regressionWrite(t, filepath.Join(private, "install.sh"), []byte("#!/bin/sh\nhelm install rancher rancher-latest/rancher --version 2.15.3 --set bootstrapPassword=fixture-bootstrap-secret,replicas=3 --set-string registryPassword=fixture-registry-secret\n"))
	p.retainDiscoveredClusters([]clusterView{{ID: id, Name: "Fixture", Type: "local", Version: "v2.15.3", KubeconfigPath: filepath.Join(private, "config"), Error: "fixture-probe-secret"}})
	p.retainDeploymentDetails(clusterDeploymentDetailsResponse{ClusterID: id, RancherVersion: "v2.15.3", Warnings: []string{"fixture-warning-secret"}})
	op := &rancherOperationRecord{ID: operationID(), ClusterID: id, Kind: "upgrade", Status: "succeeded", Plan: &rancherUpgradePlan{Digest: "sha256:preserved", kubeconfig: filepath.Join(private, "config"), archive: []byte("fixture-chart-secret")}}
	if err := p.saveRancherOperation(op); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/cluster-history?cluster="+id, nil)
	request.Header.Set("X-Control-Panel-Token", p.token)
	response := httptest.NewRecorder()
	p.handleClusterHistory(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var disk strings.Builder
	if err := filepath.WalkDir(durableDataDir(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		disk.Write(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{disk.String(), response.Body.String()} {
		for _, secret := range []string{"fixture-bootstrap-secret", "fixture-registry-secret", "fixture-probe-secret", "fixture-warning-secret", "fixture-chart-secret", private} {
			if strings.Contains(text, secret) {
				t.Fatal("private value survived history persistence/export")
			}
		}
		for _, fact := range []string{"v2.15.3", "sha256:preserved", "helm install"} {
			if !strings.Contains(text, fact) {
				t.Fatalf("useful provenance was lost: %s", fact)
			}
		}
	}
}
