package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (p *localControlPanel) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.Error(w, "direct setup start is disabled; use Start isolated run so the run gets a dedicated slot and state", http.StatusGone)
}

func (p *localControlPanel) handleRunSlotStart(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.Error(w, "direct isolated run start is disabled; open the Setup tab, resolve the plan, then press Continue", http.StatusGone)
}

func (p *localControlPanel) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := p.startReadiness(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "readiness started"})
}

func (p *localControlPanel) handleDownstreamRetry(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RunID   string `json:"runId"`
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Confirm != typedConfirmationPhrase {
		http.Error(w, "typed confirmation must equal "+typedConfirmationPhrase, http.StatusBadRequest)
		return
	}
	if err := p.startConfiguredDownstreamsForRun(req.RunID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "downstream provisioning started"})
}

func (p *localControlPanel) handleAbortOperation(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Operation string `json:"operation"`
		RunID     string `json:"runId"`
		Confirm   string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Confirm != typedConfirmationPhrase {
		http.Error(w, "typed confirmation must equal "+typedConfirmationPhrase, http.StatusBadRequest)
		return
	}

	operation, validOperation := parsePanelOperationName(req.Operation)
	if !validOperation {
		http.Error(w, "operation must be setup, readiness, downstream, cleanup, linodeSetup, linodeCleanup, cleanupBatch, steveLab, or k3dLab", http.StatusBadRequest)
		return
	}

	if err := p.abortOperation(operation, req.RunID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "stop requested"})
}

func (p *localControlPanel) handleCleanup(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Confirm         string   `json:"confirm"`
		RunID           string   `json:"runId"`
		RunIDs          []string `json:"runIds"`
		All             bool     `json:"all"`
		CleanupTestLab  bool     `json:"cleanupTestLab"`
		CleanupCacheLab bool     `json:"cleanupCacheLab"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	confirm := req.Confirm
	batchRequested := req.All || req.RunIDs != nil
	if strings.TrimSpace(req.RunID) != "" && batchRequested {
		http.Error(w, "runId cannot be combined with all or runIds", http.StatusBadRequest)
		return
	}
	if req.All && req.RunIDs != nil {
		http.Error(w, "all and runIds are mutually exclusive", http.StatusBadRequest)
		return
	}
	if batchRequested {
		if confirm != typedConfirmationPhrase {
			http.Error(w, "typed confirmation must equal "+typedConfirmationPhrase, http.StatusBadRequest)
			return
		}

		runIDs, err := p.resolveCleanupBatchRunIDs(req.RunIDs, req.All)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := p.startCleanupBatchWithLabCleanup(runIDs, req.CleanupTestLab, req.CleanupCacheLab); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]any{
			"status":       "cleanup batch started",
			"cleanupBatch": p.snapshotCleanupBatch(),
		})
		return
	}

	if confirm != typedConfirmationPhrase {
		http.Error(w, "typed confirmation must equal "+typedConfirmationPhrase, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.RunID) != "" {
		if err := p.startCleanupForRunWithLabCleanup(req.RunID, req.CleanupTestLab, req.CleanupCacheLab); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]string{"status": "cleanup started"})
		return
	}

	record, ok := p.readCurrentRunRecord()
	if !ok {
		http.Error(w, "cleanup requires a recorded run", http.StatusConflict)
		return
	}
	if err := p.startCleanupForRunWithLabCleanup(record.RunID, req.CleanupTestLab, req.CleanupCacheLab); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "cleanup started"})
}

func (p *localControlPanel) handleCostLedgerReset(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p.anyOperationRunning() {
		http.Error(w, fmt.Sprintf("cannot reset cost ledger while %s is running", p.runningOperationName()), http.StatusConflict)
		return
	}

	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Confirm != typedConfirmationPhrase {
		http.Error(w, "typed confirmation must equal "+typedConfirmationPhrase, http.StatusBadRequest)
		return
	}

	if err := resetCostLedger(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"status": "cost ledger reset",
		"costs":  discoverCostHistory(),
	})
}

func (p *localControlPanel) handleLocalArtifactsClean(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p.anyOperationRunning() {
		http.Error(w, fmt.Sprintf("cannot clean local artifacts while %s is running", p.runningOperationName()), http.StatusConflict)
		return
	}

	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Confirm != typedConfirmationPhrase {
		http.Error(w, "typed confirmation must equal "+typedConfirmationPhrase, http.StatusBadRequest)
		return
	}

	result, err := p.cleanLocalArtifacts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]any{
		"status":    "local artifacts cleaned",
		"removed":   result.Removed,
		"workspace": p.workspaceState(),
		"costs":     discoverCostHistory(),
	})
}

func (p *localControlPanel) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p.anyOperationRunning() {
		http.Error(w, fmt.Sprintf("cannot stop panel while %s is running", p.runningOperationName()), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "shutting down"})

	go func() {
		time.Sleep(150 * time.Millisecond)
		p.workers.Stop()
		p.stopCacheLab()
		p.stopTestLab()
		p.stopDailyReadiness()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.server.Shutdown(shutdownCtx)
	}()
}
