package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupHAInstance(t *testing.T, instanceNum int, outputs map[string]string, resolvedPlan *RancherResolvedPlan) error {
	maskGitHubActionsValue(viper.GetString("rancher.bootstrap_password"))

	haDir := haInstanceDir(instanceNum)
	haOutputs := getHAOutputs(instanceNum, outputs)
	if err := settings.ValidateRKE2IngressControllerConfig(); err != nil {
		return err
	}
	ingressController := settings.CurrentRKE2IngressController()
	rke2K8sVersion := viper.GetString("k8s.version")
	if resolvedPlan != nil {
		rke2K8sVersion = resolvedPlan.RecommendedRKE2Version
	}
	if err := validateRKE2IngressControllerVersion(rke2K8sVersion, ingressController); err != nil {
		return fmt.Errorf("incompatible RKE2 ingress configuration: %w", err)
	}
	if _, _, err := rke2IngressConfigManifest(ingressController, haOutputs.LoadBalancerSourceCIDRs); err != nil {
		return fmt.Errorf("invalid RKE2 ingress configuration: %w", err)
	}
	ingressDaemonSetName, err := rke2IngressDaemonSetName(ingressController)
	if err != nil {
		return err
	}
	serverIPs := haOutputs.ServerIPs
	if len(serverIPs) == 0 {
		return fmt.Errorf("no RKE2 server IPs found for HA %d", instanceNum)
	}

	ips := append([]string{}, haOutputs.ServerIPs...)
	ips = append(ips, haOutputs.ServerPrivateIPs...)
	ips = append(ips, haOutputs.GPUWorkerIP, haOutputs.GPUWorkerPrivateIP)
	for _, ip := range ips {
		if strings.TrimSpace(ip) == "" {
			continue
		}
		if CheckIPAddress(ip) != "valid" {
			return fmt.Errorf("invalid IP address: %s", ip)
		}
	}

	absHADir, err := absoluteFromWorkingDir(haDir)
	if err != nil {
		return err
	}

	if _, err := os.Stat(absHADir); os.IsNotExist(err) {
		if mkdirErr := os.MkdirAll(absHADir, 0o700); mkdirErr != nil {
			return fmt.Errorf("failed to create directory %s: %w", absHADir, mkdirErr)
		}
		log.Printf("Created directory %s", absHADir)
	}

	helmCommand, err := resolvedRancherInstallCommand(instanceNum, resolvedPlan)
	if err != nil {
		return err
	}
	helmCommand = rancherHelmCommandForHA(helmCommand, haOutputs.RancherURL)

	CreateInstallScript(helmCommand, haDir, ingressDaemonSetName)

	log.Printf("Setting up first server node with IP %s", serverIPs[0])
	err = setupFirstServerNode(serverIPs[0], haOutputs, ingressController, resolvedPlan)
	if err != nil {
		return fmt.Errorf("failed to setup first server node: %w", err)
	}

	token, err := getNodeToken(serverIPs[0])
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	var wg sync.WaitGroup
	var setupErr error
	var setupErrMutex sync.Mutex

	for i, ip := range serverIPs[1:] {
		wg.Add(1)
		nodeNum := i + 2

		go func(ip string, nodeNum int) {
			defer wg.Done()

			log.Printf("Setting up server node %d with IP %s", nodeNum, ip)
			err := setupAdditionalServerNode(ip, token, haOutputs, ingressController, resolvedPlan)
			if err != nil {
				setupErrMutex.Lock()
				setupErr = fmt.Errorf("failed to setup server node %d: %w", nodeNum, err)
				setupErrMutex.Unlock()
			}
		}(ip, nodeNum)
	}

	if strings.TrimSpace(haOutputs.GPUWorkerIP) != "" {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()

			log.Printf("Setting up GPU worker node with IP %s", ip)
			if err := setupGPUWorkerNode(ip, token, haOutputs, ingressController, resolvedPlan); err != nil {
				setupErrMutex.Lock()
				setupErr = fmt.Errorf("failed to setup GPU worker node: %w", err)
				setupErrMutex.Unlock()
			}
		}(haOutputs.GPUWorkerIP)
	}

	wg.Wait()

	if setupErr != nil {
		return fmt.Errorf("node setup error: %w", setupErr)
	}

	log.Printf("Waiting for cluster to fully initialize...")
	time.Sleep(30 * time.Second)

	err = getAndSaveKubeconfig(serverIPs[0], haDir)
	if err != nil {
		t.Logf("Warning: Failed to save kubeconfig: %v", err)
	}

	installScriptPath := filepath.Join(haDir, "install.sh")
	log.Printf("Executing install script at %s", installScriptPath)

	absHADirForScript, dirErr := absoluteFromWorkingDir(haDir)
	if dirErr != nil {
		return dirErr
	}

	if _, err := os.Stat(absHADirForScript); os.IsNotExist(err) {
		if mkdirErr := os.MkdirAll(absHADirForScript, 0o700); mkdirErr != nil {
			return fmt.Errorf("failed to create directory %s: %w", absHADirForScript, mkdirErr)
		}
		log.Printf("Created directory %s", absHADirForScript)
	}

	absInstallScriptPath := filepath.Join(absHADirForScript, "install.sh")
	absKubeConfigPath := filepath.Join(absHADirForScript, "kube_config.yaml")

	cmd := exec.Command(absInstallScriptPath)
	cmd.Dir = absHADirForScript
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", absKubeConfigPath))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if execErr := cmd.Run(); execErr != nil {
		return fmt.Errorf("failed to execute install script: %w", execErr)
	}

	log.Printf("Install script executed successfully")
	log.Printf("HA %d setup complete", instanceNum)
	if githubActions() {
		log.Printf("HA %d LB: configured", instanceNum)
		log.Printf("HA %d Rancher URL: configured", instanceNum)
	} else {
		log.Printf("HA %d LB: %s", instanceNum, haOutputs.LoadBalancerDNS)
		log.Printf("HA %d Rancher URL: %s", instanceNum, clickableURL(haOutputs.RancherURL))
	}

	return nil
}
