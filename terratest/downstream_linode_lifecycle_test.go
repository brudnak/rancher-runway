package test

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
)

type provisioningClusterStatus struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Phase       string `json:"phase"`
		Ready       bool   `json:"ready"`
		ClusterName string `json:"clusterName"`
		Conditions  []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

type podList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	} `json:"items"`
}

func TestHAProvisionLinodeDownstream(t *testing.T) {
	requireExplicitLifecycleTest(t, "TestHAProvisionLinodeDownstream")
	setupConfig(t)

	linodeToken := linodeAccessToken()
	if linodeToken == "" {
		t.Skip("Linode access token is not configured; skipping Linode downstream provisioning")
	}

	totalHAs := viper.GetInt("total_has")
	plans, err := legacyLinodeDownstreamPlans(totalHAs)
	if err != nil {
		t.Fatal(err)
	}
	provisionConfiguredLinodeDownstreamPlans(t, plans, linodeToken)
}

func TestHAProvisionConfiguredLinodeDownstreams(t *testing.T) {
	requireExplicitLifecycleTest(t, "TestHAProvisionConfiguredLinodeDownstreams")
	setupConfig(t)

	totalHAs := viper.GetInt("total_has")
	if totalHAs < 1 {
		t.Fatal("total_has must be at least 1")
	}
	plans, err := configuredLinodeDownstreamPlans(totalHAs)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.AnyLinodeDownstreamPlanEnabled(plans) {
		t.Skip("no downstream Linode plans are enabled")
	}
	linodeToken := linodeAccessToken()
	if linodeToken == "" {
		t.Fatal("Linode access token is required for enabled downstream Linode plans")
	}
	provisionConfiguredLinodeDownstreamPlans(t, plans, linodeToken)
}

