package test

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (p *localControlPanel) discoverLinodeDockerClustersForRun(record panelRunRecord, outputs map[string]string) []clusterView {
	versions := record.RancherVersions
	if len(versions) == 0 {
		versions = readRequestedRancherVersionsForPanel(p.totalHAs)
	}
	setupRunning := p.operationRunning(panelOperationLinodeSetup)
	runID := safeRunPathSegment(record.RunID)
	total := record.TotalHAs
	if total < 1 {
		total = len(versions)
	}
	if total < 1 {
		total = 1
	}

	clusters := make([]clusterView, 0, total)
	for i := 1; i <= total; i++ {
		rancherURL := ""
		ip := ""
		if outputs != nil {
			rancherURL = clickableURL(outputs[fmt.Sprintf("linode_%d_rancher_url", i)])
			ip = outputs[fmt.Sprintf("linode_%d_ip", i)]
		}
		if rancherURL == "" && ip == "" && !setupRunning {
			continue
		}
		cluster := clusterView{
			ID:             runScopedClusterName(runID, fmt.Sprintf("linode-docker-%d", i)),
			RunID:          runID,
			Type:           "linode",
			DeploymentType: deploymentTypeLinodeDocker,
			Role:           "docker",
			HAIndex:        i,
			Name:           runScopedClusterName(runID, fmt.Sprintf("Docker Rancher %d", i)),
			RancherURL:     rancherURL,
			LoadBalancer:   ip,
			Available:      rancherURL != "" || ip != "",
		}
		if len(versions) >= i {
			cluster.Version = versions[i-1]
		}
		if setupRunning && !cluster.Available {
			cluster.Provisioning = true
			cluster.ProvisioningMessage = "Linode setup is running. The Rancher URL will appear after Terraform apply completes."
			cluster.Error = "waiting for Linode output"
		}
		if cluster.Available && rancherURL != "" && linodeDockerRancherHTTPReachable(rancherURL) {
			cluster.Reachable = true
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func linodeDockerRancherHTTPReachable(rancherURL string) bool {
	rancherURL = strings.TrimSpace(rancherURL)
	if rancherURL == "" {
		return false
	}
	client := &http.Client{
		Timeout: 1500 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	ready, _ := rancherHTTPReadyForPanel(client, rancherURL)
	return ready
}

func rancherHTTPReadyForPanel(client *http.Client, rancherURL string) (bool, string) {
	rootProbe, rootErr := rancherHTTPProbeForPanel(client, rancherURL)
	if rootErr != nil {
		return false, fmt.Sprintf("root error: %v", rootErr)
	}
	apiProbe, apiErr := rancherHTTPProbeForPanel(client, strings.TrimRight(rancherURL, "/")+"/v3")
	if apiErr != nil {
		return false, fmt.Sprintf("root=%d api error: %v", rootProbe.Status, apiErr)
	}
	if !rancherHTTPProbeReadyForPanel(rootProbe) {
		return false, fmt.Sprintf("root=%d %s api=%d", rootProbe.Status, rancherHTTPProbeNotReadyReasonForPanel(rootProbe), apiProbe.Status)
	}
	if !rancherHTTPProbeReadyForPanel(apiProbe) {
		return false, fmt.Sprintf("root=%d api=%d %s", rootProbe.Status, apiProbe.Status, rancherHTTPProbeNotReadyReasonForPanel(apiProbe))
	}
	return true, fmt.Sprintf("root=%d api=%d", rootProbe.Status, apiProbe.Status)
}

type rancherHTTPProbeResultForPanel struct {
	Status int
	Body   string
}

func rancherHTTPProbeForPanel(client *http.Client, target string) (rancherHTTPProbeResultForPanel, error) {
	resp, err := client.Get(target)
	if err != nil {
		return rancherHTTPProbeResultForPanel{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return rancherHTTPProbeResultForPanel{Status: resp.StatusCode, Body: string(body)}, nil
}

func rancherHTTPProbeReadyForPanel(probe rancherHTTPProbeResultForPanel) bool {
	if rancherHTTPProbeAPIAggregationNotReadyForPanel(probe) {
		return false
	}
	switch probe.Status {
	case http.StatusOK,
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound:
		return true
	default:
		return false
	}
}

func rancherHTTPProbeAPIAggregationNotReadyForPanel(probe rancherHTTPProbeResultForPanel) bool {
	return strings.Contains(strings.ToLower(probe.Body), "api aggregation not ready")
}

func rancherHTTPProbeNotReadyReasonForPanel(probe rancherHTTPProbeResultForPanel) string {
	if rancherHTTPProbeAPIAggregationNotReadyForPanel(probe) {
		return "body=API Aggregation not ready"
	}
	return "not ready"
}

func (p *localControlPanel) discoverHostedTenantClustersForRun(record panelRunRecord, outputs map[string]string) []clusterView {
	versions := record.RancherVersions
	if len(versions) == 0 {
		versions = readRequestedRancherVersionsForPanel(p.totalHAs)
	}
	setupRunning := p.operationRunning(panelOperationSetup)
	runID := safeRunPathSegment(record.RunID)
	totalInstances := record.TotalHAs
	if totalInstances < 1 {
		totalInstances = configuredRancherInstanceCount()
	}
	if totalInstances < 1 {
		totalInstances = p.totalHAs
	}

	clusters := make([]clusterView, 0, totalInstances)
	for i := 1; i <= totalInstances; i++ {
		instanceDir := p.hostedTenantInstanceDirForRun(record, i)
		kubeconfigPath := filepath.Join(instanceDir, "kube_config.yaml")
		kubeconfigExists := pathExists(kubeconfigPath)
		hasRunSignal := kubeconfigExists ||
			pathExists(instanceDir) ||
			hasHostedTenantFlatOutput(outputs, i) ||
			setupRunning
		if !hasRunSignal {
			continue
		}

		role := "tenant"
		displayName := fmt.Sprintf("Tenant Rancher %d", i-1)
		if i == 1 {
			role = "host"
			displayName = "Host Rancher"
		}
		cluster := clusterView{
			ID:             hostedTenantClusterIDForRun(runID, i),
			RunID:          runID,
			Type:           "local",
			DeploymentType: deploymentTypeHostedTenantK3S,
			Role:           role,
			HAIndex:        i,
			Name:           runScopedClusterName(runID, displayName),
			DownloadName:   runScopedDownloadName(runID, fmt.Sprintf("hosted-tenant-%d.yaml", i)),
			KubeconfigPath: kubeconfigPath,
		}
		if len(versions) >= i {
			cluster.Version = versions[i-1]
		}
		if outputs != nil {
			cluster.RancherURL = clickableURL(outputs[fmt.Sprintf("hosted_%d_rancher_url", i)])
		}
		if !kubeconfigExists {
			if setupRunning {
				cluster.Provisioning = true
				cluster.ProvisioningMessage = "Setup is running. Kubeconfig will appear after Terraform and K3s bootstrap complete."
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
			continue
		}
		cluster.Reachable = true
		cluster.Pods = pods
		clusters = append(clusters, cluster)
	}
	return clusters
}

func (p *localControlPanel) operationRunning(name panelOperationName) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.operationLocked(name).Running
}

func hasHAFlatOutput(outputs map[string]string, instanceNum int) bool {
	if outputs == nil {
		return false
	}

	prefix := fmt.Sprintf("ha_%d_", instanceNum)
	for key, value := range outputs {
		if strings.HasPrefix(key, prefix) && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func hasHostedTenantFlatOutputs(outputs map[string]string) bool {
	if outputs == nil {
		return false
	}
	for key, value := range outputs {
		if strings.HasPrefix(key, "hosted_") && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func hasHostedTenantFlatOutput(outputs map[string]string, instanceNum int) bool {
	if outputs == nil {
		return false
	}
	prefix := fmt.Sprintf("hosted_%d_", instanceNum)
	for key, value := range outputs {
		if strings.HasPrefix(key, prefix) && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
