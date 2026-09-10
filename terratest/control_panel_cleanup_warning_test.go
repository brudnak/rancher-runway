package test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func newPanelCleanupWarningTestPanel(t *testing.T, runID string) *localControlPanel {
	t.Helper()
	workspace := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", workspace)
	now := time.Now()
	panel := &localControlPanel{
		repoRoot:   workspace,
		testDir:    workspace,
		operations: newPanelOperations(),
	}
	panel.writeCurrentRunRecord(panelRunRecord{
		RunID:     runID,
		Status:    "ready",
		CreatedAt: now,
		UpdatedAt: now,
	})
	panel.operations[panelOperationCleanup] = &panelOperationState{
		Running:   true,
		StartedAt: &now,
		UpdatedAt: &now,
		RunID:     runID,
		Output:    []string{"[control-panel] cleanup started"},
	}
	return panel
}

func TestCleanupWarningSuccessRemovesRunAndSurvivesRestartAndFiltering(t *testing.T) {
	const runID = "warning-success"
	panel := newPanelCleanupWarningTestPanel(t, runID)
	warning := manualLinodeCleanupWarning(errors.New("HA 1 cluster cluster-one: management API unavailable"))
	panel.appendOperationOutput(panelOperationCleanup, cleanupWarningLogLine(warning))

	panel.finishPanelCommand(panelCommandSpec{
		Operation:   panelOperationCleanup,
		DisplayName: "cleanup",
		SuccessLine: "[control-panel] Cleanup completed successfully",
	}, nil)

	if _, ok := panel.readRunRecord(runID); ok {
		t.Fatal("successful AWS cleanup with a downstream warning kept the run record")
	}
	// Move the result outside the ordinary one-hour success window so this
	// specifically exercises durable warning retention rather than recency.
	oldFinishedAt := time.Now().Add(-2 * time.Hour)
	panel.mu.Lock()
	panel.operations[panelOperationCleanup].FinishedAt = &oldFinishedAt
	panel.operations[panelOperationCleanup].UpdatedAt = &oldFinishedAt
	panel.persistOperationsLocked()
	panel.mu.Unlock()
	snapshot := panel.snapshotOperationForRuns(panelOperationCleanup, map[string]bool{})
	if snapshot.Error != "" || snapshot.Warning != warning || snapshot.FinishedAt == nil {
		t.Fatalf("warning cleanup snapshot = %#v", snapshot)
	}
	if statusWarning := localWorkspaceOperation(snapshot).Warning; statusWarning != warning {
		t.Fatalf("workspace status warning = %q, want %q", statusWarning, warning)
	}

	loaded := &localControlPanel{}
	loaded.loadPersistedOperations(true)
	persisted := loaded.snapshotOperationForRuns(panelOperationCleanup, map[string]bool{})
	if persisted.Warning != warning || persisted.FinishedAt == nil || persisted.Error != "" {
		t.Fatalf("persisted warning cleanup snapshot = %#v", persisted)
	}
}

func TestCleanupWarningFailurePreservesRunAndWarning(t *testing.T) {
	const runID = "warning-failure"
	panel := newPanelCleanupWarningTestPanel(t, runID)
	warning := manualLinodeCleanupWarning(errors.New("HA 1 cluster cluster-one: management API unavailable"))
	panel.appendOperationOutput(panelOperationCleanup, cleanupWarningLogLine(warning))
	managementErr := errors.New("terraform destroy failed")

	panel.finishPanelCommand(panelCommandSpec{
		Operation:   panelOperationCleanup,
		DisplayName: "cleanup",
		SuccessLine: "[control-panel] Cleanup completed successfully",
	}, managementErr)

	record, ok := panel.readRunRecord(runID)
	if !ok || record.Status != "cleanup_failed" {
		t.Fatalf("failed cleanup run record = %#v, %v; want cleanup_failed", record, ok)
	}
	snapshot := panel.snapshotOperation(panelOperationCleanup)
	if snapshot.Warning != warning || !strings.Contains(snapshot.Error, managementErr.Error()) {
		t.Fatalf("failed warning cleanup snapshot = %#v", snapshot)
	}
}

