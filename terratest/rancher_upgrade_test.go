package test

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
)

const (
	rancherUpgradeHeartbeatInterval   = 2 * time.Minute
	rancherUpgradeDiagnosticTimeout   = 10 * time.Second
	rancherUpgradeDiagnosticLineLimit = 200
)

type rancherUpgradeDiagnostic struct {
	title    string
	maxLines int
	name     string
	args     []string
}

func TestHAUpgradeRancher(t *testing.T) {
	requireExplicitLifecycleTest(t, "TestHAUpgradeRancher")
	setupConfig(t)

	upgradeVersion := normalizeVersionInput(os.Getenv("RANCHER_UPGRADE_VERSION"))
	if upgradeVersion == "" {
		t.Skip("RANCHER_UPGRADE_VERSION is not set; skipping Rancher upgrade")
	}
	if err := validateInstalledRancherHelmVersion(); err != nil {
		t.Fatalf("Rancher upgrade tooling preflight failed: %v", err)
	}

	totalHAs := viper.GetInt("total_has")
	if totalHAs < 1 {
		t.Fatal("total_has must be at least 1")
	}

	if totalHAs == 1 {
		viper.Set("rancher.version", upgradeVersion)
		viper.Set("rancher.versions", []string{})
	} else {
		upgradeVersions := make([]string, totalHAs)
		for i := range upgradeVersions {
			upgradeVersions[i] = upgradeVersion
		}
		viper.Set("rancher.versions", upgradeVersions)
	}
	upgradePlans, err := resolveAutoRancherPlans(totalHAs)
	if err != nil {
		t.Fatalf("failed to resolve Rancher upgrade plan for %s: %v", upgradeVersion, err)
	}
	for i, plan := range upgradePlans {
		if err := writeRancherResolutionArtifact("upgrade", i+1, plan); err != nil {
			t.Fatalf("failed to write Rancher upgrade resolution artifact: %v", err)
		}
	}

	terraformOptions := getTerraformOptions(t, totalHAs)
	outputs := getTerraformOutputs(t, terraformOptions)
	if len(outputs) == 0 {
		t.Fatal("No outputs received from terraform")
	}

	var wg sync.WaitGroup
	errCh := make(chan error, totalHAs)
	for i := 1; i <= totalHAs; i++ {
		instanceNum := i
		plan := upgradePlans[i-1]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := upgradeHAInstanceRancher(instanceNum, outputs, plan); err != nil {
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
		t.Fatalf("Rancher upgrade failed:\n%s", strings.Join(failures, "\n"))
	}

	timeout := durationFromEnv("RANCHER_UPGRADE_READY_TIMEOUT", durationFromEnv("RANCHER_READY_TIMEOUT", 30*time.Minute))
	initialDelay := durationFromEnv("RANCHER_UPGRADE_READY_INITIAL_DELAY", 45*time.Second)
	settleDelay := durationFromEnv("RANCHER_UPGRADE_READY_SETTLE_DELAY", durationFromEnv("RANCHER_READY_SETTLE_DELAY", 60*time.Second))

	errCh = make(chan error, totalHAs)
	for i := 1; i <= totalHAs; i++ {
		instanceNum := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := waitForHAReady(instanceNum, outputs, timeout, initialDelay, settleDelay); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	failures = failures[:0]
	for err := range errCh {
		failures = append(failures, err.Error())
	}
	if len(failures) > 0 {
		t.Fatalf("Rancher upgrade readiness failed:\n%s", strings.Join(failures, "\n"))
	}
}

func upgradeHAInstanceRancher(instanceNum int, outputs map[string]string, plan *RancherResolvedPlan) error {
	haOutputs := getHAOutputs(instanceNum, outputs)
	haDir := haInstanceDir(instanceNum)
	absHADir, err := absoluteFromWorkingDir(haDir)
	if err != nil {
		return err
	}

	absKubeConfigPath := filepath.Join(absHADir, "kube_config.yaml")
	if _, err := os.Stat(absKubeConfigPath); err != nil {
		return fmt.Errorf("kubeconfig not available for HA %d at %s: %w", instanceNum, absKubeConfigPath, err)
	}

	helmCommand := buildAutoHelmCommand(
		rancherHelmOperationUpgrade,
		plan.ChartRepoAlias,
		plan.ChartVersion,
		viper.GetString("rancher.bootstrap_password"),
		plan.RancherImage,
		plan.RancherImageTag,
		plan.AgentImage,
		plan.UseRancherImageFields,
	)
	helmCommand = rancherHelmCommandForHA(helmCommand, haOutputs.RancherURL)

	log.Printf("[upgrade][ha-%d] Upgrading Rancher at %s to requested version %s using %s/rancher@%s",
		instanceNum, clickableURL(haOutputs.RancherURL), plan.RequestedVersion, plan.ChartRepoAlias, plan.ChartVersion)
	for _, explanation := range plan.Explanation {
		log.Printf("[upgrade][ha-%d] %s", instanceNum, explanation)
	}

	cmd := exec.Command("bash", "-lc", helmCommand)
	cmd.Dir = absHADir
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", absKubeConfigPath))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := runRancherUpgradeCommandWithHeartbeat(cmd, instanceNum, rancherUpgradeHeartbeatInterval); err != nil {
		logRancherUpgradeFailureDiagnostics(instanceNum, absHADir, absKubeConfigPath)
		return fmt.Errorf("failed to run Rancher upgrade for HA %d: %w", instanceNum, err)
	}

	log.Printf("[upgrade][ha-%d] Helm upgrade completed; waiting for Rancher readiness next", instanceNum)
	return nil
}

func runRancherUpgradeCommandWithHeartbeat(cmd *exec.Cmd, instanceNum int, interval time.Duration) error {
	if interval <= 0 {
		return cmd.Run()
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	startedAt := time.Now()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			log.Printf("[upgrade][ha-%d] Helm upgrade is still running (%s elapsed)", instanceNum, time.Since(startedAt).Round(time.Second))
		}
	}
}

