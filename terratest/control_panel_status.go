package test

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type LocalWorkspaceStatus struct {
	RepoRoot   string                             `json:"repoRoot"`
	TestDir    string                             `json:"testDir"`
	ConfigPath string                             `json:"configPath"`
	TotalHAs   int                                `json:"totalHAs"`
	Panel      LocalPanelSession                  `json:"panel"`
	Workspace  LocalWorkspaceRunState             `json:"workspace"`
	Preflight  LocalWorkspacePreflight            `json:"preflight"`
	Clusters   []LocalWorkspaceCluster            `json:"clusters"`
	Operations map[string]LocalWorkspaceOperation `json:"operations"`
}

type LocalPanelSession struct {
	Running   bool       `json:"running"`
	PID       int        `json:"pid,omitempty"`
	URL       string     `json:"url,omitempty"`
	SessionID string     `json:"sessionId,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	LogPath   string     `json:"logPath,omitempty"`
}

type LocalWorkspacePreflight struct {
	Ready   bool                  `json:"ready"`
	Summary string                `json:"summary"`
	Items   []LocalWorkspaceCheck `json:"items"`
}

type LocalWorkspaceRunState struct {
	Mode                     string                    `json:"mode"`
	SlotID                   string                    `json:"slotId"`
	SlotName                 string                    `json:"slotName"`
	CanStartFresh            bool                      `json:"canStartFresh"`
	CanStartIsolatedRun      bool                      `json:"canStartIsolatedRun"`
	IsolatedRunBlockedReason string                    `json:"isolatedRunBlockedReason,omitempty"`
	Summary                  string                    `json:"summary"`
	CurrentRun               *LocalWorkspaceRunRecord  `json:"currentRun,omitempty"`
	Runs                     []LocalWorkspaceRunRecord `json:"runs"`
	SharedPathLabels         []string                  `json:"sharedPathLabels"`
}

type LocalWorkspaceRunRecord struct {
	RunID                 string    `json:"runId"`
	SlotID                string    `json:"slotId"`
	SlotName              string    `json:"slotName"`
	Status                string    `json:"status"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
	TotalHAs              int       `json:"totalHAs"`
	AWSPrefix             string    `json:"awsPrefix,omitempty"`
	Route53FQDN           string    `json:"route53Fqdn,omitempty"`
	Owner                 string    `json:"owner,omitempty"`
	CustomHostnamePrefix  string    `json:"customHostnamePrefix,omitempty"`
	RancherVersions       []string  `json:"rancherVersions,omitempty"`
	GPUWorkerEnabled      bool      `json:"gpuWorkerEnabled,omitempty"`
	GPUWorkerInstanceType string    `json:"gpuWorkerInstanceType,omitempty"`
	TerraformBackend      string    `json:"terraformBackend"`
	TerraformModuleDir    string    `json:"terraformModuleDir,omitempty"`
	TerraformStatePath    string    `json:"terraformStatePath,omitempty"`
	TerraformDataDir      string    `json:"terraformDataDir,omitempty"`
	HAOutputRoot          string    `json:"haOutputRoot"`
	SharedPaths           []string  `json:"sharedPaths"`
}

type LocalWorkspaceCheck struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	Version     string `json:"version,omitempty"`
	Recommended string `json:"recommended,omitempty"`
	Minimum     string `json:"minimum,omitempty"`
}

type LocalWorkspaceCluster struct {
	ID                    string `json:"id"`
	Type                  string `json:"type"`
	HAIndex               int    `json:"haIndex"`
	Name                  string `json:"name"`
	Version               string `json:"version,omitempty"`
	RancherURL            string `json:"rancherUrl,omitempty"`
	LoadBalancer          string `json:"loadBalancer,omitempty"`
	GPUWorkerIP           string `json:"gpuWorkerIp,omitempty"`
	GPUWorkerPrivateIP    string `json:"gpuWorkerPrivateIp,omitempty"`
	GPUWorkerInstanceType string `json:"gpuWorkerInstanceType,omitempty"`
	GPUWorkerAMI          string `json:"gpuWorkerAmi,omitempty"`
	GPUWorkerSubnetID     string `json:"gpuWorkerSubnetId,omitempty"`
	Namespace             string `json:"namespace,omitempty"`
	ManagementClusterID   string `json:"managementClusterId,omitempty"`
	KubeconfigPath        string `json:"kubeconfigPath,omitempty"`
	Provisioning          bool   `json:"provisioning,omitempty"`
	Available             bool   `json:"available"`
	Reachable             bool   `json:"reachable"`
	Error                 string `json:"error,omitempty"`
	PodCount              int    `json:"podCount"`
}

