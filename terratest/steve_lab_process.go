package test

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func (p *localControlPanel) runSteveLabCommand(record *steveLabRunRecord, dir string, name string, args ...string) error {
	return p.runSteveLabCommandWithEnv(record, dir, nil, name, args...)
}

func (p *localControlPanel) runSteveLabCommandWithEnv(record *steveLabRunRecord, dir string, env []string, name string, args ...string) error {
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] $ "+name+" "+strings.Join(args, " "))
	path, err := resolveLocalToolPath(name)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = localToolEnv(env)
	cmd.SysProcAttr = panelCommandSysProcAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.setOperationPID(panelOperationSteveLab, cmd.Process.Pid)
	var wg sync.WaitGroup
	wg.Add(2)
	go p.capturePanelCommandStream(&wg, panelOperationSteveLab, stdout)
	go p.capturePanelCommandStream(&wg, panelOperationSteveLab, stderr)
	wg.Wait()
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(*record)
	err = cmd.Wait()
	p.clearOperationPID(panelOperationSteveLab, cmd.Process.Pid)
	return err
}

func (p *localControlPanel) startSteveEndpoint(record *steveLabRunRecord) error {
	binaryPath := steveEndpointBinaryPath(record)
	args := steveEndpointArgs(record)
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] $ "+binaryPath+" "+strings.Join(args, " "))
	logFile, err := os.OpenFile(record.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open Steve log: %w", err)
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = record.SourceDir
	cmd.Env = localToolEnv(steveEndpointEnv(record))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = panelCommandSysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("failed to start Steve endpoint: %w", err)
	}
	p.setOperationPID(panelOperationSteveLab, cmd.Process.Pid)
	record.StevePID = cmd.Process.Pid
	record.Status = "starting"
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(*record)
	p.appendOperationOutput(panelOperationSteveLab, fmt.Sprintf("[steve-lab] Steve endpoint started pid %d", cmd.Process.Pid))
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Logs "+record.LogPath)
	go p.watchSteveEndpointProcess(*record, cmd, logFile)
	if err := waitForLocalPort(record.HTTPSPort, cmd.Process.Pid, 2*time.Minute); err != nil {
		if processAlive(cmd.Process.Pid) {
			_ = interruptProcessTree(cmd.Process.Pid)
		} else {
			record.StevePID = 0
		}
		p.clearOperationPID(panelOperationSteveLab, cmd.Process.Pid)
		if tail := steveLogTail(record.LogPath); tail != "" {
			p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Startup log tail:\n"+tail)
		}
		return steveEndpointFailure(record, err)
	}
	record.Status = "serving"
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(*record)
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] HTTPS endpoint "+record.HTTPSURL)
	return nil
}

func steveEndpointArgs(record *steveLabRunRecord) []string {
	args := []string{
		"--kubeconfig", record.Kubeconfig,
		"--http-listen-port", fmt.Sprintf("%d", record.HTTPPort),
		"--https-listen-port", fmt.Sprintf("%d", record.HTTPSPort),
	}
	if record.SQLCacheFlag {
		args = append(args, "--sql-cache")
	}
	if record.EnableMetrics {
		args = append(args, "--enable-metrics")
		if record.MetricsUpdateIntervalSeconds > 0 && record.MetricsUpdateIntervalSeconds != 15 {
			args = append(args, fmt.Sprintf("--metrics-update-interval-seconds=%d", record.MetricsUpdateIntervalSeconds))
		}
	}
	args = append(args, record.ExtraArgs...)
	return args
}

func steveEndpointEnv(record *steveLabRunRecord) []string {
	env := []string{
		"CGO_ENABLED=0",
		"KUBECONFIG=" + record.Kubeconfig,
	}
	if record.EnableMetrics {
		env = append(env, "CATTLE_PROMETHEUS_METRICS=true")
	}
	env = append(env, record.ExtraEnv...)
	return env
}

