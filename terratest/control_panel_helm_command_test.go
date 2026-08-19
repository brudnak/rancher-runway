package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseLocalHAClusterID(t *testing.T) {
	tests := []struct {
		name      string
		clusterID string
		runID     string
		haIndex   int
		ok        bool
	}{
		{name: "legacy", clusterID: "ha-1-local", haIndex: 1, ok: true},
		{name: "scoped", clusterID: "run-91877b96-ha-2-local", runID: "91877b96", haIndex: 2, ok: true},
		{name: "hyphenated run", clusterID: "run-qa-ha-3-ha-2-local", runID: "qa-ha-3", haIndex: 2, ok: true},
		{name: "downstream", clusterID: "run-91877b96-ha-1-downstream-fleet-default-qa", ok: false},
		{name: "missing run", clusterID: "run--ha-1-local", ok: false},
		{name: "unsafe run", clusterID: "run-..-ha-1-local", ok: false},
		{name: "unknown run", clusterID: "run-unknown-ha-1-local", ok: false},
		{name: "zero index", clusterID: "run-91877b96-ha-0-local", ok: false},
		{name: "noncanonical index", clusterID: "run-91877b96-ha-01-local", ok: false},
		{name: "missing suffix", clusterID: "run-91877b96-ha-1", ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runID, haIndex, ok := parseLocalHAClusterID(test.clusterID)
			if ok != test.ok || runID != test.runID || haIndex != test.haIndex {
				t.Fatalf("parseLocalHAClusterID(%q) = (%q, %d, %t), want (%q, %d, %t)", test.clusterID, runID, haIndex, ok, test.runID, test.haIndex, test.ok)
			}
		})
	}
}

func TestRecordedLocalHAClusterByIDUsesRunRecordArtifactPath(t *testing.T) {
	panel, haDir := newRecordedLocalHAFixture(t, "qa-ha-3", 2, 2)

	cluster, ok := panel.recordedLocalHAClusterByID("run-qa-ha-3-ha-2-local")
	if !ok {
		t.Fatal("expected recorded local HA cluster to resolve")
	}
	if cluster.ID != "run-qa-ha-3-ha-2-local" || cluster.RunID != "qa-ha-3" || cluster.HAIndex != 2 || cluster.Type != "local" {
		t.Fatalf("unexpected resolved cluster: %#v", cluster)
	}
	if want := filepath.Join(haDir, "kube_config.yaml"); cluster.KubeconfigPath != want {
		t.Fatalf("expected kubeconfig path %q, got %q", want, cluster.KubeconfigPath)
	}

	if _, ok := panel.recordedLocalHAClusterByID("run-qa-ha-3-ha-3-local"); ok {
		t.Fatal("expected out-of-range HA index to be rejected")
	}
}

func TestRecordedLocalHAClusterByIDRejectsNonHADeployment(t *testing.T) {
	panel, _ := newRecordedLocalHAFixture(t, "hosted123", 1, 1)
	record, ok := panel.readRunRecord("hosted123")
	if !ok {
		t.Fatal("expected fixture run record")
	}
	record.DeploymentType = deploymentTypeHostedTenantK3S
	panel.writeRunRecord(record)

	if _, ok := panel.recordedLocalHAClusterByID("run-hosted123-ha-1-local"); ok {
		t.Fatal("expected hosted-tenant record to be rejected as a local HA cluster")
	}
}

func TestHandleHelmCommandDownloadUsesRecordedLocalPathWithoutDiscovery(t *testing.T) {
	panel, _ := newRecordedLocalHAFixture(t, "91877b96", 1, 1)
	marker := installDiscoveryCommandSentinels(t)

	tests := []struct {
		name       string
		query      string
		wantPrefix string
	}{
		{name: "install", query: "", wantPrefix: "helm install rancher rancher-latest/rancher"},
		{name: "upgrade", query: "&mode=upgrade", wantPrefix: "helm upgrade --install rancher rancher-latest/rancher"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/helm-command?cluster=run-91877b96-ha-1-local"+test.query, nil)
			request.Header.Set("X-Control-Panel-Token", "token")
			recorder := httptest.NewRecorder()

			panel.handleHelmCommandDownload(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status ok, got %d: %s", recorder.Code, recorder.Body.String())
			}
			if !strings.HasPrefix(recorder.Body.String(), test.wantPrefix) {
				t.Fatalf("expected command prefix %q, got:\n%s", test.wantPrefix, recorder.Body.String())
			}
		})
	}

	if data, err := os.ReadFile(marker); err == nil {
		t.Fatalf("endpoint invoked live discovery command(s): %s", strings.TrimSpace(string(data)))
	} else if !os.IsNotExist(err) {
		t.Fatalf("failed to inspect discovery marker: %v", err)
	}
}

