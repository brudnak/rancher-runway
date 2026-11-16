package test

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// Cluster connectivity can precede Rancher's installation of downstream system
// charts. Wait for the deployment before attempting to replace its image.
func waitForWebhookDeployment(ctx context.Context, kubeconfigPath, namespace string, interval time.Duration, logPrefix string) (webhookDeploymentTarget, error) {
	if interval <= 0 {
		return webhookDeploymentTarget{}, fmt.Errorf("webhook discovery interval must be positive")
	}
	start := time.Now()
	var lastErr error = fmt.Errorf("discovery has not run")
	attempt := 0
	for ctx.Err() == nil {
		attempt++
		target, err := discoverWebhookDeploymentContext(ctx, kubeconfigPath, namespace)
		if err == nil && ctx.Err() == nil {
			return target, nil
		}
		if err == nil {
			err = ctx.Err()
		}
		lastErr = err
		log.Printf("%s Discovery attempt %d after %s: %s", logPrefix, attempt, time.Since(start).Round(time.Second),
			redactDiagnosticOutput(sanitizeKubeDiagnosticOutput(fmt.Sprint(err))))
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return webhookDeploymentTarget{}, fmt.Errorf("waiting for rancher-webhook deployment stopped after %d attempts: %w; last discovery error: %v", attempt, ctx.Err(), lastErr)
}

// Bound both the API request and the subprocess, including inherited output
// pipes. kubectl warnings stay off stdout so they cannot corrupt deployment JSON.
func runWebhookDiscoveryKubectlOutput(ctx context.Context, kubeconfigPath string, args ...string) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	commandArgs := append([]string{"--kubeconfig", kubeconfigPath, "--request-timeout=20s"}, args...)
	cmd := exec.CommandContext(requestCtx, "kubectl", commandArgs...)
	cmd.WaitDelay = time.Second
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	detail := redactDiagnosticOutput(sanitizeKubeDiagnosticOutput(stderr.String()))
	detail = lastNonEmptyLines(detail, 40)
	if err != nil {
		if requestCtx.Err() != nil {
			err = requestCtx.Err()
		}
		return "", fmt.Errorf("kubectl %s failed: %w (%s)", strings.Join(args, " "), err, detail)
	}
	if detail != "" {
		log.Printf("[webhook][kubectl warning] %s", detail)
	}
	return string(output), nil
}

func logWebhookDiscoveryDiagnostics(kubeconfigPath, logPrefix string) {
	logWebhookDiagnostics(kubeconfigPath, logPrefix, [][]string{
		{"get", "deployments,pods,jobs", "-A", "-o", "wide"},
		{"get", "apps.catalog.cattle.io", "-A"},
		{"get", "events", "-A", "--sort-by=.lastTimestamp"},
	})
}

func logWebhookRolloutDiagnostics(kubeconfigPath, logPrefix string, target webhookDeploymentTarget) {
	deploy := "deployment/" + target.DeploymentName
	// The old ReplicaSet can still be serving. Ask for every pod of this
	// deployment, including the last terminated instance of the webhook.
	logs := []string{"logs", deploy, "-n", target.Namespace, "-c", target.ContainerName, "--all-pods=true", "--ignore-errors=true", "--tail=40", "--limit-bytes=8192"}
	logWebhookDiagnostics(kubeconfigPath, logPrefix, [][]string{
		{"get", deploy, "-n", target.Namespace, "-o", "custom-columns=IMAGES:.spec.template.spec.containers[*].image,SECRET-VOLUMES:.spec.template.spec.volumes[*].secret.secretName,MOUNT-PATHS:.spec.template.spec.containers[*].volumeMounts[*].mountPath,CONDITIONS:.status.conditions[*].reason"},
		append(logs, "--previous"),
		logs,
	})
}

func logWebhookDiagnostics(kubeconfigPath, logPrefix string, commands [][]string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, args := range commands {
		if ctx.Err() != nil {
			break
		}
		output, err := runWebhookDiscoveryKubectlOutput(ctx, kubeconfigPath, args...)
		if err != nil {
			log.Printf("%s [diagnostics] %v", logPrefix, err)
			continue
		}
		output = redactDiagnosticOutput(sanitizeKubeDiagnosticOutput(output))
		output = lastNonEmptyLines(output, 120)
		if output == "" {
			output = "(no output)"
		}
		log.Printf("%s [diagnostics] %s:\n%s", logPrefix, strings.Join(args, " "), output)
	}
}
