package test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
)

func newDownstreamPanelTest(t *testing.T) *localControlPanel {
	t.Helper()
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	return &localControlPanel{
		token:      "token",
		totalHAs:   2,
		repoRoot:   workspace,
		testDir:    workspace,
		operations: newPanelOperations(),
	}
}

func enabledDownstreamPlan() settings.LinodeDownstreamPlan {
	plan := settings.DefaultLinodeDownstreamPlan()
	plan.Enabled = true
	return plan
}

func TestCreateCurrentRunRecordFreezesLinodeDownstreamPlans(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeHARKE2)
	plans := []settings.LinodeDownstreamPlan{enabledDownstreamPlan(), settings.DefaultLinodeDownstreamPlan()}
	viper.Set(settings.DownstreamLinodeConfigKey, plans)

	panel := newDownstreamPanelTest(t)
	panel.createCurrentRunRecord("run-freeze", time.Now())

	record, ok := panel.readRunRecord("run-freeze")
	if !ok {
		t.Fatal("expected frozen run record")
	}
	if !reflect.DeepEqual(record.DownstreamLinodePlans, plans) {
		t.Fatalf("frozen plans = %#v, want %#v", record.DownstreamLinodePlans, plans)
	}
	if record.DownstreamStatus != panelDownstreamStatusPending {
		t.Fatalf("downstream status = %q, want %q", record.DownstreamStatus, panelDownstreamStatusPending)
	}

	changed := enabledDownstreamPlan()
	changed.Region = "eu-west"
	viper.Set(settings.DownstreamLinodeConfigKey, []settings.LinodeDownstreamPlan{changed, changed})
	reloaded, ok := panel.readRunRecord("run-freeze")
	if !ok || !reflect.DeepEqual(reloaded.DownstreamLinodePlans, plans) {
		t.Fatalf("run plans changed with live config: %#v", reloaded.DownstreamLinodePlans)
	}
}

func TestPanelCommandEnvPassesFrozenDownstreamPlans(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	plans := []settings.LinodeDownstreamPlan{enabledDownstreamPlan(), settings.DefaultLinodeDownstreamPlan()}
	panel.writeRunRecord(panelRunRecord{
		RunID:                 "run-env",
		Status:                "ready",
		DeploymentType:        deploymentTypeHARKE2,
		TotalHAs:              len(plans),
		DownstreamLinodePlans: plans,
	})
	panel.operations[panelOperationDownstream].RunID = "run-env"
	t.Setenv(panelDownstreamLinodePlansEnv, `[{"enabled":false,"region":"wrong"}]`)

	env := panel.panelCommandEnv(panelOperationDownstream)
	prefix := panelDownstreamLinodePlansEnv + "="
	values := make([]string, 0, 1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			values = append(values, strings.TrimPrefix(item, prefix))
		}
	}
	if len(values) != 1 {
		t.Fatalf("downstream plan env occurrences = %d, want 1: %#v", len(values), values)
	}
	var got []settings.LinodeDownstreamPlan
	if err := json.Unmarshal([]byte(values[0]), &got); err != nil {
		t.Fatalf("decode downstream plan env: %v", err)
	}
	if !reflect.DeepEqual(got, plans) {
		t.Fatalf("downstream plan env = %#v, want frozen %#v", got, plans)
	}
}

func TestDownstreamFailurePreservesReadyManagementRun(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	panel.writeCurrentRunRecord(panelRunRecord{
		RunID:                 "run-failure",
		Status:                "ready",
		DeploymentType:        deploymentTypeHARKE2,
		DownstreamLinodePlans: []settings.LinodeDownstreamPlan{enabledDownstreamPlan()},
		DownstreamStatus:      panelDownstreamStatusRunning,
	})
	panel.operations[panelOperationDownstream] = &panelOperationState{Running: true, RunID: "run-failure"}

	panel.finishPanelCommand(panelCommandSpec{
		Operation:   panelOperationDownstream,
		DisplayName: "downstream provisioning",
	}, errors.New("simulated Linode failure"))

	record, ok := panel.readRunRecord("run-failure")
	if !ok {
		t.Fatal("downstream failure removed the management run record")
	}
	if record.Status != "ready" {
		t.Fatalf("management status = %q, want ready", record.Status)
	}
	if record.DownstreamStatus != panelDownstreamStatusFailed || !strings.Contains(record.DownstreamError, "simulated Linode failure") {
		t.Fatalf("downstream failure fields = %q / %q", record.DownstreamStatus, record.DownstreamError)
	}
}

