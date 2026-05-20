package test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func newPanelOperations() map[panelOperationName]*panelOperationState {
	return map[panelOperationName]*panelOperationState{
		panelOperationSetup:         {},
		panelOperationReadiness:     {},
		panelOperationCleanup:       {},
		panelOperationLinodeSetup:   {},
		panelOperationLinodeCleanup: {},
	}
}

func allPanelOperationNames() []panelOperationName {
	return []panelOperationName{
		panelOperationSetup,
		panelOperationReadiness,
		panelOperationCleanup,
		panelOperationLinodeSetup,
		panelOperationLinodeCleanup,
	}
}

func conflictingPanelOperationNames(operation panelOperationName) []panelOperationName {
	switch operation {
	case panelOperationLinodeSetup, panelOperationLinodeCleanup:
		return []panelOperationName{panelOperationLinodeSetup, panelOperationLinodeCleanup}
	default:
		return []panelOperationName{panelOperationSetup, panelOperationReadiness, panelOperationCleanup}
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
	record, ok := p.readRunRecord(runID)
	if !ok {
		return fmt.Errorf("cleanup requires a recorded run: %s", runID)
	}
	operation := panelOperationCleanup
	if isLinodeDockerRecord(record) {
		operation = panelOperationLinodeCleanup
	}
	return p.startPanelCommand(panelCommandSpec{
		Operation:   operation,
		DisplayName: "cleanup",
		TestName:    "TestHACleanup",
		Timeout:     "30m",
		RunID:       record.RunID,
		StartLine:   fmt.Sprintf("[control-panel] Starting canonical cleanup for run %s via go test -run ^TestHACleanup$", record.RunID),
		SuccessLine: "[control-panel] Cleanup completed successfully",
	})
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
	pid := op.PID
	op.Output = append(op.Output, fmt.Sprintf("[control-panel] Stop requested for %s run %s. Terraform state and run records will be preserved.", operation, op.RunID))
	now := time.Now()
	op.UpdatedAt = &now
	p.persistOperationsLocked()
	p.mu.Unlock()

	if pid <= 0 {
		return fmt.Errorf("%s has no tracked process id yet; wait a moment and retry", operation)
	}
	return interruptProcessTree(pid)
}

func (p *localControlPanel) startPanelCommand(spec panelCommandSpec) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conflictingOperationRunningLocked(spec.Operation) {
		return fmt.Errorf("%s is already running", p.runningConflictingOperationNameLocked(spec.Operation))
	}

	op := p.operationLocked(spec.Operation)
	if op.Running {
		return fmt.Errorf("%s is already running", spec.DisplayName)
	}

	now := time.Now()
	runID := safeRunPathSegment(spec.RunID)
	if runID == "" || runID == "unknown" {
		token, err := randomConfirmationToken()
		if err != nil {
			return fmt.Errorf("failed to create %s run id: %w", spec.DisplayName, err)
		}
		runID = token[:8]
	}
	command := fmt.Sprintf("go test -v -run '^%s$' -timeout %s -count=1 ./terratest", spec.TestName, spec.Timeout)

	if spec.Operation == panelOperationSetup || spec.Operation == panelOperationLinodeSetup {
		if err := p.prepareTerraformModuleForRun(runID); err != nil {
			return err
		}
	}

	op.Running = true
	op.PID = 0
	op.StartedAt = &now
	op.FinishedAt = nil
	op.Error = ""
	op.RunID = runID
	op.Command = command
	op.UpdatedAt = &now
	op.Output = []string{
		fmt.Sprintf("[control-panel] Run %s", runID),
		"[control-panel] " + command,
		spec.StartLine,
	}
	if spec.Operation == panelOperationSetup || spec.Operation == panelOperationLinodeSetup {
		p.createCurrentRunRecord(runID, now)
	}
	p.persistOperationsLocked()

	go p.runPanelCommand(spec)
	return nil
}

