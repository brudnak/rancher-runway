package test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (p *localControlPanel) discoverDownstreamClusters(local clusterView, records []downstreamOutputRecord) []clusterView {
	if !local.Available {
		return nil
	}

	provisioningClusters, err := discoverProvisioningDownstreamClusters(local.KubeconfigPath)
	if err != nil {
		return downstreamClustersFromRecords(local, records, err)
	}

	recordByName := downstreamRecordsByClusterKey(records)
	activeIDs := map[string]bool{}
	clusters := make([]clusterView, 0, len(provisioningClusters))
	for _, item := range provisioningClusters {
		key := provisioningClusterRecordKey(item.Namespace, item.Name)
		record := recordByName[key]
		clusterID := downstreamClusterIDForRun(local.RunID, local.HAIndex, item.Namespace, item.Name)
		activeIDs[clusterID] = true
		cluster := clusterView{
			ID:                  clusterID,
			RunID:               local.RunID,
			Type:                "downstream",
			HAIndex:             local.HAIndex,
			Name:                item.Name,
			Version:             record.KubernetesVersion,
			RancherURL:          local.RancherURL,
			Namespace:           item.Namespace,
			ManagementClusterID: item.ManagementClusterID,
			DownloadName:        safeKubeconfigDownloadName(item.Name),
			Available:           true,
		}
		if record.KubeconfigPath != "" {
			cluster.KubeconfigPath = record.KubeconfigPath
		}
		if cluster.ManagementClusterID == "" {
			cluster.Provisioning = true
			cluster.ProvisioningMessage = "Waiting for Rancher to assign a downstream cluster id"
			clusters = append(clusters, cluster)
			continue
		}

		kubeconfigPath, err := p.ensureDownstreamKubeconfig(local.HAIndex, local.RancherURL, cluster.ID, item.ManagementClusterID, record.KubeconfigPath)
		if err != nil {
			cluster.Provisioning = true
			cluster.ProvisioningMessage = "Waiting for downstream kubeconfig"
			clusters = append(clusters, cluster)
			continue
		}
		cluster.KubeconfigPath = kubeconfigPath

		pods, err := fetchAllPods(kubeconfigPath)
		if err != nil {
			cluster.Provisioning = true
			cluster.ProvisioningMessage = "Waiting for downstream Kubernetes API"
			clusters = append(clusters, cluster)
			continue
		}
		cluster.Reachable = true
		cluster.Pods = pods
		clusters = append(clusters, cluster)
	}
	p.pruneStaleDownstreamKubeconfigs(local.RunID, local.HAIndex, activeIDs)

	return clusters
}

