package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testClusterResolver(id, host, path, ctx string) (string, error) {
	if id != "" && id != "cluster-a" && id != "cluster-b" {
		return "", fmt.Errorf("unknown cluster")
	}
	if id == "cluster-b" && host != "" && host != "other.test" {
		return "", fmt.Errorf("target does not match cluster")
	}
	if id == "" {
		return "cluster-a", nil
	}
	return id, nil
}
func TestTestLabClusterRecordCRUDAndPersistence(t *testing.T) {
	s := testLabFixture(t)
	s.resolveCluster = testClusterResolver
	req := testLabFixtureRequest()
	req.Action = "save-plan"
	value, err := s.mutate(req)
	if err != nil {
		t.Fatal(err)
	}
	plan := value.(testLabPlan)
	if plan.ClusterID != "cluster-a" {
		t.Fatalf("missing automatic association: %+v", plan)
	}
	reviewed, err := s.review(req)
	if err != nil || reviewed.(map[string]any)["clusterId"] != "cluster-a" {
		t.Fatalf("review association: %v %v", reviewed, err)
	}
	req.ClusterID = "cluster-b"
	if _, err := s.review(req); err == nil {
		t.Fatal("accepted stale picker target")
	}
	if _, err := s.mutate(req); err == nil {
		t.Fatal("saved a plan under the wrong cluster")
	}
	if _, err := s.mutate(testLabRequest{Action: "rename-plan", ID: plan.ID, Name: "A useful saved plan"}); err != nil {
		t.Fatal(err)
	}
	runID := cacheLabID()
	s.library.Runs = []testLabRun{{ID: runID, Host: "example.test", Name: "Original", Status: "passed"}}
	if _, err := s.mutate(testLabRequest{Action: "assign-cluster", Scope: "run", ID: runID, ClusterID: "cluster-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.mutate(testLabRequest{Action: "rename-run", ID: runID, Name: "Baseline validation"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.mutate(testLabRequest{Action: "assign-cluster", Scope: "run", ID: runID, ClusterID: "cluster-b"}); err == nil {
		t.Fatal("relabeled result under mismatched host")
	}
	if _, err := s.mutate(testLabRequest{Action: "assign-cluster", Scope: "run", ID: runID}); err == nil {
		t.Fatal("accepted implicit unlink")
	}
	restored, err := newTestLabService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if restored.library.Plans[0].ClusterID != "cluster-a" || restored.library.Plans[0].Name != "A useful saved plan" || restored.library.Runs[0].ClusterID != "cluster-a" || restored.library.Runs[0].Name != "Baseline validation" {
		t.Fatal("cluster record edits did not survive reopen")
	}
	s.library.Runs[0].Status = "running"
	for _, action := range []string{"rename-run", "assign-cluster"} {
		if _, err := s.mutate(testLabRequest{Action: action, Scope: "run", ID: runID, Name: "New", ClusterID: "cluster-a"}); err == nil {
			t.Fatal("edited active test record")
		}
	}
	s.library.Runs[0].Status = "passed"
	oldRoot := s.root
	s.root = filepath.Join(t.TempDir(), "missing")
	if _, err := s.mutate(testLabRequest{Action: "rename-run", ID: runID, Name: "Should roll back"}); err == nil {
		t.Fatal("persist unexpectedly succeeded")
	}
	s.root = oldRoot
	if s.library.Runs[0].Name != "Baseline validation" {
		t.Fatal("failed persistence changed in-memory record")
	}
}
func TestLabClusterBackfillOnlyUniqueTargets(t *testing.T) {
	candidates := []clusterWorkspaceRecord{{ID: "first", URL: "https://shared.test"}, {ID: "second", URL: "https://shared.test"}, {ID: "unique", URL: "https://unique.test"}}
	s := testLabFixture(t)
	s.library.Runs = []testLabRun{{ID: cacheLabID(), Host: "shared.test", Status: "passed"}, {ID: cacheLabID(), Host: "UNIQUE.test", Status: "passed"}, {ID: cacheLabID(), Host: "unique.test", ClusterID: "explicit", Status: "passed"}, {ID: cacheLabID(), Host: "unique.test", Status: "running"}}
	s.library.Plans = []testLabPlan{{ID: cacheLabID(), Host: "unique.test"}}
	if err := s.backfillClusters(candidates); err != nil {
		t.Fatal(err)
	}
	if s.library.Runs[0].ClusterID != "" || s.library.Runs[1].ClusterID != "unique" || s.library.Runs[2].ClusterID != "explicit" || s.library.Runs[3].ClusterID != "" || s.library.Plans[0].ClusterID != "unique" {
		t.Fatal("unsafe legacy association")
	}
	cache, w := cacheTestService(t)
	cache.library.Workspaces[0].Kind = "rancher"
	cache.library.Workspaces[0].URL = "https://unique.test"
	if err := cache.backfillClusters(candidates); err != nil {
		t.Fatal(err)
	}
	if cache.library.Workspaces[0].ID != w.ID || cache.library.Workspaces[0].ClusterID != "unique" {
		t.Fatal("legacy cache was not linked")
	}
	if err := cache.backfillClusters([]clusterWorkspaceRecord{{ID: "replacement", URL: "https://unique.test"}}); err != nil {
		t.Fatal(err)
	}
	if cache.library.Workspaces[0].ClusterID != "unique" {
		t.Fatal("overwrote existing cache association")
	}
}
func TestClusterLabCleanupScopeAndActiveGuards(t *testing.T) {
	tests := testLabFixture(t)
	cache, w := cacheTestService(t)
	done, active, other := cacheLabID(), cacheLabID(), cacheLabID()
	tests.library.Runs = []testLabRun{{ID: done, Name: "Finished", ClusterID: "cluster-a", Status: "passed"}, {ID: active, Name: "Active", ClusterID: "cluster-a", Status: "running"}, {ID: other, Name: "Other", ClusterID: "cluster-b", Status: "failed"}}
	tests.library.Plans = []testLabPlan{{ID: cacheLabID(), ClusterID: "cluster-a", Name: "Keep saved plan"}}
	for _, id := range []string{done, active, other} {
		tests.logs[id] = "activity"
		if err := os.WriteFile(filepath.Join(tests.root, id+".log"), []byte("activity"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cache.library.Workspaces[0].ClusterID = "cluster-a"
	snapshot := cacheTestSnapshot(t, cache, w, cacheTestDB(t, "CREATE TABLE records(id INTEGER)"), "Baseline")
	cache.library.Job = cacheLabJob{Running: true, Workspace: w.ID}
	p := &localControlPanel{testLab: tests, cacheLab: cache}
	warnings := p.cleanupLabClusters([]string{"cluster-a"}, true, true)
	if len(warnings) != 2 || len(tests.library.Runs) != 2 || len(tests.library.Plans) != 1 || len(cache.library.Snapshots) != 1 {
		t.Fatalf("cleanup crossed active scope: %v", warnings)
	}
	if _, err := os.Stat(filepath.Join(tests.root, done+".log")); !os.IsNotExist(err) {
		t.Fatal("completed result log retained")
	}
	if _, err := os.Stat(filepath.Join(tests.root, active+".log")); err != nil {
		t.Fatal("active log removed")
	}
	if _, err := tests.mutate(testLabRequest{Action: "delete-run", ID: other, ClusterID: "cluster-a", Confirm: typedConfirmationPhrase}); err == nil {
		t.Fatal("stale association allowed result deletion")
	}
	cache.library.Job.Running = false
	if _, err := cache.mutate(cacheLabRequest{Action: "delete-workspace", Workspace: w.ID, ClusterID: "cluster-b", Confirm: typedConfirmationPhrase}); err == nil {
		t.Fatal("stale association allowed cache deletion")
	}
	warnings = p.cleanupLabClusters([]string{"cluster-a"}, false, true)
	if len(warnings) != 0 || len(cache.library.Workspaces) != 0 || len(cache.library.Snapshots) != 0 || len(tests.library.Runs) != 2 || len(tests.library.Plans) != 1 {
		t.Fatalf("optional cache cleanup failed: %v", warnings)
	}
	if _, err := os.Stat(filepath.Join(cache.root, w.ID, snapshot.ID+".db")); !os.IsNotExist(err) {
		t.Fatal("cache snapshot remained")
	}
}
func TestCacheLabClusterLinksLockSnapshotSource(t *testing.T) {
	s, _ := cacheTestService(t)
	s.resolveCluster = func(id, host, path, ctx string) (string, error) {
		if id == "" {
			return "cluster-a", nil
		}
		return id, nil
	}
	profile := cacheLabWorkspace{Kind: "rancher", URL: "https://example.test", Name: "Baseline", ClusterID: "cluster-a"}
	value, err := s.saveWorkspace(context.Background(), cacheLabRequest{Profile: profile, Token: "fixture:private-token"})
	if err != nil {
		t.Fatal(err)
	}
	w := value.(cacheLabWorkspace)
	if err := os.MkdirAll(filepath.Join(s.root, w.ID), 0700); err != nil {
		t.Fatal(err)
	}
	cacheTestSnapshot(t, s, w, cacheTestDB(t, "CREATE TABLE records(id INTEGER)"), "Snapshot")
	for _, change := range []func(*cacheLabWorkspace){func(v *cacheLabWorkspace) { v.URL = "https://other.test" }, func(v *cacheLabWorkspace) { v.Cluster = "downstream" }, func(v *cacheLabWorkspace) { v.Context = "other-context" }, func(v *cacheLabWorkspace) { v.ClusterID = "cluster-b" }} {
		edited := w
		change(&edited)
		if _, err := s.saveWorkspace(context.Background(), cacheLabRequest{Profile: edited}); err == nil {
			t.Fatal("rebound snapshotted connection")
		}
	}
	if _, err := s.mutate(cacheLabRequest{Action: "assign-cluster", Workspace: w.ID, ClusterID: "cluster-b"}); err == nil {
		t.Fatal("reassigned snapshotted workspace")
	}
	edited := w
	edited.Name = "Renamed safely"
	edited.ClusterID = ""
	value, err = s.saveWorkspace(context.Background(), cacheLabRequest{Profile: edited})
	if err != nil || value.(cacheLabWorkspace).ClusterID != "cluster-a" {
		t.Fatalf("nickname/token edit lost association: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(s.root, "library.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-token") {
		t.Fatal("credential leaked into cluster metadata")
	}
	s.library.Job = cacheLabJob{Running: true, Workspace: w.ID}
	if _, err := s.mutate(cacheLabRequest{Action: "assign-cluster", Workspace: w.ID, ClusterID: "cluster-a"}); err == nil {
		t.Fatal("changed active capture association")
	}
}

func TestClusterLabSummaryExcludesConnectionSecrets(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
	tests := testLabFixture(t)
	tests.library.Runs = []testLabRun{{ID: cacheLabID(), ClusterID: "cluster-a", Name: "Validation", Host: "example.test", Status: "failed", SHA: testLabFixtureSHA, Results: []testLabResult{{Package: "validation/example", Name: "TestFirst", Status: "pass"}}, Error: "do-not-expose-log-content"}}
	tests.library.Plans = []testLabPlan{{ID: cacheLabID(), ClusterID: "cluster-a", Name: "Plan", Host: "example.test", HasConfig: true}}
	cache, w := cacheTestService(t)
	cache.library.Workspaces[0].ClusterID = "cluster-a"
	cache.library.Workspaces[0].CAPEM = "private-certificate-marker"
	cache.library.Workspaces[0].Kubeconfig = "private-path-marker"
	cache.library.Workspaces[0].Queries = []cacheLabSavedQuery{{Name: "query", SQL: "private-query-marker"}}
	cache.tokens[w.ID] = "private-token-marker"
	snapshot := cacheTestSnapshot(t, cache, w, cacheTestDB(t, "CREATE TABLE records(id INTEGER)"), "Before")
	if _, err := cache.mutate(cacheLabRequest{Action: "snapshot", Workspace: w.ID, Snapshot: snapshot.ID, Name: "Before", Folder: "Investigation", Notes: "Keep these notes", Favorite: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.mutate(cacheLabRequest{Action: "rename-snapshot", Workspace: w.ID, Snapshot: snapshot.ID, Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	p := &localControlPanel{testLab: tests, cacheLab: cache, clusterWorkspaces: map[string]clusterWorkspaceRecord{"cluster-a": {ID: "cluster-a", URL: "https://example.test"}}}
	summary, err := p.labClusterRecords()
	if err != nil {
		t.Fatal(err)
	}
	data := summary["cluster-a"]
	if len(data.TestRuns) != 1 || len(data.TestPlans) != 1 || len(data.CacheWorkspaces) != 1 || len(data.Snapshots) != 1 || len(data.TestRuns[0].Results) != 1 || data.TestRuns[0].SHA != testLabFixtureSHA {
		t.Fatalf("missing useful summary: %+v", data)
	}
	snap := data.Snapshots[0]
	if snap.Name != "Renamed" || snap.Folder != "Investigation" || snap.Notes != "Keep these notes" || !snap.Favorite || snap.Tables != 1 {
		t.Fatalf("rename damaged snapshot metadata: %+v", snap)
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-certificate-marker", "private-path-marker", "private-query-marker", "private-token-marker", "do-not-expose-log-content"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("summary exposed %q", secret)
		}
	}
}

func TestCacheLabSteveClusterLinkPersistsWithoutCapture(t *testing.T) {
	s, _ := cacheTestService(t)
	record := steveLabRunRecord{RunID: "steve-fixture", SteveRef: "v0.10.6"}
	clusterID := localLabClusterWorkspaceID("steve", record.RunID)
	id, err := s.prepareSteveWorkspace(record, clusterID)
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.prepareSteveWorkspace(record, clusterID)
	if err != nil || same != id {
		t.Fatal("Steve source duplicated", err)
	}
	restored, err := newCacheLabService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	w, err := restored.workspaceLocked(id)
	if err != nil || w.ClusterID != clusterID {
		t.Fatal("Steve link did not survive restart", err)
	}
	s.library.Job = cacheLabJob{Running: true, Workspace: id}
	if _, err := s.prepareSteveWorkspace(record, clusterID); err == nil {
		t.Fatal("changed an active capture workspace")
	}
	s.library.Job.Running = false
	current, _ := s.workspaceLocked(id)
	current.ClusterID = ""
	if err := s.backfillClusters([]clusterWorkspaceRecord{{ID: clusterID, Role: "steve", RunID: record.RunID}}); err != nil {
		t.Fatal(err)
	}
	current, _ = s.workspaceLocked(id)
	if current.ClusterID != clusterID {
		t.Fatal("legacy Steve workspace not associated")
	}
	current.ClusterID = "explicit"
	if _, err := s.prepareSteveWorkspace(record, clusterID); err != nil {
		t.Fatal(err)
	}
	current, _ = s.workspaceLocked(id)
	if current.ClusterID != "explicit" {
		t.Fatal("overrode explicit workspace link")
	}
}