func TestConfiguredDownstreamStartsOnlyForEnabledFrozenPlans(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	worker := filepath.Join(t.TempDir(), "lifecycle-worker")
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write lifecycle worker: %v", err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)

	panel.writeCurrentRunRecord(panelRunRecord{
		RunID:                 "run-enabled",
		Status:                "ready",
		DeploymentType:        deploymentTypeHARKE2,
		DownstreamLinodePlans: []settings.LinodeDownstreamPlan{enabledDownstreamPlan()},
		DownstreamStatus:      panelDownstreamStatusPending,
	})
	panel.startConfiguredDownstreamsAfterReadiness("run-enabled")
	waitForPanelOperation(t, panel, panelOperationDownstream)
	waitForRunDownstreamStatus(t, panel, "run-enabled", panelDownstreamStatusReady)

	snapshot := panel.snapshotOperation(panelOperationDownstream)
	if !strings.Contains(snapshot.Command, "TestHAProvisionConfiguredLinodeDownstreams") || !strings.Contains(snapshot.Command, "35m") {
		t.Fatalf("downstream command = %q", snapshot.Command)
	}
	record, ok := panel.readRunRecord("run-enabled")
	if !ok || record.Status != "ready" || record.DownstreamStatus != panelDownstreamStatusReady {
		t.Fatalf("completed run = %#v, %v", record, ok)
	}

	panel.operations[panelOperationDownstream] = &panelOperationState{}
	panel.writeCurrentRunRecord(panelRunRecord{
		RunID:                 "run-disabled",
		Status:                "ready",
		DeploymentType:        deploymentTypeHARKE2,
		DownstreamLinodePlans: []settings.LinodeDownstreamPlan{settings.DefaultLinodeDownstreamPlan()},
	})
	panel.startConfiguredDownstreamsAfterReadiness("run-disabled")
	if snapshot := panel.snapshotOperation(panelOperationDownstream); snapshot.StartedAt != nil || snapshot.Running {
		t.Fatalf("disabled downstream plan started an operation: %#v", snapshot)
	}
}

func TestReadinessPublishesManagementReadyBeforeStartingDownstream(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	worker := filepath.Join(t.TempDir(), "lifecycle-worker")
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write lifecycle worker: %v", err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)

	panel.writeCurrentRunRecord(panelRunRecord{
		RunID:                 "run-ready-order",
		Status:                "setup_complete",
		DeploymentType:        deploymentTypeHARKE2,
		DownstreamLinodePlans: []settings.LinodeDownstreamPlan{enabledDownstreamPlan()},
		DownstreamStatus:      panelDownstreamStatusPending,
	})
	panel.operations[panelOperationReadiness] = &panelOperationState{Running: true, RunID: "run-ready-order"}
	managementWasReady := false
	panel.finishPanelCommand(panelCommandSpec{
		Operation:   panelOperationReadiness,
		DisplayName: "readiness",
		SuccessLine: "readiness complete",
		AfterSuccess: func() {
			record, ok := panel.readRunRecord("run-ready-order")
			managementWasReady = ok && record.Status == "ready"
			panel.startConfiguredDownstreamsAfterReadiness("run-ready-order")
		},
	}, nil)
	if !managementWasReady {
		t.Fatal("downstream callback ran before management status was ready")
	}
	waitForPanelOperation(t, panel, panelOperationDownstream)
	waitForRunDownstreamStatus(t, panel, "run-ready-order", panelDownstreamStatusReady)
	record, ok := panel.readRunRecord("run-ready-order")
	if !ok || record.Status != "ready" || record.DownstreamStatus != panelDownstreamStatusReady {
		t.Fatalf("readiness/downstream completion record = %#v, %v", record, ok)
	}
}

