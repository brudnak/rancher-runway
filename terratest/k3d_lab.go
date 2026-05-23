package test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type k3dLabPanelState struct {
	Preflight   systemReadinessState   `json:"preflight"`
	Operation   panelOperationSnapshot `json:"operation"`
	Clusters    []k3dLabRecord         `json:"clusters"`
	K3SVersions []string               `json:"k3sVersions"`
}

type k3dLabRecord struct {
	RunID       string    `json:"runId"`
	Status      string    `json:"status"`
	K3SVersion  string    `json:"k3sVersion"`
	ClusterName string    `json:"clusterName"`
	APIPort     int       `json:"apiPort,omitempty"`
	APIURL      string    `json:"apiUrl,omitempty"`
	Kubeconfig  string    `json:"kubeconfig"`
	RunDir      string    `json:"runDir"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Error       string    `json:"error,omitempty"`
}

type k3dLabStartRequest struct {
	K3SVersion string `json:"k3sVersion"`
	APIPort    int    `json:"apiPort"`
}

func (p *localControlPanel) handleK3DLabState(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, p.k3dLabPanelState(true))
}

func (p *localControlPanel) handleK3DLabStart(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req k3dLabStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := p.startK3DLab(req); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "k3d lab started"})
}

func (p *localControlPanel) handleK3DLabInstall(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := p.startK3DLabInstallK3D(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "k3d install started"})
}

func (p *localControlPanel) handleK3DLabStop(w http.ResponseWriter, r *http.Request) {
	p.handleK3DLabLifecycleAction(w, r, "stop")
}

func (p *localControlPanel) handleK3DLabRestart(w http.ResponseWriter, r *http.Request) {
	p.handleK3DLabLifecycleAction(w, r, "restart")
}

func (p *localControlPanel) handleK3DLabDelete(w http.ResponseWriter, r *http.Request) {
	p.handleK3DLabLifecycleAction(w, r, "delete")
}

func (p *localControlPanel) handleK3DLabKubeconfigSave(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	runID := safeRunPathSegment(req.RunID)
	if runID == "" {
		http.Error(w, "runId is required", http.StatusBadRequest)
		return
	}
	record, ok := p.readK3DLabRecord(runID)
	if !ok {
		http.Error(w, "K3D Lab cluster not found", http.StatusNotFound)
		return
	}
	content, filename, err := localLabKubeconfigContent(record.Kubeconfig, k3dLabKubeconfigDownloadName(record))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	path, err := saveDownloadFile(filename, content, 0o600)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{
		"filename": filepath.Base(path),
		"path":     path,
	})
}

func (p *localControlPanel) handleK3DLabOutputClear(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p.mu.Lock()
	op := p.operationLocked(panelOperationK3DLab)
	op.Output = nil
	now := time.Now()
	op.UpdatedAt = &now
	p.persistOperationsLocked()
	p.mu.Unlock()
	writeJSON(w, map[string]string{"status": "cleared"})
}

func (p *localControlPanel) handleK3DLabLifecycleAction(w http.ResponseWriter, r *http.Request, action string) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RunID     string `json:"runId"`
		DeleteDir bool   `json:"deleteDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	record, err := p.runK3DLabAction(req.RunID, action, req.DeleteDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"status": action, "cluster": record})
}

func (p *localControlPanel) k3dLabPanelState(includePreflight bool) k3dLabPanelState {
	preflight := systemReadinessState{Ready: false, Summary: "Open K3D Lab to check local Docker and k3d tools."}
	if includePreflight {
		preflight = collectK3DLabPreflight()
	}
	return k3dLabPanelState{
		Preflight:   preflight,
		Operation:   p.snapshotOperation(panelOperationK3DLab),
		Clusters:    p.listK3DLabRecords(),
		K3SVersions: defaultK3SVersions(),
	}
}