func (p *localControlPanel) runPanelCommand(spec panelCommandSpec) {
	args := []string{"test", "-v", "-run", fmt.Sprintf("^%s$", spec.TestName), "-timeout", spec.Timeout, "-count=1", "./terratest"}
	cmd := exec.Command("go", args...)
	cmd.Dir = p.repoRoot
	cmd.Env = p.panelCommandEnv(spec.Operation)
	cmd.SysProcAttr = panelCommandSysProcAttr()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.finishPanelCommand(spec, fmt.Errorf("failed to capture %s output: %w", spec.DisplayName, err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.finishPanelCommand(spec, fmt.Errorf("failed to capture %s output: %w", spec.DisplayName, err))
		return
	}

	if err := cmd.Start(); err != nil {
		p.finishPanelCommand(spec, fmt.Errorf("failed to start %s command: %w", spec.DisplayName, err))
		return
	}
	p.setOperationPID(spec.Operation, cmd.Process.Pid)

	var wg sync.WaitGroup
	wg.Add(2)
	go p.capturePanelCommandStream(&wg, spec.Operation, stdout)
	go p.capturePanelCommandStream(&wg, spec.Operation, stderr)
	wg.Wait()

	p.finishPanelCommand(spec, cmd.Wait())
}

func (p *localControlPanel) panelCommandEnv(operation panelOperationName) []string {
	env := os.Environ()
	var runID string
	var haOutputRoot string
	var terraformModuleDir string
	var terraformStatePath string
	var terraformDataDir string
	var slotID = defaultRunSlotID
	var runDeploymentType string
	var runTotalHAs int
	var runRancherVersions []string
	var runAWSPrefix string
	var runRoute53FQDN string

	p.mu.Lock()
	op := p.operationLocked(operation)
	runID = strings.TrimSpace(op.RunID)
	p.mu.Unlock()

	recordLoaded := false
	if (operation == panelOperationSetup || operation == panelOperationLinodeSetup) && runID != "" {
		slotID = panelRunSlotID(runID)
		haOutputRoot = p.haOutputRootForRun(runID)
		terraformModuleDir = p.terraformModuleDirForRun(runID)
		terraformStatePath = p.terraformStatePathForRun(runID)
		terraformDataDir = p.terraformDataDirForRun(runID)
	} else if runID != "" {
		if record, ok := p.readRunRecord(runID); ok {
			recordLoaded = true
			slotID = record.SlotID
			haOutputRoot = record.HAOutputRoot
			terraformModuleDir = record.TerraformModuleDir
			terraformStatePath = record.TerraformStatePath
			terraformDataDir = record.TerraformDataDir
			runDeploymentType = deploymentTypeForRunEnv(record)
			runTotalHAs = record.TotalHAs
			runRancherVersions = record.RancherVersions
			runAWSPrefix = record.AWSPrefix
			runRoute53FQDN = record.Route53FQDN
		}
	}
	if !recordLoaded && haOutputRoot == "" {
		if record, ok := p.readCurrentRunRecord(); ok {
			runID = record.RunID
			slotID = record.SlotID
			haOutputRoot = record.HAOutputRoot
			terraformModuleDir = record.TerraformModuleDir
			terraformStatePath = record.TerraformStatePath
			terraformDataDir = record.TerraformDataDir
			runDeploymentType = deploymentTypeForRunEnv(record)
			runTotalHAs = record.TotalHAs
			runRancherVersions = record.RancherVersions
			runAWSPrefix = record.AWSPrefix
			runRoute53FQDN = record.Route53FQDN
		}
	}

	if runID != "" {
		env = append(env, runIDEnv+"="+runID)
	}
	env = append(env, panelNonInteractiveEnv+"=1")
	if slotID != "" {
		env = append(env, "HA_RANCHER_RUN_SLOT="+slotID)
	}
	if strings.TrimSpace(haOutputRoot) != "" {
		env = append(env, haOutputRootEnv+"="+haOutputRoot)
	}
	if strings.TrimSpace(terraformModuleDir) != "" {
		env = append(env, terraformModuleDirEnv+"="+terraformModuleDir)
	}
	if strings.TrimSpace(terraformStatePath) != "" {
		env = append(env, terraformStatePathEnv+"="+terraformStatePath)
	}
	if strings.TrimSpace(terraformDataDir) != "" {
		env = append(env, terraformDataDirEnv+"="+terraformDataDir)
	}
	if strings.TrimSpace(runDeploymentType) != "" {
		env = append(env, runDeploymentTypeEnv+"="+runDeploymentType)
	}
	if runTotalHAs > 0 {
		env = append(env, runTotalHAsEnv+"="+fmt.Sprintf("%d", runTotalHAs))
	}
	if len(runRancherVersions) > 0 {
		env = append(env, runRancherVersionsEnv+"="+strings.Join(runRancherVersions, ","))
	}
	if strings.TrimSpace(runAWSPrefix) != "" {
		env = append(env, runAWSPrefixEnv+"="+runAWSPrefix)
	}
	if strings.TrimSpace(runRoute53FQDN) != "" {
		env = append(env, runRoute53FQDNEnv+"="+runRoute53FQDN)
	}
	return env
}

func deploymentTypeForRunEnv(record panelRunRecord) string {
	if value := strings.TrimSpace(record.DeploymentType); value != "" {
		return value
	}
	moduleDir := filepath.ToSlash(strings.TrimSpace(record.TerraformModuleDir))
	if strings.Contains(moduleDir, "linode-docker-cattle") {
		return deploymentTypeLinodeDocker
	}
	return deploymentTypeHARKE2
}

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
	now := time.Now()
	op.UpdatedAt = &now
	p.persistOperationsLocked()
}

