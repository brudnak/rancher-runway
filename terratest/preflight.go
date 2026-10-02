package test

import (
	"context"
	"fmt"
	goversion "github.com/hashicorp/go-version"
	"log"
	"os/exec"
	"strings"
	"time"
)

var (
	rke2TraefikMinimumVersion = goversion.Must(goversion.NewVersion("1.30.3"))
	rke2TraefikDefaultVersion = goversion.Must(goversion.NewVersion("1.36.0"))
	rke2NginxRemovalVersion   = goversion.Must(goversion.NewVersion("1.37.0"))
)

func validateLocalToolingPreflight(helmCommands []string) error {
	if _, err := configuredSSMReadyTimeout(); err != nil {
		return err
	}
	log.Printf("[preflight] Validating local tooling before provisioning...")

	requiredCommands := []string{"kubectl", "helm", "terraform"}
	for _, commandName := range requiredCommands {
		if _, err := exec.LookPath(commandName); err != nil {
			return fmt.Errorf("%s is required locally but was not found in PATH", commandName)
		}
	}
	if err := validateInstalledRancherHelmVersion(); err != nil {
		return err
	}

	helmRepoAliases := helmRepoAliasesFromCommands(helmCommands)
	if err := ensureRancherHelmRepos(helmRepoAliases, true); err != nil {
		return err
	}

	if err := refreshHelmRepoIndexes(); err != nil {
		return err
	}

	helmRepoOutput, err := exec.Command("helm", "repo", "list").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run 'helm repo list': %w", err)
	}

	missingHelmRepos := findMissingHelmRepos(string(helmRepoOutput), helmCommands)
	if len(missingHelmRepos) > 0 {
		return fmt.Errorf("missing required Helm repos locally: %s", strings.Join(missingHelmRepos, ", "))
	}

	log.Printf("[preflight] Local tooling validated successfully")
	return nil
}

func validateInstalledRancherHelmVersion() error {
	output, err := exec.Command("helm", "version", "--template", "{{.Version}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to determine the installed Helm version: %w", err)
	}
	return validateRancherHelmVersion(string(output))
}

func validateRancherHelmVersion(raw string) error {
	raw = strings.TrimSpace(raw)
	parsed, err := goversion.NewVersion(strings.TrimPrefix(raw, "v"))
	if err != nil || raw == "" {
		return fmt.Errorf("Rancher installs and upgrades require Helm 3; could not parse installed version %q", raw)
	}
	if parsed.Segments()[0] != 3 {
		return fmt.Errorf("Rancher installs and upgrades require Helm 3; found %s", raw)
	}
	return nil
}

func validateRancherHelmCommandsUseExternalTLS(helmCommands []string) error {
	for i, helmCommand := range helmCommands {
		if rancherHelmCommandUsesExternalTLS(helmCommand) {
			continue
		}
		return fmt.Errorf("rancher.helm_commands[%d] must include --set tls=external because this AWS setup terminates public TLS at the ALB and forwards HTTP/80 to Rancher", i)
	}
	return nil
}

