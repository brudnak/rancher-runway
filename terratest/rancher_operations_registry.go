package test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Only migrate official distribution registries. Private mirrors and custom
// server repositories are deployment policy, not inferred from an image tag.
func upgradeSystemRegistry(image, current string) (string, bool) {
	registry, repository, ok := splitRegistryRepository(image)
	if !ok {
		registry, repository = "docker.io", image
	}
	official := func(s string) bool {
		return s == "" || s == "docker.io" || s == "registry.rancher.com" || s == "stgregistry.suse.com"
	}
	if repository != "rancher/rancher" || !official(registry) || !official(current) {
		return current, false
	}

	return registry, true
}

func prepareUpgradeRegistry(plan *rancherUpgradePlan, values map[string]any) (string, bool, error) {
	current, _ := values["systemDefaultRegistry"].(string)
	env, _ := values["extraEnv"].([]any)
	for _, item := range env {
		entry, _ := item.(map[string]any)
		if entry["name"] == "CATTLE_SYSTEM_DEFAULT_REGISTRY" {
			value, ok := entry["value"].(string)
			if !ok || entry["valueFrom"] != nil {
				return "", false, fmt.Errorf("system registry comes from an external environment value; review its compatibility before upgrading")
			}
			current = strings.TrimSpace(value)
		}
	}
	target, managed := upgradeSystemRegistry(plan.Image, current)
	if plan.SystemDefaultRegistry != nil && (!managed || *plan.SystemDefaultRegistry != target) {
		return "", false, fmt.Errorf("system registry configuration changed since review; review a fresh upgrade plan")
	}
	if !managed {
		return current, false, nil
	}
	if plan.SystemDefaultRegistry == nil {
		plan.SystemDefaultRegistry = &target
	}
	return target, true, nil
}

func verifyUpgradeRegistry(ctx context.Context, plan *rancherUpgradePlan, command rancherUpgradeCommand) error {
	if plan.SystemDefaultRegistry == nil {
		return nil
	}
	raw, err := command(ctx, nil, "kubectl", "--kubeconfig", plan.kubeconfig, "--request-timeout=10s", "get", "settings.management.cattle.io", "system-default-registry", "-o", "json")
	if err != nil {
		return fmt.Errorf("cannot verify the post-upgrade system image registry")
	}
	var setting struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &setting) != nil || setting.Value != *plan.SystemDefaultRegistry {
		return fmt.Errorf("Rancher's system image registry did not converge to the reviewed source %s", *plan.SystemDefaultRegistry)
	}
	return nil
}
