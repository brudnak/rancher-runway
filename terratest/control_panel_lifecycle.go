package test

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const packagedLifecycleBinaryEnv = "RANCHER_RUNWAY_LIFECYCLE_BIN"

const panelDownstreamLinodePlansEnv = "RANCHER_RUNWAY_DOWNSTREAM_LINODE_PLANS"

type panelLifecycleInvocation struct {
	path    string
	args    []string
	dir     string
	display string
}

func newPanelOperations() map[panelOperationName]*panelOperationState {
	return map[panelOperationName]*panelOperationState{
		panelOperationSetup:         {},
		panelOperationReadiness:     {},
		panelOperationDownstream:    {},
		panelOperationCleanup:       {},
		panelOperationLinodeSetup:   {},
		panelOperationLinodeCleanup: {},
		panelOperationCleanupBatch:  {},
		panelOperationSteveLab:      {},
		panelOperationK3DLab:        {},
		panelOperationAWSCleanup:    {},
	}
}

func allPanelOperationNames() []panelOperationName {
	return []panelOperationName{
		panelOperationSetup,
		panelOperationReadiness,
		panelOperationDownstream,
		panelOperationCleanup,
		panelOperationLinodeSetup,
		panelOperationLinodeCleanup,
		panelOperationCleanupBatch,
		panelOperationSteveLab,
		panelOperationK3DLab,
		panelOperationAWSCleanup,
	}
}

func parsePanelOperationName(value string) (panelOperationName, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "setup":
		return panelOperationSetup, true
	case "readiness":
		return panelOperationReadiness, true
	case "downstream":
		return panelOperationDownstream, true
	case "cleanup":
		return panelOperationCleanup, true
	case "linodesetup":
		return panelOperationLinodeSetup, true
	case "linodecleanup":
		return panelOperationLinodeCleanup, true
	case "cleanupbatch":
		return panelOperationCleanupBatch, true
	case "stevelab":
		return panelOperationSteveLab, true
	case "k3dlab":
		return panelOperationK3DLab, true
	default:
		return "", false
	}
}

func conflictingPanelOperationNames(operation panelOperationName) []panelOperationName {
	switch operation {
	case panelOperationLinodeSetup, panelOperationLinodeCleanup:
		return []panelOperationName{panelOperationLinodeSetup, panelOperationLinodeCleanup}
	case panelOperationSteveLab:
		return []panelOperationName{panelOperationSteveLab}
	case panelOperationK3DLab:
		return []panelOperationName{panelOperationK3DLab}
	default:
		return []panelOperationName{panelOperationSetup, panelOperationReadiness, panelOperationDownstream, panelOperationCleanup}
	}
}

func (p *localControlPanel) startSetup() error {
	preflight := p.collectPanelPreflight()
	if !preflight.Ready {
		return fmt.Errorf("preflight blocked setup: %s", preflight.Summary)
	}

	afterSuccess := p.startReadinessAfterSetup
	if isHostedTenantK3SDeployment() {
		afterSuccess = nil
	}
	operation := panelOperationSetup
	if isLinodeDockerDeployment() {
		operation = panelOperationLinodeSetup
	}
	return p.startPanelCommand(panelCommandSpec{
		Operation:    operation,
		DisplayName:  "setup",
		TestName:     "TestHaSetup",
		Timeout:      "90m",
		StartLine:    "[control-panel] Starting canonical setup via go test -run ^TestHaSetup$",
		SuccessLine:  "[control-panel] Setup completed successfully",
		AfterSuccess: afterSuccess,
	})
}

func (p *localControlPanel) startReadiness() error {
	deployment, record, outputs := p.readinessDeploymentType()
	if err := p.readinessPreflightError(deployment, record, outputs); err != nil {
		return err
	}

	spec := readinessCommandSpec(deployment)
	if record.RunID != "" {
		spec.RunID = record.RunID
	}
	if deployment == deploymentTypeHARKE2 && record.RunID != "" {
		runID := record.RunID
		spec.AfterSuccess = func() {
			p.startConfiguredDownstreamsAfterReadiness(runID)
		}
	}
	return p.startPanelCommand(spec)
}