func provisionConfiguredLinodeDownstreamPlans(t *testing.T, plans []settings.LinodeDownstreamPlan, linodeToken string) {
	t.Helper()
	work, err := determineDownstreamProvisioningWork(
		plans,
		readDownstreamOutputRecord,
		downstreamManagementKubeconfigPath,
		getProvisioningClusterStatus,
	)
	if err != nil {
		t.Fatalf("Downstream reuse preflight failed before mutation: %v", err)
	}
	if len(work) == 0 {
		log.Printf("[downstream] All enabled downstream clusters are already active; nothing to provision")
		return
	}

	plansNeedingMutation, err := downstreamPlansForProvisioningWork(len(plans), work)
	if err != nil {
		t.Fatalf("Downstream provisioning work preflight failed before mutation: %v", err)
	}
	catalogTimeout := durationFromEnv("LINODE_CATALOG_TIMEOUT", 45*time.Second)
	catalogCtx, cancelCatalog := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancelCatalog()
	catalog, err := collectLinodeCatalog(catalogCtx, &http.Client{Timeout: 15 * time.Second}, linodeCatalogDefaultAPIBaseURL, linodeToken)
	if err != nil {
		t.Fatalf("Linode provider catalog preflight failed before downstream mutation: %v", err)
	}
	if err := validateLinodeDownstreamPlansAgainstCatalog(plansNeedingMutation, catalog); err != nil {
		t.Fatalf("Linode provider plan preflight failed before downstream mutation: %v", err)
	}

	totalHAs := len(plans)
	terraformOptions := getTerraformOptions(t, totalHAs)
	outputs := getTerraformOutputs(t, terraformOptions)
	if len(outputs) == 0 {
		t.Fatal("No outputs received from terraform")
	}

	runID := downstreamProvisioningRunID()
	namePrefix := strings.TrimSpace(os.Getenv("LINODE_CLUSTER_PREFIX"))
	namePrefix = downstreamClusterNamePrefix(namePrefix, runID)

	timeout := durationFromEnv("LINODE_DOWNSTREAM_TIMEOUT", 15*time.Minute)
	var wg sync.WaitGroup
	errCh := make(chan error, totalHAs)
	for _, item := range work {
		item := item
		instanceNum := item.InstanceNum
		haOutputs := getHAOutputs(instanceNum, outputs)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := provisionLinodeDownstreamForHA(instanceNum, haOutputs, item.Plan, item.Existing, linodeToken, namePrefix, runID, timeout); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	var failures []string
	for err := range errCh {
		failures = append(failures, err.Error())
	}
	if len(failures) > 0 {
		t.Fatalf("Linode downstream provisioning failed:\n%s", strings.Join(failures, "\n"))
	}
}

type downstreamProvisioningWorkItem struct {
	InstanceNum int
	Plan        settings.LinodeDownstreamPlan
	Existing    *downstreamOutputRecord
}

type downstreamOutputRecordReader func(haIndex int) (downstreamOutputRecord, bool, error)
type downstreamKubeconfigPathResolver func(instanceNum int) (string, error)

func determineDownstreamProvisioningWork(plans []settings.LinodeDownstreamPlan, readRecord downstreamOutputRecordReader, resolveKubeconfig downstreamKubeconfigPathResolver, getStatus provisioningClusterStatusGetter) ([]downstreamProvisioningWorkItem, error) {
	if readRecord == nil {
		return nil, fmt.Errorf("downstream output record reader must not be nil")
	}
	if resolveKubeconfig == nil {
		return nil, fmt.Errorf("downstream kubeconfig resolver must not be nil")
	}

	work := make([]downstreamProvisioningWorkItem, 0, len(plans))
	for index, plan := range plans {
		if !plan.Enabled {
			continue
		}
		instanceNum := index + 1
		kubeconfigPath, err := resolveKubeconfig(instanceNum)
		if err != nil {
			return nil, fmt.Errorf("resolve management kubeconfig for HA %d: %w", instanceNum, err)
		}
		existing, found, err := readRecord(instanceNum)
		if err != nil {
			return nil, fmt.Errorf("read downstream output record for HA %d: %w", instanceNum, err)
		}

		item := downstreamProvisioningWorkItem{InstanceNum: instanceNum, Plan: plan}
		if found {
			reusable, err := reusableActiveDownstreamRecord(existing, kubeconfigPath, getStatus)
			if err != nil {
				return nil, fmt.Errorf("cannot safely verify recorded downstream cluster %s for HA %d: %w", existing.ClusterName, instanceNum, err)
			}
			if reusable {
				log.Printf("[downstream][ha-%d] Reusing already-active recorded cluster %s (%s)", instanceNum, existing.ClusterName, existing.ManagementClusterID)
				continue
			}
			existingCopy := existing
			item.Existing = &existingCopy
		}
		work = append(work, item)
	}
	return work, nil
}

func downstreamPlansForProvisioningWork(planCount int, work []downstreamProvisioningWorkItem) ([]settings.LinodeDownstreamPlan, error) {
	if planCount < 0 {
		return nil, fmt.Errorf("downstream plan count must not be negative")
	}
	plans := make([]settings.LinodeDownstreamPlan, planCount)
	seen := make(map[int]struct{}, len(work))
	for _, item := range work {
		if item.InstanceNum < 1 || item.InstanceNum > planCount {
			return nil, fmt.Errorf("downstream provisioning work references HA %d outside plan range 1-%d", item.InstanceNum, planCount)
		}
		if _, exists := seen[item.InstanceNum]; exists {
			return nil, fmt.Errorf("downstream provisioning work contains duplicate HA %d", item.InstanceNum)
		}
		seen[item.InstanceNum] = struct{}{}
		plan := item.Plan
		plan.Enabled = true
		plans[item.InstanceNum-1] = plan
	}
	return plans, nil
}

func downstreamManagementKubeconfigPath(instanceNum int) (string, error) {
	kubeconfigPath := filepath.Join(haInstanceDir(instanceNum), "kube_config.yaml")
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return "", fmt.Errorf("kubeconfig not available at %s: %w", kubeconfigPath, err)
	}
	return kubeconfigPath, nil
}

