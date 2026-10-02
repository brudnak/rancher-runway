package test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (p *localControlPanel) handleSteveLabState(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, p.steveLabPanelState(true))
}

func (p *localControlPanel) handleSteveLabVersions(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, discoverSteveVersions())
}

func (p *localControlPanel) handleSteveLabRef(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		http.Error(w, "ref is required", http.StatusBadRequest)
		return
	}
	writeJSON(w, inspectSteveRefDetails(ref))
}

func (p *localControlPanel) handleSteveLabStart(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req steveLabStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := p.startSteveLab(req); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "steve lab started"})
}

func (p *localControlPanel) handleSteveLabStop(w http.ResponseWriter, r *http.Request) {
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
	record, err := p.stopSteveLabEndpoint(req.RunID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"status": "stopped", "run": record})
}

func (p *localControlPanel) handleSteveLabCleanup(w http.ResponseWriter, r *http.Request) {
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
		DeleteK3D bool   `json:"deleteK3d"`
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
	if p.snapshotOperation(panelOperationSteveLab).Running {
		http.Error(w, "Steve Lab is running; stop it before cleanup", http.StatusConflict)
		return
	}
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		http.Error(w, "Steve Lab run not found", http.StatusNotFound)
		return
	}
	if record.StevePID > 0 {
		stopped, err := p.stopSteveLabEndpoint(record.RunID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		record = stopped
	}
	var removed []string
	if req.DeleteK3D && strings.TrimSpace(record.ClusterName) != "" {
		k3dPath, err := resolveLocalToolPath("k3d")
		if err != nil {
			http.Error(w, "k3d was not found", http.StatusBadGateway)
			return
		}
		cmd := exec.Command(k3dPath, "cluster", "delete", record.ClusterName)
		cmd.Env = localToolEnv(nil)
		if output, err := cmd.CombinedOutput(); err != nil {
			trimmed := strings.TrimSpace(string(output))
			if !k3dDeleteMissingClusterOK(trimmed) {
				http.Error(w, fmt.Sprintf("failed to delete k3d cluster: %s", trimmed), http.StatusBadGateway)
				return
			}
		}
		removed = append(removed, record.ClusterName)
	}
	if req.DeleteDir && strings.TrimSpace(record.RunDir) != "" {
		if err := os.RemoveAll(record.RunDir); err != nil {
			http.Error(w, fmt.Sprintf("failed to remove run directory: %v", err), http.StatusInternalServerError)
			return
		}
		removed = append(removed, record.RunDir)
	}
	record.Status = "cleaned"
	record.UpdatedAt = time.Now()
	record.Error = ""
	if req.DeleteDir {
		_ = p.deleteSteveLabRunRecord(record.RunID)
	} else {
		_ = p.writeSteveLabRunRecord(record)
	}
	writeJSON(w, map[string]any{"status": "cleaned", "removed": removed})
}

func (p *localControlPanel) handleSteveLabKubeconfigSave(w http.ResponseWriter, r *http.Request) {
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
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		http.Error(w, "Steve Lab run not found", http.StatusNotFound)
		return
	}
	content, filename, err := localLabKubeconfigContent(record.Kubeconfig, steveLabKubeconfigDownloadName(record))
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

func (p *localControlPanel) handleSteveLabSqliteVacuum(w http.ResponseWriter, r *http.Request) {
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
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		http.Error(w, "Steve Lab run not found", http.StatusNotFound)
		return
	}

	sourceDBPath := filepath.Join(record.SourceDir, "informer_object_cache.db")
	if _, err := os.Stat(sourceDBPath); os.IsNotExist(err) {
		http.Error(w, "SQLite database file not found yet. Make sure Steve has started and initialized the cache.", http.StatusNotFound)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "failed to find home directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	downloadsDir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		http.Error(w, "failed to create Downloads directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("steve-cache-%s.db", record.RunID)
	targetDBPath := uniqueDownloadPath(downloadsDir, filename)

	db, err := sql.Open("sqlite", sourceDBPath)
	if err != nil {
		http.Error(w, "failed to open source database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	escapedPath := strings.ReplaceAll(targetDBPath, "'", "''")
	query := fmt.Sprintf("VACUUM INTO '%s'", escapedPath)
	if _, err := db.Exec(query); err != nil {
		http.Error(w, "failed to vacuum SQLite database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{
		"filename": filepath.Base(targetDBPath),
		"path":     targetDBPath,
	})
}

func (p *localControlPanel) handleSteveLabOutputClear(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p.mu.Lock()
	op := p.operationLocked(panelOperationSteveLab)
	op.Output = nil
	now := time.Now()
	op.UpdatedAt = &now
	p.persistOperationsLocked()
	p.mu.Unlock()
	writeJSON(w, map[string]string{"status": "cleared"})
}

func (p *localControlPanel) handleSteveLabLogs(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runID := safeRunPathSegment(r.URL.Query().Get("runId"))
	if runID == "" {
		http.Error(w, "runId is required", http.StatusBadRequest)
		return
	}
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		http.Error(w, "Steve Lab run not found", http.StatusNotFound)
		return
	}

	content, err := os.ReadFile(record.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]string{"text": "(no Steve log file found yet)"})
			return
		}
		http.Error(w, "failed to read Steve log: "+err.Error(), http.StatusInternalServerError)
		return
	}

	outputText := string(content)
	if len(outputText) > 1000000 {
		outputText = outputText[len(outputText)-1000000:]
	}

	writeJSON(w, map[string]string{"text": outputText})
}

func (p *localControlPanel) steveLabPanelState(includePreflight bool) steveLabPanelState {
	preflight := systemReadinessState{Ready: false, Summary: "Open Steve Lab to check local Docker and k3d tools."}
	if includePreflight {
		preflight = collectSteveLabPreflight()
	}
	return steveLabPanelState{
		Preflight:   preflight,
		Operation:   p.snapshotOperation(panelOperationSteveLab),
		Runs:        p.listSteveLabRunRecords(),
		K3SVersions: defaultK3SVersions(),
	}
}
