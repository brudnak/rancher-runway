package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (p *localControlPanel) startSteveLab(req steveLabStartRequest) error {
	preflight := collectSteveLabPreflight()
	if !preflight.Ready {
		return fmt.Errorf("Steve Lab preflight blocked run: %s", preflight.Summary)
	}
	ref := strings.TrimSpace(req.SteveRef)
	if ref == "" {
		return fmt.Errorf("Steve ref is required")
	}
	if !validSteveRef(ref) {
		return fmt.Errorf("Steve ref contains unsupported characters")
	}
	k3sVersion := normalizeSteveLabK3SVersion(req.K3SVersion)
	if k3sVersion == "" {
		k3sVersion = defaultK3SVersions()[0]
	}
	extraEnv, err := normalizeSteveLabEnv(req.ExtraEnv)
	if err != nil {
		return err
	}
	extraArgs, err := normalizeSteveLabArgs(req.ExtraArgs)
	if err != nil {
		return err
	}
	metricsInterval := 0
	if req.EnableMetrics {
		metricsInterval = normalizeSteveLabMetricsInterval(req.MetricsUpdateIntervalSeconds)
	}

	if active := p.activeSteveLabRunRecords(); len(active) > 0 {
		if !req.Replace {
			return fmt.Errorf("Steve Lab already has an active endpoint; stop it or replace it before launching another")
		}
		if err := p.cleanupActiveSteveLabRuns(active); err != nil {
			return err
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conflictingOperationRunningLocked(panelOperationSteveLab) {
		return fmt.Errorf("Steve Lab is already running")
	}
	token, err := randomConfirmationToken()
	if err != nil {
		return fmt.Errorf("failed to create Steve Lab run id: %w", err)
	}
	runID := "steve-" + token[:8]
	now := time.Now()
	runDir := filepath.Join(p.steveLabRunsDir(), runID)
	httpsPort := req.HTTPSPort
	if httpsPort <= 0 {
		var err error
		httpsPort, err = p.allocateLocalLabPort()
		if err != nil {
			return fmt.Errorf("failed to allocate HTTPS port: %w", err)
		}
	} else if err := p.ensureLocalLabPortAvailable(httpsPort); err != nil {
		return err
	}
	record := steveLabRunRecord{
		RunID:                        runID,
		Status:                       "running",
		SteveRef:                     ref,
		K3SVersion:                   k3sVersion,
		ClusterName:                  "rancher-runway-" + runID,
		Kubeconfig:                   filepath.Join(runDir, "kubeconfig.yaml"),
		RunDir:                       runDir,
		SourceDir:                    filepath.Join(runDir, "steve"),
		HTTPSPort:                    httpsPort,
		HTTPSURL:                     fmt.Sprintf("https://127.0.0.1:%d", httpsPort),
		LogPath:                      filepath.Join(runDir, "steve.log"),
		KeepCluster:                  true,
		EnableMetrics:                req.EnableMetrics,
		MetricsUpdateIntervalSeconds: metricsInterval,
		ExtraEnv:                     extraEnv,
		ExtraArgs:                    extraArgs,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
	op := p.operationLocked(panelOperationSteveLab)
	op.Running = true
	op.InProcess = true
	op.PID = 0
	op.StartedAt = &now
	op.FinishedAt = nil
	op.Error = ""
	op.RunID = runID
	op.Command = fmt.Sprintf("Steve %s on k3d %s", ref, k3sVersion)
	op.UpdatedAt = &now
	op.Output = []string{
		fmt.Sprintf("[steve-lab] Run %s", runID),
		fmt.Sprintf("[steve-lab] Steve ref: %s", ref),
		fmt.Sprintf("[steve-lab] k3d image: %s", k3sImage(k3sVersion)),
	}
	if record.EnableMetrics {
		op.Output = append(op.Output, fmt.Sprintf("[steve-lab] Prometheus metrics enabled with %ds update interval", record.MetricsUpdateIntervalSeconds))
	}
	if len(record.ExtraEnv) > 0 {
		op.Output = append(op.Output, fmt.Sprintf("[steve-lab] Custom Steve env vars: %d", len(record.ExtraEnv)))
	}
	if len(record.ExtraArgs) > 0 {
		op.Output = append(op.Output, fmt.Sprintf("[steve-lab] Custom Steve args: %s", strings.Join(record.ExtraArgs, " ")))
	}
	p.persistOperationsLocked()
	if err := p.writeSteveLabRunRecord(record); err != nil {
		op.Running = false
		op.InProcess = false
		op.Error = err.Error()
		p.persistOperationsLocked()
		return err
	}
	if err := p.startPanelWorker(func() { p.runSteveLab(record) }); err != nil {
		op.Running = false
		op.Error = err.Error()
		p.persistOperationsLocked()
		return err
	}
	return nil
}

func (p *localControlPanel) runSteveLab(record steveLabRunRecord) {
	err := p.runSteveLabSteps(&record)
	if err != nil {
		record.Status = "failed"
		record.Error = err.Error()
		record.UpdatedAt = time.Now()
		_ = p.writeSteveLabRunRecord(record)
		p.finishSteveLabOperation(err)
		return
	}
	record.Status = "serving"
	record.Error = ""
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(record)
	p.finishSteveLabOperation(nil)
}

func (p *localControlPanel) runSteveLabSteps(record *steveLabRunRecord) error {
	if err := os.MkdirAll(record.RunDir, 0o755); err != nil {
		return err
	}
	if err := p.downloadSteveSourceArchive(record); err != nil {
		return err
	}
	record.SteveCommit = record.SteveRef
	if commit, err := resolveSteveGitHubCommit(record.SteveRef); err == nil && commit != "" {
		record.SteveCommit = commit
	}

	if err := p.prepareSteveEndpoint(record); err != nil {
		return err
	}
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(*record)

	if err := p.runSteveLabCommand(record, record.RunDir, "k3d", "cluster", "create", record.ClusterName, "--image", k3sImage(record.K3SVersion), "--wait", "--timeout", "180s"); err != nil {
		return err
	}
	k3dPath, err := resolveLocalToolPath("k3d")
	if err != nil {
		return fmt.Errorf("k3d was not found: %w", err)
	}
	kubeconfigCmd := exec.Command(k3dPath, "kubeconfig", "get", record.ClusterName)
	kubeconfigCmd.Env = localToolEnv(nil)
	kubeconfig, err := kubeconfigCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to export k3d kubeconfig: %w", err)
	}
	if err := os.WriteFile(record.Kubeconfig, kubeconfig, 0o600); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Wrote kubeconfig "+record.Kubeconfig)
	if err := p.runSteveLabCommand(record, record.RunDir, "kubectl", "--kubeconfig", record.Kubeconfig, "wait", "node", "--all", "--for=condition=Ready", "--timeout=180s"); err != nil {
		return err
	}

	if record.SQLCache {
		p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Applying Project CRD prerequisites for SQL cache...")
		if err := ensureSQLCachePrereqs(record.Kubeconfig); err != nil {
			p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Warning: failed to apply SQL cache prerequisites: "+err.Error())
		}
	}

	return p.startSteveEndpoint(record)
}
