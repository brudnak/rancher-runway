package test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

func (p *localControlPanel) logRequest(r *http.Request) (clusterView, string, string, string, error) {
	clusterID := strings.TrimSpace(r.URL.Query().Get("cluster"))
	pod := strings.TrimSpace(r.URL.Query().Get("pod"))
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	container := strings.TrimSpace(r.URL.Query().Get("container"))
	if clusterID == "" || pod == "" {
		return clusterView{}, "", "", "", fmt.Errorf("cluster and pod are required")
	}
	if namespace == "" {
		namespace = "cattle-system"
	}

	for _, cluster := range p.discoverClusters() {
		if cluster.ID == clusterID {
			if !cluster.Available {
				return clusterView{}, "", "", "", fmt.Errorf("cluster is not available")
			}
			if !cluster.Reachable {
				return clusterView{}, "", "", "", fmt.Errorf("cluster is not reachable")
			}
			return cluster, pod, namespace, container, nil
		}
	}

	return clusterView{}, "", "", "", fmt.Errorf("cluster %s not found", clusterID)
}

func (p *localControlPanel) clusterByID(clusterID string) (clusterView, error) {
	for _, cluster := range p.discoverClusters() {
		if cluster.ID == clusterID {
			return cluster, nil
		}
	}
	return clusterView{}, fmt.Errorf("cluster %s not found", clusterID)
}

// recordedLocalHAClusterByID resolves the one cluster kind whose artifact path
// is completely described by a run record. It intentionally does not replace
// clusterByID: downstream, hosted-tenant, and Linode clusters still require the
// full discovery path used by their callers.
func (p *localControlPanel) recordedLocalHAClusterByID(clusterID string) (clusterView, bool) {
	runID, haIndex, ok := parseLocalHAClusterID(clusterID)
	if !ok {
		return clusterView{}, false
	}

	var record panelRunRecord
	if runID != "" {
		var found bool
		record, found = p.readRunRecord(runID)
		if !found || !sameRunID(record.RunID, runID) {
			return clusterView{}, false
		}
	} else {
		// Unscoped IDs are emitted only for the pre-run legacy artifact layout.
		// Do not let one alias a scoped run's recorded HA output directory.
		if len(p.listRunRecords()) != 0 {
			return clusterView{}, false
		}
		record = panelRunRecord{
			RunID:        "",
			TotalHAs:     p.totalHAs,
			HAOutputRoot: p.currentHAOutputRoot(),
		}
	}

	if deployment := strings.TrimSpace(record.DeploymentType); deployment != "" && deployment != deploymentTypeHARKE2 {
		return clusterView{}, false
	}
	totalHAs := record.TotalHAs
	if totalHAs < 1 {
		totalHAs = p.totalHAs
	}
	if haIndex < 1 || haIndex > totalHAs {
		return clusterView{}, false
	}

	haDir := p.haInstanceDirForRun(record, haIndex)
	if !pathExists(haDir) && !p.operationRunning(panelOperationSetup) && !p.operationRunning(panelOperationReadiness) {
		// Terraform output or downstream records can also make a cluster visible.
		// Let full discovery handle those uncommon cases to preserve its behavior.
		return clusterView{}, false
	}

	canonicalRunID := ""
	if runID != "" {
		canonicalRunID = safeRunPathSegment(record.RunID)
	}
	kubeconfigPath := filepath.Join(haDir, "kube_config.yaml")
	return clusterView{
		ID:             localClusterIDForRun(canonicalRunID, haIndex),
		RunID:          canonicalRunID,
		Type:           "local",
		DeploymentType: deploymentTypeHARKE2,
		HAIndex:        haIndex,
		Name:           runScopedClusterName(canonicalRunID, fmt.Sprintf("HA %d Local", haIndex)),
		KubeconfigPath: kubeconfigPath,
		DownloadName:   runScopedDownloadName(canonicalRunID, fmt.Sprintf("local-ha-%d.yaml", haIndex)),
		Available:      pathExists(kubeconfigPath),
	}, true
}

func parseLocalHAClusterID(clusterID string) (string, int, bool) {
	clusterID = strings.TrimSpace(clusterID)
	if !strings.HasSuffix(clusterID, "-local") {
		return "", 0, false
	}

	prefix := strings.TrimSuffix(clusterID, "-local")
	runID := ""
	indexText := ""
	if strings.HasPrefix(prefix, "ha-") {
		indexText = strings.TrimPrefix(prefix, "ha-")
	} else if strings.HasPrefix(prefix, "run-") {
		runAndIndex := strings.TrimPrefix(prefix, "run-")
		separator := strings.LastIndex(runAndIndex, "-ha-")
		if separator <= 0 {
			return "", 0, false
		}
		runID = runAndIndex[:separator]
		indexText = runAndIndex[separator+len("-ha-"):]
		if runID == "unknown" || safeRunPathSegment(runID) != runID {
			return "", 0, false
		}
	} else {
		return "", 0, false
	}

	haIndex, err := strconv.Atoi(indexText)
	if err != nil || haIndex < 1 || localClusterIDForRun(runID, haIndex) != clusterID {
		return "", 0, false
	}
	return runID, haIndex, true
}
