package test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clusterWorkspaceTestPanel(t *testing.T) *localControlPanel {
	t.Helper()
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	t.Setenv("GITHUB_WORKSPACE", root)
	p := &localControlPanel{token: "workspace-token", testDir: root, repoRoot: root, operations: newPanelOperations(), clusterSnapshot: map[string]clusterView{}}
	t.Cleanup(func() { p.workers.Stop(); _ = p.workers.Wait(context.Background()) })
	return p
}

func TestClusterWorkspacesStableNicknameRestartAndArchive(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	original := clusterView{ID: "run-original-ha-1-local", RunID: "original", Name: "HA 1 Local", RancherURL: "https://RANCHER.test:443/", Version: "v2.15.2", Role: "local", KubeconfigPath: "/private/example.yaml"}
	p.writeRunRecord(panelRunRecord{RunID: "original", CreatedAt: time.Now()})
	p.rememberClusterSnapshot([]clusterView{original})
	if err := p.renameClusterWorkspace(original.ID, "My regression Rancher"); err != nil {
		t.Fatal(err)
	}
	original.Name = "Discovered name changed"
	p.rememberClusterSnapshot([]clusterView{original})
	rows, err := p.labClusterCandidates()
	if err != nil || len(rows) != 1 || rows[0].Nickname != "My regression Rancher" || rows[0].Name != original.Name || rows[0].ID != original.ID || rows[0].Version != "v2.15.2" || rows[0].Archived {
		t.Fatalf("metadata lost identity: %+v %v", rows, err)
	}
	p.removeRunRecord("original")
	reopened := &localControlPanel{}
	rows, err = reopened.labClusterCandidates()
	if err != nil || len(rows) != 1 || !rows[0].Archived || rows[0].Nickname != "My regression Rancher" || rows[0].URL != "https://rancher.test" {
		t.Fatalf("archive after reopen: %+v %v", rows, err)
	}
	info, err := os.Stat(p.clusterWorkspacesPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("manifest permissions: %v %v", info, err)
	}
	if err = reopened.renameClusterWorkspace(original.ID, ""); err != nil {
		t.Fatal(err)
	}
	rows, _ = reopened.labClusterCandidates()
	if rows[0].Nickname != "" || rows[0].Name != original.Name {
		t.Fatal("clearing nickname did not preserve original name")
	}
	for _, nickname := range []string{strings.Repeat("x", 81), "bad\nname"} {
		if err := reopened.renameClusterWorkspace(original.ID, nickname); err == nil {
			t.Fatalf("invalid nickname accepted: %q", nickname)
		}
	}
}

func TestClusterWorkspaceResolutionIsUniqueAndTargetBound(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	kube := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	p.rememberClusterSnapshot([]clusterView{{ID: "first", RancherURL: "https://rancher.test", KubeconfigPath: kube}})
	for _, host := range []string{"rancher.test", "https://RANCHER.test:443/"} {
		id, err := p.resolveLabCluster("", host, "", "")
		if err != nil || id != "first" {
			t.Fatalf("normalized association: %q %v", id, err)
		}
	}
	if id, err := p.resolveLabCluster("", "", kube, ""); err != nil || id != "first" {
		t.Fatalf("kubeconfig match: %s %v", id, err)
	}
	for _, in := range [][4]string{{"missing", "", "", ""}, {"first", "other.test", "", ""}, {"first", "", kube, "other-context"}, {"", "https://user:password@rancher.test", "", ""}, {"", "https://rancher.test/?token=secret", "", ""}} {
		if _, err := p.resolveLabCluster(in[0], in[1], in[2], in[3]); err == nil {
			t.Fatalf("invalid or mismatched target accepted: %+v", in)
		}
	}
	if id, err := p.resolveLabCluster("first", "", "", ""); err != nil || id != "first" {
		t.Fatalf("manual import assignment: %s %v", id, err)
	}
	p.rememberClusterSnapshot([]clusterView{{ID: "second", RancherURL: "https://rancher.test"}})
	if _, err := p.resolveLabCluster("", "rancher.test", "", ""); err == nil || !strings.Contains(err.Error(), "several") {
		t.Fatalf("reused host silently reassociated: %v", err)
	}
	if id, err := p.resolveLabCluster("second", "rancher.test", "", ""); err != nil || id != "second" {
		t.Fatalf("explicit identity could not disambiguate: %s %v", id, err)
	}
	external, err := p.resolveLabCluster("", "external.test", "", "")
	if err != nil || !strings.HasPrefix(external, "external-") {
		t.Fatalf("external identity: %s %v", external, err)
	}
	reopened := &localControlPanel{}
	if id, err := reopened.resolveLabCluster("", "https://EXTERNAL.test/", "", ""); err != nil || id != external {
		t.Fatalf("external identity changed after restart: %s %v", id, err)
	}
}

