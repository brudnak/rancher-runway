package test

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (p *localControlPanel) discoverClusters() []clusterView {
	runRecords := p.listRunRecords()
	if len(runRecords) == 0 {
		if record, ok := p.readCurrentRunRecord(); ok {
			runRecords = []panelRunRecord{record}
		}
	}
	if len(runRecords) == 0 {
		return p.discoverClustersForRun(panelRunRecord{
			RunID:           "",
			TotalHAs:        p.totalHAs,
			RancherVersions: readRequestedRancherVersionsForPanel(p.totalHAs),
			HAOutputRoot:    p.currentHAOutputRoot(),
		})
	}

	clusters := make([]clusterView, 0)
	for _, record := range runRecords {
		clusters = append(clusters, p.discoverClustersForRun(record)...)
	}
	return clusters
}

func (p *localControlPanel) gpuInfrastructure() GPUInfrastructureSummary {
	runRecords := p.listRunRecords()
	if len(runRecords) == 0 {
		if record, ok := p.readCurrentRunRecord(); ok {
			runRecords = []panelRunRecord{record}
		}
	}
	if len(runRecords) == 0 {
		runRecords = []panelRunRecord{{
			RunID:    "",
			TotalHAs: p.totalHAs,
		}}
	}

	var details []GPUInfrastructureDetail
	for _, record := range runRecords {
		details = append(details, p.gpuInfrastructureForRun(record)...)
	}
	return GPUInfrastructureSummary{
		Active:  len(details) > 0,
		Count:   len(details),
		Details: details,
	}
}

func (p *localControlPanel) gpuInfrastructureForRun(record panelRunRecord) []GPUInfrastructureDetail {
	outputs, err := readTerraformFlatOutputsWithModule(p.repoRoot, record.TerraformStatePath, record.TerraformDataDir, record.TerraformModuleDir)
	if err != nil {
		return nil
	}

	totalHAs := record.TotalHAs
	if totalHAs < 1 {
		totalHAs = p.totalHAs
	}
	runID := safeRunPathSegment(record.RunID)
	details := make([]GPUInfrastructureDetail, 0, totalHAs)
	for i := 1; i <= totalHAs; i++ {
		prefix := fmt.Sprintf("ha_%d", i)
		ip := strings.TrimSpace(outputs[fmt.Sprintf("%s_gpu_worker_ip", prefix)])
		privateIP := strings.TrimSpace(outputs[fmt.Sprintf("%s_gpu_worker_private_ip", prefix)])
		if ip == "" && privateIP == "" {
			continue
		}
		instanceType := strings.TrimSpace(outputs[fmt.Sprintf("%s_gpu_worker_instance_type", prefix)])
		if instanceType == "" {
			instanceType = strings.TrimSpace(record.GPUWorkerInstanceType)
		}
		details = append(details, GPUInfrastructureDetail{
			RunID:        runID,
			HAIndex:      i,
			IP:           ip,
			PrivateIP:    privateIP,
			InstanceType: instanceType,
		})
	}
	return details
}

func (p *localControlPanel) discoverClustersForRun(record panelRunRecord) []clusterView {
	outputs, _ := readTerraformFlatOutputsWithModule(p.repoRoot, record.TerraformStatePath, record.TerraformDataDir, record.TerraformModuleDir)
	switch recordDeploymentType(record, outputs) {
	case deploymentTypeHostedTenantK3S:
		return p.discoverHostedTenantClustersForRun(record, outputs)
	case deploymentTypeLinodeDocker:
		return p.discoverLinodeDockerClustersForRun(record, outputs)
	}
	versions := record.RancherVersions
	if len(versions) == 0 {
		versions = readRequestedRancherVersionsForPanel(p.totalHAs)
	}
	downstreamRecords, _ := readDownstreamOutputRecordsForRun(record.RunID)
	recordsByHA := downstreamRecordsByHA(downstreamRecords)
	setupRunning := p.operationRunning(panelOperationSetup)
	readinessRunning := p.operationRunning(panelOperationReadiness)
	runID := safeRunPathSegment(record.RunID)
	totalHAs := record.TotalHAs
	if totalHAs < 1 {
		totalHAs = p.totalHAs
	}

	clusters := make([]clusterView, 0, totalHAs)
	for i := 1; i <= totalHAs; i++ {
		haDir := p.haInstanceDirForRun(record, i)
		kubeconfigPath := filepath.Join(haDir, "kube_config.yaml")
		kubeconfigExists := pathExists(kubeconfigPath)
		hasRunSignal := kubeconfigExists ||
			pathExists(haDir) ||
			hasHAFlatOutput(outputs, i) ||
			len(recordsByHA[i]) > 0 ||
			setupRunning ||
			readinessRunning
		if !hasRunSignal {
			continue
		}

		cluster := clusterView{
			ID:           localClusterIDForRun(runID, i),
			RunID:        runID,
			Type:         "local",
			HAIndex:      i,
			Name:         runScopedClusterName(runID, fmt.Sprintf("HA %d Local", i)),
			DownloadName: runScopedDownloadName(runID, fmt.Sprintf("local-ha-%d.yaml", i)),
		}
		if len(versions) >= i {
			cluster.Version = versions[i-1]
		}
		cluster.KubeconfigPath = kubeconfigPath
		if outputs != nil {
			cluster.RancherURL = clickableURL(outputs[fmt.Sprintf("ha_%d_rancher_url", i)])
			cluster.LoadBalancer = outputs[fmt.Sprintf("ha_%d_aws_lb", i)]
			cluster.GPUWorkerIP = outputs[fmt.Sprintf("ha_%d_gpu_worker_ip", i)]
			cluster.GPUWorkerPrivateIP = outputs[fmt.Sprintf("ha_%d_gpu_worker_private_ip", i)]
			cluster.GPUWorkerInstanceType = outputs[fmt.Sprintf("ha_%d_gpu_worker_instance_type", i)]
			cluster.GPUWorkerAMI = outputs[fmt.Sprintf("ha_%d_gpu_worker_ami", i)]
			cluster.GPUWorkerSubnetID = outputs[fmt.Sprintf("ha_%d_gpu_worker_subnet_id", i)]
		}

		if !kubeconfigExists {
			if setupRunning {
				cluster.Provisioning = true
				cluster.ProvisioningMessage = "Setup is running. Kubeconfig will appear after Terraform and RKE2 bootstrap complete."
			}
			cluster.Error = "kubeconfig not found"
			clusters = append(clusters, cluster)
			continue
		}

		cluster.Available = true
		pods, err := fetchLocalRancherPods(cluster.KubeconfigPath)
		if err != nil {
			cluster.Error = err.Error()
			clusters = append(clusters, cluster)
			clusters = append(clusters, p.discoverDownstreamClusters(cluster, recordsByHA[i])...)
			continue
		}

		cluster.Reachable = true
		cluster.Pods = pods
		clusters = append(clusters, cluster)
		clusters = append(clusters, p.discoverDownstreamClusters(cluster, recordsByHA[i])...)
	}

	return clusters
}

func recordDeploymentType(record panelRunRecord, outputs map[string]string) string {
	if record.DeploymentType != "" {
		return record.DeploymentType
	}
	if hasHostedTenantFlatOutputs(outputs) {
		return deploymentTypeHostedTenantK3S
	}
	return deploymentType()
}
