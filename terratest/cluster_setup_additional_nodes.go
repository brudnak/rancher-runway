package test

import (
	"fmt"
	"github.com/spf13/viper"
	"log"
	"os"
	"strings"
	"time"
)

func setupAdditionalServerNode(ip, token string, haOutputs TerraformOutputs, ingressController string, resolvedPlan *RancherResolvedPlan) error {
	rke2K8sVersion := viper.GetString("k8s.version")
	expectedInstallerSHA256 := viper.GetString("rke2.install_script_sha256")
	if resolvedPlan != nil {
		rke2K8sVersion = resolvedPlan.RecommendedRKE2Version
		expectedInstallerSHA256 = resolvedPlan.InstallerSHA256
	}
	var err error

	cmd := "sudo mkdir -p /etc/rancher/rke2"
	if err := runRemoteSetupStep("setupAdditionalServerNode/create-config-dir", ip, cmd); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	firstServerIP := haOutputs.Server1IP
	if len(haOutputs.ServerIPs) > 0 {
		firstServerIP = haOutputs.ServerIPs[0]
	}
	configContent := rke2AdditionalServerConfigContent(firstServerIP, token, haOutputs, ingressController)

	cmd = fmt.Sprintf("sudo bash -c 'cat > /etc/rancher/rke2/config.yaml << EOL\n%s\nEOL'", configContent)
	if err := runRemoteSetupStep("setupAdditionalServerNode/write-config", ip, cmd); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	preloadImages := viper.GetBool("rke2.preload_images")

	if preloadImages {
		log.Printf("[setupAdditionalServerNode] Pre-downloading RKE2 images for %s...", ip)

		cmd = "sudo mkdir -p /var/lib/rancher/rke2/agent/images"
		if err := runRemoteSetupStep("setupAdditionalServerNode/create-images-dir", ip, cmd); err != nil {
			log.Printf("[setupAdditionalServerNode] FAILED to create images directory: %v", err)
			return fmt.Errorf("failed to create images directory: %w", err)
		}

		log.Printf("[setupAdditionalServerNode] Downloading and validating RKE2 images for %s...", ip)
		imagesDownloadCommand, commandErr := buildRKE2ImagesDownloadCommand(rke2K8sVersion, ingressController)
		if commandErr != nil {
			return fmt.Errorf("failed to prepare RKE2 image downloads: %w", commandErr)
		}
		_, err = RunCommand(imagesDownloadCommand, ip)
		if err != nil {
			log.Printf("[setupAdditionalServerNode] FAILED to download/validate images: %v", err)
			return fmt.Errorf("failed to download/validate RKE2 images: %w", err)
		}

		imagesMoveCommand, commandErr := buildRKE2ImagesMoveCommand(rke2K8sVersion, ingressController)
		if commandErr != nil {
			return fmt.Errorf("failed to prepare RKE2 image preload: %w", commandErr)
		}
		if err := runRemoteSetupStep("setupAdditionalServerNode/move-images", ip, imagesMoveCommand); err != nil {
			log.Printf("[setupAdditionalServerNode] FAILED to move images: %v", err)
			return fmt.Errorf("failed to move images: %w", err)
		}
		log.Printf("[setupAdditionalServerNode] Images pre-loaded and validated successfully for %s", ip)
	}

	dockerUsername := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	dockerPassword := strings.TrimSpace(os.Getenv("DOCKERHUB_PASSWORD"))

	if dockerUsername != "" && dockerPassword != "" {
		log.Printf("[setupAdditionalServerNode] Configuring Docker Hub authentication for %s...", ip)

		registriesConfig := rke2RegistriesConfigContent(dockerUsername, dockerPassword)
		cmd = fmt.Sprintf("sudo bash -c 'cat > /etc/rancher/rke2/registries.yaml << EOL\n%s\nEOL'", registriesConfig)
		if err := runRemoteSetupStep("setupAdditionalServerNode/write-registries", ip, cmd); err != nil {
			log.Printf("[setupAdditionalServerNode] FAILED to create registries.yaml: %v", err)
			return fmt.Errorf("failed to create registries.yaml: %w", err)
		}
		log.Printf("[setupAdditionalServerNode] Docker Hub authentication configured for %s", ip)
	} else {
		log.Printf("[setupAdditionalServerNode] No Docker Hub credentials provided, skipping registries.yaml creation for %s", ip)
	}

	if err := configureRKE2IngressForExternalTLS(ip, ingressController, haOutputs.LoadBalancerSourceCIDRs); err != nil {
		return fmt.Errorf("failed to configure RKE2 ingress for external TLS: %w", err)
	}

	log.Printf("[setupAdditionalServerNode] Installing RKE2 version %s on %s...", rke2K8sVersion, ip)
	cmd, err = buildRKE2InstallCommand("server", rke2K8sVersion, expectedInstallerSHA256)
	if err != nil {
		return fmt.Errorf("failed to build RKE2 install command: %w", err)
	}
	_, err = RunCommand(cmd, ip)
	if err != nil {
		return fmt.Errorf("failed to install RKE2: %w", err)
	}

	cmd = "sudo systemctl enable rke2-server.service"
	_, err = RunCommand(cmd, ip)
	if err != nil {
		return fmt.Errorf("failed to enable RKE2 server: %w", err)
	}

	cmd = "sudo systemctl start rke2-server.service"
	_, err = RunCommand(cmd, ip)
	if err != nil {
		logRemoteRKE2Diagnostics("setupAdditionalServerNode/start-rke2", ip, "rke2-server.service")
		return fmt.Errorf("failed to start RKE2 server: %w", err)
	}

	log.Printf("Waiting for RKE2 to initialize on %s (this may take several minutes)...", ip)
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		cmd = "sudo systemctl is-active --quiet rke2-server && echo 'active' || echo 'inactive'"
		status, err := RunCommand(cmd, ip)
		if err == nil && strings.TrimSpace(status) == "active" {
			log.Printf("RKE2 initialized successfully on %s", ip)
			return nil
		}

		time.Sleep(10 * time.Second)
	}

	logRemoteRKE2Diagnostics("setupAdditionalServerNode/wait-rke2", ip, "rke2-server.service")
	return fmt.Errorf("timeout waiting for RKE2 to initialize on %s", ip)
}

