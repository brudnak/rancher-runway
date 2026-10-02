package test

import (
	"fmt"
	"github.com/spf13/viper"
	"log"
	"os"
	"strings"
	"time"
)

func setupFirstServerNode(ip string, haOutputs TerraformOutputs, ingressController string, resolvedPlan *RancherResolvedPlan) error {
	maskGitHubActionsValue(ip)
	log.Printf("[setupFirstServerNode] Starting setup for IP %s", ip)
	rke2K8sVersion := viper.GetString("k8s.version")
	expectedInstallerSHA256 := viper.GetString("rke2.install_script_sha256")
	if resolvedPlan != nil {
		rke2K8sVersion = resolvedPlan.RecommendedRKE2Version
		expectedInstallerSHA256 = resolvedPlan.InstallerSHA256
	}

	log.Printf("[setupFirstServerNode] Creating config directory...")
	cmd := "sudo mkdir -p /etc/rancher/rke2"
	if err := runRemoteSetupStep("setupFirstServerNode/create-config-dir", ip, cmd); err != nil {
		log.Printf("[setupFirstServerNode] FAILED to create config directory: %v", err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	log.Printf("[setupFirstServerNode] Config directory created")

	configContent := rke2FirstServerConfigContent(haOutputs, ingressController)

	if githubActions() {
		log.Printf("[setupFirstServerNode] Creating config file with %d TLS SAN entries", len(rke2TLSSANs(haOutputs)))
	} else {
		log.Printf("[setupFirstServerNode] Creating config file with content:\n%s", configContent)
	}
	cmd = fmt.Sprintf("sudo bash -c 'cat > /etc/rancher/rke2/config.yaml << EOL\n%s\nEOL'", configContent)
	if err := runRemoteSetupStep("setupFirstServerNode/write-config", ip, cmd); err != nil {
		log.Printf("[setupFirstServerNode] FAILED to create config file: %v", err)
		return fmt.Errorf("failed to create config file: %w", err)
	}
	log.Printf("[setupFirstServerNode] Config file created")

	log.Printf("[setupFirstServerNode] Verifying config file...")
	cmd = "sudo cat /etc/rancher/rke2/config.yaml"
	output, err := RunCommand(cmd, ip)
	if err != nil {
		log.Printf("[setupFirstServerNode] WARNING: Could not read config file: %v", err)
	} else {
		if githubActions() {
			log.Printf("[setupFirstServerNode] Config file verified (%d bytes)", len(output))
		} else {
			log.Printf("[setupFirstServerNode] Config file contents:\n%s", output)
		}
	}

	preloadImages := viper.GetBool("rke2.preload_images")

	if preloadImages {
		log.Printf("[setupFirstServerNode] Pre-downloading RKE2 images to avoid Docker Hub rate limiting...")

		cmd = "sudo mkdir -p /var/lib/rancher/rke2/agent/images"
		if err := runRemoteSetupStep("setupFirstServerNode/create-images-dir", ip, cmd); err != nil {
			log.Printf("[setupFirstServerNode] FAILED to create images directory: %v", err)
			return fmt.Errorf("failed to create images directory: %w", err)
		}
		log.Printf("[setupFirstServerNode] Images directory created")

		log.Printf("[setupFirstServerNode] Downloading and validating RKE2 images (this may take a few minutes)...")
		imagesDownloadCommand, commandErr := buildRKE2ImagesDownloadCommand(rke2K8sVersion, ingressController)
		if commandErr != nil {
			return fmt.Errorf("failed to prepare RKE2 image downloads: %w", commandErr)
		}
		output, err = RunCommand(imagesDownloadCommand, ip)
		if err != nil {
			log.Printf("[setupFirstServerNode] FAILED to download/validate images: %v", err)
			return fmt.Errorf("failed to download/validate RKE2 images: %w", err)
		}
		log.Printf("[setupFirstServerNode] Images downloaded and checksum validated successfully")

		imagesMoveCommand, commandErr := buildRKE2ImagesMoveCommand(rke2K8sVersion, ingressController)
		if commandErr != nil {
			return fmt.Errorf("failed to prepare RKE2 image preload: %w", commandErr)
		}
		if err := runRemoteSetupStep("setupFirstServerNode/move-images", ip, imagesMoveCommand); err != nil {
			log.Printf("[setupFirstServerNode] FAILED to move images: %v", err)
			return fmt.Errorf("failed to move images: %w", err)
		}
		log.Printf("[setupFirstServerNode] Images pre-loaded successfully")
	} else {
		log.Printf("[setupFirstServerNode] Image pre-loading disabled, will pull from registry")
	}

	dockerUsername := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	dockerPassword := strings.TrimSpace(os.Getenv("DOCKERHUB_PASSWORD"))
	maskGitHubActionsValue(dockerUsername)
	maskGitHubActionsValue(dockerPassword)

	if dockerUsername != "" && dockerPassword != "" {
		log.Printf("[setupFirstServerNode] Configuring Docker Hub authentication...")

		registriesConfig := rke2RegistriesConfigContent(dockerUsername, dockerPassword)
		cmd = fmt.Sprintf("sudo bash -c 'cat > /etc/rancher/rke2/registries.yaml << EOL\n%s\nEOL'", registriesConfig)
		if err := runRemoteSetupStep("setupFirstServerNode/write-registries", ip, cmd); err != nil {
			log.Printf("[setupFirstServerNode] FAILED to create registries.yaml: %v", err)
			return fmt.Errorf("failed to create registries.yaml: %w", err)
		}
		log.Printf("[setupFirstServerNode] Docker Hub authentication configured")
	} else {
		log.Printf("[setupFirstServerNode] No Docker Hub credentials provided, skipping registries.yaml creation")
	}

	if err := configureRKE2IngressForExternalTLS(ip, ingressController, haOutputs.LoadBalancerSourceCIDRs); err != nil {
		return fmt.Errorf("failed to configure RKE2 ingress for external TLS: %w", err)
	}

	log.Printf("[setupFirstServerNode] Installing RKE2 version %s...", rke2K8sVersion)
	cmd, err = buildRKE2InstallCommand("server", rke2K8sVersion, expectedInstallerSHA256)
	if err != nil {
		return fmt.Errorf("failed to build RKE2 install command: %w", err)
	}
	output, err = RunCommand(cmd, ip)
	if err != nil {
		log.Printf("[setupFirstServerNode] FAILED to install RKE2: %v", err)
		log.Printf("[setupFirstServerNode] Install output: %s", output)
		return fmt.Errorf("failed to install RKE2: %w", err)
	}
	log.Printf("[setupFirstServerNode] RKE2 installed successfully. Output: %s", output)

	log.Printf("[setupFirstServerNode] Verifying RKE2 binary...")
	cmd = "which rke2 || ls -la /usr/local/bin/rke2 || echo 'RKE2 binary not found in expected locations'"
	output, err = RunCommand(cmd, ip)
	log.Printf("[setupFirstServerNode] RKE2 binary check: %s", output)

	log.Printf("[setupFirstServerNode] Enabling RKE2 server service...")
	cmd = "sudo systemctl enable rke2-server.service"
	output, err = RunCommand(cmd, ip)
	if err != nil {
		log.Printf("[setupFirstServerNode] FAILED to enable RKE2 server: %v", err)
		return fmt.Errorf("failed to enable RKE2 server: %w", err)
	}
	log.Printf("[setupFirstServerNode] RKE2 server enabled. Output: %s", output)

	log.Printf("[setupFirstServerNode] Starting RKE2 server service...")
	cmd = "sudo systemctl start rke2-server.service"
	output, err = RunCommand(cmd, ip)
	if err != nil {
		log.Printf("[setupFirstServerNode] FAILED to start RKE2 server: %v", err)
		log.Printf("[setupFirstServerNode] Gathering diagnostic information...")

		cmd = "sudo systemctl status rke2-server.service --no-pager"
		statusOutput, statusErr := RunCommand(cmd, ip)
		if statusErr == nil {
			log.Printf("[setupFirstServerNode] Service status:\n%s", redactDiagnosticOutput(statusOutput))
		} else {
			log.Printf("[setupFirstServerNode] Could not get service status: %v", statusErr)
		}

		cmd = "sudo journalctl -u rke2-server.service --no-pager -n 100"
		logsOutput, logsErr := RunCommand(cmd, ip)
		if logsErr == nil {
			log.Printf("[setupFirstServerNode] Recent logs:\n%s", redactDiagnosticOutput(logsOutput))
		} else {
			log.Printf("[setupFirstServerNode] Could not get logs: %v", logsErr)
		}

		return fmt.Errorf("failed to start RKE2 server: %w", err)
	}
	log.Printf("[setupFirstServerNode] RKE2 server start command completed. Output: %s", output)

	log.Printf("[setupFirstServerNode] Checking initial service status...")
	cmd = "sudo systemctl status rke2-server.service"
	output, _ = RunCommand(cmd, ip)
	log.Printf("[setupFirstServerNode] Service status:\n%s", redactDiagnosticOutput(output))

	log.Printf("[setupFirstServerNode] Waiting for RKE2 to initialize on %s (this may take several minutes)...", ip)
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		log.Printf("[setupFirstServerNode] Attempt %d/%d: Checking for node-token file...", i+1, maxRetries)

		cmd = "sudo test -f /var/lib/rancher/rke2/server/node-token && echo 'ready' || echo 'not-ready'"
		status, err := RunCommand(cmd, ip)
		log.Printf("[setupFirstServerNode] Node-token check result: '%s'", status)

		if err == nil && strings.TrimSpace(status) == "ready" {
			log.Printf("[setupFirstServerNode] RKE2 initialized successfully on %s", ip)

			cmd = "sudo cat /var/lib/rancher/rke2/server/node-token"
			token, tokenErr := RunCommand(cmd, ip)
			if tokenErr != nil {
				log.Printf("[setupFirstServerNode] WARNING: Token file exists but cannot read it: %v", tokenErr)
			} else {
				log.Printf("[setupFirstServerNode] Token successfully read (length: %d)", len(token))
			}

			return nil
		}

		if i%3 == 0 {
			log.Printf("[setupFirstServerNode] Checking service status (attempt %d)...", i+1)
			cmd = "sudo systemctl status rke2-server.service --no-pager"
			statusOutput, _ := RunCommand(cmd, ip)
			log.Printf("[setupFirstServerNode] Service status:\n%s", redactDiagnosticOutput(statusOutput))

			log.Printf("[setupFirstServerNode] Checking recent logs...")
			cmd = "sudo journalctl -u rke2-server.service --no-pager -n 20"
			logsOutput, _ := RunCommand(cmd, ip)
			log.Printf("[setupFirstServerNode] Recent logs:\n%s", redactDiagnosticOutput(logsOutput))
		}

		log.Printf("[setupFirstServerNode] Waiting 10 seconds before next check...")
		time.Sleep(10 * time.Second)
	}

	log.Printf("[setupFirstServerNode] TIMEOUT: Final diagnostic information:")

	cmd = "sudo systemctl status rke2-server.service --no-pager"
	output, _ = RunCommand(cmd, ip)
	log.Printf("[setupFirstServerNode] Final service status:\n%s", redactDiagnosticOutput(output))

	cmd = "sudo journalctl -u rke2-server.service --no-pager -n 50"
	output, _ = RunCommand(cmd, ip)
	log.Printf("[setupFirstServerNode] Last 50 log lines:\n%s", redactDiagnosticOutput(output))

	cmd = "sudo ls -la /var/lib/rancher/rke2/server/"
	output, _ = RunCommand(cmd, ip)
	log.Printf("[setupFirstServerNode] Contents of /var/lib/rancher/rke2/server/:\n%s", output)

	return fmt.Errorf("timeout waiting for RKE2 to initialize on %s", ip)
}