type manualHelmValidationResult struct {
	Index   int    `json:"index"`
	OK      bool   `json:"ok"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

func validateManualHelmCommandsForPlanning(helmCommands []string, k8sVersions []string) []manualHelmValidationResult {
	results := make([]manualHelmValidationResult, 0, len(helmCommands))
	if _, err := exec.LookPath("helm"); err != nil {
		for i := range helmCommands {
			results = append(results, manualHelmValidationResult{
				Index:   i,
				OK:      false,
				Summary: "helm was not found in PATH",
				Detail:  err.Error(),
			})
		}
		return results
	}

	repoAliases := helmRepoAliasesFromCommands(helmCommands)
	if err := ensureRancherHelmRepos(repoAliases, true); err != nil {
		for i := range helmCommands {
			results = append(results, manualHelmValidationResult{
				Index:   i,
				OK:      false,
				Summary: "Helm repo setup failed",
				Detail:  err.Error(),
			})
		}
		return results
	}
	if err := refreshHelmRepoIndexes(); err != nil {
		for i := range helmCommands {
			results = append(results, manualHelmValidationResult{
				Index:   i,
				OK:      false,
				Summary: "Helm repo update failed",
				Detail:  err.Error(),
			})
		}
		return results
	}

	for i, helmCommand := range helmCommands {
		result := manualHelmValidationResult{Index: i}
		if strings.TrimSpace(helmCommand) == "" {
			result.Summary = "Helm command is empty"
			results = append(results, result)
			continue
		}
		if err := validateManualHelmCommandStructure(helmCommand); err != nil {
			result.Summary = "Helm command could not be parsed"
			result.Detail = err.Error()
			results = append(results, result)
			continue
		}
		if !rancherHelmCommandUsesExternalTLS(helmCommand) {
			result.Summary = "Missing external TLS setting"
			result.Detail = "Add --set tls=external so the AWS load balancer can terminate public TLS."
			results = append(results, result)
			continue
		}
		if strings.TrimSpace(manualHelmKubeVersionForIndex(k8sVersions, i)) == "" {
			result.Summary = "Invalid RKE2 version"
			if i >= len(k8sVersions) {
				result.Detail = fmt.Sprintf("Add an RKE2 version for HA %d before validating Helm compatibility.", i+1)
			} else if _, err := normalizeRKE2VersionInput(k8sVersions[i]); err != nil {
				result.Detail = err.Error()
			} else {
				result.Detail = "Add a valid RKE2 version before validating Helm compatibility."
			}
			results = append(results, result)
			continue
		}
		output, err := renderManualHelmCommandTemplate(helmCommand, manualHelmKubeVersionForIndex(k8sVersions, i))
		if err != nil {
			result.Summary = "Helm template render failed"
			result.Detail = output
			if result.Detail == "" {
				result.Detail = err.Error()
			}
			results = append(results, result)
			continue
		}
		result.OK = true
		result.Summary = "Helm command rendered successfully"
		result.Detail = output
		results = append(results, result)
	}
	return results
}

func manualHelmKubeVersionForIndex(k8sVersions []string, index int) string {
	if index < 0 || index >= len(k8sVersions) {
		return ""
	}
	version, err := normalizeRKE2VersionInput(k8sVersions[index])
	if err != nil {
		return ""
	}
	return helmKubeVersionFromRKE2Version(version)
}

func validateManualHelmCommandStructure(helmCommand string) error {
	fields, err := parseHelmCommandFields(helmCommand)
	if err != nil {
		return err
	}
	invocation, err := manualHelmInvocationFromFields(fields)
	if err != nil {
		return err
	}
	if invocation.releaseName == "" {
		return fmt.Errorf("release name is required")
	}
	if invocation.chartRef == "" {
		return fmt.Errorf("chart reference is required")
	}
	if !strings.HasSuffix(invocation.chartRef, "/rancher") {
		return fmt.Errorf("chart reference must point to a Rancher chart such as rancher-latest/rancher")
	}
	return nil
}

type manualHelmInvocation struct {
	operation    string
	releaseName  string
	chartRef     string
	trailingArgs []string
}

func manualHelmInvocationFromFields(fields []string) (manualHelmInvocation, error) {
	if len(fields) < 2 {
		return manualHelmInvocation{}, fmt.Errorf("command must start with helm install or helm upgrade")
	}
	if fields[0] != "helm" {
		return manualHelmInvocation{}, fmt.Errorf("command must start with helm")
	}
	operation := fields[1]
	if operation != "install" && operation != "upgrade" {
		return manualHelmInvocation{}, fmt.Errorf("command must use helm install or helm upgrade")
	}

	positionals := make([]string, 0, 2)
	trailingArgs := make([]string, 0, len(fields))
	for i := 2; i < len(fields); i++ {
		field := fields[i]
		if isShellControlField(field) {
			return manualHelmInvocation{}, fmt.Errorf("shell control operator %q is not supported in manual Helm commands", field)
		}
		if strings.HasPrefix(field, "-") {
			trailingArgs = append(trailingArgs, field)
			if helmFlagConsumesValue(field) && i+1 < len(fields) {
				i++
				trailingArgs = append(trailingArgs, fields[i])
			}
			continue
		}
		if len(positionals) < 2 {
			positionals = append(positionals, field)
			continue
		}
		trailingArgs = append(trailingArgs, field)
	}
	if len(positionals) < 2 {
		return manualHelmInvocation{}, fmt.Errorf("command must include a release name and chart reference")
	}
	return manualHelmInvocation{
		operation:    operation,
		releaseName:  positionals[0],
		chartRef:     positionals[1],
		trailingArgs: trailingArgs,
	}, nil
}

func helmFlagValue(fields []string, flagName string) string {
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if field == flagName && i+1 < len(fields) {
			return strings.TrimSpace(fields[i+1])
		}
		if strings.HasPrefix(field, flagName+"=") {
			return strings.TrimSpace(strings.TrimPrefix(field, flagName+"="))
		}
	}
	return ""
}

func renderManualHelmCommandTemplate(helmCommand, kubeVersion string) (string, error) {
	fields, err := parseHelmCommandFields(helmCommand)
	if err != nil {
		return "", err
	}
	invocation, err := manualHelmInvocationFromFields(fields)
	if err != nil {
		return "", err
	}
	args := []string{"template", invocation.releaseName, invocation.chartRef}
	args = append(args, helmTemplateCompatibleArgs(invocation.trailingArgs)...)
	if kubeVersion != "" && !helmArgsIncludeFlag(args, "--kube-version") {
		args = append(args, "--kube-version", kubeVersion)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		return trimmed, fmt.Errorf("helm template timed out")
	}
	if err != nil {
		return trimmed, err
	}
	if trimmed == "" {
		return "helm template completed without output", nil
	}
	return firstNLines(trimmed, 8), nil
}

func helmKubeVersionFromRKE2Version(rke2Version string) string {
	version := strings.TrimSpace(rke2Version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	if plus := strings.Index(version, "+"); plus >= 0 {
		version = version[:plus]
	}
	if version == "" {
		return ""
	}
	return version
}

func helmArgsIncludeFlag(args []string, flagName string) bool {
	for _, arg := range args {
		if arg == flagName || strings.HasPrefix(arg, flagName+"=") {
			return true
		}
	}
	return false
}

func helmTemplateCompatibleArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := strings.SplitN(arg, "=", 2)[0]
		switch name {
		case "--install", "--wait", "--wait-for-jobs", "--atomic", "--cleanup-on-fail", "--dry-run", "--debug":
			continue
		case "--timeout":
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
			}
			continue
		default:
			filtered = append(filtered, arg)
		}
	}
	return filtered
}
