package test

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var errCleanupBatchCanceled = errors.New("cleanup batch cancellation requested")

type panelCleanupBatchFailure struct {
	RunID string `json:"runId"`
	Error string `json:"error"`
}

type panelCleanupBatchProgress struct {
	Processed int `json:"processed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Remaining int `json:"remaining"`
	Total     int `json:"total"`
}

type panelCleanupBatchSnapshot struct {
	Running         bool                       `json:"running"`
	PID             int                        `json:"pid,omitempty"`
	StartedAt       *time.Time                 `json:"startedAt,omitempty"`
	FinishedAt      *time.Time                 `json:"finishedAt,omitempty"`
	Error           string                     `json:"error,omitempty"`
	Output          []string                   `json:"output"`
	RunIDs          []string                   `json:"runIds"`
	CompletedRunIDs []string                   `json:"completedRunIds"`
	Failures        []panelCleanupBatchFailure `json:"failures"`
	CancelRequested bool                       `json:"cancelRequested"`
	CurrentRunID    string                     `json:"currentRunId,omitempty"`
	Progress        panelCleanupBatchProgress  `json:"progress"`
	UpdatedAt       *time.Time                 `json:"updatedAt,omitempty"`
}

func isCloudPanelOperation(operation panelOperationName) bool {
	switch operation {
	case panelOperationSetup,
		panelOperationReadiness,
		panelOperationDownstream,
		panelOperationCleanup,
		panelOperationLinodeSetup,
		panelOperationLinodeCleanup,
		panelOperationCleanupBatch:
		return true
	default:
		return false
	}
}

func (p *localControlPanel) resolveCleanupBatchRunIDs(requested []string, all bool) ([]string, error) {
	if all {
		records := p.listRunRecords()
		if len(records) == 0 {
			return nil, fmt.Errorf("cleanup batch requires at least one recorded run")
		}
		runIDs := make([]string, 0, len(records))
		for _, record := range records {
			runIDs = append(runIDs, safeRunPathSegment(record.RunID))
		}
		return runIDs, nil
	}

	if len(requested) == 0 {
		return nil, fmt.Errorf("cleanup batch requires at least one runId")
	}

	seen := make(map[string]string, len(requested))
	runIDs := make([]string, 0, len(requested))
	for _, rawRunID := range requested {
		trimmed := strings.TrimSpace(rawRunID)
		if trimmed == "" {
			return nil, fmt.Errorf("cleanup batch runIds cannot contain an empty value")
		}
		runID := safeRunPathSegment(trimmed)
		if previous, ok := seen[runID]; ok {
			return nil, fmt.Errorf("cleanup batch runIds %q and %q normalize to the same run %q", previous, rawRunID, runID)
		}
		seen[runID] = rawRunID

		record, ok := p.readRunRecord(runID)
		if !ok {
			return nil, fmt.Errorf("cleanup batch requires a recorded run: %s", rawRunID)
		}
		runIDs = append(runIDs, safeRunPathSegment(record.RunID))
	}
	return runIDs, nil
}

func (p *localControlPanel) startCleanupBatch(runIDs []string) error {
	if len(runIDs) == 0 {
		return fmt.Errorf("cleanup batch requires at least one recorded run")
	}

	p.mu.Lock()
	for _, operation := range []panelOperationName{
		panelOperationSetup,
		panelOperationReadiness,
		panelOperationDownstream,
		panelOperationCleanup,
		panelOperationLinodeSetup,
		panelOperationLinodeCleanup,
		panelOperationCleanupBatch,
	} {
		op := p.operationLocked(operation)
		if operation != panelOperationCleanupBatch && op.Running && op.PID > 0 && !processAlive(op.PID) {
			p.markOperationStaleLocked(operation, op,
				"operation process exited before reporting completion",
				"[control-panel] Operation process exited before reporting completion; status marked stale.",
			)
			p.persistOperationsLocked()
		}
		if op.Running {
			p.mu.Unlock()
			return fmt.Errorf("cannot start cleanup batch while %s is running", operation)
		}
	}

	now := time.Now()
	batch := p.operationLocked(panelOperationCleanupBatch)
	*batch = panelOperationState{
		Running:         true,
		StartedAt:       &now,
		UpdatedAt:       &now,
		Output:          []string{fmt.Sprintf("[control-panel] Starting cleanup batch for %d recorded run(s)", len(runIDs))},
		RunIDs:          append([]string(nil), runIDs...),
		CompletedRunIDs: []string{},
		Failures:        []panelCleanupBatchFailure{},
	}
	p.persistOperationsLocked()
	p.mu.Unlock()

	go p.runCleanupBatch(append([]string(nil), runIDs...))
	return nil
}

func (p *localControlPanel) runCleanupBatch(runIDs []string) {
	for index, runID := range runIDs {
		p.mu.Lock()
		batch := p.operationLocked(panelOperationCleanupBatch)
		if !batch.Running || batch.CancelRequested {
			p.mu.Unlock()
			break
		}
		batch.RunID = runID
		batch.PID = 0
		now := time.Now()
		batch.UpdatedAt = &now
		batch.Output = appendBatchOutput(batch.Output, fmt.Sprintf("[control-panel] Destroying run %s (%d of %d)", runID, index+1, len(runIDs)))
		p.persistOperationsLocked()
		p.mu.Unlock()

		err := p.runCleanupBatchItem(runID)
		if errors.Is(err, errCleanupBatchCanceled) {
			break
		}
		p.finishCleanupBatchItem(runID, err)

		p.mu.Lock()
		canceled := p.operationLocked(panelOperationCleanupBatch).CancelRequested
		p.mu.Unlock()
		if canceled {
			break
		}
	}

	p.finishCleanupBatch()
}

func (p *localControlPanel) runCleanupBatchItem(runID string) error {
	if p.cleanupBatchRunner != nil {
		return p.cleanupBatchRunner(runID)
	}

	completion := make(chan error, 1)
	if err := p.startCleanupForRunWithBatch(runID, true, completion); err != nil {
		return err
	}
	return <-completion
}

func (p *localControlPanel) finishCleanupBatchItem(runID string, runErr error) {
	if runErr == nil {
		p.removeRunRecord(runID)
	} else {
		p.updateRunRecordStatus(runID, "cleanup_failed")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	batch := p.operationLocked(panelOperationCleanupBatch)
	now := time.Now()
	batch.PID = 0
	batch.RunID = ""
	batch.UpdatedAt = &now
	if runErr == nil {
		batch.CompletedRunIDs = append(batch.CompletedRunIDs, runID)
		batch.Output = appendBatchOutput(batch.Output, fmt.Sprintf("[control-panel] Cleanup completed successfully for run %s", runID))
	} else {
		failure := panelCleanupBatchFailure{RunID: runID, Error: runErr.Error()}
		batch.Failures = append(batch.Failures, failure)
		batch.Output = appendBatchOutput(batch.Output, fmt.Sprintf("[control-panel] Cleanup failed for run %s: %s", runID, runErr))
	}
	p.persistOperationsLocked()
}

func (p *localControlPanel) finishCleanupBatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	batch := p.operationLocked(panelOperationCleanupBatch)
	if !batch.Running {
		return
	}

	processed := len(batch.CompletedRunIDs) + len(batch.Failures)
	finishedAt := time.Now()
	batch.Running = false
	batch.PID = 0
	batch.RunID = ""
	batch.FinishedAt = &finishedAt
	batch.UpdatedAt = &finishedAt
	switch {
	case batch.CancelRequested:
		batch.Error = fmt.Sprintf("cleanup batch canceled after %d of %d run(s)", processed, len(batch.RunIDs))
		batch.Output = appendBatchOutput(batch.Output, "[control-panel] Cleanup batch canceled; queued run records were preserved")
	case len(batch.Failures) > 0:
		batch.Error = fmt.Sprintf("cleanup batch finished with %d failure(s) out of %d run(s)", len(batch.Failures), len(batch.RunIDs))
		batch.Output = appendBatchOutput(batch.Output, "[control-panel] Cleanup batch finished with errors; failed run records were preserved")
	default:
		batch.Error = ""
		batch.Output = appendBatchOutput(batch.Output, "[control-panel] Cleanup batch completed successfully")
	}
	p.persistOperationsLocked()
}

func (p *localControlPanel) snapshotCleanupBatch() panelCleanupBatchSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	batch := p.operationLocked(panelOperationCleanupBatch)

	completed := append([]string(nil), batch.CompletedRunIDs...)
	failures := append([]panelCleanupBatchFailure(nil), batch.Failures...)
	runIDs := append([]string(nil), batch.RunIDs...)
	output := append([]string(nil), batch.Output...)
	if completed == nil {
		completed = []string{}
	}
	if failures == nil {
		failures = []panelCleanupBatchFailure{}
	}
	if runIDs == nil {
		runIDs = []string{}
	}
	if output == nil {
		output = []string{}
	}
	processed := len(completed) + len(failures)
	remaining := len(runIDs) - processed
	if remaining < 0 {
		remaining = 0
	}

	return panelCleanupBatchSnapshot{
		Running:         batch.Running,
		PID:             batch.PID,
		StartedAt:       batch.StartedAt,
		FinishedAt:      batch.FinishedAt,
		Error:           batch.Error,
		Output:          output,
		RunIDs:          runIDs,
		CompletedRunIDs: completed,
		Failures:        failures,
		CancelRequested: batch.CancelRequested,
		CurrentRunID:    batch.RunID,
		Progress: panelCleanupBatchProgress{
			Processed: processed,
			Succeeded: len(completed),
			Failed:    len(failures),
			Remaining: remaining,
			Total:     len(runIDs),
		},
		UpdatedAt: batch.UpdatedAt,
	}
}

func (p *localControlPanel) clearCompletedCleanupBatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	batch := p.operationLocked(panelOperationCleanupBatch)
	if batch.Running {
		return
	}
	*batch = panelOperationState{}
	p.persistOperationsLocked()
}

func (p *localControlPanel) mirrorCleanupBatchOutputLocked(operation panelOperationName, runID string, line string, now time.Time) {
	if operation != panelOperationCleanup && operation != panelOperationLinodeCleanup {
		return
	}
	batch := p.operationLocked(panelOperationCleanupBatch)
	if !batch.Running || !sameRunID(batch.RunID, runID) {
		return
	}
	batch.Output = appendBatchOutput(batch.Output, line)
	batch.UpdatedAt = &now
}

func (p *localControlPanel) mirrorCleanupBatchPIDLocked(operation panelOperationName, runID string, pid int, now time.Time) bool {
	if operation != panelOperationCleanup && operation != panelOperationLinodeCleanup {
		return false
	}
	batch := p.operationLocked(panelOperationCleanupBatch)
	if !batch.Running || !sameRunID(batch.RunID, runID) {
		return false
	}
	batch.PID = pid
	batch.UpdatedAt = &now
	return batch.CancelRequested
}

func (p *localControlPanel) clearCleanupBatchPIDLocked(operation panelOperationName, runID string, now time.Time) {
	if operation != panelOperationCleanup && operation != panelOperationLinodeCleanup {
		return
	}
	batch := p.operationLocked(panelOperationCleanupBatch)
	if !batch.Running || !sameRunID(batch.RunID, runID) {
		return
	}
	batch.PID = 0
	batch.UpdatedAt = &now
}

func appendBatchOutput(output []string, line string) []string {
	output = append(output, line)
	if len(output) > 2000 {
		return append([]string(nil), output[len(output)-2000:]...)
	}
	return output
}
