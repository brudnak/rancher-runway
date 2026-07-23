package test

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

const (
	rancherConfigNamespace      = "cattle-system"
	rancherConfigName           = "rancher-config"
	rancherWebhookConfigDataKey = "rancher-webhook"
)

type downstreamWebhookKubectl struct {
	output func(string, ...string) (string, error)
	direct func(string, ...string) error
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
		output: runKubectlOutput,
		direct: runKubectlDirect,
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

	existing, err := runner.output(
		kubeconfigPath,
		"get", "configmap", rancherConfigName,
		"-n", rancherConfigNamespace,
		"--ignore-not-found",
		"-o", "name",
	)
	if err != nil {
		return fmt.Errorf("inspect %s/%s: %w", rancherConfigNamespace, rancherConfigName, err)
	}

	if strings.TrimSpace(existing) == "" {
		createErr := runner.direct(
			kubeconfigPath,
			"create", "configmap", rancherConfigName,
			"-n", rancherConfigNamespace,
			"--from-literal="+rancherWebhookConfigDataKey+"="+valuesJSON,
		)
		if createErr == nil {
			return nil
		}

		// Another controller or test may create rancher-config between the get
		// and create calls. A merge patch makes that race harmless while also
		// preserving every unrelated data key in the newly created ConfigMap.
		patchErr := patchDownstreamRancherWebhookConfig(kubeconfigPath, valuesJSON, runner)
		if patchErr == nil {
			return nil
		}
		return fmt.Errorf("create %s/%s failed: %v; merge patch after possible create race failed: %w",
			rancherConfigNamespace, rancherConfigName, createErr, patchErr)
	}

	return patchDownstreamRancherWebhookConfig(kubeconfigPath, valuesJSON, runner)
}

func patchDownstreamRancherWebhookConfig(kubeconfigPath, valuesJSON string, runner downstreamWebhookKubectl) error {
	patch, err := rancherWebhookConfigMergePatch(valuesJSON)
	if err != nil {
		return err
	}
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
