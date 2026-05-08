package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlPanelKubeconfigNames(t *testing.T) {
	if got := localClusterID(2); got != "ha-2-local" {
		t.Fatalf("expected local cluster id, got %q", got)
	}
	if got := downstreamClusterID(1, "fleet-default", "QA Cluster"); got != "ha-1-downstream-fleet-default-qa-cluster" {
		t.Fatalf("expected downstream cluster id, got %q", got)
	}
	if got := safeKubeconfigDownloadName("QA Cluster"); got != "qa-cluster.yaml" {
		t.Fatalf("expected safe kubeconfig download name, got %q", got)
	}
}

func TestHandleShutdownBlocksWhileLifecycleRuns(t *testing.T) {
	panel := &localControlPanel{
		token:      "token",
		operations: newPanelOperations(),
	}
	panel.operations[panelOperationSetup].Running = true

	request := httptest.NewRequest(http.MethodPost, "/api/shutdown", nil)
	request.Header.Set("X-Control-Panel-Token", "token")
	recorder := httptest.NewRecorder()

	panel.handleShutdown(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status conflict, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "setup") {
		t.Fatalf("expected running setup message, got %q", recorder.Body.String())
	}
}

func TestPruneStaleDownstreamKubeconfigsRemovesMissingClusters(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)

	cacheDir := filepath.Join(automationOutputDir(), "control-panel")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}

	activeID := downstreamClusterID(1, "fleet-default", "active")
	staleID := downstreamClusterID(1, "fleet-default", "stale")
	otherHAID := downstreamClusterID(2, "fleet-default", "stale")
	for _, id := range []string{activeID, staleID, otherHAID} {
		if err := os.WriteFile(filepath.Join(cacheDir, id+".yaml"), []byte(id), 0o600); err != nil {
			t.Fatalf("failed to write cached kubeconfig: %v", err)
		}
	}

	panel := &localControlPanel{
		downstreamKubeconfigCache: map[string]string{
			activeID: filepath.Join(cacheDir, activeID+".yaml"),
			staleID:  filepath.Join(cacheDir, staleID+".yaml"),
		},
	}

	panel.pruneStaleDownstreamKubeconfigs("", 1, map[string]bool{activeID: true})

	if _, err := os.Stat(filepath.Join(cacheDir, activeID+".yaml")); err != nil {
		t.Fatalf("expected active kubeconfig to remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, staleID+".yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected stale kubeconfig to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, otherHAID+".yaml")); err != nil {
		t.Fatalf("expected other HA kubeconfig to remain: %v", err)
	}
	if _, ok := panel.downstreamKubeconfigCache[staleID]; ok {
		t.Fatal("expected stale cache entry to be removed")
	}
	if _, ok := panel.downstreamKubeconfigCache[activeID]; !ok {
		t.Fatal("expected active cache entry to remain")
	}
}

func TestDiscoverClustersSkipsConfiguredHAWithoutRunArtifacts(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(filepath.Join(repoRoot, "modules", "aws"), 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}

	panel := &localControlPanel{
		totalHAs:   1,
		repoRoot:   repoRoot,
		testDir:    testDir,
		operations: newPanelOperations(),
	}

	if got := panel.discoverClusters(); len(got) != 0 {
		t.Fatalf("expected no phantom clusters without run artifacts, got %#v", got)
	}
}

