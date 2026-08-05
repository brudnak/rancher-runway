package test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func newCleanupBatchTestPanel(t *testing.T, records ...panelRunRecord) *localControlPanel {
	t.Helper()
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	panel := &localControlPanel{
		token:      "token",
		repoRoot:   workspace,
		testDir:    workspace,
		operations: newPanelOperations(),
	}
	for _, record := range records {
		panel.writeRunRecord(record)
	}
	return panel
}

func waitForCleanupBatch(t *testing.T, panel *localControlPanel) panelCleanupBatchSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := panel.snapshotCleanupBatch()
		if !snapshot.Running && snapshot.FinishedAt != nil {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cleanup batch did not finish: %#v", panel.snapshotCleanupBatch())
	return panelCleanupBatchSnapshot{}
}

func TestResolveCleanupBatchRunIDsValidatesSelection(t *testing.T) {
	now := time.Now()
	panel := newCleanupBatchTestPanel(t,
		panelRunRecord{RunID: "run-a", CreatedAt: now.Add(-time.Minute)},
		panelRunRecord{RunID: "run-b", CreatedAt: now},
	)

	selected, err := panel.resolveCleanupBatchRunIDs([]string{"RUN-A", "run-b"}, false)
	if err != nil {
		t.Fatalf("resolve selected cleanup batch: %v", err)
	}
	if want := []string{"run-a", "run-b"}; !reflect.DeepEqual(selected, want) {
		t.Fatalf("selected run ids = %#v, want %#v", selected, want)
	}

	all, err := panel.resolveCleanupBatchRunIDs(nil, true)
	if err != nil {
		t.Fatalf("resolve all cleanup batch: %v", err)
	}
	if want := []string{"run-b", "run-a"}; !reflect.DeepEqual(all, want) {
		t.Fatalf("all run ids = %#v, want frozen newest-first snapshot %#v", all, want)
	}

	tests := []struct {
		name      string
		requested []string
		wantError string
	}{
		{name: "empty", requested: []string{}, wantError: "at least one runId"},
		{name: "empty item", requested: []string{"run-a", " "}, wantError: "empty value"},
		{name: "unknown", requested: []string{"missing"}, wantError: "recorded run"},
		{name: "normalization collision", requested: []string{"Run A", "run-a"}, wantError: "normalize to the same run"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := panel.resolveCleanupBatchRunIDs(tc.requested, false)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantError)
			}
		})
	}
}

func TestHandleCleanupBatchValidatesShapeAndConfirmation(t *testing.T) {
	panel := newCleanupBatchTestPanel(t, panelRunRecord{RunID: "run-a", CreatedAt: time.Now()})
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "all and selected", body: `{"all":true,"runIds":["run-a"],"confirm":"destroy all"}`, wantError: "mutually exclusive"},
		{name: "legacy and selected", body: `{"runId":"run-a","runIds":["run-a"],"confirm":"destroy selected"}`, wantError: "cannot be combined"},
		{name: "wrong all confirmation", body: `{"all":true,"confirm":"destroy"}`, wantError: "destroy all"},
		{name: "wrong selected confirmation", body: `{"runIds":["run-a"],"confirm":"destroy"}`, wantError: "destroy selected"},
		{name: "empty selected", body: `{"runIds":[],"confirm":"destroy selected"}`, wantError: "at least one runId"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/cleanup", strings.NewReader(tc.body))
			request.Header.Set("X-Control-Panel-Token", "token")
			recorder := httptest.NewRecorder()

			panel.handleCleanup(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tc.wantError) {
				t.Fatalf("body = %q, want containing %q", recorder.Body.String(), tc.wantError)
			}
		})
	}
}