func (p *localControlPanel) watchSteveEndpointProcess(record steveLabRunRecord, cmd *exec.Cmd, logFile *os.File) {
	err := cmd.Wait()
	_ = logFile.Close()
	// The endpoint is intentionally detached infrastructure. Only its final
	// record update belongs to the app; shutdown must not terminate the endpoint.
	_, done, workerErr := p.workers.Begin()
	if workerErr != nil {
		return
	}
	defer done()
	current, ok := p.readSteveLabRunRecord(record.RunID)
	if !ok {
		return
	}
	if current.StevePID != record.StevePID {
		return
	}
	current.StevePID = 0
	current.UpdatedAt = time.Now()
	if err != nil && current.Status != "stopped" && current.Status != "cleaned" {
		current.Status = "failed"
		current.Error = steveEndpointFailure(&current, fmt.Errorf("Steve endpoint exited: %w", err)).Error()
	} else if current.Status == "serving" {
		current.Status = "stopped"
	}
	_ = p.writeSteveLabRunRecord(current)
}

func (p *localControlPanel) stopSteveLabEndpoint(runID string) (steveLabRunRecord, error) {
	runID = safeRunPathSegment(runID)
	if runID == "" {
		return steveLabRunRecord{}, fmt.Errorf("runId is required")
	}
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		return steveLabRunRecord{}, fmt.Errorf("Steve Lab run not found: %s", runID)
	}
	if record.StevePID <= 0 {
		record.Status = "stopped"
		record.UpdatedAt = time.Now()
		_ = p.writeSteveLabRunRecord(record)
		return record, nil
	}
	if processAlive(record.StevePID) {
		if err := interruptProcessTree(record.StevePID); err != nil {
			return record, err
		}
	}
	record.StevePID = 0
	record.Status = "stopped"
	record.UpdatedAt = time.Now()
	record.Error = ""
	if err := p.writeSteveLabRunRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

func (p *localControlPanel) activeSteveLabRunRecords() []steveLabRunRecord {
	var active []steveLabRunRecord
	for _, record := range p.listSteveLabRunRecords() {
		if record.StevePID > 0 || record.Status == "running" || record.Status == "starting" || record.Status == "serving" {
			active = append(active, record)
		}
	}
	return active
}

func (p *localControlPanel) cleanupActiveSteveLabRuns(records []steveLabRunRecord) error {
	for _, record := range records {
		if record.StevePID > 0 && processAlive(record.StevePID) {
			if err := interruptProcessTree(record.StevePID); err != nil {
				return fmt.Errorf("failed to stop active Steve endpoint %s: %w", record.RunID, err)
			}
		}
		if strings.TrimSpace(record.ClusterName) != "" {
			if err := deleteK3DCluster(record.ClusterName); err != nil {
				return fmt.Errorf("failed to delete active Steve k3d cluster %s: %w", record.ClusterName, err)
			}
		}
		if strings.TrimSpace(record.RunDir) != "" {
			if err := os.RemoveAll(record.RunDir); err != nil {
				return fmt.Errorf("failed to remove active Steve run directory %s: %w", record.RunDir, err)
			}
		}
		if err := p.deleteSteveLabRunRecord(record.RunID); err != nil {
			return err
		}
	}
	return nil
}

func deleteK3DCluster(clusterName string) error {
	k3dPath, err := resolveLocalToolPath("k3d")
	if err != nil {
		return err
	}
	cmd := exec.Command(k3dPath, "cluster", "delete", clusterName)
	cmd.Env = localToolEnv(nil)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil && !strings.Contains(trimmed, "No nodes found") && !strings.Contains(trimmed, "not found") {
		if trimmed != "" {
			return fmt.Errorf("%s", trimmed)
		}
		return err
	}
	return nil
}

func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address %s", listener.Addr())
	}
	return addr.Port, nil
}

func waitForLocalPort(port int, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	var lastErr error
	for time.Now().Before(deadline) {
		if pid > 0 && !processAlive(pid) {
			return fmt.Errorf("Steve process exited before %s opened", address)
		}
		conn, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	if lastErr != nil {
		return fmt.Errorf("timed out waiting for Steve endpoint %s: %w", address, lastErr)
	}
	return fmt.Errorf("timed out waiting for Steve endpoint %s", address)
}
