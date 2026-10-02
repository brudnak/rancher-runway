package test

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func panelDisplayTitle(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Operation"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func panelCommandSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func interruptProcessTree(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process id %d", pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGINT); err == nil {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGINT); err != nil {
		return fmt.Errorf("failed to interrupt process %d: %w", pid, err)
	}
	return nil
}

func (p *localControlPanel) lifecycleStatePath() string {
	return filepath.Join(automationOutputDir(), "control-panel", "lifecycle-state.json")
}

func (p *localControlPanel) loadPersistedOperations(markStaleRunning bool) {
	path := p.lifecycleStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[control-panel] Failed to read lifecycle state %s: %v", path, err)
		}
		return
	}

	var persisted map[panelOperationName]*panelOperationState
	if err := json.Unmarshal(data, &persisted); err != nil {
		log.Printf("[control-panel] Failed to parse lifecycle state %s: %v", path, err)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.operations = newPanelOperations()
	for name, op := range persisted {
		if op == nil {
			continue
		}
		p.operations[name] = op
	}
	if markStaleRunning {
		p.markStaleRunningOperationsLocked()
		p.clearCompletedCleanupSuccessLocked()
		p.persistOperationsLocked()
	}
}

func (p *localControlPanel) markStaleRunningOperationsLocked() {
	for _, name := range allPanelOperationNames() {
		op := p.operationLocked(name)
		if !op.Running {
			continue
		}
		if name == panelOperationCleanupBatch {
			p.markOperationStaleLocked(name, op,
				"panel restarted before the cleanup batch reported completion",
				"[control-panel] Panel restarted before the cleanup batch reported completion; queued run records were preserved.",
			)
			continue
		}
		if op.PID > 0 && processAlive(op.PID) {
			now := time.Now()
			op.Output = append(op.Output, "[control-panel] Reattached to the still-running lifecycle worker after panel restart.")
			op.UpdatedAt = &now
			continue
		}
		p.markOperationStaleLocked(name, op,
			"panel restarted before this operation reported completion",
			"[control-panel] Panel restarted before this operation reported completion; status marked stale.",
		)
	}
}

func (p *localControlPanel) markOperationStaleLocked(name panelOperationName, op *panelOperationState, errorMessage, outputLine string) {
	now := time.Now()
	op.Running = false
	op.PID = 0
	op.FinishedAt = &now
	op.UpdatedAt = &now
	op.Error = errorMessage
	op.Output = append(op.Output, outputLine)
	if name == panelOperationDownstream {
		p.updateRunDownstreamStatus(op.RunID, panelDownstreamStatusFailed, errorMessage)
	}
}

func (p *localControlPanel) clearCompletedCleanupSuccessLocked() {
	for _, name := range []panelOperationName{panelOperationCleanup, panelOperationLinodeCleanup} {
		op := p.operationLocked(name)
		if op.Running || op.FinishedAt == nil || strings.TrimSpace(op.Error) != "" || strings.TrimSpace(op.Warning) != "" {
			continue
		}
		*op = panelOperationState{}
	}
}

func (p *localControlPanel) persistOperationsLocked() {
	path := p.lifecycleStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[control-panel] Failed to create lifecycle state directory: %v", err)
		return
	}

	data, err := json.MarshalIndent(p.operations, "", "  ")
	if err != nil {
		log.Printf("[control-panel] Failed to encode lifecycle state: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Printf("[control-panel] Failed to write lifecycle state %s: %v", path, err)
	}
}