func TestCleanupBatchRunsSequentiallyAndContinuesAfterFailure(t *testing.T) {
	now := time.Now()
	panel := newCleanupBatchTestPanel(t,
		panelRunRecord{RunID: "run-a", CreatedAt: now.Add(-2 * time.Minute), Status: "setup_complete"},
		panelRunRecord{RunID: "run-b", CreatedAt: now.Add(-time.Minute), Status: "setup_complete"},
		panelRunRecord{RunID: "run-c", CreatedAt: now, Status: "setup_complete"},
	)

	var mu sync.Mutex
	called := []string{}
	panel.cleanupBatchRunner = func(runID string) error {
		mu.Lock()
		called = append(called, runID)
		mu.Unlock()
		if runID == "run-b" {
			return errors.New("simulated destroy failure")
		}
		return nil
	}

	if err := panel.startCleanupBatch([]string{"run-a", "run-b", "run-c"}); err != nil {
		t.Fatalf("start cleanup batch: %v", err)
	}
	snapshot := waitForCleanupBatch(t, panel)

	mu.Lock()
	gotCalled := append([]string(nil), called...)
	mu.Unlock()
	if want := []string{"run-a", "run-b", "run-c"}; !reflect.DeepEqual(gotCalled, want) {
		t.Fatalf("runner calls = %#v, want sequential order %#v", gotCalled, want)
	}
	if want := []string{"run-a", "run-c"}; !reflect.DeepEqual(snapshot.CompletedRunIDs, want) {
		t.Fatalf("completed run ids = %#v, want %#v", snapshot.CompletedRunIDs, want)
	}
	if len(snapshot.Failures) != 1 || snapshot.Failures[0].RunID != "run-b" || !strings.Contains(snapshot.Failures[0].Error, "simulated") {
		t.Fatalf("failures = %#v, want run-b failure", snapshot.Failures)
	}
	if snapshot.Progress.Processed != 3 || snapshot.Progress.Succeeded != 2 || snapshot.Progress.Failed != 1 || snapshot.Progress.Remaining != 0 || snapshot.Progress.Total != 3 {
		t.Fatalf("progress = %#v, want 3 processed/2 succeeded/1 failed", snapshot.Progress)
	}
	if snapshot.Error == "" {
		t.Fatal("expected aggregate batch error")
	}
	if _, ok := panel.readRunRecord("run-a"); ok {
		t.Fatal("expected successful run-a record to be removed")
	}
	failed, ok := panel.readRunRecord("run-b")
	if !ok || failed.Status != "cleanup_failed" {
		t.Fatalf("failed run record = %#v, %v; want preserved cleanup_failed", failed, ok)
	}
	if _, ok := panel.readRunRecord("run-c"); ok {
		t.Fatal("expected successful run-c record to be removed")
	}

	loaded := &localControlPanel{}
	loaded.loadPersistedOperations(false)
	persisted := loaded.snapshotCleanupBatch()
	if !reflect.DeepEqual(persisted.RunIDs, snapshot.RunIDs) || !reflect.DeepEqual(persisted.CompletedRunIDs, snapshot.CompletedRunIDs) || !reflect.DeepEqual(persisted.Failures, snapshot.Failures) {
		t.Fatalf("persisted batch = %#v, want %#v", persisted, snapshot)
	}
}

func TestCleanupBatchBlocksCloudOperationsAndCancelPreservesQueue(t *testing.T) {
	now := time.Now()
	panel := newCleanupBatchTestPanel(t,
		panelRunRecord{RunID: "run-a", CreatedAt: now.Add(-time.Minute), Status: "setup_complete"},
		panelRunRecord{RunID: "run-b", CreatedAt: now, Status: "setup_complete"},
	)

	started := make(chan string, 1)
	release := make(chan struct{})
	panel.cleanupBatchRunner = func(runID string) error {
		started <- runID
		<-release
		return nil
	}

	if err := panel.startCleanupBatch([]string{"run-a", "run-b"}); err != nil {
		t.Fatalf("start cleanup batch: %v", err)
	}
	if got := <-started; got != "run-a" {
		t.Fatalf("first runner call = %q, want run-a", got)
	}
	if err := panel.startCleanupForRun("run-b"); err == nil || !strings.Contains(err.Error(), "cleanup batch") {
		t.Fatalf("concurrent cleanup error = %v, want cleanup batch conflict", err)
	}
	if !panel.anyOperationRunning() || panel.runningOperationName() != string(panelOperationCleanupBatch) {
		t.Fatalf("batch should hold global lifecycle lock, running operation = %q", panel.runningOperationName())
	}
	if err := panel.abortOperation(panelOperationCleanupBatch, "run-a"); err != nil {
		t.Fatalf("request batch cancellation: %v", err)
	}
	close(release)
	snapshot := waitForCleanupBatch(t, panel)

	if !snapshot.CancelRequested || !strings.Contains(snapshot.Error, "canceled") {
		t.Fatalf("canceled snapshot = %#v", snapshot)
	}
	if snapshot.Progress.Processed != 1 || snapshot.Progress.Remaining != 1 {
		t.Fatalf("canceled progress = %#v, want one processed and one queued", snapshot.Progress)
	}
	if _, ok := panel.readRunRecord("run-a"); ok {
		t.Fatal("expected completed current run to be removed")
	}
	if _, ok := panel.readRunRecord("run-b"); !ok {
		t.Fatal("expected canceled queued run to remain recorded")
	}
	select {
	case extra := <-started:
		t.Fatalf("queued runner unexpectedly started for %s", extra)
	default:
	}
}

