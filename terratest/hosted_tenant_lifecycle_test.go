package test

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/spf13/viper"
)

type hostedTenantK3SConfig struct {
	Index       int
	DBPassword  string
	DBEndpoint  string
	RancherURL  string
	Node1IP     string
	Node2IP     string
	DisplayName string
}

func runHostedTenantSetup(t *testing.T) {
	totalInstances := hostedTenantRancherInstanceCount()
	if totalInstances < 2 {
		t.Fatal("hosted-tenant-k3s requires at least 2 Rancher instances: one host and at least one tenant")
	}

	resolvedPlans, err := prepareHostedTenantRancherConfiguration(totalInstances)
	if err != nil {
		t.Fatalf("Hosted tenant Rancher setup canceled or failed: %v", err)
	}

	helmCommands := viper.GetStringSlice("rancher.helm_commands")
	if len(helmCommands) != totalInstances {
		t.Fatalf("rancher.helm_commands has %d entries but total_rancher_instances is %d", len(helmCommands), totalInstances)
	}
	if err := validateHostedTenantConfiguration(totalInstances, helmCommands, resolvedPlans); err != nil {
		t.Fatalf("Hosted tenant config preflight failed: %v", err)
	}
	if err := validateLocalToolingPreflight(helmCommands); err != nil {
		t.Fatalf("Local tooling preflight failed before provisioning infrastructure: %v", err)
	}
	if err := validateSecretEnvironment(); err != nil {
		t.Fatalf("Secret environment preflight failed before provisioning infrastructure: %v", err)
	}

	for i, plan := range resolvedPlans {
		if err := writeRancherResolutionArtifact("hosted-tenant-install", i+1, plan); err != nil {
			t.Fatalf("Failed to write hosted tenant Rancher resolution artifact: %v", err)
		}
	}

	terraformOptions := getTerraformOptions(t, totalInstances)
	terraform.InitAndApply(t, terraformOptions)

	outputs := getTerraformOutputs(t, terraformOptions)
	hostConfig, tenantConfigs, err := hostedTenantConfigsFromOutputs(totalInstances, outputs)
	if err != nil {
		t.Fatalf("Failed to read hosted tenant Terraform outputs: %v", err)
	}

	log.Printf("[hosted-tenant] Installing host K3s %s", resolvedPlans[0].RecommendedK3SVersion)
	if err := installHostedTenantK3SCluster(hostConfig, resolvedPlans[0]); err != nil {
		t.Fatalf("Failed to install host K3s: %v", err)
	}

	hostDir := hostedTenantInstanceDir(1)
	hostHelmCommand := rancherHelmCommandWithHostname(helmCommands[0], hostConfig.RancherURL)
	if err := createHostedTenantRancherInstallScript(hostHelmCommand, hostDir); err != nil {
		t.Fatalf("Failed to create host Rancher install script: %v", err)
	}
	if err := saveHostedTenantKubeconfig(hostConfig.Node1IP, hostDir); err != nil {
		t.Fatalf("Failed to save host kubeconfig: %v", err)
	}
	if err := executeHostedTenantScript(hostDir, "install.sh"); err != nil {
		t.Fatalf("Failed to install host Rancher: %v", err)
	}
	if err := waitForHostedTenantRancherStable(hostConfig.RancherURL, 10*time.Minute); err != nil {
		t.Fatalf("Host Rancher failed to become stable: %v", err)
	}

	adminToken, err := createRancherAdminToken(hostConfig.RancherURL, viper.GetString("rancher.bootstrap_password"))
	if err != nil {
		t.Fatalf("Failed to create host Rancher admin token: %v", err)
	}
	if err := configureRancherServerURL(hostConfig.RancherURL, adminToken); err != nil {
		t.Fatalf("Failed to configure host Rancher server-url: %v", err)
	}

	log.Printf("[hosted-tenant] Host Rancher https://%s is ready for tenant imports", hostConfig.RancherURL)

	for i, tenantConfig := range tenantConfigs {
		tenantNumber := i + 1
		planIndex := i + 1
		log.Printf("[hosted-tenant] Installing tenant %d K3s %s", tenantNumber, resolvedPlans[planIndex].RecommendedK3SVersion)
		if err := installHostedTenantK3SCluster(tenantConfig, resolvedPlans[planIndex]); err != nil {
			t.Fatalf("Failed to install tenant %d K3s: %v", tenantNumber, err)
		}
		if err := importHostedTenantCluster(hostConfig.RancherURL, adminToken, tenantNumber, tenantConfig); err != nil {
			t.Fatalf("Failed to import tenant %d cluster: %v", tenantNumber, err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(tenantConfigs))
	for i, tenantConfig := range tenantConfigs {
		wg.Add(1)
		tenantNumber := i + 1
		helmCommand := helmCommands[i+1]
		go func() {
			defer wg.Done()
			if err := installHostedTenantRancher(tenantNumber, tenantConfig, helmCommand); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("Hosted tenant Rancher install failed: %v", err)
		}
	}

	log.Printf("[hosted-tenant] Host Rancher https://%s", hostConfig.RancherURL)
	for i, tenantConfig := range tenantConfigs {
		log.Printf("[hosted-tenant] Tenant Rancher %d https://%s", i+1, tenantConfig.RancherURL)
	}
}

func validateHostedTenantConfiguration(totalInstances int, helmCommands []string, plans []*RancherResolvedPlan) error {
	if totalInstances < hostedTenantMinInstances {
		return fmt.Errorf("total_rancher_instances must be at least 2")
	}
	if totalInstances > hostedTenantMaxInstances {
		return fmt.Errorf("total_rancher_instances cannot exceed 4")
	}
	if len(helmCommands) != totalInstances {
		return fmt.Errorf("rancher.helm_commands must contain %d command(s)", totalInstances)
	}
	if len(plans) != totalInstances {
		return fmt.Errorf("resolved plan count %d does not match total_rancher_instances %d", len(plans), totalInstances)
	}
	if password := hostedTenantRDSPassword(); password == "" {
		return fmt.Errorf("tf_vars.aws_rds_password or AWS_RDS_PASSWORD must be set for hosted-tenant-k3s")
	} else if err := validateHostedTenantRDSPassword(password); err != nil {
		return err
	}
	for i, plan := range plans {
		if plan == nil || strings.TrimSpace(plan.RecommendedK3SVersion) == "" {
			return fmt.Errorf("missing resolved K3s version for instance %d", i+1)
		}
		if strings.TrimSpace(plan.K3SInstallerSHA256) == "" {
			return fmt.Errorf("missing K3s installer checksum for instance %d", i+1)
		}
		if viper.GetBool("k3s.preload_images") && strings.TrimSpace(plan.K3SAirgapImageSHA256) == "" {
			return fmt.Errorf("missing K3s airgap image checksum for instance %d", i+1)
		}
	}
	return nil
}

func hostedTenantConfigsFromOutputs(totalInstances int, outputs map[string]string) (hostedTenantK3SConfig, []hostedTenantK3SConfig, error) {
	configs := make([]hostedTenantK3SConfig, 0, totalInstances)
	for i := 1; i <= totalInstances; i++ {
		cfg := hostedTenantConfigFromOutputs(i, outputs)
		if cfg.Node1IP == "" || cfg.Node2IP == "" || cfg.DBEndpoint == "" || cfg.DBPassword == "" || cfg.RancherURL == "" {
			return hostedTenantK3SConfig{}, nil, fmt.Errorf("missing hosted_%d Terraform outputs", i)
		}
		configs = append(configs, cfg)
	}
	return configs[0], configs[1:], nil
}

func hostedTenantConfigFromOutputs(index int, outputs map[string]string) hostedTenantK3SConfig {
	prefix := fmt.Sprintf("hosted_%d", index)
	role := "tenant"
	if index == 1 {
		role = "host"
	}
	return hostedTenantK3SConfig{
		Index:       index,
		DBPassword:  outputs[prefix+"_mysql_password"],
		DBEndpoint:  outputs[prefix+"_mysql_endpoint"],
		RancherURL:  strings.TrimSuffix(outputs[prefix+"_rancher_url"], "."),
		Node1IP:     outputs[prefix+"_server1_ip"],
		Node2IP:     outputs[prefix+"_server2_ip"],
		DisplayName: fmt.Sprintf("%s-%d", role, index),
	}
}

func hostedTenantInstanceDir(index int) string {
	name := "host-rancher"
	if index > 1 {
		name = fmt.Sprintf("tenant-%d-rancher", index-1)
	}
	if root := strings.TrimSpace(os.Getenv(haOutputRootEnv)); root != "" {
		return filepath.Join(root, name)
	}
	return name
}

func cleanupHostedTenantInstances(totalInstances int) {
	for i := 1; i <= totalInstances; i++ {
		dir := hostedTenantInstanceDir(i)
		for _, name := range []string{"install.sh", "import.sh", "kube_config.yaml"} {
			RemoveFile(filepath.Join(dir, name))
		}
		RemoveFolder(dir)
	}
}

func installHostedTenantK3SCluster(config hostedTenantK3SConfig, plan *RancherResolvedPlan) error {
	version := strings.TrimSpace(plan.RecommendedK3SVersion)
	if version == "" {
		return fmt.Errorf("K3s version must not be empty for %s", config.DisplayName)
	}

	if err := prepareHostedTenantK3SNode(config.Node1IP, config, "SECRET", version, plan); err != nil {
		return fmt.Errorf("prepare first K3s node: %w", err)
	}
	if err := installHostedTenantK3SServer(config.Node1IP, version, plan); err != nil {
		return fmt.Errorf("install first K3s node: %w", err)
	}
	token, err := waitForHostedTenantK3SToken(config.Node1IP)
	if err != nil {
		return err
	}
	if err := waitForHostedTenantK3SNodeReady(config.Node1IP, 5*time.Minute); err != nil {
		logHostedTenantK3SDiagnostics(config.Node1IP)
		return err
	}

	if err := prepareHostedTenantK3SNode(config.Node2IP, config, token, version, plan); err != nil {
		return fmt.Errorf("prepare second K3s node: %w", err)
	}
	if err := installHostedTenantK3SServer(config.Node2IP, version, plan); err != nil {
		return fmt.Errorf("install second K3s node: %w", err)
	}
	if err := waitForHostedTenantK3SNodeReady(config.Node2IP, 5*time.Minute); err != nil {
		logHostedTenantK3SDiagnostics(config.Node2IP)
		return err
	}
	return nil
}

func prepareHostedTenantK3SNode(nodeIP string, config hostedTenantK3SConfig, token, version string, plan *RancherResolvedPlan) error {
	if _, err := RunCommand("sudo mkdir -p /etc/rancher/k3s /var/lib/rancher/k3s/agent/images", nodeIP); err != nil {
		return fmt.Errorf("failed creating K3s directories: %w", err)
	}
	if err := writeHostedTenantRemoteFile(nodeIP, "/etc/rancher/k3s/config.yaml", hostedTenantK3SConfigContent(config, token, nodeIP)); err != nil {
		return fmt.Errorf("failed writing K3s config: %w", err)
	}

	if username, password := dockerHubCredentialsFromEnv(); username != "" && password != "" {
		if err := writeHostedTenantRemoteFile(nodeIP, "/etc/rancher/k3s/registries.yaml", hostedTenantK3SRegistriesContent(username, password)); err != nil {
			return fmt.Errorf("failed writing registries config: %w", err)
		}
	}

	if !viper.GetBool("k3s.preload_images") {
		return nil
	}
	cmd := fmt.Sprintf(
		`tmp_images="$(mktemp /tmp/k3s-airgap-images-amd64.XXXXXX)"
trap 'rm -f "$tmp_images"' EXIT
curl -fsSL -o "$tmp_images" %s
if ! echo %s"  $tmp_images" | sha256sum -c -; then
  echo "SECURITY ERROR: K3s image checksum validation failed" >&2
  exit 1
fi
sudo mv "$tmp_images" /var/lib/rancher/k3s/agent/images/k3s-airgap-images-amd64.tar.zst
trap - EXIT`,
		shellQuote(hostedK3SAirgapImageURL(version)),
		shellQuote(plan.K3SAirgapImageSHA256),
	)
	if _, err := RunCommand(cmd, nodeIP); err != nil {
		return fmt.Errorf("failed preloading K3s images: %w", err)
	}
	return nil
}

func installHostedTenantK3SServer(nodeIP, version string, plan *RancherResolvedPlan) error {
	cmd := fmt.Sprintf(
		`tmp_script="$(mktemp /tmp/k3s-install.XXXXXX)"
trap 'rm -f "$tmp_script"' EXIT
curl -fsSL -o "$tmp_script" %s
if ! echo %s"  $tmp_script" | sha256sum -c -; then
  echo "SECURITY ERROR: K3s installer checksum validation failed" >&2
  exit 1
fi
sudo INSTALL_K3S_VERSION=%s sh "$tmp_script" server`,
		shellQuote(hostedK3SInstallScriptURL(version)),
		shellQuote(plan.K3SInstallerSHA256),
		shellQuote(version),
	)
	if _, err := RunCommand(cmd, nodeIP); err != nil {
		logHostedTenantK3SDiagnostics(nodeIP)
		return err
	}
	return nil
}

func waitForHostedTenantK3SToken(nodeIP string) (string, error) {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		token, err := RunCommand("sudo test -s /var/lib/rancher/k3s/server/token && sudo cat /var/lib/rancher/k3s/server/token", nodeIP)
		if err == nil && strings.TrimSpace(token) != "" {
			return strings.TrimSpace(token), nil
		}
		time.Sleep(10 * time.Second)
	}
	logHostedTenantK3SDiagnostics(nodeIP)
	return "", fmt.Errorf("timed out waiting for K3s token on %s", nodeIP)
}

func waitForHostedTenantK3SNodeReady(nodeIP string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := RunCommand("sudo systemctl is-active k3s || true", nodeIP)
		if err == nil && strings.TrimSpace(status) == "active" {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("timed out waiting for K3s node %s to become ready", nodeIP)
}

func logHostedTenantK3SDiagnostics(nodeIP string) {
	for _, cmd := range []string{
		"sudo systemctl status k3s --no-pager || true",
		"sudo journalctl -u k3s --no-pager -n 80 || true",
	} {
		output, err := RunCommand(cmd, nodeIP)
		if err != nil {
			log.Printf("[hosted-tenant] failed collecting diagnostics on %s with %q: %v", nodeIP, cmd, err)
			continue
		}
		log.Printf("[hosted-tenant] K3s diagnostics from %s:\n%s", nodeIP, output)
	}
}

func hostedTenantK3SConfigContent(config hostedTenantK3SConfig, token, nodeIP string) string {
	lines := []string{
		fmt.Sprintf("token: %s", hostedTenantYAMLQuote(token)),
		fmt.Sprintf("datastore-endpoint: %s", hostedTenantYAMLQuote(fmt.Sprintf("mysql://tfadmin:%s@tcp(%s)/k3s", config.DBPassword, config.DBEndpoint))),
		"tls-san:",
	}
	for _, san := range []string{config.RancherURL, config.Node1IP, config.Node2IP} {
		if strings.TrimSpace(san) != "" {
			lines = append(lines, fmt.Sprintf("  - %s", hostedTenantYAMLQuote(san)))
		}
	}
	lines = append(lines, fmt.Sprintf("node-external-ip: %s", hostedTenantYAMLQuote(nodeIP)))
	return strings.Join(lines, "\n")
}

func hostedTenantK3SRegistriesContent(username, password string) string {
	return strings.Join([]string{
		"configs:",
		"  docker.io:",
		"    auth:",
		fmt.Sprintf("      username: %s", hostedTenantYAMLQuote(username)),
		fmt.Sprintf("      password: %s", hostedTenantYAMLQuote(password)),
	}, "\n")
}

func dockerHubCredentialsFromEnv() (string, string) {
	return strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME")), strings.TrimSpace(os.Getenv("DOCKERHUB_PASSWORD"))
}

func hostedTenantYAMLQuote(value string) string {
	quoted, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%q", value)
	}
	return string(quoted)
}

func writeHostedTenantRemoteFile(nodeIP, path, content string) error {
	cmd := fmt.Sprintf("cat <<'EOF' | sudo tee %s >/dev/null\n%s\nEOF", shellQuote(path), content)
	_, err := RunCommand(cmd, nodeIP)
	return err
}

func saveHostedTenantKubeconfig(nodeIP, scriptDir string) error {
	rawKubeconfig, err := RunCommand("sudo cat /etc/rancher/k3s/k3s.yaml", nodeIP)
	if err != nil {
		return fmt.Errorf("failed to get K3s kubeconfig from node %s: %w", nodeIP, err)
	}
	configIP := fmt.Sprintf("https://%s:6443", nodeIP)
	output := strings.Replace(rawKubeconfig, "https://127.0.0.1:6443", configIP, -1)

	absScriptDir, err := absoluteFromWorkingDir(scriptDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absScriptDir, 0o700); err != nil {
		return fmt.Errorf("failed to create script dir %s: %w", absScriptDir, err)
	}
	return os.WriteFile(filepath.Join(absScriptDir, "kube_config.yaml"), []byte(output), 0o600)
}

func createHostedTenantRancherInstallScript(helmCommand, scriptDir string) error {
	installScript := fmt.Sprintf(`#!/bin/bash
set -euo pipefail
if [ ! -f "kube_config.yaml" ]; then
  echo "ERROR: kube_config.yaml not found. Make sure you're in the right directory."
  exit 1
fi
export KUBECONFIG="$(pwd)/kube_config.yaml"
kubectl cluster-info
helm repo update
kubectl create namespace cattle-system --dry-run=client -o yaml | kubectl apply -f -
%s
`, helmCommand)

	absScriptDir, err := absoluteFromWorkingDir(scriptDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absScriptDir, 0o700); err != nil {
		return fmt.Errorf("failed to create script dir %s: %w", absScriptDir, err)
	}
	return os.WriteFile(filepath.Join(absScriptDir, "install.sh"), []byte(installScript), 0o700)
}

func executeHostedTenantScript(scriptDir, scriptName string) error {
	absScriptDir, err := absoluteFromWorkingDir(scriptDir)
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(absScriptDir, scriptName)
	cmd := exec.Command(scriptPath)
	cmd.Dir = absScriptDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", filepath.Join(absScriptDir, "kube_config.yaml")))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func waitForHostedTenantRancherStable(rancherURL string, timeout time.Duration) error {
	client := rancherReadyHTTPClient()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ready, summary := rancherHTTPReady(client, clickableURL(rancherURL))
		if ready {
			return nil
		}
		log.Printf("[hosted-tenant] Waiting for Rancher %s: %s", rancherURL, summary)
		time.Sleep(15 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for Rancher %s to become stable", timeout, rancherURL)
}

func importHostedTenantCluster(hostURL, adminToken string, tenantNumber int, tenantConfig hostedTenantK3SConfig) error {
	clusterName := fmt.Sprintf("imported-tenant-%d", tenantNumber)
	if err := createHostedTenantImport(hostURL, adminToken, clusterName); err != nil {
		return err
	}
	time.Sleep(30 * time.Second)

	manifestURL, err := latestHostedTenantManifestURL(hostURL, adminToken)
	if err != nil {
		return err
	}
	scriptDir := hostedTenantInstanceDir(tenantConfig.Index)
	if err := saveHostedTenantKubeconfig(tenantConfig.Node1IP, scriptDir); err != nil {
		return err
	}
	if err := applyHostedTenantImportManifest(scriptDir, manifestURL); err != nil {
		return err
	}
	return waitForHostedTenantClusterActive(hostURL, adminToken, clusterName, 10*time.Minute)
}

func createHostedTenantImport(hostURL, adminToken, clusterName string) error {
	client := hostedTenantRancherHTTPClient()
	payload := map[string]interface{}{
		"type": "provisioning.cattle.io.cluster",
		"metadata": map[string]string{
			"namespace": "fleet-default",
			"name":      clusterName,
		},
		"spec": map[string]interface{}{},
	}
	return postRancherJSON(client, strings.TrimRight(clickableURL(hostURL), "/")+"/v1/provisioning.cattle.io.clusters", adminToken, payload, &struct{}{})
}

func latestHostedTenantManifestURL(hostURL, adminToken string) (string, error) {
	client := hostedTenantRancherHTTPClient()
	var response struct {
		Data []struct {
			ManifestURL string `json:"manifestUrl"`
			CreatedTS   int64  `json:"createdTS"`
		} `json:"data"`
	}
	if err := getRancherJSON(client, strings.TrimRight(clickableURL(hostURL), "/")+"/v3/clusterregistrationtokens", adminToken, &response); err != nil {
		return "", err
	}
	sort.Slice(response.Data, func(i, j int) bool { return response.Data[i].CreatedTS > response.Data[j].CreatedTS })
	for _, item := range response.Data {
		if strings.TrimSpace(item.ManifestURL) != "" {
			return item.ManifestURL, nil
		}
	}
	return "", fmt.Errorf("host Rancher did not return an import manifest URL")
}

func applyHostedTenantImportManifest(scriptDir, manifestURL string) error {
	absScriptDir, err := absoluteFromWorkingDir(scriptDir)
	if err != nil {
		return err
	}
	cmd := exec.Command("kubectl", "apply", "-f", manifestURL, "--validate=false", "--insecure-skip-tls-verify")
	cmd.Dir = absScriptDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", filepath.Join(absScriptDir, "kube_config.yaml")))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func waitForHostedTenantClusterActive(hostURL, adminToken, clusterName string, timeout time.Duration) error {
	client := hostedTenantRancherHTTPClient()
	targetURL := strings.TrimRight(clickableURL(hostURL), "/") + "/v1/provisioning.cattle.io.clusters"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var response struct {
			Data []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Status struct {
					Phase string `json:"phase"`
					Ready bool   `json:"ready"`
				} `json:"status"`
			} `json:"data"`
		}
		if err := getRancherJSON(client, targetURL, adminToken, &response); err != nil {
			log.Printf("[hosted-tenant] Waiting for cluster %s status: %v", clusterName, err)
			time.Sleep(15 * time.Second)
			continue
		}
		for _, cluster := range response.Data {
			if cluster.Metadata.Name == clusterName && (cluster.Status.Phase == "Active" || cluster.Status.Ready) {
				return nil
			}
		}
		time.Sleep(15 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for cluster %s to become Active", timeout, clusterName)
}

func installHostedTenantRancher(tenantNumber int, tenantConfig hostedTenantK3SConfig, helmCommand string) error {
	scriptDir := hostedTenantInstanceDir(tenantConfig.Index)
	helmCommand = rancherHelmCommandWithHostname(helmCommand, tenantConfig.RancherURL)
	if err := createHostedTenantRancherInstallScript(helmCommand, scriptDir); err != nil {
		return fmt.Errorf("tenant %d install script: %w", tenantNumber, err)
	}
	if err := saveHostedTenantKubeconfig(tenantConfig.Node1IP, scriptDir); err != nil {
		return fmt.Errorf("tenant %d kubeconfig: %w", tenantNumber, err)
	}
	if err := executeHostedTenantScript(scriptDir, "install.sh"); err != nil {
		return fmt.Errorf("tenant %d install: %w", tenantNumber, err)
	}
	if err := waitForHostedTenantRancherStable(tenantConfig.RancherURL, 8*time.Minute); err != nil {
		return fmt.Errorf("tenant %d readiness: %w", tenantNumber, err)
	}
	return nil
}

func hostedTenantRancherHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}
