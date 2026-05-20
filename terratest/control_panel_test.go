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

func TestExtractHelmCommandFromInstallScript(t *testing.T) {
	script := `#!/bin/bash
set -euo pipefail

echo "Installing Rancher..."
helm install rancher rancher-latest/rancher \
  --namespace cattle-system \
  --version 2.14.0 \
  --set tls=external

echo "Rancher installation complete!"`

	command, err := extractHelmCommandFromInstallScript(script)
	if err != nil {
		t.Fatalf("extractHelmCommandFromInstallScript returned error: %v", err)
	}
	if strings.Contains(command, "Rancher installation complete") {
		t.Fatalf("expected only Helm command, got:\n%s", command)
	}
	if !strings.Contains(command, "helm install rancher rancher-latest/rancher") || !strings.Contains(command, "--set tls=external") {
		t.Fatalf("unexpected Helm command:\n%s", command)
	}
}

func TestPrepareHelmUpgradeCommandFromInstall(t *testing.T) {
	command := `helm install rancher rancher-latest/rancher \
  --namespace cattle-system \
  --version 2.14.0 \
  --set tls=external`

	got, err := prepareHelmUpgradeCommand(command)
	if err != nil {
		t.Fatalf("prepareHelmUpgradeCommand returned error: %v", err)
	}
	if !strings.HasPrefix(got, "helm upgrade --install rancher rancher-latest/rancher") {
		t.Fatalf("expected helm upgrade --install command, got:\n%s", got)
	}
	if !strings.Contains(got, "--version 2.14.0") || !strings.Contains(got, "--set tls=external") {
		t.Fatalf("expected upgrade command to preserve flags, got:\n%s", got)
	}
}

func TestPrepareHelmUpgradeCommandAddsInstallFlag(t *testing.T) {
	got, err := prepareHelmUpgradeCommand("helm upgrade rancher rancher-latest/rancher --version 2.14.0")
	if err != nil {
		t.Fatalf("prepareHelmUpgradeCommand returned error: %v", err)
	}
	if !strings.HasPrefix(got, "helm upgrade --install rancher rancher-latest/rancher") {
		t.Fatalf("expected --install to be added, got %q", got)
	}
}

func TestResolveAllowedLocalPathBlocksOutsideCheckout(t *testing.T) {
	workspace := t.TempDir()
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	inside := filepath.Join(testDir, "automation-output", "runs", "abc12345")
	outside := filepath.Join(workspace, "outside")
	for _, dir := range []string{inside, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	panel := &localControlPanel{repoRoot: repoRoot, testDir: testDir}
	if got, err := panel.resolveAllowedLocalPath(inside); err != nil || got != inside {
		t.Fatalf("expected inside path to be allowed, got %q err=%v", got, err)
	}
	if _, err := panel.resolveAllowedLocalPath(outside); err == nil {
		t.Fatal("expected outside path to be blocked")
	}
}

func TestWorkspaceStateMarksRunFolderAvailability(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	runRoot := filepath.Join(testDir, "automation-output", "runs", "abc12345")
	moduleDir := filepath.Join(runRoot, "terraform", "module")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("failed to create run module dir: %v", err)
	}

	panel := &localControlPanel{repoRoot: repoRoot, testDir: testDir}
	panel.writeRunRecord(panelRunRecord{
		RunID:              "abc12345",
		SlotID:             "slot-abc12345",
		SlotName:           "Run abc12345",
		Status:             "ready",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		TerraformModuleDir: moduleDir,
	})

	state := panel.workspaceState()
	if len(state.Runs) != 1 {
		t.Fatalf("expected one run, got %d", len(state.Runs))
	}
	if state.Runs[0].RunFolderPath != runRoot {
		t.Fatalf("expected run folder %q, got %q", runRoot, state.Runs[0].RunFolderPath)
	}
	if !state.Runs[0].RunFolderExists {
		t.Fatal("expected run folder to be marked available")
	}

	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatalf("failed to remove run root: %v", err)
	}
	state = panel.workspaceState()
	if len(state.Runs) != 1 {
		t.Fatalf("expected one stale run record, got %d", len(state.Runs))
	}
	if state.Runs[0].RunFolderExists {
		t.Fatal("expected missing run folder to be marked unavailable")
	}
}