func downstreamClustersFromRecords(local clusterView, records []downstreamOutputRecord, discoverErr error) []clusterView {
	clusters := make([]clusterView, 0, len(records))
	for _, record := range records {
		cluster := clusterView{
			ID:                  downstreamClusterIDForRun(local.RunID, local.HAIndex, record.Namespace, record.ClusterName),
			RunID:               local.RunID,
			Type:                "downstream",
			HAIndex:             local.HAIndex,
			Name:                record.ClusterName,
			Version:             record.KubernetesVersion,
			RancherURL:          local.RancherURL,
			Namespace:           record.Namespace,
			ManagementClusterID: record.ManagementClusterID,
			KubeconfigPath:      record.KubeconfigPath,
			DownloadName:        safeKubeconfigDownloadName(record.ClusterName),
			Available:           record.KubeconfigPath != "" || record.ManagementClusterID != "",
			Provisioning:        true,
			ProvisioningMessage: fmt.Sprintf("Waiting for downstream discovery (%v)", discoverErr),
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func discoverProvisioningDownstreamClusters(kubeconfigPath string) ([]discoveredDownstreamCluster, error) {
	output, err := runKubectl(kubeconfigPath, "get", "clusters.provisioning.cattle.io", "-A", "-o", "json")
	if err != nil {
		return nil, err
	}

	var list provisioningClusterList
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("failed to parse provisioning clusters: %w", err)
	}

	clusters := make([]discoveredDownstreamCluster, 0, len(list.Items))
	for _, item := range list.Items {
		name := strings.TrimSpace(item.Metadata.Name)
		namespace := strings.TrimSpace(item.Metadata.Namespace)
		if name == "" || namespace == "" {
			continue
		}
		if name == "local" || namespace == "local" {
			continue
		}
		clusters = append(clusters, discoveredDownstreamCluster{
			Name:                name,
			Namespace:           namespace,
			ManagementClusterID: strings.TrimSpace(item.Status.ClusterName),
		})
	}

	managementClusters, err := discoverManagementDownstreamClusters(kubeconfigPath)
	if err == nil {
		seenManagementIDs := map[string]bool{}
		for _, cluster := range clusters {
			if cluster.ManagementClusterID != "" {
				seenManagementIDs[cluster.ManagementClusterID] = true
			}
		}
		for _, cluster := range managementClusters {
			if seenManagementIDs[cluster.ManagementClusterID] {
				continue
			}
			clusters = append(clusters, cluster)
		}
	}

	sort.Slice(clusters, func(i, j int) bool {
		left := provisioningClusterRecordKey(clusters[i].Namespace, clusters[i].Name)
		right := provisioningClusterRecordKey(clusters[j].Namespace, clusters[j].Name)
		return left < right
	})
	return clusters, nil
}

func discoverManagementDownstreamClusters(kubeconfigPath string) ([]discoveredDownstreamCluster, error) {
	output, err := runKubectl(kubeconfigPath, "get", "clusters.management.cattle.io", "-o", "json")
	if err != nil {
		return nil, err
	}

	var list managementClusterList
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("failed to parse management clusters: %w", err)
	}

	clusters := make([]discoveredDownstreamCluster, 0, len(list.Items))
	for _, item := range list.Items {
		clusterID := strings.TrimSpace(item.Metadata.Name)
		if clusterID == "" || clusterID == "local" {
			continue
		}
		name := strings.TrimSpace(item.Spec.DisplayName)
		if name == "" {
			name = clusterID
		}
		clusters = append(clusters, discoveredDownstreamCluster{
			Name:                name,
			ManagementClusterID: clusterID,
		})
	}
	return clusters, nil
}

func downstreamRecordsByHA(records []downstreamOutputRecord) map[int][]downstreamOutputRecord {
	byHA := map[int][]downstreamOutputRecord{}
	for _, record := range records {
		byHA[record.HAIndex] = append(byHA[record.HAIndex], record)
	}
	return byHA
}

func downstreamRecordsByClusterKey(records []downstreamOutputRecord) map[string]downstreamOutputRecord {
	byName := map[string]downstreamOutputRecord{}
	for _, record := range records {
		key := provisioningClusterRecordKey(record.Namespace, record.ClusterName)
		byName[key] = record
	}
	return byName
}

func provisioningClusterRecordKey(namespace, name string) string {
	return strings.TrimSpace(namespace) + "/" + strings.TrimSpace(name)
}

func localClusterID(instanceNum int) string {
	return localClusterIDForRun("", instanceNum)
}

func localClusterIDForRun(runID string, instanceNum int) string {
	runID = safeRunPathSegment(runID)
	if runID != "" && runID != "unknown" {
		return fmt.Sprintf("run-%s-ha-%d-local", runID, instanceNum)
	}
	return fmt.Sprintf("ha-%d-local", instanceNum)
}

func hostedTenantClusterIDForRun(runID string, instanceNum int) string {
	role := "tenant"
	if instanceNum == 1 {
		role = "host"
	}
	runID = safeRunPathSegment(runID)
	if runID != "" && runID != "unknown" {
		return fmt.Sprintf("run-%s-hosted-%s-%d", runID, role, instanceNum)
	}
	return fmt.Sprintf("hosted-%s-%d", role, instanceNum)
}

func downstreamClusterID(instanceNum int, namespace, name string) string {
	return downstreamClusterIDForRun("", instanceNum, namespace, name)
}

func downstreamClusterIDForRun(runID string, instanceNum int, namespace, name string) string {
	namespacePart := sanitizeIDPart(namespace)
	namePart := sanitizeIDPart(name)
	prefix := fmt.Sprintf("ha-%d", instanceNum)
	runID = safeRunPathSegment(runID)
	if runID != "" && runID != "unknown" {
		prefix = fmt.Sprintf("run-%s-ha-%d", runID, instanceNum)
	}
	if namespacePart == "" {
		return fmt.Sprintf("%s-downstream-%s", prefix, namePart)
	}
	return fmt.Sprintf("%s-downstream-%s-%s", prefix, namespacePart, namePart)
}

func sanitizeIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func safeKubeconfigDownloadName(clusterName string) string {
	name := sanitizeIDPart(clusterName)
	if name == "" {
		name = "downstream"
	}
	return name + ".yaml"
}

func (p *localControlPanel) pruneStaleDownstreamKubeconfigs(runID string, haIndex int, activeIDs map[string]bool) {
	prefix := fmt.Sprintf("ha-%d-downstream-", haIndex)
	runID = safeRunPathSegment(runID)
	if runID != "" && runID != "unknown" {
		prefix = fmt.Sprintf("run-%s-ha-%d-downstream-", runID, haIndex)
	}

	p.mu.Lock()
	for clusterID, path := range p.downstreamKubeconfigCache {
		if !strings.HasPrefix(clusterID, prefix) || activeIDs[clusterID] {
			continue
		}
		delete(p.downstreamKubeconfigCache, clusterID)
		RemoveFile(path)
	}
	p.mu.Unlock()

	cacheDir := filepath.Join(automationOutputDir(), "control-panel")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || filepath.Ext(name) != ".yaml" {
			continue
		}
		clusterID := strings.TrimSuffix(name, ".yaml")
		if activeIDs[clusterID] {
			continue
		}
		RemoveFile(filepath.Join(cacheDir, name))
	}
}