func TestHandleDownstreamRetryRequiresConfirmation(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	panel.writeRunRecord(panelRunRecord{
		RunID:                 "run-retry",
		Status:                "ready",
		DeploymentType:        deploymentTypeHARKE2,
		DownstreamLinodePlans: []settings.LinodeDownstreamPlan{enabledDownstreamPlan()},
		DownstreamStatus:      panelDownstreamStatusFailed,
	})

	request := httptest.NewRequest(http.MethodPost, "/api/downstream/retry", strings.NewReader(`{"runId":"run-retry","confirm":"retry"}`))
	request.Header.Set("X-Control-Panel-Token", "token")
	recorder := httptest.NewRecorder()
	panel.handleDownstreamRetry(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "retry downstream") {
		t.Fatalf("retry response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestCleanupBatchRejectsActiveDownstreamProvisioning(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	panel.writeRunRecord(panelRunRecord{RunID: "run-cleanup", CreatedAt: time.Now()})
	panel.operations[panelOperationDownstream].Running = true

	err := panel.startCleanupBatch([]string{"run-cleanup"})
	if err == nil || !strings.Contains(err.Error(), "downstream") {
		t.Fatalf("cleanup batch error = %v, want downstream conflict", err)
	}
}

func TestCleanupUsesCombinedDownstreamAndTerraformTimeout(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	worker := filepath.Join(t.TempDir(), "lifecycle-worker")
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write lifecycle worker: %v", err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)
	panel.writeCurrentRunRecord(panelRunRecord{
		RunID:          "run-cleanup-timeout",
		Status:         "ready",
		DeploymentType: deploymentTypeHARKE2,
	})

	if err := panel.startCleanupForRun("run-cleanup-timeout"); err != nil {
		t.Fatalf("start cleanup: %v", err)
	}
	waitForPanelOperation(t, panel, panelOperationCleanup)
	snapshot := panel.snapshotOperation(panelOperationCleanup)
	if !strings.Contains(snapshot.Command, "TestHACleanup") || !strings.Contains(snapshot.Command, "60m") {
		t.Fatalf("cleanup command = %q", snapshot.Command)
	}
}

func TestDownstreamOperationPersistsAndParticipatesInCloudConflicts(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	finishedAt := time.Now()
	panel.operations[panelOperationDownstream] = &panelOperationState{
		RunID:      "run-persisted",
		Command:    "go test -run ^TestHAProvisionConfiguredLinodeDownstreams$",
		FinishedAt: &finishedAt,
		Output:     []string{"downstream complete"},
	}
	panel.mu.Lock()
	panel.persistOperationsLocked()
	panel.mu.Unlock()

	loaded := &localControlPanel{
		token:      panel.token,
		totalHAs:   panel.totalHAs,
		repoRoot:   panel.repoRoot,
		testDir:    panel.testDir,
		operations: newPanelOperations(),
	}
	loaded.loadPersistedOperations(false)
	snapshot := loaded.snapshotOperation(panelOperationDownstream)
	if snapshot.RunID != "run-persisted" || snapshot.Command == "" || snapshot.FinishedAt == nil {
		t.Fatalf("persisted downstream snapshot = %#v", snapshot)
	}
	parsed, ok := parsePanelOperationName("downstream")
	if !ok || parsed != panelOperationDownstream || !isCloudPanelOperation(parsed) {
		t.Fatalf("downstream parsing/cloud classification = %q, %v", parsed, ok)
	}
	foundConflict := false
	for _, operation := range conflictingPanelOperationNames(panelOperationSetup) {
		if operation == panelOperationDownstream {
			foundConflict = true
			break
		}
	}
	if !foundConflict {
		t.Fatal("setup cloud conflicts do not include downstream provisioning")
	}
}

func TestRestartMarksStaleDownstreamFailedAndPreservesManagementReady(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	panel.writeCurrentRunRecord(panelRunRecord{
		RunID:                 "run-restart",
		Status:                "ready",
		DeploymentType:        deploymentTypeHARKE2,
		DownstreamLinodePlans: []settings.LinodeDownstreamPlan{enabledDownstreamPlan()},
		DownstreamStatus:      panelDownstreamStatusRunning,
	})
	startedAt := time.Now().Add(-time.Minute)
	panel.operations[panelOperationDownstream] = &panelOperationState{
		Running:   true,
		PID:       -1,
		StartedAt: &startedAt,
		RunID:     "run-restart",
		Output:    []string{"downstream running"},
	}
	panel.mu.Lock()
	panel.persistOperationsLocked()
	panel.mu.Unlock()

	loaded := &localControlPanel{
		token:      panel.token,
		totalHAs:   panel.totalHAs,
		repoRoot:   panel.repoRoot,
		testDir:    panel.testDir,
		operations: newPanelOperations(),
	}
	loaded.loadPersistedOperations(true)
	snapshot := loaded.snapshotOperation(panelOperationDownstream)
	if snapshot.Running || snapshot.Error == "" {
		t.Fatalf("stale downstream snapshot = %#v", snapshot)
	}
	record, ok := loaded.readRunRecord("run-restart")
	if !ok {
		t.Fatal("stale downstream reconciliation removed the management run")
	}
	if record.Status != "ready" || record.DownstreamStatus != panelDownstreamStatusFailed {
		t.Fatalf("stale downstream run = %#v", record)
	}
	if !strings.Contains(record.DownstreamError, "panel restarted") {
		t.Fatalf("stale downstream error = %q", record.DownstreamError)
	}
}

func TestClusterDiscoveryScopesDownstreamRecordsToRun(t *testing.T) {
	panel := newDownstreamPanelTest(t)
	if err := writeDownstreamOutputRecord(downstreamOutputRecord{
		RunID:       "run-a",
		HAIndex:     1,
		RancherHost: "https://rancher-a.example.test",
		ClusterName: "downstream-a",
	}); err != nil {
		t.Fatalf("write run-a downstream record: %v", err)
	}

	runAClusters := panel.discoverClustersForRun(panelRunRecord{RunID: "run-a", TotalHAs: 1, DeploymentType: deploymentTypeHARKE2})
	if len(runAClusters) == 0 {
		t.Fatal("run-a discovery did not see its downstream record as a run signal")
	}
	runBClusters := panel.discoverClustersForRun(panelRunRecord{RunID: "run-b", TotalHAs: 1, DeploymentType: deploymentTypeHARKE2})
	if len(runBClusters) != 0 {
		t.Fatalf("run-b discovery leaked run-a downstream state: %#v", runBClusters)
	}
}

func waitForPanelOperation(t *testing.T, panel *localControlPanel, operation panelOperationName) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := panel.snapshotOperation(operation)
		if !snapshot.Running && snapshot.FinishedAt != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s operation did not finish: %#v", operation, panel.snapshotOperation(operation))
}

func waitForRunDownstreamStatus(t *testing.T, panel *localControlPanel, runID, status string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if record, ok := panel.readRunRecord(runID); ok && record.DownstreamStatus == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, ok := panel.readRunRecord(runID)
	t.Fatalf("run %s downstream status did not become %s: %#v, %v", runID, status, record, ok)
}
