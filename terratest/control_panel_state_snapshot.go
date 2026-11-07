package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
	"strings"
	"time"
)

func (p *localControlPanel) buildState() panelState {
	workspace := p.workspaceState()
	activeRunIDs := map[string]bool{}
	for _, record := range workspace.Runs {
		activeRunIDs[safeRunPathSegment(record.RunID)] = true
	}
	return panelState{
		Panel: panelSessionState{
			SessionID:            p.sessionID,
			StartedAt:            p.startedAt,
			RepoRoot:             p.repoRoot,
			ConfigPath:           p.configPath,
			StarterConfigCreated: p.starterConfigCreated,
			Build:                buildinfo.Current(),
		},
		Workspace:     workspace,
		RunOperations: p.snapshotRunOperations(activeRunIDs),
		TestLab:       p.testLabActivity(),
		Setup:         p.snapshotOperationForRuns(panelOperationSetup, activeRunIDs),
		Readiness:     p.snapshotOperationForRuns(panelOperationReadiness, activeRunIDs),
		Downstream:    p.snapshotOperationForRuns(panelOperationDownstream, activeRunIDs),
		LinodeSetup:   p.snapshotOperationForRuns(panelOperationLinodeSetup, activeRunIDs),
		LinodeCleanup: p.snapshotOperationForRuns(panelOperationLinodeCleanup, activeRunIDs),
		Steve:         p.steveLabPanelState(false),
		K3D:           p.k3dLabPanelState(false),
		Clusters:      p.clusterDiscoveryState(),
		AWS:           p.awsDiscoveryState(workspace.Runs),
		AWSCleanup:    p.snapshotAWSCleanup(),
		Cleanup:       p.snapshotOperationForRuns(panelOperationCleanup, activeRunIDs),
		CleanupBatch:  p.snapshotCleanupBatch(),
		Costs:         discoverCostHistory(),
	}
}

func (p *localControlPanel) snapshotOperation(name panelOperationName) panelOperationSnapshot {
	return p.snapshotOperationForRuns(name, nil)
}

func (p *localControlPanel) snapshotOperationForRuns(name panelOperationName, activeRunIDs map[string]bool) panelOperationSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()

	name = p.summaryOperationKeyLocked(name)
	return p.snapshotOperationLocked(name, activeRunIDs)
}

func (p *localControlPanel) snapshotRunOperations(activeRunIDs map[string]bool) []panelOperationSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshots := []panelOperationSnapshot{}
	for _, name := range p.panelOperationNamesLocked() {
		op := p.operations[name]
		if !isProvisioningPanelOperation(name) || op.RunID == "" || (op.StartedAt == nil && len(op.Output) == 0) {
			continue
		}
		// A retained pre-upgrade summary may refer to a run now tracked by key.
		if name == panelOperationBase(name) && p.operations[panelOperationForRun(name, op.RunID)] != nil {
			continue
		}
		snapshot := p.snapshotOperationLocked(name, activeRunIDs)
		if snapshot.RunID != "" {
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots
}

func (p *localControlPanel) snapshotOperationLocked(name panelOperationName, activeRunIDs map[string]bool) panelOperationSnapshot {
	op := p.operationLocked(name)
	p.refreshOperationLocked(name, op)
	cleanupOperation := name == panelOperationCleanup || name == panelOperationLinodeCleanup
	recentCleanup := cleanupOperation && op.FinishedAt != nil && op.Error == "" && time.Since(*op.FinishedAt) < time.Hour
	retainedCleanupWarning := cleanupOperation && op.FinishedAt != nil && strings.TrimSpace(op.Warning) != ""
	if activeRunIDs != nil && !panelOperationActive(op) && op.RunID != "" && !activeRunIDs[safeRunPathSegment(op.RunID)] && !recentCleanup && !retainedCleanupWarning {
		return panelOperationSnapshot{Output: []string{}}
	}
	outputCopy := append([]string(nil), op.Output...)
	if outputCopy == nil {
		outputCopy = []string{}
	}
	return panelOperationSnapshot{
		Operation:  panelOperationBase(name),
		Running:    op.Running,
		PID:        op.PID,
		StartedAt:  op.StartedAt,
		FinishedAt: op.FinishedAt,
		Error:      op.Error,
		Warning:    op.Warning,
		Output:     outputCopy,
		RunID:      op.RunID,
		Command:    op.Command,
		UpdatedAt:  op.UpdatedAt,
	}
}