type LocalWorkspaceOperation struct {
	Running     bool       `json:"running"`
	PID         int        `json:"pid,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Error       string     `json:"error,omitempty"`
	RunID       string     `json:"runId,omitempty"`
	Command     string     `json:"command,omitempty"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
	OutputLines int        `json:"outputLines"`
}

func InspectLocalWorkspace(repoRoot string) (LocalWorkspaceStatus, error) {
	resolvedRoot := strings.TrimSpace(repoRoot)
	var testDir string
	var err error
	if resolvedRoot == "" {
		resolvedRoot, testDir, err = resolveControlPanelPaths(".")
	} else {
		resolvedRoot, testDir, err = resolveControlPanelPaths(resolvedRoot)
	}
	if err != nil {
		return LocalWorkspaceStatus{}, err
	}

	if err := setupConfigE(resolvedRoot); err != nil {
		return LocalWorkspaceStatus{}, fmt.Errorf("failed to read config: %w", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		return LocalWorkspaceStatus{}, fmt.Errorf("failed to determine working directory: %w", err)
	}
	if err := os.Chdir(testDir); err != nil {
		return LocalWorkspaceStatus{}, fmt.Errorf("failed to enter terratest directory: %w", err)
	}
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "[ha-rancher] failed to restore working directory %s: %v\n", originalDir, restoreErr)
		}
	}()

	panel := &localControlPanel{
		totalHAs:                  viper.GetInt("total_has"),
		repoRoot:                  resolvedRoot,
		testDir:                   testDir,
		configPath:                viper.ConfigFileUsed(),
		operations:                newPanelOperations(),
		rancherTokens:             map[int]string{},
		downstreamKubeconfigCache: map[string]string{},
	}
	panel.loadPersistedOperations(false)

	preflight := panel.collectPanelPreflight()
	clusters := panel.discoverClusters()
	workspaceState := panel.workspaceState()
	activeRunIDs := map[string]bool{}
	for _, record := range workspaceState.Runs {
		activeRunIDs[safeRunPathSegment(record.RunID)] = true
	}
	return LocalWorkspaceStatus{
		RepoRoot:   resolvedRoot,
		TestDir:    testDir,
		ConfigPath: panel.configPath,
		TotalHAs:   panel.totalHAs,
		Panel:      inspectLocalPanelSession(resolvedRoot),
		Workspace:  localWorkspaceRunState(workspaceState),
		Preflight:  localWorkspacePreflight(preflight),
		Clusters:   localWorkspaceClusters(clusters),
		Operations: map[string]LocalWorkspaceOperation{
			string(panelOperationSetup):     localWorkspaceOperation(panel.snapshotOperationForRuns(panelOperationSetup, activeRunIDs)),
			string(panelOperationReadiness): localWorkspaceOperation(panel.snapshotOperationForRuns(panelOperationReadiness, activeRunIDs)),
			string(panelOperationCleanup):   localWorkspaceOperation(panel.snapshotOperationForRuns(panelOperationCleanup, activeRunIDs)),
		},
	}, nil
}

func InspectGPUInfrastructure(repoRoot string) (GPUInfrastructureSummary, error) {
	resolvedRoot := strings.TrimSpace(repoRoot)
	var testDir string
	var err error
	if resolvedRoot == "" {
		resolvedRoot, testDir, err = resolveControlPanelPaths(".")
	} else {
		resolvedRoot, testDir, err = resolveControlPanelPaths(resolvedRoot)
	}
	if err != nil {
		return GPUInfrastructureSummary{}, err
	}

	if err := setupConfigE(resolvedRoot); err != nil {
		return GPUInfrastructureSummary{}, fmt.Errorf("failed to read config: %w", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		return GPUInfrastructureSummary{}, fmt.Errorf("failed to determine working directory: %w", err)
	}
	if err := os.Chdir(testDir); err != nil {
		return GPUInfrastructureSummary{}, fmt.Errorf("failed to enter terratest directory: %w", err)
	}
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "[ha-rancher] failed to restore working directory %s: %v\n", originalDir, restoreErr)
		}
	}()

	panel := &localControlPanel{
		totalHAs:   viper.GetInt("total_has"),
		repoRoot:   resolvedRoot,
		testDir:    testDir,
		configPath: viper.ConfigFileUsed(),
	}
	return panel.gpuInfrastructure(), nil
}

