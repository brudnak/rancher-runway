package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitForDiscovery[T any](t *testing.T, snapshot *panelDiscoverySnapshot[T]) T {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		snapshot.mu.Lock()
		value, refreshing := snapshot.value, snapshot.refreshing
		snapshot.mu.Unlock()
		if !refreshing {
			return value
		}
		select {
		case <-deadline:
			t.Fatal("discovery did not finish")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestPanelDiscoveryKeepsLastResultAndCoalescesSlowRefresh(t *testing.T) {
	snapshot := panelDiscoverySnapshot[int]{value: 7, finishedAt: time.Now().Add(-time.Minute)}
	release := make(chan struct{})
	var calls atomic.Int32
	defer func() {
		close(release)
		if got := waitForDiscovery(t, &snapshot); got != 8 {
			t.Errorf("completed discovery = %d, want 8", got)
		}
		if got, refreshing := snapshot.snapshot(func() int { return 9 }); got != 8 || refreshing {
			t.Errorf("fresh result = %d, refreshing = %v", got, refreshing)
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("started %d discovery scans, want exactly one", got)
		}
	}()
	collect := func() int {
		calls.Add(1)
		<-release
		return 8
	}
	var polls sync.WaitGroup
	for range 20 {
		polls.Go(func() {
			if got, refreshing := snapshot.snapshot(collect); got != 7 || !refreshing {
				t.Errorf("pending discovery = %d, refreshing = %v", got, refreshing)
			}
		})
	}
	finished := make(chan struct{})
	go func() { polls.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("polls blocked on remote discovery")
	}
	// The collector may still be waiting to run; all pollers already observed
	// the in-flight marker. It must never start more than one remote scan.
	if got := calls.Load(); got > 1 {
		t.Fatalf("started %d concurrent discovery scans", got)
	}
}

func TestHandleStateRespondsDuringBlockedDiscoveryAndKeepsSafetyGate(t *testing.T) {
	root := t.TempDir()
	testDir := filepath.Join(root, "terratest")
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", testDir)
	panel := &localControlPanel{
		token: "test-token", repoRoot: root, testDir: testDir,
		operations: newPanelOperations(),
	}
	panel.operations[panelOperationSetup].Running = true
	panel.operations[panelOperationSetup].RunID = "test-run"
	panel.writeRunRecord(panelRunRecord{RunID: "test-run", TotalHAs: 1})
	release := make(chan struct{})
	panel.clusterDiscovery.snapshot(func() panelClusterState {
		<-release
		updated := time.Now()
		return panelClusterState{Items: []clusterView{{ID: "test-cluster", Error: "unreachable"}}, UpdatedAt: &updated}
	})
	panel.awsDiscovery.snapshot(func() panelAWSInventoryState {
		<-release
		return panelAWSInventoryState{UpdatedAt: time.Now(), Error: "scan timed out"}
	})
	finishDiscovery := sync.OnceFunc(func() {
		close(release)
		waitForDiscovery(t, &panel.clusterDiscovery)
		waitForDiscovery(t, &panel.awsDiscovery)
	})
	defer finishDiscovery()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/state?token=test-token", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	finished := make(chan struct{})
	go func() { panel.handleState(response, request); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("startup status waited for remote discovery")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("state returned %d: %s", response.Code, response.Body.String())
	}
	var state panelState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !state.Setup.Running || state.Workspace.CanStartIsolatedRun || len(state.Workspace.Runs) != 1 {
		t.Fatalf("local safety state was lost: setup=%+v workspace=%+v", state.Setup, state.Workspace)
	}
	if !state.Clusters.Refreshing || !state.AWS.Refreshing || state.Clusters.Items == nil || state.AWS.Items == nil {
		t.Fatalf("pending discovery was not reported: clusters=%+v AWS=%+v", state.Clusters, state.AWS)
	}
	finishDiscovery()
	response = httptest.NewRecorder()
	panel.handleState(response, request)
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Clusters.Refreshing || len(state.Clusters.Items) != 1 || state.Clusters.Items[0].Error != "unreachable" || state.Clusters.UpdatedAt == nil {
		t.Fatalf("completed cluster scan was not published: %+v", state.Clusters)
	}
	if state.AWS.Refreshing || state.AWS.Error != "scan timed out" || state.AWS.UpdatedAt.IsZero() {
		t.Fatalf("completed AWS scan was not published: %+v", state.AWS)
	}
}

func TestPanelDiscoveryCommandsHaveProcessDeadlines(t *testing.T) {
	binDir := t.TempDir()
	for _, name := range []string{"kubectl", "terraform"} {
		// Simulate a CLI that ignores its own request timeout, without leaving
		// child processes behind after the command context kills it.
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	results := make(chan error, 2)
	go func() { _, err := runKubectl("unused", "get", "pods"); results <- err }()
	go func() { _, err := readTerraformFlatOutputsWithModule(binDir, "", "", binDir); results <- err }()
	deadline := time.After(8 * time.Second)
	for range 2 {
		select {
		case err := <-results:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("command returned %v, want deadline exceeded", err)
			}
		case <-deadline:
			t.Fatal("discovery command exceeded its process deadline")
		}
	}
}