func collectK3DLabPreflight() systemReadinessState {
	tools := []systemReadinessToolConfig{
		{Name: "Docker", Command: "docker", Args: []string{"version", "--format", "{{.Server.Version}}"}, VersionPattern: `([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "24.0.0"},
		{Name: "k3d", Command: "k3d", Args: []string{"version"}, VersionPattern: `k3d version v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "5.6.0", RecommendedVersion: "5.8.3"},
		{Name: "kubectl", Command: "kubectl", Args: []string{"version", "--client=true"}, VersionPattern: `Client Version: v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "1.30.0"},
	}
	items := make([]systemReadinessItem, 0, len(tools))
	for _, tool := range tools {
		item := checkSystemReadinessTool(tool)
		if tool.Name == "kubectl" && item.Status == "error" {
			item.Status = "warning"
			item.Detail = item.Detail + " K3D Lab can still create clusters, but the post-create node readiness check will be skipped."
		}
		items = append(items, item)
	}
	ready := true
	warnings := 0
	for _, item := range items {
		if item.Status == "error" && item.Name != "kubectl" {
			ready = false
		}
		if item.Status == "warning" {
			warnings++
		}
	}
	summary := "Ready to run k3d locally"
	if !ready {
		summary = "K3D Lab needs local tools before it can run"
	} else if warnings > 0 {
		summary = fmt.Sprintf("Ready with %d warning(s)", warnings)
	}
	return systemReadinessState{Ready: ready, Summary: summary, Items: items}
}

func (p *localControlPanel) startK3DLab(req k3dLabStartRequest) error {
	preflight := collectK3DLabPreflight()
	if !preflight.Ready {
		return fmt.Errorf("K3D Lab preflight blocked run: %s", preflight.Summary)
	}
	k3sVersion := normalizeSteveLabK3SVersion(req.K3SVersion)
	if k3sVersion == "" {
		k3sVersion = defaultK3SVersions()[0]
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conflictingOperationRunningLocked(panelOperationK3DLab) {
		return fmt.Errorf("K3D Lab is already running an action")
	}
	token, err := randomConfirmationToken()
	if err != nil {
		return fmt.Errorf("failed to create K3D Lab id: %w", err)
	}
	runID := "k3d-" + token[:8]
	now := time.Now()
	runDir := filepath.Join(p.k3dLabRunsDir(), runID)
	apiPort := req.APIPort
	if apiPort <= 0 {
		apiPort, err = p.allocateLocalLabPort()
		if err != nil {
			return fmt.Errorf("failed to allocate k3d API port: %w", err)
		}
	} else if err := p.ensureLocalLabPortAvailable(apiPort); err != nil {
		return err
	}
	record := k3dLabRecord{
		RunID:       runID,
		Status:      "creating",
		K3SVersion:  k3sVersion,
		ClusterName: "rancher-runway-" + runID,
		APIPort:     apiPort,
		APIURL:      fmt.Sprintf("https://127.0.0.1:%d", apiPort),
		Kubeconfig:  filepath.Join(runDir, "kubeconfig.yaml"),
		RunDir:      runDir,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	op := p.operationLocked(panelOperationK3DLab)
	op.Running = true
	op.PID = 0
	op.StartedAt = &now
	op.FinishedAt = nil
	op.Error = ""
	op.RunID = runID
	op.Command = fmt.Sprintf("k3d %s on %s", record.ClusterName, k3sVersion)
	op.UpdatedAt = &now
	op.Output = []string{
		fmt.Sprintf("[k3d-lab] Run %s", runID),
		fmt.Sprintf("[k3d-lab] k3d image: %s", k3sImage(k3sVersion)),
		fmt.Sprintf("[k3d-lab] API endpoint: %s", record.APIURL),
	}
	p.persistOperationsLocked()
	if err := p.writeK3DLabRecord(record); err != nil {
		op.Running = false
		op.Error = err.Error()
		p.persistOperationsLocked()
		return err
	}
	go p.runK3DLabCreate(record)
	return nil
}

func (p *localControlPanel) runK3DLabCreate(record k3dLabRecord) {
	err := p.runK3DLabCreateSteps(&record)
	if err != nil {
		record.Status = "failed"
		record.Error = err.Error()
		record.UpdatedAt = time.Now()
		_ = p.writeK3DLabRecord(record)
		p.finishK3DLabOperation(err)
		return
	}
	record.Status = "running"
	record.Error = ""
	record.UpdatedAt = time.Now()
	_ = p.writeK3DLabRecord(record)
	p.finishK3DLabOperation(nil)
}

func (p *localControlPanel) runK3DLabCreateSteps(record *k3dLabRecord) error {
	if err := os.MkdirAll(record.RunDir, 0o755); err != nil {
		return err
	}
	if err := p.runK3DLabCommand(record, record.RunDir, "k3d", "cluster", "create", record.ClusterName, "--image", k3sImage(record.K3SVersion), "--api-port", fmt.Sprintf("127.0.0.1:%d", record.APIPort), "--wait", "--timeout", "180s"); err != nil {
		return err
	}
	if err := p.exportK3DLabKubeconfig(record); err != nil {
		return err
	}
	if _, err := resolveLocalToolPath("kubectl"); err != nil {
		p.appendOperationOutput(panelOperationK3DLab, "[k3d-lab] kubectl was not found; skipping node readiness wait after k3d reported the cluster ready")
		return nil
	}
	if err := p.runK3DLabCommand(record, record.RunDir, "kubectl", "--kubeconfig", record.Kubeconfig, "wait", "node", "--all", "--for=condition=Ready", "--timeout=180s"); err != nil {
		p.appendOperationOutput(panelOperationK3DLab, "[k3d-lab] kubectl readiness wait failed after kubeconfig export; leaving the cluster record available: "+err.Error())
	}
	return nil
}

func (p *localControlPanel) startK3DLabInstallK3D() error {
	brewPath, err := resolveLocalToolPath("brew")
	if err != nil {
		return fmt.Errorf("Homebrew is required for one-click k3d install and was not found")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conflictingOperationRunningLocked(panelOperationK3DLab) {
		return fmt.Errorf("K3D Lab is already running an action")
	}
	now := time.Now()
	op := p.operationLocked(panelOperationK3DLab)
	op.Running = true
	op.PID = 0
	op.StartedAt = &now
	op.FinishedAt = nil
	op.Error = ""
	op.RunID = "install-k3d"
	op.Command = "brew install k3d"
	op.UpdatedAt = &now
	op.Output = []string{
		"[k3d-lab] Installing k3d with Homebrew",
		"[k3d-lab] $ brew install k3d",
	}
	p.persistOperationsLocked()
	go p.runK3DLabInstallK3D(brewPath)
	return nil
}

func (p *localControlPanel) runK3DLabInstallK3D(brewPath string) {
	cmd := exec.Command(brewPath, "install", "k3d")
	cmd.Env = localToolEnv(nil)
	cmd.SysProcAttr = panelCommandSysProcAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.finishK3DLabOperation(err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.finishK3DLabOperation(err)
		return
	}
	if err := cmd.Start(); err != nil {
		p.finishK3DLabOperation(err)
		return
	}
	p.setOperationPID(panelOperationK3DLab, cmd.Process.Pid)
	var wg sync.WaitGroup
	wg.Add(2)
	go p.capturePanelCommandStream(&wg, panelOperationK3DLab, stdout)
	go p.capturePanelCommandStream(&wg, panelOperationK3DLab, stderr)
	wg.Wait()
	err = cmd.Wait()
	if err == nil {
		p.appendOperationOutput(panelOperationK3DLab, "[k3d-lab] k3d installed. Refresh tools to unlock Start k3d.")
	}
	p.finishK3DLabOperation(err)
}

func (p *localControlPanel) runK3DLabAction(runID string, action string, deleteDir bool) (record k3dLabRecord, actionErr error) {
	record, ok := p.readK3DLabRecord(runID)
	if !ok {
		return k3dLabRecord{}, fmt.Errorf("K3D Lab cluster not found: %s", runID)
	}

	p.mu.Lock()
	if p.conflictingOperationRunningLocked(panelOperationK3DLab) {
		p.mu.Unlock()
		return record, fmt.Errorf("K3D Lab is already running an action")
	}
	now := time.Now()
	op := p.operationLocked(panelOperationK3DLab)
	op.Running = true
	op.PID = 0
	op.StartedAt = &now
	op.FinishedAt = nil
	op.Error = ""
	op.RunID = runID
	op.Command = fmt.Sprintf("k3d %s %s", action, record.ClusterName)
	op.UpdatedAt = &now
	op.Output = []string{
		fmt.Sprintf("[k3d-lab] %s %s", strings.ToUpper(action[:1])+action[1:], runID),
		fmt.Sprintf("[k3d-lab] API endpoint: %s", record.APIURL),
	}
	p.persistOperationsLocked()
	p.mu.Unlock()
	defer func() {
		p.finishK3DLabOperation(actionErr)
	}()

	switch action {
	case "stop":
		if err := p.runK3DLabCommand(&record, record.RunDir, "k3d", "cluster", "stop", record.ClusterName); err != nil {
			return record, err
		}
		record.Status = "stopped"
	case "restart":
		if err := p.runK3DLabCommand(&record, record.RunDir, "k3d", "cluster", "start", record.ClusterName); err != nil {
			return record, err
		}
		record.Status = "running"
		record.Error = ""
		if err := p.exportK3DLabKubeconfig(&record); err != nil {
			return record, err
		}
	case "delete":
		if err := p.runK3DLabCommand(&record, record.RunDir, "k3d", "cluster", "delete", record.ClusterName); err != nil {
			return record, err
		}
		record.Status = "deleted"
		if deleteDir && strings.TrimSpace(record.RunDir) != "" {
			if err := os.RemoveAll(record.RunDir); err != nil {
				return record, err
			}
		}
		if deleteDir {
			if err := p.deleteK3DLabRecord(record.RunID); err != nil {
				return record, err
			}
			return record, nil
		}
	default:
		return record, fmt.Errorf("unsupported k3d action %q", action)
	}
	record.UpdatedAt = time.Now()
	if err := p.writeK3DLabRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

func (p *localControlPanel) exportK3DLabKubeconfig(record *k3dLabRecord) error {
	k3dPath, err := resolveLocalToolPath("k3d")
	if err != nil {
		return fmt.Errorf("k3d was not found: %w", err)
	}
	cmd := exec.Command(k3dPath, "kubeconfig", "get", record.ClusterName)
	cmd.Env = localToolEnv(nil)
	kubeconfig, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to export k3d kubeconfig: %w", err)
	}
	return os.WriteFile(record.Kubeconfig, kubeconfig, 0o600)
}

func (p *localControlPanel) runK3DLabCommand(record *k3dLabRecord, dir string, name string, args ...string) error {
	p.appendOperationOutput(panelOperationK3DLab, "[k3d-lab] $ "+name+" "+strings.Join(args, " "))
	path, err := resolveLocalToolPath(name)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = localToolEnv(nil)
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
	p.setOperationPID(panelOperationK3DLab, cmd.Process.Pid)
	var wg sync.WaitGroup
	wg.Add(2)
	go p.capturePanelCommandStream(&wg, panelOperationK3DLab, stdout)
	go p.capturePanelCommandStream(&wg, panelOperationK3DLab, stderr)
	wg.Wait()
	record.UpdatedAt = time.Now()
	_ = p.writeK3DLabRecord(*record)
	return cmd.Wait()
}

func (p *localControlPanel) finishK3DLabOperation(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	op := p.operationLocked(panelOperationK3DLab)
	op.Running = false
	op.PID = 0
	finished := time.Now()
	op.FinishedAt = &finished
	op.UpdatedAt = &finished
	if err != nil {
		op.Error = err.Error()
		op.Output = append(op.Output, "[k3d-lab] Finished with error: "+err.Error())
	} else {
		op.Error = ""
		op.Output = append(op.Output, "[k3d-lab] K3D Lab action completed successfully")
	}
	p.persistOperationsLocked()
}

func (p *localControlPanel) allocateLocalLabPort() (int, error) {
	used := p.localLabUsedPorts()
	for i := 0; i < 25; i++ {
		port, err := freeLocalPort()
		if err != nil {
			return 0, err
		}
		if used[port] {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("could not allocate an unused local lab port")
}

func (p *localControlPanel) ensureLocalLabPortAvailable(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	if p.localLabUsedPorts()[port] {
		return fmt.Errorf("port %d is already used by a local lab endpoint", port)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("port %d is not available: %w", port, err)
	}
	_ = listener.Close()
	return nil
}

func (p *localControlPanel) localLabUsedPorts() map[int]bool {
	used := map[int]bool{}
	for _, record := range p.listSteveLabRunRecords() {
		if record.Status == "serving" || record.Status == "starting" || record.StevePID > 0 {
			if record.HTTPPort > 0 {
				used[record.HTTPPort] = true
			}
			if record.HTTPSPort > 0 {
				used[record.HTTPSPort] = true
			}
		}
	}
	for _, record := range p.listK3DLabRecords() {
		if record.Status == "running" || record.Status == "creating" || record.Status == "stopped" {
			if record.APIPort > 0 {
				used[record.APIPort] = true
			}
		}
	}
	return used
}

func (p *localControlPanel) k3dLabRunsDir() string {
	path := filepath.Join(automationOutputDir(), "control-panel", "k3d-lab", "runs")
	if abs, err := absoluteFromWorkingDir(path); err == nil {
		return abs
	}
	return path
}

func (p *localControlPanel) k3dLabRecordPath(runID string) string {
	return filepath.Join(p.k3dLabRunsDir(), safeRunPathSegment(runID)+".json")
}

func (p *localControlPanel) writeK3DLabRecord(record k3dLabRecord) error {
	if err := os.MkdirAll(p.k3dLabRunsDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.k3dLabRecordPath(record.RunID), data, 0o600)
}

func (p *localControlPanel) deleteK3DLabRecord(runID string) error {
	path := p.k3dLabRecordPath(runID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (p *localControlPanel) readK3DLabRecord(runID string) (k3dLabRecord, bool) {
	data, err := os.ReadFile(p.k3dLabRecordPath(runID))
	if err != nil {
		return k3dLabRecord{}, false
	}
	var record k3dLabRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return k3dLabRecord{}, false
	}
	return record, true
}

func (p *localControlPanel) listK3DLabRecords() []k3dLabRecord {
	entries, err := os.ReadDir(p.k3dLabRunsDir())
	if err != nil {
		return nil
	}
	records := make([]k3dLabRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.k3dLabRunsDir(), entry.Name()))
		if err != nil {
			continue
		}
		var record k3dLabRecord
		if err := json.Unmarshal(data, &record); err == nil {
			if record.Status == "deleted" {
				continue
			}
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records
}