func TestStartReadinessBlocksEmptyWorkspaceWithoutGeneratingTerraformInputs(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	terraformDir := filepath.Join(repoRoot, "modules", "aws")
	if err := os.MkdirAll(terraformDir, 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}

	panel := &localControlPanel{
		totalHAs:   1,
		repoRoot:   repoRoot,
		testDir:    testDir,
		operations: newPanelOperations(),
	}

	err := panel.startReadiness()
	if err == nil {
		t.Fatal("expected readiness to be blocked without completed setup")
	}
	if !strings.Contains(err.Error(), "completed setup") {
		t.Fatalf("expected completed setup guidance, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(terraformDir, "terraform.tfvars")); !os.IsNotExist(statErr) {
		t.Fatalf("expected readiness guard to avoid generating terraform.tfvars, stat err=%v", statErr)
	}
}

func TestDiscoverClustersShowsProvisioningHAWhileSetupRuns(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(filepath.Join(repoRoot, "modules", "aws"), 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}

	panel := &localControlPanel{
		totalHAs: 1,
		repoRoot: repoRoot,
		testDir:  testDir,
		operations: map[panelOperationName]*panelOperationState{
			panelOperationSetup:     {Running: true},
			panelOperationReadiness: {},
			panelOperationCleanup:   {},
		},
	}

	clusters := panel.discoverClusters()
	if len(clusters) != 1 {
		t.Fatalf("expected setup placeholder cluster, got %#v", clusters)
	}
	if !clusters[0].Provisioning {
		t.Fatalf("expected setup placeholder to be marked provisioning, got %#v", clusters[0])
	}
	if clusters[0].Available {
		t.Fatalf("expected setup placeholder to be unavailable until kubeconfig exists, got %#v", clusters[0])
	}
}

func TestDiscoverClustersShowsGeneratedHADirectoryAsMissing(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(filepath.Join(repoRoot, "modules", "aws"), 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(testDir, "high-availability-1"), 0o755); err != nil {
		t.Fatalf("failed to create HA dir: %v", err)
	}

	panel := &localControlPanel{
		totalHAs:   1,
		repoRoot:   repoRoot,
		testDir:    testDir,
		operations: newPanelOperations(),
	}

	clusters := panel.discoverClusters()
	if len(clusters) != 1 {
		t.Fatalf("expected generated HA directory to be shown, got %#v", clusters)
	}
	if clusters[0].Error != "kubeconfig not found" {
		t.Fatalf("expected missing kubeconfig error, got %#v", clusters[0])
	}
}

func TestPanelOperationSnapshotsInitializeAndCapOutput(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", t.TempDir())

	panel := &localControlPanel{}

	initial := panel.snapshotOperation(panelOperationReadiness)
	if initial.Running {
		t.Fatal("expected initial readiness operation to be idle")
	}
	if initial.Output == nil {
		t.Fatal("expected initial output to be an empty slice for stable JSON")
	}

	for i := 0; i < 505; i++ {
		panel.appendOperationOutput(panelOperationReadiness, "line")
	}

	snapshot := panel.snapshotOperation(panelOperationReadiness)
	if len(snapshot.Output) != 500 {
		t.Fatalf("expected output to be capped at 500 lines, got %d", len(snapshot.Output))
	}
}

func TestPanelOperationStatePersistsAndMarksStaleRunningOperations(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)

	startedAt := time.Now().Add(-time.Minute)
	panel := &localControlPanel{operations: newPanelOperations()}
	panel.operations[panelOperationSetup] = &panelOperationState{
		Running:   true,
		StartedAt: &startedAt,
		RunID:     "abc12345",
		Command:   "go test -v -run '^TestHaSetup$'",
		Output:    []string{"running"},
	}
	panel.mu.Lock()
	panel.persistOperationsLocked()
	panel.mu.Unlock()

	loaded := &localControlPanel{}
	loaded.loadPersistedOperations(true)

	snapshot := loaded.snapshotOperation(panelOperationSetup)
	if snapshot.Running {
		t.Fatal("expected restored running operation to be marked stale")
	}
	if snapshot.Error == "" {
		t.Fatal("expected stale operation to have an error")
	}
	if snapshot.RunID != "abc12345" {
		t.Fatalf("expected run id to persist, got %q", snapshot.RunID)
	}
	if !strings.Contains(strings.Join(snapshot.Output, "\n"), "marked stale") {
		t.Fatalf("expected stale marker in output, got %#v", snapshot.Output)
	}
}

func TestPanelOperationStateCanLoadReadOnlyWithoutMarkingStale(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)

	startedAt := time.Now().Add(-time.Minute)
	panel := &localControlPanel{operations: newPanelOperations()}
	panel.operations[panelOperationSetup] = &panelOperationState{
		Running:   true,
		StartedAt: &startedAt,
		RunID:     "abc12345",
		Command:   "go test -v -run '^TestHaSetup$'",
		Output:    []string{"running"},
	}
	panel.mu.Lock()
	panel.persistOperationsLocked()
	panel.mu.Unlock()

	loaded := &localControlPanel{}
	loaded.loadPersistedOperations(false)

	snapshot := loaded.snapshotOperation(panelOperationSetup)
	if !snapshot.Running {
		t.Fatal("expected read-only load to preserve running operation")
	}
	if snapshot.Error != "" {
		t.Fatalf("expected no stale error on read-only load, got %q", snapshot.Error)
	}
	if snapshot.RunID != "abc12345" {
		t.Fatalf("expected run id to persist, got %q", snapshot.RunID)
	}
}

