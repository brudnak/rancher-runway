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

	op := p.operationLocked(name)
	if op.Running && !op.InProcess && op.PID > 0 && !processAlive(op.PID) {
		p.markOperationStaleLocked(name, op,
			"operation process exited before reporting completion",
			"[control-panel] Operation process exited before reporting completion; status marked stale.",
		)
		p.persistOperationsLocked()
	}
	cleanupOperation := name == panelOperationCleanup || name == panelOperationLinodeCleanup
	recentCleanup := cleanupOperation && op.FinishedAt != nil && op.Error == "" && time.Since(*op.FinishedAt) < time.Hour
	retainedCleanupWarning := cleanupOperation && op.FinishedAt != nil && strings.TrimSpace(op.Warning) != ""
	if activeRunIDs != nil && !op.Running && op.RunID != "" && !activeRunIDs[safeRunPathSegment(op.RunID)] && !recentCleanup && !retainedCleanupWarning {
		return panelOperationSnapshot{Output: []string{}}
	}
	outputCopy := append([]string(nil), op.Output...)
	if outputCopy == nil {
		outputCopy = []string{}
	}
	return panelOperationSnapshot{
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