func rancherUpgradeFailureDiagnostics() []rancherUpgradeDiagnostic {
	return []rancherUpgradeDiagnostic{
		{
			title:    "Helm client version",
			maxLines: 20,
			name:     "helm",
			args:     []string{"version", "--short"},
		},
		{
			title:    "Rancher Helm release history",
			maxLines: 80,
			name:     "helm",
			args:     []string{"history", "rancher", "--namespace", "cattle-system"},
		},
		{
			title:    "Rancher Helm release status",
			maxLines: rancherUpgradeDiagnosticLineLimit,
			name:     "helm",
			args:     []string{"status", "rancher", "--namespace", "cattle-system", "--show-desc"},
		},
		{
			title:    "pre-upgrade hook job",
			maxLines: 80,
			name:     "kubectl",
			args:     []string{"get", "job", "rancher-pre-upgrade", "--namespace", "cattle-system", "--output", "wide"},
		},
		{
			title:    "pre-upgrade hook pods",
			maxLines: 80,
			name:     "kubectl",
			args:     []string{"get", "pods", "--namespace", "cattle-system", "--selector", "job-name=rancher-pre-upgrade", "--output", "wide"},
		},
		{
			title:    "pre-upgrade hook job description",
			maxLines: rancherUpgradeDiagnosticLineLimit,
			name:     "kubectl",
			args:     []string{"describe", "job", "rancher-pre-upgrade", "--namespace", "cattle-system"},
		},
		{
			title:    "pre-upgrade hook pod descriptions",
			maxLines: rancherUpgradeDiagnosticLineLimit,
			name:     "kubectl",
			args:     []string{"describe", "pods", "--namespace", "cattle-system", "--selector", "job-name=rancher-pre-upgrade"},
		},
		{
			title:    "pre-upgrade hook logs",
			maxLines: rancherUpgradeDiagnosticLineLimit,
			name:     "kubectl",
			args:     []string{"logs", "--selector", "job-name=rancher-pre-upgrade", "--namespace", "cattle-system", "--all-containers", "--prefix", "--tail", "200", "--max-log-requests", "10"},
		},
		{
			title:    "recent cattle-system events",
			maxLines: 120,
			name:     "kubectl",
			args:     []string{"get", "events", "--namespace", "cattle-system", "--sort-by=.lastTimestamp"},
		},
	}
}

func logRancherUpgradeFailureDiagnostics(instanceNum int, workingDir, kubeconfigPath string) {
	log.Printf("[upgrade][ha-%d][diagnostics] collecting bounded pre-upgrade hook diagnostics", instanceNum)
	for _, diagnostic := range rancherUpgradeFailureDiagnostics() {
		ctx, cancel := context.WithTimeout(context.Background(), rancherUpgradeDiagnosticTimeout)
		cmd := exec.CommandContext(ctx, diagnostic.name, diagnostic.args...)
		cmd.Dir = workingDir
		cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
		outputBytes, err := cmd.CombinedOutput()
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()

		output := formatRancherUpgradeDiagnosticOutput(string(outputBytes), diagnostic.maxLines)
		if timedOut {
			log.Printf("[upgrade][ha-%d][diagnostics] %s timed out after %s", instanceNum, diagnostic.title, rancherUpgradeDiagnosticTimeout)
		} else if err != nil {
			log.Printf("[upgrade][ha-%d][diagnostics] %s failed: %v", instanceNum, diagnostic.title, err)
		}
		log.Printf("[upgrade][ha-%d][diagnostics] %s:\n%s", instanceNum, diagnostic.title, output)
	}
}

func formatRancherUpgradeDiagnosticOutput(output string, maxLines int) string {
	output = sanitizeDiagnosticOutput(output)
	output = lastNonEmptyLines(output, maxLines)
	if output == "" {
		return "(no output)"
	}
	return output
}
