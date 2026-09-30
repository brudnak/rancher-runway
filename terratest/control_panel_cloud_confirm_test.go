package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestControlPanelCloudActionsRequireExactConfirmation(t *testing.T) {
	panel := newCleanupBatchTestPanel(t)
	actions := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]any
	}{
		{"retry downstream", panel.handleDownstreamRetry, map[string]any{"runId": "missing"}},
		{"stop operation", panel.handleAbortOperation, map[string]any{"operation": "cleanupBatch"}},
		{"destroy run", panel.handleCleanup, map[string]any{"runId": "missing"}},
		{"destroy all", panel.handleCleanup, map[string]any{"all": true}},
		{"destroy selected", panel.handleCleanup, map[string]any{"runIds": []string{"missing"}}},
		{"reset costs", panel.handleCostLedgerReset, map[string]any{}},
		{"clean local artifacts", panel.handleLocalArtifactsClean, map[string]any{}},
	}
	for _, action := range actions {
		t.Run(action.name, func(t *testing.T) {
			for _, phrase := range []string{"", "CONFIRM", " confirm ", action.name} {
				action.body["confirm"] = phrase
				raw, err := json.Marshal(action.body)
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(raw)))
				request.Header.Set("X-Control-Panel-Token", "token")
				response := httptest.NewRecorder()
				action.handler(response, request)
				if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "typed confirmation must equal confirm") {
					t.Fatalf("confirmation %q: status %d, body %s", phrase, response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestHandleCleanupBatchConfirmKeepsSelectedScope(t *testing.T) {
	panel := newCleanupBatchTestPanel(t,
		panelRunRecord{RunID: "run-a", CreatedAt: time.Now()},
		panelRunRecord{RunID: "run-b", CreatedAt: time.Now()},
	)
	called := make(chan string, 2)
	panel.cleanupBatchRunner = func(runID string) error {
		called <- runID
		return nil
	}
	request := httptest.NewRequest(http.MethodPost, "/api/cleanup", strings.NewReader(`{"runIds":["run-a"],"confirm":"confirm"}`))
	request.Header.Set("X-Control-Panel-Token", "token")
	response := httptest.NewRecorder()
	panel.handleCleanup(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	waitForCleanupBatch(t, panel)
	close(called)
	var runs []string
	for run := range called {
		runs = append(runs, run)
	}
	if len(runs) != 1 || runs[0] != "run-a" {
		t.Fatalf("cleanup changed scope: %v", runs)
	}
}