func (p *localControlPanel) startReadinessAfterSetup() {
	if err := p.startReadiness(); err != nil {
		p.appendOperationOutput(panelOperationReadiness, "[control-panel] Readiness was not started automatically: "+err.Error())
	}
}

func readinessCommandSpec(deployment string) panelCommandSpec {
	if deployment == deploymentTypeLinodeDocker {
		return panelCommandSpec{
			Operation:   panelOperationReadiness,
			DisplayName: "readiness",
			TestName:    "TestLinodeDockerWaitReady",
			Timeout:     "35m",
			StartLine:   "[control-panel] Waiting for Linode Docker Rancher readiness via go test -run ^TestLinodeDockerWaitReady$",
			SuccessLine: "[control-panel] Linode Docker readiness checks completed successfully",
		}
	}
	return panelCommandSpec{
		Operation:   panelOperationReadiness,
		DisplayName: "readiness",
		TestName:    "TestHAWaitReady",
		Timeout:     "35m",
		StartLine:   "[control-panel] Waiting for Rancher and rancher-webhook readiness via go test -run ^TestHAWaitReady$",
		SuccessLine: "[control-panel] Readiness checks completed successfully",
	}
}

func (p *localControlPanel) startConfiguredDownstreamsAfterReadiness(runID string) {
	record, ok := p.readRunRecord(runID)
	if !ok || !runRecordHasEnabledDownstreamPlans(record) ||
		record.DownstreamStatus == panelDownstreamStatusRunning || record.DownstreamStatus == panelDownstreamStatusReady {
		return
	}
	if err := p.startConfiguredDownstreamsForRun(runID); err != nil {
		p.updateRunDownstreamStatus(runID, panelDownstreamStatusFailed, err.Error())
		p.appendOperationOutput(panelOperationDownstream, "[control-panel] Downstream provisioning was not started automatically: "+err.Error())
	}
}

func (p *localControlPanel) startConfiguredDownstreamsForRun(runID string) error {
	runID = safeRunPathSegment(runID)
	if runID == "" || runID == "unknown" {
		return fmt.Errorf("downstream provisioning requires a recorded run id")
	}
	record, ok := p.readRunRecord(runID)
	if !ok {
		return fmt.Errorf("downstream provisioning requires a recorded run: %s", runID)
	}
	if recordDeploymentType(record, nil) != deploymentTypeHARKE2 {
		return fmt.Errorf("configured Linode downstream provisioning is only supported for ha-rke2 management runs")
	}
	if record.Status != "ready" {
		return fmt.Errorf("management Rancher run %s must be ready before downstream provisioning", runID)
	}
	if !runRecordHasEnabledDownstreamPlans(record) {
		return fmt.Errorf("run %s has no enabled Linode downstream plans", runID)
	}
	if p.operationRunning(panelOperationDownstream) || record.DownstreamStatus == panelDownstreamStatusRunning {
		return fmt.Errorf("downstream provisioning is already running")
	}
	if record.DownstreamStatus == panelDownstreamStatusReady {
		return fmt.Errorf("run %s downstream provisioning is already complete", runID)
	}

	enabledCount := enabledDownstreamPlanCount(record)
	err := p.startPanelCommand(panelCommandSpec{
		Operation:   panelOperationDownstream,
		DisplayName: "downstream provisioning",
		TestName:    "TestHAProvisionConfiguredLinodeDownstreams",
		Timeout:     "35m",
		RunID:       record.RunID,
		StartLine: fmt.Sprintf(
			"[control-panel] Provisioning %d configured Linode downstream cluster(s) after management readiness",
			enabledCount,
		),
		SuccessLine: "[control-panel] Configured Linode downstream provisioning completed successfully",
	})
	if err != nil {
		p.updateRunDownstreamStatus(runID, panelDownstreamStatusFailed, err.Error())
	}
	return err
}

func runRecordHasEnabledDownstreamPlans(record panelRunRecord) bool {
	return enabledDownstreamPlanCount(record) > 0
}

