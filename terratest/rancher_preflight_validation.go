package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"gopkg.in/yaml.v3"
	"net/http"
	"regexp"
	"strings"
)

func preflightEditorAuthorized(r *http.Request, token string) bool {
	if strings.TrimSpace(r.URL.Query().Get("token")) == token {
		return true
	}
	return requestFromLoopback(r) && sameOriginBrowserRequest(r)
}

func normalizePreflightVersions(versions []string) ([]string, error) {
	if len(versions) == 0 {
		return nil, fmt.Errorf("at least one Rancher version is required")
	}

	normalized := make([]string, 0, len(versions))
	for i, version := range versions {
		normalizedVersion := imagelookup.NormalizeVersionInput(version)
		if normalizedVersion == "" {
			return nil, fmt.Errorf("version for HA %d cannot be empty", i+1)
		}
		normalized = append(normalized, normalizedVersion)
	}

	return normalized, nil
}

func normalizePreflightAgentImages(agentImages []string, total int) ([]string, error) {
	if len(agentImages) == 0 {
		return make([]string, total), nil
	}
	if len(agentImages) != total {
		return nil, fmt.Errorf("agent image overrides must contain %d entries", total)
	}
	normalized := make([]string, total)
	for i, image := range agentImages {
		normalized[i] = strings.TrimSpace(image)
		if normalized[i] != "" {
			if err := validateCustomAgentImage(normalized[i]); err != nil {
				return nil, fmt.Errorf("agent image for HA %d: %w", i+1, err)
			}
		}
	}
	return normalized, nil
}

func hasNonEmptyString(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

var installerSHA256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func normalizeManualPreflight(update settings.PreflightConfigUpdate) ([]string, []string, []string, error) {
	helmCommands := nonEmptyStringSlice(update.HelmCommands)
	if len(helmCommands) == 0 {
		return nil, nil, nil, fmt.Errorf("at least one manual Helm command is required")
	}
	for i, command := range helmCommands {
		if err := validateManualHelmCommandStructure(command); err != nil {
			return nil, nil, nil, fmt.Errorf("helm command for HA %d is invalid: %w", i+1, err)
		}
		normalizedCommand, err := manualHelmCommandForServerLayout(command, update.ServerCount)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("helm command for HA %d is invalid for the selected server layout: %w", i+1, err)
		}
		helmCommands[i] = normalizedCommand
	}
	if err := validateRancherHelmCommandsUseExternalTLS(helmCommands); err != nil {
		return nil, nil, nil, err
	}

	k8sVersions, err := normalizeManualK8SVersions(update.K8SVersions, len(helmCommands))
	if err != nil {
		return nil, nil, nil, err
	}
	installerSHA256s, err := normalizeManualInstallerSHA256s(update, k8sVersions)
	if err != nil {
		return nil, nil, nil, err
	}
	return helmCommands, k8sVersions, installerSHA256s, nil
}

func manualHelmCommandForServerLayout(command string, serverCount int) (string, error) {
	if settings.NormalizeRKE2ServerCount(serverCount) != 1 {
		return command, nil
	}

	if replicas, ok := helmCommandSetValue(command, "replicas"); ok {
		if strings.Trim(replicas, `"'`) != "1" {
			return "", fmt.Errorf("single-server layout needs Rancher replicas=1; change the replicas override or choose a 3/5 server layout")
		}
		return command, nil
	}

	return strings.TrimSpace(command) + " \\\n  --set replicas=1", nil
}

func normalizeManualK8SVersions(versions []string, totalHAs int) ([]string, error) {
	if len(versions) != totalHAs {
		return nil, fmt.Errorf("k8s.versions must contain %d version(s)", totalHAs)
	}
	normalized := make([]string, 0, len(versions))
	for i, version := range versions {
		normalizedVersion, err := normalizeRKE2VersionInput(version)
		if err != nil {
			return nil, fmt.Errorf("RKE2 version for HA %d is invalid: %w", i+1, err)
		}
		normalized = append(normalized, normalizedVersion)
	}
	return normalized, nil
}

func normalizeManualInstallerSHA256s(update settings.PreflightConfigUpdate, k8sVersions []string) ([]string, error) {
	if update.ResolveInstallerSHA {
		resolved := make([]string, 0, len(k8sVersions))
		cache := map[string]string{}
		for _, version := range k8sVersions {
			if checksum := cache[version]; checksum != "" {
				resolved = append(resolved, checksum)
				continue
			}
			checksum, err := resolveInstallerSHA256(version)
			if err != nil {
				return nil, fmt.Errorf("resolve RKE2 installer SHA256 for %s: %w", version, err)
			}
			cache[version] = checksum
			resolved = append(resolved, checksum)
		}
		return resolved, nil
	}

	if len(update.InstallerSHA256s) != len(k8sVersions) {
		return nil, fmt.Errorf("installer SHA256 values must contain %d checksum(s)", len(k8sVersions))
	}
	normalized := make([]string, 0, len(update.InstallerSHA256s))
	for i, checksum := range update.InstallerSHA256s {
		checksum = strings.ToLower(strings.TrimSpace(checksum))
		if !installerSHA256Pattern.MatchString(checksum) {
			return nil, fmt.Errorf("installer SHA256 for HA %d must be a 64-character hex checksum", i+1)
		}
		normalized = append(normalized, checksum)
	}
	return normalized, nil
}

func ensureMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if value := mappingValue(mapping, key); value != nil {
		if value.Kind != yaml.MappingNode {
			value.Kind = yaml.MappingNode
			value.Tag = "!!map"
			value.Style = 0
			value.Content = nil
		}
		return value
	}

	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapping.Content = append(mapping.Content, keyNode, valueNode)
	return valueNode
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}

	return nil
}

func deleteMappingKey(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}