func localWorkspaceRunState(state panelWorkspaceState) LocalWorkspaceRunState {
	out := LocalWorkspaceRunState{
		Mode:                     state.Mode,
		SlotID:                   state.SlotID,
		SlotName:                 state.SlotName,
		CanStartFresh:            state.CanStartFresh,
		CanStartIsolatedRun:      state.CanStartIsolatedRun,
		IsolatedRunBlockedReason: state.IsolatedRunBlockedReason,
		Summary:                  state.Summary,
		SharedPathLabels:         append([]string(nil), state.SharedPathLabels...),
	}
	for _, record := range state.Runs {
		out.Runs = append(out.Runs, localWorkspaceRunRecord(record))
	}
	if state.CurrentRun != nil {
		current := localWorkspaceRunRecord(*state.CurrentRun)
		out.CurrentRun = &current
	}
	return out
}

func localWorkspaceRunRecord(record panelRunRecord) LocalWorkspaceRunRecord {
	return LocalWorkspaceRunRecord{
		RunID:                 record.RunID,
		SlotID:                record.SlotID,
		SlotName:              record.SlotName,
		Status:                record.Status,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
		TotalHAs:              record.TotalHAs,
		AWSPrefix:             record.AWSPrefix,
		Route53FQDN:           record.Route53FQDN,
		Owner:                 record.Owner,
		CustomHostnamePrefix:  record.CustomHostnamePrefix,
		RancherVersions:       append([]string(nil), record.RancherVersions...),
		GPUWorkerEnabled:      record.GPUWorkerEnabled,
		GPUWorkerInstanceType: record.GPUWorkerInstanceType,
		TerraformBackend:      record.TerraformBackend,
		TerraformModuleDir:    record.TerraformModuleDir,
		TerraformStatePath:    record.TerraformStatePath,
		TerraformDataDir:      record.TerraformDataDir,
		HAOutputRoot:          record.HAOutputRoot,
		SharedPaths:           append([]string(nil), record.SharedPaths...),
	}
}

func inspectLocalPanelSession(repoRoot string) LocalPanelSession {
	logPath := panelLaunchLogPath()
	session, ok, err := readPanelSession()
	if err != nil || !ok || !samePath(session.RepoRoot, repoRoot) {
		return LocalPanelSession{LogPath: logPath}
	}
	running := processAlive(session.PID) && panelSessionHealthy(session.URL)
	startedAt := session.StartedAt
	return LocalPanelSession{
		Running:   running,
		PID:       session.PID,
		URL:       session.URL,
		SessionID: session.SessionID,
		StartedAt: &startedAt,
		LogPath:   logPath,
	}
}

func localWorkspacePreflight(preflight systemReadinessState) LocalWorkspacePreflight {
	items := make([]LocalWorkspaceCheck, 0, len(preflight.Items))
	for _, item := range preflight.Items {
		items = append(items, LocalWorkspaceCheck{
			Name:        item.Name,
			Status:      item.Status,
			Detail:      item.Detail,
			Version:     item.Version,
			Recommended: item.Recommended,
			Minimum:     item.Minimum,
		})
	}
	return LocalWorkspacePreflight{
		Ready:   preflight.Ready,
		Summary: preflight.Summary,
		Items:   items,
	}
}

func localWorkspaceClusters(clusters []clusterView) []LocalWorkspaceCluster {
	out := make([]LocalWorkspaceCluster, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, LocalWorkspaceCluster{
			ID:                    cluster.ID,
			Type:                  cluster.Type,
			HAIndex:               cluster.HAIndex,
			Name:                  cluster.Name,
			Version:               cluster.Version,
			RancherURL:            cluster.RancherURL,
			LoadBalancer:          cluster.LoadBalancer,
			GPUWorkerIP:           cluster.GPUWorkerIP,
			GPUWorkerPrivateIP:    cluster.GPUWorkerPrivateIP,
			GPUWorkerInstanceType: cluster.GPUWorkerInstanceType,
			GPUWorkerAMI:          cluster.GPUWorkerAMI,
			GPUWorkerSubnetID:     cluster.GPUWorkerSubnetID,
			Namespace:             cluster.Namespace,
			ManagementClusterID:   cluster.ManagementClusterID,
			KubeconfigPath:        cluster.KubeconfigPath,
			Provisioning:          cluster.Provisioning,
			Available:             cluster.Available,
			Reachable:             cluster.Reachable,
			Error:                 cluster.Error,
			PodCount:              len(cluster.Pods),
		})
	}
	return out
}

func localWorkspaceOperation(operation panelOperationSnapshot) LocalWorkspaceOperation {
	return LocalWorkspaceOperation{
		Running:     operation.Running,
		PID:         operation.PID,
		StartedAt:   operation.StartedAt,
		FinishedAt:  operation.FinishedAt,
		Error:       operation.Error,
		RunID:       operation.RunID,
		Command:     operation.Command,
		UpdatedAt:   operation.UpdatedAt,
		OutputLines: len(operation.Output),
	}
}
