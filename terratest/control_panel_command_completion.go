package test

import (
	"bufio"
	"fmt"
	"io"
	"sync"
	"time"
)

func (p *localControlPanel) capturePanelCommandStream(wg *sync.WaitGroup, operation panelOperationName, reader io.Reader) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		p.appendOperationOutput(operation, scanner.Text())
	}
}

func (p *localControlPanel) appendOperationOutput(operation panelOperationName, line string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	op := p.operationLocked(operation)
	op.Output = append(op.Output, line)
	if len(op.Output) > 500 {
		op.Output = append([]string(nil), op.Output[len(op.Output)-500:]...)
	}
	if warning := cleanupWarningFromOutputLine(line); warning != "" {
		op.Warning = appendPanelWarning(op.Warning, warning)
	}
	now := time.Now()
	op.UpdatedAt = &now
	p.mirrorCleanupBatchOutputLocked(operation, op.RunID, line, now)
	p.persistOperationsLocked()
}

func (p *localControlPanel) finishPanelCommand(spec panelCommandSpec, err error) {
	if spec.Completion != nil {
		defer func() { spec.Completion <- err }()
	}
	var shouldRunAfterSuccess bool
	var runID string
	p.mu.Lock()
	op := p.operationLocked(spec.Operation)
	runID = op.RunID
	canceled := op.CancelRequested
	op.Running = false
	op.PID = 0
	finishedAt := time.Now()
	op.FinishedAt = &finishedAt
	op.UpdatedAt = &finishedAt
	p.clearCleanupBatchPIDLocked(spec.Operation, op.RunID, finishedAt)
	if err != nil {
		op.Error = err.Error()
		op.Output = append(op.Output, fmt.Sprintf("[control-panel] %s finished with error: %s", panelDisplayTitle(spec.DisplayName), err.Error()))
		p.persistOperationsLocked()
		p.mu.Unlock()
		if !spec.BatchChild {
			p.updateRunStatusAfterOperation(spec.Operation, runID, err)
		}
		return
	}

	op.Error = ""
	op.Output = append(op.Output, spec.SuccessLine)
	p.persistOperationsLocked()
	shouldRunAfterSuccess = spec.AfterSuccess != nil
	p.mu.Unlock()

	if !spec.BatchChild {
		p.updateRunStatusAfterOperation(spec.Operation, runID, nil)
	}
	if !spec.BatchChild && (spec.Operation == panelOperationCleanup || spec.Operation == panelOperationLinodeCleanup) {
		p.finishRunLabCleanup(runID, spec.LabCleanup, spec.Operation, canceled)
	}
	if shouldRunAfterSuccess {
		spec.AfterSuccess()
	}
}

func (p *localControlPanel) setOperationPID(operation panelOperationName, pid int) {
	p.mu.Lock()
	op := p.operationLocked(operation)
	op.PID = pid
	now := time.Now()
	op.UpdatedAt = &now
	op.Output = append(op.Output, fmt.Sprintf("[control-panel] Process pid %d", pid))
	interruptForBatchCancel := p.mirrorCleanupBatchPIDLocked(operation, op.RunID, pid, now)
	p.persistOperationsLocked()
	p.mu.Unlock()

	if interruptForBatchCancel {
		if err := interruptProcessTree(pid); err != nil {
			p.appendOperationOutput(operation, fmt.Sprintf("[control-panel] Failed to interrupt cleanup after batch cancellation: %s", err))
		}
	}
}

func (p *localControlPanel) updateRunStatusAfterOperation(operation panelOperationName, runID string, operationErr error) {
	success := operationErr == nil
	switch operation {
	case panelOperationSetup, panelOperationLinodeSetup:
		if success {
			p.updateRunRecordStatus(runID, "setup_complete")
			return
		}
		p.updateRunRecordStatus(runID, "setup_failed")
	case panelOperationReadiness:
		if success {
			p.updateRunRecordStatus(runID, "ready")
			return
		}
		p.updateRunRecordStatus(runID, "readiness_failed")
	case panelOperationDownstream:
		if success {
			p.updateRunDownstreamStatus(runID, panelDownstreamStatusReady, "")
			return
		}
		errorMessage := "downstream provisioning failed; management Rancher remains ready"
		if operationErr != nil {
			errorMessage = operationErr.Error()
		}
		p.updateRunDownstreamStatus(runID, panelDownstreamStatusFailed, errorMessage)
	case panelOperationCleanup, panelOperationLinodeCleanup:
		if success {
			p.removeRunRecord(runID)
			return
		}
		p.updateRunRecordStatus(runID, "cleanup_failed")
	}
}

func (p *localControlPanel) operationLocked(name panelOperationName) *panelOperationState {
	if p.operations == nil {
		p.operations = newPanelOperations()
	}
	op, ok := p.operations[name]
	if !ok {
		op = &panelOperationState{}
		p.operations[name] = op
	}
	return op
}

func (p *localControlPanel) anyOperationRunningLocked() bool {
	p.rancherOps.mu.Lock()
	rancherBusy := len(p.rancherOps.active) > 0
	p.rancherOps.mu.Unlock()
	if rancherBusy {
		return true
	}
	for _, name := range allPanelOperationNames() {
		op := p.operationLocked(name)
		if name != panelOperationCleanupBatch && op.Running && !op.InProcess && op.PID > 0 && !processAlive(op.PID) {
			p.markOperationStaleLocked(name, op,
				"operation process exited before reporting completion",
				"[control-panel] Operation process exited before reporting completion; status marked stale.",
			)
			p.persistOperationsLocked()
			continue
		}
		if op.Running {
			return true
		}
	}
	return false
}

func (p *localControlPanel) conflictingOperationRunningLocked(operation panelOperationName) bool {
	p.rancherOps.mu.Lock()
	rancherBusy := len(p.rancherOps.active) > 0
	p.rancherOps.mu.Unlock()
	if rancherBusy {
		return true
	}
	if p.operationLocked(panelOperationAWSCleanup).Running {
		return true
	}
	for _, name := range conflictingPanelOperationNames(operation) {
		op := p.operationLocked(name)
		if op.Running && !op.InProcess && op.PID > 0 && !processAlive(op.PID) {
			p.markOperationStaleLocked(name, op,
				"operation process exited before reporting completion",
				"[control-panel] Operation process exited before reporting completion; status marked stale.",
			)
			p.persistOperationsLocked()
			continue
		}
		if op.Running {
			return true
		}
	}
	return false
}

func (p *localControlPanel) anyOperationRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.anyOperationRunningLocked()
}

func (p *localControlPanel) runningOperationNameLocked() string {
	for _, name := range allPanelOperationNames() {
		if p.operationLocked(name).Running {
			return string(name)
		}
	}
	return "operation"
}

func (p *localControlPanel) runningConflictingOperationNameLocked(operation panelOperationName) string {
	p.rancherOps.mu.Lock()
	rancherBusy := len(p.rancherOps.active) > 0
	p.rancherOps.mu.Unlock()
	if rancherBusy {
		return "Rancher upgrade or downstream creation"
	}
	if p.operationLocked(panelOperationAWSCleanup).Running {
		return "AWS inventory cleanup"
	}
	for _, name := range conflictingPanelOperationNames(operation) {
		if p.operationLocked(name).Running {
			return string(name)
		}
	}
	return "operation"
}

func (p *localControlPanel) runningOperationName() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.runningOperationNameLocked()
}