func TestHandleHelmCommandDownloadPreservesRequestGuardsAndReadErrors(t *testing.T) {
	panel, haDir := newRecordedLocalHAFixture(t, "guard123", 1, 1)
	marker := installDiscoveryCommandSentinels(t)

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/helm-command?cluster=run-guard123-ha-1-local", nil)
	unauthorizedRecorder := httptest.NewRecorder()
	panel.handleHelmCommandDownload(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected unauthorized request to be forbidden, got %d: %s", unauthorizedRecorder.Code, unauthorizedRecorder.Body.String())
	}

	wrongMethod := httptest.NewRequest(http.MethodPost, "/api/helm-command?cluster=run-guard123-ha-1-local", nil)
	wrongMethod.Header.Set("X-Control-Panel-Token", "token")
	wrongMethodRecorder := httptest.NewRecorder()
	panel.handleHelmCommandDownload(wrongMethodRecorder, wrongMethod)
	if wrongMethodRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected non-GET request to be rejected, got %d: %s", wrongMethodRecorder.Code, wrongMethodRecorder.Body.String())
	}

	missingCluster := httptest.NewRequest(http.MethodGet, "/api/helm-command", nil)
	missingCluster.Header.Set("X-Control-Panel-Token", "token")
	missingClusterRecorder := httptest.NewRecorder()
	panel.handleHelmCommandDownload(missingClusterRecorder, missingCluster)
	if missingClusterRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected missing cluster to be rejected, got %d: %s", missingClusterRecorder.Code, missingClusterRecorder.Body.String())
	}

	if err := os.Remove(filepath.Join(haDir, "install.sh")); err != nil {
		t.Fatalf("failed to remove install script fixture: %v", err)
	}
	missingScript := httptest.NewRequest(http.MethodGet, "/api/helm-command?cluster=run-guard123-ha-1-local", nil)
	missingScript.Header.Set("X-Control-Panel-Token", "token")
	missingScriptRecorder := httptest.NewRecorder()
	panel.handleHelmCommandDownload(missingScriptRecorder, missingScript)
	if missingScriptRecorder.Code != http.StatusBadGateway {
		t.Fatalf("expected missing install script to remain a bad gateway error, got %d: %s", missingScriptRecorder.Code, missingScriptRecorder.Body.String())
	}
	if !strings.Contains(missingScriptRecorder.Body.String(), "failed to read install script") {
		t.Fatalf("expected install script read error, got %q", missingScriptRecorder.Body.String())
	}

	if data, err := os.ReadFile(marker); err == nil {
		t.Fatalf("guard or local read-error request invoked live discovery command(s): %s", strings.TrimSpace(string(data)))
	} else if !os.IsNotExist(err) {
		t.Fatalf("failed to inspect discovery marker: %v", err)
	}
}

// This benchmark covers the latency-sensitive path: a single run-record read
// and local artifact stat must replace cluster discovery, which can issue one
// Terraform process and several kubectl requests with 5-second timeouts.
func BenchmarkRecordedLocalHAClusterByID(b *testing.B) {
	panel, _ := newRecordedLocalHAFixture(b, "bench123", 1, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := panel.recordedLocalHAClusterByID("run-bench123-ha-1-local"); !ok {
			b.Fatal("expected benchmark cluster to resolve")
		}
	}
}

func newRecordedLocalHAFixture(tb testing.TB, runID string, haIndex, totalHAs int) (*localControlPanel, string) {
	tb.Helper()
	workspace := tb.TempDir()
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	tb.Setenv("GITHUB_WORKSPACE", testDir)

	haRoot := filepath.Join(testDir, "automation-output", "runs", runID, "ha")
	haDir := filepath.Join(haRoot, "high-availability-"+strconv.Itoa(haIndex))
	if err := os.MkdirAll(haDir, 0o755); err != nil {
		tb.Fatalf("failed to create HA fixture directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(haDir, "kube_config.yaml"), []byte("apiVersion: v1\n"), 0o600); err != nil {
		tb.Fatalf("failed to write kubeconfig fixture: %v", err)
	}
	installScript := `#!/bin/bash
helm install rancher rancher-latest/rancher \
  --namespace cattle-system \
  --version 2.15.0 \
  --set tls=external
`
	if err := os.WriteFile(filepath.Join(haDir, "install.sh"), []byte(installScript), 0o700); err != nil {
		tb.Fatalf("failed to write install script fixture: %v", err)
	}

	panel := &localControlPanel{
		token:      "token",
		totalHAs:   totalHAs,
		repoRoot:   repoRoot,
		testDir:    testDir,
		operations: newPanelOperations(),
	}
	panel.writeRunRecord(panelRunRecord{
		RunID:          runID,
		SlotID:         panelRunSlotID(runID),
		Status:         "ready",
		DeploymentType: deploymentTypeHARKE2,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		TotalHAs:       totalHAs,
		HAOutputRoot:   haRoot,
	})
	return panel, haDir
}

func installDiscoveryCommandSentinels(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	marker := filepath.Join(binDir, "called")
	t.Setenv("CONTROL_PANEL_DISCOVERY_MARKER", marker)
	script := "#!/bin/sh\nprintf '%s\\n' \"$0\" >> \"$CONTROL_PANEL_DISCOVERY_MARKER\"\nexit 97\n"
	for _, name := range []string{"terraform", "kubectl"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o700); err != nil {
			t.Fatalf("failed to write %s sentinel: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)
	return marker
}