func (p *localControlPanel) finishPanelCommand(spec panelCommandSpec, err error) {
	var shouldRunAfterSuccess bool
	var runID string
	p.mu.Lock()
	op := p.operationLocked(spec.Operation)
	runID = op.RunID
	op.Running = false
	op.PID = 0
	finishedAt := time.Now()
	op.FinishedAt = &finishedAt
	op.UpdatedAt = &finishedAt
	if err != nil {
		op.Error = err.Error()
		op.Output = append(op.Output, fmt.Sprintf("[control-panel] %s finished with error: %s", panelDisplayTitle(spec.DisplayName), err.Error()))
		p.persistOperationsLocked()
		p.mu.Unlock()
		p.updateRunStatusAfterOperation(spec.Operation, runID, false)
		return
	}

	op.Error = ""
	op.Output = append(op.Output, spec.SuccessLine)
	p.persistOperationsLocked()
	shouldRunAfterSuccess = spec.AfterSuccess != nil
	p.mu.Unlock()

	p.updateRunStatusAfterOperation(spec.Operation, runID, true)
	if shouldRunAfterSuccess {
		spec.AfterSuccess()
	}
}

func (p *localControlPanel) setOperationPID(operation panelOperationName, pid int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	op := p.operationLocked(operation)
	op.PID = pid
	now := time.Now()
	op.UpdatedAt = &now
	op.Output = append(op.Output, fmt.Sprintf("[control-panel] Process pid %d", pid))
	p.persistOperationsLocked()
}

func (p *localControlPanel) updateRunStatusAfterOperation(operation panelOperationName, runID string, success bool) {
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
	for _, name := range allPanelOperationNames() {
		op := p.operationLocked(name)
		if op.Running && op.PID > 0 && !processAlive(op.PID) {
			now := time.Now()
			op.Running = false
			op.PID = 0
			op.FinishedAt = &now
			op.UpdatedAt = &now
			op.Error = "operation process exited before reporting completion"
			op.Output = append(op.Output, "[control-panel] Operation process exited before reporting completion; status marked stale.")
			continue
		}
		if op.Running {
			return true
		}
	}
	return false
}

func (p *localControlPanel) conflictingOperationRunningLocked(operation panelOperationName) bool {
	for _, name := range conflictingPanelOperationNames(operation) {
		op := p.operationLocked(name)
		if op.Running && op.PID > 0 && !processAlive(op.PID) {
			now := time.Now()
			op.Running = false
			op.PID = 0
			op.FinishedAt = &now
			op.UpdatedAt = &now
			op.Error = "operation process exited before reporting completion"
			op.Output = append(op.Output, "[control-panel] Operation process exited before reporting completion; status marked stale.")
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
	now := time.Now()
	for _, name := range allPanelOperationNames() {
		op := p.operationLocked(name)
		if !op.Running {
			continue
		}
		op.Running = false
		op.PID = 0
		op.FinishedAt = &now
		op.UpdatedAt = &now
		op.Error = "panel restarted before this operation reported completion"
		op.Output = append(op.Output, "[control-panel] Panel restarted before this operation reported completion; status marked stale.")
	}
}

func (p *localControlPanel) clearCompletedCleanupSuccessLocked() {
	for _, name := range []panelOperationName{panelOperationCleanup, panelOperationLinodeCleanup} {
		op := p.operationLocked(name)
		if op.Running || op.FinishedAt == nil || strings.TrimSpace(op.Error) != "" {
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