func enabledDownstreamPlanCount(record panelRunRecord) int {
	count := 0
	for _, plan := range record.DownstreamLinodePlans {
		if plan.Enabled {
			count++
		}
	}
	return count
}

func (p *localControlPanel) readinessDeploymentType() (string, panelRunRecord, map[string]string) {
	if record, ok := p.readCurrentRunRecord(); ok {
		outputs, _ := readTerraformFlatOutputsWithModule(p.repoRoot, record.TerraformStatePath, record.TerraformDataDir, record.TerraformModuleDir)
		return recordDeploymentType(record, outputs), record, outputs
	}
	outputs, _ := p.readTerraformFlatOutputs()
	return recordDeploymentType(panelRunRecord{}, outputs), panelRunRecord{}, outputs
}

func (p *localControlPanel) readinessPreflightError(deployment string, record panelRunRecord, outputs map[string]string) error {
	if deployment == deploymentTypeHostedTenantK3S {
		return fmt.Errorf("readiness checks are currently only wired for ha-rke2; hosted-tenant-k3s setup waits for host and tenant Ranchers during setup")
	}
	if deployment == deploymentTypeLinodeDocker {
		total := record.TotalHAs
		if total < 1 {
			total = configuredRancherInstanceCount()
		}
		if total < 1 {
			total = p.totalHAs
		}
		if total < 1 {
			return fmt.Errorf("Linode Docker readiness requires at least one configured Rancher instance")
		}
		if len(outputs) == 0 {
			return fmt.Errorf("Linode Docker readiness requires Terraform outputs from a completed setup")
		}
		missingOutputs := make([]string, 0, total)
		for i := 1; i <= total; i++ {
			if strings.TrimSpace(outputs[fmt.Sprintf("linode_%d_rancher_url", i)]) == "" && strings.TrimSpace(outputs[fmt.Sprintf("linode_%d_ip", i)]) == "" {
				missingOutputs = append(missingOutputs, fmt.Sprintf("linode_%d_*", i))
			}
		}
		if len(missingOutputs) > 0 {
			return fmt.Errorf("Linode Docker readiness requires Terraform outputs from a completed setup; missing %s", strings.Join(missingOutputs, ", "))
		}
		return nil
	}
	if p.totalHAs < 1 {
		return fmt.Errorf("readiness requires at least one configured HA")
	}

	missingKubeconfigs := make([]string, 0, p.totalHAs)
	for i := 1; i <= p.totalHAs; i++ {
		kubeconfigPath := filepath.Join(p.haInstanceDir(i), "kube_config.yaml")
		if !pathExists(kubeconfigPath) {
			missingKubeconfigs = append(missingKubeconfigs, kubeconfigPath)
		}
	}
	if len(missingKubeconfigs) > 0 {
		return fmt.Errorf("readiness requires a completed setup; missing kubeconfig %s", strings.Join(missingKubeconfigs, ", "))
	}

	outputs, err := p.readTerraformFlatOutputs()
	if err != nil {
		return fmt.Errorf("readiness requires Terraform outputs from a completed setup: %w", err)
	}

	missingOutputs := make([]string, 0, p.totalHAs)
	for i := 1; i <= p.totalHAs; i++ {
		if !hasHAFlatOutput(outputs, i) {
			missingOutputs = append(missingOutputs, fmt.Sprintf("ha_%d_*", i))
		}
	}
	if len(missingOutputs) > 0 {
		return fmt.Errorf("readiness requires Terraform outputs from a completed setup; missing %s", strings.Join(missingOutputs, ", "))
	}

	return nil
}

func (p *localControlPanel) startCleanup() error {
	record, ok := p.readCurrentRunRecord()
	if !ok {
		return fmt.Errorf("cleanup requires a recorded run")
	}
	return p.startCleanupForRun(record.RunID)
}

func (p *localControlPanel) startCleanupForRun(runID string) error {
	return p.startCleanupForRunWithBatch(runID, false, nil)
}

func (p *localControlPanel) startCleanupForRunWithBatch(runID string, batchChild bool, completion chan<- error) error {
	return p.startCleanupForRunWithOptions(runID, batchChild, completion, panelLabCleanupOptions{})
}

