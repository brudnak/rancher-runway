package test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (p *localControlPanel) startPanelCommand(spec panelCommandSpec) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if isCloudPanelOperation(spec.Operation) {
		batch := p.operationLocked(panelOperationCleanupBatch)
		if batch.Running && !spec.BatchChild {
			return fmt.Errorf("cleanup batch is already running")
		}
		if spec.BatchChild && (!batch.Running || batch.CancelRequested) {
			return errCleanupBatchCanceled
		}
	}

	if p.conflictingOperationRunningLocked(spec.Operation) {
		return fmt.Errorf("%s is already running", p.runningConflictingOperationNameLocked(spec.Operation))
	}

	op := p.operationLocked(spec.Operation)
	if op.Running {
		return fmt.Errorf("%s is already running", spec.DisplayName)
	}
	_, workerDone, workerErr := p.workers.Begin()
	if workerErr != nil {
		return workerErr
	}
	launched := false
	defer func() {
		if !launched {
			workerDone()
		}
	}()

	now := time.Now()
	runID := safeRunPathSegment(spec.RunID)
	if runID == "" || runID == "unknown" {
		token, err := randomConfirmationToken()
		if err != nil {
			return fmt.Errorf("failed to create %s run id: %w", spec.DisplayName, err)
		}
		runID = token[:8]
	}
	invocation, err := p.lifecycleInvocation(spec)
	if err != nil {
		return err
	}
	command := invocation.display

	if spec.Operation == panelOperationSetup || spec.Operation == panelOperationLinodeSetup {
		if err := p.prepareTerraformModuleForRun(runID); err != nil {
			return err
		}
	}

	op.Running = true
	op.CancelRequested = false
	op.PID = 0
	op.StartedAt = &now
	op.FinishedAt = nil
	op.Error = ""
	op.Warning = ""
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
	} else if spec.Operation == panelOperationDownstream {
		p.updateRunDownstreamStatus(runID, panelDownstreamStatusRunning, "")
	}
	p.persistOperationsLocked()

	launched = true
	p.lifecycleWorkers.Add(1)
	go func() {
		defer workerDone()
		defer p.lifecycleWorkers.Done()
		p.runPanelCommand(spec)
	}()
	return nil
}

func (p *localControlPanel) runPanelCommand(spec panelCommandSpec) {
	invocation, err := p.lifecycleInvocation(spec)
	if err != nil {
		p.finishPanelCommand(spec, err)
		return
	}
	cmd := exec.Command(invocation.path, invocation.args...)
	cmd.Dir = invocation.dir
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

func (p *localControlPanel) lifecycleInvocation(spec panelCommandSpec) (panelLifecycleInvocation, error) {
	testPattern := fmt.Sprintf("^%s$", spec.TestName)
	workspaceVersion := ""
	if data, err := os.ReadFile(filepath.Join(p.repoRoot, ".rancher-runway-runtime-version")); err == nil {
		workspaceVersion = strings.TrimSpace(string(data))
	}
	if helper := strings.TrimSpace(os.Getenv(packagedLifecycleBinaryEnv)); helper != "" {
		helper, err := filepath.Abs(helper)
		if err != nil {
			return panelLifecycleInvocation{}, fmt.Errorf("resolve packaged lifecycle worker: %w", err)
		}
		info, err := os.Stat(helper)
		if err != nil {
			return panelLifecycleInvocation{}, fmt.Errorf("packaged lifecycle worker is unavailable at %s: %w", helper, err)
		}
		if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			return panelLifecycleInvocation{}, fmt.Errorf("packaged lifecycle worker is not executable: %s", helper)
		}
		if workspaceVersion != "" {
			runtimeRoot := filepath.Dir(filepath.Dir(helper))
			data, err := os.ReadFile(filepath.Join(runtimeRoot, ".rancher-runway-runtime-version"))
			if err != nil || strings.TrimSpace(string(data)) != workspaceVersion {
				return panelLifecycleInvocation{}, fmt.Errorf("packaged lifecycle worker does not match workspace runtime %s", workspaceVersion)
			}
		}
		args := []string{
			"-test.v",
			"-test.run=" + testPattern,
			"-test.timeout=" + spec.Timeout,
			"-test.count=1",
		}
		return panelLifecycleInvocation{
			path:    helper,
			args:    args,
			dir:     p.testDir,
			display: fmt.Sprintf("rancher-runway-lifecycle -test.run='%s' -test.timeout=%s", testPattern, spec.Timeout),
		}, nil
	}
	if workspaceVersion != "" {
		return panelLifecycleInvocation{}, fmt.Errorf("packaged runtime %s is missing its lifecycle worker", workspaceVersion)
	}

	args := []string{"test", "-v", "-run", testPattern, "-timeout", spec.Timeout, "-count=1", "./terratest"}
	return panelLifecycleInvocation{
		path:    "go",
		args:    args,
		dir:     p.repoRoot,
		display: fmt.Sprintf("go test -v -run '%s' -timeout %s -count=1 ./terratest", testPattern, spec.Timeout),
	}, nil
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
	var runDownstreamLinodePlansJSON string

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
			if data, err := json.Marshal(record.DownstreamLinodePlans); err == nil {
				runDownstreamLinodePlansJSON = string(data)
			}
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
			if data, err := json.Marshal(record.DownstreamLinodePlans); err == nil {
				runDownstreamLinodePlansJSON = string(data)
			}
		}
	}

	if runID != "" {
		env = append(env, runIDEnv+"="+runID)
	}
	env = append(env, panelNonInteractiveEnv+"=1")
	if operation == panelOperationSetup {
		env = panelEnvWithValue(env, deferSetupDownstreamsEnv, "1")
	}
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
	if operation == panelOperationDownstream {
		if runDownstreamLinodePlansJSON == "" {
			runDownstreamLinodePlansJSON = "[]"
		}
		env = panelEnvWithValue(env, panelDownstreamLinodePlansEnv, runDownstreamLinodePlansJSON)
	}
	return env
}

func panelEnvWithValue(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return append(out, prefix+value)
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
