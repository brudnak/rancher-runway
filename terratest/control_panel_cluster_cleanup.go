package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
)

const cleanupClusterIDEnv = "HA_RANCHER_CLEANUP_CLUSTER_ID"

func destroyedHAPath(moduleDir string, index int) string {
	return filepath.Join(moduleDir, fmt.Sprintf(".runway-destroyed-ha-%d", index))
}

func destroyedHA(moduleDir string, index int) bool {
	return moduleDir != "" && pathExists(destroyedHAPath(moduleDir, index))
}

func (p *localControlPanel) clusterCleanupRecord(clusterID string) (panelRunRecord, int, error) {
	runID, index, ok := parseLocalHAClusterID(clusterID)
	if !ok || runID == "" || clusterID != localClusterIDForRun(runID, index) {
		return panelRunRecord{}, 0, fmt.Errorf("select a recorded RKE2 management cluster")
	}
	record, found := p.readRunRecord(runID)
	if !found || record.RunID != runID || (record.DeploymentType != "" && record.DeploymentType != deploymentTypeHARKE2) || index > record.TotalHAs {
		return panelRunRecord{}, 0, fmt.Errorf("cluster %s does not belong to a recorded RKE2 run", clusterID)
	}
	if record.TerraformModuleDir != p.terraformModuleDirForRun(runID) || record.HAOutputRoot != p.haOutputRootForRun(runID) ||
		record.TerraformStatePath != p.terraformStatePathForRun(runID) || record.TerraformDataDir != p.terraformDataDirForRun(runID) {
		return panelRunRecord{}, 0, fmt.Errorf("cluster %s requires its recorded isolated Terraform workspace", clusterID)
	}
	if destroyedHA(record.TerraformModuleDir, index) {
		return panelRunRecord{}, 0, fmt.Errorf("cluster %s was already destroyed", clusterID)
	}
	return record, index, nil
}

func (p *localControlPanel) handleClusterCleanup(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Confirm   string `json:"confirm"`
		ClusterID string `json:"clusterId"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "expected clusterId and confirm", http.StatusBadRequest)
		return
	}
	if req.Confirm != typedConfirmationPhrase {
		http.Error(w, "typed confirmation must equal "+typedConfirmationPhrase, http.StatusBadRequest)
		return
	}
	record, index, err := p.clusterCleanupRecord(req.ClusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	name := runScopedClusterName(record.RunID, fmt.Sprintf("HA %d Local", index))
	err = p.startPanelCommand(panelCommandSpec{
		Operation: panelOperationCleanup, TestName: "TestDestroyRancherCluster", Timeout: "60m",
		RunID: record.RunID, ClusterID: req.ClusterID, DisplayName: "destroy " + name,
		StartLine:   "[control-panel] Checking " + name + " for individual destroy",
		SuccessLine: "[control-panel] Destroyed " + name,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, p.snapshotOperation(panelOperationCleanup))
}