func TestClusterWorkspaceAPIIsPrivateAndFlat(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.rememberClusterSnapshot([]clusterView{{ID: "known", Name: "Known cluster", RancherURL: "https://known.test", KubeconfigPath: "/private/kubeconfig.yaml"}})
	p.testLab = testLabFixture(t)
	p.cacheLab, _ = cacheTestService(t)
	p.testLab.library.Runs = []testLabRun{{ID: cachelab.ID(), ClusterID: "known", Host: "known.test", Name: "Recent check", Status: "passed"}}
	call := func(method, body, token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/api/cluster-workspaces", strings.NewReader(body))
		request.Header.Set("X-Control-Panel-Token", token)
		response := httptest.NewRecorder()
		p.handleClusterWorkspaces(response, request)
		return response
	}
	if result := call("GET", "", ""); result.Code != 403 {
		t.Fatal("unauthenticated metadata exposed")
	}
	if result := call("DELETE", "", "workspace-token"); result.Code != 405 {
		t.Fatal("unexpected method accepted")
	}
	for _, body := range []string{`{"action":"rename","id":"known","nickname":"Name","unknown":true}`, `{"action":"rename","id":"missing","nickname":"Name"}`} {
		if result := call("POST", body, "workspace-token"); result.Code != 400 {
			t.Fatal(result.Code, result.Body.String())
		}
	}
	result := call("POST", `{"action":"rename","id":"known","nickname":"Favorite"}`, "workspace-token")
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	if strings.Contains(result.Body.String(), "kubeconfig") || strings.Contains(result.Body.String(), "private/") {
		t.Fatal("private connection path exposed")
	}
	var response struct {
		Clusters []map[string]json.RawMessage `json:"clusters"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Clusters) != 1 {
		t.Fatalf("response: %s", result.Body.String())
	}
	for _, key := range []string{"testRuns", "testPlans", "cacheWorkspaces", "snapshots"} {
		if raw, ok := response.Clusters[0][key]; !ok || string(raw) == "null" {
			t.Fatalf("missing flattened %s: %s", key, result.Body.String())
		}
	}
	if !strings.Contains(string(response.Clusters[0]["testRuns"]), "Recent check") {
		t.Fatal("test history not linked")
	}
}

func TestClusterWorkspaceBadManifestPreserved(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	raw := []byte(`{"version":1,"clusters":[`)
	if err := os.MkdirAll(filepath.Dir(p.clusterWorkspacesPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.clusterWorkspacesPath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.resolveLabCluster("", "example.test", "", ""); err == nil {
		t.Fatal("corrupt manifest silently replaced")
	}
	after, _ := os.ReadFile(p.clusterWorkspacesPath())
	if string(after) != string(raw) {
		t.Fatal("corrupt manifest overwritten")
	}
}

func TestClusterWorkspaceCleanupOnlyAfterSuccessfulExactRun(t *testing.T) {
	for _, scenario := range []struct {
		name                  string
		request, fail, cancel bool
	}{
		{name: "default retain"}, {name: "explicit success", request: true}, {name: "failed destroy", request: true, fail: true}, {name: "canceled destroy", request: true, cancel: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			p := clusterWorkspaceTestPanel(t)
			p.writeRunRecord(panelRunRecord{RunID: "run-a", CreatedAt: time.Now()})
			p.writeRunRecord(panelRunRecord{RunID: "run-b", CreatedAt: time.Now()})
			p.rememberClusterSnapshot([]clusterView{{ID: "cluster-a", RunID: "run-a", RancherURL: "https://a.test"}, {ID: "cluster-b", RunID: "run-b", RancherURL: "https://b.test"}})
			p.testLab = testLabFixture(t)
			p.cacheLab, _ = cacheTestService(t)
			p.testLab.library.Runs = []testLabRun{{ID: cachelab.ID(), Host: "a.test", Name: "A", Status: "passed"}, {ID: cachelab.ID(), Host: "b.test", Name: "B", Status: "passed"}}
			p.testLab.library.Plans = []testLabPlan{{ID: cachelab.ID(), Host: "a.test", Name: "Keep plan"}}
			options, err := p.freezeRunLabCleanup("run-a", scenario.request, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(options.ClusterIDs) != 1 || options.ClusterIDs[0] != "cluster-a" {
				t.Fatalf("incorrect frozen scope %+v", options)
			}
			p.operationLocked(panelOperationCleanupBatch).CancelRequested = scenario.cancel
			var cleanupErr error
			if scenario.fail {
				cleanupErr = errors.New("destroy failed")
			}
			p.finishCleanupBatchItemWithLabCleanup("run-a", cleanupErr, options)
			want := 2
			if scenario.request && !scenario.fail && !scenario.cancel {
				want = 1
			}
			if len(p.testLab.library.Runs) != want || len(p.testLab.library.Plans) != 1 {
				t.Fatalf("unexpected local cleanup: %+v", p.testLab.library)
			}
			if want == 1 && p.testLab.library.Runs[0].Name != "B" {
				t.Fatal("unrelated result removed")
			}
			if scenario.cancel && !strings.Contains(p.operationLocked(panelOperationCleanupBatch).Warning, "retained") {
				t.Fatal("canceled local cleanup not reported")
			}
			rows, err := p.labClusterCandidates()
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.ID == "cluster-a" && row.Archived == scenario.fail {
					t.Fatalf("archive status wrong: %+v", row)
				}
			}
		})
	}
}

func TestClusterWorkspaceKubeconfigContextPinned(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\ncurrent-context: management\nusers:\n- name: admin\n  user:\n    exec:\n      command: should-never-run\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p.rememberClusterSnapshot([]clusterView{{ID: "management", Name: "Rancher", RancherURL: "https://rancher.test", KubeconfigPath: path}})
	if id, err := p.resolveLabCluster("management", "", path, "management"); err != nil || id != "management" {
		t.Fatalf("effective context mismatch: %s %v", id, err)
	}
	if _, err := p.resolveLabCluster("management", "", path, "other"); err == nil {
		t.Fatal("alternate context linked to management")
	}
	if err := os.WriteFile(path, []byte("current-context: other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.resolveLabCluster("management", "", path, ""); err == nil {
		t.Fatal("changed current context silently rebound management")
	}
	raw, _ := os.ReadFile(p.clusterWorkspacesPath())
	if strings.Contains(string(raw), "should-never-run") || strings.Contains(string(raw), "users") {
		t.Fatal("kubeconfig credentials or exec config copied into registry")
	}
}

func TestClusterWorkspaceLocalLabsRetainMetadataAfterRemoval(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	steve := steveLabRunRecord{RunID: "steve-history", Status: "stopped", ClusterName: "Local SQL cache", SteveRef: "v0.10.6", Kubeconfig: "/private/steve.yaml"}
	k3d := k3dLabRecord{RunID: "k3d-history", Status: "running", ClusterName: "Local Kubernetes", K3SVersion: "v1.34.0", Kubeconfig: "/private/k3d.yaml"}
	if err := p.writeSteveLabRunRecord(steve); err != nil {
		t.Fatal(err)
	}
	if err := p.writeK3DLabRecord(k3d); err != nil {
		t.Fatal(err)
	}
	rows, err := p.labClusterCandidates()
	if err != nil || len(rows) != 2 {
		t.Fatalf("local labs not discovered: %+v %v", rows, err)
	}
	id := localLabClusterWorkspaceID("steve", steve.RunID)
	if resolved, err := p.resolveLabCluster(id, "", "", ""); err != nil || resolved != id {
		t.Fatalf("Steve import identity: %s %v", resolved, err)
	}
	if err := p.renameClusterWorkspace(id, "SQL investigation"); err != nil {
		t.Fatal(err)
	}
	if err := p.deleteSteveLabRunRecord(steve.RunID); err != nil {
		t.Fatal(err)
	}
	reopened := &localControlPanel{}
	rows, err = reopened.labClusterCandidates()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.ID == id {
			found = true
			if !row.Archived || row.Nickname != "SQL investigation" || row.Version != steve.SteveRef {
				t.Fatalf("Steve history lost: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("Steve descriptor removed with runtime")
	}
}

func TestClusterWorkspaceActiveCleanupReportsWarning(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.writeRunRecord(panelRunRecord{RunID: "run-a", CreatedAt: time.Now()})
	p.rememberClusterSnapshot([]clusterView{{ID: "cluster-a", RunID: "run-a", RancherURL: "https://a.test"}})
	p.testLab = testLabFixture(t)
	p.cacheLab, _ = cacheTestService(t)
	p.testLab.library.Runs = []testLabRun{{ID: cachelab.ID(), Host: "a.test", Name: "Still running", ClusterID: "cluster-a", Status: "running"}}
	options, err := p.freezeRunLabCleanup("run-a", true, false)
	if err != nil {
		t.Fatal(err)
	}
	p.finishCleanupBatchItemWithLabCleanup("run-a", nil, options)
	if len(p.testLab.library.Runs) != 1 || !strings.Contains(p.operationLocked(panelOperationCleanupBatch).Warning, "retained") {
		t.Fatal("active result was not retained and explained")
	}
	if len(p.operationLocked(panelOperationCleanupBatch).CompletedRunIDs) != 1 || len(p.operationLocked(panelOperationCleanupBatch).Failures) != 0 {
		t.Fatal("local cleanup warning changed successful infrastructure result")
	}
}

func TestClusterHistoryCRUDSurvivesRestart(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "create", Name: "Regression", Version: "v2.15.3-head", Milestone: "v2.16.0", Notes: "Upgrade evidence"}); err != nil {
		t.Fatal(err)
	}
	rows, err := p.labClusterCandidates()
	if err != nil || len(rows) != 1 || !rows[0].Archived {
		t.Fatalf("create: %+v %v", rows, err)
	}
	id := rows[0].ID
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "update", ID: id, Name: "Renamed", Version: "head", Milestone: "v2.17.0", Notes: "Keep this"}); err != nil {
		t.Fatal(err)
	}
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "delete", ID: id}); err != nil {
		t.Fatal(err)
	}
	reopened := &localControlPanel{}
	rows, err = reopened.labClusterCandidates()
	if err != nil || len(rows) != 0 {
		t.Fatalf("trash reappeared after restart: %+v %v", rows, err)
	}
	if err := reopened.mutateClusterHistory(clusterHistoryRequest{Action: "restore", ID: id}); err != nil {
		t.Fatal(err)
	}
	rows, err = reopened.labClusterCandidates()
	if err != nil || len(rows) != 1 || rows[0].Name != "Renamed" || rows[0].Milestone != "v2.17.0" || rows[0].Notes != "Keep this" || rows[0].Version != "head" {
		t.Fatalf("restore lost metadata: %+v %v", rows, err)
	}
}

func TestClusterHistoryProtectsActiveClustersAndEvidence(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.rememberClusterSnapshot([]clusterView{{ID: "live", Name: "Live", RunID: "run"}})
	for _, action := range []string{"update", "delete"} {
		if err := p.mutateClusterHistory(clusterHistoryRequest{Action: action, ID: "live", Name: "Changed"}); err == nil {
			t.Fatalf("%s accepted for live cluster", action)
		}
	}
	p.mu.Lock()
	p.clusterSnapshot = map[string]clusterView{}
	p.mu.Unlock()
	p.testLab = testLabFixture(t)
	p.cacheLab, _ = cacheTestService(t)
	p.testLab.library.Runs = []testLabRun{{ID: cachelab.ID(), ClusterID: "live", Name: "Evidence", Status: "passed"}}
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "delete", ID: "live"}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/cluster-workspaces", nil)
	request.Header.Set("X-Control-Panel-Token", "workspace-token")
	result := httptest.NewRecorder()
	p.handleClusterWorkspaces(result, request)
	var response struct {
		Clusters []clusterWorkspaceResponse `json:"clusters"`
		Trash    []clusterWorkspaceResponse `json:"trash"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Clusters) != 0 || len(response.Trash) != 1 || len(response.Trash[0].TestRuns) != 1 {
		t.Fatalf("trash lost evidence: %s", result.Body.String())
	}
	if len(p.testLab.library.Runs) != 1 {
		t.Fatal("underlying evidence deleted")
	}
}
