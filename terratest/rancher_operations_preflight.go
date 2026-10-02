package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type rancherUpgradeCommand func(context.Context, []byte, string, ...string) ([]byte, error)

// Review and execution use the same chart, preserved values and image overrides.
// Temporary material never becomes part of the browser response or saved plan.
func prepareRancherUpgrade(ctx context.Context, plan *rancherUpgradePlan, command rancherUpgradeCommand) ([]string, func(), error) {
	dir, err := os.MkdirTemp("", "runway-upgrade-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	ok := false
	defer func() {
		if !ok {
			cleanup()
		}
	}()
	chart := filepath.Join(dir, "rancher.tgz")
	if err = os.WriteFile(chart, plan.archive, 0600); err != nil {
		return nil, nil, err
	}
	// Preserve existing environment overrides while replacing any old agent image.
	valuesRaw, err := command(ctx, nil, "helm", "--kubeconfig", plan.kubeconfig, "get", "values", "rancher", "-n", "cattle-system", "--all", "-o", "json")
	if err != nil {
		return nil, nil, err
	}
	var values map[string]any
	if err = json.Unmarshal(valuesRaw, &values); err != nil {
		return nil, nil, err
	}
	registry, manageRegistry, err := prepareUpgradeRegistry(plan, values)
	if err != nil {
		return nil, nil, err
	}
	env, _ := values["extraEnv"].([]any)
	next := []any{}
	for _, item := range env {
		entry, ok := item.(map[string]any)
		if !ok || (entry["name"] != "CATTLE_AGENT_IMAGE" && (!manageRegistry || entry["name"] != "CATTLE_SYSTEM_DEFAULT_REGISTRY")) {
			next = append(next, item)
		}
	}
	agentImage := plan.AgentImage
	if manageRegistry {
		agentImage = strings.TrimPrefix(agentImage, registry+"/")
	}
	next = append(next, map[string]any{"name": "CATTLE_AGENT_IMAGE", "value": agentImage})
	if manageRegistry {
		// Runtime override also replaces Rancher's persisted setting. Keep chart hook
		// and audit image defaults paired with their published chart versions.
		next = append(next, map[string]any{"name": "CATTLE_SYSTEM_DEFAULT_REGISTRY", "value": registry})
	}
	overrideValues := map[string]any{"extraEnv": next}
	if manageRegistry {
		// Avoid duplicate CATTLE_SYSTEM_DEFAULT_REGISTRY entries in the rendered pod.
		overrideValues["systemDefaultRegistry"] = ""
		if plan.imageFields {
			chartRegistry, _ := values["systemDefaultRegistry"].(string)
			if chartRegistry == "" {
				chartRegistry = "docker.io"
			}
			for _, key := range []string{"preUpgrade", "postDelete", "auditLog"} {
				section, _ := values[key].(map[string]any)
				image, _ := section["image"].(map[string]any)
				imageRegistry, _ := image["registry"].(string)
				if imageRegistry == "" {
					imageRegistry = chartRegistry
				}
				overrideValues[key] = map[string]any{"image": map[string]any{"registry": imageRegistry}}
			}
		}
	}
	overrides, _ := json.Marshal(overrideValues)
	valuesFile := filepath.Join(dir, "overrides.json")
	if err = os.WriteFile(valuesFile, overrides, 0600); err != nil {
		return nil, nil, err
	}
	args := []string{"--kubeconfig", plan.kubeconfig, "upgrade", "rancher", chart, "-n", "cattle-system", "--reuse-values", "-f", valuesFile}
	if plan.imageFields {
		registry, repository, ok := splitRegistryRepository(plan.Image)
		if !ok {
			registry, repository = "docker.io", plan.Image
		}
		// --reuse-values may retain deprecated keys, which take precedence over
		// image.* in transitional charts. Clear them so the reviewed image wins.
		for _, setting := range []string{"rancherImage=", "rancherImageTag=", "image.registry=" + registry, "image.repository=" + repository, "image.tag=" + plan.ImageTag} {
			args = append(args, "--set-string", setting)
		}
	} else {
		args = append(args, "--set-string", "rancherImage="+plan.Image, "--set-string", "rancherImageTag="+plan.ImageTag)
	}
	args = append(args, "--timeout", "30m")
	ok = true
	return args, cleanup, nil
}

func preflightRancherUpgrade(ctx context.Context, plan *rancherUpgradePlan, command rancherUpgradeCommand) error {
	args, cleanup, err := prepareRancherUpgrade(ctx, plan, command)
	if err != nil {
		return fmt.Errorf("read upgrade configuration: %w", err)
	}
	defer cleanup()
	// Helm renders with cluster access but does not persist the upgrade.
	// This does not certify Rancher data migrations or downstream compatibility.
	if _, err = command(ctx, nil, "helm", append(args, "--dry-run=server")...); err != nil {
		return fmt.Errorf("Helm preflight failed; choose a compatible chart or repair the cluster before retrying: %w", err)
	}
	return nil
}
