package test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestPanelConcurrentProvisioningKeepsRunsSeparate(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeHARKE2)
	viper.Set("total_has", 1)
	panel.totalHAs = 1
	module := filepath.Join(panel.repoRoot, "modules", "aws")
	if err := os.MkdirAll(module, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "main.tf"), []byte("# fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	gates := t.TempDir()
	worker := filepath.Join(t.TempDir(), "worker")
	script := `#!/bin/bash
set -eu
trap 'exit 73' INT TERM
stage=setup
if [[ "$*" == *TestReadiness* ]]; then stage=readiness; fi
echo "$stage $HA_RANCHER_RUN_ID"
cat "$HA_RANCHER_TOOL_CONFIG_PATH"
touch "$TEST_RUN_GATES/$HA_RANCHER_RUN_ID-$stage-started"
while [[ ! -f "$TEST_RUN_GATES/$HA_RANCHER_RUN_ID-$stage-finish" ]]; do sleep 0.02; done
`
	if err := os.WriteFile(worker, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)
	t.Setenv("TEST_RUN_GATES", gates)
	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(gates, "absent-repos"))
	t.Setenv("HELM_REGISTRY_CONFIG", filepath.Join(gates, "absent-registry"))
	release := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(gates, name+"-finish"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, run := range []string{"prime", "community", "third"} {
			release(run + "-setup")
			release(run + "-readiness")
		}
		panel.workers.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := panel.workers.Wait(ctx); err != nil {
			t.Error(err)
		}
	})
	await := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("timed out waiting for isolated worker")
	}
	start := func(runID string, after func()) error {
		return panel.startPanelCommand(panelCommandSpec{
			Operation: panelOperationSetup, RunID: runID, DisplayName: "setup",
			TestName: "TestSetup", Timeout: "1m", AfterSuccess: after,
		})
	}

	handoff := make(chan struct{})
	continueHandoff := make(chan struct{})
	chained := make(chan error, 1)
	viper.Set("rancher.distro", "prime")
	if err := start("prime", func() {
		close(handoff)
		<-continueHandoff
		chained <- panel.startPanelCommand(panelCommandSpec{
			Operation: panelOperationReadiness, RunID: "prime", DisplayName: "readiness",
			TestName: "TestReadiness", Timeout: "1m",
		})
	}); err != nil {
		t.Fatal(err)
	}
	viper.Set("rancher.distro", "community")
	if err := start("community", nil); err != nil {
		close(continueHandoff)
		t.Fatal(err)
	}
	// Ensure cleanup can always release the callback, including an assertion failure.
	t.Cleanup(func() {
		select {
		case <-continueHandoff:
		default:
			close(continueHandoff)
		}
	})
	await(func() bool {
		return pathExists(filepath.Join(gates, "prime-setup-started")) &&
			pathExists(filepath.Join(gates, "community-setup-started"))
	})
	if err := start("third", nil); err == nil || !strings.Contains(err.Error(), "two provisioning") {
		t.Fatalf("third run should be rejected, got %v", err)
	}
	if err := panel.abortOperation(panelOperationSetup, ""); err == nil || !strings.Contains(err.Error(), "runId") {
		t.Fatalf("ambiguous stop should require a run ID, got %v", err)
	}
	await(func() bool {
		snapshots := panel.snapshotRunOperations(nil)
		if len(snapshots) != 2 {
			return false
		}
		for _, snapshot := range snapshots {
			if !strings.Contains(strings.Join(snapshot.Output, "\n"), "distro: "+snapshot.RunID) {
				return false
			}
		}
		return true
	})
	snapshots := panel.snapshotRunOperations(nil)
	if len(snapshots) != 2 {
		t.Fatalf("expected two independent operation snapshots, got %#v", snapshots)
	}
	for _, snapshot := range snapshots {
		lines := strings.Join(snapshot.Output, "\n")
		if snapshot.Operation != panelOperationSetup || !snapshot.Running ||
			!strings.Contains(lines, "distro: "+snapshot.RunID) {
			t.Fatalf("worker output/config crossed runs: %#v", snapshot)
		}
	}

	release("prime-setup")
	select {
	case <-handoff:
	case <-time.After(5 * time.Second):
		t.Fatal("setup did not reach its readiness handoff")
	}
	if err := start("third", nil); err == nil {
		t.Fatal("completion callback must retain its run slot")
	}
	close(continueHandoff)
	if err := <-chained; err != nil {
		t.Fatalf("same-run readiness handoff rejected: %v", err)
	}
	await(func() bool {
		snapshot := panel.snapshotOperation(panelOperationForRun(panelOperationReadiness, "prime"))
		return strings.Contains(strings.Join(snapshot.Output, "\n"), "distro: prime")
	})
	ready := panel.snapshotOperation(panelOperationForRun(panelOperationReadiness, "prime"))
	if ready.RunID != "prime" || !strings.Contains(strings.Join(ready.Output, "\n"), "distro: prime") {
		t.Fatalf("readiness used the other run's configuration: %#v", ready)
	}
	if err := panel.abortOperation(panelOperationSetup, "community"); err != nil {
		t.Fatal(err)
	}
	await(func() bool {
		return !panel.snapshotOperation(panelOperationForRun(panelOperationSetup, "community")).Running
	})
	if !panel.snapshotOperation(panelOperationForRun(panelOperationReadiness, "prime")).Running {
		t.Fatal("stopping Community also stopped Prime")
	}
	release("prime-readiness")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := panel.workers.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if panel.anyOperationRunning() {
		t.Fatal("completed pipelines still occupy admission slots")
	}
}

