package test

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	rancherConfigNamespace                    = "cattle-system"
	rancherConfigName                         = "rancher-config"
	rancherWebhookConfigDataKey               = "rancher-webhook"
	downstreamWebhookConfigMaxAttempts        = 24
	downstreamWebhookConfigRetryInterval      = 5 * time.Second
	downstreamWebhookConfigConvergenceTimeout = 2 * time.Minute
	downstreamWebhookKubectlRequestTimeout    = 15 * time.Second
)

type downstreamWebhookKubectl struct {
	output func(string, ...string) (string, error)
	direct func(string, ...string) error
	sleep  func(time.Duration)
}

func configureDownstreamRancherWebhookImage(webhookImage string) error {
	webhookImage = strings.TrimSpace(webhookImage)
	if webhookImage == "" {
		return nil
	}

	valuesJSON, err := rancherWebhookValuesJSON(webhookImage)
	if err != nil {
		return err
	}
	records, err := readDownstreamOutputRecords()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		log.Printf("[upgrade][webhook] No downstream cluster records found; skipping downstream rancher-config update")
		return nil
	}

	runner := downstreamWebhookKubectl{
		output: runDownstreamWebhookKubectlOutput,
		direct: runDownstreamWebhookKubectlDirect,
		sleep:  time.Sleep,
	}
	var failures []string
	for _, record := range records {
		kubeconfigPath, err := ensureDownstreamKubeconfig(record)
		if err == nil {
			err = configureDownstreamRancherWebhookConfig(kubeconfigPath, valuesJSON, runner)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("HA %d downstream cluster %s: %v", record.HAIndex, record.ClusterName, err))
			continue
		}
		log.Printf("[upgrade][ha-%d][downstream:%s] Persisted webhook candidate %s in %s/%s data key %s",
			record.HAIndex, record.ClusterName, webhookImage, rancherConfigNamespace, rancherConfigName, rancherWebhookConfigDataKey)
	}
	if len(failures) > 0 {
		return fmt.Errorf("failed to configure downstream Rancher webhook values:\n%s", strings.Join(failures, "\n"))
	}
	return nil
}

func configureDownstreamRancherWebhookConfig(kubeconfigPath, valuesJSON string, runner downstreamWebhookKubectl) error {
	if strings.TrimSpace(kubeconfigPath) == "" {
		return fmt.Errorf("downstream kubeconfig path must not be empty")
	}
	if !json.Valid([]byte(valuesJSON)) {
		return fmt.Errorf("Rancher webhook values must be valid JSON")
	}
	if runner.output == nil || runner.direct == nil {
		return fmt.Errorf("downstream kubectl runner is incomplete")
	}

	patch, err := rancherWebhookConfigMergePatch(valuesJSON)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(downstreamWebhookConfigConvergenceTimeout)
	attempts := 0
	var lastErr error
	for attempt := 1; attempt <= downstreamWebhookConfigMaxAttempts; attempt++ {
		attempts = attempt
		exists, currentValue, inspectErr := inspectDownstreamRancherWebhookConfig(kubeconfigPath, runner)
		if inspectErr != nil {
			lastErr = fmt.Errorf("inspect %s/%s: %w", rancherConfigNamespace, rancherConfigName, inspectErr)
		} else if exists && currentValue == valuesJSON {
			return nil
		} else {
			operation := "create"
			var mutationErr error
			if exists {
				operation = "patch"
				mutationErr = patchDownstreamRancherWebhookConfig(kubeconfigPath, patch, runner)
			} else {
				mutationErr = createDownstreamRancherWebhookConfig(kubeconfigPath, valuesJSON, runner)
			}

			// Always read the value back, even when the mutation failed. Rancher
			// can delete a ConfigMap between get and patch, or create one between
			// get and create. In either case another actor may already have put the
			// desired value in place, and the read-back is the source of truth.
			readBackExists, readBackValue, readBackErr := inspectDownstreamRancherWebhookConfig(kubeconfigPath, runner)
			switch {
			case readBackErr != nil:
				lastErr = downstreamWebhookConfigAttemptError(operation, mutationErr,
					fmt.Errorf("read back %s/%s: %w", rancherConfigNamespace, rancherConfigName, readBackErr))
			case readBackExists && readBackValue == valuesJSON:
				return nil
			case !readBackExists:
				lastErr = downstreamWebhookConfigAttemptError(operation, mutationErr,
					fmt.Errorf("read-back found %s/%s absent", rancherConfigNamespace, rancherConfigName))
			default:
				lastErr = downstreamWebhookConfigAttemptError(operation, mutationErr,
					fmt.Errorf("read-back key %s did not contain the requested webhook values", rancherWebhookConfigDataKey))
			}
		}

		if attempt < downstreamWebhookConfigMaxAttempts && time.Now().Before(deadline) {
			retryInterval := downstreamWebhookConfigRetryInterval
			if remaining := time.Until(deadline); remaining < retryInterval {
				retryInterval = remaining
			}
			if retryInterval <= 0 {
				break
			}
			log.Printf("[upgrade][webhook] %s/%s did not converge on attempt %d/%d: %v; retrying in %s",
				rancherConfigNamespace, rancherConfigName, attempt, downstreamWebhookConfigMaxAttempts,
				lastErr, retryInterval)
			if runner.sleep != nil {
				runner.sleep(retryInterval)
			}
			continue
		}
		break
	}

	return fmt.Errorf("%s/%s webhook values did not converge after %d attempts within %s: %w",
		rancherConfigNamespace, rancherConfigName, attempts, downstreamWebhookConfigConvergenceTimeout, lastErr)
}