func TestWorkspaceStateRemapsMovedCheckoutRunFolder(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	repoRoot := filepath.Join(workspace, "rancher-runway")
	testDir := filepath.Join(repoRoot, "terratest")
	oldRepoRoot := filepath.Join(workspace, "ha-rancher-rke2")
	runRoot := filepath.Join(testDir, "automation-output", "runs", "abc12345")
	moduleDir := filepath.Join(runRoot, "terraform", "module")
	statePath := filepath.Join(runRoot, "terraform", "terraform.tfstate")
	dataDir := filepath.Join(runRoot, "terraform", ".terraform")
	haOutputRoot := filepath.Join(runRoot, "ha")
	for _, dir := range []string{moduleDir, dataDir, haOutputRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create run dir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(statePath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("failed to create terraform state: %v", err)
	}

	oldRunRoot := filepath.Join(oldRepoRoot, "terratest", "automation-output", "runs", "abc12345")
	panel := &localControlPanel{repoRoot: repoRoot, testDir: testDir}
	panel.writeRunRecord(panelRunRecord{
		RunID:              "abc12345",
		SlotID:             "slot-abc12345",
		SlotName:           "Run abc12345",
		Status:             "ready",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		TerraformBackend:   "local (" + filepath.Join(oldRunRoot, "terraform", "terraform.tfstate") + ")",
		TerraformModuleDir: filepath.Join(oldRunRoot, "terraform", "module"),
		TerraformStatePath: filepath.Join(oldRunRoot, "terraform", "terraform.tfstate"),
		TerraformDataDir:   filepath.Join(oldRunRoot, "terraform", ".terraform"),
		HAOutputRoot:       filepath.Join(oldRunRoot, "ha"),
		RunFolderPath:      oldRunRoot,
	})

	state := panel.workspaceState()
	if len(state.Runs) != 1 {
		t.Fatalf("expected one run, got %d", len(state.Runs))
	}
	run := state.Runs[0]
	if !run.RunFolderExists {
		t.Fatal("expected moved run folder to be marked available")
	}
	if run.RunFolderPath != runRoot {
		t.Fatalf("expected remapped run folder %q, got %q", runRoot, run.RunFolderPath)
	}
	if run.TerraformModuleDir != moduleDir {
		t.Fatalf("expected remapped module dir %q, got %q", moduleDir, run.TerraformModuleDir)
	}
	if run.TerraformStatePath != statePath {
		t.Fatalf("expected remapped state path %q, got %q", statePath, run.TerraformStatePath)
	}
	if run.TerraformDataDir != dataDir {
		t.Fatalf("expected remapped data dir %q, got %q", dataDir, run.TerraformDataDir)
	}
	if run.HAOutputRoot != haOutputRoot {
		t.Fatalf("expected remapped HA output root %q, got %q", haOutputRoot, run.HAOutputRoot)
	}
	if want := "local (" + statePath + ")"; run.TerraformBackend != want {
		t.Fatalf("expected remapped backend %q, got %q", want, run.TerraformBackend)
	}
	readRun, ok := panel.readRunRecord("abc12345")
	if !ok {
		t.Fatal("expected run record to be readable")
	}
	if readRun.TerraformStatePath != statePath {
		t.Fatalf("expected readRunRecord to remap state path %q, got %q", statePath, readRun.TerraformStatePath)
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

func TestHandleControlPanelStaticAssetServesAllowlistedModules(t *testing.T) {
	panel := &localControlPanel{token: "token"}

	request := httptest.NewRequest(http.MethodGet, "/static/control_panel_clusters.js", nil)
	request.Header.Set("X-Control-Panel-Token", "token")
	recorder := httptest.NewRecorder()

	panel.handleControlPanelStaticAsset(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status ok, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Fatalf("expected javascript content type, got %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "createClusterPanel") {
		t.Fatalf("expected cluster module body, got %q", recorder.Body.String())
	}
}

func TestHandleControlPanelStaticAssetBlocksUnknownAssets(t *testing.T) {
	panel := &localControlPanel{token: "token"}

	request := httptest.NewRequest(http.MethodGet, "/static/not-allowed.js", nil)
	request.Header.Set("X-Control-Panel-Token", "token")
	recorder := httptest.NewRecorder()

	panel.handleControlPanelStaticAsset(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status not found, got %d: %s", recorder.Code, recorder.Body.String())
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

func TestDiscoverLinodeDockerClustersMarksHTTPReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	panel := &localControlPanel{
		totalHAs:   1,
		repoRoot:   t.TempDir(),
		testDir:    t.TempDir(),
		operations: newPanelOperations(),
	}
	clusters := panel.discoverLinodeDockerClustersForRun(panelRunRecord{
		RunID:           "abc123",
		TotalHAs:        1,
		DeploymentType:  deploymentTypeLinodeDocker,
		RancherVersions: []string{"2.14.2-alpha3"},
	}, map[string]string{
		"linode_1_rancher_url": server.URL,
		"linode_1_ip":          "203.0.113.10",
	})

	if len(clusters) != 1 {
		t.Fatalf("expected one Linode Docker cluster, got %#v", clusters)
	}
	if clusters[0].Type != "linode" || clusters[0].DeploymentType != deploymentTypeLinodeDocker {
		t.Fatalf("expected Linode Docker cluster metadata, got %#v", clusters[0])
	}
	if clusters[0].LoadBalancer != "203.0.113.10" {
		t.Fatalf("expected Linode IP in load balancer field for compatibility, got %#v", clusters[0])
	}
	if clusters[0].KubeconfigPath != "" {
		t.Fatalf("Docker Rancher must not expose a kubeconfig path, got %#v", clusters[0])
	}
	if !clusters[0].Reachable {
		t.Fatalf("expected HTTP-ready Docker Rancher to be reachable, got %#v", clusters[0])
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

func TestSnapshotOperationHidesFailedCleanupForRemovedRun(t *testing.T) {
	finishedAt := time.Now().Add(-time.Minute)
	panel := &localControlPanel{operations: newPanelOperations()}
	panel.operations[panelOperationCleanup] = &panelOperationState{
		StartedAt:  &finishedAt,
		FinishedAt: &finishedAt,
		Error:      "exit status 1",
		RunID:      "abc12345",
		Command:    "go test -v -run '^TestHACleanup$'",
		Output:     []string{"cleanup failed"},
	}

	snapshot := panel.snapshotOperationForRuns(panelOperationCleanup, map[string]bool{})
	if snapshot.Error != "" {
		t.Fatalf("expected failed cleanup for removed run to be hidden, got %q", snapshot.Error)
	}
	if snapshot.RunID != "" {
		t.Fatalf("expected cleanup run id to be hidden, got %q", snapshot.RunID)
	}
	if len(snapshot.Output) != 0 {
		t.Fatalf("expected cleanup output to be hidden, got %#v", snapshot.Output)
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

func TestCleanLocalArtifactsRemovesResidueButKeepsCostLedger(t *testing.T) {
	workspace := t.TempDir()
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	t.Setenv("GITHUB_WORKSPACE", testDir)

	terraformDir := filepath.Join(repoRoot, "modules", "aws")
	if err := os.MkdirAll(terraformDir, 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(testDir, "high-availability-1"), 0o755); err != nil {
		t.Fatalf("failed to create HA dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(terraformDir, "terraform.tfvars"), []byte("generated"), 0o600); err != nil {
		t.Fatalf("failed to write terraform.tfvars: %v", err)
	}

	outputDir := filepath.Join(testDir, "automation-output")
	controlPanelDir := filepath.Join(outputDir, "control-panel")
	for _, dir := range []string{filepath.Join(outputDir, "runs", "abc12345"), controlPanelDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create artifact dir: %v", err)
		}
	}
	costLedger := filepath.Join(controlPanelDir, "cost-ledger.sqlite")
	for path, content := range map[string]string{
		filepath.Join(outputDir, "runs", "abc12345", "terraform.tfstate"):   "state",
		filepath.Join(outputDir, "rancher-resolution-install-ha-1.json"):    "{}",
		filepath.Join(controlPanelDir, "run-abc12345-ha-1-downstream.yaml"): "yaml",
		filepath.Join(controlPanelDir, "lifecycle-state.json"):              "{}",
		costLedger: "sqlite",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write artifact %s: %v", path, err)
		}
	}

	panel := &localControlPanel{
		repoRoot:   repoRoot,
		testDir:    testDir,
		totalHAs:   1,
		operations: newPanelOperations(),
	}

	result, err := panel.cleanLocalArtifacts()
	if err != nil {
		t.Fatalf("cleanLocalArtifacts returned error: %v", err)
	}
	if len(result.Removed) == 0 {
		t.Fatal("expected local artifacts to be removed")
	}

	for _, path := range []string{
		filepath.Join(testDir, "high-availability-1"),
		filepath.Join(terraformDir, "terraform.tfvars"),
		filepath.Join(outputDir, "runs"),
		filepath.Join(outputDir, "rancher-resolution-install-ha-1.json"),
		filepath.Join(controlPanelDir, "run-abc12345-ha-1-downstream.yaml"),
		filepath.Join(controlPanelDir, "lifecycle-state.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", path, err)
		}
	}
	if _, err := os.Stat(costLedger); err != nil {
		t.Fatalf("expected cost ledger to be preserved: %v", err)
	}
}

func TestCleanLocalArtifactsBlocksWhenRunRecordsExist(t *testing.T) {
	workspace := t.TempDir()
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	t.Setenv("GITHUB_WORKSPACE", testDir)
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}

	panel := &localControlPanel{
		repoRoot:   repoRoot,
		testDir:    testDir,
		totalHAs:   1,
		operations: newPanelOperations(),
	}
	panel.createCurrentRunRecord("abc12345", time.Now())

	if _, err := panel.cleanLocalArtifacts(); err == nil {
		t.Fatal("expected cleanLocalArtifacts to block while a run record exists")
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