func setupGPUWorkerNode(ip, token string, haOutputs TerraformOutputs, ingressController string, resolvedPlan *RancherResolvedPlan) error {
	rke2K8sVersion := viper.GetString("k8s.version")
	expectedInstallerSHA256 := viper.GetString("rke2.install_script_sha256")
	if resolvedPlan != nil {
		rke2K8sVersion = resolvedPlan.RecommendedRKE2Version
		expectedInstallerSHA256 = resolvedPlan.InstallerSHA256
	}

	cmd := "sudo mkdir -p /etc/rancher/rke2"
	if err := runRemoteSetupStep("setupGPUWorkerNode/create-config-dir", ip, cmd); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	firstServerIP := haOutputs.Server1IP
	if len(haOutputs.ServerIPs) > 0 {
		firstServerIP = haOutputs.ServerIPs[0]
	}
	instanceType := strings.TrimSpace(haOutputs.GPUWorkerInstanceType)
	if instanceType == "" {
		instanceType = "unknown"
	}
	configContent := fmt.Sprintf(`server: https://%s:9345
token: %s
node-label:
  - "ha-rancher-rke2/gpu-worker=true"
  - "ha-rancher-rke2/rancher-ai=true"
  - "ha-rancher-rke2/gpu-instance-type=%s"`,
		firstServerIP,
		token,
		instanceType)

	cmd = fmt.Sprintf("sudo bash -c 'cat > /etc/rancher/rke2/config.yaml << EOL\n%s\nEOL'", configContent)
	if err := runRemoteSetupStep("setupGPUWorkerNode/write-config", ip, cmd); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	if viper.GetBool("rke2.preload_images") {
		log.Printf("[setupGPUWorkerNode] Pre-downloading RKE2 images for %s...", ip)

		cmd = "sudo mkdir -p /var/lib/rancher/rke2/agent/images"
		if err := runRemoteSetupStep("setupGPUWorkerNode/create-images-dir", ip, cmd); err != nil {
			return fmt.Errorf("failed to create images directory: %w", err)
		}

		imagesDownloadCommand, commandErr := buildRKE2ImagesDownloadCommand(rke2K8sVersion, ingressController)
		if commandErr != nil {
			return fmt.Errorf("failed to prepare RKE2 image downloads: %w", commandErr)
		}
		if _, err := RunCommand(imagesDownloadCommand, ip); err != nil {
			return fmt.Errorf("failed to download/validate RKE2 images: %w", err)
		}

		imagesMoveCommand, commandErr := buildRKE2ImagesMoveCommand(rke2K8sVersion, ingressController)
		if commandErr != nil {
			return fmt.Errorf("failed to prepare RKE2 image preload: %w", commandErr)
		}
		if err := runRemoteSetupStep("setupGPUWorkerNode/move-images", ip, imagesMoveCommand); err != nil {
			return fmt.Errorf("failed to move images: %w", err)
		}
	}

	dockerUsername := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	dockerPassword := strings.TrimSpace(os.Getenv("DOCKERHUB_PASSWORD"))
	if dockerUsername != "" && dockerPassword != "" {
		maskGitHubActionsValue(dockerUsername)
		maskGitHubActionsValue(dockerPassword)
		registriesConfig := rke2RegistriesConfigContent(dockerUsername, dockerPassword)
		cmd = fmt.Sprintf("sudo bash -c 'cat > /etc/rancher/rke2/registries.yaml << EOL\n%s\nEOL'", registriesConfig)
		if err := runRemoteSetupStep("setupGPUWorkerNode/write-registries", ip, cmd); err != nil {
			return fmt.Errorf("failed to create registries.yaml: %w", err)
		}
	}

	log.Printf("[setupGPUWorkerNode] Installing RKE2 agent version %s on %s...", rke2K8sVersion, ip)
	cmd, err := buildRKE2InstallCommand("agent", rke2K8sVersion, expectedInstallerSHA256)
	if err != nil {
		return fmt.Errorf("failed to build RKE2 install command: %w", err)
	}
	if _, err := RunCommand(cmd, ip); err != nil {
		return fmt.Errorf("failed to install RKE2 agent: %w", err)
	}

	cmd = "sudo systemctl enable rke2-agent.service"
	if _, err := RunCommand(cmd, ip); err != nil {
		return fmt.Errorf("failed to enable RKE2 agent: %w", err)
	}

	cmd = "sudo systemctl start rke2-agent.service"
	if _, err := RunCommand(cmd, ip); err != nil {
		return fmt.Errorf("failed to start RKE2 agent: %w", err)
	}

	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		cmd = "sudo systemctl is-active --quiet rke2-agent && echo 'active' || echo 'inactive'"
		status, err := RunCommand(cmd, ip)
		if err == nil && strings.TrimSpace(status) == "active" {
			log.Printf("[setupGPUWorkerNode] RKE2 agent is active on GPU worker %s", ip)
			return nil
		}
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("timeout waiting for RKE2 agent to initialize on GPU worker %s", ip)
}
