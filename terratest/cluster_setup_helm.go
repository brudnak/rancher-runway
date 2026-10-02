package test

import (
	"fmt"
	"github.com/spf13/viper"
	"strings"
)

func resolvedRancherInstallCommand(instanceNum int, resolvedPlan *RancherResolvedPlan) (string, error) {
	if instanceNum < 1 {
		return "", fmt.Errorf("HA instance number must be at least 1, got %d", instanceNum)
	}
	if resolvedPlan != nil {
		if len(resolvedPlan.HelmCommands) != 1 {
			return "", fmt.Errorf("resolved Rancher plan for HA %d has %d Helm commands; expected exactly one", instanceNum, len(resolvedPlan.HelmCommands))
		}
		command := strings.TrimSpace(resolvedPlan.HelmCommands[0])
		if command == "" {
			return "", fmt.Errorf("resolved Rancher plan for HA %d has an empty Helm command", instanceNum)
		}
		return command, nil
	}

	// Preserve the legacy/manual setup path when a resolved plan is unavailable.
	helmCommands := viper.GetStringSlice("rancher.helm_commands")
	if instanceNum > len(helmCommands) {
		return "", fmt.Errorf("rancher.helm_commands has %d entries; cannot select HA %d", len(helmCommands), instanceNum)
	}
	command := strings.TrimSpace(helmCommands[instanceNum-1])
	if command == "" {
		return "", fmt.Errorf("rancher.helm_commands[%d] is empty", instanceNum-1)
	}
	return command, nil
}

func rancherHelmCommandForHA(helmCommand, rancherURL string) string {
	helmCommand = rancherHelmCommandWithHostname(helmCommand, rancherURL)
	if viper.GetInt("rke2.server_count") == 1 && !helmCommandSetsValue(helmCommand, "replicas") {
		helmCommand = strings.TrimSpace(helmCommand) + " \\\n  --set replicas=1"
	}
	return helmCommand
}

func rancherHelmCommandWithHostname(helmCommand, rancherURL string) string {
	if strings.Contains(helmCommand, "--set hostname=") {
		return strings.Replace(
			helmCommand,
			"--set hostname="+strings.Split(strings.Split(helmCommand, "--set hostname=")[1], " ")[0],
			"--set hostname="+rancherURL,
			1,
		)
	}
	return strings.TrimSpace(helmCommand) + fmt.Sprintf(" \\\n  --set hostname=%s", rancherURL)
}

func helmCommandSetsValue(command, key string) bool {
	_, ok := helmCommandSetValue(command, key)
	return ok
}

func helmCommandSetValue(command, key string) (string, bool) {
	fields, err := parseHelmCommandFields(command)
	if err != nil {
		return "", strings.Contains(command, key+"=")
	}
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		switch {
		case field == "--set" || field == "--set-string" || field == "--set-json":
			if i+1 < len(fields) {
				if value, ok := helmSetValueForKey(fields[i+1], key); ok {
					return value, true
				}
			}
			i++
		case strings.HasPrefix(field, "--set="):
			if value, ok := helmSetValueForKey(strings.TrimPrefix(field, "--set="), key); ok {
				return value, true
			}
		case strings.HasPrefix(field, "--set-string="):
			if value, ok := helmSetValueForKey(strings.TrimPrefix(field, "--set-string="), key); ok {
				return value, true
			}
		case strings.HasPrefix(field, "--set-json="):
			if value, ok := helmSetValueForKey(strings.TrimPrefix(field, "--set-json="), key); ok {
				return value, true
			}
		}
	}
	return "", false
}

func helmSetValueContainsKey(value, key string) bool {
	_, ok := helmSetValueForKey(value, key)
	return ok
}

func helmSetValueForKey(value, key string) (string, bool) {
	for _, part := range strings.Split(value, ",") {
		name, rawValue, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(name) == key {
			return strings.TrimSpace(rawValue), true
		}
	}
	return "", false
}