func TestCleanupWarningSurvivesOutputTruncation(t *testing.T) {
	panel := newPanelCleanupWarningTestPanel(t, "warning-output-cap")
	warning := manualLinodeCleanupWarning(errors.New("HA 1 cluster cluster-one: management API unavailable"))
	panel.appendOperationOutput(panelOperationCleanup, cleanupWarningLogLine(warning))
	for i := 0; i < 501; i++ {
		panel.appendOperationOutput(panelOperationCleanup, fmt.Sprintf("ordinary cleanup output %d", i))
	}

	snapshot := panel.snapshotOperation(panelOperationCleanup)
	if len(snapshot.Output) != 500 {
		t.Fatalf("cleanup output lines = %d, want capped 500", len(snapshot.Output))
	}
	if strings.Contains(strings.Join(snapshot.Output, "\n"), cleanupManualWarningMarker) {
		t.Fatal("test did not evict the warning marker from capped output")
	}
	if snapshot.Warning != warning {
		t.Fatalf("structured warning after output truncation = %q, want %q", snapshot.Warning, warning)
	}
}

func TestCleanupBatchMirrorsWarningWithoutTurningSuccessIntoFailure(t *testing.T) {
	tests := []struct {
		name            string
		managementErr   error
		wantSucceeded   int
		wantFailed      int
		wantRecord      bool
		wantRecordState string
	}{
		{name: "management success", wantSucceeded: 1},
		{name: "management failure", managementErr: errors.New("terraform destroy failed"), wantFailed: 1, wantRecord: true, wantRecordState: "cleanup_failed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const runID = "batch-warning"
			panel := newPanelCleanupWarningTestPanel(t, runID)
			startedAt := time.Now()
			panel.operations[panelOperationCleanupBatch] = &panelOperationState{
				Running:   true,
				StartedAt: &startedAt,
				UpdatedAt: &startedAt,
				RunID:     runID,
				RunIDs:    []string{runID},
				Output:    []string{"[control-panel] cleanup batch started"},
			}
			warning := manualLinodeCleanupWarning(errors.New("HA 1 cluster cluster-one: management API unavailable"))
			panel.appendOperationOutput(panelOperationCleanup, cleanupWarningLogLine(warning))

			spec := panelCommandSpec{
				Operation:   panelOperationCleanup,
				DisplayName: "cleanup",
				SuccessLine: "[control-panel] Cleanup completed successfully",
				BatchChild:  true,
			}
			panel.finishPanelCommand(spec, tc.managementErr)
			panel.finishCleanupBatchItem(runID, tc.managementErr)
			panel.finishCleanupBatch()

			snapshot := panel.snapshotCleanupBatch()
			if snapshot.Progress.Succeeded != tc.wantSucceeded || snapshot.Progress.Failed != tc.wantFailed {
				t.Fatalf("batch progress = %#v, want succeeded=%d failed=%d", snapshot.Progress, tc.wantSucceeded, tc.wantFailed)
			}
			if !strings.Contains(snapshot.Warning, "Run "+runID+":") || !strings.Contains(snapshot.Warning, "cluster-one") {
				t.Fatalf("batch warning = %q", snapshot.Warning)
			}
			if tc.managementErr == nil {
				if snapshot.Error != "" || !strings.Contains(strings.Join(snapshot.Output, "\n"), "completed with warnings") {
					t.Fatalf("successful warning batch snapshot = %#v", snapshot)
				}
			} else if snapshot.Error == "" || len(snapshot.Failures) != 1 {
				t.Fatalf("failed warning batch snapshot = %#v", snapshot)
			}
			record, ok := panel.readRunRecord(runID)
			if ok != tc.wantRecord {
				t.Fatalf("run record present = %v, want %v", ok, tc.wantRecord)
			}
			if ok && record.Status != tc.wantRecordState {
				t.Fatalf("run record status = %q, want %q", record.Status, tc.wantRecordState)
			}

			loaded := &localControlPanel{}
			loaded.loadPersistedOperations(false)
			if got := loaded.snapshotCleanupBatch().Warning; got != snapshot.Warning {
				t.Fatalf("persisted batch warning = %q, want %q", got, snapshot.Warning)
			}
		})
	}
}