func TestPanelOperationStateClearsCompletedCleanupOnInteractiveStartup(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)

	finishedAt := time.Now().Add(-time.Minute)
	panel := &localControlPanel{operations: newPanelOperations()}
	panel.operations[panelOperationCleanup] = &panelOperationState{
		StartedAt:  &finishedAt,
		FinishedAt: &finishedAt,
		RunID:      "abc12345",
		Command:    "go test -v -run '^TestHACleanup$'",
		Output:     []string{"[control-panel] Cleanup completed successfully"},
	}
	panel.mu.Lock()
	panel.persistOperationsLocked()
	panel.mu.Unlock()

	loaded := &localControlPanel{}
	loaded.loadPersistedOperations(true)

	snapshot := loaded.snapshotOperation(panelOperationCleanup)
	if snapshot.FinishedAt != nil {
		t.Fatal("expected successful cleanup result to clear on interactive startup")
	}
	if snapshot.RunID != "" {
		t.Fatalf("expected cleanup run id to clear, got %q", snapshot.RunID)
	}
	if len(snapshot.Output) != 0 {
		t.Fatalf("expected cleanup output to clear, got %#v", snapshot.Output)
	}
}

func TestPanelPreflightBlocksGeneratedWorkspaceState(t *testing.T) {
	workspace := t.TempDir()
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	terraformDir := filepath.Join(repoRoot, "modules", "aws")
	if err := os.MkdirAll(filepath.Join(testDir, "high-availability-1"), 0o755); err != nil {
		t.Fatalf("failed to create HA dir: %v", err)
	}
	if err := os.MkdirAll(terraformDir, 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(terraformDir, "terraform.tfvars"), []byte("generated"), 0o600); err != nil {
		t.Fatalf("failed to write terraform.tfvars: %v", err)
	}

	panel := &localControlPanel{repoRoot: repoRoot, testDir: testDir}
	item := panel.checkSetupWorkspaceState()
	if item.Status != "error" {
		t.Fatalf("expected generated workspace state to block setup, got %#v", item)
	}
	if !strings.Contains(item.Detail, "high-availability-1") {
		t.Fatalf("expected HA directory in detail, got %q", item.Detail)
	}
}

func TestPanelPreflightBlocksGeneratedTerraformInputsOnly(t *testing.T) {
	workspace := t.TempDir()
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	terraformDir := filepath.Join(repoRoot, "modules", "aws")
	if err := os.MkdirAll(terraformDir, 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(terraformDir, "terraform.tfvars"), []byte("generated"), 0o600); err != nil {
		t.Fatalf("failed to write terraform.tfvars: %v", err)
	}

	panel := &localControlPanel{repoRoot: repoRoot, testDir: testDir}
	item := panel.checkSetupWorkspaceState()
	if item.Status != "error" {
		t.Fatalf("expected generated terraform inputs to block, got %#v", item)
	}
	if !strings.Contains(item.Detail, "cleanup residue") {
		t.Fatalf("expected cleanup residue guidance, got %q", item.Detail)
	}
}

func TestPanelPreflightBlocksCurrentRunRecord(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}

	panel := &localControlPanel{repoRoot: repoRoot, testDir: testDir, totalHAs: 1}
	panel.createCurrentRunRecord("abc12345", time.Now())

	item := panel.checkSetupWorkspaceState()
	if item.Status != "blocked" {
		t.Fatalf("expected current run record to block setup, got %#v", item)
	}
	if !strings.Contains(item.Detail, "live HA run") {
		t.Fatalf("expected live run guidance, got %q", item.Detail)
	}
}

func runHAControlPanelTest(t *testing.T) {
	if err := RunHAControlPanel(""); err != nil {
		t.Fatal(err)
	}
}
