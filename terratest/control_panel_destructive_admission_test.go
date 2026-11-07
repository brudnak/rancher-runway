package test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type provisioningDuringRequestReader struct {
	reader io.Reader
	start  func()
}

func (r *provisioningDuringRequestReader) Read(data []byte) (int, error) {
	if r.start != nil {
		r.start()
		r.start = nil
	}
	return r.reader.Read(data)
}

func TestLocalDestructiveActionsRecheckAdmissionAfterRequestBody(t *testing.T) {
	for _, action := range []string{"artifacts", "costs"} {
		for _, completing := range []bool{false, true} {
			name := action + "/running"
			if completing {
				name = action + "/handoff"
			}
			t.Run(name, func(t *testing.T) {
				repoRoot := t.TempDir()
				p := &localControlPanel{
					token:      "test-token",
					repoRoot:   repoRoot,
					testDir:    filepath.Join(repoRoot, "terratest"),
					operations: newPanelOperations(),
				}
				t.Setenv("GITHUB_WORKSPACE", p.testDir)
				artifact := filepath.Join(p.testDir, "automation-output", "runs", "prime", "state")
				handler := p.handleLocalArtifactsClean
				if action == "costs" {
					artifact = costLedgerPath()
					handler = p.handleCostLedgerReset
				}
				if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(artifact, []byte("preserve"), 0o600); err != nil {
					t.Fatal(err)
				}
				body := &provisioningDuringRequestReader{
					reader: strings.NewReader(`{"confirm":"confirm"}`),
					start: func() {
						// A new run can be admitted while an HTTP request body is still arriving.
						p.mu.Lock()
						p.operations[panelOperationForRun(panelOperationSetup, "prime")] = &panelOperationState{
							RunID: "prime", Running: !completing, Completing: completing,
						}
						p.mu.Unlock()
					},
				}
				request := httptest.NewRequest(http.MethodPost, "/", body)
				request.Header.Set("X-Control-Panel-Token", "test-token")
				response := httptest.NewRecorder()
				handler(response, request)
				if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "setup is running") {
					t.Fatalf("destructive action was not blocked: %d %s", response.Code, response.Body.String())
				}
				if data, err := os.ReadFile(artifact); err != nil || string(data) != "preserve" {
					t.Fatalf("active run artifact was changed: %q, %v", data, err)
				}
			})
		}
	}
}
