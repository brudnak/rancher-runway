package test

import (
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/spf13/viper"
	"log"
	"os/exec"
	"strings"
	"time"
)

func buildAutoHelmCommands(totalHAs int, operation, chartRepoAlias, chartVersion, bootstrapPassword, rancherImage, rancherImageTag, agentImage string, useRancherImageFields bool) []string {
	command := buildAutoHelmCommand(operation, chartRepoAlias, chartVersion, bootstrapPassword, rancherImage, rancherImageTag, agentImage, useRancherImageFields)
	commands := make([]string, totalHAs)
	for i := 0; i < totalHAs; i++ {
		commands[i] = command
	}
	return commands
}

func buildAutoHelmCommand(operation, chartRepoAlias, chartVersion, bootstrapPassword, rancherImage, rancherImageTag, agentImage string, useRancherImageFields bool) string {
	operation = strings.ToLower(strings.TrimSpace(operation))
	if operation == "" {
		operation = rancherHelmOperationInstall
	}
	helmImages := normalizeHelmImageSettings(chartRepoAlias, rancherImage, rancherImageTag, agentImage, useRancherImageFields)

	var baseSettings []string
	switch operation {
	case rancherHelmOperationInstall:
		baseSettings = []string{
			"helm install rancher " + chartRepoAlias + "/rancher \\",
		}
	case rancherHelmOperationUpgrade:
		baseSettings = []string{
			"helm upgrade rancher " + chartRepoAlias + "/rancher \\",
			"  --install \\",
		}
	default:
		panic(fmt.Sprintf("unsupported Rancher Helm operation %q", operation))
	}

	baseSettings = append(baseSettings, []string{
		"  --namespace cattle-system \\",
		"  --version " + chartVersion + " \\",
		"  --set hostname=placeholder \\",
		"  --set-string " + shellQuoteHelmSetString("bootstrapPassword", bootstrapPassword) + " \\",
		"  --set tls=external \\",
		"  --set global.cattle.psp.enabled=false \\",
		"  --set agentTLSMode=system-store",
	}...)

	if helmImages.clearSystemDefaultRegistry {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set systemDefaultRegistry= \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.imageRegistry != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set image.registry=" + helmImages.imageRegistry + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.imageRepository != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set image.repository=" + helmImages.imageRepository + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.imageTag != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set image.tag=" + helmImages.imageTag + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.rancherImage != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set rancherImage=" + helmImages.rancherImage + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.rancherImageTag != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set rancherImageTag=" + helmImages.rancherImageTag + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.agentImage != "" {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set 'extraEnv[0].name=CATTLE_AGENT_IMAGE' \\",
			"  --set 'extraEnv[0].value=" + helmImages.agentImage + "' \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if helmImages.runtimeSystemDefaultRegistry != "" {
		// A blank chart value omits CATTLE_SYSTEM_DEFAULT_REGISTRY, which leaves
		// Rancher's previous database value intact on upgrade. Override it at
		// runtime without changing the chart's hook or audit image registries.
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set 'extraEnv[1].name=CATTLE_SYSTEM_DEFAULT_REGISTRY' \\",
			"  --set-string " + shellQuoteHelmSetString("extraEnv[1].value", helmImages.runtimeSystemDefaultRegistry) + " \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if operation == rancherHelmOperationUpgrade {
		// Keep the hook image registry and tag paired as published by the chart.
		// Optimus charts can use shell prereleases that do not exist in the
		// production registry, so overriding only the registry breaks the hook.
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --wait \\",
			"  --wait-for-jobs \\",
			"  --timeout 30m \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}
	if viper.GetInt("rke2.server_count") == 1 {
		baseSettings = append(baseSettings[:len(baseSettings)-1], append([]string{
			"  --set replicas=1 \\",
		}, baseSettings[len(baseSettings)-1:]...)...)
	}

	return strings.Join(baseSettings, "\n")
}

func shellQuoteHelmSetString(key, value string) string {
	return shellQuote(key + "=" + escapeHelmSetValue(value))
}

func escapeHelmSetValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `,`, `\,`)
	return value
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

type rancherWebhookOverrideValues struct {
	Global rancherWebhookGlobalValues `json:"global"`
	Image  rancherWebhookImageValues  `json:"image"`
}

type rancherWebhookGlobalValues struct {
	Cattle rancherWebhookCattleValues `json:"cattle"`
}

type rancherWebhookCattleValues struct {
	SystemDefaultRegistry string `json:"systemDefaultRegistry"`
}

type rancherWebhookImageValues struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

func rancherHelmCommandWithWebhookImage(command, image string) (string, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return command, nil
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("Rancher Helm command must not be empty")
	}

	payload, err := rancherWebhookValuesJSON(image)
	if err != nil {
		return "", err
	}

	// Rancher's chart forwards this scalar only to its managed webhook chart.
	// This overrides an inherited Prime registry without redirecting Rancher or
	// any other system image.
	return strings.TrimSpace(command) + " \\\n  --set-literal " + shellQuote("webhook="+payload), nil
}

func rancherHelmCommandsWithWebhookImage(commands []string, image string) ([]string, error) {
	configured := make([]string, len(commands))
	for i, command := range commands {
		var err error
		configured[i], err = rancherHelmCommandWithWebhookImage(command, image)
		if err != nil {
			return nil, fmt.Errorf("Helm command %d: %w", i+1, err)
		}
	}
	return configured, nil
}

