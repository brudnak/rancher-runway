package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInteractiveSetupConcurrentApprovalStartsOneRun(t *testing.T) {
	var starts atomic.Int32
	server := &interactiveServer{
		token: "test", phase: phaseReview, submitted: true,
		plans: []*RancherResolvedPlan{{}},
		responseHandler: func(action string, plans []*RancherResolvedPlan) (interactiveRunStart, error) {
			starts.Add(1)
			return interactiveRunStart{RunID: "prime-run", DeploymentType: deploymentTypeHARKE2}, nil
		},
	}
	mux := http.NewServeMux()
	server.registerHandlersAt(mux, nil, "")
	results := make(chan *httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			request := httptest.NewRequest(http.MethodPost, "/respond?token=test", strings.NewReader("action=continue"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			results <- response
		}()
	}
	workers.Wait()
	close(results)
	successes := 0
	for response := range results {
		if response.Code == http.StatusConflict {
			continue
		}
		if response.Code != http.StatusOK {
			t.Fatalf("unexpected response %d: %s", response.Code, response.Body)
		}
		successes++
		var result map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["runId"] != "prime-run" || result["deploymentType"] != deploymentTypeHARKE2 {
			t.Fatalf("missing run context: %s", response.Body)
		}
	}
	if successes != 1 || starts.Load() != 1 {
		t.Fatalf("successful responses %d, starts %d", successes, starts.Load())
	}
}

func TestInteractiveSetupAdmissionPreservesReviewedConfiguration(t *testing.T) {
	for _, submitted := range []bool{false, true} {
		t.Run(fmt.Sprint(submitted), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tool-config.yml")
			original := []byte("rancher:\n  distro: prime\n")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			server := &interactiveServer{
				token: "test", configPath: path, submitted: submitted,
				setupAdmission: func() error { return fmt.Errorf("two provisioning runs are already active") },
			}
			mux := http.NewServeMux()
			server.registerHandlersAt(mux, nil, "")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/submit?token=test", strings.NewReader("versions=2.13.0")))
			if !submitted && response.Code != http.StatusConflict {
				t.Fatalf("admission status %d: %s", response.Code, response.Body)
			}
			content, err := os.ReadFile(path)
			if err != nil || string(content) != string(original) {
				t.Fatal("rejected submission changed the configuration")
			}
		})
	}
}

func TestPanelCompletingEarlierRunPreservesCurrentRun(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())
	panel := &localControlPanel{}
	now := time.Now()
	panel.writeCurrentRunRecord(panelRunRecord{RunID: "prime", Status: "setup_running", UpdatedAt: now})
	panel.writeCurrentRunRecord(panelRunRecord{RunID: "community", Status: "setup_running", UpdatedAt: now})
	panel.updateRunRecordStatus("prime", "ready")
	if current, ok := panel.readCurrentRunRecord(); !ok || current.RunID != "community" {
		t.Fatalf("older completion changed current run: %#v", current)
	}
	if prime, ok := panel.readRunRecord("prime"); !ok || prime.Status != "ready" {
		t.Fatalf("older run lost its completion status: %#v", prime)
	}
}

func TestPanelRemovedRunPrunesOnlyItsCompletedOperationHistory(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())
	panel := &localControlPanel{operations: newPanelOperations()}
	panel.writeRunRecord(panelRunRecord{RunID: "removed"})
	removedKey := panelOperationForRun(panelOperationSetup, "removed")
	activeKey := panelOperationForRun(panelOperationSetup, "active")
	panel.operations[removedKey] = &panelOperationState{RunID: "removed", Output: []string{"old log"}}
	panel.operations[activeKey] = &panelOperationState{RunID: "active", Running: true}
	panel.removeRunRecord("removed")
	if _, exists := panel.operations[removedKey]; exists {
		t.Fatal("removed run retained its completed history")
	}
	if op := panel.operations[activeKey]; op == nil || !op.Running {
		t.Fatal("removing another run changed the active worker")
	}
	var persisted map[panelOperationName]*panelOperationState
	data, err := os.ReadFile(panel.lifecycleStatePath())
	if err != nil || json.Unmarshal(data, &persisted) != nil || persisted[removedKey] != nil {
		t.Fatal("removed run history remained in persisted state")
	}
}