func TestCleanupBatchRejectsActiveCloudOperation(t *testing.T) {
	panel := newCleanupBatchTestPanel(t, panelRunRecord{RunID: "run-a", CreatedAt: time.Now()})
	panel.operations[panelOperationReadiness].Running = true

	err := panel.startCleanupBatch([]string{"run-a"})
	if err == nil || !strings.Contains(err.Error(), "readiness") {
		t.Fatalf("error = %v, want readiness conflict", err)
	}
}

func TestCleanupBatchChildRefusesPidlessCancellationRace(t *testing.T) {
	panel := newCleanupBatchTestPanel(t, panelRunRecord{RunID: "run-a", CreatedAt: time.Now()})
	panel.operations[panelOperationCleanupBatch] = &panelOperationState{
		Running:         true,
		RunID:           "run-a",
		RunIDs:          []string{"run-a"},
		CancelRequested: true,
	}

	err := panel.startPanelCommand(panelCommandSpec{
		Operation:   panelOperationCleanup,
		DisplayName: "cleanup",
		TestName:    "TestHACleanup",
		Timeout:     "30m",
		RunID:       "run-a",
		BatchChild:  true,
	})
	if !errors.Is(err, errCleanupBatchCanceled) {
		t.Fatalf("error = %v, want cleanup batch cancellation", err)
	}
	if panel.operations[panelOperationCleanup].Running {
		t.Fatal("cleanup child started after cancellation was requested")
	}
}

func TestHandleAbortOperationAcceptsCleanupBatchName(t *testing.T) {
	panel := newCleanupBatchTestPanel(t)
	panel.operations[panelOperationCleanupBatch] = &panelOperationState{
		Running: true,
		RunIDs:  []string{"run-a"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/operations/abort", strings.NewReader(`{"operation":"cleanupBatch","confirm":"stop"}`))
	request.Header.Set("X-Control-Panel-Token", "token")
	recorder := httptest.NewRecorder()

	panel.handleAbortOperation(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !panel.operations[panelOperationCleanupBatch].CancelRequested {
		t.Fatal("expected cleanup batch cancellation to be recorded")
	}
}

func TestClearCompletedCleanupBatchDoesNotClearRunningBatch(t *testing.T) {
	panel := newCleanupBatchTestPanel(t)
	finishedAt := time.Now()
	panel.operations[panelOperationCleanupBatch] = &panelOperationState{
		FinishedAt:      &finishedAt,
		Error:           "one failed",
		RunIDs:          []string{"run-a"},
		CompletedRunIDs: []string{},
		Failures:        []panelCleanupBatchFailure{{RunID: "run-a", Error: "failed"}},
	}

	panel.clearCompletedCleanupBatch()
	if snapshot := panel.snapshotCleanupBatch(); snapshot.FinishedAt != nil || len(snapshot.RunIDs) != 0 || len(snapshot.Failures) != 0 {
		t.Fatalf("completed cleanup batch was not cleared: %#v", snapshot)
	}

	panel.operations[panelOperationCleanupBatch] = &panelOperationState{
		Running: true,
		RunIDs:  []string{"run-b"},
	}
	panel.clearCompletedCleanupBatch()
	if snapshot := panel.snapshotCleanupBatch(); !snapshot.Running || !reflect.DeepEqual(snapshot.RunIDs, []string{"run-b"}) {
		t.Fatalf("running cleanup batch was cleared: %#v", snapshot)
	}
}