func rancherWebhookValuesJSON(image string) (string, error) {
	registry, repository, tag, err := imagelookup.ParseRegistryImage(image)
	if err != nil {
		return "", fmt.Errorf("parse Rancher webhook image %q: %w", strings.TrimSpace(image), err)
	}
	values := rancherWebhookOverrideValues{
		Global: rancherWebhookGlobalValues{
			Cattle: rancherWebhookCattleValues{
				SystemDefaultRegistry: registry,
			},
		},
		Image: rancherWebhookImageValues{
			Repository: repository,
			Tag:        tag,
		},
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode Rancher webhook Helm values: %w", err)
	}
	return string(payload), nil
}

type helmImageSettings struct {
	clearSystemDefaultRegistry   bool
	runtimeSystemDefaultRegistry string
	rancherImage                 string
	rancherImageTag              string
	imageRegistry                string
	imageRepository              string
	imageTag                     string
	agentImage                   string
}

func normalizeHelmImageSettings(chartRepoAlias, rancherImage, rancherImageTag, agentImage string, useRancherImageFields bool) helmImageSettings {
	settings := helmImageSettings{
		agentImage: strings.TrimSpace(agentImage),
	}
	rancherImage = strings.TrimSpace(rancherImage)
	rancherImageTag = strings.TrimSpace(rancherImageTag)

	if useRancherImageFields {
		settings.imageTag = rancherImageTag
		if imageRegistry, imageRepository, ok := splitRegistryRepository(rancherImage); ok {
			settings.imageRegistry = imageRegistry
			settings.imageRepository = imageRepository
		} else {
			settings.imageRepository = rancherImage
		}
	} else {
		settings.rancherImage = rancherImage
		settings.rancherImageTag = rancherImageTag
	}

	agentRegistry, agentRepository, agentOK := splitRegistryRepository(settings.agentImage)
	// Prime fallback charts default to the production registry. Clear the chart
	// value when using an agent from another registry, preserving existing hook
	// image behavior. Optimus charts continue to use their published defaults.
	if chartRepoAlias == "rancher-prime" && agentOK && agentRegistry != "registry.rancher.com" {
		settings.clearSystemDefaultRegistry = true
		serverRegistry, _, _ := splitRegistryRepository(rancherImage)
		if serverRegistry == "stgregistry.suse.com" && agentRegistry == serverRegistry {
			// The staging build's system images also live in staging. Set its
			// runtime registry explicitly to replace a persisted Prime registry,
			// and let Rancher prefix the relative agent image exactly once.
			// A custom agent-only registry need not contain other system images.
			settings.runtimeSystemDefaultRegistry = serverRegistry
			settings.agentImage = agentRepository
		}
	}
	return settings
}

func chartSupportsRancherImageFields(chartRepoAlias, chartVersion string) (bool, error) {
	output, err := exec.Command("helm", "show", "values", chartRepoAlias+"/rancher", "--version", chartVersion).Output()
	if err != nil {
		return false, fmt.Errorf("helm show values failed: %w", err)
	}
	return valuesSupportTopLevelRancherImageFields(string(output)), nil
}

func requireResolvedRancherChartImageFieldSupport(chartRepoAlias, chartVersion string) (bool, error) {
	supported, err := chartSupportsRancherImageFields(chartRepoAlias, chartVersion)
	if err != nil {
		return false, fmt.Errorf("inspect resolved Rancher chart %s/rancher@%s before use: %w", chartRepoAlias, chartVersion, err)
	}
	return supported, nil
}

func resolveChartImageFieldSupportWithCache(cache map[string]bool, chartRepoAlias, chartVersion string) (bool, error) {
	cacheKey := chartRepoAlias + "/rancher@" + chartVersion
	if cached, ok := cache[cacheKey]; ok {
		log.Printf("[resolver] Reusing Rancher chart values inspection for %s", cacheKey)
		return cached, nil
	}

	startedAt := time.Now()
	log.Printf("[resolver] Inspecting Rancher chart values for %s...", cacheKey)
	supported, err := requireResolvedRancherChartImageFieldSupport(chartRepoAlias, chartVersion)
	if err != nil {
		log.Printf("[resolver] Rancher chart values inspection for %s failed after %s: %v", cacheKey, time.Since(startedAt).Round(time.Millisecond), err)
		return false, err
	}
	cache[cacheKey] = supported
	log.Printf("[resolver] Rancher chart values inspection for %s completed in %s", cacheKey, time.Since(startedAt).Round(time.Millisecond))
	return supported, nil
}

func valuesSupportTopLevelRancherImageFields(values string) bool {
	inImageBlock := false
	hasRepository := false
	hasTag := false

	for _, line := range strings.Split(values, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			if inImageBlock {
				return hasRepository && hasTag
			}
			if trimmed == "image:" {
				inImageBlock = true
			}
			continue
		}
		if !inImageBlock {
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "repository:"):
			hasRepository = true
		case strings.HasPrefix(trimmed, "tag:"):
			hasTag = true
		}
	}

	return inImageBlock && hasRepository && hasTag
}

func splitRegistryRepository(image string) (string, string, bool) {
	image = strings.TrimSpace(image)
	registry, repository, ok := strings.Cut(image, "/")
	if !ok || registry == "" || repository == "" {
		return "", "", false
	}
	if !strings.Contains(registry, ".") && !strings.Contains(registry, ":") && registry != "localhost" {
		return "", "", false
	}
	return registry, repository, true
}
