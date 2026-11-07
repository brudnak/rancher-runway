package test

import (
	"fmt"
	"sort"
	"strings"
)

const maxConcurrentProvisioningRuns = 2

func panelOperationBase(name panelOperationName) panelOperationName {
	base, _, _ := strings.Cut(string(name), ":")
	return panelOperationName(base)
}

func panelOperationForRun(name panelOperationName, runID string) panelOperationName {
	name = panelOperationBase(name)
	if !isProvisioningPanelOperation(name) || strings.TrimSpace(runID) == "" {
		return name
	}
	return panelOperationName(string(name) + ":" + safeRunPathSegment(runID))
}

func isProvisioningPanelOperation(name panelOperationName) bool {
	switch panelOperationBase(name) {
	case panelOperationSetup, panelOperationLinodeSetup, panelOperationReadiness, panelOperationDownstream:
		return true
	default:
		return false
	}
}

func isDestructivePanelOperation(name panelOperationName) bool {
	switch panelOperationBase(name) {
	case panelOperationCleanup, panelOperationLinodeCleanup, panelOperationCleanupBatch, panelOperationAWSCleanup:
		return true
	default:
		return false
	}
}

func panelOperationActive(op *panelOperationState) bool {
	return op != nil && (op.Running || op.Completing)
}

// Include persisted legacy entries and every run-specific operation.
func (p *localControlPanel) panelOperationNamesLocked() []panelOperationName {
	for _, name := range allPanelOperationNames() {
		p.operationLocked(name)
	}
	names := make([]panelOperationName, 0, len(p.operations))
	for name := range p.operations {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

func (p *localControlPanel) refreshOperationLocked(name panelOperationName, op *panelOperationState) {
	if op.Running && name != panelOperationCleanupBatch && !op.InProcess && op.PID > 0 && !processAlive(op.PID) {
		p.markOperationStaleLocked(name, op,
			"operation process exited before reporting completion",
			"[control-panel] Operation process exited before reporting completion; status marked stale.")
		p.persistOperationsLocked()
	}
}

// A completing operation retains its slot while its record is saved and the
// next stage is admitted. Other runs cannot take that slot between stages.
func (p *localControlPanel) provisioningAdmissionErrorLocked(runID string) error {
	p.rancherOps.mu.Lock()
	rancherBusy := len(p.rancherOps.active) > 0
	p.rancherOps.mu.Unlock()
	if rancherBusy {
		return fmt.Errorf("Rancher upgrade or downstream creation is already running")
	}
	runs := make(map[string]bool)
	for _, name := range p.panelOperationNamesLocked() {
		op := p.operations[name]
		p.refreshOperationLocked(name, op)
		if !panelOperationActive(op) {
			continue
		}
		if isDestructivePanelOperation(name) {
			return fmt.Errorf("%s is already running", panelOperationBase(name))
		}
		if !isProvisioningPanelOperation(name) {
			continue
		}
		if runID != "" && sameRunID(runID, op.RunID) {
			if op.Running {
				return fmt.Errorf("%s is already running for run %s", panelOperationBase(name), op.RunID)
			}
			runs[op.RunID] = true
			continue
		}
		if name == panelOperationBase(name) {
			return fmt.Errorf("%s is already running; wait for this earlier run before starting another", name)
		}
		runs[op.RunID] = true
	}
	if runID != "" && runs[runID] {
		return nil
	}
	if len(runs) >= maxConcurrentProvisioningRuns {
		return fmt.Errorf("two provisioning runs are already active; wait for one to finish")
	}
	return nil
}

// Base-name readers retain the legacy summary, preferring an active operation.
// Run-aware callers use the exact key and never select another run.
func (p *localControlPanel) summaryOperationKeyLocked(name panelOperationName) panelOperationName {
	if name != panelOperationBase(name) || !isProvisioningPanelOperation(name) {
		return name
	}
	best := name
	selected := p.operationLocked(name)
	for _, candidate := range p.panelOperationNamesLocked() {
		if panelOperationBase(candidate) != name {
			continue
		}
		op := p.operations[candidate]
		p.refreshOperationLocked(candidate, op)
		if panelOperationActive(op) != panelOperationActive(selected) {
			if panelOperationActive(op) {
				best, selected = candidate, op
			}
			continue
		}
		if op.UpdatedAt != nil && (selected.UpdatedAt == nil || op.UpdatedAt.After(*selected.UpdatedAt)) {
			best, selected = candidate, op
		}
	}
	return best
}