func provisionLinodeDownstreamForHA(instanceNum int, haOutputs TerraformOutputs, plan settings.LinodeDownstreamPlan, existing *downstreamOutputRecord, linodeToken, namePrefix, runID string, timeout time.Duration) (provisionErr error) {
	kubeconfigPath := filepath.Join(haInstanceDir(instanceNum), "kube_config.yaml")
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return fmt.Errorf("kubeconfig not available for HA %d at %s: %w", instanceNum, kubeconfigPath, err)
	}

	if existing != nil {
		log.Printf("[downstream][ha-%d] Cleaning recorded cluster %s before provisioning retry", instanceNum, existing.ClusterName)
		if err := deleteLinodeDownstream(*existing, durationFromEnv("LINODE_DOWNSTREAM_DELETE_TIMEOUT", 20*time.Minute)); err != nil {
			return fmt.Errorf("cannot safely retry HA %d while recorded downstream cluster %s remains: %w", instanceNum, existing.ClusterName, err)
		}
	}

	suffix := randomHex(4)
	clusterName := downstreamClusterName(namePrefix, runID, instanceNum, suffix)

	adminToken, err := createRancherAdminToken(haOutputs.RancherURL, viper.GetString("rancher.bootstrap_password"))
	if err != nil {
		return err
	}
	if err := configureRancherServerURL(haOutputs.RancherURL, adminToken); err != nil {
		return err
	}
	kubernetesVersion, err := resolveDownstreamKubernetesVersion(haOutputs.RancherURL, adminToken, plan.Distribution, plan.KubernetesVersion)
	if err != nil {
		return err
	}

	cfg := downstreamProvisioningConfig{
		ClusterName:       clusterName,
		MachineName:       dnsLabel("nc-" + clusterName + "-pool1"),
		SecretName:        dnsLabel("cc-" + clusterName),
		Namespace:         defaultLinodeNamespace,
		Distribution:      strings.ToLower(strings.TrimSpace(plan.Distribution)),
		KubernetesVersion: kubernetesVersion,
		Region:            strings.TrimSpace(plan.Region),
		InstanceType:      strings.TrimSpace(plan.InstanceType),
		Image:             strings.TrimSpace(plan.Image),
		LinodeToken:       linodeToken,
	}
	record := downstreamOutputRecordFromConfig(instanceNum, cfg, haOutputs)
	if err := writeDownstreamProvisioningPhase(&record, "planned", ""); err != nil {
		return err
	}
	defer func() {
		if provisionErr == nil {
			return
		}
		safeError := redactDownstreamProvisioningError(provisionErr, linodeToken, adminToken)
		if err := writeDownstreamProvisioningPhase(&record, "failed", safeError); err != nil {
			provisionErr = errors.Join(provisionErr, fmt.Errorf("failed to persist downstream failure state: %w", err))
		}
	}()

	log.Printf("[downstream][ha-%d] Creating one-node Linode %s cluster %s on %s (%s, %s, %s)",
		instanceNum, strings.ToUpper(cfg.Distribution), cfg.ClusterName, clickableURL(haOutputs.RancherURL), cfg.KubernetesVersion, cfg.Region, cfg.InstanceType)

	if err := ensureLinodeNodeDriverActive(kubeconfigPath); err != nil {
		return err
	}

	if err := kubectlApply(kubeconfigPath, renderLinodeCredentialSecretManifest(cfg)); err != nil {
		return err
	}
	if err := writeDownstreamProvisioningPhase(&record, "credential-created", ""); err != nil {
		return err
	}

	machineName, err := createLinodeMachineConfig(haOutputs.RancherURL, adminToken, cfg)
	if err != nil {
		return err
	}
	cfg.MachineName = machineName
	record.MachineConfig = machineName
	log.Printf("[downstream][ha-%d] Created Linode machine config %s", instanceNum, cfg.MachineName)
	if err := writeDownstreamProvisioningPhase(&record, "machine-config-created", ""); err != nil {
		return err
	}

	if err := kubectlApply(kubeconfigPath, renderLinodeDownstreamClusterManifest(cfg)); err != nil {
		return err
	}
	if err := writeDownstreamProvisioningPhase(&record, "provisioning", ""); err != nil {
		return err
	}

	if err := waitForProvisioningClusterActive(kubeconfigPath, cfg.Namespace, cfg.ClusterName, timeout); err != nil {
		return err
	}
	status, err := getProvisioningClusterStatus(kubeconfigPath, cfg.Namespace, cfg.ClusterName)
	if err != nil {
		return err
	}
	managementClusterID := strings.TrimSpace(status.Status.ClusterName)
	if managementClusterID == "" {
		return fmt.Errorf("downstream cluster %s is active but status.clusterName is empty", cfg.ClusterName)
	}
	record.ManagementClusterID = managementClusterID
	if err := writeDownstreamProvisioningPhase(&record, "active-pending-artifacts", ""); err != nil {
		return err
	}
	if _, err := writeDownstreamKubeconfig(instanceNum, cfg, haOutputs, adminToken, managementClusterID); err != nil {
		return err
	}
	if err := writeDownstreamEnvironment(instanceNum, cfg, haOutputs, adminToken); err != nil {
		return err
	}
	if err := writeDownstreamProvisioningPhase(&record, "active", ""); err != nil {
		return err
	}

	log.Printf("[downstream][ha-%d] Linode downstream cluster %s is active", instanceNum, cfg.ClusterName)
	return nil
}