func (p *localControlPanel) startCleanupForRunWithLabCleanup(runID string, tests, cache bool) error {
	record, ok := p.readRunRecord(runID)
	if !ok {
		return fmt.Errorf("cleanup requires a recorded run: %s", runID)
	}
	runID = record.RunID
	options, err := p.freezeRunLabCleanup(runID, tests, cache)
	if err != nil {
		return err
	}
	return p.startCleanupForRunWithOptions(runID, false, nil, options)
}

func (p *localControlPanel) startCleanupForRunWithOptions(runID string, batchChild bool, completion chan<- error, options panelLabCleanupOptions) error {
	record, ok := p.readRunRecord(runID)
	if !ok {
		return fmt.Errorf("cleanup requires a recorded run: %s", runID)
	}
	operation := panelOperationCleanup
	if isLinodeDockerRecord(record) {
		operation = panelOperationLinodeCleanup
	}
	err := p.startPanelCommand(panelCommandSpec{
		Operation:   operation,
		DisplayName: "cleanup",
		TestName:    "TestHACleanup",
		Timeout:     "60m",
		RunID:       record.RunID,
		StartLine:   fmt.Sprintf("[control-panel] Starting canonical cleanup for run %s via go test -run ^TestHACleanup$", record.RunID),
		SuccessLine: "[control-panel] Cleanup completed successfully",
		BatchChild:  batchChild,
		Completion:  completion,
		LabCleanup:  options,
	})
	if err == nil && !batchChild {
		p.clearCompletedCleanupBatch()
	}
	return err
}

func (p *localControlPanel) abortOperation(operation panelOperationName, runID string) error {
	p.mu.Lock()
	op := p.operationLocked(operation)
	if !op.Running {
		p.mu.Unlock()
		return fmt.Errorf("%s is not running", operation)
	}
	if strings.TrimSpace(runID) != "" && !sameRunID(op.RunID, runID) {
		p.mu.Unlock()
		return fmt.Errorf("%s is running for run %s, not %s", operation, op.RunID, runID)
	}
	if operation == panelOperationCleanupBatch {
		op.CancelRequested = true
		op.Output = append(op.Output, "[control-panel] Stop requested for cleanup batch. The current destroy will be interrupted and queued slots will remain recorded.")
		now := time.Now()
		op.UpdatedAt = &now
		pid := op.PID
		p.persistOperationsLocked()
		p.mu.Unlock()
		if pid <= 0 {
			return nil
		}
		return interruptProcessTree(pid)
	}
	if operation == panelOperationCleanup || operation == panelOperationLinodeCleanup {
		batch := p.operationLocked(panelOperationCleanupBatch)
		if batch.Running && sameRunID(batch.RunID, op.RunID) {
			batch.CancelRequested = true
			batch.Output = append(batch.Output, "[control-panel] Stop requested for the current batch destroy. Queued slots will remain recorded.")
			now := time.Now()
			batch.UpdatedAt = &now
		}
	}
	op.CancelRequested = true
	pid := op.PID
	if operation == panelOperationSteveLab {
		op.Output = append(op.Output, fmt.Sprintf("[control-panel] Stop requested for %s run %s. Local k3d cluster and run files may need cleanup.", operation, op.RunID))
	} else if operation == panelOperationDownstream {
		op.Output = append(op.Output, fmt.Sprintf("[control-panel] Stop requested for downstream provisioning run %s. The ready management Rancher and run record will be preserved; partial downstream resources remain recorded for retry or destroy.", op.RunID))
	} else {
		op.Output = append(op.Output, fmt.Sprintf("[control-panel] Stop requested for %s run %s. Terraform state and run records will be preserved.", operation, op.RunID))
	}
	now := time.Now()
	op.UpdatedAt = &now
	p.persistOperationsLocked()
	p.mu.Unlock()

	if pid <= 0 {
		return fmt.Errorf("%s has no tracked process id yet; wait a moment and retry", operation)
	}
	return interruptProcessTree(pid)
}
