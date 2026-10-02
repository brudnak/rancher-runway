package test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestClusterHistoryObservationsAndPermanentDeletion(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "create", Name: "History"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := p.labClusterCandidates()
	id := rows[0].ID
	details := clusterDeploymentDetailsResponse{ClusterID: id, ConfiguredVersion: "head", RancherVersion: "v2.16.0-alpha1", WebhookChartVersion: "104.0.1", Warnings: []string{"token=supersecret"}, Images: []clusterDeploymentImage{{DeclaredImage: "rancher/rancher:head", Digest: "sha256:exact"}}}
	p.retainDeploymentDetails(details)
	p.retainDeploymentDetails(details)
	details.WebhookChartVersion = "104.0.2"
	p.retainDeploymentDetails(details)
	reopened := &localControlPanel{}
	events, err := reopened.readClusterHistory(id)
	if err != nil || len(events) != 2 {
		t.Fatalf("history lost or duplicated: %+v %v", events, err)
	}
	raw, _ := json.Marshal(events)
	if strings.Contains(string(raw), "supersecret") || !strings.Contains(string(raw), "sha256:exact") || !strings.Contains(string(raw), "104.0.1") || !strings.Contains(string(raw), "104.0.2") {
		t.Fatalf("provenance/redaction failed: %s", raw)
	}
	purge := clusterHistoryRequest{Action: "purge", ID: id, ConfirmID: id, ConfirmPhrase: "DELETE PERMANENTLY"}
	if err = p.mutateClusterHistory(purge); err == nil {
		t.Fatal("purge outside trash accepted")
	}
	if err = p.mutateClusterHistory(clusterHistoryRequest{Action: "delete", ID: id}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []clusterHistoryRequest{{Action: "purge", ID: id}, {Action: "purge", ID: id, ConfirmID: id}, {Action: "purge", ID: id, ConfirmPhrase: "DELETE PERMANENTLY"}} {
		if p.mutateClusterHistory(bad) == nil {
			t.Fatal("missing confirmation accepted")
		}
	}
	if err = p.mutateClusterHistory(purge); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(p.clusterHistoryDir(id)); !os.IsNotExist(err) {
		t.Fatalf("archive retained: %v", err)
	}
	p.retainDeploymentDetails(details)
	p.rememberClusterSnapshot([]clusterView{{ID: id, Name: "Stale discovery", Version: "head"}})
	if _, err = os.Stat(p.clusterHistoryDir(id)); !os.IsNotExist(err) {
		t.Fatal("late probe recreated purged history")
	}
	if p.mutateClusterHistory(clusterHistoryRequest{Action: "restore", ID: id}) == nil {
		t.Fatal("purged history restored")
	}
	p.clusterWorkspacesMu.Lock()
	record := p.clusterWorkspaces[id]
	p.clusterWorkspacesMu.Unlock()
	if record.Name != "" || record.Version != "" || !record.Purged {
		t.Fatalf("purge retained personal metadata: %+v", record)
	}
}
func TestClusterHistoryExportAuthorization(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.mutateClusterHistory(clusterHistoryRequest{Action: "create", Name: "Saved"})
	rows, _ := p.labClusterCandidates()
	id := rows[0].ID
	p.saveClusterHistory(id, "test", map[string]string{"version": "v2.15.2"})
	request := httptest.NewRequest("GET", "/api/cluster-history?cluster="+id, nil)
	response := httptest.NewRecorder()
	p.handleClusterHistory(response, request)
	if response.Code != 403 {
		t.Fatal("unauthorized export")
	}
	request.Header.Set("X-Control-Panel-Token", p.token)
	response = httptest.NewRecorder()
	p.handleClusterHistory(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "v2.15.2") {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestClusterHistoryRepeatedOperations(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "create", Name: "Upgraded"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := p.labClusterCandidates()
	id := rows[0].ID
	for _, op := range []rancherOperationRecord{
		{ID: "one", ClusterID: id, Kind: "upgrade", Status: "succeeded", From: "v2.15.2", To: "v2.15.3"},
		{ID: "two", ClusterID: id, Kind: "upgrade", Status: "failed", From: "v2.15.3", To: "v2.16.0"},
		{ID: "three", ClusterID: id, Kind: "upgrade", Status: "running", From: "v2.15.3", To: "head"},
		{ID: "four", ClusterID: id, Kind: "upgrade", Status: "running", From: "v2.15.3", To: "v2.16-head"},
	} {
		if err := p.saveRancherOperation(&op); err != nil {
			t.Fatal(err)
		}
	}
	p.rancherOps.active = map[string]string{id: "four"}
	request := httptest.NewRequest("GET", "/api/cluster-history?cluster="+id, nil)
	request.Header.Set("X-Control-Panel-Token", p.token)
	response := httptest.NewRecorder()
	p.handleClusterHistory(response, request)
	var archive struct {
		Operations []rancherOperationRecord `json:"operations"`
	}
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &archive); err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, op := range archive.Operations {
		statuses[op.ID] = op.Status
	}
	if len(statuses) != 4 || statuses["one"] != "succeeded" || statuses["two"] != "failed" || statuses["three"] != "interrupted" || statuses["four"] != "running" {
		t.Fatalf("operation history lost or misclassified: %v", statuses)
	}
}