func TestPanelRunOperationsRecoverPersistedWorkersAndBlockCleanup(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	now := time.Now()
	for _, runID := range []string{"prime", "community"} {
		key := panelOperationForRun(panelOperationSetup, runID)
		panel.operations[key] = &panelOperationState{
			Running: true, PID: os.Getpid(), RunID: runID, StartedAt: &now, UpdatedAt: &now,
		}
	}
	panel.mu.Lock()
	panel.persistOperationsLocked()
	panel.mu.Unlock()
	restored := &localControlPanel{operations: newPanelOperations()}
	restored.loadPersistedOperations(true)
	if snapshots := restored.snapshotRunOperations(nil); len(snapshots) != 2 || !snapshots[0].Running || !snapshots[1].Running {
		t.Fatalf("restart lost active workers: %#v", snapshots)
	}
	restored.mu.Lock()
	defer restored.mu.Unlock()
	if !restored.conflictingOperationRunningLocked(panelOperationCleanup) ||
		!restored.conflictingOperationRunningLocked(panelOperationLinodeCleanup) {
		t.Fatal("cleanup must stay excluded during concurrent provisioning")
	}
	if err := restored.provisioningAdmissionErrorLocked(""); err == nil {
		t.Fatal("restored workers must count toward the two-run limit")
	}
}

func TestReadinessPreflightUsesRequestedRunArtifacts(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	panel.totalHAs = 5
	record := panelRunRecord{
		RunID: "prime", TotalHAs: 1, DeploymentType: deploymentTypeHARKE2,
		HAOutputRoot: panel.haOutputRootForRun("prime"),
	}
	dir := filepath.Join(record.HAOutputRoot, "high-availability-1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kube_config.yaml"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	panel.writeCurrentRunRecord(panelRunRecord{
		RunID: "community", TotalHAs: 5, HAOutputRoot: panel.haOutputRootForRun("community"),
	})
	if err := panel.readinessPreflightError(deploymentTypeHARKE2, record, map[string]string{
		"ha_1_rancher_url": "https://prime.example.test",
	}); err != nil {
		t.Fatalf("readiness consulted another run's paths or instance count: %v", err)
	}
}
