package test

import (
	"fmt"
	"log"
	"time"
)

func getNodeToken(ip string) (string, error) {
	log.Printf("[getNodeToken] Retrieving node token from %s", ip)
	cmd := "sudo cat /var/lib/rancher/rke2/server/node-token"
	token, err := RunCommand(cmd, ip)
	if err != nil {
		log.Printf("[getNodeToken] FAILED to get node token: %v", err)
		return "", fmt.Errorf("failed to get node token: %w", err)
	}
	log.Printf("[getNodeToken] Token retrieved successfully (length: %d)", len(token))
	return token, nil
}

func runRemoteSetupStep(label, ip, cmd string) error {
	const attempts = 2
	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			log.Printf("[%s] Retrying remote setup step on %s (attempt %d/%d)", label, ip, attempt, attempts)
		}

		if _, err := RunCommandWithTimeout(cmd, ip, 90, 24); err != nil {
			lastErr = err
			log.Printf("[%s] Remote setup step failed on %s (attempt %d/%d): %v", label, ip, attempt, attempts, err)
			if attempt < attempts {
				logRemoteSetupDiagnostics(label, ip)
				time.Sleep(10 * time.Second)
			}
			continue
		}

		return nil
	}

	return lastErr
}

func logRemoteSetupDiagnostics(label, ip string) {
	cmd := "printf 'cloud-init: '; cloud-init status --long || true; printf '\\nsystem: '; systemctl is-system-running || true; printf '\\nssm: '; systemctl is-active amazon-ssm-agent snap.amazon-ssm-agent.amazon-ssm-agent 2>/dev/null || true; printf '\\ndisk:\\n'; df -h / /var || true"
	output, err := RunCommandWithTimeout(cmd, ip, 30, 12)
	if err != nil {
		log.Printf("[%s] Could not collect remote setup diagnostics from %s: %v", label, ip, err)
		return
	}
	log.Printf("[%s] Remote setup diagnostics for %s:\n%s", label, ip, redactDiagnosticOutput(output))
}

func logRemoteRKE2Diagnostics(label, ip, unit string) {
	cmd := fmt.Sprintf("sudo systemctl status %s --no-pager || true; printf '\\n--- journal ---\\n'; sudo journalctl -u %s --no-pager -n 80 || true", unit, unit)
	output, err := RunCommandWithTimeout(cmd, ip, 60, 18)
	if err != nil {
		log.Printf("[%s] Could not collect RKE2 diagnostics from %s: %v", label, ip, err)
		return
	}
	log.Printf("[%s] RKE2 diagnostics for %s:\n%s", label, ip, redactDiagnosticOutput(output))
}