type provisioningClusterStatusGetter func(kubeconfigPath, namespace, clusterName string) (provisioningClusterStatus, error)

func reusableActiveDownstreamRecord(record downstreamOutputRecord, kubeconfigPath string, getStatus provisioningClusterStatusGetter) (bool, error) {
	if !strings.EqualFold(strings.TrimSpace(record.Phase), "active") {
		return false, nil
	}
	if getStatus == nil {
		return false, fmt.Errorf("provisioning cluster status getter must not be nil")
	}
	status, err := getStatus(kubeconfigPath, record.Namespace, record.ClusterName)
	if err != nil {
		if provisioningClusterNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if !strings.EqualFold(strings.TrimSpace(status.Status.Phase), "active") && !status.Status.Ready {
		return false, fmt.Errorf("record is active but live cluster phase is %q and ready=%t", status.Status.Phase, status.Status.Ready)
	}
	recordedManagementID := strings.TrimSpace(record.ManagementClusterID)
	liveManagementID := strings.TrimSpace(status.Status.ClusterName)
	if recordedManagementID == "" || liveManagementID == "" {
		return false, fmt.Errorf("active downstream record or live cluster is missing its management cluster id")
	}
	if recordedManagementID != liveManagementID {
		return false, fmt.Errorf("management cluster id changed from %q to %q", recordedManagementID, liveManagementID)
	}
	return true, nil
}

func ensureLinodeNodeDriverActive(kubeconfigPath string) error {
	output, err := runKubectlOutput(kubeconfigPath, "get", "nodedriver.management.cattle.io", "linode", "-o", "json")
	if err != nil {
		return fmt.Errorf("linode node driver is not available: %w", err)
	}

	var driver struct {
		Spec struct {
			Active bool `json:"active"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(output), &driver); err != nil {
		return fmt.Errorf("failed to parse Linode node driver: %w", err)
	}
	if driver.Spec.Active {
		return waitForLinodeMachineConfigAPI(kubeconfigPath, durationFromEnv("LINODE_DRIVER_TIMEOUT", 5*time.Minute))
	}

	log.Printf("[downstream] Activating Linode node driver")
	if err := runKubectlDirect(kubeconfigPath, "patch", "nodedriver.management.cattle.io", "linode", "--type=merge", "-p", `{"spec":{"active":true}}`); err != nil {
		return err
	}
	return waitForLinodeMachineConfigAPI(kubeconfigPath, durationFromEnv("LINODE_DRIVER_TIMEOUT", 5*time.Minute))
}

func waitForLinodeMachineConfigAPI(kubeconfigPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := runKubectlOutput(kubeconfigPath, "api-resources", "--api-group", "rke-machine-config.cattle.io", "-o", "name")
		if err == nil {
			for _, resource := range strings.Fields(output) {
				if resource == "linodeconfigs" || resource == "linodeconfigs.rke-machine-config.cattle.io" {
					return nil
				}
			}
			log.Printf("[downstream] Waiting for Linode machine config API; current resources: %s", strings.Join(strings.Fields(output), ", "))
		} else {
			log.Printf("[downstream] Waiting for Linode machine config API: %v", err)
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for Linode machine config API", timeout)
}

func TestHADeleteLinodeDownstream(t *testing.T) {
	requireExplicitLifecycleTest(t, "TestHADeleteLinodeDownstream")
	setupConfig(t)

	records, err := readDownstreamOutputRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Skip("no downstream-ha-*.json files found; skipping Linode downstream cleanup")
	}

	timeout := durationFromEnv("LINODE_DOWNSTREAM_DELETE_TIMEOUT", 20*time.Minute)
	if err := cleanupDownstreamOutputRecords(records, timeout, deleteLinodeDownstream); err != nil {
		t.Fatalf("Linode downstream cleanup failed:\n%v", err)
	}
}

func cleanupRecordedLinodeDownstreams(timeout time.Duration) error {
	records, err := readDownstreamOutputRecords()
	if err != nil {
		return err
	}
	return cleanupDownstreamOutputRecords(records, timeout, deleteLinodeDownstream)
}

func cleanupDownstreamOutputRecords(records []downstreamOutputRecord, timeout time.Duration, deleteRecord func(downstreamOutputRecord, time.Duration) error) error {
	if deleteRecord == nil {
		return fmt.Errorf("downstream cleanup function must not be nil")
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(records))
	for _, record := range records {
		record := record
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := deleteRecord(record, timeout); err != nil {
				errCh <- fmt.Errorf("HA %d cluster %s: %w", record.HAIndex, record.ClusterName, err)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	var failures []error
	for err := range errCh {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func deleteLinodeDownstream(record downstreamOutputRecord, timeout time.Duration) (cleanupErr error) {
	if record.Namespace == "" {
		record.Namespace = defaultLinodeNamespace
	}
	if err := writeDownstreamProvisioningPhase(&record, "deleting", ""); err != nil {
		return fmt.Errorf("persist deleting phase: %w", err)
	}
	defer func() {
		if cleanupErr == nil {
			return
		}
		if err := writeDownstreamProvisioningPhase(&record, "cleanup-failed", redactDownstreamProvisioningError(cleanupErr)); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("persist cleanup failure: %w", err))
		}
	}()

	kubeconfigPath := filepath.Join(haInstanceDir(record.HAIndex), "kube_config.yaml")
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return fmt.Errorf("kubeconfig not available for HA %d at %s: %w", record.HAIndex, kubeconfigPath, err)
	}

	log.Printf("[downstream][ha-%d] Deleting Linode downstream cluster %s", record.HAIndex, record.ClusterName)
	if err := runKubectlDirect(kubeconfigPath, "delete", "clusters.provisioning.cattle.io", record.ClusterName, "-n", record.Namespace, "--ignore-not-found=true"); err != nil {
		return err
	}
	if err := waitForProvisioningClusterDeleted(kubeconfigPath, record.Namespace, record.ClusterName, timeout); err != nil {
		return err
	}

	if record.MachineConfig != "" {
		if err := runKubectlDirect(kubeconfigPath, "delete", "linodeconfig.rke-machine-config.cattle.io", record.MachineConfig, "-n", record.Namespace, "--ignore-not-found=true"); err != nil {
			return fmt.Errorf("delete Linode machine config %s: %w", record.MachineConfig, err)
		}
	}
	if record.SecretName != "" {
		if err := runKubectlDirect(kubeconfigPath, "delete", "secret", record.SecretName, "-n", "cattle-global-data", "--ignore-not-found=true"); err != nil {
			return fmt.Errorf("delete Linode credential secret %s: %w", record.SecretName, err)
		}
	}

	if err := removeDownstreamOutputArtifactsForRun(record.RunID, record.HAIndex); err != nil {
		return err
	}
	log.Printf("[downstream][ha-%d] Removed downstream output records for %s", record.HAIndex, record.ClusterName)
	return nil
}

func waitForProvisioningClusterDeleted(kubeconfigPath, namespace, clusterName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := getProvisioningClusterStatus(kubeconfigPath, namespace, clusterName)
		if err != nil {
			if provisioningClusterNotFound(err) {
				log.Printf("[downstream] Cluster %s deleted", clusterName)
				return nil
			}
			log.Printf("[downstream] Waiting for cluster %s deletion; status check failed: %v", clusterName, err)
		} else {
			log.Printf("[downstream] Waiting for cluster %s deletion", clusterName)
		}
		time.Sleep(20 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for downstream cluster %s deletion", timeout, clusterName)
}

func provisioningClusterNotFound(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "notfound") || strings.Contains(message, "not found")
}

func createLinodeMachineConfig(rancherURL, bearerToken string, cfg downstreamProvisioningConfig) (string, error) {
	rancherURL = strings.TrimRight(clickableURL(rancherURL), "/")
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	var out struct {
		ID       string `json:"id"`
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	apiURL := fmt.Sprintf("%s/v1/rke-machine-config.cattle.io.linodeconfigs/%s", rancherURL, url.PathEscape(cfg.Namespace))
	if err := postRancherJSON(client, apiURL, bearerToken, linodeMachineConfigPayload(cfg), &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.Metadata.Name) != "" {
		return out.Metadata.Name, nil
	}
	if strings.TrimSpace(out.ID) != "" {
		parts := strings.Split(out.ID, "/")
		return parts[len(parts)-1], nil
	}
	return "", fmt.Errorf("Rancher LinodeConfig response did not include a machine config name")
}

func waitForProvisioningClusterActive(kubeconfigPath, namespace, clusterName string, timeout time.Duration) error {
	start := time.Now()
	deadline := start.Add(timeout)
	attempt := 0

	for time.Now().Before(deadline) {
		attempt++
		status, err := getProvisioningClusterStatus(kubeconfigPath, namespace, clusterName)
		if err != nil {
			log.Printf("[downstream] Cluster %s status unavailable on attempt %d: %v", clusterName, attempt, err)
		} else {
			summary := summarizeProvisioningClusterStatus(status)
			log.Printf("[downstream] Cluster %s attempt %d after %s: %s", clusterName, attempt, time.Since(start).Round(time.Second), summary)
			if attempt == 1 || attempt%6 == 0 {
				logDownstreamProvisioningDiagnostics(kubeconfigPath, namespace, clusterName, strings.TrimSpace(status.Status.ClusterName))
			}
			if strings.EqualFold(status.Status.Phase, "Active") || status.Status.Ready {
				return nil
			}
		}
		time.Sleep(20 * time.Second)
	}

	return fmt.Errorf("timed out after %s waiting for downstream cluster %s to become active", timeout, clusterName)
}

func logDownstreamProvisioningDiagnostics(kubeconfigPath, namespace, clusterName, managementClusterID string) {
	commands := [][]string{
		{"describe", "clusters.provisioning.cattle.io", clusterName, "-n", namespace},
		{"get", "linodeconfigs.rke-machine-config.cattle.io", "-n", namespace, "-o", "wide"},
		{"get", "clusters.cluster.x-k8s.io", "-A", "-o", "wide"},
		{"describe", "clusters.cluster.x-k8s.io", clusterName, "-n", namespace},
		{"get", "machinedeployments.cluster.x-k8s.io", "-A", "-o", "wide"},
		{"get", "machinesets.cluster.x-k8s.io", "-A", "-o", "wide"},
		{"get", "machines.cluster.x-k8s.io", "-A", "-o", "wide"},
		{"get", "machines.cluster.x-k8s.io", "-A", "-l", "cluster.x-k8s.io/cluster-name=" + clusterName, "-o", "yaml"},
		{"get", "jobs", "-n", namespace, "-o", "wide"},
		{"get", "pods", "-n", namespace, "-o", "wide"},
		{"get", "events", "-n", namespace, "--sort-by=.lastTimestamp"},
	}
	if managementClusterID != "" {
		commands = append(commands,
			[]string{"get", "clusters.management.cattle.io", managementClusterID, "-o", "yaml"},
		)
	}

	for _, args := range commands {
		output, err := runKubectlOutput(kubeconfigPath, args...)
		label := strings.Join(args, " ")
		if err != nil {
			log.Printf("[downstream][diagnostics][%s] %v", label, err)
			continue
		}
		log.Printf("[downstream][diagnostics][%s]\n%s", label, trimDiagnosticOutput(output))
	}
	logDownstreamMachinePodDiagnostics(kubeconfigPath, namespace, clusterName)
}

func logDownstreamMachinePodDiagnostics(kubeconfigPath, namespace, clusterName string) {
	output, err := runKubectlOutput(kubeconfigPath, "get", "pods", "-n", namespace, "-o", "json")
	if err != nil {
		log.Printf("[downstream][diagnostics][get pods -n %s -o json] %v", namespace, err)
		return
	}

	var pods podList
	if err := json.Unmarshal([]byte(output), &pods); err != nil {
		log.Printf("[downstream][diagnostics][get pods -n %s -o json] parse failed: %v", namespace, err)
		return
	}

	logged := 0
	for _, pod := range pods.Items {
		podName := strings.TrimSpace(pod.Metadata.Name)
		if podName == "" || !strings.Contains(podName, clusterName) {
			continue
		}
		logged++
		logDownstreamDiagnosticCommand(kubeconfigPath, "describe", "pod", podName, "-n", namespace)
		logDownstreamDiagnosticCommand(kubeconfigPath, "logs", podName, "-n", namespace, "--all-containers=true", "--tail=200")
		if logged >= 5 {
			log.Printf("[downstream][diagnostics] skipping remaining machine pod logs after %d pods", logged)
			return
		}
	}
	if logged == 0 {
		log.Printf("[downstream][diagnostics] no machine pods matched cluster %s in namespace %s", clusterName, namespace)
	}
}

func logDownstreamDiagnosticCommand(kubeconfigPath string, args ...string) {
	output, err := runKubectlOutput(kubeconfigPath, args...)
	label := strings.Join(args, " ")
	if err != nil {
		log.Printf("[downstream][diagnostics][%s] %v", label, err)
		return
	}
	log.Printf("[downstream][diagnostics][%s]\n%s", label, trimDiagnosticOutput(output))
}

func trimDiagnosticOutput(output string) string {
	const maxLen = 6000
	output = strings.TrimSpace(output)
	if len(output) <= maxLen {
		return output
	}
	return output[:maxLen] + "\n...<truncated>"
}

func getProvisioningClusterStatus(kubeconfigPath, namespace, clusterName string) (provisioningClusterStatus, error) {
	output, err := runKubectlOutput(kubeconfigPath, "get", "clusters.provisioning.cattle.io", clusterName, "-n", namespace, "-o", "json")
	if err != nil {
		return provisioningClusterStatus{}, err
	}

	var status provisioningClusterStatus
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		return provisioningClusterStatus{}, fmt.Errorf("failed to parse provisioning cluster status: %w", err)
	}
	return status, nil
}

func summarizeProvisioningClusterStatus(status provisioningClusterStatus) string {
	parts := []string{fmt.Sprintf("phase=%s ready=%t cluster=%s", status.Status.Phase, status.Status.Ready, status.Status.ClusterName)}
	for _, condition := range status.Status.Conditions {
		if condition.Status == "" || condition.Type == "" {
			continue
		}
		detail := fmt.Sprintf("%s=%s", condition.Type, condition.Status)
		if condition.Reason != "" {
			detail += "/" + condition.Reason
		}
		if condition.Message != "" {
			detail += " " + condition.Message
		}
		parts = append(parts, detail)
	}
	return strings.Join(parts, "; ")
}

func downstreamOutputRecordFromConfig(instanceNum int, cfg downstreamProvisioningConfig, haOutputs TerraformOutputs) downstreamOutputRecord {
	record := downstreamOutputRecord{
		RunID:             downstreamOutputRunID(os.Getenv(runIDEnv)),
		HAIndex:           instanceNum,
		RancherHost:       clickableURL(haOutputs.RancherURL),
		ClusterName:       cfg.ClusterName,
		KubeconfigPath:    downstreamKubeconfigPath(instanceNum),
		Distribution:      cfg.Distribution,
		KubernetesVersion: cfg.KubernetesVersion,
		LinodeRegion:      cfg.Region,
		LinodeType:        cfg.InstanceType,
		LinodeImage:       cfg.Image,
		MachineConfig:     cfg.MachineName,
		SecretName:        cfg.SecretName,
		Namespace:         cfg.Namespace,
	}
	if strings.EqualFold(cfg.Distribution, "k3s") {
		record.K3SVersion = cfg.KubernetesVersion
	}
	return record
}

func writeDownstreamProvisioningPhase(record *downstreamOutputRecord, phase, errorMessage string) error {
	record.Phase = strings.TrimSpace(phase)
	record.Error = strings.TrimSpace(errorMessage)
	return writeDownstreamOutputRecord(*record)
}

func redactDownstreamProvisioningError(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, secret := range secrets {
		if secret = strings.TrimSpace(secret); secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	const maxErrorLength = 4000
	if len(message) > maxErrorLength {
		message = message[:maxErrorLength] + "…"
	}
	return message
}

func writeDownstreamEnvironment(instanceNum int, cfg downstreamProvisioningConfig, haOutputs TerraformOutputs, adminToken string) error {
	if strings.TrimSpace(adminToken) == "" {
		return fmt.Errorf("Rancher admin token must not be empty")
	}
	envPath := downstreamOutputPathForRun(os.Getenv(runIDEnv), fmt.Sprintf("downstream-ha-%d.env", instanceNum))
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return err
	}
	envContent := fmt.Sprintf("RANCHER_HOST=%s\nRANCHER_ADMIN_TOKEN=%s\nCLUSTER_NAME=%s\n", rancherTestsHost(haOutputs.RancherURL), adminToken, cfg.ClusterName)
	return os.WriteFile(envPath, []byte(envContent), 0o600)
}

func downstreamKubeconfigPath(instanceNum int) string {
	return downstreamOutputPathForRun(os.Getenv(runIDEnv), fmt.Sprintf("downstream-ha-%d.kubeconfig", instanceNum))
}

func writeDownstreamKubeconfig(instanceNum int, cfg downstreamProvisioningConfig, haOutputs TerraformOutputs, adminToken, managementClusterID string) (string, error) {
	kubeconfigPath := downstreamKubeconfigPath(instanceNum)
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o755); err != nil {
		return "", err
	}
	kubeconfig, err := generateRancherKubeconfig(haOutputs.RancherURL, adminToken, managementClusterID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600); err != nil {
		return "", err
	}
	log.Printf("[downstream][ha-%d] Wrote downstream kubeconfig for %s (%s)", instanceNum, cfg.ClusterName, managementClusterID)
	return kubeconfigPath, nil
}

func kubectlApply(kubeconfigPath, manifest string) error {
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply failed: %w", err)
	}
	return nil
}

func randomHex(byteCount int) string {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func dnsLabel(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if len(result) > 53 {
		result = strings.Trim(result[:53], "-")
	}
	if result == "" {
		return "downstream"
	}
	return result
}

func shortRunID(runID string) string {
	runID = strings.TrimSpace(runID)
	if len(runID) <= 8 {
		return runID
	}
	return runID[len(runID)-8:]
}

func downstreamClusterNamePrefix(explicitPrefix, runID string) string {
	if explicitPrefix = strings.TrimSpace(explicitPrefix); explicitPrefix != "" {
		return explicitPrefix
	}
	if strings.TrimSpace(runID) != "" {
		return "gha"
	}
	return "rancher-runway"
}

func downstreamProvisioningRunID() string {
	for _, environmentName := range []string{"GITHUB_RUN_ID", "SIGNOFF_RUN_ID", runIDEnv} {
		if value := strings.TrimSpace(os.Getenv(environmentName)); value != "" {
			return value
		}
	}
	return ""
}

func downstreamClusterName(prefix, runID string, haIndex int, uniqueSuffix string) string {
	tail := fmt.Sprintf("ha%d-%s", haIndex, uniqueSuffix)
	if strings.TrimSpace(runID) != "" {
		tail = shortRunID(runID) + "-" + tail
	}
	tail = dnsLabel(tail)
	prefix = dnsLabel(prefix)
	const maxClusterNameLength = 53
	availablePrefixLength := maxClusterNameLength - len(tail) - 1
	if availablePrefixLength <= 0 {
		if len(tail) > maxClusterNameLength {
			tail = strings.Trim(tail[len(tail)-maxClusterNameLength:], "-")
		}
		return tail
	}
	if len(prefix) > availablePrefixLength {
		prefix = strings.Trim(prefix[:availablePrefixLength], "-")
	}
	if prefix == "" {
		return tail
	}
	return prefix + "-" + tail
}