func inspectDownstreamRancherWebhookConfig(kubeconfigPath string, runner downstreamWebhookKubectl) (bool, string, error) {
	output, err := runner.output(
		kubeconfigPath,
		"get", "configmap", rancherConfigName,
		"-n", rancherConfigNamespace,
		"--ignore-not-found",
		"-o", "json",
	)
	if err != nil {
		return false, "", err
	}
	if strings.TrimSpace(output) == "" {
		return false, "", nil
	}

	var configMap struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &configMap); err != nil {
		return false, "", fmt.Errorf("decode %s/%s: %w", rancherConfigNamespace, rancherConfigName, err)
	}
	return true, configMap.Data[rancherWebhookConfigDataKey], nil
}

func createDownstreamRancherWebhookConfig(kubeconfigPath, valuesJSON string, runner downstreamWebhookKubectl) error {
	if err := runner.direct(
		kubeconfigPath,
		"create", "configmap", rancherConfigName,
		"-n", rancherConfigNamespace,
		"--from-literal="+rancherWebhookConfigDataKey+"="+valuesJSON,
	); err != nil {
		return fmt.Errorf("create %s/%s: %w", rancherConfigNamespace, rancherConfigName, err)
	}
	return nil
}

func patchDownstreamRancherWebhookConfig(kubeconfigPath, patch string, runner downstreamWebhookKubectl) error {
	if err := runner.direct(
		kubeconfigPath,
		"patch", "configmap", rancherConfigName,
		"-n", rancherConfigNamespace,
		"--type=merge",
		"--patch", patch,
	); err != nil {
		return fmt.Errorf("patch %s/%s: %w", rancherConfigNamespace, rancherConfigName, err)
	}
	return nil
}

func downstreamWebhookConfigAttemptError(operation string, mutationErr, readBackErr error) error {
	if mutationErr == nil {
		return fmt.Errorf("%s completed but configuration did not converge: %w", operation, readBackErr)
	}
	return fmt.Errorf("%s failed: %v; configuration did not converge: %w", operation, mutationErr, readBackErr)
}

// These narrow kubectl runners keep stderr diagnostics separate from stdout so
// warnings cannot turn an absent --ignore-not-found response into invalid JSON.
// The request timeout also prevents one API call from consuming the full
// ConfigMap convergence window.
func runDownstreamWebhookKubectlOutput(kubeconfigPath string, args ...string) (string, error) {
	commandArgs := downstreamWebhookKubectlCommandArgs(kubeconfigPath, args...)
	cmd := exec.Command("kubectl", commandArgs...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("kubectl %s failed: %w (%s)", strings.Join(args, " "), err, detail)
		}
		return "", fmt.Errorf("kubectl %s failed: %w", strings.Join(args, " "), err)
	}
	if warning := strings.TrimSpace(stderr.String()); warning != "" {
		log.Printf("[upgrade][webhook][kubectl warning] %s", warning)
	}
	return string(output), nil
}

func runDownstreamWebhookKubectlDirect(kubeconfigPath string, args ...string) error {
	commandArgs := downstreamWebhookKubectlCommandArgs(kubeconfigPath, args...)
	cmd := exec.Command("kubectl", commandArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func downstreamWebhookKubectlCommandArgs(kubeconfigPath string, args ...string) []string {
	commandArgs := []string{
		"--kubeconfig", kubeconfigPath,
		"--request-timeout=" + downstreamWebhookKubectlRequestTimeout.String(),
	}
	return append(commandArgs, args...)
}

func rancherWebhookConfigMergePatch(valuesJSON string) (string, error) {
	if !json.Valid([]byte(valuesJSON)) {
		return "", fmt.Errorf("Rancher webhook values must be valid JSON")
	}
	patch, err := json.Marshal(struct {
		Data map[string]string `json:"data"`
	}{
		Data: map[string]string{rancherWebhookConfigDataKey: valuesJSON},
	})
	if err != nil {
		return "", fmt.Errorf("encode Rancher webhook ConfigMap patch: %w", err)
	}
	return string(patch), nil
}
